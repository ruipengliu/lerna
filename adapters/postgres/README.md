# PostgreSQL 原账本

`Open(ctx, dsn, options...)` 使用 pgx 驱动及 sqlc 生成的显式 SQL，返回实现 [runtime.Store](../../runtime/store.go) 的 Store。构造不会迁移、发送请求或接纳工作。

新部署由管理入口显式调用 `Migrate(ctx)`。迁移以数据库事务及管理锁保存制品摘要、检查点与永久 `database_id`；再次执行必须匹配同一制品。业务 Scope 的 DatabaseID 使用此身份，不使用连接串摘要。

已有 owner 恢复时，受信部署目录必须固定原 `database_id`，通过 `WithExpectedDatabaseID(id)` 校验。空库、不同库或损坏身份返回 `invalid_state`；不得重新迁移空库后继续原 owner。数据库身份检查不替代隔离旧主、备份完整性或最近撤权记录检查。

`WithMaxConnections(n)` 为每个实例声明 1–128 的独立池，默认 32。宿主应分别 Open 控制、收尾及普通工作池；相同 DSN 仍读取同一个原数据库身份。普通积压不能占用控制池。连接队列与每笔操作的 context 必须有明确超时。

事务按 tenant/owner/database 隔离，只能访问已声明 namespace 根。记录当前头与不可变历史分开，修订从 1 开始；内部记录键及业务键最多 512 字节，允许内容准确版本等复合键。Bind 的目标必须已在完全相同 namespace 创建，复合 FK 包含 tenant/owner。List 返回 1–1000 条稳定 ID 顺序记录；每条 JSON 最多 256 KiB。完整关系遍历使用所有页，不能把一页视为全集。

原命令键在领域写入前加锁，业务 SAVEPOINT、固定回执和新增 Job 共同提交。领域当前头使用行锁；调用方必须遵守原命令→领域门禁→Job 的设计锁序。领取使用有限批次 `FOR UPDATE SKIP LOCKED`，裁决时刻每次读取 `clock_timestamp()`。Job 时间保存为准确 UTC 纳秒，范围为 1970 至 2261 年。

Claim 的 holder/epoch/observed_work_revision 保持准确；Renew 不更新原观察值。新来源事实 Raise 递增 work_revision，相同 source_ref 重传不递增。旧 Finish 在新版本到达后保存合法原事实并释放为 ready；过期领取使整笔受保护写入回滚。Hint 不创建责任、不增版本、不重开 done。

Within 返回 committed/rolled_back/commit_unknown。连接中断等未知提交按原命令或阶段核验；不能从错误推断失败。未知 Claim/Renew 仅能以保留下来的原候选进行 CheckClaim，不拼造新 Claim。原确认截止不会因未知续租自动延长。

生成查询执行 `sqlc generate`（锁定 v1.31.1），源为 [queries](queries/) 和 [migrations](migrations/)，生成物位于 [gen](gen/)。PG/SQLite SQL、锁、时钟及提交实现分别维护，共享包仅包含纯合同校验。

真实数据库验证运行 `go test -race ./conformance/contract`；通过 HARNESS_TEST_POSTGRES_DSN 和 PGPASSWORD 提供测试库。未提供 PG 测试库时测试明确 skip，不能把 SQLite 通过算作 PG 通过。合同涵盖重复命令、历史/业务键/范围、Job 竞争、提交回复丢失及在真实提交边界 SIGKILL 写者后重开原库。

当前证据为本机 PG 17.11；三 AZ 同步耐久、主库隔离、UTC 异常处理、生产容量、磁盘保护与 RPO/RTO 需要另行运行验收。
