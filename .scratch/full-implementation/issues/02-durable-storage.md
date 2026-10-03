# 02 durable-storage

Status: resolved
Blocked by: 01

依据 [实施规格](../spec.md) 与根 AGENTS.md，保留准确身份、负责方、事务、门禁和恢复。实际编译、公开接口行为、正反例及所需平台证据均通过后才关闭。

## Comments

Implemented in 4955a15、1cd1df9、257a58d 及本工单最终恢复 guard 提交；首三项已集成 code-dev。独立 PG/SQLite migration、sqlc CRUD/历史查询、tenant/owner/database/participants 校验、原命令行锁与 SAVEPOINT/回执原子提交、语义唯一键、Job/Claim/提交三态均通过公开 seam 验证。

已运行 `go test -mod=readonly -race -count=1 ./adapters/internal/durable ./adapters/sqlite ./adapters/postgres ./conformance/contract`、相同包的 vet/build、`sqlc generate` 无派生漂移、gofmt 与 diff 检查。PG 用实际 17.11 服务，SQLite 为 go-sqlite3 v1.14.52（内置 SQLite 3.53.4）的真实 WAL/FULL/FK 持久文件。PG 通过 HARNESS_TEST_POSTGRES_DSN/PGPASSWORD 命令环境注入；凭据未写入工单、代码或日志。

行为证据覆盖 16 次同命令并发唯一裁决、业务拒绝回滚 SAVEPOINT 后固定 rejected、原版本/键/范围与有界关系、旧 Finish 与新 Raise（含同事务 Raise）、续租保原 observed revision、Guard 后提交前暂停过期的整笔回滚、Hint 不重开 done、16 个并发 Claim 独占、PG SKIP LOCKED 绕开持锁工作、未知 Claim/Renew 原 tuple 独立核验、永久墓碑跨重启拒绝、before/after commit reply loss 的 record/receipt/Job 原子集合，并在两种数据库上实际 SIGKILL 写者后重新打开原库观察同一集合。进程 helper 本身是测试驱动，不是独立能力通过项。

PG 提供 WithMaxConnections(1..128)，便于宿主声明非零独立控制/收尾池；两种 Store 的 WithExpectedDatabaseID 从受信目录核验原库，拒绝用新空库恢复既有 owner。新部署管理迁移与恢复模式区分见 adapter README。

集成强化 `ef2ae435` 增加短期 QueryBinding metadata 的独立迁移/有界清理、路由用 TxSnapshotReader.Peek，以及 Guard 非锁定预检→领域闭包后 sorted Job 强核→actual commit 前 fresh clock。SAVEPOINT 回滚恢复领取/完成登记。两个库的命令聚合封套独立 1MiB 严格 codec 与 `003_command_envelopes.sql` 保留普通领域/单命令/单回执 256KiB 上限及旧迁移摘要。真实两库整套 `go test -race ./conformance/contract -count=1` PASS10.176s；PG旧反序曾 RED40P01→GREEN，回滚 SAVEPOINT Finish 后竞争完成使原受保护业务回滚，大原命令/回执重开仍保原决定。Within 不自动重跑捕获状态；仅调用方重建纯闭包后可有限重试确认回滚。证据 `storage-hardening-verification.json` 位于宿主开发环境目录，绑定该提交与全部迁移/Schema摘要。

验证绑定 Go 1.26.8、sqlc 1.31.1、profile architecture-2026-10-data1，Schema SHA256 `168c24b7f7a29b4e64b5e35fedb76333b9b259920dd51f2de4f4fdb4d8742eaf`。记录/业务键最多 512 字节，JSON 最多 256 KiB，List 1–1000，Job 准确 UTC 纳秒支持 1970–2261 年；测试集合每例使用独立随机原 ID，不依靠 mock 数据库。

生产三 AZ 同步耐久、旧主隔离、UTC 异常处理、文件系统掉电/丢盘、磁盘保护、真实生产扫描规模、备份撤权完整性及 RPO/RTO 不在本机证据内，仍需对应平台验收；数据库合同通过不开放任何未完成领域方法。
