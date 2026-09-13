# 22 票执行内容读取的调用资格边界

执行数据查询计量首先需要明确其归属。原 `execution.Content.Read/Save` 仅收到 token、输入引用和能力，无法确定原任务及调用。适配器不能据此自行读取最新 Worker 并当作调用者资格。

新增可选的消费方接口 `execution.RequestContent`：`ReadFor/SaveFor` 接收原 `execution.Request`，其中包含 OperationID、Qualification、输入引用、精确能力描述摘要及控制/资源版本。原 Content 实现继续沿原接口工作。

调用点如下：

| 执行入口 | 传给 Content 的请求 | 保存操作身份 |
| --- | --- | --- |
| Invoke 接纳前输入读取 | 当前已通过基本接纳核对的请求 | 不保存 |
| Run 执行前输入读取 | 持久化调用记录中的 Request | 不保存 |
| 同步结果/核对结果保存 | 原调用记录中的 Request | 原记录 OutputOperation |
| 异步观察证据保存 | 原调用记录中的 Request | 原异步路径为此次保存分配的身份，未改变旧行为 |

Request 是归属信息，不授予新权限；当前授权、任务控制、原执行资格与恢复观察是否允许扣费，仍需在相应消费方实现中检查。本接口不自动加载最新资格，不重置任务额度，也不改变原执行/回执重放规则。

验证通过真实 SDK、SQLite、Content 与 HTTP 获取完成。观察包装器将所有业务调用转发给原受控适配器，只记录收到的 Request。新增测试接入前观察到 reads=0、saves=0；接入后，同进程与关闭重开存储两种路径均保留完整原请求，两次必要读取、一次结果保存，重复 Invoke/Run 不增加读取，结果引用及 OutputOperation 保持。该测试 race 通过（2.894s）。

这只是计量所需的信息边界。生产计量适配器尚未绑定 RequestContent，也尚未证明恢复时的预算资格规则；不能据此声称执行内部 Content 查询已经计费。完整 executioncheck/asynccheck race 回归通过（27.141s/20.628s），execution 包无独立测试；相关 vet 与 diff 检查通过。22 票最终全仓验证与整票双轴审查仍未完成。

独立增量审查：以 `git diff 535b630...ef6c0cc` 为固定范围，Standards 与 Spec 两名独立审查者均未发现 actionable findings。审查为静态核对，未代替上述真实测试；22 票最终审查仍以 6d40e16 为基点，并保留全部原验收条件。

## 正常执行的查询适配器

后续新增 `fetchoutput.Adapter.WithQueries`，返回仅用于执行的 `ExecutionContent`，通过 RequestContent 取得原请求资格后构造 taskcontent。它不对外暴露普通失败事实读取入口；不带 Request 的 Read/Save 直接拒绝。原通用 Adapter 行为保持，观察者与其他上下文可继续独立绑定自己的权限和预算。

真实 Core/SDK 测试中，Invoke 的 GET/READ、Run 的 GET/READ、结果保存的输入/已取得证据两个 GET 共六次实际查询进入原任务的 Actions.Queries。PUT 不计为查询，重建适配器不创建新额度。原调用结束、Core 进入答案阶段后，再用旧请求读取会被原资格检查拒绝，不升级资格，也不消耗当前 Worker 的查询记录。

新增测试最初因缺少 WithQueries 编译失败；接入后 SDK 请求边界、正常执行计量、任务失败读取和重开额度相关 race 通过（7.721s），vet/diff 检查通过。原失败事实/行动/进程恢复相关 race 通过（21.008s）。

当前是正常执行装配验证；研究 CLI 尚未启用这项执行计量。合法恢复可能持有原行动资格和显式的当前恢复资格，需要按已有 WorkPort.actionRecovery 规则核对两者，不能简单把请求替换为当前 Worker。该恢复预算绑定、完整研究装配及获取驱动内部读取计量尚未完成；原票全部验收仍保留。

正常执行计量增量已提交为 `2a87c29`。随后独立审查固定范围 `git diff 57d22b7...2a87c29`：Standards 与 Spec 均无 actionable findings。该结论仅覆盖本增量，不扩大为研究 CLI 或合法恢复计量已完成。

## 原行动与显式恢复资格的计费入口

新增 `ActionPort.ChargeExecutionQuery(ActionBinding, key)`。它先验证完整原行动绑定，再使用现有 guardAction 核对当前租约、任务控制、期限和显式 WithActionRecovery 资格，最后追加到同一 Actions.Queries。普通 ChargeQuery 和执行查询共享同一计数、唯一键及上限，不另建恢复额度。

fetchoutput.WithQueries 现消费该操作级接口，而非只传 Qualification 的普通查询接口。每次查询将原请求的操作、资格、输入、能力摘要及资源/控制版本整体传到 Core；taskcontent 仍在实际 Content I/O 前计费。

Core 公共接口测试证明：原租约有效时可以计费；租约替换后未重新绑定的宿主及旧恢复绑定均被拒绝；显式绑定当前恢复租约后可为原 DISPATCHED 行动计费，改变能力摘要仍被拒绝；普通查询与执行查询合计耗尽 32 条后，再换一个合法恢复租约仍返回 QUERY_BUDGET_EXCEEDED。原操作及其 Qualification 未替换。tasks 完整 race 通过（12.960s），SDK/Content 计量与失败读取/重开额度相关 race 通过（7.942s），vet/diff 通过。

此次恢复用例为仍处于 RUNNING 的行动租约过期。WAITING/reconciliation 恢复开始时的内部版本变化、取消与迟到事实尚需实际执行/进程测试；尚未声称所有恢复状态支持 Content 计量，也未开启研究 CLI 的此项计量。

该操作级恢复计费增量提交为 `3b2701b`。独立静态审查固定范围 `git diff 2a87c29...3b2701b`，Standards 与 Spec 均无 actionable findings。补充普通/执行共享额度的定向 Core race 复跑通过（1.831s）。

## WAITING 恢复开始的单次内部版本变化

新增 Core 用例复现：显式恢复宿主在 WAITING/reconciliation 状态可计费，但 GuardExecution 合法开始恢复并把 Task.Version 加一后，同一宿主的下一次查询返回 VERSION_CONFLICT。

Core 现仅在这一已授权的开始事务中保存 Action.RecoveryStart。查询时，除原 guardAction 路径外，仅允许该精确恢复宿主跨过记录中的一次版本加一：当前资格除版本外必须一致，ExecutionVersion 必须匹配，当前执行操作必须仍为原操作，再重新执行全部原控制、租约、期限及行动检查。任何后续任务变化或 Worker 替换均不匹配。原 Invocation Qualification 不变，查询不能取得新租约，也不能续期。

RecoveryStart 是内部状态，提案预填会被整批拒绝；零值不序列化，原持久化状态保持可读。测试通过 Core Load 核对恢复起点已保存，并验证下一次任务变化及替换租约不会激活旧对象。完整 tasks race 通过（12.880s）；恢复起点、伪造提案、执行计量及答案进程恢复相关 race 通过（tasks 3.735s、fetchcheck 18.025s），vet/diff 通过。

这一验证仍为 Core 状态转换；真实执行/进程下的 WAITING 恢复装配、取消及迟到事实、研究 CLI 完整计量仍需继续完成，未作为本增量已交付项。

该 Core 起点修复提交为 `b390a93`。独立静态审查 `git diff 3b2701b...b390a93`，Standards 与 Spec 均无 actionable findings；该结论不代替真实 SDK 恢复覆盖。

## SDK 恢复驱动检查与查询计量共用起点

真实 SDK/SQLite/HTTP 用例进一步复现：即使执行服务、查询适配器和获取驱动都显式绑定同一 WAITING 恢复资格，合法开始后的驱动 Check 仍返回 denied，实际获取未成功。此前查询入口独有的起点例外不足以支持完整执行路径。

将精确起点识别收敛到 Core guardAction：只有非 consume 检查允许已绑定宿主跨过自身记录的单次版本加一，仍执行原行动、控制、租约、期限、输入版本等全部检查。查询入口直接复用这一检查，不再维护第二份例外。consume=true 不允许沿起点再取得执行许可，也不允许跨后续任务或 Worker 变化。

新增 SDK 测试在 Invoke 接受且进入 WAITING 后关闭并重开实际 SQLite/Content 存储，显式重建恢复绑定，Run 成功取得本地 HTTP 正文，原 Request 不变，六次 Content 查询累计到原账本。Core 用例同时核对合法驱动检查、重复开始拒绝、后续任务变化与 Worker 替换拒绝。定向 race 通过（tasks 1.783s、fetchcheck 4.395s），vet/diff 通过。相关完整 race 回归均通过：tasks 13.735s、executioncheck 24.130s、asynccheck 17.440s、fetchcheck 155.219s。最终全仓验收未执行。

此用例是存储关闭重开，并非新增子进程崩溃实验；取消、迟到事实和研究 CLI 完整装配仍未完成。

独立增量静态审查以 958eb61 为基点，覆盖本节实现及新增 SDK 测试：Standards 与 Spec 均无 actionable findings。22 票最终整票审查仍以 6d40e16 为基点。
