# 工程工具

`make setup` 在忽略目录建立锁定 Python 环境。`make contracts-check` 使用 [check-contracts.mjs](check-contracts.mjs) 运行架构校验器、编译根 Proto、检查封装和当前资产位置；无需全局安装 protoc。Python 生成物和描述符只写入 `dev/.state/generated/proto/`。

[check-asset-locations.py](check-asset-locations.py) 核对唯一维护源、source-map 当前目标和 UML sourceDoc。历史 `.scratch` roundtrip、渲染 manifest 与原材料摘要保留，不把旧生成器作为当前图的维护入口。

Go 线协议生成、严格解析和 SQL 生成随实现切片引入。将来 protoc-gen-go 输出到 `internal/wire/rpc/v1/`，它不是 `api/` 的领域类型；PG/SQLite 的 sqlc 查询和产物各自保存在对应存储适配器。
