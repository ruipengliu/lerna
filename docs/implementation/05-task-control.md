# 05：暂停、取消和恢复任务

对应 [05 票](../../.scratch/harness-implementation/issues/05-control.md)。新增持久控制请求、稳定接纳回执、实际落实进展与在途处置。任务主状态、RUN/PAUSE/CANCEL 意图和等待原因分别保存；暂停使用 WAITING，不新增 PAUSED 主状态。

## 组装和验证

```sh
go run ./cmd/contractcheck -profile task-control-v1
make verify
```

在 [04 的宿主组装](04-bounded-worker.md)上增加控制服务与托管本地绑定：

```go
controlLimits := tasks.ControlLimits{
    MaxOperations: 32, MaxObservations: 32, MaxChecks: 4,
    PollInterval: 10 * time.Millisecond,
    StopTimeout: 30 * time.Millisecond, IOTimeout: time.Second,
}
controls, err := service.Controls(controlLimits)
// 检查 err；service 与 Worker 使用同一 RuntimeStore 原子边界。
transport := tasklocal.BindManaged(service, controls, credential)
client := sdk.NewTaskClient(transport, "local")
controlClient := sdk.NewControlClient(transport, "local")
operationID, err := authority.NewOperation(ctx, credential)
// 检查 err，保存原身份与语义供未知回包时核对。
receipt, err := controlClient.Request(ctx, tasks.ControlRequest{
    OperationID: operationID, Ref: task.Ref,
    ExpectedVersion: task.Version, Intent: "PAUSE", Reason: "用户暂停",
})
// receipt.Outcome 描述原操作接纳结果。
// client.Get 读取当前 Control.Progress、Control.Intent、WaitingReasons。
// controlClient.Lookup(ctx, operationID) 核对原不可变回执。
```

PAUSE、CANCEL、RUN 分别要求当前 `task.pause`、`task.cancel`、`task.resume`。效果或控制处置来源核对单独要求 `task.reconcile`；安装管理员、task.submit/read/execute 均不自动授予这些权利。沿用原主体、资源根、用途 task、位置 local 的策略与 continuous Grant。SDK/Get 仍要求当前 task.read；控制回执 Lookup 还检查原操作对应的当前控制权限。

`Bind` 保持 03/04 接纳绑定，未组装控制服务时控制请求返回 UNSUPPORTED；`BindManaged` 提供完整本地二进制绑定。本票没有远程 gRPC/WebSocket 或 UI 页面，SDK 提供上层呈现所需字段。

## 接纳与实际落实

| 观察面 | 含义 |
| --- | --- |
| 调用尚未得到接纳回执 | 调用方显示待确认；超时不能推断请求未被接纳，须核对原 operation_id。 |
| ControlReceipt.Outcome=accepted | 原控制意图已持久接纳；回执包含原版本与目标，后续控制不改写它。 |
| Control.Progress=ACCEPTED | 当前控制已受理、仍待在途处置或核对；不是已停止。 |
| Control.Progress=APPLIED | 当前控制已落实；RUN 的落实只表示解除暂停并重判条件，不保证任务可以执行。 |
| Control.Progress=NOT_PREVENTED | 可信来源证明既有工作已在控制生效前完成，保留完成结果并说明未能阻止。 |
| already_COMPLETED / already_FAILED / already_CANCELLED | 对终态新请求返回未改变任务的结果（即使提交时版本已旧），不重新打开任务。 |

原操作重放先于新版本和终态判断，并重新检查当前权限。改变非终态的新操作要求当前任务版本；终态仅记录 already_* 回执，保持结果与版本，避免完成先提交时把迟到取消误报为普通版本冲突；同一操作改变意图、目标、版本或原因返回身份冲突。任务提交、控制和授权管理共享 operation_id 占用，不能跨类型重用。控制记录独立于短期授权管理回执清理保留，接纳窗口结束后原控制仍可核对。

控制接纳把意图、任务版本、控制记录、处置状态和内部回执纳入授权/运行 CAS 快照。授权撤回或工作更新先提交时，竞争方重新读取并核验。拒绝恢复取消、非法版本等失败不会留下部分身份占用。

同意图的新请求可以产生新的接纳回执，当前 OperationID 指向后续控制；重复 PAUSE/CANCEL 不推迟首次同意图的 `AcceptedAt` 生效边界。该时间用于判断先前完成证据，不能被重复取消推迟。RUN 只能解除 PAUSE，不能撤销 CANCEL；非暂停任务的新 RUN 请求拒绝。终态拒绝状态更新，原回执仍可查询。

## Worker 与在途工作

Worker 在领取后持久记录 start 边界，再在事务外调用 Brain。claim 占用尝试额度并固定决策版本；start 标记本轮可能在途，保持决策版本。控制变更递增任务版本，阻止旧提案准入、新领取、重领和新的目标调用。持久意图是权威，Runner 的周期查询只用于发现变化。

领取但尚未 start 的工作可以直接暂停或取消。已 start 的工作必须确认实际结束；标记 start 与真正进入调用之间发生崩溃也按可能在途处理。旧版 RUNNING 数据缺少该边界时采取同样保守处理。

Runner 发现控制后取消调用 context，在 StopTimeout 内等待实际返回。配合取消的 Brain 返回后记录 STOPPED；不配合时记录 UNKNOWN 与 WAITING/reconciliation，保留在途标记，不能宣称 APPLIED。实际返回后，晚到观察者记录停止，丢弃脚本提案。原调用真正返回前保持其并发槽，不因发出取消信号或停等超时而提前释放。

脚本观察身份由原工作、Owner/epoch、领取代次、决策版本与观察类型确定。`tasks.ScriptObservation` 可重建该身份供 LookupCommit 核对；构造观察值本身不证明工作已停止。未知观察提交返回 PendingObservation，包含原请求；晚到观察的回包丢失也保留可重建身份。

进程内 Go 代码不能被强制杀死。迟到观察者随已存在的有限在途调用等待，不启动额外目标工作；失败的观察不能被当作落实成功，宿主继续从持久待核对状态恢复。

## 有限处置与可信事实

`WorkPort.PollControl/ReserveCheck/Observe` 与独立 `DispositionSource.RequestStop/Inspect` 构成处置边界。宿主将来源绑定到原 WorkerID 和主体，检查原 WorkID、代次、决策基线、Owner/epoch；普通 SDK 不暴露这些内部写入口。旧领取可以提供原工作事实，但不能据此启动新目标动作或任意推进新领取。

`ReconcileDisposition` 每次先持久占用一个 MaxChecks 处置尝试，再于事务外请求停止、核对来源，最后提交观察。调用方提供稳定的预约 change_id；预约回包未知时先核对原身份，不调用来源。重复已预约身份不会再次调用来源，崩溃后的不确定尝试不退还，需要新尝试时使用剩余处置额度。观察回包未知保留原 PendingObservation，重放返回原结果。

| 来源事实 | 处理 |
| --- | --- |
| UNKNOWN、停止/核对超时或不可用 | WAITING/reconciliation，保留在途工作与未落实进展。 |
| STOPPED | 清除在途核对等待；PAUSE 落实为 WAITING/pause，CANCEL 落实为 CANCELLED，RUN 重判其他等待。 |
| COMPLETED 且有已验证的先前完成证据 | 显式启用效果证据的可信来源可确认完成；结果、证据引用和完成时间有界且满足生效边界，状态为 COMPLETED/NOT_PREVENTED。 |

`WorkerBinding.AllowEffectEvidence` 由受信宿主显式配置并在领取时固定；默认脚本来源不能把未准入提案升级为效果事实。启用该能力的 Adapter 必须自行验证证据，本票只使用可控来源夹具，不宣称真实 API/GUI 可取消或效果已验证。迟到事实不能覆盖终态、新领取或错误来源；同观察身份不同语义被拒绝。

`ControlService.ListPending` 包含尚无用户控制意图的在途工作，通过有界页和 task_id 游标发现本主体待处置工作，支持通知丢失与进程重启。它不授予目标执行资格；处置完成与任务终态仍由共享持久状态判定。

普通超时、续租失败或存储异常退出也保留脚本实际返回的观察者；无效果能力、无用户控制的 STOPPED 可由原绑定以当前 task.execute 确认，不授予效果核对或用户控制权限。核对入口优先接受 task.reconcile，即使 task.execute 已撤回也可确认停止并保留授权等待。该兼容路径每任务最多记录 MaxAttempts 次停止观察；有预算且实际返回后才能重试，原提案丢弃。期限到达而工作仍在途时保存 WAITING/deadline/reconciliation，不生成失败终态。

start 后崩溃的宿主先 ListPending，再以当前 task.reconcile 调用 `controls.PrepareDisposition(ctx, credential, ref)` 固定有限处置配置，随后通过原来源执行 ReconcileDisposition。该准备操作不改变任务版本、意图或目标额度，可幂等重做；不需要伪造一次 PAUSE/CANCEL。原来源确认停止后才允许有限重试；进程退出本身不能证明远程效果停止。

## 恢复与限额

恢复只移除暂停原因。已有预算等待、授权等待、期限和 reconciliation 继续保留；执行授权会重新核验，Attempts、未知占用与固定 RunLimits 不清零。RUN 已受理但旧工作仍在途时，继续等待该工作处置；不能立刻重新决策。

ControlLimits 在本地权威首次控制接纳或 PrepareDisposition 时固定，各任务保存其处置限制。操作和观察各为每任务 1–128 条，处置检查每任务 1–32 次，PollInterval 为 10ms–1s，StopTimeout/IOTimeout 为 1ms–1s。首次控制尚未出现时 Runner 用不超过 10ms 的发现周期；已有任务控制配置时采用不慢于续租周期的配置值。实际响应还受有界存储调用耗时约束。

处置不扣目标 MaxSteps，也不能用于生成新目标工作。操作、观察或处置容量耗尽返回不可用并保留当前事实，不制造 CANCELLED/FAILED。既有 8 MiB 运行分区、16 MiB 共享快照与目标工作记录上限继续生效，记录清理和归档不在本票。

Store 与处置 Adapter 必须遵守 context 时限；参考 profile 验证有界超时源。用户控制接纳不依赖目标预算，必要处置有独立有限预算，处置耗尽后仍保持待核对。

## 数据与协议兼容

新增控制记录、结构化等待原因、start/在途元数据和观察日志，私有运行分区仍可读取 03/04 的 Format=1。没有控制字段的旧任务按 RUN 理解。旧 WAITING 的单一 StopReason 在控制处理中保留为等待原因；旧 RUNNING 在控制时保守标记待核对，不以缺少新字段证明没有在途工作。

`profiles/control/testdata/04-runtime.gob` 由 `6395a00` 原实现生成，包含旧 RUNNING 和预算 WAITING。SHA-256 为 `1e58f437139dad584501cf009e7529fea4b1ed8a462a3613386b8b7cc275898a`。验证将旧分区装入真实 SQLite，控制与重开文件后核对原身份、预算和等待。03 升级路径继续由 04 的固定旧分区回归覆盖，不删除数据库升级，也不支持旧二进制写入降级。

Protobuf 新增控制请求、不可变回执、控制状态与等待原因，SDK 接受 CANCELLED 并验证固定消息、相关性及有界字段。旧接纳绑定对新控制操作明确 UNSUPPORTED；历史验收报告不改写。

## 验证范围

`task-control-v1` 包含 26 项契约检查与 5 项真实 SQLite 子进程中断检查，覆盖权限撤回竞态、控制/领取/完成竞态、稳定回执、窗口清理、在途停止、未知效果、有限处置、旧数据升级及跨主体拒绝。

子进程在 after-start、before-control、after-control、before-settle、settle-lost-reply 退出。before-settle 夹具先保存独立来源的 STOPPED 状态，再在控制器落实前退出；重启读取原来源事实并核对原观察。它是模拟来源与进程故障证据，不替代真实外部效果、断电或磁盘损坏验证。

after-start 在子进程持久 start 后退出，重开原 SQLite 后通过独立来源停止事实恢复同一工作，并完成第二次有限尝试，全程无用户控制请求。

目标范围 Go 测试另验证普通超时实际返回后的成功重试、期限未知效果仍可接收可信完成、完成先提交后的旧版本取消回执、重复取消不移动生效边界、未知观察提交携带原身份、迟到脚本结果不发布及恢复保留预算等待。完整验证与双轴审查见下文。


## 完成与验证记录（2026-09-10）

实现提交 `25f3abf`，恢复与 SDK 修复 `3d944ba`，执行权限撤回后的收尾修复 `849fc86`。最终 make verify 全部通过：依赖校验、生成一致性、编译、go vet、全量 race 测试、SDK 样例及所有 profile。

- [05 控制报告](evidence/05-task-control-report.json)：31 项必需检查（26 项契约、5 项真实 SQLite 子进程恢复）。
- [验证阶段](evidence/05-verification-stages.json)：全部阶段 passed。
- 01–04 回归：[SDK 32 项](evidence/05-sdk-regression-report.json)、[授权 30 项](evidence/05-auth-regression-report.json)、[任务 13 项](evidence/05-tasks-regression-report.json)、[Worker 26 项](evidence/05-worker-regression-report.json)。
- [双轴审查](05-task-control-review.md)：Standards 1 项 P3、Spec 2 项 P1 和 1 项 P2，均修复并由原审查者复核关闭。

报告对应 `849fc867e93c13b42d7fb8c76fe4e48e83507c66`，环境 Go 1.26.1、linux/amd64、SQLite 3.53.4。构建 dirty=true，源于验证时保留的无关 docs/architecture/README.md、skills-lock.json 修改；本票代码已提交，历史报告未覆盖。后续只提交完成文档与原始报告，不将本票夹具解释为真实 API/GUI 或整个系统验收。
