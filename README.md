# Lerna / Harness

按 [技术架构](docs/architecture/README.md) 建立的 Go/TypeScript monorepo。当前可运行开发角色、数据库探测、健康检查与退出处理；Task、Decision、Operation、WSS/gRPC 和业务恢复随后续切片交付。业务就绪入口始终返回未就绪。

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
build/harness status --url http://127.0.0.1:18080
```

完整启动、端口、数据目录和范围见 [开发手册](dev/README.md)。公开端口预留在 `api/`，装配入口在 `runtime/`，九模块实现位于 `internal/`，远程客户端位于 `sdk/`，参考前端位于 `apps/web/`。机器契约唯一源为根 [contracts](contracts/README.md)，规范正文仍在 [架构契约](docs/architecture/contracts/README.md)。
