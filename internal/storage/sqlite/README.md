# SQLite

modernc SQLite 的单拥有者适配。使用绝对原路径、WAL/FULL/foreign_keys、一个连接、OS 排他锁和单写队列 `BEGIN IMMEDIATE`；锁文件为数据库路径加 `.lock`，与现有宿主一致。存储关闭后才释放锁。运行 Open 要求原文件与迁移版本存在；显式迁移才创建新数据库，父目录必须已准备。

SQL 位于 queries/，sqlc 产物独立位于 querygen/，不把 PG SQL 改写成 SQLite。SQLite 套件验证逻辑 worker、单宿主崩溃重开及框架不变量；它不证明 PG 多进程竞争正确。

[显式迁移](../../../migrations/sqlite/README.md) · [公共适配规则](../README.md) · [证据](../../../.scratch/reliable-work/evidence.md)
