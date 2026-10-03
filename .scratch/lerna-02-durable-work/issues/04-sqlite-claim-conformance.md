# 04: SQLite 工作接替与双适配器一致性

**What to build:** 同一个 Host 工作行为套件在 SQLite 与 PG 验证原 Job 接替、修订竞争和旧 Claim 隔离。

**Blocked by:** 02 — SQLite 同版接纳；03 — PG 修订领取

**Status:** resolved

- [x] 在真实单写 SQLite 上实现相同领取／续租／条件提交语义，不降低修订或 epoch 绑定；两 adapter 经相同 Host 套件观察独立事实。
- [x] 新增工作与旧完成两种同步顺序均保留较新工作；过期 Claim、旧 worker 和篡改 claimed_revision 被拒，新 worker 正常推进。
- [x] 关闭重开后 Job、work／completed／claimed 修订及 epoch 可恢复，业务对象和固定回执不变；查询仍通过原 owner。
- [x] 扫描和 Claim 持久化使用有限短事务和可信 owner 时间，处理在事务外；单写协调、取消和 busy 均有正常对照。
- [x] 真实 v2 领取迁移可从已保留的 v1 writer 输入向前升级；不改写已应用脚本、不删库重建，不用伪造旧 schema 代替旧版本。
- [x] 记录两库实际版本、PG 隔离／同步提交、SQLite 每连接 WAL／FULL 和并发结果；为等待／配额／故障提供真实共同基础。

## Comments

2026-10-03，票04完成。SQLite Claim 产品实现 `3376e08`；合入最新 integration `3a7f1f8`（含票09的 PG schema 锁域修复）并修复重启测试清理后的准确受测代码 `ff22936882a2f7b4404b943aadd2274ab0e506ea`。本票仅在自身 branch/worktree 提交；未 push、未合入 root、未创建 PR、未清理 worktree。切片02继续 in-progress，等待/退避、容量/配额、正文墓碑、完整迁移失败恢复与 SIGKILL 仍由后票承担。

**实际实现。** SQLite 实现已有 runtime `ClaimStore` 和 project 消费方 `WorkRepository`，未扩大 admission Storage、公开合同或创建通用存储框架。真实文件上的 owner 单写协调与 BEGIN IMMEDIATE 串行短事务；持久 scan_at 索引和 LIMIT 1–64 扫描，无内存通知依赖。领取固定输入 text/revision 快照、原 Job/对象/阶段、worker、claimed_revision、epoch 和 lease_until；续租要求全部旧绑定和有效期限，返回新的截止 token；完成要求 now < lease_until，尚无接替也拒绝已过期 Claim。原 Job 接替只递增 epoch，不新建业务身份。Trigger 拒绝回退/重复 work_revision，保留有效 Claim，并允许 done Job 接纳新修订；完成只推进 claimed_revision，较新的 work_revision 继续 ready。消费方 Project 在领取事务结束后处理准确 UTF-8 SHA256，条件完成与投影共同提交，处理不增加输入 revision。

**共同 seam 与行为。** 从03真实 PG 测试提取为 `adapter_work_test.go` 中同一组13个行为函数，由 PG 和 SQLite 执行同一断言及独立预期，不复制一份 SQLite 业务断言。沿用已授权 Host Tx/storage、Host Observe 和 public GetCommand seams，PG-specific SKIP LOCKED/真实持锁扫描与票09锁域测试保留专属。共有故事包括已知 hello 与 Unicode/NUL 摘要、旧准确快照和新修订、两种真实 pre-COMMIT 同步顺序、全部 Claim 绑定篡改、过期尚无替换、原 Job 接替/epoch增加、旧 worker 拒绝和新 worker 正常完成、修订/epoch/int64 上界、有限 batch/context、非法投影整笔回滚、12 worker 同轮竞争、五份责任有界批次最终完成、事务 scope 拒绝，以及关闭重开后的 Claim/lease/epoch/claimed_revision 与原 receipt。PG 同轮 worker 使用独立连接，SQLite 同 owner worker 通过唯一写协调器；第二独立 SQLite writable Host 继续由既有真实跨进程排除测试验证。

**时间边界。** 所有 SQLite 时间 SQL operand 显式绑定 v1 的固定九位 UTC `2006-01-02T15:04:05.000000000Z`，包含 created/updated/due、scan eligibility、Claim、renew、complete 与旧租约相等绑定。未混用 go-sqlite3 默认 time.Time 文本或 julianday/REAL。共同 Worker 继续 UTC 微秒 Claim 精度；v1 nanosecond 字节保留。实际共同边界使用独立明确预期：fraction=0 或123456微秒，due前1微秒不得领取、准确due可领取；1ms租约在同秒999微秒时 complete/renew可行，在1000微秒相等或1001微秒时拒绝，并可由 epoch2 worker 正常推进。新工作与旧完成两顺序均保留 revision2；真实 writer/对象锁等待期间推进可信 owner clock 后拒绝过期完成。SQLite-specific 真实外部 SQL busy 锁和已取消 writer queue 均不改变待处理 Job，解除故障后 epoch1正常领取完成。

**实际 v2 与旧来源。** `adapters/sqlite/migrations/host/0002_claims.sql` 真正扩展 leased/done、claimed_revision/worker/lease_epoch/lease_until、绑定/进展约束、eligibility scan索引及 projected_revision/text_digest。迁移在有限同一 Tx 重建 v1 ready-only Job 表并保留原键/Job/输入/回执，然后记录 checksum；Migrate 重跑检查两版不可变身份，MigrationVersions 保留已应用 v1及v2。v1 `sha256:324dd9c72a00438095596b59c80bf21e66a02eb53d7182ddba67e4784e2c0203` 未改；v2 `sha256:3791b3fc5ca49c18eee04b2afcaa54c2aae9c5afbf3f1e3fd98f5cce8715e01c`。已保留真实 `f4fb057` v1 writer/`80aebbf` fixture 的完整 SQLite file 复制到新的本轮文件后真实升级；原 applied/expired 回执、pending 原 Job 和原 text/revision 保持，升级后 epoch1 Claim并完成其已知字节的 SHA256投影。没有用当前v2 writer 手填旧schema、修改immutable0001/v1 fixture或PG0002、删库重建。本票证明实际历史来源可以应用v2并Claim，票07仍负责完整升级重开/故障恢复出口。

**TDD 与实际失败。** 实际读取 `.agents/skills/tdd/{SKILL.md,tests.md,mocking.md}` 和 implement-spec，遵循已确认 seam。首个 tracer 在 SQLite 缺 Claim 时编译 red；实际 SQL Claim 最初返回空 batch，暴露 SQLite `$8` 等命名占位符按出现顺序而非数字位置绑定的问题，改为 SQLite `?NNN` 明确位置后 green。既有 PG 行为转为共享回归后，Trigger回退先返回 SQLite constraint错误、completion-first 新工作先得到 dependency_unavailable：增加正值/严格新修订条件和 done→ready 后 green。旧快照、续租、时间边界、scope、关闭重开、busy/取消与历史升级新增回归直接通过，不伪记未发生 red。已有 v1 metadata断言在实际v2后失败，改为核验两版版本/checksum和历史来源的v1身份。

首次完整两库运行11.914s失败：所有业务断言通过，但关闭重开测试关闭了 PG schema 创建 Store，清理时 sql database is closed，不能记为绿。修复测试装配：PG 关闭独立业务 writer，保留实际创建并登记 schema 的 setup Store 供有限清理；SQLite仍先关闭唯一 Host再重开。之后完整套件11.269s及 integration-race22.544s均green且当前各轮登记schema均正常清理。失败轮的随机schema名称仅在已退出测试进程的配置map中，日志未保存准确nonce；无法重新确认该scope，不按正文/时间窗/prefix猜测DROP。依主任务裁决保留一个未重新确认归属的测试schema，并明确这一清理限制；未删除调用者数据库或其他namespace，未把该失败轮说成已清干净。

**版本、命令与范围。** 当前实际 Go1.27.1、pgx/v5 v5.11.0、go-sqlite3 v1.14.52、GCC14.2.0、pnpm12.8.1；PG18.6、READ COMMITTED、synchronous_commit on、statement_timeout2s/lock_timeout1s；driver bundled SQLite3.53.4，每个实际连接WAL/FULL(2)/foreign_keysON(1)/busy_timeout100ms经suite核验。Host Tx上限3s，测试context15s、PG清理5s、suite timeout120s，lease1ms–5min。真实SQLite各轮 TMPDIR 为本轮自登记 `/workspace` 本地overlayfs目录，测试文件不使用默认/tmp tmpfs；测试结束后该空临时目录已清理。PG每例只创建登记随机schema，DSN仅由程序从600保护文件读入明确测试环境，未打印/提交。

实际通过 `make bootstrap`、`make fmt`、`make check`（不需要PG服务）、`make test-race`、两库 mandatory `make test-integration`（-count=1，11.269s）、`go test -race -count=1 -tags=integration -timeout=120s ./conformance/recovery/... ./internal/durableworkdemo/...`（22.544s/1.406s）、两库v1 SHA256SUMS、`go mod verify` 和 `git diff --check`；SQLite选择integration2.400s、选择integration-race10.559s先行通过。`env -u LERNA_TEST_POSTGRES_DSN make test-integration`按预期硬失败，不skip。准确最终feature tip远端CI仍由root整合后核验；这些本地结果不声称SIGKILL、native SQLite COMMIT答复未知、断电、外部效果隔离或生产故障域耐久。

2026-10-03 root 核实整合提交 b1674b2 的远端 CI 37149178541 success：双库必需集成8.417s、race15.292s均-count=1，十项旧来源校验和不变，详见[准确CI证据](../ci-verification.md#双库修订领取检查点)。此证据不替代整片退出，也不改变失败轮scope限制。
