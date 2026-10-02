# 模块与数据 UML 图册导读

[方案入口](README.md) · [技术总览](technical-overview.md) · [HTML 图集](diagrams/architecture-atlas.html) · [原生可编辑图册](diagrams/uml-models.drawio) · [系统全景图](diagrams/system-panorama.drawio)

图册把当前九模块设计投影为静态逻辑组件图和领域类图：先识别谁提供能力、依赖谁，再看哪些对象保存事实、由谁裁决、如何关联。它补充系统全景图中的内部结构与数据关系；处理时序、完整状态规则和实现约束继续以所属模块正文为准。

全册共 20 页：前两页建立系统与跨 owner 主线，后续九组各含组件页、数据页。组件位置及容器只表达逻辑分工，不划定进程、owner 或数据库事务；这些边界可能重合，也可能分开。owner 指固定的逻辑裁决者，Orchestrator 是单个任务的 owner。

## 图例与阅读约定

组件以 `«component»` 标注，接口以 `«interface»` 标注；合并框保留原文中的职责名称。公共接口最多摘录三个已登记方法，空方法区的内部 port 表示实现适配边界。未画出的依赖、方法和记录仍须满足原契约。

领域类每个仅选三至五个关键属性。公共对象对应协议 Schema，内部记录来自实现文档；每组导读分别给出 Schema 链接与内部记录来源。它们不表示新增代码类或公共 Schema。`ID`、`Revision`、`Ref`、`Enum`、`Set` 等是排版简写，完整类型、必填性及数组上限以同版 [Schema](../../contracts/schemas/protocol.schema.json) 为准；字段省略不表示可选。属性名前的 `/` 表示派生值，`{readOnly}` 表示调用方不能独立修改。

| UML 关系 | 线型与端点 | 含义及本册用法 |
| --- | --- | --- |
| 关联 | 实线；本册不画导航箭头 | 对象间的领域关系；两端标多重性。使用。 |
| 依赖 | 虚线、开放箭头，指向被依赖者 | 组件使用另一个组件或接口；不表示时间顺序。使用。 |
| 实现 | 虚线、空心三角，指向接口 | 组件提供该接口的能力。使用。 |
| 泛化 | 实线、空心三角，指向一般分类 | 特化关系；当前资料未建立代码继承体系，本册不使用。 |
| 聚合 | 实线、整体端空心菱形 | 共享整体与部分关系；本册不使用。 |
| 组合 | 实线、整体端实心菱形 | 强整体与部分关系及生命周期约束；本册不使用。 |

后文关系表与图采用同一读法：`Task — OperationIntent` 的左端为 `1`、右端为 `0..*`，表示每份意图关联一个 Task，而每个 Task 可关联零至多份意图。`0..1` 必须连同条件阅读，可能表达尚未接纳、特定分支或当前查询投影，不能一概理解为可随意缺省。

普通关联不承诺数据库外键、共同事务、级联删除或代码持有方式。跨 owner 关联按原对象标识和修订核对，引用不转移写权。`ContentRef`、`SourceBinding`、`Proposal`、`SurfaceSnapshot` 等值对象可按相同值复用；图上共用一个节点不把它们变成独立权威记录，也不推导反向唯一性。

## 页面索引

| 页 | 视图 | 要回答的问题 | 导读 |
| --- | --- | --- | --- |
| 01 | 系统组件 | 九模块如何依赖与交接 | [系统](#system) |
| 02 | 跨 owner 核心对象 | Orchestrator 意图、执行效果与正式结果如何关联 | [核心对象](#core) |
| 03 | 任务编排器组件 | 准入、持久工作与事实归并如何分工 | [任务编排器](#orchestrator) |
| 04 | 任务编排器数据 | 意图、预留、工作与结果如何保存 | [任务编排器](#orchestrator) |
| 05 | 大脑组件 | 单轮决策如何读取输入、调用与恢复 | [大脑](#brain) |
| 06 | 大脑数据 | 请求、固定上下文、调用与提案如何绑定 | [大脑](#brain) |
| 07 | 执行组件 | 接纳、实际启动检查与驱动如何协作 | [执行](#execution) |
| 08 | 执行数据 | 版本绑定、尝试、控制与资源如何关联 | [执行](#execution) |
| 09 | 记忆组件 | 记忆、内容、索引与关闭职责如何分开 | [记忆](#memory) |
| 10 | 记忆数据 | 正文、来源、当前控制与副本如何关联 | [记忆](#memory) |
| 11 | 安全组件 | 许可、确认、端点与撤销如何裁决 | [权限与隔离](#security) |
| 12 | 安全数据 | 原使用、用量结算与离线额度如何绑定 | [权限与隔离](#security) |
| 13 | 交互组件 | 呈现、输入转交与业务消费如何分工 | [交互](#interaction) |
| 14 | 交互数据 | 页面、请求、提交与确认如何关联 | [交互](#interaction) |
| 15 | 协作组件 | 内部建子与外部创建如何恢复 | [协作](#collaboration) |
| 16 | 协作数据 | 委派、唯一子映射与额度如何关联 | [协作](#collaboration) |
| 17 | 评测组件 | 运行、报告、证据适用性与发布如何分工 | [评测](#evaluation) |
| 18 | 评测数据 | 不可变证据与当前批准如何关联 | [评测](#evaluation) |
| 19 | 扩展组件 | 准备、切换、入口检查与引用核对如何协作 | [扩展](#extensions) |
| 20 | 扩展数据 | 安装锁定清单、历史激活与当前实例如何关联 | [扩展](#extensions) |

<a id="system"></a>
## 01 · 系统组件

先沿交互、Orchestrator、Brain、Executor／协作阅读任务主线。Orchestrator 组装获准上下文，Brain 给出单轮提案，Orchestrator 独立准入；执行效果及子任务事实再归并回 Orchestrator。Memory／Content 提供准确版本及当前获准材料。

再看治理依赖：实际使用端向 Security 取得使用依据，资源入口仍检查当前状态；Evaluation 使用获准观测并管理评测与批准，Extensions 管理精确版本和实际实例。Evaluation 指向 Orchestrator 的依赖表示读取获准观测，图中不展开反向事实传输；Extensions 指向 Brain 的装配边是代表性依赖，不限定其只能装配 Brain。

依赖箭头省略返回与部分控制路径。Security 内也有不同事实 owner；Extensions 装配不授予业务权限。整体边界与完整交接依据见[技术总览](technical-overview.md#2-系统全景图的阅读方法)及[方案入口](README.md)。

<a id="core"></a>
## 02 · 跨 owner 核心对象

本页沿 Orchestrator 固定意图、Executor 保存效果、Orchestrator 固定成功成果阅读。`OperationIntent` 是 [Orchestrator 内部记录](orchestrator/README.md#records)；公共对象为 [Task](../../contracts/schemas/protocol.schema.json#/$defs/Task)、[Operation](../../contracts/schemas/protocol.schema.json#/$defs/Operation)、[Attempt](../../contracts/schemas/protocol.schema.json#/$defs/Attempt)、[Result](../../contracts/schemas/protocol.schema.json#/$defs/Result) 与 [ContentRef](../../contracts/schemas/protocol.schema.json#/$defs/ContentRef)。

| 关联（左 — 右） | 左端 | 右端 | 条件与边界 |
| --- | --- | --- | --- |
| Task — OperationIntent | 1 | 0..* | Orchestrator 准入并固定原意图。 |
| OperationIntent — Operation | 1 | 0..1 | 仅指正常 Invoke 已接纳的 Operation；取消先到留下的取消墓碑不算完整接纳记录。 |
| Operation — Attempt | 1 | 0..* | 接纳尚未准备时可为零；Attempt 不证明已发送，协议分页上限不是终身尝试数。 |
| Task — Result | 1 | 0..1 | 当前目标的正式 Result 仅在 `status=succeeded` 时存在。 |
| Result — ContentRef | 0..* | 1..100 | 成果引用可复用，不取得内容生命周期所有权。 |
| OperationIntent — ContentRef | 0..* | 1 | 固定 `input_ref`；实际读取另行获准。 |
| Operation — ContentRef | 0..* | 0..101 | 当前投影中最多 100 个 `evidence_refs` 与一个可选 `result_ref` 的引用集合，不含完整历史。 |

接纳、效果成立与任务完成有不同确认点；`0..1` 包含尚未派发或尚未接纳的阶段。任务取消后，未知或可能迟到的效果及费用仍沿原操作核对，迟到证据不重开任务。准确内容引用可以保留，但不证明字节仍存在或当前可读。

依据：[Orchestrator 记录与提交](orchestrator/implementation.md#data-flow)、[执行记录](execution/implementation.md#data-flow)、[共同契约](contracts/README.md)。

<a id="orchestrator"></a>
## 03–04 · 任务编排器

组件页先看 CommandHandler 与 TaskCoordinator／BudgetLedger 的同步裁决，再看 JobRunner、SnapshotAssembler／PlanMaterializer 和 FactReducer 的持续工作。默认参考实现共用 Orchestrator 的本地事务范围；Brain、远端 Executor 等调用在短事务之外。领取 job 只取得本轮处理租约，不证明任务成功。

公共对象：[Task](../../contracts/schemas/protocol.schema.json#/$defs/Task)、[Result](../../contracts/schemas/protocol.schema.json#/$defs/Result)。内部记录：Snapshot、OperationIntent、ReceivedFact、Job、BudgetReservation，见[记录结构](orchestrator/implementation.md#data-flow)；ReceivedFact 是 Orchestrator 的归并副本，其 `owner` 字段仍指原事实权威。

| 关联（左 — 右） | 左端 | 右端 | 条件与边界 |
| --- | --- | --- | --- |
| Task — Snapshot | 1 | 0..* | 每轮固定决策输入。 |
| Task — OperationIntent | 1 | 0..* | 保存已准入的原操作。 |
| Task — Job | 1 | 0..* | 同时覆盖推进及终态后的收尾责任。 |
| Task — Result | 1 | 0..1 | 仅成功有 Result；失败与取消另存结束说明。 |
| OperationIntent — BudgetReservation | 0..1 | 0..* | 操作计价项关联原意图；Brain 等其他计费来源可无操作意图，每笔预留仍唯一绑定原计费来源。 |
| OperationIntent — ReceivedFact | 0..1 | 0..* | 归并事实也可来自 decision 或委派，故可不关联操作意图。 |

准入消费原 Decision 身份或计划步骤身份之一，并与不可变意图、预留及派发 job 共同提交。本页保留主要对象；条件及准确成果关系见[主文精简类图](orchestrator/README.md#records)，计划版本、消费身份及历史记录见[持久对象关系](orchestrator/implementation.md#data-flow)，预留、固定分配及两端结算见[账务关系](orchestrator/implementation.md#accounting-relations)。各图沿用同一字段与规则，不声明额外数据库外键。`status`、`control`、`wait_reasons`、`open_effects`、`accounting_open` 独立，暂停和未知效果不是新的 Task 状态分类。

答复丢失查原 decision、command 或 operation；重启继续原 jobs。取消可保留效果与账务收尾，迟到成功不创建 Result。内容和历史清理须检查未决引用，长期去重与终态索引继续拒绝旧请求和已终结对象重新启动。

依据：[模块契约](orchestrator/README.md)、[内部职责](orchestrator/implementation.md#module-shape)、[数据与唯一约束](orchestrator/implementation.md#data-flow)。

<a id="brain"></a>
## 05–06 · 大脑

DecisionService 接纳固定请求，ContextReader 校验准确输入与来源，RecoveryWorker 沿原阶段调用策略、校验器和 ModelAdapter。DecisionStore 保存原决策及继续责任；模型、内容调用在事务外。Brain 无 Task 写权，ModelAdapter 不执行模型返回的工具调用。

公共对象：[DecisionRequest](../../contracts/schemas/protocol.schema.json#/$defs/DecisionRequest)、[BrainContext](../../contracts/schemas/protocol.schema.json#/$defs/BrainContext)、[DecisionRecord](../../contracts/schemas/protocol.schema.json#/$defs/DecisionRecord)、[ModelCall](../../contracts/schemas/protocol.schema.json#/$defs/ModelCall)、[Proposal](../../contracts/schemas/protocol.schema.json#/$defs/Proposal)。DecisionRecord 是协议响应视图，底层接纳、输入、输出与 jobs 见[持久记录](brain/implementation.md#data-flow)。

| 关联（左 — 右） | 左端 | 右端 | 条件与边界 |
| --- | --- | --- | --- |
| DecisionRequest — BrainContext | 0..* | 1 | `context_ref` 固定一个正文版本；同一准确上下文可被多次引用。 |
| DecisionRequest — DecisionRecord | 1 | 0..1 | 成功接纳后恰有一份原决策；拒绝请求可无记录。 |
| DecisionRecord — ModelCall | 1 | 0..1 | 确定性路径不建调用；更换 worker 不重新发送模型请求。 |
| DecisionRecord — Proposal | 0..* | 0..1 | 仅 `completed` 有提案；Proposal 是值，不宣称跨决策全局独占。 |

BrainContext 由 Orchestrator 固定，正文由内容 owner 保存；每次实际来源处理仍分别记事实。Proposal 的 `act`、`need_context`、`request_input`、`complete`、`fail` 是 `kind` 分支，图不建立继承关系。提案完成后仍须通过 Orchestrator 的当前准入。

供应商可能已处理而答复丢失时，查询原模型调用；不支持查询则保留 `provider_result_unknown` 和费用责任。取消关闭决策推进，模型停止仍须证据，`usage_final` 单独确认；迟到输出不能变成新一轮提案。

依据：[单轮决策契约](brain/README.md#brain-contracts)、[模型恢复](brain/README.md#model-recovery)、[参考实现](brain/implementation.md)。

<a id="execution"></a>
## 07–08 · 执行

先区分接纳与控制入口、执行工作者和实际发送前置检查。StartBarrier 在实际入口检查最新 TaskGate、资源状态及使用依据；Driver 按固定版本发请求或查询原目标。Executor 与资源 owner 各保存自身事实，共进程或共库也不把外部目标纳入本地原子提交。

公共对象：[Capability](../../contracts/schemas/protocol.schema.json#/$defs/Capability)、[Binding](../../contracts/schemas/protocol.schema.json#/$defs/Binding)、[Operation](../../contracts/schemas/protocol.schema.json#/$defs/Operation)、[Attempt](../../contracts/schemas/protocol.schema.json#/$defs/Attempt)、[TaskGate](../../contracts/schemas/protocol.schema.json#/$defs/TaskGate)、[RuntimeResourceState](../../contracts/schemas/protocol.schema.json#/$defs/RuntimeResourceState)、[ResourceLease](../../contracts/schemas/protocol.schema.json#/$defs/ResourceLease)。

| 关联（左 — 右） | 左端 | 右端 | 条件与边界 |
| --- | --- | --- | --- |
| Capability — Binding | 1 | 0..* | Binding 固定准确能力版本。 |
| Capability — Operation | 1 | 0..* | 沿原 `Invoke.capability_ref`；Operation 返回投影不因此新增字段。 |
| Binding — Operation | 1 | 0..* | 沿原 `Invoke.binding_ref`；目录更新不改写原意图。 |
| TaskGate — Operation | 1 | 0..* | 同 Executor 内以 `(orchestrator_id, task_id)` 为键的任务控制状态；每次启动复核，接纳快照不是永久许可。 |
| Operation — Attempt | 1 | 0..* | 接纳后可尚未准备尝试；实际请求分别记录，原业务键不变。 |
| Operation — RuntimeResourceState | 0..* | 0..* | 仅适用于能力声明的资源域；历史多项关联不允许并发占用。 |
| RuntimeResourceState — ResourceLease | 0..1 | 0..1 | 仅当前 `lease` 投影，不含历史租约；期限另按 `expires_at` 判断。 |

Attempt 有 `prepared_at` 不证明已发送，缺 `sent_at` 也不证明未跨发送边界。TaskGate 的有效控制合并祖先限制，与资源 owner 的 `control_epoch` 独立；两个维度都须有效。租约到期不证明旧动作已结束；独立不可变 Observation 及 GUI 前提见[执行契约](execution/README.md)。

取消先到时保存禁止迟到启动的取消墓碑。控制在发送边界前落实可拒绝启动；边界后只能关闭后续发送并保留在途集合，不能直接写成 `not_started`。答复丢失沿原目标关联核对，`execution_state`、`effect`、`may_apply_later` 与最终用量分别确认。

依据：[执行契约](execution/README.md)、[内部启动检查](execution/implementation.md#module-shape)、[持久关联](execution/implementation.md#data-flow)。

<a id="memory"></a>
## 09–10 · 记忆、内容与来源

Memory facade 管理记忆修订、查询、提取与视图，ContentStore 管理准确正文及内容控制。MetadataStore 封装各自元数据边界，同名不要求 Memory owner 与内容 owner 共库。先持久正文，再提交业务引用；跨 owner 不承诺原子提交。索引只提供候选，处理许可与结果披露许可分别成立。

公共对象：[MemoryRecord](../../contracts/schemas/protocol.schema.json#/$defs/MemoryRecord)、[MemoryControl](../../contracts/schemas/protocol.schema.json#/$defs/MemoryControl)、[ExtractionCandidate](../../contracts/schemas/protocol.schema.json#/$defs/ExtractionCandidate)、[ContentRef](../../contracts/schemas/protocol.schema.json#/$defs/ContentRef)、[SourceBinding](../../contracts/schemas/protocol.schema.json#/$defs/SourceBinding)、[ContentControl](../../contracts/schemas/protocol.schema.json#/$defs/ContentControl)、[ContentCopy](../../contracts/schemas/protocol.schema.json#/$defs/ContentCopy)。

| 关联（左 — 右） | 左端 | 右端 | 条件与边界 |
| --- | --- | --- | --- |
| MemoryRecord — ContentRef | 0..* | 1 | 正文可跨 owner；不证明字节仍存在或当前可读。 |
| MemoryRecord — SourceBinding | 0..* | 1..100 | 来源依赖为值；数组个数不证明反向唯一归属。 |
| SourceBinding — ContentRef | 0..* | 1 | `source_ref` 可跨 owner；来源依赖须无环。 |
| ContentControl — ContentRef | 0..1 | 1 | 控制以精确版本为键；完整控制记录不可取时仍可保留引用。 |
| ContentCopy — ContentRef | 0..* | 1 | `copy_id` 固定原内容、持有者与用途；副本不改变内容 owner。 |
| ExtractionCandidate — MemoryRecord | 0..1 | 0..1 | `saved` 固定唯一发布的记忆及初始修订；pending／rejected 不产生正式记忆。 |
| ExtractionCandidate — ContentRef | 0..* | 1 | 候选正文引用不授予保存权限；拒绝或取消候选不撤销已独立保存的记忆。 |
| MemoryControl — MemoryRecord | 0..1 | 0..1 | 按 owner、记忆 ID 与当前修订对应；deleted 只保留控制／墓碑，不构造 deleted 的 MemoryRecord。 |

写答复丢失查原命令；候选唯一发布约束防止两个确认生成两份正式记忆，发布后的纠正与删除仍由 Memory owner 裁决。分页每页复核当前来源与披露权限，旧索引命中或历史修订不能绕过撤权。

关闭成功确认 owner 停止新使用并保存清理责任，各 holder 的 `use_stopped` 和 `physical_state` 另查。unknown、residual 或尚未确认停用都不能汇总为 complete；旧备份先恢复内容禁用与删除记录，镜像不自动取得离线使用授权。

依据：[记忆与内容契约](memory/README.md)、[查询分页](memory/README.md#memory-pages)、[参考实现与清理](memory/implementation.md)。

<a id="security"></a>
## 11–12 · 权限与隔离

GrantLedger／LeaseLedger 裁决许可占用与结算，PairingController 管理端点，RevocationWorker 保存并履行撤销传播责任。ConfirmationStore 由实际 consumer owner 在自身数据库内装配并参加本地事务；通用代码不形成跨库确认中心。身份适配、资源规范化与实际资源入口各有边界，不能替代许可裁决。

公共对象：[ConfirmationRecord](../../contracts/schemas/protocol.schema.json#/$defs/ConfirmationRecord)、[GrantRecord](../../contracts/schemas/protocol.schema.json#/$defs/GrantRecord)、[UseReceipt](../../contracts/schemas/protocol.schema.json#/$defs/UseReceipt)、[UseSettlementRecord](../../contracts/schemas/protocol.schema.json#/$defs/UseSettlementRecord)、[OfflineLeaseRecord](../../contracts/schemas/protocol.schema.json#/$defs/OfflineLeaseRecord)、[EndpointRecord](../../contracts/schemas/protocol.schema.json#/$defs/EndpointRecord)。

| 关联（左 — 右） | 左端 | 右端 | 条件与边界 |
| --- | --- | --- | --- |
| GrantRecord — ConfirmationRecord | 0..1 | 1 | 仅 `consumer_method=grant.issue` 分支；确认与签发同 owner、同事务消费。 |
| UseReceipt — GrantRecord | 0..* | 1..100 | 引用全部必要许可修订；同 owner 全部占用或全部拒绝。 |
| UseSettlementRecord — UseReceipt | 0..1 | 1 | allowed 恰有一份结算，denied 无结算；不改写原回执。 |
| OfflineLeaseRecord — GrantRecord | 0..* | 1..100 | 从这些许可预留额度；到期不返还未知用量。 |
| OfflineLeaseRecord — EndpointRecord | 0..* | 1 | 固定签发时端点及实例的授权状态；新实例不接手旧租约。 |
| GrantRecord（子）— GrantRecord（父） | 0..* | 0..1 | 默认一条父链；每父可有多子，权限逐维收缩并检查当前撤销。 |
| OfflineLeaseRecord — ConfirmationRecord | 0..1 | 1 | 仅 `grant.lease.allocate` 分支；关联沿原 consumer command，不新增租约 `confirmation_ref` 字段。 |

先固定业务操作，再取得有限使用回执，使用端复核任务控制、资源占用与授权期限后启动。跨 owner 的部分占用保留原事实，全部依据未齐或窗口失效都不能行动；`grant.check` 的当前检查不代替固定 `grant.use`。答复丢失只查原 use，不延长原窗口。

撤销不改写历史 allowed 决定；获知撤销或到 `start_before` 后禁止新启动。计量 owner 先保存累计用量，Grant owner 按差额结算，取得最终用量已核清的记录后才释放 held；最终零用量也不恢复 once 一次性授权。重配对、重启与正文清理仍保留未结用量、一次消费及去重与终态索引，旧备份不能证明去重与终态记录完整时不开放消费。

OfflineLeaseRecord 的生命周期为 open、closed、reconciled；open 同时覆盖尚未使用及已经使用。已知使用从原账本读取，首次消费不再维护另一项生命周期迁移。授权检查、扣额与原使用登记仍共同提交。

依据：[授权契约](security/README.md)、[有限离线](security/README.md#offline)、[同事务确认与账本](security/implementation.md)。

<a id="interaction"></a>
## 13–14 · 应用与交互

SurfaceService 保存页面快照，InputService 与 DeliveryWorker 保存输入及固定目标命令并恢复投递。Renderer、TrustedConfirmationHost 属于 CLI／Web 宿主；请求和确认的消费归实际业务 owner。`interaction.request_read` 按请求 owner 路由，方法前缀不改变写权。

公共对象：[Surface](../../contracts/schemas/protocol.schema.json#/$defs/Surface)、[SurfaceSnapshot](../../contracts/schemas/protocol.schema.json#/$defs/SurfaceSnapshot)、[InputRequestView](../../contracts/schemas/protocol.schema.json#/$defs/InputRequestView)、[InputSubmission](../../contracts/schemas/protocol.schema.json#/$defs/InputSubmission)、[ApplicationEventSubmission](../../contracts/schemas/protocol.schema.json#/$defs/ApplicationEventSubmission)、[ConfirmationRecord](../../contracts/schemas/protocol.schema.json#/$defs/ConfirmationRecord)。Presentation 是[设备呈现内部记录](interaction/implementation.md)。

| 关联（左 — 右） | 左端 | 右端 | 条件与边界 |
| --- | --- | --- | --- |
| Surface — SurfaceSnapshot | 0..* | 1 | 当前完整快照值；不推导独立身份、反向唯一归属或级联生命周期。 |
| Surface — Presentation | 1 | 0..* | `(surface_id, endpoint_id)` 唯一；无记录时采用 closed 初始意图。 |
| SurfaceSnapshot — InputRequestView | 0..* | 0..100 | 准确请求修订可跨 owner，须覆盖所有 input／action 引用；同请求可在多页展示。 |
| Surface — InputSubmission | 1 | 0..* | 固定原界面对象标识；清理页面不删除未结目标映射或转交责任。 |
| InputSubmission — InputRequestView | 0..* | 1 | 多份回答可竞争同一请求；业务 owner 最多消费一个有效回答。 |
| Surface — ApplicationEventSubmission | 1 | 0..* | 仅 `task_ref` 缺省时接纳；固定页面修订与 `app_binding`。 |
| ConfirmationRecord — InputRequestView | 0..* | 0..1 | 仅 `task.accept_result` 恰关联一个 acceptance 请求，依据原命令 payload；其他消费者无此关联。 |

独立 Surface 不必创建任务；已绑定任务的页面由受信 Orchestrator 投影器更新。验收确认还绑定原 `goal_revision` 与 `candidate_hash`；多个未消费确认尝试不改变最终业务消费唯一性。`seen_revision` 只说明设备显示，不证明用户消费或外部效果。

SurfaceSnapshot 的 input 块只持有准确 request_ref；Renderer 经业务 owner 读取 InputRequestView.schema，页面不另存表单字段约束。请求修订过期或 owner 不可达时，相应输入不可用，不能使用旧页面中的另一份结构继续消费。

正常转交先保存 queued 和目标命令，再领取 sending 并查询原回执。答复丢失保留 sending；queued 撤回与领取竞争，sending 后只登记撤回请求并继续核对。必需预览撤权、过期或来源关闭时，业务 owner 拒绝相关消费；旧按钮与缓存不能放行。关窗不取消任务，清理历史快照不删除消费事实。

依据：[交互契约](interaction/README.md)、[呈现、转交与确认实现](interaction/implementation.md)、[确认消费](security/implementation.md)。

<a id="collaboration"></a>
## 15–16 · Agent 协作

DelegationAdmission 固定有界委派，InternalChildFactory 在父 Orchestrator 内建子，ExternalAgentAdapter 沿原外部创建键交接。DelegationReducer 归并原生事实，ControlPropagator／SettlementCoordinator 继续控制与收尾。图中依赖不表示时序；两个 required port 是内部实现边界。

公共对象：[Task](../../contracts/schemas/protocol.schema.json#/$defs/Task)、[Delegation](../../contracts/schemas/protocol.schema.json#/$defs/Delegation)、[AgentBinding](../../contracts/schemas/protocol.schema.json#/$defs/AgentBinding)、[RuntimeBudgetAllocation](../../contracts/schemas/protocol.schema.json#/$defs/RuntimeBudgetAllocation)。AgentDescriptor、InternalChildLink、ExternalTaskLink 为[内部记录](collaboration/implementation.md#data-flow)。

| 关联（左 — 右） | 左端 | 右端 | 条件与边界 |
| --- | --- | --- | --- |
| Task（父）— Delegation | 1 | 0..* | 委派固定父任务。 |
| Delegation — AgentBinding | 0..* | 1 | 精确绑定可被多个委派引用。 |
| AgentBinding — AgentDescriptor | 0..* | 1 | 固定准确 Agent 声明。 |
| Delegation — InternalChildLink | 1 | 0..1 | 与外部映射互斥；preparing 可尚无映射。 |
| Delegation — ExternalTaskLink | 1 | 0..1 | 仅已建立映射；创建未知时先保存原 creation key 与 job。 |
| InternalChildLink — Task（子） | 0..1 | 1 | 同 Orchestrator，映射两方向唯一；普通根 Task 可无内部映射。 |
| Delegation — RuntimeBudgetAllocation | 1 | 1 | 仅限已分配给协作委派的额度；一般预算分配不在此关系范围。 |

内部创建将子任务、唯一映射、额度和首 job 共同提交。外部 `remote_task_id` 归远端，图不把它当成本地 Task 或跨库外键。preparing 时两种映射都可缺省，但原创建键、额度及发送／查询责任仍须持久保存。

外部答复丢失、父取消或重启后，沿原创建键及原控制继续，不建第二个远端任务。无法证明未接纳或最终封账时保留预留。`/phase` 是同一已提交修订上按[判定顺序](collaboration/README.md)生成的只读摘要，不另存可写阶段；`phase=closed` 必须来自持久 Closure，要求目标封闭、效果核清及最终封账。父取消或子成功都不抹去这些责任，最小映射及已终结对象标识长期保留。

依据：[协作契约](collaboration/README.md)、[存储唯一约束](collaboration/implementation.md#2-存储与唯一约束)、[协作实现](collaboration/implementation.md)。

<a id="evaluation"></a>
## 17–18 · 观测、评测与改进

组件页沿冻结计划、隔离运行、证据封存、适用性检查与批准、逐目标发布阅读。各工作者共享评测 owner 的 repositories 与 jobs，分进程不产生新的写权威。隔离环境及独立判定器是内部 port；目标实际激活与就绪由 Extensions 裁决。

公共对象：[EvaluationPlan](../../contracts/schemas/protocol.schema.json#/$defs/EvaluationPlan)、[EvaluationRunRecord](../../contracts/schemas/protocol.schema.json#/$defs/EvaluationRunRecord)、[EvaluationReport](../../contracts/schemas/protocol.schema.json#/$defs/EvaluationReport)、[FeedbackExposure](../../contracts/schemas/protocol.schema.json#/$defs/FeedbackExposure)、[ReleaseApproval](../../contracts/schemas/protocol.schema.json#/$defs/ReleaseApproval)、[Activation](../../contracts/schemas/protocol.schema.json#/$defs/Activation)。PlanEligibility 是[正式评测资格的派生记录](evaluation/implementation.md#data-flow)。

| 关联（左 — 右） | 左端 | 右端 | 条件与边界 |
| --- | --- | --- | --- |
| EvaluationPlan — EvaluationRunRecord | 1 | 0..1 | 每冻结计划唯一整体运行；分批与有限 Attempt 不新建 run。 |
| EvaluationPlan — EvaluationReport | 1 | 0..* | 报告绑定准确计划；当前文档未规定每计划全局唯一报告。 |
| EvaluationPlan — PlanEligibility | 1 | 0..1 | `plan_id` 唯一；并非所有用途的计划都被规定必须有此内部记录。 |
| PlanEligibility — FeedbackExposure | 0..* | 0..* | 按来源组及发生时点判断失效；正常封存后反馈不使原报告失效。 |
| FeedbackExposure — EvaluationReport | 0..* | 0..1 | 暴露可先于报告；此边不是受影响报告的完整集合。 |
| EvaluationReport — ReleaseApproval | 1 | 0..* | 固定报告及摘要，批准另查当前证据适用性与发布条件。 |
| ReleaseApproval — Activation | 1 | 0..* | 跨 owner，以固定激活身份交接有限目标；批准不等于当前实例就绪。 |
| ReleaseApproval（新版） — ReleaseApproval（旧版） | 0..* | 0..1 | 非空 rollback_lock 必须绑定独立 rollback_approval_ref；当前只引用同 owner，旧版批准的有效性独立核验。 |

报告正文及摘要不可变，PlanEligibility 保存可异步收敛的正式评测资格的派生视图；正式使用还须同步核验原暴露事实，不能仅凭图中的 eligible 状态放行。候选谱系、保留占用、过程尝试、全部实际样本与发布记录未展开，各项评测与发布条件仍须满足。正常反馈先登记暴露再返回；迟发现的早期泄露保存唯一影响 job，随后分页投影失效与撤回，原报告继续供诊断。

环境准备失联沿固定到 run/sample/arm 的原创建键恢复。取消整体 run，逐一保存全部已创建及创建未知环境的 seal 责任，清理独立推进；不能新建环境掩盖旧责任。新版批准撤回不撤回独立旧版批准；回退以新的 activation 使用旧批准，其当前有效性与新版停用、残留分别核验。发布推进读取真实 active、当前实例 ready 及观察证据；重启重新取得 reopen 依据。

依据：[评测契约与验收条件](evaluation/README.md)、[运行与发布实现](evaluation/implementation.md)、[实际宿主就绪](extensions/implementation.md)。

<a id="extensions"></a>
## 19–20 · 扩展与宿主

LifecycleManager 组织准备、切换、停用和恢复，PackageVerifier／ArtifactReader 核验准确内容，ReferenceCollector 查询各持有者，BindingRouter 控制实际入口。management jobs 恢复原步骤；下载、装载、排空及远端核对在事务外。内容 port 是内部边界，不新增公开协议方法。

公共对象：[PackageManifest](../../contracts/schemas/protocol.schema.json#/$defs/PackageManifest)、[InstallLock](../../contracts/schemas/protocol.schema.json#/$defs/InstallLock)、[Activation](../../contracts/schemas/protocol.schema.json#/$defs/Activation)、[InstanceReadiness](../../contracts/schemas/protocol.schema.json#/$defs/InstanceReadiness)、[ReleaseApproval](../../contracts/schemas/protocol.schema.json#/$defs/ReleaseApproval)。ActiveBinding、LockReference 是[宿主内部记录](extensions/implementation.md#4-管理账本与索引)。

| 关联（左 — 右） | 左端 | 右端 | 条件与边界 |
| --- | --- | --- | --- |
| PackageManifest — InstallLock | 1 | 0..* | 固定清单、配置与依赖；同一包清单可生成面向不同平台或配置的安装锁定清单。 |
| InstallLock — Activation（new_lock_id） | 1 | 0..* | 激活固定新版安装锁定清单；已准备的清单可尚未激活。 |
| InstallLock — Activation（old_lock_id） | 0..1 | 0..* | 首次安装的 old_lock_id 为 null。 |
| ReleaseApproval — Activation | 1 | 0..* | 跨 owner；Evaluation 裁决当前有效性，历史引用不授予新的启动许可。 |
| InstallLock — ActiveBinding | 1 | 0..* | 每 target／port 仅一个当前 generation。 |
| Activation — InstanceReadiness | 1 | 0..1 | 仅当前查询投影；`phase=active` 必须有本实例 ready，不限制历史实例检查数量。 |
| InstallLock — LockReference | 1 | 0..* | `(lock_id, owner_kind, owner_id)` 唯一；引用不产生销毁级联。 |

正常切换完成排空、批准核验、装载及自检，并满足本实例真实入口条件后才为 active。Activation 保存原切换与历史启动依据，ActiveBinding 保存当前指针。指针提交后崩溃，按原安装锁定清单与代际恢复并取得新实例 reopen 依据，不再次迁移或推进 generation。Activation 的 revision 随持久可见投影变化递增，供列表、读取与通知合并；它与图中活动 generation 分开，不能用代际未变推导实例就绪状态未变。图册仍只列关键属性，完整字段见共享 Schema。

批准与启动共库时共同裁决；远端使用有限依据，离线租约仅在明确获准时适用。获知撤回，或当前检查不可用且没有仍有效的有限启动／离线续用依据时，关闭相应新使用；原责任核对继续；远端新实例不能沿用旧实例 ApprovalUse 或租约。历史激活与效果继续保留。

LockReference 是跨 owner 索引，释放事实仍由任务、原操作、迁移或回退等持有者给出。dispose 与新引用比较 `reference_revision`；任一持有者失联或引用未知时，不能按零引用清理。字节清理后保留原管理去重与终态索引。

依据：[扩展契约](extensions/README.md)、[管理账本](extensions/implementation.md#4-管理账本与索引)、[切换与重启](extensions/implementation.md#key-sequence)。

## 定义与验证边界

方法名称、输入输出与公共字段以[方法登记](../../contracts/schemas/methods.json)、[协议 Schema](../../contracts/schemas/protocol.schema.json)及[方法与协议编码格式](contracts/protocol.md)为准；模块 README 定义行为与保证，implementation 文档定义参考职责、内部记录和提交机制。跨模块取舍查阅[设计决策](decisions.md)，动态过程查阅[贯穿场景](walkthrough.md)。

图册是当前设计基线的静态投影，不新增类继承、ORM、外键或部署承诺。图文一致、链接和实际渲染检查只能验证文档资产；耐久提交、隔离、恢复、当前授权、状态检查与外部效果仍需运行证据。交付状态及运行验收范围分别见[交付审查](review.md)与[验收设计](validation/README.md)。
