# 04：运行并恢复有限任务

对应 [04 票](../../.scratch/harness-implementation/issues/04-worker.md)。Core 从 03 的原 decide 工作领取并占用额度，事务外调用脚本 Brain，将合法结果、COMPLETED、工作完成、记录与回执一起持久化。SDK/Get 可读取正式结果。该切片不执行模型、外部 API、GUI、记忆写入或子任务。

## 组装与运行

```sh
go run ./cmd/contractcheck -profile bounded-worker-v1
make verify
```

具名 profile 自动使用受限临时 SQLite 文件、显式策略与 Grant、受控时钟和脚本实现；输出 JSON，失败返回非零退出码。无外部服务依赖。宿主在 [03 的组装](03-durable-tasks.md) 上使用同一个受信时钟：

```go
limits := tasks.RunLimits{
    Lease: time.Second, RenewEvery: 200 * time.Millisecond,
    DecisionTimeout: time.Second, IOTimeout: time.Second,
    MaxAttempts: 3, MaxConcurrent: 1,
}
port, err := service.BindWorker(tasks.WorkerBinding{
    Token: credential, Subject: subject, WorkerID: "worker-a",
}, limits)
// 检查 err；凭据来自宿主，不来自 TaskRequest 或 Brain。
brain := tasks.BrainFunc(func(ctx context.Context, in tasks.DecisionInput) (tasks.Proposal, error) {
    return tasks.Proposal{
        Kind: "result", BaseVersion: in.Task.Version,
        Complete: true, Result: "脚本结果",
    }, nil
})
runner, err := tasks.NewRunner(port, brain, limits, clock)
// 检查 err；clock 与 authorization.New 使用同一受信时间来源。
candidate, err := service.Load(ctx, task.Ref)
// 检查 err。
result, err := runner.Run(ctx, candidate)
// 检查 err；正式结果以 result.Task 或有当前读取权限的 SDK/Get 为准。
```

安装管理员不自动具备执行业务的权限。主体须为原任务创建主体，具有资源根上的当前 `task.execute` 允许策略及 continuous Grant，用途 `task`、位置 `local`。`task.submit` 和 `task.read` 不足以执行。WorkerBinding 是受信宿主组装口，不是远程业务接口；允许的任务范围由固定的 namespace/resource/owner 和绑定 subject 限定，WorkerID 区分同一逻辑 Owner 内的实例。重启沿用逻辑 Owner；领取代次区分新的执行资格。

Brain 输入是占用本次额度之后的 Task、原 Work 摘要与剩余步数。只有 `Kind=result`、`Complete=true`、基线版本匹配、非空且不超过 64 KiB 的 UTF-8 结果可完成任务。结果判断证明契约闭环，不证明答案质量。未知类型、行动请求、部分结果、无效 UTF-8 与超大输出不会发布成功终态。

## 状态、资格与原子提交

| 操作 | 任务版本与工作变化 | 原子约束 |
| --- | --- | --- |
| claim | QUEUED/过期 RUNNING → RUNNING；版本 +1、Attempts +1；原工作 Generation +1。 | namespace、owner、epoch、版本、work_id、当前 generation；不存在有效租约；当前授权、期限与额度。 |
| renew | 保留任务版本和领取代次；延长租约，不超过任务期限。 | 当前 Worker、版本、代次、未到期租约和当前授权。 |
| complete | 合法提案 → COMPLETED；版本 +1、保存 Result、Work.Done。 | 当前授权、Owner/epoch、任务版本、当前 Worker/代次和期限；提案基线必须匹配。 |
| stop | 可重试失败 → QUEUED；等待条件 → WAITING；不可恢复且无外部待核对效果 → FAILED。 | 与当前领取相同的前提；不退还 Attempts。 |

TaskService、RunStore 与 WorkPort 共用 `authorization.UpdateRuntime` 的 SQLite CAS 快照。竞争会重跑事务回调及授权判断；Brain、唤醒与调度选择在事务外。没有仅存于队列或调度器的另一份工作终态。

`Service.Commit` 继续处理严格的初始任务创建，拒绝任意覆盖既有任务。执行推进使用绑定后的 `WorkPort.Commit(WorkChange)`；`WorkChange.Kind` 仅支持上表四类，并携带 Qualification。该受约束提交写入同一 RunStore 日志，`Service.Load/LookupCommit` 可核对它的回执。对外 TaskRequest 不增加状态、Owner、凭据或领取写入口。

合法续租更新元数据和记录，保持决策版本。领取、重新领取、完成、停止都会递增任务版本；旧版本、旧代次或旧 Worker 不能推进新事实。已完成工作与 WAITING 不出现在可执行候选中，终态不可重新领取。当前代次在任务期限已到时仅可落盘 deadline 停止，不能借此复活租约或发布迟到结果。

## 有限执行与停止原因

RunLimits 在第一次工作变更时持久固定；恢复端必须匹配，不能更换进程后扩大限制。Lease 为 100ms–1m，RenewEvery 为 1ms–Lease/3，DecisionTimeout、IOTimeout 均为 1ms–1m，MaxAttempts 为 1–3，MaxConcurrent 为 1–4。参考值见示例，超界配置拒绝。每任务内部提交最多 256 条；运行分区 8 MiB、共享快照 16 MiB 的既有容量限制继续生效，耗尽时返回不可用，不伪造持久状态。

MaxSteps 约束决策尝试，实际最多 `min(MaxSteps, MaxAttempts)` 次。额度在调用前持久占用，成功、失败、超时、崩溃以及无法确定是否开始的调用均不退还。一次 `Run` 处理一次尝试；新一轮有界调度可以领取 QUEUED 重试，不能无限重置重试次数。

| 原因 | 持久行为 |
| --- | --- |
| authorization | WAITING，返回当前授权错误；查询仍单独要求当前读取权限。受信运行口只落盘停止，不因拒绝授权而披露结果。 |
| budget / retry_limit | WAITING，自动运行结束。本票没有预算调整或用户恢复 API。 |
| deadline | FAILED；没有本票外部效果待核对。 |
| invalid_proposal / brain_failure | FAILED，正式结果为空。 |
| unavailable / timeout | 若仍有额度，QUEUED 等待后续有界尝试；否则 WAITING 并记录 budget/retry_limit。 |
| interrupted | 有界落盘 WAITING；调用者取消不阻止一次有限的停止尝试。 |
| 存储不可用、受信时间异常、未知提交 | 返回明确错误并停止新工作；未保存的状态不报告为 RUNNING/COMPLETED。保留已持久事实，恢复后核对。 |

Runner 在领取后、调用前再次续租校验当前资格，并对共享受信时钟与已检查时间、租约比较。调用有 context 时限和周期续租；返回后仍在存储边界复核授权、版本与期限。续租或停止结果未知时优先核对，不能额外落盘一个假失败。

Go 无法强行杀死不配合 context 的进程内 Brain。Runner 保证调用者有界返回、丢弃迟到结果，并在该 Brain 真正返回前保留并发槽，防止反复超时无限堆积调用。强进程隔离属于后续运行器工作。Store、Wakeup 和 Selector 是受信组件，必须遵守有界调用契约；宿主固定 Runner 数量，MaxConcurrent 是每个 Runner 的限制。

## 提交身份、恢复与兼容

每次领取、续租、完成有稳定 change_id。提案的类型和原始结果字节以 SHA-256 摘要参与语义核对，日志不保存被拒绝的大载荷；只有正式完成结果进入 Task。相同身份与语义重试返回原提交快照和回执，先于新状态版本比较；语义、绑定 Worker 或配置改变产生身份冲突。`WorkPort.Lookup` 查询原提交快照，重新验证当前执行权限；`RunStore.LookupCommit` 保留受信内核核对入口。

Runner 遇到 OUTCOME_UNKNOWN 或提交调用取消/超时，执行一次有界 Lookup。仍无法确认时返回 `PendingCommit`，保留完整原 WorkChange；`Runner.Reconcile` 仅核对该身份，不调用 Brain，不创建新工作。一次 NotFound 不是“原提交永远不会成功”的证据。持有原请求的宿主可继续核对，或以完全相同的请求重发；不要生成新的身份猜测结果。

进程退出后从原文件 Load/LookupCommit/ListRecoverable。已接纳结果直接恢复；未准入提案可在原租约过期、仍有额度时重领原工作并重新决策。Brain 调用不承诺恰好一次。候选使用有界 keyset 分页；`Runner.Dispatch` 接受独立的 RecoverySource、Selector 和 Wakeup，每次只处理一次唤醒和一页候选，筛选结果不得包含页外引用或重复引用。并发新增在游标之前的任务由下一轮从头扫描发现。

私有 gob 运行分区保持 Format=1，新增 Attempts、结果、停止原因、RunLimits、工作租约及回执扩展字段；03 数据缺失字段自然为零，第一次领取建立限制与代次，不删除旧数据库。固定测试资产 `profiles/worker/testdata/03-runtime.gob` 由修订 `4e88885` 的原实现生成，SHA-256 为 `f1d1e6560f2fd864e0702b882ec4846f2b8717fee71cb2a5b4ef3796ce984be6`。升级检查将原分区装入真实 SQLite 原子快照，执行旧任务，重开文件后核对原 work_id 与创建回执。旧二进制写入降级仍不支持。

TaskSnapshot 新增 attempts/result/stop_reason，SDK 接受本票状态和递增版本，现有 Submit/LookupOperation 去重与当前权限继续生效。01–03 既有历史报告保持不变；当前 durable-tasks-v1 只验证接纳，将执行标为 not_run，并指向独立的 bounded-worker-v1。

## 验证范围

`bounded-worker-v1` 包含 22 项契约检查及 4 项真实子进程检查。覆盖正式结果与重开文件、独立数据库句柄争用、续租、过期重领、旧提案、版本和所有权、非法提案、预算与期限、授权撤回竞争、存储和时间异常、未知回包、超时并发、冻结配置、调度与临时运行接口替换及 03 数据升级。

子进程在 before-claim、after-claim、proposal-before-admission、completion-lost-reply 处以 exit(73) 结束；父进程等待退出后重开同一文件，核对原提交、任务、工作、额度和结果。每个子进程最长 10 秒。这不是断电、磁盘损坏或敌对宿主证据。

临时内存运行接口只验证基础共同契约，其固定认证身份是测试条件；不计为第二授权实现或第二耐久后端。脚本策略替换不计为真实模型替换。完整答案、用户控制、远程 Worker 与端云协议仍按后续票验收。

## 验收记录

2026-09-10 在 linux/amd64、Go 1.26.1、protoc 36.1、modernc.org/sqlite v1.58.0（SQLite 3.53.4）执行 `make verify`。实际验证修订为 `eff59e3ae73cca40074e3dd043c7ee3816750cdb`，构建时工作区 clean；本记录和证据随后提交。

- [阶段报告](evidence/04-verification-stages.json)：依赖、生成一致性、编译、go vet、全量 `go test -race ./...`、SDK 样例及四个 profile 全部通过。
- [Worker 报告](evidence/04-bounded-worker-report.json)：26 项必需检查通过，其中 4 项为真实 SQLite 子进程中断恢复。
- [SDK 回归](evidence/04-sdk-regression-report.json) 32 项、[授权回归](evidence/04-auth-regression-report.json) 30 项、[持久任务回归](evidence/04-tasks-regression-report.json) 13 项必需检查通过；不将其他声明项计为已验证能力。
- 目标范围测试另验证延迟领取回包不能启动过期决策、内部创建不能伪造运行事实、9 MiB 无效提案可持久停止及原身份重放/语义冲突。拒绝的大提案不占据持久日志空间，旧版本的非法提案不能推进任务状态。
- [双轴审查](04-bounded-worker-review.md)：Standards 0 项；Spec 1 项 P2，修复后经原审查者复核关闭，未解决 0 项。
