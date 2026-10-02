# 本地开发

[工程方案](../docs/architecture/engineering.md) · [宿主装配](../docs/architecture/deployment.md) · [机器契约](../contracts/README.md)

默认使用本机多进程和 Docker PostgreSQL。Go 1.27.1、Node 22.12+、pnpm 10.7.0、Python 3.9+ 与 Docker Compose 必须可用；仓库固定 Go/TS/Python 依赖版本和 PostgreSQL 18.6 多平台镜像摘要。首次安装由 `make setup` 完成，开发身份不需要 IdP。

## 启动与退出

在仓库根执行：

```sh
make setup
make dev
```

`make setup` 保留已有 `dev/.env`，只为首次初始化生成两份独立本机密码，文件权限为 0600；该文件和运行目录均不进入 Git。`make dev` 先检查本次端口和原目录，再启动数据库并确认受限应用身份能够连接，然后构建宿主并启动整组本机进程。任一子进程启动失败或意外退出时，脚本停止本次其余子进程。

Ctrl-C/SIGTERM 触发有界退出，宿主先关闭启动可用状态，再排空已有诊断请求。脚本在 8 秒后终止本次仍未退出的子进程组；这是骨架的进程清理，不能证明未来外部业务效果已取消。退出后 PostgreSQL 继续运行；`make deps-down` 停止容器，命名卷保留。脚本不执行 `down -v`。

从另一终端运行 `make dev-stop` 可以停止当前整组进程；它连接仅本用户可访问的本地 supervisor socket，不按旧 PID 猜测或批量杀进程。

只启动中间件使用 `make deps-up`。显式单体使用 `make dev-single`：一个 `all` 宿主加 Vite，使用独立 SQLite 数据目录，不启动 PostgreSQL。两种模式共用部分本机端口，分别启动。

## 本机进程清单

| 进程 | 配置 | 管理入口 | 当前职责 |
| --- | --- | --- | --- |
| gateway | [gateway.json](config/gateway.json) | `127.0.0.1:18080` | 网关角色的生命周期与依赖探测 |
| application | [application.json](config/application.json) | `127.0.0.1:18081` | 应用角色的生命周期与依赖探测 |
| worker-1 | [worker-1.json](config/worker-1.json) | `127.0.0.1:18082` | 第一名工作者的生命周期与依赖探测 |
| worker-2 | [worker-2.json](config/worker-2.json) | `127.0.0.1:18083` | 第二名工作者的生命周期与依赖探测 |
| executor | [executor.json](config/executor.json) | `127.0.0.1:18084` | 执行宿主的生命周期与依赖探测 |
| Vite | [参考应用](../apps/web/README.md) | `127.0.0.1:5173` | 显示实际宿主依赖状态 |
| PostgreSQL 容器 | [compose.yaml](compose.yaml) | `127.0.0.1:5432` | 五进程共享的本机权威数据库 |

[topology.json](topology.json) 是开发脚本与前端代理共用的静态实例清单。角色配置不把模块目录各自变成服务；领域、事务与进程边界继续按架构实现。每个后端产生新的随机 boot_id，固定配置的 instance_id 只用于本地诊断。

管理地址只允许回环 IP。这些 HTTP 地址承载 `lerna-dev-status/1` 诊断，不是 harness/1 业务接口。WSS、gRPC、服务发现和业务鉴权尚未实现，不将管理 HTTP 冒充生产传输。

## 健康与真实范围

| 路径 | 成功含义 | 当前行为 |
| --- | --- | --- |
| `GET /health/live` | 进程能够回应 | 200 |
| `GET /health/startup` | 数据库当前可连接、原本地根仍存在且未排空 | 正常 200；缺依赖或排空 503 |
| `GET /health/ready` | 能够安全接纳业务 | 始终 503，领域、恢复与批准检查待实现 |
| `GET /status` | 有限本地诊断 | 返回角色、boot_id、依赖状态和缺失项 |
| 其他路径 | 当前未支持 | 501，无业务回执或副作用 |

两个默认 worker 各自打开诊断连接池，领域 handlers 尚未装配。公共[持久工作库](../internal/durable/README.md)通过隔离数据库与独立测试进程验证；默认启动成功仍不证明领域就绪。原开发数据库可显式建立公共命令/作业表，启动不自动迁移。

## 数据、权限与恢复

- PostgreSQL 使用命名卷 `lerna-dev_postgres-data`，PG18 的挂载目标为 `/var/lib/postgresql`；数据库提升和跨可用区恢复未在本机模拟。[官方镜像卷规则](https://github.com/docker-library/docs/tree/master/postgres#pgdata)
- 初次初始化的 [SQL](postgres/init.sql) 创建 `lerna_app`，它没有 superuser、建库、建角色、复制或 BYPASSRLS 权限，也没有公共 schema 的 CREATE 权限。`postgres` 只用于开发初始化和显式迁移，应用进程使用独立凭据。
- 多进程内容根为 `dev/.state/multiprocess/content/`，受管文件目标为 `dev/.state/multiprocess/managed-files/`；它们是两个不同目标，当前没有实现内容发布或文件操作。
- 单体使用 `dev/.state/single/database/harness.db`、独立内容和文件根。SQLite 启用 WAL、FULL、foreign_keys 和一个连接，并以覆盖生命周期的 OS 排他锁限制单实例；PG 进程不共用此锁。
- 日志保存在 `dev/.state/logs/`。已启动宿主不会在根目录失联时另建空目录；`make setup` 是显式初始准备，不是旧任务恢复命令。

不要通过更改 `.env` 期待已有 PG 卷中的密码自动改变；官方初始化 SQL 仅在新卷运行。保留原凭据，改凭据需针对原数据库明确更新。若首次初始化中断，先检查原卷、角色和数据库，再补齐缺失步骤，启动脚本不会删卷重新开始。

## 构建与检查

```sh
make build
make check
make durable-check
make migrate-postgres
# 单体停止后：make migrate-sqlite
make sql-check
build/harness version
build/harness status --url http://127.0.0.1:18080
```

构建四项 Go 命令及 TS SDK/Web，新增 harness-migrate。`make check` 运行 Go race/vet、TS、两方言 SQL 生成差异、静态契约/Proto 检查；未提供测试 PG 配置时 Go 套件跳过 PG 部分，真实 PG 必须另运行 `make durable-check`。

`make durable-check` 复用或启动 Docker PG，读取 dev/.env 而不打印凭据；每个用例创建随机隔离 PG 数据库并只清理自己创建的库，SQLite 使用临时原文件。测试不迁移主库、不清理原卷；小负载原始计量写入忽略的 dev/.state/durable-cost.jsonl。[当前证据](../.scratch/reliable-work/evidence.md)分别报告公共约束与未实现的领域/生产边界。

迁移通过 `make migrate-postgres` / `make migrate-sqlite` 独立执行，重复升级幂等；它们不启动宿主。PG 使用独立迁移身份，SQLite 要求原单体停止并持有原文件锁。契约 Proto 输出仍在 dev/.state/generated/proto/，SQL 产物分别进入适配器的 querygen/。
