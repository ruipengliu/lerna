# Lerna 领域上下文

Lerna 是面向智能应用开发者的 Agent 运行框架：在用户授权、预算和期限内持续推进目标，并以实际效果和证据判断是否完成。Task、效果、权限和完成依据采用稳定语义，决策、上下文、记忆检索与协作策略允许独立替换。

本仓库采用单一上下文，本文件是领域语言与关键约束的入口。依据为 2026-10-03 的 [架构设计](docs/architecture/README.md)；当前已建立 Go／TypeScript 公共值合同、严格编解码和共同验证入口。业务内核、完整 SDK、持久恢复及生产指标仍待后续切片实现验证；准确进度与证据见[实现进度](.scratch/lerna-implementation/progress.md)。下文描述领域设计要求，不表示全部能力已实现。

## 目标与范围

首批服务内部个人智能应用团队，同时提供可独立运行的开源实现。首条完整场景是研究报告：接收问题与约束，取得真实来源，形成有引用的报告，保存到获准位置并读回核验；过程中支持补充要求、暂停、恢复和取消。

首阶段以可靠任务推进、应用接入和分布式组件替换为主。长期记忆、端侧自主目标、Agent 委派、GUI、未来触发和受控经验改进按场景开放；未开放的能力显式返回不支持。第一版不建设通用 DAG 平台、全局事件总线、自动跨分片任务迁移、任意程序栈恢复或无限自治循环。范围限制不免除已开放能力的恢复、授权和效果核验责任。

## 领域语言

以下名称用于文档、接口、问题描述与测试。`_Avoid_` 表示不得把这些名称用作同义词，并不禁止在各自含义下使用它们。字段与完整枚举见 [数据与存储](docs/architecture/data-model.md)。

### 目标、判断与结果

**Task（任务）**：对一个目标持续推进并形成正式结果的责任对象，可以跨越多轮交互和客户端连接。
_Avoid_：Session、对话回合、Job、一次模型调用。

**Session（会话）**：组织用户输入、对话分支和呈现的交互对象，可以关联多个 Task。
_Avoid_：Task、任务执行状态。

**Requirement（条件）**：从原始要求提炼出的可检查条件，涵盖成果内容、外部效果和过程约束，并标明是否必要。
_Avoid_：模型自行设定的成功标准、综合质量分。

**Snapshot（上下文快照）**：一次决策实际采用的不可变输入集合，包含准确目标与控制修订、材料来源、进展、预算和组件版本。
_Avoid_：可变提示字符串、完整会话历史、供应商提示缓存。

**Decision（决策记录）**：在固定 Snapshot、组件和限额下进行的一次决策记录；它不是决策模块本身。
_Avoid_：Decision Engine、已准入行动。

**Proposal（提案）**：决策引擎提交的结构化推进建议，可提出条件修订、行动、输入请求、候选结果或无法继续的原因。
_Avoid_：执行许可、已发生效果、正式 Result。

**ConditionResult（条件判断）**：对准确条件、目标修订和成果作出的 `pass`、`fail` 或 `unknown` 判断，附规则、证据与适用限制。
_Avoid_：Task 终态、Evaluator 调用成功。

**Result（正式结果）**：Task 进入终态时固定的成果、逐条件判断、终结原因或限制的记录；失败和取消也有 Result。
_Avoid_：模型最终文本、临时流片段、远端 completed 回执。

### 负责方、执行与恢复

**owner（逻辑负责方）**：保存一类事实并裁决其变更的逻辑服务身份。
_Avoid_：worker、进程、机器地址。

**worker（工作者）**：暂时领取并处理已保存工作责任的进程，可以更换而不改变业务身份。
_Avoid_：owner、Agent。

**Command（命令）**：受信主体向固定 owner 请求一次业务裁决的原始请求身份。
_Avoid_：网络帧、传输重试次数、Operation。

**Job（持久工作）**：已经提交、仍需处理的后续工作责任。
_Avoid_：Task、一次 Claim、内存队列项。

**Claim（领取）**：worker 在有限租约和代次下取得的临时处理资格。
_Avoid_：新的业务责任、外部效果已隔离的证明。

**OperationIntent（行动意图）**：Orchestrator 准入并固定业务含义的行动记录，包括准确参数、来源、授权使用、预算预留与能力绑定。
_Avoid_：Proposal、外部执行成功。

**Operation（操作）**：Executor 接纳和核对一项行动的逻辑身份，贯穿物理尝试、效果观察与关闭。
_Avoid_：一次 Attempt、Job、HTTP 请求。

**Attempt（物理尝试）**：原 Operation 下的一次真实执行尝试；新增尝试需要安全重试依据。
_Avoid_：新的 Operation、自动重试许可。

**Effect（效果）**：对原 Operation 实际结果的领域判断，取 `applied`、`not_applied` 或 `unknown`；是否仍可能迟到发生另由 `may_apply_later` 表示。
_Avoid_：传输成功、操作阶段、任务成功。

**Responsibility（未结责任登记）**：Task 侧追踪操作或委派及其关闭依据的记录，覆盖已经准入但远端尚未接纳的工作。
_Avoid_：远端操作列表、异步统计数量。

### 内容、权限与扩展

**Content / ContentRef（内容与内容引用）**：Content 保存准确版本的字节及来源；ContentRef 标识该版本、摘要和负责方，不自带读取权限。
_Avoid_：Memory、任意 URL、最新同名文件。

**Memory（记忆）**：获准跨任务复用的事实、偏好、推断或经验，保留来源、时间和适用范围。
_Avoid_：任务历史、原始工具输出、向量索引。

**Grant / GrantUse（许可与许可使用）**：Grant 是受信主体授予的结构化资源与用途许可；GrantUse 是绑定准确行动、额度和有效期的使用依据。
_Avoid_：用户目标、模型建议、Capability 声明。

**Confirmation（确认）**：绑定原命令、准确参数、请求版本、本人身份与有效期的受信决定。
_Avoid_：普通聊天中的肯定回复、授权范围自动扩大。

**Reservation（预算预留）**：从可用预算中为原行动保留的有限额度；它与实际消费及最终结算分别记录。
_Avoid_：费用估算、已结算费用、子任务新增的根预算。

**Capability / Binding（能力与绑定）**：Capability 声明操作含义及其保证边界；Binding 将能力连接到准确实现和资源。
_Avoid_：资源访问许可、仅凭工具名称选择实现。

**InstallLock / Activation（安装锁定与激活）**：InstallLock 固定制品、依赖、配置和合同版本；Activation 是可供任务固定引用的准确激活版本，默认选择由 BindingHead 指向。
_Avoid_：最新同名插件、安装即就绪、发布批准即运行资格。

**Skill / Plugin / Agent**：Skill 是操作知识；Plugin 是携带代码或适配器的安装包；Agent 是持有目标、上下文和决策循环的工作主体。
_Avoid_：用一个“插件”概念代替三者、把远程模块等同于 Agent。

**Delegation（委派）**：绑定父目标修订、独立子目标、条件、授权范围、预算和期限的子工作责任。
_Avoid_：任意远程调用、可直接决定父 Task 成功的远端任务。

## 职责与边界

四层依次为应用层、Agent 领域层、可靠运行层和平台适配层。业务责任、代码依赖和部署进程分别划分；一个模块不必是一个进程，同机部署也不自动共享事务。完整分工见 [架构与模块](docs/architecture/architecture.md)。

| 负责模块 | 权威事实或职责 | 边界 |
| --- | --- | --- |
| Interaction | Session、原始输入、投递和受信呈现 | 界面关闭、归档或断线不能裁决 Task |
| Orchestrator | Task、目标与条件、行动准入、正式 Result | 唯一任务控制与完成裁决者；不伪造外部效果 |
| ContextCompiler | 在 Orchestrator 管理下形成 Snapshot | 不建立第二套目标权威；不得静默裁掉必要约束 |
| 决策引擎（Decision Engine，`decision_engine`） | Decision、Proposal、模型请求与用量 | 只提建议；不直接执行行动或写 Task 终态 |
| Executor | Operation 接纳、Attempt、Effect、资源占用 | 保存实际执行事实；不改变用户目标 |
| Evaluator | 按准确规则作出 ConditionResult | 判断条件，不裁决整个 Task |
| Memory / Content | 可复用语义 / 准确版本字节与来源 | 长期保存、当前读取和派生使用分别受控 |
| 授权与预算模块 | Grant、有限使用、预留与结算 | 在准入与真实入口落实，不交给提示词执行 |
| 扩展与评测模块 | 安装、批准、就绪、实验与发布 | 评测通过、发布批准和实例就绪分别成立 |
| 可靠运行层（`runtime`） | 命令接纳、事务、Job、领取与恢复 | 不判断任务成功，不自行决定重发外部动作 |

“任务运行内核”包括任务控制、必要共同领域规则和可靠运行机制；代码中的 `runtime` 仅指可靠运行层。领域声明接口，平台适配器实现接口，宿主注入；Orchestrator 不导入默认能力实现的内部存储。

## 必须保留的规则

1. **身份与权威稳定。** 原 Command、Task owner 和 Operation 不因断线、超时或更换 worker 而改变。相同命令只能查询或重传原内容；修改内容不得复用原键。接纳回执固定，异步执行结果另记。见 [ADR-0003](docs/adr/0003-stable-owner-distributed-deployment.md)、[ADR-0004](docs/adr/0004-transactional-durable-work.md)。
2. **目标修订使旧建议失效。** `goal_revision` 表示目标或条件变化，`control_revision` 表示准入控制变化，`revision` 用于 Task 并发修改。Orchestrator 只消费匹配当前修订的提案；已经发生的效果和费用仍保留。见 [任务推进](docs/architecture/task-lifecycle.md#一轮推进的算法)。
3. **成功需要逐项证据。** 条件必须覆盖原要求；全部必要条件有效通过，相关效果确定且不会迟到改变成果，子目标与委托关闭后，Orchestrator 才能提交成功 Result。模型文本、远端 completed、空条件集合或平均分均不能替代该判断。见 [ADR-0002](docs/adr/0002-stable-kernel-replaceable-strategies.md)。
4. **取消与收尾分开。** 取消封闭新目标准入并保留传播、核对和清理责任；`effects_closed`、`spending_closed`、`cleanup_complete` 分别更新。Task 终态不复活，补充工作建立关联的新 Task。见 [ADR-0001](docs/adr/0001-task-session-separation.md)、[ADR-0005](docs/adr/0005-explicit-effect-uncertainty.md)。
5. **权限与预算由实际入口执行。** 准入和真实启动都检查准确行动、当前许可及限额；Skill、Plugin、记忆和子 Agent 不扩权。费用未知不按零结算，子额度不突破根预算，离线窗口不因重启延长。见 [ADR-0006](docs/adr/0006-authorization-budget-at-action-boundaries.md)。
6. **恢复保留来源与版本。** 每次决策固定材料和组件版本；新绑定不能静默改写在途工作的含义。长期保存、处理和披露分别授权，派生材料受全部实际处理来源约束。见 [ADR-0007](docs/adr/0007-versioned-content-memory-snapshots.md)、[ADR-0009](docs/adr/0009-versioned-activation-and-recovery.md)。

## 技术基线与验证边界

当前设计选择 Go 内核与默认宿主，初期同仓一个 module，Go 与 TypeScript SDK 同版验证。云端使用 PostgreSQL，设备执行账本使用 SQLite，大内容进入对象存储；业务事实与后续 Job 在声明的本地事务中共同提交。端云使用 WSS，内部使用 gRPC，内容字节使用 HTTPS，MCP 与 A2A 经适配器接入。详见 [部署基线](docs/architecture/deployment.md#技术基线)和 [ADR-0008](docs/adr/0008-internal-contracts-external-protocol-adapters.md)。

生产首版按分布式装配；开发可单进程或本机多进程。默认云端保存 Task 权威，设备保存本机执行和补传责任；设备自主目标需要独立 Orchestrator，不能失联后接管云端原 Task。

框架不保证任意第三方动作恰好执行一次，不承诺撤销已发生动作，也不能在缺少来源或独立真值时保证自然语言答案正确。容量、质量与灾备数字都是待验目标，不能引用为当前实现能力；验证门槛见 [实施与验证](docs/architecture/validation.md)，已取得证据的范围见 [文档检查结果](docs/architecture/verification.md)。

## 按工作查阅

| 要处理的问题 | 入口 |
| --- | --- |
| 理解关键选择及其理由，评估是否需要改变 | [ADR 索引](docs/adr/README.md)、[原设计取舍](docs/architecture/decisions.md) |
| 实现目标、控制、完成和失败处理 | [任务生命周期](docs/architecture/task-lifecycle.md) |
| 实现持久工作、事务和效果恢复 | [可靠运行](docs/architecture/runtime.md)、[数据与存储](docs/architecture/data-model.md) |
| 接入应用或替换组件 | [接口与协议](docs/architecture/contracts.md)、[能力模块](docs/architecture/capabilities.md) |
| 处理权限、来源、预算和版本生命周期 | [授权与扩展](docs/architecture/governance.md) |
| 规划实施、故障实验和优化验收 | [实施与验证](docs/architecture/validation.md)、[ADR-0010](docs/adr/0010-evidence-gated-improvement.md) |

修改领域规则时同步本文件、相关详细设计与 ADR。与现有 ADR 冲突的方案必须明确指出冲突及理由；改变决定时新增记录并关联被替代记录，保留原取舍依据。用语强度沿用架构文档的“必须／不得”“建议／不建议”“可选”。
