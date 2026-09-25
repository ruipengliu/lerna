# 任务、执行与 UI 协议映射

[协议阅读入口](protocol.md) · [领域协议目录](domain-profiles.md) · [公共线格式](wire-format.md) · [静态与互操作验证](validation/README.md)

本页把任务、执行和 UI 的领域合同映射到标准消息。下列领域章节是本协议的规范性引用，第三方实现须同时满足其可观察行为和本目录的 Schema、注册表及公共交付规则；本页不再定义第二套任务或 UI 状态机。领域文档中的默认存储、宿主和工作扫描方式不构成指定实现要求。

<a id="lifecycle"></a>
## 1. 按职责读取规范

| 实现职责 | 必须阅读的领域规范 | 本目录中的编码依据 |
| --- | --- | --- |
| 任务请求方／任务权威 | [任务接纳、状态、输入与终态](../task-kernel/lifecycle.md)、[接口与固定结果恢复](../task-kernel/storage-and-interfaces.md)、[暂停、预算、补证和控制恢复](../task-kernel/control-and-management.md) | [task](schemas/task.schema.json)、[task-control](schemas/task-control.schema.json) |
| 执行请求方／执行端 | [能力声明与接口](../capability-and-execution/catalog-and-contracts.md)、[执行、效果、取消与恢复](../capability-and-execution/execution-and-recovery.md)、[跨端事实及管理](../capability-and-execution/remote-contracts.md) | [execution](schemas/execution.schema.json)、[execution-control](schemas/execution-control.schema.json) |
| UI 提供方／渲染端／输入转交方 | [UI 交互规范全文](../application-and-interaction/interaction-contract.md)；输入、内容、每端呈现以及所用目录／预览／管理能力均须遵守 | [ui](schemas/ui.schema.json)、[interaction](schemas/interaction.schema.json) |

任务、执行和 UI 的对象标识、裁决者和成功含义沿各自规范；message_id、operation_id、响应及最终事件的线关联沿[公共线格式](wire-format.md#response)。同逻辑端点的本地交接也遵守[持久交接](message-contract.md#handoff)，无需经云端绕行。对跨端调用，领域恢复及身份前提按[恢复规范](recovery-and-control.md#resume)和[领域协议目录](domain-profiles.md)装配。

<a id="input"></a>
## 2. 输入与动作的线映射

完整规则以 [UI 输入转交](../application-and-interaction/interaction-contract.md#input)和[任务输入消费](../task-kernel/lifecycle.md#wait)为准，包括接纳与消费的不同成功点、唯一父子关联、重复投递、多端竞争、过期及丢失回执后的行为。

| 交接 | 线消息与关联 | 规范定位 |
| --- | --- | --- |
| 渲染端 → UI 权威 | ui.input 携带 surface_id、seen_revision、input_request_id、值及父 operation_id；需要预览时由受信宿主附加 preview_receipt | [输入规范](../application-and-interaction/interaction-contract.md#input)、[预览证明](../application-and-interaction/interaction-contract.md#preview) |
| UI 权威 → 任务权威 | task.input 使用固定业务子 operation_id、原 task_id／input_request_id、回应及适用凭据 | [任务消费](../task-kernel/lifecycle.md#wait)；UI 父操作不是业务子操作 |
| UI 权威 → 渲染端 | 原 ui.input response 与 ui.input_result 关联父操作；ui.query_operation 查询父处理状态 | [UI 原操作恢复](../application-and-interaction/interaction-contract.md#input) |
| 渲染端 → UI 权威 | ui.action 业务载荷为 surface_id、seen_revision、action_id；控件种类不改变注册 lane | [声明式动作](../application-and-interaction/interaction-contract.md#management)；submit_input 只生成 ui.input |

父子关联的持久时点由 UI 规范规定，协议允许首次转交前确定；默认宿主选择在接纳事务同时固定。线消息无需暴露其内部表结构或工作领取方式。

<a id="view"></a>
## 3. 内容和呈现载荷

完整内容、输入完整性、修订、delta 适用条件和 fallback 以[内容恢复规范](../application-and-interaction/interaction-contract.md#view)为准。

| 消息 | 关键载荷与线关联 |
| --- | --- |
| ui.snapshot@1 | 同一 surface／revision 下的完整 view、input_requests 与 final |
| ui.get@1 | 请求 surface_id；成功返回完整快照及源端点 presentation={revision, state} |
| ui.delta@1 | surface_id、base_revision、新 revision 及已有文字块的追加数据 |
| ui.input_requested@1 | 输入投影通知；恢复基线仍是完整 get／snapshot |
| task.query_projection@1 | 固定任务中间投影和结构化必需预览，编码见 [interaction Schema](schemas/interaction.schema.json)，语义见[预览合同](../application-and-interaction/interaction-contract.md#preview) |

<a id="presentation"></a>
### 每端呈现条件更新

ui.set_presentation@1 请求包含 surface_id、expected_revision 和 state，成功同步答复包含 surface_id、revision 和 state。源端点取自受信上下文；完整的初始状态、条件更新、原决定恢复、本端意图持久化、关窗与重连行为集中在[每端开闭规范](../application-and-interaction/interaction-contract.md#presentation)。内容 seen_revision 与呈现 expected_revision 对应不同事实，不能互换。

<a id="result-recovery"></a>
## 4. 任务和执行结果的恢复映射

| 恢复对象 | 请求与返回 | 领域权威定义 |
| --- | --- | --- |
| 当前任务 | task.query → 当前任务及控制、预算投影 | [任务接口](../task-kernel/storage-and-interfaces.md) |
| 固定正式结果 | task.query_result@1 按 task_id 返回完整原 task.result；包括原修订、摘要及成果引用 | [正式结果查询与恢复](../task-kernel/storage-and-interfaces.md#result-recovery)，包括未终态、清理、无权及内容未取得的行为 |
| 原任务控制操作 | task.query_operation → 原处理状态；task.query_cancel → 原取消完整固定答复 | [控制恢复](../task-kernel/control-and-management.md#recovery) |
| 原执行操作及取消 | execution.query → 当前事实；execution.query_cancel → 原取消完整固定答复 | [事实投影](../capability-and-execution/remote-contracts.md#facts)、[取消查询](../capability-and-execution/remote-contracts.md#management) |

execution.invoke 的 capability、capability_version 和 arguments 按已验证声明编码；关联任务时携带 task_id 与 owner_epoch，独立获准操作可不关联任务。精确使用依据及当前门禁沿[执行合同](../capability-and-execution/remote-contracts.md#catalog)。执行结果的 state、execution、effect 分别编码过程、执行事实和效果，其判定不由通信模块或 UI 代替。

<a id="catalog"></a>
## 5. 消息族查阅

下表是交接类型导航；全部消息、kind、lane、可靠性、scope 和支持版本以[注册表](schemas/standard-registry.json)为准，精确请求／响应字段以对应 Schema 为准。task.input 和 ui.set_presentation 同步完成；异步请求的接纳及最终事件按所属领域合同处理。

| 消息族 | 请求／事件及结果入口 |
| --- | --- |
| task | submit → result；input；cancel → cancel_result；query／query_result／query_operation；status／input_requested |
| execution | invoke → result；cancel → cancel_result；query；progress／fact |
| ui | input → input_result；action → action_result；query_operation；get／snapshot／delta／input_requested；set_presentation |
| 目录与订阅 | task.list、ui.list_surfaces、双方 subscribe／query_subscription／unsubscribe／directory_changed，见[目录合同](../application-and-interaction/interaction-contract.md#directory) |
| 任务控制及其他管理 | 见 [task-control Schema](schemas/task-control.schema.json)与[领域协议目录](domain-profiles.md)，UI 展示责任见[受信管理规范](../application-and-interaction/interaction-contract.md#management) |

<a id="view-reference"></a>
### 声明式视图元素

视图以 `{type, version, data}` 编码，内建 harness.ui.document@1 的元素如下；安全展示与输入关联遵守[UI 规范](../application-and-interaction/interaction-contract.md#view)。扩展类型与显式 fallback 由[视图协商](extensions.md#views)定义。

| 元素 | 编码内容 |
| --- | --- |
| text／markdown | 展示文字 |
| content | 受控内容引用 |
| form | text、number、boolean、choice 字段及输入请求关联 |
| actions | submit_input、cancel_task 声明式意图及动作标识 |
| 自定义视图 | 已协商类型、版本、数据与显式 fallback |

## 6. 示例与验证路径

[task-ui-flow.json](examples/task-ui-flow.json) 展开握手、任务提交、截图、输入和最终呈现。端点尾号 1 为渲染端，2 为任务核心与 UI 管理器，3 为执行端；2 → 2 的 task.input 是本地可靠交接。截图能力为 com.example.device.capture@1，参数见[示例 Schema](schemas/example-extension.schema.json)。核心固定投影、必需预览及实际内容取得另按[交互领域示例](examples/interaction-domain-flow.json)和规范核对；静态消息串不代表提供方已实现。累计回执集中在示例末尾便于阅读，实现须按回执策略及时确认。

[result-and-presentation.json](examples/result-and-presentation.json) 编码多端开闭和超窗结果查询；[interaction-domain-flow.json](examples/interaction-domain-flow.json) 编码目录、订阅与预览合同。示例的消息时间、身份和持久记录是静态前提，不能证明运行恢复已成立。完整业务链按[整体设计推演](../design-walkthrough.md)阅读。

本目录[验证入口](validation/README.md)检查 Schema、注册、消息／操作关联和互操作映射。输入竞争、回执丢失、关窗重启、内容与呈现恢复的完整刺激及预期只在[应用运行矩阵](../application-and-interaction/validation.md#tests)定义；任务和执行结果另依各自领域验收，不在协议重写业务成功条件。
