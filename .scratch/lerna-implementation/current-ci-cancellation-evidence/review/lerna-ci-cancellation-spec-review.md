# Spec — current CI Run cancellation repair

固定 baseline `75f0429d709d95b6a63137c397fcd9d80203ed5f`，HEAD `3570180514ce0a6cee4daa27430540003574533d`；命令 `git diff 75f0429d709d95b6a63137c397fcd9d80203ed5f...HEAD`。172 路径，4 个代码文件，其余归档；提交 `3570180/5ebd91d/2f961a0/0c1f924`。独立 Spec 静态轴，未看另一轴，未运行 Go/PG/Node/fmt 或修改产品。

来源：`.scratch/lerna-implementation/issues/current-ci-cancellation.md`（issue）及 `current-ci-cancellation-qualification.md`；已采用 `/tmp/lerna-current-ci-cancellation-decision.md`（Run decision，SHA `1b097620…`）和 `/tmp/lerna-ci-run-blocked-sql-fixture-decision.md`（fixture decision，SHA `851a9e89…`）。

**代码 findings：0。** 未发现缺失的实现、未经要求的范围扩张或错误实现。issue13 要求“聚合实际 lane 错误和 caller 的真实 Err”；Run decision20 要求“wait for all three goroutines … caller’s actual Err() if nonnil”。产品保留原 caller，把 child cancellation 与 caller 分开，收齐三个有 lane 标签的实际 outcome 后返回 `errors.Join`；未丢 driver/commit-unknown、改写 PG 错误、fabricate caller cancel、重试或改变期限/Claim/配额。两个有限机械 barrier 测试分别核真实 caller cancellation/all causes 和 live-caller first fault/internal stop，不称 native CI 原因复现。

fixture decision15–16 要求“三 bounded ordinary Store.Within transactions”及“三 registered Run identities waiting on the exact … advisory SQL and exact blocker”。完整读取 522 行真实 PG 控制：同 Store 的 Runner/Repository/Clock/worker/callback、parseable application-name DSN、peer intent-before-Open、PID/backend_start/database/user/tag 注册、三 setup 的实际 release/join 和 idle-generation 核验、原持锁方及精确 advisory SQL 等待均保留。没有额外 LockPool preflight、借 token 或注入 driver 错误；注册明确是 after-startup/pre-Run，非 exhaustive census。

fixture decision22 要求“original projections, reservations, later ordinary completion, fixed receipts and every entry joined”。正常释放/真实取消均检查所有 entry exited、实际原因保留、公开 control/reconciliation 投影、原 ordinary reservation、原 Claim 后续 Complete 与三固定 receipt；正常释放先于取消资格。完整阅读两份 normal PG raw：0.505/0.562s PASS、真实三 SQL waits、完整公开尾及 observer first Close confirmed。既有 fixture 的有限 holder/Run join 和 first-Close gate 独立于 error classification；进程 absence 不补历史 Close。

**已知未完成资格：1 组，非新代码缺陷。** issue14–15 要求“PG…及 SQLite…普通和 race 通过”与“受影响检查…资源证据…合入并 push，核对新 CI”。PG pair race、原九 selector 的 18 项检查、当前 check/资源审计/交付及新 CI 尚待；partial 文档保持 claimed，没有冒报完成或原失败 SQL 已重现。
