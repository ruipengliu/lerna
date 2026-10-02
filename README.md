# Lerna / Harness

按 [技术架构](docs/architecture/README.md) 建立的 Go/TypeScript monorepo。已实现开发角色生命周期和[可靠接纳与持久作业框架](internal/durable/README.md)，含 PostgreSQL/SQLite 适配、显式迁移及真实故障套件。Task、Decision、Operation、WSS/gRPC 和领域恢复随后续切片交付；默认业务就绪入口仍为 503。

在仓库根执行：

```sh
make setup       # 固定依赖、生成本机凭据及初始数据目录
make dev         # Docker PostgreSQL + 五个本机后端进程 + Vite
```

打开 [本地开发页](http://127.0.0.1:5173)。Ctrl-C 停止本次后端和 Vite；数据库保留运行，`make deps-down` 停止容器并保留数据卷。SQLite 单体使用 `make dev-single`，与多进程模式分别启动。

从另一终端使用 `make dev-stop` 停止当前开发进程组。

```sh
make build
make check
make durable-check    # 真实 PG/SQLite、独立 worker 与故障证据
make migrate-postgres # 显式升级原开发 PG；启动不自动迁移
# 单体停止后使用 make migrate-sqlite
build/harness status --url http://127.0.0.1:18080
```

完整启动、端口、数据目录和范围见 [开发手册](dev/README.md)。公开端口预留在 `api/`，装配入口在 `runtime/`，九模块实现位于 `internal/`，远程客户端位于 `sdk/`，参考前端位于 `apps/web/`。机器契约唯一源为根 [contracts](contracts/README.md)，规范正文仍在 [架构契约](docs/architecture/contracts/README.md)。
