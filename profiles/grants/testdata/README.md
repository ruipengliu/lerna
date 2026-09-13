# 05 授权升级夹具

`05-authorization.gob` 由原提交 `37a51da` 的 AuthorizationStore 在真实 SQLite 中创建并导出，旧类型没有 Signed 字段。包含本地 continuous 授权 `old-local`、策略及原管理回执。

- SHA-256：`38719de0f52a20c5f5f6d8bbc8055ae854818215e61defebbb73587b44ce927f`。
- 受控时间：2026-09-10 00:00:00 UTC；主体 admin，命名空间 local。
- 固定测试凭证：64 个小写 a。文件中身份摘要、随机权威/接纳窗口密钥仅属于此公开隔离夹具，未用于任何真实账户。
- 配置同 profile authConfig；授权范围 root/read/task/local，有效期一小时。

验证把旧编码作为可信存储夹具装入隔离 SQLite，使用旧身份读取及签发新材料，再重开数据库核对旧回执和授权。它不代表允许生产中任意替换身份状态或旧备份安全恢复。
