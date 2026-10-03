# SQLite 设备原账本

`Open(path, options...)` 只打开持久文件，拒绝内存库。Store 实现 [runtime.Store](../../runtime/store.go)，不接纳或发送工作。迁移通过独立管理入口显式调用 `Migrate(ctx)`；构造不会竞相改表。

所有连接启用 WAL、FULL 同步及 foreign_keys。每个实例只有一个 database/sql 连接和有 context 取消的单写队列，写事务用 `BEGIN IMMEDIATE`；多个进程的写入由 SQLite 文件锁继续串行。等待不透明重跑领域闭包，不照搬 PG 行锁或 SKIP LOCKED。只读访问可能等待本实例的短写事务，宿主须限制事务时长。

迁移在一笔事务内登记 SQL 制品摘要、检查点与永久 database_id。业务 Scope 必须匹配该身份、受信 tenant/owner 及已声明 namespace 根。原 owner 恢复必须通过 `WithExpectedDatabaseID(id)` 核验受信目录中的原身份；新空文件或不同库不会被当作原账本接纳。这个检查不替代原设备介质、最近关闭/撤权记录与旧写者隔离的恢复证据。

当前头及准确历史分开保存，Create 不复用身份，Put 显式比较旧 revision。内部对象键及业务键最多 512 字节，允许 `content_id:version`；每条严格 JSON 最多 256 KiB，List 每页 1–1000 条且按 ID 稳定排序。Bind 的目标必须已在完全相同 namespace 创建，复合 FK 包含 tenant/owner，删除默认 RESTRICT。

原命令、纯数据库 SAVEPOINT 中的领域写入、回执和 Job 同库提交。业务拒绝回滚 SAVEPOINT 后仍保存固定 rejected 回执。墓碑永久占用原身份。Tx 不跨库、tenant 或 owner，不逃逸到 goroutine，也不包含外部调用；事务关闭后使用被拒绝。

时间裁决每次重新读取 SQLite 当前 UTC，不使用事务开始时间。Job 时间按准确纳秒保存，支持 1970 至 2261 年。Claim 保留领取观察值；续租不刷新它，未知续租不延长原确认截止。新 source_ref 的 Raise 推进工作版本，相同事实重传不会增加责任；Hint 不创建、不增版本、不重开 done。过期 worker 的受保护写入整笔回滚，新 Raise 不会被旧 done 或等待覆盖。

Within 的提交结果为 committed/rolled_back/commit_unknown；未知结果必须沿原命令/阶段查询。`WithCommitFault` 为管理验证提供 before/after commit 故障，不能作为普通业务配置。合同测试还在提交边界实际 SIGKILL 写者，以重开原库后的公开接口读回证明 record/receipt/Job 的原子集合。

生成执行 `sqlc generate`（v1.31.1）；[queries](queries/) 与 [migrations](migrations/) 是 SQLite 独立源，[gen](gen/) 为派生资产。`go test -race ./conformance/contract` 运行真实持久文件测试，CGO 与 GCC 为当前 go-sqlite3 驱动的构建依赖。

默认设备账本不裁决云端 Task，不提供断网接管。当前验证为本机文件；设备丢盘、文件系统掉电保证、磁盘保护、备份撤权完整性及可信时间异常需要对应真实平台实验。
