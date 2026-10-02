# PostgreSQL

pgx/database/sql 的显式短事务适配。领取使用 `FOR UPDATE SKIP LOCKED`，时间使用实际裁决时的 `clock_timestamp()`；命令及作业锁由数据库承担。应用池最多 16 个连接，测试为 4 个；可选 LISTEN 另占 1 个连接。通知为空提示，可丢失、合并；提交后发送，监听建立与重连都补触发扫描，定期扫描负责恢复。

运行身份只需既有 schema 的 DML 权限；[迁移](../../../migrations/postgres/README.md) 使用独立身份与 goose 会话锁。Open 检查版本，不迁移。现有 internal/host 的连接探测仍仅用于诊断。

SQL 位于 queries/，固定 sqlc 生成物位于 querygen/。真实数据库测试、网络 COMMIT 确认截断及独立 worker 接替见[证据](../../../.scratch/reliable-work/evidence.md)。
