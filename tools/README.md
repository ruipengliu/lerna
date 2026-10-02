# 工程工具

`make setup` 在忽略目录建立锁定 Python 环境。`make contracts-check` 使用 [check-contracts.mjs](check-contracts.mjs) 运行架构校验器、编译根 Proto、检查封装和当前资产位置；无需全局安装 protoc。Python 生成物和描述符只写入 `dev/.state/generated/proto/`。

[check-asset-locations.py](check-asset-locations.py) 核对唯一维护源、source-map 当前目标和 UML sourceDoc。历史 `.scratch` roundtrip、渲染 manifest 与原材料摘要保留，不把旧生成器作为当前图的维护入口。

`make sql-generate` 使用固定 sqlc v1.31.1 从两方言 SQL 生成各自 querygen；`make sql-check` 重新生成并检查差异，生成物只由工具维护。Go 线协议生成与传输严格 codec 随后续切片引入，Proto 仍不作为 api 领域类型。

`make durable-check` 经 test-durable.mjs 运行真实数据库 race 套件；durable-env.mjs 只向子进程环境提供本机连接凭据。原始小负载证据写入忽略的 dev/.state/durable-cost.jsonl。migrate-local.mjs 对应两项显式迁移命令，使用独立迁移身份或原 SQLite 文件锁；角色启动不会调用它。
