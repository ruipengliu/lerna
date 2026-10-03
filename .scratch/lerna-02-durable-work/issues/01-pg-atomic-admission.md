# 01: PG 原命令原子接纳与恢复查询

**What to build:** Host 开发者在真实 PostgreSQL 中接纳一个无外部副作用的工作，丢答复、重开与原键重传仍取得原固定决定。

**Blocked by:** 无（切片 01 完整退出后可开始）

**Status:** resolved

- [x] 内部 Host 演示命令固定为 host-durable-work-1／host／durable_work.record，经专属闭合 Schema 验证；关联对象 kind=durable_work，不修改公共 1.0.0 方法清单，不伪造 Task。受信主体由宿主注入，目标 owner 与事务范围显式一致。
- [x] 业务事实、固定原回执与必要 Job 在同一有限短事务共提交；Tx 显式绑定数据库和 owner，repository 复用该事务。事务内不得等待网络、模型、对象上传或用户输入；正常、回滚及重开经真实数据库、Host 观察和受信 command.get 验证，不读私有表代替验收。
- [x] 按 tenant、owner、command_id 去重；同原内容返回不变决定，异内容／主体／期限／目标或预期修订复用稳定冲突，不执行第二份内容、不覆盖原决定。
- [x] 原键查找先于新接纳截止；新过期命令固定拒绝，已有命令截止已过仍返回原决定；提交结果未知保持原身份，不报确定失败后新建请求。
- [x] 公开 command.get 返回真实固定回执与 progress none，工作结果和修订用 Host 观察；越权存在／不存在均不泄露，查询和重传不会迁移 owner。
- [x] PG 隔离、同步提交及有限 statement／lock 期限显式核验；并发原键正常对照、租户／owner 隔离及统一锁序通过真实 SQL 竞争。
- [x] 首次迁移版本化且具 checksum；保留准确 v1 迁移及其真实 Host writer 生成的可重建升级输入、命令、版本和输入摘要。v1 输入至少保留已接纳且 Job 待处理、固定过期拒绝及原键重传事实；领取新增字段留给票据 03 的真实 v2 功能迁移，不预填后伪造空升级。
- [x] 锁定必要驱动与迁移来源；新增真实集成入口及 CI 配置，缺服务必须失败；测试数据隔离、有限清理，不依赖个人绝对路径或提交密钥。

## Comments

2026-10-03，票01完成。切片02仍为 in-progress；Claim、SQLite、调度及进程故障票据未提前实现。

**实现与边界。** 实际 v1 writer 代码冻结于 `988f8b7`。runtime 只组织原键/摘要/截止/同 Tx 固定回执；`internal/durableworkdemo` 保存 consumer-owned Repository 合同与演示前态，Host 仅装配，具体 SQL 在 PG adapter。目录依据已授权的 Host 演示布局裁决。内部命令使用专属闭合 Schema，并复用已有严格词法、共同值和原摘要；1.0.0 方法清单、公开 Schema 和 digest 算法未改。输入、固定 applied/rejected 回执与待处理 project Job 在同一 owner/数据库 Tx 提交；v1 没有 Claim/lease 列。保存原命令最小版本、profile、method、target、主体绑定、期限、预期修订及摘要，不另存一份无用途正文。

**TDD。** 实际读取 `.agents/skills/tdd/SKILL.md`、`tests.md`、`mocking.md`，沿用已确认的内部 Host Tx/存储合同、Host 观察和公开 `contract.GetCommand` seams。逐步 red→green 的记录：内部闭合 record 与首个原子接纳测试先因实现不存在失败；Tx owner/数据库/过期 token 验证先因缺显式 repository owner 参数失败，再加入一致 scope；合法零 Unicode 标量先在 PG text 写入失败，再用 bytea 保存原文；nil Tx 先 panic，再稳定返回 ErrScope；最小原命令元数据测试先因存储合同缺 Metadata 失败，再在 v1 真正保存；真实 COMMIT 确认丢失后公开 Encode 先拒绝错误 next_action，再修正为 query_or_retransmit_original 并验证 Host 返回边界。其余回归逐条经相同真实 seams 验证，没有按私有表、调用次数或手造镜像公式断言。

**真实 PostgreSQL 结果。** 正常接纳可经 Host 观察输入 revision1、原 Job ID、work_revision1/completed_revision0/ready，并经受信公开 command.get 读取原固定回执及 progress none。控制对照与拒绝覆盖：回滚全部三类事实后原键 not_found，正常重试再提交；真实连接关闭重开后原键/Job 不变；24 个同原键并发全部返回同一回执且只一份修订；12 个新命令同 expected_revision 竞争只有一个 applied，其余固定 revision_changed；准确主体/有序委托链允许表、租户/owner 隔离、无权存在/不存在统一 forbidden；持有原 owner 的同原键实际 SQL 锁时，另外租户与 owner 的同 command/object ID 并发仍各自正常提交；修改正文、主体、期限、目标及预期修订稳定 idempotency_conflict；trace 变化仍返回原决定；原键先于截止，新的截止边界固定 expired；缺省/错误前态拒绝、相同正文的新显式修改推进原 Job、最大 int64 修订固定 unsupported。真实对象锁竞争约 1s 内因 lock_timeout 回滚，之后正常对照成功。所有业务断言经 Host 或 public GetCommand，存储 scope/配置断言经已确认的内部端口。

**提交未知故障边界。** 有界 PostgreSQL wire proxy 转发真实 COMMIT，收到服务器 CommandComplete(COMMIT) 与 ReadyForQuery 后抑制答复并断开。Host 返回可由公开 contract.Encode 验证的 commit_unknown，保留原 CommandRef；独立连接查到 applied 后原键重传仍只一份 Job。这与已确认提交后故意丢 Host 答复的测试分别记录。未把断连、任何 PgError 或 context 取消一概解释为确定未提交；只有驱动明确 ErrTxCommitRollback 的合同能在 COMMIT 分支证明回滚。没有宣称 SIGKILL 或外部效果故障已验证。

**版本与配置。** 根 Go 唯一 module；实际安装并锁定 `github.com/jackc/pgx/v5 v5.11.0` 的 database/sql adapter（及 go.sum 间接依赖），Go 1.27.1。实际服务器 `18.6 (Debian 18.6-1.pgdg12+2)`；事务连接观察 READ COMMITTED、synchronous_commit on、statement_timeout 2s、lock_timeout 1s；Host Tx context 最大 3s，套件/代理/清理均另有有限期限。服务及 CI 锁定镜像 `postgres@sha256:3725f4e2499eef5134592b3b4ab79a543ed7f8e533b05b5b637af926630f6650`。默认可信时点来自原键锁后的数据库 clock_timestamp；截止边界测试只替换 Clock 系统边界，所有同次接纳仍用同一数据库事务。没有长事务外部等待。

**v1 来源及升级输入。** [pg-v1 provenance](../../../conformance/fixtures/durable-work/pg-v1/provenance.md) 和该目录的 SHA256SUMS、writer-revision.txt、writer-observation.json、commands.json、真实 database.sql、准确 0001_admission.sql 已保留。实际执行 `scripts/generate-pg-v1-fixture.sh 988f8b7 /tmp/lerna-pg-v1-export-01`（DSN 仅从显式环境注入），归档准确历史源码到独立临时目录构建 writer；它真实创建 applied/待处理 Job、expired 固定拒绝并重传原命令后，由 pg_dump 18.6 导出。迁移 checksum 为 `sha256:f8d04d373b039a425b4f6d0a7b7dd4410971c00faf91cdba3f68a9204579127e`，完整 dump 为 `sha256:1d2195606544b288e14a0e2819844ee187b07e097fe7e96f83513013f1ad8b60`。来源再生成已通过；随机 Job ID/DB 时间可不同，历史代码、输入及行为保持。原 dump 另在专用 DB 的独立已登记 schema 真实恢复，经当前 Host/GetCommand 重传与观察确认原 Job ID 和固定两类决定保持；这只证明 v1 输入可恢复，不是尚未实现的 v1→v2 升级出口。后03/04的真实 Claim 变更形成 v2，后07继续验升级，不用 v2 writer 手填旧数据。

**命令与结果。** `go mod verify`、`make fmt`、`make check`、`make test-race`、`LERNA_TEST_POSTGRES_DSN=<显式专用测试库> make test-integration`、`go test -race -tags=integration -timeout=120s ./conformance/recovery/... ./internal/durableworkdemo/...` 全部通过。真实 PG 套件18个测试均通过；基础 make check 不访问外部服务。`env -u LERNA_TEST_POSTGRES_DSN make test-integration` 按预期硬失败；指向不可用回环端口的明确测试 DSN 也硬失败而非 skip。缺配置/连接错误输出不含连接串或凭据。首次 CI 添加实际 PG 服务及同入口/race 任务；feature 工作树本地未自行 push，远端准确 tip CI 由整合任务后续执行，不能将已配置 CI 说成已远端通过。

**隔离与限制。** 全套使用新专用测试数据库，每例登记随机本轮 schema，只清理自己实际创建的范围；writer 固定专用 schema 也仅在创建成功后有限清理，不 DROP 调用者数据库、不接触历史 smoke。Make fmt/lint 覆盖新增实际 Go 源码；集成入口校验真实 v1 artifact 校验和。生成脚本仅依赖已声明的 Go/Git/Bash/pg_dump 18.6 及标准 Unix 工具，没有个人路径或未声明 rg 依赖。尚未验证 SQLite、Claim/执行完成、调度、SIGKILL、断电/故障域、生产凭据/授权服务或生产耐久；本票不关闭切片02整体。

**远端实际核验补记。** 整合后准确提交 `e5f26b87fb8914dc16bb6837abff6607a50cddb1` 的 push CI [37145113569](https://github.com/ruipengliu/lerna/actions/runs/37145113569) 已真实 success，基础合同与 PG 服务集成／race 两个 job 全部通过；五项 v1 校验和 OK，真实集成 1.778s、race 3.551s，均未显示 cached。详见[CI记录](../ci-verification.md)。上文“后续执行”为 feature 工作树完成时的历史状态，整片02仍未退出。
