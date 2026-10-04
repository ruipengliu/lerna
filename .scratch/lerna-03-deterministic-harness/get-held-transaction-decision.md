# 03 Get 遇到持有中的完成事务：窄决定

## 推荐

**保留现有 Get 的 Decision advisory lock；采用 parent03 提出的两个实际有限 checkpoint 修正 pre-COMMIT recovery 场景。** 先在 Source Proposal 已提交、Finish 尚未开始时公开取得原 Running/usage；再在真实完成回调成功、Core.Commit 前观察有限 unavailable。持有真实完成事务期间，公开 Get 不是必须读到旧 running。不得改成两个无锁 READ COMMITTED 查询以迁就旧断言。无需新 ADR、通用读模型或产品端口。

读取基线是 `88d234c7a7bfd567b1594e775e1ae3f730c18db9`，相对 root949；本次仅 git 固定源码、已采用决定和安全日志读取，无 build/test/DB/服务调用、环境或凭据读取、私表查询。下述测试要求尚待实际执行。

## 已核实原因与证据限度

1. `.scratch/lerna-03-deterministic-harness/control-decisions.md` §5 明确允许纯 Get 取 Decision 锁以获得同一观察，且不得反向取 pool 锁。`components/decision_engine/service.go:Get` 先当前鉴权、Tx 内检查时间，再 LockDecision、再次检查当前时间与资格，最后读取 Stop 并返回同一观察。
2. `adapters/postgres/decision_engine/store.go:LockDecision` 先取准确 scope 的 `pg_advisory_xact_lock`，再 ReadDecision；`control.go:ReadStop` 是同 Tx 后续查询。已遵守该锁的写者不能在两次读之间更新 Decision/Stop。Get 不等待外部发布，也不需要 pool 锁。
3. `worker.go:finish` 的同一实际 Tx 依次执行 lockedWork、SaveDecision、Complete。`recovery/decision_process_test.go:heldRuleStore.Within` 只有实际业务回调返回 nil 且捕获准确 completed marker 后才进入 `completed_staged_before_commit` hold；实际 PG Core 在回调返回后才检查 ctx 并 Commit。故该门确实位于实际 SaveDecision + Complete SQL 成功之后、原 Commit 调用之前，不是“模拟 completed record”。
4. 子进程/fixture 当前设置为 Tx 3 s、statement 2 s、lock 1 s。held Tx 持有相同 Decision 锁，父侧 Get 因而可能在有限等锁后返回 unavailable。该行为与原先可无锁读到旧行的故事不同，符合已采用控制观察设计。
5. 日志 `/tmp/lerna-03-ticket-03-pg-child-migration3-normal.log` 记录该 pre-COMMIT 测试的 normal_release 与 SIGKILL 两子例均在 `original staged Decision absent` 失败，包耗时 6.802 s，actual_exit=1。日志未打印 Get union 或底层 SQLSTATE，**只能直接证明不是 Found，不能独立证明 native lock timeout 或 Decision 不存在**。当前源码和门提供原因解释；改后须明确观测 public unavailable，若报告确切 SQLSTATE 则需另有实际安全证据，不能从统一错误映射反推。

## 最小场景调整

### 先取得实际公开基线：采用补充方案

parent03 的补充方案足够且推荐采用：为本 pre-COMMIT 故事复用已有 `heldRulePublisher` 的 `published_before_finish` 门，Source 的真实 Proposal Publish 已成功返回后暂停，父侧独立读回两份实际 Source 内容，并通过原公开 Get 取得 FoundRunning 和完整 `confirmedUsage`，关联原 claim/lease。这里只增加测试装配和该场景的一次有限 checkpoint，不更改生产代码、原 lease、资格或数据库事实。

父侧明确核对第一个 stage 的名称/Scenario/generation/refs 后 release，才等待同一 child 的第二个 `completed_staged_before_commit` stage；两个门各消费自己的 release，不能一条 release 串过两门。第二门仍必须经过原 SaveDecision + Complete 的完整 callback 成功，不能改成只收到 SaveDecision marker 就暂停。第二门的 refs 必须与第一门相同。

第一门不得消耗到原 Claim 失效后再把 Finish 拒绝当成功；真实正常分支必须到达第二门并实际 Commit。两次独立读取与调度均受原有限期限约束；若 race 下超出原 lease，报告实际失败并调整测试调度/装配的原因，不能静默续租、放宽资格或推测已到门。第二门里的 bounded Get 可以越过已完成资格检查时的 lease，但仍不得超过实际 Tx deadline；不新增错误的 Commit 后资格重判来改变原生产语义。

### 门内：明确不确定性，保留独立 Source 事实

- 保留实际 stage 身份、generation、ProposalRef、ArtifactRef 断言；独立 Source 连接鉴权读回实际 Proposal/Artifact，检查准确原 Decision/Snapshot/refs 和预期 artifact bytes。Source 已提交与 Decision Tx 尚未获得确认是两件事。
- pre-COMMIT 门内 Get 必须明确匹配 `AsUnavailable()`、准确 DecisionRef、`dependency_unavailable`，并确认调用在有限期限内返回。不能以任意非 Found、Go error、result_unavailable、expired、forbidden 或 nil 作为等价通过；这些会隐藏不同故障。
- 不再写“公开观察到了 running”或“unavailable 证明未 Commit”。门内 public unavailable 只证明该查询当时无法提供有资格的一致视图。真正 pre-COMMIT 定位来自实际回调/Commit 顺序，事务结果来自释放或 SIGKILL 后的独立公开观察。
- 使用现有有限 Get/锁期限，一次观察后立即释放或 kill，不循环探测或任意 sleep。父侧读回与 Get 必须给子 Tx deadline 留出余量；若预算不足应调整测试专用的有限 deadline 组合并记录实际值，不能关闭 timeout 或把子 Tx 自行超时当成功的 SIGKILL 场景。

### normal_release：确认真实 Commit 出口

- 正确 release；必须取得原 child 成功 reply、确认实际 Wait 与 pipe cleanup，然后同时 reopen Decision/Source owners。
- 公开 Get 必须为原 completed，ProposalRef/准确内容/ArtifactRef 不变，原 GetCommand accepted receipt 不变，实际已知费用单位和金额、RuleStarts=1、RuleSteps=1、MeasurementsComplete 及本场景已知测量均正确。独立读回 Proposal 和 artifact 两者的原 bytes，不只检查 ref。
- normal 路径 completed.Usage 必须与第一门实际公开捕获的完整 `confirmedUsage` 相等，同时保留本场景已知 RuleStarts/Steps/Cost/测量完成断言。不得从第二门 unavailable 补一个零值 running。独立的 publication-before-Finish 故事也继续保留。
- 可再 Step 并确认 Processed=0，证明同事务的 completed 与 Job Complete 在公开行为上闭合；不得为检查 Job 而读私表。

### SIGKILL：确认实际回滚及原 prepared 恢复

- 在该门上实际 KillWait，确认真实 SIGKILL、无正常 reply、物理端点退出。若 child 已因 Tx deadline 自行失败，不能算该 SIGKILL 场景通过。保留双方 owner 的 borrowed handle/未知关闭责任。
- 同时 reopen 两个 owner，在任何恢复 Step 前公开 Get 必须 FoundRunning；其完整 usage 必须等于第一门实际公开捕获的 `confirmedUsage`，并独立读回此前准确 Source 内容。这个新观察证明已重开数据库的持久状态仍为 running，不能倒写成第二门曾可读。
- 按返回的原 LeaseUntil 有限等待后，恢复 Step 必须实际处理原工作；随后 Get completed、原 Proposal/Artifact refs 与 bytes、原 receipt、累计 usage 与此真实 running 基线相同，RuleStarts/Cost 不新增。不能新建 Decision 或重新计算来填补观察。
- 可再 Step 确认 Processed=0。保持正常对照与 SIGKILL 的对称收尾；不能只保留故障分支。

## 其它门不变，替代方案暂不采用

`published_before_finish` 的暂停在 Source 发布完成、进入 Finish Tx 之前，原公开 FoundRunning 及 usage 比较仍有意义，不能统一放宽为 unavailable。`completed_commit_before_reply` 的 hold 发生在 Core 已确认 Commit 返回之后，锁已释放；此时独立 Get 必须 FoundCompleted，不能接受 unavailable 以混淆 before/after 边界。普通 child 正常场景也继续要求正常公开结果。

单条 SQL 在同一 statement snapshot 中联合读取 Decision/Stop，理论上可提供不阻塞写者的内部一致快照；但需要新的准确消费方读端口/PG 实现，处理不存在对象、Stop 组合、解析验证、当前鉴权和数据库当前时间，以及并发控制正反例。它是将来有真实读可用性需求时的合法优化，不是本次故障的必要修复，也不自动比现有锁更正确。对不同表连续两次普通 READ COMMITTED 查询没有同一 snapshot 保证，明确不采用。也不以全局 SERIALIZABLE/REPEATABLE READ 改变现有 Tx 行为来修测试。

## 必须补齐的验证

实施方固定新 SHA 后重跑受影响四个真实 PG child 故事的 normal/race：普通正常、Source 发布后、实际完成 Commit 前、Commit 后 reply 前；每个已有 normal/kill 子例完整保留。报告实际 union、准确门与退出、两 owner reopen、原内容/receipt/usage/Job 行为，区分不确定窗口与后来确认。有关 cancel/Get 同一观察及当前鉴权既有检查不得删除。

当前红日志没有完成这些证据；本决定不追认其通过，也不宣称发生了 native Commit error、主机断电或 production 恢复。现有一致读取设计不变，改的是恢复故事中已失效的公开可见性假设；按授权可直接实施，无须再向用户例行确认。
