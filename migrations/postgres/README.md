# PostgreSQL 迁移

00001 建立公共 durable_commands、durable_jobs 及活动责任索引；领域业务表随自己的切片迁移。goose 使用 durable_schema_version 和会话 advisory lock；应用 Open 只验证版本 1。

`make migrate-postgres` 读取忽略的 dev/.env，将独立迁移身份只通过子进程环境传入 CLI，显式升级本机原数据库。重复执行幂等。新环境可用 `build/harness-migrate --driver postgres --url-env ENV_NAME`，URL 只放对应环境变量。

宿主启动不迁移；应用角色不获 DDL 权限。SQL 的 Down 仅为迁移资产，当前 CLI 不暴露降级/清空入口，长期最小身份及未结责任不能因回滚代码而删除。版本兼容部署另按[宿主规则](../../docs/architecture/deployment.md)验收。
