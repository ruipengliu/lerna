# 受信参考宿主

本包实现 [治理端口](../../internal/governance/ports.go)，只服务宿主明确预登记的静态内置组件；不执行上传制品，不声称具有不可信 native 代码隔离能力。授权、绑定当前代次和正式数据资格仍由业务数据库裁决。

`NewBuiltinHost(BuiltinHostConfig)` 需要独占用途的 Linux 私有目录、固定 tenant/owner、绑定真实主体的 `Content`、受信时钟、完整安装清单 allowlist 和有限 readiness TTL。准备核验准确制品字节，并进行真实私有文件写入、fsync、回读和删除。初始化先保存原 instance/generation 意图，再启动原实例并由它执行自检；就绪前没有模型或目标请求。Fence 等待实际句柄退出，进程丢失通过 `/proc` 原 PID/starttime 核对；同 ID 不创建替身。Dispose 先关准入，活实例保留 residual，最小终态 journal 保留以防复活。

`Content.Read/Publish` 都在数据库事务外调用；实现必须核验当前来源用途，并按准确 ID 幂等出版。证明只含摘要和绑定，`ProcessedSources` 保留实际引用，`DisclosedSources` 为空。目录需 mode 0700、当前进程 UID 所有。真实记录采用 Linux flock、文件 fsync、原子 rename 和父目录 fsync；`os.Root` 限定所有本地路径。关闭宿主前须调用 `Close` 收束原实例。

验证：`go test -race ./adapters/governance` 使用真实文件内容、实例句柄和 SIGKILL 子进程；测试核验准确 allowlist、拒绝错误代次、实际退出、活实例残留、崩溃后原 ID 不复活及死进程 fence。这只证明本机受信内置宿主范围。真实外部组件、跨主机进程接管和高影响校准没有配置时继续拒绝。
