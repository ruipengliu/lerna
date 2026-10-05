# 02: 提交目标与创建任务

**What to build:** 用户在命令行输入一个目标，Lerna 持久保存输入、创建会话和任务，并能查询任务快照。这是第一条端到端切片：Protobuf 定义命令身份、回执、会话输入和任务；持久工作的命令回执落在 SQLite（本地档）；进程内装配（harness）可以被测试用公共契约命令驱动。

**Blocked by:** 01（骨架与工具链）

**Status:** resolved

- [x] 命令行可以提交目标、查看会话和任务的全量快照与当前阶段
- [x] 同一 `CommandIdentity` 同内容的重复提交返回原决定，只有一个任务；同标识不同内容被拒绝（测试标注 G3 及持久工作的命令回执规则）
- [x] "提交结果未知"时可以用原标识查询决定，明确区分"没看到"和"确定没提交"
- [x] 业务修改、回执和必要的待办工作在同一事务提交；处理函数没有事务外副作用
- [x] SQLite 的同步和日志设置满足本地档的声明（ADR 0001），配置写在一处
- [x] harness 与生产宿主共用同一装配代码；测试只通过公共契约命令驱动，断言持久记录
- [x] 所有记录和接口带用户范围，错误身份被拒绝

## Answer

Implemented on `codex/m1-ticket02`. Public Protobuf commands and snapshots are in `contracts/proto/lerna/v1/submission.proto`; the production CLI and tests share `cmd/assembly`. The reception transaction persists `SUBMITTED` plus the original input reference and one decision job. The next adjudication transaction atomically writes session input/head/task association, task, immutable decision and job completion. Goal text is staged separately with its source identity, version and acquisition time.

Evidence: `core/durable/durable_test.go` submits through the public command, then closes/reopens real SQLite and reads the session, task and original goal. `conformance/submission_test.go` verifies immutable duplicate decisions, semantic conflicts, issuer namespace separation (including delimiter collisions), concurrent duplicates, rejection without sequence advancement, wrong-user/issuer rejection, and `NOT_FOUND` versus `UNAVAILABLE` with unknown acceptance. `conformance/cli_test.go` exercises the same production CLI adapter. `make check` passes (format, production/fault lint, protocol checks, race tests, rule annotations and generated-code freshness). Fault injection is added by03.

The new dependency `github.com/mattn/go-sqlite3 v1.14.52` supplies the maintained native SQLite driver; it was chosen over a pure-Go translation for direct SQLite/VFS integration and requires CGO. Runtime evidence: SQLite3.53.4, source ID `2026-07-24 19:02:57 bf7c7f30031888f4e796e429ab3978879485813aaca6f641c7b33e4e09459bcc`; the single pinned write connection verifies WAL, synchronous FULL and fullfsync ON. User/domain/profile are fixed in the database. `PowerLossQualified=false` explicitly retains the21 qualification obligation; these tests do not claim physical power-loss survival.

Run: `go run ./cmd/lerna --db /tmp/lerna.db submit --command goal-1 --goal "Write a greeting"`; use `receipt goal-1`, `task <id>` or `session <id>` to query full snapshots. `recover` processes already persisted pending goals. The caller supplies and retains the command identity for safe retry.

The durable decision callback documents that business refusal must precede writes; admission with multiple mutating participants in05 must add savepoints or equivalent rollback before recording a refusal. Lease/fencing behavior remains04.
