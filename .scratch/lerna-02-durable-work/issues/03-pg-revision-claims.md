# 03: PG 修订领取与过期 Claim 隔离

**What to build:** 工作者在原 Job 领取准确修订；新增工作、过期接替和迟到提交均不丢失已保存责任。

**Blocked by:** 01 — PG 原命令原子接纳与恢复查询

**Status:** resolved

- [x] 真实 PG 的有界持久扫描可独立领取已提交 Job，通知不是唯一恢复路径；领取固定 worker、claimed_revision、epoch 与 lease 截止。
- [x] 领域阶段输入在领取事务中得到一致观察，处理在持锁事务外，结果用有限短事务核验 Claim 后提交；不把任意 worker 回调放入长期事务。
- [x] 领取后加入新工作，再完成旧修订只推进 claimed_revision，较新 work_revision 仍可领取；两种并发顺序均有受控同步点及独立 Host 结果。
- [x] 续租和完成要求原 Job／worker／epoch／claimed_revision／有效期限绑定；调用方修改 claimed_revision 或已过期未被接替的旧 Claim 均不能提交。
- [x] 过期由同 owner 的可信时间裁决，接替增加 epoch 且保留 Job／业务对象身份；旧 worker 迟到拒绝，新 worker 正常完成，不宣称外部效果已隔离。
- [x] 真实锁序、原键／对象／Job 竞争及扫描上界有正常并发对照和有限截止；准确修订、epoch 越界拒绝，不能回退或整数溢出。
- [x] 领取所需的实际新增字段／约束以真实 v2 向前迁移引入，保留 v1 的准确 writer 与脚本证据，为票据 07 提供真实旧→新路径。

## Comments

2026-10-03，票03完成，准确实现代码 `18b80ce`。切片02仍为 in-progress；SQLite Claim、等待/退避、公平/配额、旧 writer 完整升级恢复和进程 SIGKILL 由后票承担。

**实现与边界。** 新增实际 runtime `ClaimStore`，保持接纳 `JobStore` 与 Host admission Storage 不变；project 消费端声明小的 `WorkRepository`，Host 仅装配，SQL 全在 PG adapter。扫描按 tenant/owner、持久 eligibility time 的索引及 LIMIT 1–64 取候选，不先锁 Job；消费端尝试原对象 advisory lock，固定输入行，再条件领取 Job（SKIP LOCKED）。record 保持原键→对象/输入→Job；claim/renew/complete 都保持对象/输入→Job，无 Job→对象反序。没有通知依赖、无限候选列表或事务中的业务回调。扫描遇到同对象竞争可少领；公平及竞争租户进展界尚属票06。

Claim 固定原 Job/对象/阶段、worker、claimed_revision、epoch、lease_until；续租返回新的租约 token，旧截止 token 不能继续提交。有效截止为严格 now < lease_until，已过期尚未被替换也拒绝，接替保留原 Job/对象并增加 epoch。epoch 的递增在 bigint 上界前条件保护；负数/零/最大值伪造 token 均拒绝，revision 不能由旧 Trigger 回退。`Project` 在领取事务返回后对准确 UTF-8 text 计算 SHA256，`Complete` 短事务中核验全部绑定后，共同提交 project 的 inputRevision/textDigest 和本次 completed_revision；派生计算不增加 Input.revision。较新 work_revision 保持 ready，不写回旧 work_revision。无外部动作，租约到期只隔离受控数据库写入，不能证明外部进程或效果停止。

**真实 v2。** `0002_claims.sql` 实际增加 worker/claimed_revision/lease_epoch/lease_until、leased/done 状态及绑定/进展约束、generated scan_at 和 owner 范围 eligibility 索引，并加入 projected_revision/text_digest 及有效投影约束。v2 checksum 为 `sha256:cdb7dea9f55ee8ac9201a943cecf8096108b48bc208409e1372bbf17295b2297`。Migrate 在有限同一迁移事务中应用 v1→v2，重跑验证两版 checksum；MigrationVersions 返回真实已应用 version/checksum，正常 v2 Claim/完成经重开连接实际使用。发布过的 PG0001 原字节和 `sha256:f8d04d373b039a425b4f6d0a7b7dd4410971c00faf91cdba3f68a9204579127e` 未改；`988f8b7` 准确 v1 writer、dump、SHA256SUMS 均保留且集成入口校验通过。票03证明 fresh 库实际 v1→v2 初始化与新行为，票07仍须用保存的历史 writer 输入完成旧数据升级/恢复出口；没有用空迁移或删库重建替代。

**TDD 与同步输入。** 实际读 `.agents/skills/tdd/SKILL.md`、`tests.md`、`mocking.md`，沿用已授权 Host Tx/storage、工作观察与 public GetCommand seams。纵向 red→green：首个 scan/snapshot/project 用例因 NewWorker/Project/Projection 不存在编译失败，最小实现后通过；较新修订用例先因旧完成把原 Job 一律设 done 触发真实 jobs_progress_state 拒绝，再改为只推进 claimed_revision 并保留 ready 后通过；续租故事先因 Renew 未实现编译失败，完成后伪造 lease 的错误分类暴露检查次序，改为绑定/不缩短期限的条件更新后通过；旧 Trigger 边界先返回 PG constraint 错误而未给明确范围拒绝，再增加正值及严格新修订条件后通过。其余并发、边界与正常对照作为已实现机制的回归追加并直接通过，不伪记未发生的 red。所有业务结果经 Host Observe/GetCommand，无私有表查询或内部调用次数断言。

新增11个真实 PG 测试覆盖：无通知扫描与已知 hello 摘要、准确 Unicode/NUL UTF-8 摘要；领取 revision1 后提交 revision2，旧快照不变化、旧完成只推进1、原 Job 领取2并正常完成、原固定回执不变；renew 全部绑定篡改、续租后旧 token、到期前1μs重开仍不领、到期边界旧 Claim 未被接替也不能 renew/complete、替换后迟到拒绝及新 worker 正常完成；负/零/最大伪造 revision/epoch、旧 Trigger 回退拒绝，合法最大 int64 修订可领取/完成、进一步输入变更固定 unsupported；无期限/nil context、batch和lease界限拒绝；非法投影失败后完成/输入结果整笔回滚并正常重试；迁移真实版本/checksum与实际 v2 使用。并发用真实 TxRunner 的 pre-COMMIT channel gate 保持现有 Host 业务实现、原键/对象/Job 锁均由真实 SQL 获得，在新工作先提交和旧完成先提交两序下同时启动竞争事务并由 Host 独立观察 revision2 保留。另有真实对象锁 holder 与扫描 batch1/batch2 对照、五份责任最终完成、12个独立连接/worker 同轮竞争仅一个获得原 Job，以及等待对象锁期间可信时钟推进到到期后拒绝旧完成。同步点均有 context deadline，没有长 sleep；测试 gate 只注入故障/同步，不是生产事务的外部等待。

**版本、配置与验证。** Go 1.27.1；pgx/v5 v5.11.0 database/sql adapter；实际 PostgreSQL `18.6 (Debian 18.6-1.pgdg12+2)`，READ COMMITTED、synchronous_commit on、statement_timeout 2s、lock_timeout 1s，事务上限3s；套件/同步 context 15s，清理5s，集成命令 timeout120s。Claim lease支持1ms–5min，UTC微秒；默认原 owner 可信时间来自 PG clock_timestamp，领取/续租/完成在获得对象锁后再次取时。边界用一个所有同 owner worker 共用的受信可控 Clock，未以不同机器本机时间竞争裁决到期。每例随机自登记 schema，有限清理仅删自己成功创建的范围；不碰 smoke、不删数据库、不输出 DSN/密码。没有新增个人绝对环境依赖。

实际执行且通过：`go mod verify`、`git diff --check`、`make check`、`make test-race`、`LERNA_TEST_POSTGRES_DSN=<显式专用测试库> make test-integration`（原18+新增11=29个真实PG测试）、`go test -race -tags=integration -timeout=120s ./conformance/recovery/... ./internal/durableworkdemo/...`。基础 check 不访问外部服务；集成入口仍缺配置/不可用服务硬失败。此票本地不 push，准确 feature tip 的远端 CI 由整合任务核验，不能将本地通过记为已远端通过。全部七AC满足后 resolved，但不关闭切片02整体，也不宣称 SQLite Claim、SIGKILL、断电/故障域或生产耐久已验证。

**远端核验补记。** 经merger整合并推送的准确提交 `e8e3384e4cce1f63187ff84f281283a023e07ab1`，实际 [CI 37146622433](https://github.com/ruipengliu/lerna/actions/runs/37146622433) success；基础合同与PG集成/race job均通过，29项真实集成2.534s、race4.866s且非cached，v1校验和保持。详见[CI证据](../ci-verification.md#pg-修订领取检查点)。上文“由整合任务核验”为feature工作树当时状态。
