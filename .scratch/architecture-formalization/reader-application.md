# 正式架构审校：应用接入开发者

Status: ready-for-human

审校日期：2026-10-02。读者角色：熟悉 API、数据库事务、后台作业，初读不熟悉 Harness 术语的独立应用接入开发者。

初读严格按正式正文 `README → application-workflow → core-data-model → interaction/session-and-task、interaction/implementation → contracts 方法与 Schema → request-data-flows` 进行；形成以下复述后，再查正式交互 README、Orchestrator 专题和 Security 实现以排除遗漏。没有先读 `.draft`、research、其他 scratch 审校或预期问题清单，也没有编辑正式正文。当前尚无运行 SDK 是明确交付边界，不作为缺陷；自由输入、多 Lane、完整分支不进入本次要求。

## 初读复述：“生成报告并保存”怎样接入

1. 用户准确目标先发布为 Content。应用在自己的本地事务保存 Message、选定逻辑 Orchestrator、完整原 `task.submit` Command、消息关联及 outbox/job；成功保存只可显示“消息已保存”。`task.submit` 的 applied 表示原 Task 已创建，应用保存 `(orchestrator_id, task_id)`、`submit_command_id` 并补 SessionTaskLink。Session 可以包含多个 Task，关闭页面或归档 Session 不取消它们。
2. 内核固定目标与获准材料产生 Decision，准入工具 Operation；应用查询原 Task，无需创建内部子记录。报告生成、Content 保存、文件写入是不同事实。成功需要全部必要条件、目标覆盖、原写 Effect 及独立读取实际目标字节；只有 `task.result` 的固定 Result 可以支撑完成标记，工具回执或助手“已保存”文字不能代替它。
3. 接纳答复丢失或 TaskLink 写回失败，应用沿首发前保存的原服务和完整 Command 查 `receipt_lookup`，补回同一 Task 关联；不换 Orchestrator、不新建任务。渲染缓存丢失时读取原 Surface、Task、InputSubmission 和当前可披露 Content，隔离旧回调；不重跑模型或工具，也不因重开页面自动 resume。
4. 澄清由实际业务 owner 保存版本化 InputRequest。回答固定 request_id/revision、answer_ref 和 preview_refs，经 `interaction.input` 的 queued/sending 转交；只有原 `task.input` 消费事务才是回答生效。纯聊天批注只保存消息，不隐式改变目标或发模型请求。本人确认先固定完整 consumer Command，受信 `confirmation.decide` 保存 approved；原业务 owner 在自己的事务一次消费，普通聊天“同意”无效。成果验收也不能消除未知外部效果。
5. 暂停是 control=paused，阻断新决策、评估和行动；原效果、费用继续核对，已有完整证据仍可成功。取消是 status=cancelled，结束目标推进，迟到事实继续收尾。界面分别显示 Orchestrator 决定和执行入口实际落实，不能从 applied 宣称所有外部进程已停。
6. Result 引用存在但正文当前不可披露时，保留获准最小状态、原版本/关联和具体缺口，禁用依赖正文的输入，停止展示缓存；不能把历史判断改成正文仍可读，也不把正文关闭误报为原任务未成功。

## 接入判断

原提交/Task/Session 关联、分层成功点、暂停取消、结果正文不可读、原决定恢复与旧流隔离基本可按正式正文落实。下面三项仍要求接入者自行发明字段含义或交接方式；建议在发布前补齐。无需扩大首版能力或新增 Session/Run 状态机。

### APP-01：结构化回答的 Content 字节编码没有共同定义

- 优先级：P1。这影响已承诺的目录澄清和多字段结构化回答的独立互操作。
- 正式位置：[交互实现：2.1 输入 Schema 子集](../../docs/architecture/interaction/implementation.md#21-输入-schema-子集)，尤其第 168–177 行；[应用入口](../../docs/architecture/application-workflow.md#entry-points)；[Schema](../../docs/architecture/contracts/schemas/protocol.schema.json) 的 `/$defs/SurfaceInputSchema`、`/$defs/TaskInputInput`、`/$defs/InteractionInputInput`；[输入消费示例](../../docs/architecture/contracts/examples/protocol/07-input-consumption.json) 的 answer_ref。
- 我的实际理解：Renderer 读 InputRequest.schema.fields 生成表单，取得各字段值后发布 answer_ref 指向准确回答正文。原业务 owner 必须读取同一字节并检查未知字段、类型、选项与范围，不能在重试时重新编码默认值。
- 依据与问题：正文定义 fields 的 name/type/required/label，却只说 answer_ref 指向“准确回答正文”。线 Schema 只约束 ContentRef，未定义回答正文顶层结构、media_type、字段到键的映射、choice/choices 的值表示、可选空值/缺省及 action 如何对应 allowed_actions。现有示例 answer_ref 使用 text/plain，未展示 fields 对应的真实字节。一个实现保存 `{"directory":"/reports"}`，另一个保存纯文本 `/reports` 或 `{"fields":[...]}`，都不能从现有正文排除；消费者无法据此实现相同校验。
- 必要修订：为已支持表单回答定义一种明确、有界的正文编码（或声明按哪份受信绑定提供编码规则），固定字段映射和五种类型、选择 ID、空值/省略与允许动作的表达；说明编码后发布 Content 并固定引用。添加至少一个带实际正文及字段校验的目录回答例子；若编码属于共同契约，纳入相应 Schema/跨调用验证，不只展示随机 ContentRef 摘要。

### APP-02：省略 Surface 的直接 API 路径无法保证发现待答请求

- 优先级：P2。带 Surface 的默认 UI 有 request_refs；问题仅限正式明确允许的直接 API 组合。
- 正式位置：[应用入口](../../docs/architecture/application-workflow.md#entry-points) 的“直接 API 应用可省略 Session 和 Surface”、查看问题及直接 task.input 段；[用户输入](../../docs/architecture/orchestrator/task-lifecycle.md#task-input)；[Schema](../../docs/architecture/contracts/schemas/protocol.schema.json) 的 `/$defs/Task/properties/wait_reasons`、`/$defs/WaitReason`、`/$defs/InteractionRequest_ReadInput`；[方法登记](../../docs/architecture/contracts/schemas/methods.json) 的 task.read 与 interaction.request_read。
- 我的实际理解：无 Surface 时，应用先用 task.read 看到输入等待，再向请求 owner 调 interaction.request_read，取得问题、schema 与预览，最后直接 task.input 回答。
- 依据与问题：task.read 只返回 Task；可定位请求的候选字段是 wait_reasons[].object_ref，但 WaitReason.required 只有 kind 和 resume_condition，且正式正文没有规定 kind=input 时 object_ref 必须指向准确的 InputRequest 修订。request_read 则必须得到完整 owner_id/id/revision。一个合法 Task 返回 `{"kind":"input","resume_condition":"需要保存目录"}`，直接调用者就没有可用的请求发现入口；不能从自然语言条件猜 request_id。
- 必要修订：明确无 Surface 的发现链，例如规定 input 等待必须携带准确请求 ObjectRef，并在 Schema/关联校验和示例中保证这一条件；说明被替代或已消费的请求如何从当前 Task 取得新引用。若该组合暂不支持自动发现，应收窄“可省略 Surface”的适用范围并指出宿主必须提供的已定义内部入口。

### APP-03：acceptance 队列和先固定的可信 consumer Command 没有连接起来

- 优先级：P2。直接受信 task.accept_result 路径可实现；泛称 acceptance 可以经交互输入队列转交仍缺精确映射。
- 正式位置：[应用入口](../../docs/architecture/application-workflow.md#entry-points) 第 21、26、31 行；[交互输入与验收](../../docs/architecture/interaction/README.md#input-consumption) 第 147 行；[输入接纳事务](../../docs/architecture/interaction/implementation.md#input-durable-boundaries)；[两端确认](../../docs/architecture/interaction/implementation.md#confirmation-races)；[Security：固定命令后请求受信确认](../../docs/architecture/security/implementation.md#固定命令后请求受信确认)；[Schema](../../docs/architecture/contracts/schemas/protocol.schema.json) 的 `/$defs/InteractionInputInput`、`/$defs/TaskAccept_ResultInput`、`/$defs/ConfirmationRequestInput`；[受信验收示例](../../docs/architecture/contracts/examples/protocol/54-confirmation-acceptance.json)。
- 我的实际理解：acceptance InputRequest 也能从 Surface 呈现，普通“同意”无效。受信宿主应预先固定 task.accept_result 的 command_id、candidate_hash、goal_revision 和 confirmation_ref，再请求/决定 Confirmation，最后消费同一完整命令。
- 依据与问题：共同确认规则明确 consumer_command_id 和完整 Command 在本人决定前固定；但交互队列规则在 `interaction.input` 接纳时由 InputService 构造唯一 target_command_id。公共 InteractionInputInput 只有 input_id/surface_id/request_id/revision/answer_ref/preview_refs，没有如何识别预先保存的 consumer Command 或 Confirmation 的字段。InputRequestView 也不含其引用。受信验收示例走 confirmation.request→decide→直接 task.accept_result，没有展示 interaction.input 到该已批准命令的队列交接。因此“处理器把 acceptance 转到 task.accept_result”还不足以决定命令何时生成、如何保证 target_command_id=consumer_command_id、confirm/deny 如何成为输入队列状态。
- 必要修订：明确首版验收采用受信宿主直接提交预先固定 task.accept_result，并说明它不走普通 interaction.input 队列；或定义已有处理器如何耐久绑定该 consumer Command、队列如何复用同一身份和确认引用、拒绝本人确认是否生成 InputSubmission。补一条完整字段序列。无需为普通聊天添加确认能力或新 Run 协议。

## 已核对但不登记为问题

- SessionTaskLink 创建来源至多一个是应用登记域的内部约束，task.submit 没有 Session 字段并非遗漏；`interaction/implementation.md#session-storage` 明确了原 Command 的关联和补写事务。
- Task.revision、goal_revision、control_revision 的用途在 `orchestrator/task-lifecycle.md` 的修订表已有区别；控制传播与旧提案失效不需要单独新增 Run。
- 结果正文失效不重写固定 Result；Surface 的 gaps、当前 content.get 资格与最小管理披露已有说明。
- PresentationStore 是默认宿主内部存储边界，公共 surface_read 不带 Presentation 本身不能直接认定矛盾；远端适配器若不共享该宿主仍应由其装配声明补明确切读取端口，但本轮不要求新增公共查询。
- 不要求首条报告任务实现全部 105 个方法，不把确定性续行与数据流程 D 为计量固定的三轮模型回放混为最低实现要求。

## 复核状态

报告已交根审校者。等待针对性修订；复核以本报告三项可实现性缺口是否闭合为准，不要求额外自由输入、完整分支或运行 SDK。

### 2026-10-02 定向复核：正式正文

状态：APP-01／02／03 的正文缺口均已 resolved；机器资产正在由另一审校者完成，本轮不将其进行中的文件或旧内容重复登记为缺陷。最终字段/真实字节复核待资产 ready 后补记。

- APP-01 resolved（正文）：[共同回答合同](../../docs/architecture/contracts/protocol.md#input-answer-body)与[交互编码](../../docs/architecture/interaction/implementation.md#input-answer-content)已经定义封闭 `input-answer/1`、JCS 无 BOM UTF-8、application/json、准确字节摘要及长度、字段名映射、五种值、Unicode 码点长度、安全整数、选择 ID/顺序、可选省略与 null 拒绝。实际目录回答的完整 JSON 使初读者不再需要发明正文。Renderer、InputService 和普通业务消费者的独立复验及原引用重投也明确。
- APP-02 resolved（正文）：[应用入口](../../docs/architecture/application-workflow.md#entry-points)、[Task 请求发现](../../docs/architecture/orchestrator/task-lifecycle.md#task-input)和[请求/等待事务](../../docs/architecture/orchestrator/implementation.md#input-waits)已经串起 `task.read.wait_reasons[kind=input].object_ref → interaction.request_read → task.input`。完整披露必须带准确 owner/request/revision；修订、消费、过期、替代与 Task 等待在同事务更新；受限披露用 QueryResult.gaps，并禁止从空可见集合推断等待解除。无 Surface 路径有明确发现与刷新方式。
- APP-03 resolved（正文）：[验收交付](../../docs/architecture/interaction/implementation.md#acceptance-delivery)已经明确先耐久固定 consumer Command/Confirmation，再 approve，随后把同一完整命令放进回答 Content，通过交互队列采用原 command_id。InputService 从当前请求和认证绑定确定服务并与原确认逐项比较；原业务共事务消费请求与确认。deny/expired 不造虚构回答，queued 撤回不改写批准，sending 查原消费，同一交互 owner 的 consumer→input 唯一绑定保留到最小关闭记录；选择队列后宿主不并行直发。批准/排队/业务消费/Task 成功可以分别实现和呈现。

本次只读针对性正式修订，没有新增真实正文缺口。上述已 resolved 指实现规则已经闭合，不代表本人认证、数据库竞争或运行 SDK 已被实测证明。

### 2026-10-02 最终定向复核：Schema 与真实字节

状态：APP-01／02／03 全部 resolved。正式正文与新增机器资产一致，本读者审校通过；没有剩余真实缺口。

- APP-01：`protocol.schema.json` 的 `InputAnswerContentRef` 将准确引用限制为 application/json 和最多 1 MiB，TaskInputInput、InteractionInputInput、InputSubmission 已引用同一定义；`input-answer.schema.json` 提供封闭封套、五类可表达值及准确消费者分支。`validate_input_answers.py` 对当前请求执行字段/动作/类型/选项校验，并用共享 JCS 实现核对真实 UTF-8 字节、hash 与 byte_length。阅读了目录文本、目录选择、五类字段、空选择、空文本、可选省略、保留空白及受信验收等 9 份实际正文，均符合已复核正文，不再仅靠随机 ContentRef 表示回答。
- APP-02：WaitReason 对 kind=input 条件要求完整 ObjectRef；InputRequestView 约束任务归属及请求类型。新增 [66-input-request-discovery.json](../../docs/architecture/contracts/examples/protocol/66-input-request-discovery.json) 确实展示无 Surface 的 task.read→准确请求读取→task.input，三个入口的 owner/request/revision/answer_ref 一致。发现投影与反例包含旧修订、错误 owner/Task、关闭请求、遗漏等待、无引用以及受限披露/gaps，契约不会接受一项无身份却可回答的 input 等待。
- APP-03：acceptance InputRequest 机器分支固定单一必填 decision=accept 选择及 allowed_actions=[accept]；正文消费者只能是 task.accept_result。验收真实字节的 command_id 与给定 Confirmation.consumer_command_id、queued InputSubmission.target_command_id 相同，完整 consumer Command 逐字段相等；校验器同时检查请求、候选、目标修订、确认修订/状态/期限/摘要及原命令交付关联。缺消费者、换命令/确认/目标/期限、非 approved、请求/候选不匹配等反例被拒绝。原 54 序列仍表示直接受信路径，新的真实字节 fixture 表示排队交付路径，两者没有互相替代。

验证命令：`/tmp/lerna-architecture-formalization-proto/bin/python docs/architecture/validation/validate_input_answers.py`。

实得结果：PASS；9 real input-answer byte fixtures，112 rejected answer mutations，3 input discovery projections，10 rejected discovery mutations，9 protocol answer_ref byte bindings，1 protocol input wait binding。此次只验证静态 Schema/JCS/给定记录关联；没有扩展到全局 runtime 测试，也没有把运行本人认证、内容授权、数据库原子性、队列撤回竞争或 SDK 故障恢复登记为通过。
