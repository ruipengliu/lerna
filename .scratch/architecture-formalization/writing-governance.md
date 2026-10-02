# 治理与交互专题写作记录

日期：2026-10-02。按 [spec](spec.md) 已确认的读者、完整报告任务入口和后续章节自主选择授权，整理正式 interaction、security、extensions、evaluation 四模块。初次整理未改原 .draft、研究和机器 contracts；第 6 节记录独立读者反馈后的交互与公共契约协同修订。

## 1. 开篇选择和概念前置

| 章节 | 考虑的入口 | 选定路径及理由 | 已知概念与首次引入 |
| --- | --- | --- | --- |
| Interaction README | 直接从 Surface 类型表；从用户跨设备回答保存目录 | 选后者：先能区分消息保存、任务采用回答和报告效果，再解释快照与消费 | 前置：一个报告目标和任务。首次引入 Session 对话组织、Surface 页面快照、InputRequest 原问题、InputSubmission 转交 |
| Session 专题 | 先比较五个参考仓库；先解释同对话两个目标 | 选后者：读者先知道为何需要两种生命周期，再看对象/正常关联/存储/后续分支。参考比较全文移至末节保留 | 前置：Task、输入、Content。首次引入线性消息、固定创建来源、回合只读投影 |
| Security README | 先介绍 PDP/PEP；从报告写入和独立读回 | 选后者：先回答谁准许何种内容去何处，再定义 Grant 和一次 use，沿原使用解释费用 | 前置：固定任务/文件操作。首次引入 Grant、use_id、历史决定与当前启动、结算独立记录 |
| Extensions README | 先列插件种类与 Go 接口；从报告依赖准确驱动和版本 | 选后者：实际装载内容和任务版本固定是读者要解决的问题，随后引入准备/批准/激活/ready | 前置：组件和原操作。首次引入 InstallLock、Activation、活动代际和当前实例；Plugin/Skill/Agent配置随用途解释 |
| Evaluation README | 先列指标/观测；从新报告 Skill 是否值得替换默认版本 | 选后者：一次任务成功与跨样本改善证据有不同问题。增加按方法/记录/成功点展开的正常链，再进入详细证据规则 | 前置：准确旧/新版配置。首次引入候选、冻结计划、样本/臂/Attempt、报告、反馈暴露、批准与发布 |
| 四份 implementation | 抽象列内部包；沿相应正常责任说明落库目的 | 保留全部原组件/算法/事务/图/验收，开篇改成输入保存、许可结算、活动后重启、原计划恢复的问题 | 前置为各模块README已解释的对象；内部facade/store/job再按实际处理步骤出现 |
| progressive-discovery | 直接列候选策略；从报告如何选读写能力和Skill材料 | 先说明准确绑定与选择的不同作用，再保留原三路对照与诊断、缓存和全成本规则 | 前置：Capability/Binding、InstallLock和Skill。候选策略按需，不新增目录权威 |

未为后续段落再次请求用户确认。没有引入需要更改 ADR 的新业务决定；Session 和 ApprovalLease 都补充现有持久责任的默认内部映射。

## 2. 来源覆盖与正式主定义

各源文件已完整阅读，字段表、接口表、状态图、事务、恢复和验收全部保留。迁移时相对链接已由根侧转换，库存按 [inventory-governance](inventory-governance.md) 逐组登记。

| 源 | 正式覆盖位置与变化 | 保留的重点 |
| --- | --- | --- |
| .draft/interaction/README.md 全篇 | [交互主线](../../docs/architecture/interaction/README.md)，改写开篇并先解释目录回答正常链 | 原§1–6、UI-01–11、跨来源目录完整性、当前披露、准确预览、消费与呈现成功分开 |
| .draft/interaction/implementation.md 全篇 | [交互实现](../../docs/architecture/interaction/implementation.md)，原§1–8保留；增加 [session-storage](../../docs/architecture/interaction/implementation.md#session-storage)、[session-storage-validation](../../docs/architecture/interaction/implementation.md#session-storage-validation) | 原II-01–31、请求实际owner、Confirmation实际consumer、投影周期责任、queued撤回/sending核对、SDK原命令恢复、旧世代隔离 |
| .draft/interaction/session-and-task.md 全篇 | [Session分工](../../docs/architecture/interaction/session-and-task.md#session-task-roles)、[正常关联](../../docs/architecture/interaction/session-and-task.md#session-task-flow)、[后续分支](../../docs/architecture/interaction/session-and-task.md#history-branches)、[研究依据](../../docs/architecture/interaction/session-and-task.md#session-research-basis) | 原研究表和复杂度/实施选择全文保留，移至本项目行为之后；旧自动锚点2-本项目应补齐的分工保留兼容入口 |
| .draft/security/README.md 全篇 | [授权主线](../../docs/architecture/security/README.md)，报告写入/读回开篇，正常授权图和全部§1–8保留 | Subject/Grant/Use/Settlement/Lease/Confirmation/Endpoint、strict/estimate范围、实际出口、once/撤权、隔离和公平性 |
| .draft/security/implementation.md 全篇 | [授权实现](../../docs/architecture/security/implementation.md)，开篇沿use/结算恢复；明确 [records](../../docs/architecture/security/implementation.md#records) 主锚点 | 全部账本、原确认、累计结算/outbox、离线单报告、配对秘密、S-I01–31和生产限额保留 |
| .draft/extensions/README.md 全篇 | [组件装配链](../../docs/architecture/extensions/README.md#component-lifecycle)、[Skill材料](../../docs/architecture/extensions/README.md#skill-materials) | 准确制品/配置、三类扩展、静态/动态按需、首装/兼容、独立旧批准回退、EX-01–09全部保留 |
| .draft/extensions/implementation.md 全篇 | [扩展实现](../../docs/architecture/extensions/implementation.md)，开篇沿准备→原激活→重启，原§1–11保留 | 字节先耐久、引用预登记/dispose互斥、staging、实例reopen、原迁移、残留、X-I01–16全部保留 |
| .draft/extensions/progressive-discovery.md 全篇 | [渐进发现](../../docs/architecture/extensions/progressive-discovery.md)，先解释报告选材场景 | 三路策略、实际加载诊断、当前缓存资格、原绑定、完整成本、X-03/X-09区别与HAR-10保留 |
| .draft/evaluation/README.md 全篇 | [评测主线](../../docs/architecture/evaluation/README.md)，增加 [normal-release](../../docs/architecture/evaluation/README.md#normal-release)，原§1–5保留 | 全部对象/接口、三个证据门槛、不可返还占用、所有物理尝试、源暴露/当前资格、批准/就绪/回退、EV-01–20保留 |
| .draft/evaluation/implementation.md 全篇 | [评测实现](../../docs/architecture/evaluation/implementation.md)，增加 [approval-lease-storage](../../docs/architecture/evaluation/implementation.md#approval-lease-storage) | 原§1–10、source_group_gates、formal_quarantine、SampleStartAdmission、immutable report、分页impact、逐目标rollout/rollback及V-I01–23全部保留 |

根侧 application-workflow、request-data-flows、core-data-model、walkthrough 仅阅读/交叉定位，未在本组写入。初次迁移时机器 contracts 未修改。ApprovalLease 的七个公开必填字段已对照 protocol.schema.json 原定义，补充字段只属于原命令/owner/宿主内部存储，不宣称公开新增字段；未发现需要报告为真实冲突的该对象差异。

## 3. 两处补齐的具体约束

Session 默认承载增加 app_sessions、app_messages、app_command_outbox、app_task_links 四种逻辑记录，可合并物理存储。明确认证 tenant/固定app_owner、message_id与(session_id,seq)唯一、原(tenant,logical_service_id,command_id)唯一、创建来源条件唯一、同库外键与跨域TaskRef。完整Command和目标首发前与消息/job共同保存；同受信事务可合并Task接纳，跨域则原回执后补关联，崩溃不建第二个Task。新增UI-S01–06覆盖首发前失败、接纳后关联丢失、并发原提交、同对话多目标、来源关闭未决动作和同/跨域提交。

ApprovalLease 明确放在批准owner的原事务库，可复用批准子记录或原命令决定存储，不强制新表。原command唯一绑定lease，字段包含原approval/revision/target/lock/instance/固定continue_until；当前批准、实际已激活、非零离线范围、原暴露和quarantine核验后与原回执共同提交。宿主以原Activation/InstanceReadiness保存租约引用和本次实例关系；重投不续期、新实例不能继承、时间回拨不延长。新增V-I24–26覆盖签发失回执、撤回/暴露竞争、重启/时间信任变化和清理后防重放。

保留原安全边界：Confirmation由真正消费的业务owner保存；InputRequest由业务owner创建/修订/消费；preview_refs不证明本人阅读；本地共同事务不伪造远端窗口；跨owner不能声称原子快照；归档/关窗不取消Task；停止/ready/效果/费用和物理清理分别确认。

## 4. 较新总览与研究采纳核对

[overview-before-shaping](overview-before-shaping.md)中“用户与其他Agent”的用户输入、准确成果验收、CLI/Web、断线查原输入、独立记忆管理、关窗不取消，均在Interaction主线和实现保留；内部/外部委派和子结果由核心组的Collaboration章主定义，本组未复制第二份委派规则。

总览“组件替换与版本演进”“能力发现与Skill材料加载”“发布、实际就绪与回退”的全部本组规则分别落在Extensions README/implementation/progressive-discovery与Evaluation README/implementation：准确依赖、三个发现路径及实际诊断、当前缓存资格、完整成本、兼容与改善、批次观察、独立旧版批准、格式兼容和残留责任均可直接阅读。较新总览按需能力的线性首版/自由输入未冻结、动态发现/热切换/改善评测按需、跨端预算未封账不返还和重新配对不继承也保留。

研究只作为选择依据：Harness O-03/07/08/12的内部次序与故障语料已在正文；O-09/11的经验候选与发现仍需独立对照。来源语义覆盖G-01–05不新增公共协议；AI报告方向不变成算法或收益结论；历史形式化结果不迁入当前运行保证。研究文件、固定源码、旧哈希和报告日期均未改写。

## 5. 本组静态核查

完成一轮只读结构和来源保留检查：正式10份Markdown、232个本地链接、27个Mermaid块，目标/锚点、表格列数、围栏、行尾空格均通过；正式正文不依赖.draft。按Markdown链接标签归一化后，原10份草稿的769行表格全部仍存在，没有丢失字段、接口或验收行。补充SessionTaskLink可选原提交引用和关联多重性后，最终检查覆盖正式10文件和库存/写作记录2文件，共287个本地链接，锚点、表格和围栏检查全部通过。

这些检查说明文档定位和表格保留，不证明图已渲染、业务语义完整或运行正确。独立读者将从正式正文冷读后反馈；真实DB提交、网络、UI呈现、平台隔离、模型质量和生产容量均待运行证据。未运行也未宣称新增运行测试通过。

## 6. 应用读者 APP-01／APP-03 修订

按[独立应用读者审校](reader-application.md)重新全文阅读 interaction README、implementation 和本记录后，补齐两项已支持功能的交接合同。本轮只编辑交互正文与本记录；公共 Schema、字节夹具、协议校验器与应用工作流由根及核心作者同步修订。APP-02 的直接 API 请求发现由核心章节和公共契约解决，不在交互模块复制另一套发现权威。

APP-01 的主定义在[回答正文](../../docs/architecture/interaction/implementation.md#input-answer-content)：answer_ref 固定 UTF-8 application/json 的 JCS 原字节，采用 input-answer/1 封闭对象；action_id 关联 allowed_actions，fields 按准确 schema.fields.name 映射。五种类型、Unicode 码点长度、安全整数、选项 ID、多选唯一且按声明顺序、optional 省略而不采用 null、空值与默认值、1 MiB 及更小政策上限都已明确。正文给出真实目录回答字节，并链接[正文 Schema](../../docs/architecture/contracts/schemas/input-answer.schema.json)与[实际字节用例](../../docs/architecture/contracts/examples/input-answers/README.md)。Renderer、InputService 和普通业务消费者分别验证原字节，不从 ContentRef 外形推断字段已经校验。

APP-03 的主定义在[验收交付](../../docs/architecture/interaction/implementation.md#acceptance-delivery)：受信宿主先保存完整原 task.accept_result 和预定批准修订的 confirmation_ref，完成 request/read/decide 后才创建验收交付封套。封套固定 action_id=accept、fields={decision:accept}，只增加 consumer_command；确认引用只存在于该命令 payload。InputService 核验当前准确请求、原确认和完整绑定并复用同一 command_id，不能排队时另造命令；实际 Orchestrator 独立复核并同事务消费请求、Confirmation、验收与后续核验责任。task.accept_result 未增加 answer_ref、preview_refs 或任意附加字段，队列交付和普通“同意”也未取得受信批准权。

内部持久映射在[接纳事务](../../docs/architecture/interaction/implementation.md#input-durable-boundaries)增加固定交互 owner 内认证 tenant／目标服务／consumer_command_id 到原 input_id 的唯一绑定，保留原回答和批准意图，不新增公共方法。它不声称跨交互库唯一，业务一次消费仍由原 Orchestrator 裁决。queued 撤回只停止原交付、不会把 approved 改成 denied；sending 后查原消费。选择排队交付的宿主不能并行直发消费者，本 owner 的另一个 input_id 不能复用同一 consumer 绕过原输入终态。批准、交互接纳和业务消费各自丢回执，都先查原记录，不因确认已 consumed 或当前修订已变而重做首次接纳。

新增 UI-12／13 和 II-32～37 覆盖五类字段与规范字节、非法字段和动作、验收完整队列、伪造封套、撤回与重复命令绑定、当前资格变化及共同提交失败。原交互、Session、呈现和恢复用例保留；新用例仍是待实现规格，运行服务、浏览器与终端尚无通过声明。

交互修订后再次全文复读，并只读检查本轮三文件：112 条本地链接、8 个 Mermaid 块、268 行表格；原交互两稿 201 行表格全部保留，围栏、表格列数、行尾和既有锚点通过。检查时新增正文 Schema 与字节用例仍由核心作者创建，5 处新链接待文件落地后统一复核；未据这些静态结果宣称真实消费或 UI 恢复通过。
