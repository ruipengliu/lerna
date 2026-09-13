# Harness 技术实现方案

状态：技术实现方案已定稿，架构、接口、协议、端云恢复、记忆、执行、授权、扩展、评测自进化及能力里程碑均已完成决策，可据此拆分实施任务。当前没有业务实现或运行验收结果。

本组文档的目的地是将 [项目目标基线](../harness-project-goals.md) 转换为可据此拆分实现任务的技术方案。范围覆盖 C1–C9、架构约束 A1–A4 以及联网问答、手机 GUI 两项强制专项能力。

已确认采用 Go 开发核心，开发期单机完整运行，使用 SQLite 等无需独立外部服务的组件。模块依赖接口，参考实现可按业务或生产部署需要替换；以多个模拟设备验证系统能力，不要求真机验证。

## 当前可读文档

- [设计输入与硬约束](00-design-constraints.md)：已确认要求、必须保持的运行语义，以及 Go、单机开发和模拟验证的参考条件。
- [系统结构与 Interface 提纲](01-system-architecture.md)：三系统与内核的协作职责、需要设计的 Interface 和关键数据流。
- [Go 内核与本地持久化](02-runtime-and-state.md)：已确认的模块职责、任务级原子提交接口、调度替换范围及验证要求。
- [内部契约与外部协议映射](03-contracts-and-protocols.md)：已确认的 gRPC、WebSocket、编码分工、交付语义及外部适配范围。
- [原生接口与消息目录](03-contract-message-catalog.md)：服务方法、消息关联、信封、错误、协商、消息示例和一致性矩阵。
- [端云任务所有权与恢复](04-edge-cloud-and-recovery.md)：已确认的权威分布、离线边界、协作移交、状态模型和局部恢复算法。
- [恢复状态与故障检查表](04-recovery-state-and-failure-matrix.md)：状态转换、移交故障窗口、原子边界、窗口清理及故障注入要求。
- [上下文、记忆与同步](05-memory-and-context.md)：已确认的记录模型、上下文组织、集合修改权威及生命周期恢复。
- [记忆数据契约与生命周期](05-memory-data-and-lifecycle.md)：数据字段、Interface、存储与验证设计，以及已确认的删除传播、上下文失效和同步恢复策略。
- [端云记忆驻留与联合检索](05-edge-cloud-memory.md)：已确认的端云数据分类、原文留端的检索方式、端侧挖掘和派生内容策略。
- [大脑循环与多 Agent 协作](06-brain-and-agent-collaboration.md)：已确认的有限决策步、模型替换、任务委派和完成判断。
- [决策准入、交互与协作恢复](06-decision-admission-and-collaboration.md)：已确认的提案准入、持续交互、子任务收尾、预算规则和验证矩阵。
- [工具执行与多模拟设备](07-tools-and-simulated-devices.md)：已确认 API 优先、GUI 兜底，以及超过 1000 个 API、首次正确率 90% 和有限重试任务成功率 95% 的目标。
- [执行接口、资源控制与故障验证](07-execution-contracts-and-validation.md)：已确认的目录检索、Driver、执行持久化、资源接管及参考验证方案。
- [身份、授权与用户控制](08-authorization-and-user-control.md)：已确认的本地身份管理、统一授权、执行检查位置和扩展信任范围。
- [授权生命周期、凭证与控制接口](08-authorization-lifecycle-and-interfaces.md)：已确认的 mTLS/浏览器认证、签名授权、离线有效期、凭证存储和控制接口。
- [插件、Skill 与 Agent 扩展生态](09-extension-ecosystem.md)：已确认的扩展登记、Skill 加载、运行方式和升级停用生命周期。
- [扩展清单、运行器与激活恢复](09-extension-contracts-and-activation.md)：已确认的清单锁定、管理接口、激活恢复及运行器/SDK 与格式基线。
- [运行观测、能力评测与受控自进化](10-observation-evaluation-and-evolution.md)：已确认的记录、评测、候选发布与端云隐私职责。
- [评测接口、参考基准与发布判定](10-evaluation-contracts-and-release-gates.md)：已确认的接口恢复、样本与预算、统计比较、分阶段发布及保留策略。
- [能力里程碑与实施依赖](11-milestones-and-validation.md)：已确认的四阶段、能力成熟度、完整发布边界与实施依赖。
- [专项验收、持久化替换与实施交接](11-specialized-validation-and-handoff.md)：已确认的专项样本/预算、bbolt 后端、双向迁移与实施工作包。
- [端侧 UI 交互与协议扩展](03-ui-interaction-contracts.md)：多种 UI 形态、自定义类型、用户事件、状态恢复及模拟端验收。
- [标识模型简化评估](03-identifier-model.md)：已确认的身份合并、按需关联字段、对象关系及恢复语义反例检查。
- [身份、凭据与离线时间研究](../research/harness-identity-credentials-and-offline-time.md)：官方机制证据及限制，具体方案仍需通过设计决策和实现验证。
- [扩展运行器与隔离研究](../research/harness-extension-runtime-boundaries.md)：Go 编译扩展、子进程及嵌入式 Wasm 的官方机制与边界。
- [外部 Go SDK 与 Skill 快照研究](../research/harness-extension-sdk-and-skill-snapshots.md)：SDK 固定版本、规范摘要及需补齐的协议适配。
- [能力评测统计与版本比较研究](../research/harness-evaluation-statistics-and-comparison.md)：比例、分组采样、配对比较和保留集复用的适用条件。
- [第二持久化后端研究](../research/harness-second-persistent-backend.md)：bbolt 的事务、快照、并发与迁移耐久性限制。
- [协议与 Skill 规范研究](../research/harness-protocol-contracts.md)：现行标准、适配边界和一致性测试关注点。
- [持久运行机制与存储研究](../research/harness-durable-runtime-options.md)：任务恢复、外部动作和本地依赖的机制参考。
- [真实设备执行路径研究](../research/harness-device-execution-options.md)：未来真实设备 Adapter 的背景，不构成本期选型或验收前提。
- [已有技术格局研究](../research/agent-harness-landscape-2026-09.md)：候选技术的调查线索，选型时需核对官方规范和版本。
- [Harness 技术实现方案决策地图](../../.scratch/harness-technical-design/map.md)：决策过程的统一入口。详细问题、依赖、答复与研究证据保存在独立票据中。

文档中的“已确认”来自用户接受的目标；“候选”“待决”用于待核实或待讨论的技术方案。研究报告陈述事实和适配限制，不自动构成选型决定。

## 分册与决策依据

分册按技术责任组织。一项通用能力可以涉及多册，分册也不等同于进程或可部署单元。下列分册均已创建并完成设计决策；具体运行证据须在实施阶段取得。

| 分册 | 主要内容 | 决策依据 |
| --- | --- | --- |
| `02-runtime-and-state.md` | Go 内核模块、任务状态、SQLite 参考实现与存储接口。 | [确定 Go 内核模块与本地持久化 Interface](../../.scratch/harness-technical-design/issues/05-kernel-stack.md) |
| `03-contracts-and-protocols.md` | 内部契约、外部标准映射、版本与能力协商。 | [定义内部契约与外部协议映射](../../.scratch/harness-technical-design/issues/06-protocol-contracts.md) |
| `04-edge-cloud-and-recovery.md` | 拓扑、任务所有权、断连、重启和不明结果恢复。 | [确定端云任务所有权与恢复语义](../../.scratch/harness-technical-design/issues/07-edge-cloud-recovery.md) |
| `05-memory-and-context.md` | 上下文、长期记忆、证据、同步和用户控制。 | [确定上下文、记忆与同步的数据模型](../../.scratch/harness-technical-design/issues/08-memory-context.md) |
| `06-brain-and-agent-collaboration.md` | 大脑循环、模型适配与内部/外部 Agent 协作。 | [确定大脑循环与多 Agent 协作方式](../../.scratch/harness-technical-design/issues/09-brain-agents.md) |
| `07-tools-and-simulated-devices.md` | 工具与模拟设备 Interface、观察、状态变化与故障注入。 | [确定多模拟设备与工具执行的验证方案](../../.scratch/harness-technical-design/issues/10-execution-devices.md) |
| `08-authorization-and-user-control.md` | 身份、授权、凭证使用、数据及任务控制。 | [确定身份、授权与凭证的执行位置](../../.scratch/harness-technical-design/issues/11-authorization.md) |
| `09-extension-ecosystem.md` | 插件、Skill、Agent 的声明、接入及兼容性。 | [确定插件、Skill 与外部 Agent 的接入生命周期](../../.scratch/harness-technical-design/issues/12-extension-lifecycle.md) |
| `10-observation-evaluation-and-evolution.md` | 运行诊断、评测基线、改进产物与发布回滚。 | [确定观测评测与自进化的发布闭环](../../.scratch/harness-technical-design/issues/13-observation-evolution.md) |
| `11-milestones-and-validation.md` | 能力成熟度目标、专项验收和实施依赖。 | [确定能力里程碑与实施依赖](../../.scratch/harness-technical-design/issues/14-implementation-milestones.md) |

## 设计交付核对

- 每个关键选型有结论、适用约束、选择依据和可替换位置。
- 关键 Interface 不仅说明类型，还说明授权、调用顺序、错误、恢复和必要配置。
- 任务状态与执行记录、任务上下文、长期记忆、证据和产物的责任可以区分。
- 断连、重启、取消、权限失效以及动作结果不明的行为可以据文档实施和验证。
- C1–C9、A1–A4、V1–V2 均可追溯到技术设计和验收计划。
- 决策地图中不存在阻碍实施的未解决问题；需要实际运行证据的部分标明验证方法和真实结果状态。

## 文档与票据的关系

票据保存问题、方案比较、研究资产与决策依据；本目录保存面向实现的规范。地图只保存已解决决策的简述与链接。技术方案的修改应能追溯到相关票据，不能把研究建议直接改写为用户已确认的承诺。

## 实施入口

先从 [能力里程碑](11-milestones-and-validation.md) 读取阶段和逐项成熟度目标，再按 [实施工作包](11-specialized-validation-and-handoff.md#发布证据与实施任务交接) 拆分有明确契约和验收结果的任务。API 质量规则见 [评测基准](10-evaluation-contracts-and-release-gates.md)，联网问答、GUI 与个性化见 [专项验收](11-specialized-validation-and-handoff.md)。

本轮只完成技术方案与文档检查，没有创建业务实现、安装依赖或执行发布。研究报告提供一手机制证据，不代替故障、互操作、模型质量或统计方法的实际验证。
