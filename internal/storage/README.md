# 持久存储适配

[PostgreSQL](postgres/README.md) 与 [SQLite](sqlite/README.md) 各有查询、sqlc 产物和迁移；[sqlstore](sqlstore/store.go) 只共享受限会话、扫描与提交结果分类。两种方言都实现 durable.Backend/Session，不提供公开扩展 API。

Open 必须验证显式迁移版本 1，缺失或不兼容时拒绝；启动不创建业务表。Scope 白名单来自认证/装配，公共命令和作业 SQL 都带原 Scope，可信业务 repository 也必须带相同条件。

修改对应 queries/durable.sql 和 migrations 后运行 `make sql-generate`，生成物位于各自 querygen；`make sql-check` 使用固定 sqlc 版本重新生成并检查差异。运行库复用生成的 SQL 常量，避免手写第二份查询。业务 repository 可以使用同事务的 DBTX，但不获得 Commit/Rollback 权限；只在适配器内使用这一能力。

`Stats` 的 SQL 是应用数据/时间查询计数，不包括 BEGIN/COMMIT、迁移、监听和独立观察连接；事务计数由 Engine 另提供。SQLite 单写队列的等待不是 database/sql 的 Pool WaitCount，不能用后者为零推断没有锁等待。成本证据明确区分这些口径。
