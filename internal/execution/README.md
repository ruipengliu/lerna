# Execution 参考实现范围

领域合同依据 [Execution 设计](../../docs/architecture/execution/README.md)、[存储落位](../../docs/architecture/data/storage.md)和[实施规格](../../.scratch/full-implementation/spec.md)。本包只拥有 Executor 的本机事实；设备 SQLite 不裁决云端 Task 完成。

## 已实现的边界

- `execution.invoke/cancel/control/reconcile/get/list/control.get/usage.get` 使用闭合请求与响应 Schema。原 Operation 输入不可变；取消先到保留同身份墓碑。`accepted` 只代表持久责任，独立的 Attempt、Effect、停止和费用事实由可信驱动归并。
- 当前 TaskGate 与有限 StartWindow 分别存储。开始前准备原 use、approval、control 的固定依据；真实入口的短事务只检查准确签名依据、当前本机 gate、resource epoch、environment generation 和最早截止。事务不取网络授权；云端同库 authority 可通过声明的 participant 检查本库 Grant。
- StartBarrier 先保存 `possibly_sent` 与原查询责任，确认提交才允许物理出口。提交未知、丢回执和已越过入口的恢复都只查询原目标。未知效果保持资源占用；取消和终态不丢弃迟到事实、争议或费用。自动原查询最多十轮，随后保留事实并要求显式核对。
- `resource.acquire/renew/release/takeover/get/observe/observation.get` 维护独立的控制 epoch、实例、有限 lease、准确观察窗口和实际停止事实。接管先推进 epoch，旧观察与旧实例不能越过驱动入口。
- `environment.create/get/list/stop/destroy/checkpoint/restore` 管理被动命名空间、有限字节额度、实例及代次。checkpoint 只含已提交数据；restore 不恢复旧栈、句柄、socket、授权或物理出口。迟到旧实例回调不能改变恢复后的新实例。
- 受信 `TrustedComputeDriver` 只解释闭合的 `add_decimal/concat/not/copy` 纯计算指令，输入和代码均为准确 ContentRef。输出、命名空间 head 和 Operation 结果一起 CAS 提交；旧查询不重新计算。停止等到实际运行退出才报告退出。
- 受信宿主在父 Operation 事务中分配 hostcall position，分别映射 `operation/decision/delegation` 的原命令、回执和目标。可能已送出的恢复只 Resolve 原命令；未知映射阻止下一 cell，环境关闭保留未退出子责任。默认没有 HostCallPort，未配置时在新责任产生前拒绝。
- Operation 和 Environment 列表返回持久的、有期限且绑定租户、主体、凭据代次与 collection revision 的 cursor。集合变化要求重读 snapshot，不混合两代快照。
- `execution.usage.get` 按固定 CostBound 的每个单位报告准确累计值，免费能力和尚未入口的责任也有明确的零值。独立 `UsageProof` Content 固定原 Operation/Attempt 修订、累计值、封闭及费用最终事实、send_started 次数与物理请求计数范围；未知不被填成确定零次。ProofRef 与全 Snapshot（先置 `usage_digest=""`）共同参与摘要，同一版本查询不改证明身份。没有原意图或固定费用单位的责任返回 `accounting_unknown`，不猜单位或最终性。

## 宿主装配端口

`ContentPort.ReadBytes/Publish` 位于消费方；宿主桥接准确内容权利、来源闭包和字节摘要。`AuthorityPort.PrepareStart` 在事务外取得 `PreparedStart`，`VerifyStart/VerifyControl` 在事务内验真，不得在设备 Tx 中 RPC 云端或读第二库。Prepare 和 Verify 均使用 `StartRequest.ControlWindow`，不能以原 Invoke 中过期窗口刷新原 use 或 intent。Authority 还须检查准确 BindingRef、InstallLock、当前批准、原 recipient 与目标配置，不能仅凭同租户判断可用。

驱动的 `Start` 必须在真实物理入口调用一次 barrier。`Prepare/Reconcile` 不能创建新的业务出口；`Stop` 返回实际退出依据。领域没有默认授权成功分支。

## 原生目标及验证

`adapters/execution.ManagedFiles` 使用 Go `os.Root` 根句柄、生命周期单宿主锁、逐段拒绝 symlink、拒绝 hardlink、前版本 CAS 和稳定原 journal。临时文件身份由设备号/inode 固定；真实路径替换采用 file fsync、rename、directory sync。未知原 journal 对该路径保持占用。独立读取重新打开目标并核对真实字节与来源关联，不以写缓存证明成功。

`SimulatedPhones` 将至少三台独立有状态手机保存在真实文件目标，执行 observe → action → observe → verify。epoch、原 Attempt 和状态一起持久化；旧观察、跨租户与旧实例拒绝。模拟行为是参考驱动验证，不是 Android/iOS 真机支持证据。

公开 dispatcher + 真实 SQLite/PostgreSQL 的验收位于 `conformance/integration/execution_test.go`；原生真实目标验收位于 `adapters/execution/*_test.go`。覆盖原写/独立读回、重启取消墓碑、丢写回执、入口撤权、提交未知、旧 worker 接管、独立新窗口、迟到效果、三台设备接管、被动 checkpoint/restore、受信计算 CAS、hostcall 丢接纳回执和 snapshot cursor。

## 明确未开放的能力

- 不可信程序 `environment.run_cell` 可通过 [选定 Linux WASI adapter](../../adapters/wasi/README.md)显式配置开放：wazero v1.10.1 解释器、真实 bwrap/prlimit 探针、独立进程与准确 InstallLock，成功完整 namespace 与原 Operation 同事务 CAS。未配置或平台探针不合格时保持 `unsupported`；其他平台/原生程序/自定义 guest hostcall 不由该 profile 声明。受信纯计算仍有独立 Capability。
- 本次发布的驱动均 `MaxAttempts=1`。多物理 Attempt 的目标幂等、有限安全重试与累计费用合同未验证，构造时明确拒绝 `MaxAttempts != 1`；查询原 Attempt 不算重发。
- 真机、生产外部工具、跨设备恢复、Windows 或其他未探测文件系统、断电稳定性、规模与跨 AZ 容灾没有本轮证据。当前 native 文件合同只由本机 Linux 文件系统的真实 fsync/rename/reopen 探针支持。
- 一个任务门禁的窗口和一次控制批次的 Operation 扫描限额为 100。达到限额时新开始或控制批次返回 `overloaded`；`control.get` 明示 `windows_complete=false` 与缺口，不能称全集。hostcall、命名空间 binding 与活动设备也有闭合的 100 项额度；cursor 同类最多 100 个同时有效。
- 没有配置真实 HostCallPort、内容授权桥或 authority proof 时，相应入口保持关闭；测试的受信 fixture 不替代生产身份提供方。
