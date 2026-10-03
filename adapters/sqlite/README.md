# SQLite 设备原账本

`Open(path, options...)` 只打开持久文件，拒绝内存库。Store 实现 [runtime.Store](../../runtime/store.go)，不接纳或发送工作。迁移通过独立管理入口显式调用 `Migrate(ctx)`；构造不会竞相改表。

所有连接启用 WAL、FULL 同步及 foreign_keys。每个实例只有一个 database/sql 连接和有 context 取消的单写队列，写事务用 `BEGIN IMMEDIATE`；多个进程的写入由 SQLite 文件锁继续串行。等待不透明重跑领域闭包，不照搬 PG 行锁或 SKIP LOCKED。只读访问可能等待本实例的短写事务，宿主须限制事务时长。

迁移在一笔事务内登记 SQL 制品摘要、检查点与永久 database_id。业务 Scope 必须匹配该身份、受信 tenant/owner 及已声明 namespace 根。原 owner 恢复必须通过 `WithExpectedDatabaseID(id)` 核验受信目录中的原身份；新空文件或不同库不会被当作原账本接纳。这个检查不替代原设备介质、最近关闭/撤权记录与旧写者隔离的恢复证据。

当前头及准确历史分开保存，Create 不复用身份，Put 显式比较旧 revision。内部对象键及业务键最多 512 字节，允许 `content_id:version`；每条严格 JSON 最多 256 KiB，List 每页 1–1000 条且按 ID 稳定排序。Bind 的目标必须已在完全相同 namespace 创建，复合 FK 包含 tenant/owner，删除默认 RESTRICT。

原命令、纯数据库 SAVEPOINT 中的领域写入、回执和 Job 同库提交。业务拒绝回滚 SAVEPOINT 后仍保存固定 rejected 回执。墓碑永久占用原身份。Tx 不跨库、tenant 或 owner，不逃逸到 goroutine，也不包含外部调用；事务关闭后使用被拒绝。

命令与回执各自保留 256 KiB 的严格字节边界，保存两者的 `StoredCommand` 聚合封套上限为 1 MiB。独立 `003_command_envelopes.sql` 在管理事务中替换命令表的字节约束并保留原数据、复合键及 FK；已应用制品只核摘要，不重复替换，也不改旧迁移摘要。

`TxSnapshotReader.Peek` 仅提供加上游锁前的当前路由，调用方仍须用 `Get` 核原 revision 和当前资格。Guard 先核原 Claim 并登记，Within 在领域闭包结束后强核全部领取；Finish 已保护的原 Job 保持到提交。SAVEPOINT 回滚撤销其中的保护登记，实际提交前重新核数据库时间与原确认截止。SQLite 仍由本机单写队列和跨进程文件锁串行，无 PG 行锁假设。

时间裁决每次重新读取 SQLite 当前 UTC，不使用事务开始时间。Job 时间按准确纳秒保存，支持 1970 至 2261 年。Claim 保留领取观察值；续租不刷新它，未知续租不延长原确认截止。新 source_ref 的 Raise 推进工作版本，相同事实重传不会增加责任；Hint 不创建、不增版本、不重开 done。过期 worker 的受保护写入整笔回滚，新 Raise 不会被旧 done 或等待覆盖。

Within 的提交结果为 committed/rolled_back/commit_unknown；未知结果必须沿原命令/阶段查询。`WithCommitFault` 为管理验证提供 before/after commit 故障，不能作为普通业务配置。合同测试还在提交边界实际 SIGKILL 写者，以重开原库后的公开接口读回证明 record/receipt/Job 的原子集合。

Within 不自动重跑闭包。调用方只在确认回滚且没有外部行为时，重建全部捕获状态后有限重试；失败尝试的发送标记与输出不能充当提交依据。

`QueryBindingStore` 将原 query_id、主体、凭据代次、角色与准确查询摘要、结果摘要和首次截止保存为短期元数据，不缓存披露正文。TTL 为 1 秒至 5 分钟且不刷新；再次查询仍执行当前门禁，摘要变化返回 `query_snapshot_changed`。过期原身份可重用，旧生命周期不能封存新查询。`PruneQueries` 每次只清理本 tenant/owner 的 1–1000 条已过期绑定；宿主显式调用，没有隐式清理线程。查询迁移是独立的 `002_query_bindings.sql`，原迁移制品摘要保持不变。

生成执行 `sqlc generate`（v1.31.1）；[queries](queries/) 与 [migrations](migrations/) 是 SQLite 独立源，[gen](gen/) 为派生资产。`go test -race ./conformance/contract` 运行真实持久文件测试，CGO 与 GCC 为当前 go-sqlite3 驱动的构建依赖。

默认设备账本不裁决云端 Task，不提供断网接管。当前验证为本机文件；设备丢盘、文件系统掉电保证、磁盘保护、备份撤权完整性及可信时间异常需要对应真实平台实验。
