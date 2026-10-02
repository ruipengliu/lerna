# 核心四模块写作记录

## 已确认的读者与授权

读者熟悉接口、数据库事务和后台作业，不预设 Harness 专有概念。用户已经确认完整任务入口，并授权智能体自行选择后续章节和段落、编写后交独立读者审查。因此本轮应用 writing-shape / writing-beats 的概念铺垫、逐段推进和写前重读，不再次要求用户逐段选择。原始 `.draft`、研究和 fragments 不修改。

采用的素材清单为 [inventory-core.md](inventory-core.md)，旧总览为 [overview-before-shaping.md](overview-before-shaping.md)。迁入后四模块的逐文件快照及摘要保存在 `core-before-shaping/`；它们只用于覆盖核对，不作为正式规范导航。

## 开篇与推进路径

| 模块 | 考虑的开篇 | 选定路径及先后引入的概念 |
| --- | --- | --- |
| Orchestrator | 报告任务；固定任务身份；写成后丢答复 | 报告任务 → 完成要求 → 一轮判断和行动 → Task/Decision/Operation 交接 → 同事务责任 → 控制和结果 → 字段、验证、预算及存储查阅。 |
| Brain | 本轮如何选下一步；双系统术语；单调用恢复 | 报告资料逐步取得时的本轮建议 → 固定输入 Decision → Proposal 与重新准入 → 规则/模型路径 → 输入/输出 → 模型调用恢复。S1/S2 在功能落地之后解释。 |
| Executor | 保存报告；效果 unknown；设备控制 | 固定报告版本和路径 → 接纳 Operation → Attempt 与目标效果 → 最后准入/准确绑定 → 结果覆盖 → GUI 观察和接管 → 契约与恢复。 |
| Collaboration | 两项资料核实的有界子目标；父子事务；外部失联 | 父任务交出有限子目标并保留整体完成责任 → Delegation 与唯一子映射 → 内/外部事务边界 → 进展、控制与关闭 → 账务及恢复。 |

各 implementation 开篇承接本模块正常链，先指明调用步骤对应哪些存储决定，再展开组件及持久表；每节已有数据、事务、恢复和验收规则保留。专题先给实际问题，再进入规则及例外。每次修改前重读当前文件，避免覆盖并行修改。

## 主定义分配

| 内容 | 主定义 | 其他章节的处理 |
| --- | --- | --- |
| Task 正常行为及各层成功含义 | orchestrator/README.md | 字段、算法和保证细化链接到专题。 |
| Task 字段、内部关联及方法 | orchestrator/records.md | 整合旧正式记录表和迁入草稿字段表；README 留入口，implementation 定义物理适配需求。 |
| 条件、覆盖、完成依据、证据缺陷 | orchestrator/verification.md | 保留旧正式补充，README 说明完成含义；implementation 保留存储、唯一约束和 gate 锁。 |
| 预算模式、交接与结算含义 | orchestrator/budget.md | 汇合旧正式与草稿说明；implementation 保留账本算法和并发约束。 |
| 任务修订、提案、用户输入和控制传播 | orchestrator/task-lifecycle.md | 保留旧正式成果；Brain/Executor 的最终编码与物理效果指向原模块主定义。 |
| 公共 Command/Job/Claim 算法 | reliable-work.md | durable-work.md 作为 Orchestrator 恢复阅读路径；领域差异指向 Brain/Executor/Collaboration。 |
| BrainContext、最终模型请求及 publication | brain/implementation.md | task-lifecycle 说明 O 的输入责任与调用点，不重复一份物理发送算法。 |
| Capability/Binding/Invoke/Operation 与实际出口 | execution/README.md、implementation.md | task-lifecycle 保留 O 的行动准入；durable-work 链接真实发送、结果与设备恢复。 |
| 委派映射、phase 和 Closure | collaboration/README.md、implementation.md | O 只组合本域任务和委派事实。 |

## 来源覆盖与审校状态

本节随正文修改记录每个源章节的落点、合并原因和验证结果。全部源规则仍以 `core-before-shaping/manifest.json` 与保留的 `.draft` 可核对；只删除重复规范的临存 README.draft-source，不删除来源档案。

四模块正文已完成本轮整理。新增 `orchestrator/records.md` 和 `orchestrator/budget.md` 承接既有字段及账务定义；整合后的临存 `README.draft-source.md` 已删除，原 `.draft` 和逐文件快照仍保留。下表的源文件名对应 `core-before-shaping/manifest.json`，目标均为正式目录。

| 完整读取的源文件 | 正式落点与保留范围 |
| --- | --- |
| 旧正式 orchestrator/README.md | 主线、协作边界、任务闭环、共同提交、失答复时序、控制和内部组件留在 [README](../../docs/architecture/orchestrator/README.md)；原全部任务记录、准入来源及 Content/Grant 关系移入 [records](../../docs/architecture/orchestrator/records.md)；完成依据表、最弱依据、五步完成算法、覆盖、缺陷与准确性在 [verification](../../docs/architecture/orchestrator/verification.md)；预算变更表及原账更正移入 [budget](../../docs/architecture/orchestrator/budget.md)；存储访问细则由 access-paths 与 implementation 承接。 |
| 迁入 orchestrator/README.draft-source.md | §1 接纳与模块交接、§2 目标及验证进入主 README、records、verification 和 implementation 的 task-admission/proposal-consumption；§3 状态及控制由 task-lifecycle 定义；§4 快照与准入由 task-lifecycle/implementation 定义；§5 原责任、有限进展和调度由 durable-work 及 implementation 定义；§6 全部预算模式、接受事实、allocation、事故、更正、方法及上限进入 budget；§7 字段、方法及 task.list 原规则进入 records；§8 七项判据由现有 RT-01–09、RT-13–19 及 verification 验收承接。关系图的信息进入 records 的记录表和 implementation 的对象关系，不另保留一张重复字段图。 |
| orchestrator/task-lifecycle.md | 状态、修订、固定输入、提案裁决、进展、统一准入、计划实例化、参数绑定、全部用户消息解释及消费、控制、设备接管、祖先控制、目标修订后的委派与证据保留在[同文件](../../docs/architecture/orchestrator/task-lifecycle.md)。S1/S2 的选择与规格移向 Brain 主定义，O 仍保留固定 Decision 链、反馈消费和跨轮升级责任；模型最终字段与发送顺序移向 Brain 实现。新增 context-assembly/task-input 稳定锚点。 |
| orchestrator/durable-work.md | 原就绪条件、三类入口、task.submit 共同提交时序、Brain 与 Executor 阶段差异、领域责任键、控制版本竞争图、保留及十一项断言留在[同文件](../../docs/architecture/orchestrator/durable-work.md)。公共 Claim/Guard/Finish、提交未知和续约未知移向 reliable-work；数据库方言、迁移互斥及 SQLite 宿主锁移向 storage/deployment/engineering；真实出口、结果覆盖和设备恢复改用 Executor 主定义。各处保留具体恢复入口而非泛化为自动重试。 |
| orchestrator/implementation.md | [同文件](../../docs/architecture/orchestrator/implementation.md)的模块/公共模板、全部持久记录、条件存储及门禁、锁序、接纳、提案与计划算法、控制、预算两端和更正、清理、生产及 RT-01–24 保留。开篇和导航重排，补回提交不等模型及可补能力等待的解释；字段主定义指向 records/budget，快照组装指向 task-lifecycle。 |
| orchestrator/verification.md | [同文件](../../docs/architecture/orchestrator/verification.md)全部验证类型、职责、登记/规则/实现选择、核验正常链、目标覆盖、缺陷、准确性准入及验收保留。将正常链置于实现选择之前，合入旧正式完整完成算法与 completion_basis 表。 |
| orchestrator/access-paths.md | [同文件](../../docs/architecture/orchestrator/access-paths.md)全部有限符号、八条访问表、索引访问条件、返回集合及锁范围、完整性证明、证据门禁和性能取证边界保留；开篇改从报告成功前的当前集合读取解释。 |
| orchestrator/scheduled-triggers.md | [同文件](../../docs/architecture/orchestrator/scheduled-triggers.md)规则/occurrence/Task/Job 分工、时区及有限错过/重叠策略、固定命令交付、停用/取消/恢复和验收全部保留；开篇引入每周报告。仍无公共 Schedule API/Schema 或运行保证。 |
| brain/README.md | [同文件](../../docs/architecture/brain/README.md)输入输出、五种提案、S1/S2、ModelProfile、上下文边界、单 Decision/ModelCall、模型恢复、费用与验收保留。开篇先解释本轮报告建议和正常四步；request_input 纠正为原 O 创建/一次消费 InputRequest，Interaction 呈现与转交；typed_decision 候选在普通路径之后说明。 |
| brain/implementation.md | [同文件](../../docs/architecture/brain/implementation.md)模块、公共框架、持久记录、BrainContext 完整字段、来源/水位/当前资格、最终编码、准备/发送/发布、费用交回及 BI-01–20 保留。SnapshotAssembler 的六步组装以 O 的 context-assembly 为主定义，Brain 保留接收契约与再次核验；开篇区分报告判断的三个中断点。 |
| brain/decision-paths.md | [同文件](../../docs/architecture/brain/decision-paths.md)普通、规则、固定答案类型的能力选择、规范、拒判与升级、候选选页和启用实验保留。开篇用报告选页解释选择；Jev 的供应商状态和价格观察注明来自 2026-09-28 研究快照，启用前仍须重验，未改为当前保证。 |
| execution/README.md | [同文件](../../docs/architecture/execution/README.md)默认选择、全部交接/准入、TaskGate/取消墓碑/有限启动窗口、三类效果恢复、API/文件/原结果/GUI、所有字段方法及 E-01–14 保留。开篇从固定报告保存引入 Operation、Attempt 及四步正常链，范围和可选能力移到验收段。 |
| execution/implementation.md | [同文件](../../docs/architecture/execution/implementation.md)全部结构/Store/port、公共模板、记录/索引、最终参数与编码、接纳/启动/查询/控制/取证、模拟设备、固定宿主恢复、生产与 EX-01–31 保留。开篇按接纳、发送、交回断点导航；读者反馈修正了组件版本切换与宿主进程退出两类排空的作用范围。 |
| execution/programmatic-tools.md | [同文件](../../docs/architecture/execution/programmatic-tools.md)有限计划/交互 cell、原 host-call 映射、隔离限制、费用、取消/实际退出、被动检查点、可复用环境最小合同及 X-06/X-08/HAR-09 保留；新开篇从已有报告材料筛选组合解释。完整环境协议仍为待交付能力。 |
| collaboration/README.md | [同文件](../../docs/architecture/collaboration/README.md)内部同事务/外部唯一创建、全部权限/额度收缩/费用更正、控制、phase、字段/五方法、恢复和 CO-01–07 保留；有界比较子目标前置，Delegation/allocation 正常链先解释；SDK 等待放到方法后，子会话复用放到实现边界。 |
| collaboration/implementation.md | [同文件](../../docs/architecture/collaboration/implementation.md)全部组件、公共模板、表/唯一键、phase 派生、内部创建、外部映射、跨 O 接收、进展/控制/输入/Closure、异步读取/冷恢复/可复用子会话、保留及生产与 COL 实验保留；开篇按父报告如何找回原有子任务和原额度导航。 |

### 跨模块重复定义的核对

旧正式 durable-work 中移出的数据库配置、迁移和公共工作规则已与基础组确认：SQLite 的 WAL/FULL/foreign_keys、单写队列及生命周期 OS 锁；迁移互斥、固定 migration_id、检查点和不可变内容批次；Claim 未知时保留 observed_work_revision、续约未知沿最后确认期限、current 小于 observed 属存储错误、失效 Claim 回滚整笔保护事务，均在对应主定义保留。

旧正式完成与记录内容按段搬入专题，字段及算法未以早期草稿覆盖。目标覆盖、当前所选检查、准确实现的 evidence gate、原判断缺陷及完成串行顺序仍分别可定位；Result 固定后以 notice 解释迟到缺陷，正常 retirement 不否定旧证据。

### 研究与候选的覆盖

五项目及比较研究逐项见 [inventory-core §8](inventory-core.md#8-研究中的采用负例与候选)。O-01 输入重建和最终编码落在 task-lifecycle/Brain；O-02 有限续行落在 O implementation；O-03 最后准入、准确绑定和出口落在 O/Execution；O-04 分域发送恢复、冷恢复资格落在 durable-work、Brain/Execution/Collaboration；O-05 原结果、覆盖和来源顺序落在 Execution；O-06 异步子责任落在 Collaboration；O-07 输入竞争和呈现由 O 的输入段与 Interaction 交接；O-08 装配归 Extensions；O-12 故障语料归各模块和 validation。

O-09 自动改进、O-10 程序化增强、O-11 渐进发现继续使用 X-07/08/09 的独立证据门槛。G-03 子会话复用、G-04 Schedule、G-05 可复用环境只保留已写最小边界，不承诺尚未冻结的 child.send、Schedule 或 kernel 公共互操作；G-01/02 的 Session/自由输入范围由全局和 Interaction 保持。研究观察保留固定日期和适用范围，不把旧项目的 flush、SDK 重试或 sandbox 名称当成本方案的耐久和隔离证明。

### 本组审校与验证

本组已检查正常链中的创建者、消费者、提交点及未知后的继续者，并逐项核对旧正式三个文件和迁入材料。独立运维读者提出的排空歧义已修正：版本切换停止旧 binding 的新 Operation 接纳；执行宿主退出关闭新实际启动；原效果、停止和费用核对均保留。

应用读者 APP-02 的无 Surface 澄清缺口已补齐。已有 Task.wait_reasons、InputRequestView 及 interaction.request_read 是依据；完整披露的 input 等待必须含准确 ObjectRef。task-lifecycle 解释直接读取和回答路径，records 定义字段含义，implementation#input-waits 给出 input_requests 与等待引用、Task 修订的共同提交及当前披露行为，并增加 RT-25。后续 APP-01–03 机器合同由本组补齐，见下节；没有新增方法、服务或输入账本。

公共 Schema/序列、图形渲染和三类独立读者复核由根智能体统一收束。以上是文档和设计核验，未运行数据库、目标驱动、模型质量或生产容量实验。

本组收束时的检查结果：`validation/check_documents.py` 通过，覆盖 64 份正式 Markdown、2147 个本地链接和 112 个 Mermaid 块，链接/锚点/表格/围栏共 0 个错误；本组范围的 `git diff --check` 通过。16 份原正文快照摘要全部匹配，`docs/architecture/.draft` 和 `docs/research` 均无 Git 改动。该检查不证明图形渲染或领域运行正确。

### APP-01–03 机器合同补充

根统一处理应用流程和协议说明；本组落实相关 Schema、字节、定向反例及独立检查入口。`InputAnswerContentRef` 复用 `ContentRef`，限定 `application/json` 和 1048576 字节；四个已有 `answer_ref` 定义（含 Collaboration 转交）使用它。`WaitReason.kind=input` 条件必需准确对象引用，`InputRequestView` 的验收分支固定唯一必填 `decision` 单选和 `accept` 动作，SurfaceField 的五类约束在 Schema 中绑定。现有缺少 max_length 的协议反例期待值由根同步为 schema。

[input-answer.schema.json](../../docs/architecture/contracts/schemas/input-answer.schema.json) 定义封闭正文及完整 `ConfirmationConsumerCommand`；其 20 项共享依赖打包成本地定义，检查时逐项比较原协议，避免第二份漂移合同或远端解析。验收以当前请求的 task、request revision、goal revision、candidate hash 和原 approved Confirmation 准确修订为依据，完整消费命令及已存队列的 target_command_id 必须保持；意向摘要复用现有 JCS/Confirmation 实现。

[输入回答样例](../../docs/architecture/contracts/examples/input-answers/README.md) 保存 9 份真实 UTF-8 JCS 正文与准确引用、112 条回答坏例、3 组输入发现和 10 条发现坏例。普通字段检查未知/必填/可选空值、Unicode 码点长度、整数安全范围及布尔区分、选项 ID 唯一性和声明顺序，保留空文本、空多选、空格与组合字符。所有协议序列的 9 处回答引用已绑定真实字节；新增序列 66 串起无 Surface 的 Task.read → 准确 request_read → task.input，并检查其 1 处等待对象引用。

使用 `/tmp/lerna-architecture-formalization-proto/bin/python` 的本轮结果：`validate_input_answers.py` 上述全部正反例及绑定通过；`validate_protocol.py` 为 56 个序列、371 个指定规则坏例、105 个方法通过；`validate_transport.py` 的 100/228 结构向量、8/12 序列和 ES256/JCS 向量通过；本组范围 `git diff --check` 通过。这些是静态字节、Schema 和记录关系证据，未运行身份/权限/呈现/并发/事务或持久化服务实验。
