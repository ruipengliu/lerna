# 09: PG 存储命名范围的锁隔离

**What to build:** 开发者在同一个 PostgreSQL 数据库中使用隔离 schema，持有一组存储的命令或对象锁时，另一组能够推进自己的工作；同一存储范围仍保持真实互斥。

**Blocked by:** 03 — PG 修订领取

**Status:** resolved

- [x] 基于真实并行 suites 暴露的跨 schema 锁耦合，以受控持锁同步点先复现失败，再修复命令锁、阻塞对象锁和尝试对象锁的统一存储范围；不更换 owner/id 或放宽领取断言掩盖问题。
- [x] 不同 schema 的同 owner/对象及命令身份可在另一范围持锁期间独立接纳、领取、完成并读回准确事实；同 schema 的不同 Store/连接仍共同互斥，放锁后能正常推进。
- [x] 原命令→对象→Job 锁序、固定回执、租约/epoch门禁及公共合同保持正确；不同实例不得以随机进程键绕过真实竞争。保留有限 hash 碰撞限制，不宣称数学上绝对无争用。
- [x] 不改已发布迁移及真实 v1 来源；说明同 schema 切换旧/新锁键协议需停止并排空旧事务与 worker，不伪称可混版本滚动双主运行。
- [x] 原冲突的两套真实 PG suites 并行复验、正常同范围竞争和受影响 race通过，记录准确命令、观察、版本、失败及结果。全片退出仍由主任务汇总，不增加04的业务先决边。

## Comments

2026-10-03，本票是整合真实测试发现后的独立修复，不属于原八票53AC。依据[授权代理锁范围决定](../pg-lock-scope-decision.md)；04仍只依赖02/03。两票各用独立工作树，完成前整合最新分支以解决必要文件冲突。


2026-10-03，票09完成真实 red→green 与并行复验。基线 `930d3cae32e674e6f1ca856b6b60fa198e1b1b5c`，独立工作树 `/tmp/lerna-worktrees/durable-work-09`、分支 `codex/durable-work-ticket-09`。实际读取 implement-spec 与 tdd/SKILL.md、tests.md、mocking.md，沿用已授权 Host、内部 Tx/存储及独立 Host Observe/public GetCommand seams，不重复请求确认。全 spec02 仍 in-progress；本票不代替后续 SQLite Claim、调度或恢复出口。

**实际红测与正常对照。** 新增独立 `conformance/recovery/pg_lock_scope_test.go`，没有改现有 work_test.go 或共享接纳套件。测试创建同一专用 PG database 中两组自登记随机 schema，保持相同 owner、command_id 和 input ID。有限事务经真实 LockInput/LockCommand 成功后才发送 acquired channel；测试控制释放，全部退出与清理由有限 context 覆盖，没有概率 sleep、私有表读取或调用次数断言。先执行 `go test -count=1 -tags=integration -timeout=30s ./conformance/recovery -run '^TestPGDifferentSchemasClaimAndCompleteWhileInputLocked$' -v`，旧代码实际失败 `independent schema claim while A holds input: [] <nil>`（0.308s）。随后执行相同入口、`-run '^TestPG(DifferentSchemas|SameSchemaStores)'`，跨 schema Record 实际因旧共享锁返回 dependency_unavailable；跨 schema Claim 同样空，两个同 schema 独立 Store 控制通过（整轮失败2.764s）。不是只串行复跑旧 suites。

**最小产品修复。** LockCommand、LockInput、TryLockInput 统一调用 Store 私有 lockKey，JSON tuple 为 `[schema, kind, tenant_id, owner_id, id]`，schema 只来自已验证 Store.config。保留 command/input advisory 参数类别与命令→对象→Job 锁序、所有原行锁、资格/epoch/lease 门禁，未加 instance/worker/random nonce。修复后四项锁域回归实际全部通过（1.788s）。B 在 A 仍持输入锁期间领取原 Job、事务外计算 hello 的独立已知 SHA-256、完成并经另开 Store 的 Host 读回准确原 Job/输入修订/完成修订/投影；public GetCommand 读回固定 receipt，A 的正文与待处理责任不变。B 的相同 command_id 在 A 仍持命令锁时接纳完成，并经独立 Host/公开查询读回原决定。两独立 Store 同 schema 持输入锁时 Claim 空、释放后原 Job 正常完成；持命令锁时另一个 Store 到有限锁截止仍不能接纳、公开查询 not_found，放锁后同原键成功且跨 Store 重传固定 receipt 相同。

**实际并行整合验证。** 从修复源码同时启动两个独立子进程：`go test -count=1 -tags=integration -timeout=120s ./conformance/recovery/...` 和 `go test -count=1 -race -tags=integration -timeout=120s ./conformance/recovery/... ./internal/durableworkdemo/...`。普通 recovery13.411s、race recovery22.283s/consumer1.567s，两进程 exit0。随后为加强独立观察，将两个跨 schema 案例的最终 Observe/GetCommand 明确绑定另开 Store 的 Host；最终同时启动 `make test-integration`（实际内含上述普通 go test -count=1）与上述完整 integration-race 命令，recovery普通9.583s、race17.244s/consumer1.491s，再次两exit0。原 `TestPGConcurrentNewWorkAndCompletionBothCommitOrders` 的两提交次序、同 owner/对象并发、SKIP LOCKED、租约接替、旧 worker、锁后时钟/epoch、正常投影及两库接纳均包含在实际完整 suites。进程同时启动且测试禁用结果缓存；没有把串行通过记为锁隔离修复。

**版本、配置与清理。** Go1.27.1、Node24.19.0、pnpm12.8.1、pgx/v5 v5.11.0、PostgreSQL18.6、Linux amd64/CGO_ENABLED1、GCC Debian14.2.0-19。完整套件的有效 PG Settings 断言验证 READ COMMITTED、synchronous_commit=on、transaction3s、statement2s/lock1s；外层测试context15s、套件120s。DSN 仅由启动程序从 chmod600 protected 文件读取到 LERNA_TEST_POSTGRES_DSN 后注入，没有输出或提交凭据。两轮并行测试使用 `/workspace` 下各自登记临时 TMPDIR（overlayfs），PG 只创建并清理本轮注册 schema；没有 DROP database 或访问环境 smoke。

**基础检查与兼容限制。** 使用缓存及 bootstrap 锁运行 `make bootstrap`，锁定依赖校验通过；`make fmt`、`make check`（含 vet/两方向158共同合同fixture/生成一致性/build）、`make test-race`、`go mod verify` 与 `git diff --check` 全部通过。最终 make test-integration 同时验证已提交 PG/SQLite v1 SHA256SUMS；0001/0002迁移、历史writer来源与公共1.0生成物未改。adapter README 已说明：有限 hashtext 仍可能碰撞并增加争用，本修复仅消除不同 schema 确定性别名；同 schema 从旧键协议切换必须先停止并排空所有旧事务/worker，再启动新版本，不支持混版本滚动锁协调。schema 不改变命令、Job 或逻辑 owner 身份，也不授予同线上 owner 两份权威账本。未声称远端 CI、生产多主、断电或故障域耐久；root 负责后续合并、推送及全片退出。
