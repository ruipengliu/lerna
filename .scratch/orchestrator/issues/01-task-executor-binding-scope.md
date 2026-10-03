# 执行端绑定记录需要按 Task 区分

Status: needs-triage

2026-10-02：在收缩存储 Interface 和验证 provider/resource 限额时发现，两个 Task 复用同一 Executor 会在第二次行动准入中返回 `durable.ErrInvariant`。

## 证据与原因

使用真实 SQLite 和现有 `taskFixture` 的 `act` 模式，依次接纳两个 Task，每个 Task 驱动一次 decide 和两次 poll。保持默认固定 ExecutorID 时，第二个 Task 的 poll 在 change body 返回 `durable: invalid state or version relationship`。把两个 Task 的 ExecutorID 区分后，可以完成同样准入。

[行动准入](../../../internal/orchestrator/actions.go)以 ExecutorID 作为 `executor` 记录的 ID，并将 TaskID 固定在记录中。[记录主键](../../../migrations/sqlite/00002_orchestrator.sql)是 tenant/owner/kind/id；[写入约束](../../../internal/storage/sqlite/queries/orchestrator.sql)要求冲突记录的 TaskID 不变。因此第二个 Task 对同一 Executor 的绑定不能写入，受影响行数为零并触发 invariant 错误。

本轮[调度回归](../../../tests/integration/orchestrator_scheduler_test.go)使用不同 Executor 共享 provider/resource，独立验证工作键与容量，不宣称解决同 Executor 的任务绑定。

## 待确认及验收

- 绑定身份应覆盖 Task 与 Executor；执行端本身的固定身份继续保留在绑定值中。
- 同一 Executor 可以被两个 Task 绑定；每个 Task 的控制传播、有限绑定计数和原行动身份分别成立。
- 核对已有绑定记录的兼容读取和恢复，保留原 Task/Operation/Command 与费用账本。
- 对 SQLite/PostgreSQL 补上同执行端多任务的准入与独立控制回归证据。
