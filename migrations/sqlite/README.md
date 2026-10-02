# SQLite 迁移

00001 是独立 SQLite 方言，建立公共命令/作业表及活动责任索引；版本表为 durable_schema_version。通过 goose 显式升级，运行 Open 只接受版本 1。

在单体停止后执行 `make migrate-sqlite`，使用原 dev/.state/single/database/harness.db；也可运行 `build/harness-migrate --driver sqlite --path /absolute/database.db`。迁移持有同一 OS 排他锁，正在运行的单体会阻止迁移；不另建空库绕开原锁。父目录须事先准备，重复执行幂等。

当前 CLI 只升级，不暴露 Down/清空。领域表、旧数据兼容和恢复仍须按所属切片验收。
