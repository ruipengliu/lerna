# 正式架构整理盘点：Memory、持久工作、存储、部署、契约与验收

盘点日期：2026-10-02。用途：供正式架构全集编写和交叉核对使用；本文件不改变现行架构。开始盘点时，本目录为空。当前状态：本组正文材料及契约／验证资产盘点完成；正式写作分工以 [spec.md](spec.md) 和派发记录为准。

当前设计语义以根 CONTEXT.md、现行 ADR、原正式正文及最新 docs/architecture/.draft 为准。研究只在其声明的版本、日期和实验边界内提供依据；旧形式化结果不能移作当前实现的验证。以下“必须保留”指正式正文仍须能找到这些实施约束，不要求按原文件逐字复制。行号均为本次读取源文件的行号；原正式总览另以 [overview-before-shaping.md](overview-before-shaping.md) 保留，不能用早期草稿反向覆盖后来细化。

## 1. 已确认边界与整理原则

- [CONTEXT.md](../../CONTEXT.md) L5、25–39、65–83：六组主线对象是组织读者理解的聚合，不限定物理表数；Task 固定归属原 Orchestrator；OperationIntent 与 Executor 效果不共同跨库提交；任务上下文、跨任务 Memory、Content、Grant、Confirmation 和使用结算各有责任。
- [ADR 0001](../../docs/adr/0001-retain-closed-identities.md) L3–5：最小去重与终态记录长期保留，正文按自己的许可清理。保留成本已被接受，不重新要求用户选择固定 TTL。
- [ADR 0003](../../docs/adr/0003-production-distributed.md) L3–9：生产分布式、单地域三可用区、托管优先、业务与 jobs 共库、通知可丢；开发可先单体及本机 PG 多进程，不建设公司级基础平台。目标 RPO/RTO 待真实验收。
- [ADR 0004](../../docs/adr/0004-discovery-fixed-task-routing.md) L3–5：受信发现为新任务选定逻辑负责方，调用方首次发送前保存原命令与目标；映射变化不改投原命令。客户端原记录丢失且目录无法定位时保留未知，是已接受代价。
- [ADR 0009](../../docs/adr/0009-reliable-work-framework.md) L3–5：共用接纳和有界工作模板、统一逻辑 JobStore；领域保留业务成功、外部效果、安全重试和自身事务范围。共享库不等于新增调度服务。

正式整理需要把一次用例串成“入口和输入 → 原身份和负责方 → 持久记录与唯一键 → 本地提交 → 事务外调用 → 对方持久接纳 → 本地归并 → 中断恢复 → 可观测断言”。当前资料有大量这些细节，主要风险是移稿时只保留概念总览，或将共同规则重复复制到各模块后发生漂移。

## 2. Memory 与 Content：必须保存的完整行为

源文件：[模块契约](../../docs/architecture/.draft/memory/README.md)，[实现](../../docs/architecture/.draft/memory/implementation.md)，[候选优化](../../docs/architecture/.draft/memory/optimization-plan.md)，[容量与验收](../../docs/architecture/.draft/memory/validation.md)。四篇均须保留可追溯去向。Content 是基础任务所需能力，跨任务 Memory 按需启用；整理时不能把关闭 Memory 理解为关闭内容与来源管理。

### 2.1 模块契约逐段覆盖

| 源范围 | 必须进入正式正文的内容 | 不能改写成的含义 |
| --- | --- | --- |
| README L5–54 | owner 的逻辑身份、每库单一写入权威、本地私密库与云库独立；Content 与 Memory 分工；版本化正文、候选索引、默认字面检索、单向视图的理由、代价和改选条件 | 模块等于微服务；失联端自动接管另一库；语义检索已经默认启用 |
| README L55–102 | fact/preference/inference/experience；保存、查询、copy 登记、当前正文取得与视图 ACK 的正常链；谁先持久接纳、谁在丢答复后继续；provenance、完整处理依赖与授权分别成立；原文到期不等于关闭全部派生物 | 上下文自动成为长期记忆；引用自动授予读取权；模型置信度证明事实或许可 |
| README L104–135 | create 原命令去重，replace/restrict/delete 的 expected_revision；普通纠正直达管理方法；旧修订退出查询，派生项 needs_review；正文先保存、引用与清理互斥；提取输入与唯一任务映射、检查点、逐条确认或有限预授权；任务结束触发去重与独立预算；时间、范围、冲突及准确命中去重 | revision_conflict 自动换版本重试；取消提取删除已保存 Memory；最近或相似度最高的记录自动覆盖另一来源 |
| README L137–161 | 处理前限定可授权集合；搜索需要 continuous，once 只用于预先固定范围的精确 read；默认排序；索引连续水位和权威补扫；冻结有限准确修订集合；各页当前复核；partial/changed/exhausted/gaps 的不同含义；管理 list/inspect 不依赖旧正文可读 | 全库计算后过滤等价于处理前授权；空页等于结束；集合结束等于当前授权全集；查询游标可以无限续期 |
| README L163–215 | local_only/controlled_remote/replicated；每种派生物及副本独立持有与保留；视图 S 切点和连续 Memory 变化；外部 Grant 扩权须新 view.open；离线有限许可；反向交付镜像；逻辑禁用/删除与逐 holder 停止/物理清理；旧备份恢复先禁用 | 同步或镜像 ready 取得永久/离线读权；发送关闭通知即对方停止；release.applied 即物理擦除；来源不可达等于已删除 |
| README L218–251 | ContentRef、SourceBinding、ContentPolicy、ContentControl、ContentCopyControl、MemoryRecord、Query/Page、View、CleanupReport、ExtractionCandidate 的字段语义；全部 Memory/Content 方法与成功点；content.get 的 bytes/control 两个分支及当前 holder 的最小收尾查询权限；错误后的原命令核对 | query 是异步 Command；content.get 创建副本；control 返回正文或新授权；错误自动证明原写入未发生 |
| README L254–258 | 用户级配额、关闭与清理保留份额、MI 与策略对照入口 | 静态 Schema 或文献分数替代运行、治理或质量结果 |

### 2.2 持久记录、事务与数据流

[实现 L13–132](../../docs/architecture/.draft/memory/implementation.md#module-shape) 将 facade、application、纯规则、MetadataStore、ByteStore 与 IndexWorker/ClosureWorker 分开。MemoryWriter 决定修订与发布，QueryService 保存有限集合，ExtractionCoordinator 只组织原提取任务和候选，ViewPublisher 保存快照与 ACK，ContentStore 管理字节及来源。外部调用不持数据库锁；公共框架不接管来源、发布或清理的业务裁决。index、extract、reference、copy_control、closure、view 六类作业的业务键、原标识、事务参与者和完成判据必须保留（L119–132）。query、续页、内容控制读取及原回执查询保持同步查询。

[实现 L134–202](../../docs/architecture/.draft/memory/implementation.md#data-flow) 的逻辑记录清单不能在正式文档中被“PG + 对象存储”四个字替代。每个唯一键隐含 tenant；业务修订与内容版本分别保存：

| 记录族 | 必须保留的键、约束与责任 |
| --- | --- |
| memories / memory_revisions / policies | 固定 owner、memory_id、单调 current_revision；准确修订的正文、来源和范围不可就地修改；策略亦按不可变修订保存；管理元数据与历史正文保留分开 |
| source_edges | 准确 derived_ref/source_ref 唯一、无环、完整实际处理输入；只要仍有受管派生物就保留必要关系；语义实体关系不能混入治理依赖 |
| memory_change_heads / memory_changes / index_checkpoint | 每 owner 已提交连续末端、owner+sequence 唯一的变化项、按索引版本独立的 covered_sequence；它们是同一序列上的不同事实，不是 Memory.revision |
| query_sets | query_id、认证主体、接收方、摘要、期限、有限有序准确修订集合、原始覆盖缺口；到期清理，不延长授权；管理列表冻结 ID、读取当前控制 |
| extraction_jobs / extraction_candidates | 原输入、规则与 Orchestrator 任务不可改绑；候选 ID、revision、pending/saved/rejected 和实际 memory_ref；候选终态与唯一发布 |
| views / view_snapshot_items | 固定视图范围、接收方、切点、有限版本列表与连续 ACK；每页及所需日志保留；不延长正文寿命 |
| content_objects / content_controls / content_copies / content_mirrors | owner/content/version 唯一及固定 hash/length；单调控制修订；copy 固定内容、holder、用途和保留期；镜像保留原 owner，无新所有权 |
| content_closures | 最小对象标识、关闭修订、必要关联及决定摘要，长期阻止旧请求/旧备份复活；无正文、完整参数、凭据或敏感解释 |
| reference_intents / held_copy_gates / copy_control_jobs | 原 Memory command、全部准确来源和原 copy/登记命令；control_revision 与 copy.revision 各自单调；发布与关闭共用 gate 行锁；持有期核对直至实际停止、complete 报告获接纳，residual/unknown 不删除责任 |

实现 L203–266 明确“字节先完整保存 → 内容元数据 → 引用发布”。同库引用登记与垃圾清理锁同一内容行；清理先封闭新引用，再检查无引用和 holder。分库则先共同保存 reference_intent、原 copy/命令及恢复 job，再事务外 register_copy，最后在本地 gate 事务裁决发布。远端关闭尚未同步的窗口内，本地发布可能成功；后续每次在线使用仍复核来源。已有本地 closed 不可被迟到登记/旧控制重新打开。表中五种中断（登记失答复、登记后发布失败、先关闭后候选、发布后退出、控制失败/迟到）均须对应原记录恢复，而非新建 copy。

实现 L267–340 给出比“有一个变更序号”更强的存储约束：锁 memory_change_heads 后，在同事务分配恰好需要的连续号段，保存 Memory、变化、head、回执和 jobs；回滚连同取号回滚，不能用 PG sequence、时间戳或 MAX(sequence) 代替。锁序是原命令及适用任务/预算/操作 → owner head → 内容/copy → 候选/Memory → jobs，同类稳定排序。view.open 与日志回收也先锁 head，回收使用取得锁后的新 READ COMMITTED 语句读取保留水位。每 owner head 是明确串行热点，须测量后才考虑另一变化发布机制。候选 saved 与正式 Memory、回执、索引 job 共同提交；索引写成但 checkpoint 未提交可按原范围重建。

实现 L342–402 定义 Scope 三维交集、空查询限制、字面算法及固定排序版本；获准范围先于候选计算，跨 owner 查询没有原子快照。读取 R、I、权威增量与默认同库索引使用短 REPEATABLE READ；库外索引须有匹配 I 的不可变段清单或等价快照。补扫 (I,R]、独立来源检查、冻集合后逐页复核不能删掉。每页前进或 exhausted，next_cursor 缺席当且仅当 exhausted；容量/补扫缺口要留到各页，权限扩大不向旧集合插入新成员。

实现 L403–469 定义提取与发布的持久接缝：检查点在输入和下一责任都持久后前移；真实模型调用保存全部输入；候选正文有期限；确认绑定准确 candidate_ref，create 与 candidate.saved 唯一提交。受信 memory-candidates 处理器消费 reject 事件，固定 target_command_id 并由 Memory owner 保存拒绝和暂存清理。取消阻止自动发布，已保存不撤销；用户仍可在当前许可内作新的管理决定。经验候选保存 before/after、旧准确修订、环境/工具、真实结果、失败/unknown、回退关联；批量提案是逐原命令结果，不是整批事务。改 Skill/策略或宣称普遍改善才转 Evaluation/Extensions，不把普通用户纠正升级为软件发布。

实现 L471–605 完整规定五种传输管理 kind、六类 Content 领域决定和无入站设备反向交付：先 register_copy，再固定 sender_endpoint/instance、receiver_service、ContentRef、copy、期限的 MirrorTicket 和有限空间预留，设备向受信配对地址主动上传，同原 ticket/upload_id 整份恢复。ready 只是字节；每次镜像读仍取得原 owner 当前 ContentBytesGetOutput。ReplyAck、view.ack、Change、领域 Receipt、upload ready 各有独立含义。restricted 对基础镜像也保守关闭，重建须新登记/票据；设备重启不继承旧上传实例权限，但当前认证 owner 仍可恢复关闭。view.open 在同一有界快照固定 S 与成员并登记日志保留，接收端同事务应用修订/墓碑、清理与游标后 ACK；缺口先禁旧视图再重建。

实现 L607–665 补全收尾及故障：use_stopped 需要封闭新入口且核对/终止已经进入的有限使用单元，行上写 closed 不足以证明实际停止；逐 holder 的 complete 才支持汇总。恢复先加载最小关闭及未结清理。元数据库、ByteStore、Grant/来源 owner、索引失效各有不同降级；设备镜像不成为设备 owner 的灾备主。扩展单位、串行键、query/字节两类成本、head/ACK/清理/重传观测、先限新提取/上传/视图的过载次序须一并进入部署与运行章节。

### 2.3 默认、候选与后置提案必须分开

[优化篇 L5–35](../../docs/architecture/.draft/memory/optimization-plan.md) 明确 B0 默认，B1、B2、关联扩展、模型审核尚待评测；算法和组件组织方式可借鉴研究，生产第三方依赖尚未选定。以下是已有候选方案的完整内容，不是本次新增架构决定：

| 源范围 | 已写出的候选设计与前提 | 正式整理的状态 |
| --- | --- | --- |
| L38–74 | 单可纠正断言/前提完整经验；否定、范围、事件/有效时间、来源片段、拟纠正准确引用；覆盖/保留/支撑分开审核；先处理许可与预算、后候选质量、最后保存许可；无结构化 as-of 查询 | 候选正文格式/解析器/样例尚待实现前冻结；不改现有严格信封，不增第五 Memory 类型 |
| L75–103 | B0 字面，B1 中文二元组+ASCII 词元+原词，B2 B1 与合法语义候选 RRF；首轮精确向量距离且隔离获准有限集合；两路独立水位、总预算、一跳双向已声明关联；建集合后分支失败保留缺口 | B1/B2/关联分别对照后才能启用；BM25、ANN、独立全文/图库不默认引入 |
| L104–126 | 首轮本地编码器、固定模型/tokenizer、无网络/隐式持久缓存/自主调用；index 与 query 各自 attempt/use/资源记录；计算结束不等于 checkpoint 提交；真实终止后释放槽位；向量/关系代次、准确来源、独立水位、受管清理 | 具体模型、CPU/内存/时限未冻结；远端付费嵌入和跨查询向量缓存未获采用；query 不承担持久补索引职责 |
| L128–167 | 元数据/前提 → 简短准确内容 → 必要时原材料/一次有界整理；Orchestrator 选材料，Memory 不填满上下文；经验 only 建议和前提；后台整理是时机而非权限；内部格式与投影不形成第二权威 | 不为每次 query 默认调模型；不按命中率自动删正式 Memory；停用优化不撤销已合法保存事实 |
| L170–181 | 结构化历史查询、远端付费嵌入/重排、经验转 Skill/计划、生产使用反馈四项跨模块提案 | 仅候选。须在需求、质量/成本及邻接契约通过评审后采用；当前行为与缺席降级已明确 |

[Memory 验收 L8–20](../../docs/architecture/.draft/memory/validation.md#capacity) 的初始数值（20 项/页、200 项集合、5 分钟、每用户 4 查询；1 提取、100 输入/候选；100 变更/页、8 视图；B2 每路 80、RRF 60 等权）是待测可下调配置。实际扫描、来源 RPC、字节、CPU、时限、任务累计预算另有限定。L25–73 的用户场景及 MI-01～27 都待真实运行，完整覆盖写入/索引/热写分页/双端确认/取消/孤儿/copy/ACK/备份/期限/私密派生/非保存输入/反向大内容/镜像在线读/目标篡改/关闭重启/Reply重放/跨库关闭/登记恢复/共享字节实例退出/control收尾/use_stopped/head提交顺序/快照增量/job新增责任/201条截断/旧copy worker。不能只留下 MI 编号而丢刺激和断言。

L76–126 给出候选反例和 E0～E5（另 E3a）的单因素对照：无长期记忆→B0、中文词法、混合召回、质量提取、双向一跳关联、按进展读时整理、最后已证组合。用户/会话/来源组隔离，独立 Memory 快照、holdout 不泄露，结果未知计入分母；提前冻结最小收益、逐类非劣、样本量与费用时限。未授权、重复发布或关闭后复活任一反例不能批准。L129–146 的 P0 可信基线、P1 低依赖质量、P2 按场景启用、P3 证据驱动扩展及逐阶段退出证据须保留，没有日历工期或已达标声明。

## 3. 研究材料的采用边界

本节按完整原文盘点，保留研究日期、固定版本、作者自报与本项目实测的区别；未重新联网更新其中价格、版本或论文结论。

| 源文件与全文范围 | 已进入当前设计/验收的内容 | 仍是候选、边界或历史结果 |
| --- | --- | --- |
| [Memory papers](../../docs/research/agent-memory-papers-2026-09-28.md) L1–155 | P01 覆盖/保留/支撑分审；P04 时间/替代/依赖；P05/P08 建设与在线全成本；P06 分层经验；P10/P11/P12 关闭、派生残留、再次写回与恶意材料全链验收。当前 optimization 与 validation 显式承接这些方向 | 12 篇按 2026-03-28～09-28 首次提交窗口、版本和实验设置；P02 环境探测、P03 主题摘要、P07 程序图仍按条件试验；不采用逐轮建图、自动语义覆盖、专用训练或论文分数目标。所有论文结果作者报告，未复现 |
| [Memory libraries](../../docs/research/agent-memory-libraries-2026-09-28.md) L1–196 | 固定 Go owner、PG、ContentRef 和来源/清理契约；LangMem 纯提取、Graphiti 时间/RRF、memU 外部合成、Letta 隔离整理、OpenViking 分层材料作为策略参考；故障/治理/全生命周期成本加入对照 | 七组固定提交和许可证记录不是已装配服务。Mem0 ADD-only/semantic候选入口及 history 残留、Letta仓库迁移和Git保留、memU分项提交、MemOS两条部署、OpenViking ACL索引一致性与AGPL均限制整体替换；没有选定生产第三方 Memory 依赖，没有全链删除/性能/依赖许可证明 |
| [System One](../../docs/research/system-one-models-2026-09-28.md) L1–109 | 有界选择与确定规则分开；精确候选、版本、分布、真实物理请求、关闭隐藏重试、缺 usage 不记零、未知账单与原标识恢复，是 Brain 对候选实现应保持的适配要求 | Jev 仅为只读语义候选；Choice/Score/Noul 不产完整计划/正文，confidence≠正确性/许可；计费上界、幂等/原结果与最终结算公开契约缺口未补齐。AnyJev、TypeLLM及模拟adapter是不同实验路线；无本项目调用、质量、费用、时延或恢复实测。价格/版本只保留核对日，不升为生产锁定 |
| [Model taste](../../docs/research/model-taste-x-2026-09-25.md) L1–54 | 文档写作可吸收“先抓关键取舍，解释维护代价、改变选择的条件，沿读者问题组织”的本次会话复盘（L34–50） | 六条X帖来自四人，体验与厂商声明、有限交叉样本不能决定架构或模型优劣；无可复现实验，不进入运行保证表 |
| [Formal methods](../../docs/research/formal-methods-for-architecture.md) L1–100 | 用状态/动作/前提/性质澄清提交边界；安全、活性、可达性与环境条件分别表达；模型动作→文档提交→实现入口→运行假设的轻量映射可用于正式验收索引 | 研究篇本身未运行；后来进展指向归档。TLC有限配置、Lean定理与真实实现不是同一证据；未提供机器精化证明 |
| [All mechanisms coverage](../../docs/research/all-mechanisms-coverage.md) L1–483 | 四组逐机制、逐原用例、逐义务分类和跨组复用结构可作为覆盖矩阵方法；必须显式保留 not_modeled/uncovered 与 runtime-required | 2026-09-26 归档基线：104机制族、291原模块用例、6内容场景；communication L9–50、control L52–211、lifecycle L213–353、trust L355–458、跨组L460–470、内容L472–483，不能与当前九模块和当前编号直接相加或当现行已通过 |
| [All mechanisms results](../../docs/research/all-mechanisms-verification-results.md) L1–134 | 报告模板：规格/工具/参数/性质/哈希/日志、正常模型、故意错误、见证、去公平性反例分别统计；形式、静态、运行各列边界 | 历史新增22个TLA模型、7份Lean、170项预期结果；30正向、77负面对照、55可达、1去公平性、7Lean。旧20 Schema/122类型/285消息/89非法也属于旧基线。完整生产验收执行数0，未证明当前架构、组合系统、平台耐久、物理隔离/删除、容量 |
| [Handoff results](../../docs/research/handoff-verification-results.md) L1–165 | “先持久接管再确认卸责”、事实与易失知识分离、原身份去重、预算耗尽保留gap、迟到确认可收尾；作为规则解释和旧形式结果参考 | 只证明两提交域单固定责任抽象。安全与有条件活性、四配置结果、错误/非真空见证、Lean17项公理审计均按原工具/范围；不证明多租户、多操作、fencing、取消/授权/账务组合、真实外部效果或数据库耐久；不把Work至多一次提升为外部效果exactly-once |

这些材料不会迫使用户重新选择 PostgreSQL、单写权威、长期去重记录、公共 JobStore 或 Content/Memory 分工。研究候选需要独立实验或后续契约评审；它们不阻塞当前基线正文整理。

## 4. 公共可靠工作：机械保证与领域责任

[reliable-work.md](../../docs/architecture/.draft/reliable-work.md) 全文 L1–313 的定位是待实现的内部规格，没有运行库、PG/SQLite 适配器或故障结果，也不新增公开方法。正式正文应集中定义公共规则，各领域保留映射和不同的发送/效果判断；不能把每个步骤都包装成同一种 prepare/run/commit 状态机。

| 源范围 | 必须保存的实施内容 |
| --- | --- |
| L5–40 | 先有权威库、同事务参与者、身份/可信时间、有限容量与领域恢复器；共用模板及逻辑 JobStore 的理由、代价和收缩条件；共享实现可跨宿主装配，不产生跨库事务或新业务 owner |
| L43–81 | Within(scope,participants,fn)、Lookup/Reserve、SaveReceipt、Raise、Hint、Claim、Renew、Guard、Finish、Handle、Reconcile 的输入输出和失败结果；Tx不跨租户/库/闭包/goroutine；全部领域锁后再锁job；隐式外键/唯一锁纳入验收；当前数据库时间裁决；Committed/明确回滚/CommitUnknown 分开；只已确认回滚的纯本地逻辑可有限重跑 |
| L84–118 | 当前身份和路由先验；tenant+logical_service+command唯一键与规范摘要；配对仅受信预认证域例外；原回执→最小关闭→首次准入查找顺序；无空占位；准备与责任共提交；只有方法登记允许时才返回accepted；applied-only方法有准备时有限等待/传输超时并继续原责任，不新增pending或误报not_found |
| L121–155 | scope/业务键/kind、不可复用job_id、source_ref、state/due_at、work_revision、lease_epoch/lease_until/holder、固定observed_work_revision、领域尝试预算；新责任才Raise，通知/领取/续约不增work_revision；Raise可重开done、提前due_at，不抢有效领取、不改payload或清累计预算 |
| L158–208 | 先预留实际容量再有限领取，提交后处理；领取未知先查原候选Claim，快照丢失不可拼造；请求context不继承后台工作；续约未知沿旧截止停止；cancel不证明调用退出、不提前放槽；Finish把领取失效、版本落后、已覆盖、继续等待、异常版本五分支分别处理；同事务Raise后Finish不得覆盖新责任 |
| L211–243 | 旧worker的受保护回写拒绝，但独立认证且核实的迟到事实可另事务归并；7类失败点由原标识恢复；持续关联先建持续核对责任，Hint不能重开done；内存准备/接口写入/耐久提交/可能越过外部边界分别观察；Reconcile只修复缺失job，不代替正常共提交；终态对象仍可能有费用/清理责任 |
| L246–260 | 分类池、用户公平轮转、冻结并发/扫描/事务/续约/退避；Schedule/occurrence是应用调度职责，JobStore不解释时间规则；诊断/控制/结算/清理保留容量；先停新领取再排空；PG/SQLite/领域物理表映射兑现同一逻辑保证，不复制第二责任账本；旧格式迁移和旧worker隔离 |
| L263–296 | command/object/job/holder/两种版本的可追踪关联；事务、调度、请求、恢复放大、费用分别计量；最老未覆盖责任年龄不可因Raise重领归零；FW-01～09在真实PG/SQLite和每种实际表映射运行，真实业务入口生成责任、独立目标真值，不手改job构造通过 |
| L299–313 | 九模块接入索引及领域裁决保留；公共消息、回执阶段、方法错误仍归同版contracts。框架通过不等于领域外部效果和成功通过 |

关键交叉链接须双向保留：Orchestrator 锁序/准入与Finish、Brain发送标记、Executor准备后可能已发送、Memory head与copy、授权原use结算、交互投递回执、协作子责任、扩展迁移就绪、评测固定分母。丢答复、领取失效、Effect=unknown、CommitUnknown、依赖不可用、领域拒绝不能合并成一个通用失败。

## 5. 存储布局、中间件与可恢复位置

[storage-and-middleware.md](../../docs/architecture/.draft/storage-and-middleware.md) 全文 L1–160 不是泛泛技术选型，而是各权威、字节、工作发现、当前资格与备份的存储规格。正式部署章节必须保留下列对应关系：

| 源范围 | 必须保留的记录、机制和边界 |
| --- | --- |
| L5–37 | 托管PG18、跨区对象、现有连接池/平台；shard与partition区分；Task/内部树/预算/commands/jobs共本地事务，Grant父链不拆库、Confirmation随消费owner，Memory/Content默认共库；受管文件根与journal分属目标字节/权威元数据，单故障域根不因云应用多区而自动可接管；身份按稳定映射分片，派生缓存无权裁决不存在/权限/余额 |
| L39–50 | user_source_directory按认证tenant/user在身份权威维护来源、每来源全部披露授权权威及单调目录版本；同一已提交视图返回完整集合；新路由/开户/共享扩权先预登记再生效，共用发布屏障；首屏续页核当前版本及全部authority修订；损坏恢复必须核完整备份、映射历史、预登记及全部潜在授权owner引用，无法证明只报部分/缺口；整分片维护搬迁先停新写/发送准入、隔离旧写者、取完整切点、验证后改物理映射，逻辑owner保持，不承诺在线重分片 |
| L53–83 | business/receipt/jobs或Delivery先共提交，独立短事务NOTIFY；通知失败不回滚成功业务、不要求逐条可靠补发；按类别/due/stable键SKIP LOCKED有限领取；先LISTEN提交后读当前到期；每进程有限扫描器，不为每WSS轮询；NOTIFY不含正文/凭据、不产生责任版本 |
| L86–100 | 生产先复用公司托管事务池，必要时平台装PgBouncer；本机有界直连；池不选主，扩实例先算所有连接；每事务租户上下文/RLS不绕过，LISTEN/会话advisory锁避事务池；当前身份/Grant/内容资格不换成长TTL allowed缓存 |
| L103–118 | 原upload/ticket、长度/hash/state、bucket/key/不可变对象版本；跨区字节耐久及校验后ready，随后业务引用提交；换实例查原对象、部分上传同键整份重传；孤儿与引用/清理互斥；对象缺失保留内容缺口，不改成目标未发生 |
| L121–138 | 活动责任独立热索引；全原命令键HASH分区和唯一性，不按月份分散去重；历史正文与最小终态记录分开；copy控制RPC仍逐准确copy，不虚构批量接口；partial index固定状态、时间放查询；LIMIT不是实际扫描上界；autovacuum/ANALYZE、WAL、副本、备份、重建及长期记录增长纳入成本 |
| L141–147 | 初始10%剩余空间保护水位并非收尾可写证明，接纳须兑现最坏回执/控制/收尾空间；控制无法提交不报取消已存；长期去重增长公式及参数实测；备份绑定DB切点、准确内容、InstallLock、去重终态、映射、完整来源目录/授权集合版本、映射历史、凭据恢复依据；旧备份缺后续历史只读诊断，先隔离原写者，SQLite用一致备份而非复制活动主文件 |
| L150–160 | NATS/Kafka/Redis/独立向量全文/自建HA五类竞争选择的当前成本与重新选择触发；默认均未增加。无通知扫描、通知队列故障、池退出、DB提升、对象后实例退出、长期身份增长、跨库copy关闭和旧设备入口恢复的首轮实验仍待运行 |

容量推导是设计责任：每日500万Task仅Task身份一年约18.25亿，command/operation另计；平均单条B、WAL、索引、备份与重建均未测。正式稿不能删长期身份以“简化数据模型”，也不能承诺只靠加worker解决权威库或热行瓶颈。

## 6. 宿主、生产运行与工程实施

### 6.1 共同宿主与期限

[deployment.md](../../docs/architecture/.draft/deployment.md) 全文 L1–180 同时定义技术基线、装配、持久接口和恢复入口。Go 1.27、PG18、gRPC严格JSON外壳、端云WSS、SQLite WAL/FULL、默认字面检索等是源文件核查日的选择；精确补丁及依赖摘要尚须安装锁定与实际兼容验收，不在本次整理中升级版本。

| 源范围 | 正式正文须保存的内容 |
| --- | --- |
| L5–25 | 生产角色分池、单体只作开发；Linux参考生产及Linux/macOS端侧、Go interface与服务RPC不同边界；更换技术的明确条件；不把技术可用性当成已实现产品 |
| L27–76 | 生产主部署、本机Orchestrator+云能力、云Orchestrator+端侧能力三类真实装配；开发单体/本机PG多进程/公司平台的证据各算；无公网依赖的全本机任务、混合失联的有限继续范围；Brain/Memory/Executor各须远程与独立替换，本机Brain调云模型不算 |
| L78–119 | PG当前写域、SQLite参数及OS持久条件；不可变字节先保存；五类故障恢复；transaction/command_store/job_store/content_store/identity_store/credential_store/user_source_directory/ClockAdapter七组宿主接口；Tx参与者与锁序、原命令占位不可空提交、SQLite单写短BEGIN IMMEDIATE；context取消不证明业务或调用终止；TaskPolicyRegistry只在受信同事务范围支持已获接受的估算模式 |
| L121–138 | UTC可信区间ε、覆盖允许暂停的经过时间误差ρ、检查到实际入口余量κ及信任代次；消费端要求本地UTC上界+签发者ε+κ<start_before且服从先前固定截止；重复回执、回拨、重新采样不得延长期限；共库也检查绝对期限 |
| L140–158 | 当前主/格式/最小关闭与原责任/准确安装及当前批准/实例ready顺序；首次根身份和受限首装；本地OS句柄排他锁而非PID文件、多机共享本地库拒写；每migration_id开始/检查点/完成和精确步骤摘要；内容迁移新不可变版本及原版本交接前可读；不得用DB回退删真实效果 |
| L160–180 | 诊断与获准查询、持久控制及恢复、新任务与新效果三种入口分别就绪；绑定本次实例/库/版本/配置，局部封闭；恢复入口可领取不等于未决全部清零；启动断点观察出站次数与原回执/jobs；空间不足不虚报已取消 |

期限公式必须保留为可验收义务：进程租约L=15秒只是初值，最迟本地停止点不晚于 `m₀ + (L − 2ε_db − κ) × (1−ρ)`，括号须正，窗口从续约请求发起计算。首次答复在新截止前，续约答复还须在上一已承认截止前；过期boot不复活。数据库在实际裁决处取当前时间，主库换钟证据不足先停续约/回收，隔离旧持有者或可信等待完整窗口后耐久关闭，不能先复用槽再等旧进程停止。这是平台验收前提，不能简化为“使用monotonic clock”。

### 6.2 生产拓扑、数据流与恢复

[deployment-production.md](../../docs/architecture/.draft/deployment-production.md) 全文 L1–536 的决策已确认：单地域三可用区、托管优先不绑定厂商、独立网关/应用/工作池/执行宿主；单区已确认账本RPO=0与控制/查询RTO≤60秒为待验目标。公司平台未就绪不阻塞本地编码；生产开放前必须获得真实旧主隔离、同步提升、内容介质、身份密钥、映射及完整来源目录证据。

| 源范围 | 必须保留的状态、记录与恢复约束 |
| --- | --- |
| L11–59 | 五种进程角色的权威与扩展单位、跨区拓扑、模块不等于服务；受管文件根另有故障域，账本可恢复不表示文件字节可接管；共享介质须独立验收路径解析、原子替换、持久化、独占及旧写者隔离 |
| L60–73 | 受信带版本的新任务映射、用户来源及全部披露授权权威预登记、首次发送前客户端持久原命令和目标；旧service地址不可静默指新owner；去重后判断首次接纳开放；新分片依次建立写域、装配恢复、容量/配额、目录/路由发布，再开接纳；回退仅改后续新任务，已接纳与来源不迁走 |
| L75–120 | WSS固定逻辑服务；网关外连接与应用内部绑定分开；外连接额度在身份权威、进程租约在相关分片、online_bindings在原逻辑服务、待发送在领域owner；跨库先额度再绑定、失败条件释放；binding_revision单连接单调，新流/新应用必新候选，同代次仅相同字段幂等；保留最高代次阻止迟到旧候选；旧清理不删新绑定；5/15秒租约与ClockAdapter共同成立 |
| L122–142 | 任意worker保存Delivery，当前应用发送器按自己的有效绑定、recipient与due索引有限关联扫描；Reply持久接收后ReplyAck，socket write不卸责；多连接可重复、原delivery/command去重；每进程有限扫描器、最长补扫1秒初值、无每WSS数据库轮询 |
| L144–212 | 业务同事务待发布提示，短事务锁提示头后有序发布；不能MAX(sequence)跳过未提交项；游标绑定主体/服务/过滤/窗口代次；先水位L和有界缓冲，再快照，后合并变化；内部重绑保守重建；24小时/字节双上限；5秒请求、60秒恢复、1～30秒全抖动、30分钟委托更新均不延长原期限；分层字节/关联/流槽上限与控制保留，序号实际出队分配；新增副本不迁移已有长流 |
| L214–256 | 客户端/受信宿主耐久保存完整Command、摘要、原服务和期限；本地记录失去时目录只枚举已接纳当前可披露Task，不能凭文本重造；权威当前读、准确不可变字节、可滞后索引/汇总及统计副本四类读边界；缓存/副本missing不证明未处理 |
| L258–328 | Task/首job/回执、OperationIntent/派发job、Executor效果各自事务；数据库切换后查原command/operation；PG同步on+ANY1异区落盘、提升前旧主隔离和全部已确认历史、恢复后第二耐久副本；不能自动降异步；恢复先控制/关闭/核对再新准入；十一类故障逐责任方处理；跨地域只有受限备份恢复，无同Orchestrator双写 |
| L330–362 | 接口每次外部调用、原command唯一接纳、每次query/receipt_lookup、固定探测失败时段分别计量；迟到查回补账本不改原超时；内部重绑不增加外部调用，缺探测不记成功；局部startup/readiness与liveness分开、不因依赖失败全池重启；旧事务未知查原命令 |
| L364–420 | DAU/任务/调用/驻留/字节/连接的负载展开、下界与完整后台写放大；资源公式、热键串行、SKIP LOCKED不是业务一致读；副本公式及故障剩余容量、μ>λ与B/(μ−λ)排空；分片扩容触发需全部写入、锁/WAL/连接/扫描/最老job等实测，旧Task不迁移 |
| L422–468 | 跨分片总额按受信固定owner份额及配置版本，全部分配≤总额；新增副本不复制额度，旧owner先耐久缩份额且核未结再增新方；失联份额不重分配；估算限制新承诺但不保证实际支出硬上限；控制/费用核对/推进/后台分池；身份/目录/绑定/字节四类缓存失效与有界回源；初始资源限额及MQ改选前提 |
| L470–493 | 网关、应用、worker、执行宿主各自停止新工作、排空及接替者；网关socket关闭后条件释放额度，未知释放查原connection、不扩大总额；应用换绑不重占外连接；5%批量、60/30/120秒只是初值；扩兼容格式→有界回填→切换→观察后收缩，原migration_id恢复；回退只兼容版，不回滚业务效果 |
| L495–536 | 关联ID放受权结构化记录、不作高基数指标标签；用量账本独立；来源目录缺口/费用outbox最老龄期/结算jobs/文件根unknown各自观测；PROD-01～32完整刺激、断言及报告环境条件必须保留，静态检查不执行这些实验 |

容量数字须附原假设：百万DAU、5任务/日、峰均5，导出平均57.9、峰值约290新任务/秒；4次模型调用×8秒导出9280在途，300秒驻留导出8.7万活跃；20次业务提交仅下界5800/s，200KiB/任务约0.95TiB/日。500新任务/s持续15分钟是另列未验证压力，不替换基线。`C=U×a×b` 初值20万WSS，1/2/4倍率、TLS/关联/HTTP2、快照轮换和重连成本均须展开；4MiB全排满781.25GiB是不能兑现的上限加总。`N≥ceil(L/(C×U×(1−f)))` 只适用对应无状态池，不适用DB写能力。

验收目标须连同分母、条件和局限发布：接口≥99.9%、同地域接纳/控制p95≤300ms及p99≤1秒、控制/查询单区RTO≤60秒、控制核对领取p99≤2秒/普通p95≤5秒、健康在线控制及单应用换绑p99≤5秒。在线到达不等于动作实际停止；新工作恢复、文件根耐久、最大未决年龄和积压另报。PROD覆盖包括同步切换、旧worker、洪峰、重连、内容、分片、灾备、配额、慢端、gRPC断答复、换绑、跨实例投递、暂停、跨库copy、迁移、恢复容量、ClockAdapter、发现、分母、job合并、Memory水位、集合、映射、跨源页、冷缓存、满额滚动、500/s、目录竞态、费用上调、大扇出暴露、GUI在途及文件根失联；不能合并成一个“HA测试”。

### 6.3 工程实施与具体依赖

[engineering.md](../../docs/architecture/.draft/engineering.md) 全文 L1–273 目前是待实现方案。2026-09-28已确认单仓、Go初期单module、显式SQL与独立PG/SQLite、React/TS/Vite、方舟OpenAI兼容调用及豆包独立搜索；本次不重新选择这些方向。

| 源范围 | 保留内容及状态 |
| --- | --- |
| L5–27 | 首条真实任务：接纳→提案→授权→受管文件→独立读回→完成；单体再PG多进程、专项替换端云、独立生产门槛；回放只证明机制 |
| L29–61 | 已选常规依赖：grpc-go/Protobuf、coder/websocket、严格JSON+jsontext/jsonschema、pgx/sqlc、modernc/database/sql、goose、openai-go、net/http、pnpm/React Router、IndexedDB/idb、Ajv/Vitest/RTL/Playwright、slog/OTel；版本须编译和运行锁定；原始Protobuf/JSON在宽松解码前检查；严格编码若库不支持则换实现不降契约 |
| L64–81 | 平台启动、健康、排空、发现、存储、身份密钥、时钟、观测八个边界及本地替身；受限管理入口、单独迁移动作与互斥锁；不是新增公开harness方法，不造公司控制面 |
| L84–169 | 未来api/runtime/sdk/cmd/internal九域+durable/host/wire/transport/storage/adapters/platform、contracts、两方言migrations、tests/dev/packaging/tools/docs目录；业务不依赖生成Proto/具体IO；api不导出表/job、sdk/ts不依赖React；契约仅一维护源、生成一致性及手写关联规则；独立真值只给测试判定器 |
| L171–205 | 105方法逐片开放、未实现明确不支持；最小装配与六组对象是责任清单不强制一对象一表；原正文一份，流片段不先成为事实；四类请求计量含重建/后续续行/辅助/收尾；1.1工程契约、1.2两DB与宿主、1.3文件任务、1.4真实模型费用、1.5Web多进程各自先决与退出 |
| L206–235 | 阶段二联网/三模拟设备/Memory/广度资源；阶段三三组件各独立替换及异构语言/端云/组合故障；阶段四受控改善、≥1000语义API、质量和规模；首次生产在实际开放范围独立过关，不等待全部功能，也不取消最终规模目标 |
| L237–273 | 方舟Chat Completions固定profile、原model_call_id、关闭SDK及代理隐式重试，每Decision至多一实际模型请求；请求未知/停止/最终费用能力分别声明；豆包Custom独立WebSearch默认10、上限50、Query1～100、NeedUrl=true/NeedContent=false、关闭rewrite/wait，HTTP/供应方错误和空结果分开；选URL另获准正文获取/重定向/截断/来源，模型内置web_search当前不启用；dev身份仅受限本机、生产拒启；OIDC公司参数后续冻结 |

待实现依赖、参数和平台取证不是未决架构冲突。真实凭据/模型profile/许可/费用依据缺席只关闭相应外部能力，其他独立切片继续。正式正文应留下具体切片和退出条件，不把工程篇缩为技术栈列表。

## 7. 公共契约及机器资产

本节为根作者提供交叉核对，正式 `contracts/` 与 `validation/` 由根作者维护，本组不改机器资产。源路径均在 `docs/architecture/.draft/`；完整文件清单由 [source-manifest.json](source-manifest.json) 保留。

| 源文件与范围 | 必須保存的内容与边界 |
| --- | --- |
| [contracts/README.md](../../docs/architecture/.draft/contracts/README.md) L1–144 | harness/1未发布、业务含义与Schema各自权威；accepted/applied/rejected逐方法定义；applied-only准备的有限等待而非伪accepted；tenant+logical_service+command原键、完整规范摘要、首次接纳期限与业务期限分开；长期最小关闭、gone/冲突/not_found；同进程/WSS/gRPC/HTTPS的边界；身份逐消息、三类投递/ReplyAck/集合恢复；15组错误及可执行retry、独立实现的查询与恢复义务 |
| [protocol.md](../../docs/architecture/.draft/contracts/protocol.md) L1–152 | full-harness-draft-2、105个frozen-draft方法、无reserved；2020-12封闭Schema、安全整数/JCS/十进制/准确引用；Receipt时间、stage和redaction；六对象类型的list+read映射及先订阅后枚举；成员与当前记录修订区分、权限变化使集合持续失效、每页扫描前进、partial/gap/到期/总额与恢复预算；Activation独立revision；费用超额、源账单修订交回、离线封账更正、exposure影响job、content.get两个分支；构造AuthContext/Grant等是前提不是认证证据 |
| [methods.md](../../docs/architecture/.draft/contracts/methods.md) 全文 | 105方法精确签名与Schema引用不可改成概念性接口名：brain3、collaboration5、evaluation13、execution15、extensions6、interaction10、memory18、security18、orchestrator17；方法子集声明同时承担对应查询/原回执及恢复 |
| [transport.md](../../docs/architecture/.draft/contracts/transport.md) L1–254 | harness-wss-draft-3：原服务发现、客户端持久原提交、Ready一次、连接与进程身份分离；消息/队列/控制/配额/心跳各有限；Cookie+Origin+CSRF与opaque bearer、有限pair.begin/claim、受信人类确认及sender主体不被网关覆盖；JWS ES256/JCS/kid/受众/时间；实际出队request_seq高水位、错误也消费、旧号关闭；三类Delivery、固定Reply与独立withheld、持久ACK；五种传输管理kind和整份字节恢复；镜像只读且保留原owner，每次在线bytes资格、restricted保守关闭；订阅四类缺口、权限范围变化及有限恢复；线错误与业务拒绝各算 |
| [grpc.md](../../docs/architecture/.draft/contracts/grpc.md) L1–174 | Call/EndpointChannel、一个外WSS对应当前内部流与至多一个候选；binding_id/revision/boot/limits精确不变条件、内部Ready不二次外发；5秒请求、60秒重绑、30分钟委托；原请求分类恢复，命令只查回执不盲重发；真实地址LIST+watch、20秒刷新/30秒有效、短RPC round_robin与长流槽独立容量；原始Protobuf字段扫描、oneof、严格JSON与外层/内层大小各检查；mTLS服务与delegated opaque主体、当前认证、禁应用写重试/hedging但仍去重；静态绑定不证明实际网络 |

机器资产是规范的一部分，须与引用同步迁入，不能在原草稿与正式目录形成两套维护源。本次只做文件/结构盘点，未借已有统计宣称重新执行通过：

- `contracts/` 共84个文件：5份主Markdown、4份examples说明、`harness.proto`、5份schemas及69份JSON示例。`methods.json` 当前105方法；`protocol.schema.json` 有359个 `$defs`，`transport` 27、`brain-generation` 4、`task-outcome` 5。这些是资产结构数量，不是测试数或运行能力数。
- 完成投影6份JSON含5正例与非法变体集合，专查Task终态、依据、效果及未结费用；它不包含完整目标覆盖，也不是数据库转储或线上轨迹。
- `examples/protocol/` 56份JSON（含invalid-mutations）覆盖原任务、控制、条件、预算、费用、执行、Memory/content、输入、配对、安装、评测、列表与授权变化；其README维护每条身份/修订/恢复关系索引。原40方法回归仍保留，不能以新增方法覆盖掩盖退化。
- `examples/transport/` 4份JSON保存结构、签名与请求序号轨迹，`examples/brain/` 3份JSON保存内部生成及计划实例化。前者的已认证连接/原高水位、后者的保存结果均为显式构造前提；不提供真实发送、耐久字节或模型质量证据。
- `validation/` 共21个文件，含下节5份Markdown、15份Python/JS校验实现及requirements（Python共14份，JS一份）。`protocol/{schema,exchanges,traces,runtime_rules,governance_rules,content_rules,collection_rules}.py` 是同一协议校验程序的模块，不能迁移漏掉子文件。`validate.py/validate_protocol.py/validate_transport.py/verify_transport_proofs.mjs/validate_brain.py/validate_lease.py/validate_release_recovery.py/check_documents.py` 分别保留原用途与实际运行结果边界。

## 8. 验收正文的全文覆盖

| 源文件及完整范围 | 正式正文应保留的实验组织与断言 |
| --- | --- |
| [validation/README.md](../../docs/architecture/.draft/validation/README.md) L1–202 | 证据分层、真实入口和三提交断点、独立真值/内容用途、单体→PG多进程→公司平台；SYS-01～29所有前提/刺激/观察；API按语义操作去重且每项10上下文、质量90%/95%分母、所有真实请求成本；双侧Wilson95%下界和目标点估计分开，相关样本/固定覆盖集/加权/重复不伪作独立总体；配对a/b/c/d净改善、最小实用收益/逐类非劣/费用时限/多次选择控制；target/statistical/improvement各pass/fail/inconclusive；V1来源正确与支撑、V2三独立设备；C1～9/A1～4证据映射及四阶段/首次生产独立门槛 |
| [fault-experiments.md](../../docs/architecture/.draft/validation/fault-experiments.md) L1–408 | L7–20六个故障宿主适配器与三个固定断点；L22–100接纳失答复、写成崩溃、旧worker/祖先控制、长期关闭、首装撤回、Reply与内容竞争；L102–200网关与内部重绑区别、满额/慢端/身份、跨实例路由、暂停、跨库Content/迁移、带持续流量恢复；L203–287 job新责任/旧Finish、Memory head、集合快照、ClockAdapter、长流扩容及SLO计数；L289–316派生状态消融与来源目录/费用/暴露/GUI/文件根边界；L318–357验证生命周期的十组竞态及八条真实SQL访问路径；L359–408报告判定、SYS-30～36交接、九域FW接入矩阵和成本。全部待运行 |
| [core-model-scenarios.md](../../docs/architecture/.draft/validation/core-model-scenarios.md) L1–237 | 应用/SDK主切面与固定环境/身份/证据；CM-01～04四类基础请求，C重建与继续分段；CM-05～10 Session原提交、多目标、输入撤回、继续权、Decision、Operation、Content/Grant；CM-11～17按需分支、准确绑定/隔离、子激活、Schedule、环境、Memory与公共工作，未开放合同各自列缺口；CM-S1～5静态变更/追踪/ADR/证据门槛；六种证据层未运行分列，不把Brain4/8/12与Executor0/3/6阶段数加成事务/IO |
| [harness-scenarios.md](../../docs/architecture/.draft/validation/harness-scenarios.md) L1–66 | HAR-01～11保留上下文完整出口、持久进展、最终参数及结果、耐久就绪、输入呈现、子冷恢复、暂存装配、经验候选、程序实际停止、目录发现、Session原Task交接；合法正例与故障均执行，只记录实际存在对象；固定源码成本研究不等于本项目收益 |
| [optimization-evidence.md](../../docs/architecture/.draft/validation/optimization-evidence.md) L1–138 | 九模块24项采用方向按落地/增补/实验区分，P0/P1与领域负责方；OPT-01～12组合；X-01～06压缩/读时整理/Skill/规划/委派/计划与X-07～09精炼/cell/发现各自单因素；开始前冻结主要收益、非劣、数据/来源组、费用/时限与尝试；批准/分批启用/当前Task原配置和回退条件；四工作包退出及O-01～12固定源码调研采用映射，不把候选当默认能力 |

验收方案内容必须迁入；实际历史检查数值如需保留，应引用当时报告及提交，不复制到新的“已验证”栏。本文自身的盘点和Markdown编辑也不产生任何数据库、真实出站、质量、互操作或生产通过记录。

## 9. 写作时须追踪的未冻结项与真正改选条件

| 类别 | 现状 | 本轮处理 |
| --- | --- | --- |
| 物理DDL与访问计划 | 已有逻辑记录、唯一键、串行对象、锁序、事务点与访问矩阵；具体列/索引/分区/迁移SQL、扫描/维护预算尚未编码及EXPLAIN | 保存实施约束和对应运行用例，允许实现后验证布局；不因未有DDL把方案退回泛泛概念 |
| 运行参数 | query/页/扫描/池/并发/字节/租约/发现/退避/排空/空间保护初值已有；ε、ρ、κ及真实允许暂停类型需平台证明 | 标初始或待冻结，保留公式、限制与先验冻结要求；不追问每个参数当架构问题 |
| 平台产品及配置 | 托管优先、公司平台、单地域三AZ和职责已定；产品适配、旧主隔离/完整提升/内容耐久/身份密钥/ClockAdapter证据未取得 | 明示首次生产门槛及失败封闭范围；继续本地实现与独立实验 |
| Memory候选质量 | B0默认；B1/B2、关系、提取审核、读时整理有候选和反例，模型/收益阈值/样本/成本尚需事前冻结 | 留候选正文和阶段路线，未验证不得默认启用；不要求用户现在选具体编码器或向量数据库 |
| 服务真实接入 | 方舟模型、豆包独立搜索与OIDC适配方向确定；精确profile、凭据引用、供应方原查询/停止/账单及网络外发资格未实测 | 缺一项关闭相应能力，记录unknown和计费义务；不把回放或SDK文档当接入已完成 |
| 后置能力合同 | 结构化as-of、远端付费嵌入、经验转Skill/计划/反馈及跨地域自动写、多owner全局配额服务/在线重分片均非当前基线 | 只有用户明确提出这些新保证才形成新架构选择并评审；本轮不发明字段、公开方法或新权威 |
| 中间件改选 | MQ、共享缓存、独立向量/全文/图库、自建HA、按命令分桶的热点租户路由均有实测触发条件 | 原选择已由ADR或当前设计确认，整理不重新投票；提案须给瓶颈、边界和恢复代价 |
| 正式目录与旧资料 | 已按用户确认迁入同名正式目录，原稿保留；旧研究中的相对链接有的已指未来正式位置，有的仍指草稿 | 规范主链引用正式定义，历史范围/日期不改；全局链接检查由根统一执行 |

本组没有发现需要用户重新决定的未解架构冲突。需要工程实现及实验冻结的项已与后置扩展分开。正文编辑及原正式总览的吸收记录见 [writing-storage.md](writing-storage.md)。
