# 开发范围与实现前检查

本文供开发者安排实现和评审。核心业务规则已有明确负责方、输入输出、状态和失败处理，仓库已建立领域处理器、存储、SDK 和运行装配的有界参考实现。实际开放范围与运行证据见[实施覆盖报告](implementation-coverage.md)；未完成能力和生产资格仍须逐项补齐，文档检查通过不表示服务可发布。

先实现一条用户目标闭环，再按需启用扩展。不得为一个可选功能先建设通用服务。遇到本文明确不支持的范围，必须返回unsupported，不得临时猜测另一种语义。

## 1 按这个顺序开发

| 步骤 | 开发者应产出的内容 | 开始下一步的依据 |
| --- | --- | --- |
| 1 身份与持久责任 | 原命令去重、回执、Job/Claim、可信时间、云端 PG 与端侧 SQLite 的各自事务适配 | 原提交未知和旧worker竞争测试通过；不得先接外部副作用 |
| 2 输入与目标 | Session/Submission、Task、GoalDocument、候选采纳、规则与覆盖 | 原文可追溯；空条件不能完成；steer接纳后旧目标不能继续准入 |
| 3 决策与执行 | 固定Snapshot/Decision、闭合Proposal、原Intent、授权使用、Attempt与效果归并 | 模型至多一次真实请求；回执丢失沿原身份核对；批量准入全收或全拒 |
| 4 核验与交付 | 当前条件判断、证据gate、权威 Result、云端 Task 完成事务与独立 Content 导出 | 必要效果不能用用户验收替代；导出失败不重开已完成目标 |
| 5 收尾与交互 | 费用差额、确认等待、取消、数据清理、当前授权呈现 | once不复活；未知费用不释放；UI能区分接纳、效果、完成与交付 |
| 6 可选扩展 | Schedule、环境、ChildHandle或远端资格，每次只开放通过合同验收的范围 | 下节接口与状态规则已编码，故障正反例和实际平台探针通过 |

字段和表的拆合是实现自由度；负责方、唯一键、原子提交集合、门禁和保留责任不得改变。开发者建议先写存储接口的合同测试，再写真实适配器。偏离建议时应在实现报告中说明影响。

## 2 已固定的关键业务选择

以下问题不再留给实现者自行决定。细节紧邻对应模块流程。

- 候选条件是增量upsert。未提及条件保留，先按规范语义键判重；替换绑定旧版本。第一版不隐式删除、拆分或合并条件。见[任务条件](../orchestrator/README.md#2-把目标变成条件)
- 一份 Decision 最多 4 个独立行动，在 Orchestrator 所属库中全收或全拒。外部执行不保证全成或全败。见[统一准入](../orchestrator/README.md#4-统一行动准入)
- 原命令等待确认时是accepted；本人决定是另一个命令；原业务重新核验后才一次消费。见[可信确认](../security/README.md#3-确认由真正的业务-owner-消费)
- 控制事实和启动窗口分别有身份。窗口到期不恢复once；不能证明未启动时继续核原效果。见[控制与恢复](../execution/README.md#4-取消未知与迟到)
- 原Result先在业务事务中固定，Content导出随后恢复。见[完成事务](../orchestrator/README.md#8-完成事务)
- Schedule固定时间边界、错过策略和recorded→sending切点。满槽只记录skip，不动旧槽。见[未来规则](../interaction/README.md#6-未来和周期规则)
- cell只提交完整成功的被动命名空间；已发生hostcall不随变量回滚。第一版不续跑已开始的cell。见[程序环境](../execution/README.md#7-程序化工具与可复用环境)
- ChildHandle换目标必须等待旧目标和效果关闭；费用可继续核对。见[子会话](../collaboration/README.md#6-可复用子会话)
- 远端资格检查与holder登记在完整gate锁域共同提交；成功Result的后续缺陷通知不因凭据到期丢失。见[证据资格](../evaluation/README.md#3-跨域当前资格的新增合同)
- 同一部署目标用稳定BindingHead裁决新激活、重启与停用。封存报告若被迟到事实推翻，发布资格单独失效。见[实例就绪](../extensions/README.md#2-从准备到当前实例就绪)与[实验封存](../evaluation/README.md#6-统计口径与优化门禁)

## 3 第一版明确的边界

“可选”表示部署可以不开启；开启后必须遵守对应合同，不能用部分实现冒充完整能力。

- Schedule可选。首版只有once、固定interval及daily/weekly/monthly，不支持任意RRULE、补跑或静默更换tzdb
- Environment与ChildHandle可选。首版不保存程序运行栈，不重放已开始cell，不迁移ChildHandle的owner
- 组合检查可选。一个组合DAG必须由同一治理authority负责；多authority组合暂不支持。Task的不同必要检查可以分别核验各自authority
- 发布与正式评测可选。首版要求报告资格gate、ReleaseApproval和ApprovalUse签发同治理owner/事务；跨owner发布证据资格暂不开放
- 有限Plan、typed_decision和向量检索是优化扩展，首个基本闭环不依赖它们。公开扩展前必须单独冻结闭合Schema、实际支持范围和测试

这些限制允许先实现正确的分布式主线。它们不是必须新增全局服务的理由，也不将生产目标退回单进程。

<a id="coverage"></a>
## 4 实施覆盖与开放条件

本表是完整目标的开发索引。业务语义由所链章节定义；机器合同以同版 Schema 和方法登记为准。“待交付内容”列说明各切片的完整要求，已实现与仍缺失的部分由[实施覆盖报告](implementation-coverage.md)区分，不表示该切片尚无代码。新能力仍须满足整条合同后才能开放。

| 实施切片与负责模块 | 已定义的语义与数据入口 | 当前机器合同覆盖 | 待交付内容与开放依据 |
| --- | --- | --- | --- |
| 接纳与恢复：Runtime、Orchestrator | [命令与 Job](../runtime/README.md)、[同库事务与锁序](../data/storage.md#3-同一数据库事务具体包含哪些记录) | Command、Job、Claim 等记录已有 Schema；回执、查询和完整方法登记待补 | 去重与领取处理器、PG/SQLite 各自适配及恢复 SDK；取得 F01–F04 的重复、提交未知和旧 worker 竞争证据 |
| 输入与目标：Interaction、Orchestrator | [输入与任务](../interaction/README.md)、[条件形成](../data/requirement-lifecycle.md) | Session、Message、Submission、InputRequest、GoalRevision、候选与覆盖已有记录 Schema；task.submit/cancel 有请求载荷 | 应用输入、目标采纳和控制方法的完整请求/响应/错误及客户端；F05、F16、F25 证明原文可追溯、旧输入不误消费、空条件不能完成 |
| 决策与行动：Brain、Execution、Security | [固定决策](../brain/README.md)、[真实执行入口](../execution/README.md)、[授权消费](../security/README.md) | DecisionRecord、Operation、Grant 等记录及 execution.invoke/cancel 载荷已定义；brain、resource、grant 方法族未完整形式化 | 模型与工具适配、真实出站计量、许可门禁；F03、F08、F14 及供应商探针验证单次请求、未知效果和资源隔离 |
| 核验与交付：Orchestrator、Evaluation、Memory | [完成事务](../orchestrator/README.md#8-完成事务)、[证据资格](../evaluation/README.md)、[内容发布](../memory/README.md#2-发布准确内容) | ConditionResult、Result、ContentRef 等记录已有 Schema；核验、证据导入与内容发布方法待补 | 独立验证器、完整关系索引、当前资格与结果导出恢复；F05–F07、F11 证明完成门禁及导出失败后继续责任 |
| 收尾与使用：Accounting、Security、Memory、Interaction | [账务](../accounting/README.md)、[清理与使用](../memory/README.md)、[准确输入](../interaction/README.md) | UsageSnapshot、BudgetBalance、Confirmation、AllocationClosure 及 task.billing_reconcile 载荷已定义；各方法的回执/查询与错误待补 | 账单差额、一次确认、清理和界面恢复；F08–F13、F16、F18 证明不返还已消费授权、未知费用持续占用、撤权后不披露 |
| 可选能力：Schedule、Environment、Collaboration、Extensions、Evaluation | [模块内部字段](../data/module-records.md)、本文第 3 节及对应模块方法表 | ScheduleSpec 只定义时间规则；环境、子会话、发布、正式评测等完整机器合同待冻结 | 每次选定一项开放范围，补齐方法/状态、Schema、SDK 和平台前提；执行对应 F07、F10、F15、F17、F19–F20 后才声明支持 |
| 传输与生产：Protocol、Production | [端云和 gRPC](../protocol/README.md)、[部署与恢复](../production/README.md) | harness.proto 提供外壳；领域响应、发现、传输帧及方法登记尚未形成完整发布包 | WSS/gRPC、发现/认证、背压、迁移和观测适配；F21–F24 取得通道恢复、跨区、容量和灾备证据 |

F 编号的完整刺激和判断依据统一见[故障矩阵](../validation/README.md#2-关键故障矩阵)。表内列出开发关注点；能力还须满足其依赖的授权、来源、费用和故障要求，不能只通过表内几个编号就宣布全部支持。

### 线协议怎样补齐

core.schema.json只形式化了部分记录与五条命令。模块方法表已经规定其业务字段、回执和错误；正式开放前，开发者必须将选定方法转为同版闭合Schema、方法登记、Go/TS类型和SDK恢复代码。不得因为文档列有方法名就宣告支持完整profile。

五条命令及其准确范围见[协议覆盖](../protocol/README.md#schema-coverage)。每个拟开放方法都须登记：负责方、所属 profile、请求/响应/错误 Schema、回执阶段、原命令查询方式、版本与权限前提、SDK 恢复行为和正反例。字段或语义出现缺口时先在所属模块补合同，再更新登记；新增业务取舍需进入明确的设计评审。

Schema只能检查结构。来源认证、权限、事务、作用域、真实效果和当前证据资格必须由处理器及存储实现验证。状态模型与设计样例不能替代这些工作。

### 部署和外部服务

以下条件取决于实际账户、平台或供应商。相关能力启用前必须取得证据，缺失时应关闭该能力并报告具体原因。

- 模型/工具的原请求查询能力、幂等保留期、真实计费上界及是否存在隐式重试
- 文件系统刷盘/原子替换、GUI观察与动作边界、WASI/进程隔离、实际退出与旧实例隔离
- 公司身份/密钥、可信时钟、三可用区同步提交与旧主隔离、对象介质耐久和恢复
- 外部Agent的原创建键、控制、完整效果关闭和费用更正能力
- 自然语言提炼保真、目标覆盖、判断校准和端到端质量；模型自评不能代替独立真值

TaskPolicy的预算、期限、风险类别、具体业务阈值及开放能力必须由产品/运营以准确配置确认。文中的初值是测试输入，不是未经实测即可承诺的容量或质量保证。

## 5 怎样完成本轮实现评审

开发者应选择一条具体请求，逐步指出每个事务的输入、原身份、写入记录、失败返回和下一项Job。再执行重复、乱序、超时、取消、恢复及跨租户反例。无法指出原记录或唯一负责方时，必须暂停该能力开放并补合同。

设计阶段取得了独立设计复核与设计资产检查。实现阶段另有真实 PG/SQLite、文件、模拟设备、测试 HTTP 出口和浏览器证据，准确版本及边界见[实施覆盖报告](implementation-coverage.md)。真实目标读者使用、供应商质量与对账、跨区故障和生产容量仍须独立验收。本文借鉴简明语言原则组织，不构成ISO标准符合性声明。
