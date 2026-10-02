# 默认应用与 SDK 工作流

[核心数据模型](core-data-model.md) · [四类请求的数据流程](request-data-flows.md) · [Session 与 Task](interaction/session-and-task.md) · [方法索引](contracts/methods.md)

普通应用负责组织 Session、提交与控制 Task、读取 Content 和显示授权需求。Decision、Operation 及其子记录由内核推进，应用无需逐项创建 Snapshot、ModelCall、Attempt 或 Job。需要说明某次行动时，应用组合读取原对象，不维护另一份可独立修改的“整体运行状态”。

本页定义默认应用／SDK 的组合设计，尚无可运行 SDK。下文动作名称是应用交互，不是新增 RPC；线协议仍使用[方法登记](contracts/schemas/methods.json)的 105 个领域方法及[公共调用契约](contracts/README.md)。Session 是应用内部对象；没有新增 Session、Turn、Run 或任意聊天队列协议。精确输入字段以同版 [Schema](contracts/schemas/protocol.schema.json)为准。

<a id="entry-points"></a>
## 1. 调用者需要哪些入口

宿主配置固定认证、发现、内容传输和原命令持久存储，应用在其上提供下列动作。直接 API 应用可省略 Session 和 Surface；省略界面不会改变原命令、许可或任务完成规则。

| 应用动作 | 组合的现有入口与关键输入 | 返回后如何继续 |
| --- | --- | --- |
| 提交新目标 | 先发布获准目标 Content，再调用 `task.submit`；payload 为 `goal_ref`、`orchestrator_id`、`constraints`、`policy_ref`、`budget`、`deadline` | `applied` 表示原 Task 已创建；保存返回的 `(orchestrator_id, task_id)` 与原 `submit_command_id`，再查进度 |
| 查看任务 | 对原 Task 调用 `task.read`；`target_id` 为 task_id，payload 为空 | 读取目标、控制、等待、预算、未结效果及账务；不由最近一条助手消息生成状态 |
| 查看结果 | 对同一 Task 调用 `task.result`；payload 为空 | succeeded 返回固定 Result；其他状态返回原因、未结操作及账务说明。当前完整状态仍从 `task.read` 取得 |
| 取得成果正文 | `content.get` 携准确 `content_ref` 及适用读取变体所需字段，目标为原内容 owner | 核对版本、hash、当前来源和用途后取得字节；Result 引用存在不保证此刻仍可披露正文 |
| 查看可回答的问题 | Surface 的 input 块或 `task.read.wait_reasons` 中 kind=input 的 `object_ref` 提供准确请求引用；用 `interaction.request_read` 的 `request_ref` 向实际请求 owner 读取 | 以当前请求 Schema、kind、revision、期限和必需预览形成输入；披露缺口不能靠猜测请求 ID 回答 |
| 回答结构化问题 | `interaction.input` 提交 `input_id`、`surface_id`、`request_id`、`request_revision`、`answer_ref`、`preview_refs`；随后 `interaction.input_read` 查原 input_id | 前者 applied 可能只保存 queued；后者反映转交和原业务决定，不能显示成“任务已完成” |
| 撤回排队回答 | `interaction.input_withdraw` 以 input_id 为目标，Command 的 `expected_revision` 对应原输入修订，payload 为空 | queued 竞争获胜才能成为 withdrawn；sending 后只能设置 withdrawal_requested 并核对原消费 |
| 暂停、继续或取消任务 | `task.pause`、`task.resume`、`task.cancel` 以固定 task_id 为目标，携对应预期修订及 `reason` | applied 证明 Task 控制决定已保存；实际入口是否停止和原效果是否核清另行展示 |
| 修订原目标 | 先取得当前 Task，再用 `task.revise` 提交准确 `goal_ref`、`constraints` 及预期修订 | 由 Orchestrator 决定修订；旧提案不能自动用于新目标。独立新目标走新的 task.submit |
| 对成果作受信验收 | 受信入口先固定 `task.accept_result` 的完整原命令并取得本人确认；直接提交该命令，或按下文将它原样纳入 acceptance 回答的 `consumer_command` | 交互转交复用该 command_id；业务 owner 一次消费确认与请求，仍由 Task 判断全部条件及未知效果 |
| 处理授权确认 | 受信宿主使用原 `confirmation.read`／`confirmation.decide` 及对应业务命令；准确字段沿[可信确认](interaction/implementation.md#confirmation-races) | 本人批准、业务消费与 Grant 生效分别查询；普通聊天回答不能代替确认 |
| 列表与界面恢复 | 已登记 Orchestrator 来源分别调用 `task.list`；Surface 用 `interaction.surface_read`，变化提示仅唤醒读取 | 目录失联和分页不完整明确显示；列表为空、页面关闭、旧流退出都不是取消或未接纳证明 |

正文发布由内容适配器组合已有上传管理、字节传输和 `content.put`，后者使用原 `upload_id`、准确 `content_ref`、`sources`、`policy`。应用不调用 `memory.create` 来保存每条普通聊天正文。有界内联只用于已声明允许它的字段；当前 `task.submit.goal_ref` 和结构化回答的 `answer_ref` 仍必须是 ContentRef，SDK 不能把它们替换成字符串。回答按 [`input-answer/1`](contracts/protocol.md#input-answer-body) 编成 JCS 规范 UTF-8 JSON，准确发布后固定 `application/json` 的引用、字节长度和摘要；字段值来自当前请求 schema，恢复时不重新编码默认值。

直接 API 先读原 `task.read`：完整披露的 input 等待必须带原业务 owner、request_id 与当前 revision；再调用 `interaction.request_read` 取得请求并核对仍为 open。请求被修订、消费、替代或到期时，业务 owner 在同事务更新请求和 Task 的等待引用，应用重新读取当前 Task 后再决定。Schema、问题正文或引用披露不全时保留具体缺口，不提交猜测答案。

直接 API 的澄清回答调用 `task.input`，payload 为 `request_id`、`request_revision`、`answer_ref`、`preview_refs`；这条路径没有交互转交队列，因而不能随后用 `interaction.input_withdraw` 撤回。采用 Session／Surface 的默认应用通过交互队列转交，固定处理器把 clarification 交给 task.input。`interaction.application_event` 仅交给已绑定应用处理器，不能用它虚构未定义的聊天 steering 协议。

验收入口先固定完整 consumer Command，包括原 command_id、Task 目标、请求修订、候选摘要、目标修订及预先分配的 Confirmation 引用；经 `confirmation.request`、`confirmation.read`、`confirmation.decide` 取得原准确 approved 记录。直接路径发送原 `task.accept_result`。排队路径将同一完整命令放入回答正文的 `consumer_command`，`action_id=accept`、`fields={"decision":"accept"}`，再调用 `interaction.input`；InputService 验证并保存原命令，target_command_id 必须等于原 consumer command_id，不能再生成第二个验收命令。拒绝或取消本人确认在 Confirmation 路径结束，不生成虚构的拒绝验收命令。完整队列竞争和一次消费见[交互事务](interaction/implementation.md#input-durable-boundaries)。

<a id="original-submission"></a>
## 2. 从一次输入到原 Task

1. **固定用户意图。** 新目标、原问题回答、目标修订、任务控制和单纯批注分别选择上表路径。含糊文本先保留草稿；若由模型帮助解释，仍须属于已准入的 Task／Decision，模型提案不直接成为控制命令。
2. **保存首发依据。** 准备准确内容后，按受信发现选择逻辑负责方，并在宿主耐久记录中固定完整 Command、目标逻辑服务、内容引用与 Session 消息关联。认证定位关系可以保存，通用日志不保存 bearer 原文。存储失败时不发送可恢复写入。
3. **发送一次原请求。** 进程内直接调用领域接口，跨端按现有 WSS／gRPC 绑定传送。首次接纳截止、payload 和预期修订在重发时均不改变；网络尝试可以变化，业务身份不变。
4. **解释方法自己的成功点。** task.submit 的 applied 创建原 Task；interaction.input 的 applied 保存 InputSubmission；task.input 的 applied 才说明对应请求消费。方法登记只允许 applied／rejected 时，不因后台尚在处理就增加 accepted 回执。支持 accepted 的其他方法仍按共同契约查询原决定。
5. **补齐关联并观察。** Task 接纳成功后将原 Task 引用写回 Session；答复或关联写入丢失时，沿首发前保存的原服务和命令补回同一关联。应用通过 task.read／task.result 更新视图；内核自行推进 Decision、Operation 和结算。

Session 消息、原命令和待交付关系在应用自己的本地事务共同保存。与 Orchestrator 跨事务交接时，复用持久 outbox 和原命令查询；不能仅保存消息正文就宣称 Task 已接纳。同宿主且已声明共同受限事务的参与者可以合并相应本地提交，条件见[数据流程的事务表](request-data-flows.md#transaction-boundaries)。

<a id="input-scope"></a>
## 3. 输入状态、任务状态与回合视图

应用可以把一条原提交及其回复显示成一个 Turn／Run，但它只是带原关联的视图。新目标以 submit_command_id 关联 Task，结构化回答以 input_id 关联原 request_id／request_revision 和 target_command_id；控制绑定固定 Task。缺少原关联的输出保留缺口，不自动附到“当前正在运行的任务”。

| 要表达的事实 | 权威依据 | 不能据此推导 |
| --- | --- | --- |
| 消息已保存 | 应用消息及原提交记录 | Task 已创建、输入已消费 |
| 交互已排队／正在转交 | InputSubmission 的 queued／sending | 模型已看到输入；sending 也不证明已经消费 |
| 原问题已消费 | 原业务回执、InputRequest 的 consumed_by 及交互消费投影 | 目标已经完成、批准已经签发 |
| 原输入已撤回 | queued→withdrawn 的条件提交 | 其他输入撤回或整个 Task 取消 |
| 撤回已请求 | sending 对应 withdrawal_requested | 必然阻止消费、可以立即用新身份重复发送 |
| Task 暂停／终态 | 原 task.read 的 control／status 与原控制回执 | 外部进程已退出、全部费用已结清 |
| 助手回复已呈现 | 原提交的消息关联及已发布内容 | Task succeeded；临时流片段也不能作为正式成果 |

现有结构化输入支持独立排队和 queued 撤回。**任意 Session 聊天输入的队列、优先级、steering、follow-up、多 Lane、按 RunID 控制尚未具有公共合同。** 完整支持前须明确每条提交的身份、接纳和消费位置、投递到当前或后续工作的规则、撤回竞争、迟到输出关联，再补充方法、Schema、示例与互操作验证。当前不得把一条未绑定 InputRequest 的聊天消息塞入 task.input；也不得以 task.cancel 代替撤回其中一条输入。

首版明确提供的替代选择是保存批注、回答当前请求、显式修订原目标，或提交独立新 Task；它们各有不同用户含义，不自动转译为 steering。修订冲突后先读取当前状态，由用户原意形成新的明确命令，不悄悄换 expected_revision 重发。

<a id="observation-control"></a>
## 4. 观察与控制只维护原对象

默认进度视图组合 Task 当前事实、所选 Result、原请求及获准 Surface；每一部分带自己的来源修订和缺口。六组对象无需形成一个全局递增版本。应用可批量读取和缓存准确 Content，当前内容状态、用途和来源检查仍必须成立。

控制回执只决定原 Task 的控制；关闭页面、归档 Session 和等待超时不产生取消。重开 Session 先读取原任务，active 也要结合 control、等待、当前许可、预算和继续条件；应用不因重连自动调用 task.resume。Task 取消后迟到的 Operation 效果、模型账单和来源缺陷仍归原对象处理，不能把终态重开为新的目标推进。

呈现侧保留原输入／Task 引用、Surface 来源修订和当前连接读取标记。T1 结束后 T2 已创建时，T1 的取消、费用与退出回调只更新 T1；本端取消句柄按原关联条件清理。临时流片段可有界缓冲、合并或丢弃，正式快照和完成提示须能查询对应持久事实；数据丢失后重读原对象，不重跑模型来“补回界面”。详见[旧流隔离](interaction/implementation.md#surface-generation)。

<a id="recovery"></a>
## 5. SDK 恢复决策

`receipt_lookup` 是向原逻辑服务查询 Command 回执的公共传输／宿主机制，不是新登记的领域方法。恢复完整命令与原负责服务由宿主封装，普通应用只需保留该恢复句柄及已知业务引用。

| 当前可取得的依据 | SDK 下一步 | 用户看见什么 |
| --- | --- | --- |
| 原回执已决定 | 恢复同一结果；必要时补 Session 关联、再查原 Task／输入 | 原决定和当前任务进度 |
| 支持 accepted 的原方法已接纳 | 有界查询同一 command_id；不新建替代请求 | 已接纳，等待原决定 |
| 响应未知，原服务可定位 | 查原回执；交互输入优先 `interaction.input_read`，由交互 owner 沿固定目标核对 | 正在核对，不把超时当业务拒绝 |
| 原权威明确未接纳且首次接纳期限仍有效 | 在原前提允许时原样重投完整命令 | 同一次提交继续发送 |
| 原命令已过期、gone 或原服务不可达 | 按对应恢复规则保留未知／缺口，查询已有业务对象；不更换服务或复用已回收身份 | 原提交的具体恢复限制 |
| 本端记录损坏、无法定位原命令 | 从获准 Task／Surface 目录尝试定位已知对象；不足以定位时停止自动重做 | 身份恢复缺口；空列表不是原请求未发生的证据 |
| 只有渲染缓存丢失 | 读原 Surface、Task、InputSubmission 与当前可用 Content | 同一对象恢复呈现，零新增模型／工具动作 |

本端保存答复失败不撤销远端 applied；保留首发前记录，重启查询同一决定。恢复原进度与启动新效果采用不同就绪条件，详见[部署恢复](deployment.md#recovery-readiness)和[SDK 存储边界](interaction/implementation.md#sdk-original-decision)。

## 6. 验收与计量入口

主验收从上表应用动作开始，读取原 Task／Result、输入及外部目标证据，观察实际出站次数和费用归属。沿用[HAR-04／05／11](validation/harness-scenarios.md)、交互 II-20～31 及[协议序列](contracts/examples/protocol/README.md)，不为六组对象新增六套测试专用 CRUD。

[四类数据流程](request-data-flows.md)固定直接回答、读后回答、冷恢复、保存后读回的对象与持久阶段。静态检查可以核对文档、方法名称和字段；队列竞争、真实持久性、出站次数及性能仍是待运行验收，未由本文证明。
