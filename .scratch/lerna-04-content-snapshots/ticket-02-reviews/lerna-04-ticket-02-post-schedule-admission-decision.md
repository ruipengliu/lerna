# 04 票02：调度后最终准入与回滚决定

2026-10-04；授权 Astra/high 必要窄决定。固定源码 `75202ee0077b5bf12b432b5017ad97a1dfcab681`，WT `/tmp/lerna-worktrees/content-snapshots-02`。直接读该对象的 `domain/content/service.go`、`closure.go`、`ports.go`、`adapters/postgres/internal/pgstore/core.go`，并核已采用 `ticket-02-handoff.md:37`。未读另一轴报告，未运行 native/Go/DB/build，未改源码。后续 WIP 的重复消除和时钟优化不是本稿已验证的实现。

## 1. 必须修正与最小默认

**采用首次事务全部回滚、再以原键单独固定拒绝的两次有限事务；无需 savepoint、新 Repository 端口或框架。** 当前 Put 的新版本分支在 `SaveVersion → SaveSources → ScheduleRetention → Trigger → SaveCommand` 后没有最终当前时钟；已有版本的新 Command 收紧 cap 分支也在 `SaveVersion/ScheduleRetention` 后直接存 accepted。原 `reject()` 只写 Command 并返回 nil，因此绝不能在这些写之后调用它来表达迟到拒绝：这会连同版本、cap、来源索引、水位、change、责任和 Job 一起提交。

既有 `Core.Within` 明确在 callback 返回错误时不调用 Commit；只有 callback 成功且 ctx 有效才 Commit。利用这个现有边界，不在 SQL 层拼业务补偿 DELETE，不先提交业务再试图撤销，也不把不同 owner 操作纳入伪原子事务。

## 2. 第一次事务：最终门必须覆盖全部阻塞工作

保留现有原 Command 优先路径：锁原键、锁后当前 reader、相同主体和摘要直接返回原回执；该路径不重新检查旧 Put 的 AcceptBefore、政策或保留期。新 Command 才走新准入。

新准入的两个 accepted 分支都在全部必要写入（包括可能阻塞的 Trigger 和 SaveCommand）完成后、callback 返回 nil 前，执行最终 reader 检查与 fresh DB Now。原 accepted Command 此时只是同事务暂存事实，尚不能返回 received。以该时刻检查原 AcceptBefore、已锁定准确目标及全闭包实际动作的 Policy.ValidUntil，以及请求/政策/原 Effective/Current/全部来源 Current 的最严保留 cap。政策行 FOR SHARE、版本行锁保持至事务结束；不必重跑整套闭包，也不能跨事务复用资格。保留既有拒绝原因优先级与全主体/用途/fullRef 检查，不刷新任何原 deadline、budget、revision 或 attempt。

最终 reader 无资格或检查本身失败：直接错误退出，使所有暂存写回滚；不借业务拒绝回执向已失去命令读取资格的主体披露事实。最终业务时间资格不满足而当前 reader 仍有效：返回**本次调用私有且可精确识别的 abort 标记**，携带已决定的既有拒绝原因（例如 expired/forbidden）；不要返回 nil，不要再执行原 reject。标记不是公开 ErrorCode、新协议或一般事务 API。清除/隔离暂存 accepted 变量，保证错误路径不误返回它。

裁决点是最后阻塞工作后的有限 owner 事务授权观察，不声称授权持续到客户端收到回包，更不保证墙钟不会在物理 COMMIT 时推进。新事务方案也不延长原出版或自然义务期限。Get.AcceptBefore 仍是已采用的准入语义，本决定不改变 Get。

## 3. 第二次事务：只固定原 Command 的拒绝

只有第一次 `Within` 返回上述本调用主动 abort，且不存在 CommitUnknown/其他未分类失败，才进行**最多一次**第二短 Tx；使用同一个调用有限 ctx 的剩余期限，不用 Background，不重新给总调用预算。采用现有 CheckCommandReader、LockCommand、SaveCommand 即可：

1. 验证当前 command reader；获取原 Command advisory 锁；锁等待后再核当前 reader。
2. 若已有 Command，仍按原主体/摘要优先处理：同主体同摘要返回真实已固定原 receipt；不同摘要 idempotency_conflict；主体不符 forbidden。不能用本地已观察迟到理由覆盖并发 winner 的 accepted 或 rejected。
3. 若仍不存在，仅 SaveCommand 写**原请求的** digest、subject、exact ref、purpose 与已观察理由的 rejected receipt。绝不调用 SaveVersion、SaveSources、ScheduleRetention、Trigger，也不重新执行 Put 准入。原 AcceptBefore 已过期不阻止记录其拒绝；否则 expired 永远无法固定。当前 reader 仍必须有效，SaveCommand 等待后再核 reader，失败则本次记录也回滚。
4. 第二次 Commit 成功才返回 received(rejected)。其 CommitUnknown 仍按现有协议返回原 command_ref + query_or_retransmit_original；不得返回“确定 rejected”。其他错误保留真实原因，不能伪造已有账本记录。

第一次若在真正 Commit 返回 CommitUnknown，同样直接返回现有未知结果，**不得**进入第二次拒绝事务。第二次尝试失败也不循环重试。原 key 的查询/重传沿既有当前 reader 协议恢复。

第一次主动 abort 只证明该路径没有发送 Commit，不等于本稿获得了 native Rollback/FD 关闭确认；Core 当前 deferred Rollback 没有对外确认接口。第二次重新获得同一事务级原键锁提供现有服务端串行顺序：旧事务未释放锁就只能有限等待失败，不强行绕过。不要将两 Tx 方案写成物理资源关闭证明，也无需为本次增加通用生命周期接口。

两 Tx 间崩溃时可能没有任何原 Command 记录：这不是“已固定拒绝被忘记”，而是拒绝尚未提交。原请求重传继续按当时现有协议处理；如只因政策短期资格变化而失败，之后政策合法续期时重新准入并不篡改一个不存在的回执。若期间相同原键已由并发调用固定，优先返回该真实结果。其他键对同版本的合法已提交变更不得被回滚补偿误删。

## 4. sole owner 必须验证的实际出口

- 在真实 ScheduleRetention 已执行完后，以有限机械门阻塞至 AcceptBefore 或适用 Policy.ValidUntil 过期，但仍处于测试/事务允许的有限窗口；释放后观察原 Command 固定拒绝，授权公开查询无新增版本/无该准入传播责任或出版行为；独立正常对照相同调度成功。门不能只在调度前返回伪错误。
- 已有版本新关联收紧 cap 的同类迟到：新 Command 拒绝，原版本/原回执/已出版正文与原 cap、原责任历史保持原事务前事实，不能借此“回滚”后来别人合法提交的变更。
- 同原键并发在第二 Tx 前已固定：返回其真实 receipt；异摘要冲突；reader 在原键锁或第二次账本写等待期间过期则无越权回执。所有观察使用公开/受信管理消费口，不把业务私表 SQL 当 oracle。
- 实际错误/CommitUnknown 分别覆盖首次 accepted 提交与第二次 rejected 提交；原键查询恢复真实结果。机械事务装饰器只能标注注入的返回路径，不冒充真实物理提交/关闭故障。

本稿不声称上述红绿已执行。当前 7AC 与全片出口资格不变。

## 5. 闭包时钟性能补充决定

Owner 报告最新测量版原 64/65、60 秒 ctx 在 i59 Put 处失败约 60.04 秒；累计 Install 1.155s、Put 32.442s、Step 25.969s。本稿未执行或独立复核该测量，不据此放宽任何 60/120 秒、产品预算或测试集合。

**采用 nil-actions 分支删除每节点两次无消费的 Store.Now。** 固定 752 `registeredClosure` 的第一次 Now 仅供 actions 循环，第二次仅供 `len(actions)>0` 的 CurrentRetainUntil 判断；空 actions 时二者不影响结构结果。空动作遍历仍保留完整 owner/fullRef/≤64/cycle/中间版本/published/锁定与完整来源图，原调用者的当前 policy、原 due/deadline、cap 和最终 freshNow 检查不删。数据库缺失时钟服务不再导致一个本不依赖时间的结构遍历失败是准确职责收窄，不是授权缓存。

**actions 非空默认保留每节点当前时间/cap 检查。** 单纯说“caller 最后会用 Bound+freshNow”不足以删除：后续节点可能先返回结构/来源错误，使之前已经失效的来源资格没有先裁决；各个调用者的错误与元数据披露顺序不能被优化改变。CheckPolicy 的真实 PG 实现仍须在政策锁等待后读 fresh clock，实际请求动作和装饰器调用不得跳过；不同 I/O 前后 Tx 全部重新取得资格。

只有 owner 能逐调用点证明所有成功与早退/错误分支都在任何公开观察前用完整已观察 bound 做相同授权优先裁决，并保留每动作真实检查、同 Tx 锁及各独立 Tx fresh clock，才可进一步改为最后统一时间检查。这已经比删除空动作两个无用 Now 更大，不是本轮默认。先采用无用读取消除并测原完整覆盖，性能不足再以真实阶段计数决定下一小改；不引入跨 Tx 缓存、截断闭包或统一权限框架。
