# Lerna

Lerna 是按明确合同构建的 Agent 执行框架。领域规则见 [CONTEXT.md](CONTEXT.md)，模块及运行设计见 [docs/architecture](docs/architecture/README.md)。按[实现切片](.scratch/lerna-implementation/README.md)逐步交付：切片 01 的共同命令、固定回执、受信 command.get、准确版本协商及 Go / TypeScript 编解码已完整退出；切片 02 的 PG／SQLite 原子接纳、修订工作、严格 Start／Finish、持久等待与退避、正文清理、真实旧数据升级和有限进程恢复均已合入。有限队列、共享租户配额、持久公平轮转、类别保留容量及到期维护已通过本地双库验证；整片已完成两轴审查、fixture 架构优化及准确最终 CI，[退出证据](.scratch/lerna-02-durable-work/exit-evidence.md)限定为本机双库与进程恢复。当前提供内部持久演示，生产运行服务仍待后续切片。

## 开发

工具链固定为 Go **1.27.1**、Node **24.19.0**、pnpm **12.8.1**；TypeScript **7.0.2**、Prettier **3.6.2** 和验证器由仓库锁文件安装。首次使用先安装相同版本并将命令加入 PATH：

- Go 官方归档：<https://go.dev/dl/go1.27.1.linux-amd64.tar.gz>。本次 Linux amd64 实测归档 SHA-256 为 `63d339f0da5ab53635a56f2490a7984dfe12dfcff22ad749f63edaf590168445`；其他平台使用官网对应归档与校验值。
- Node 官方下载：<https://nodejs.org/dist/v24.19.0/>；按官方 SHASUMS 核对对应平台归档。
- `npm install --global pnpm@12.8.1`。

```sh
make bootstrap       # 校验准确工具版本，安装锁定依赖
make generate        # 由 JSON Schema 重建已提交公共类型
make fmt             # 格式化 Go / TypeScript / 配置与机器契约
make check           # lint、生成一致性、测试、真实双向合同往返及构建
make test-race       # Go 公开边界的竞态检查
```

SQLite 适配器使用锁定的 go-sqlite3 v1.14.52、CGO 和驱动自带 SQLite 3.53.4；需要 C 编译器，当前写 Host 排除仅支持已实测 Linux amd64 本地文件，详见 [SQLite说明](adapters/sqlite/README.md)。

构建生命周期验收目前要求 Linux 和 `python3` 标准库：独立 watchdog 用 Linux subreaper 真实回收继承输出管道的构建子进程，验证截止处理与进程组退出。Python 不参与产品实现或合同编码。

工作区使用根目录一个 Go module 和 pnpm 工作区；基础 make check 不需要外部凭据、数据库或个人环境脚本。Make 固定 `GOTOOLCHAIN=local` 和只读模块解析；`bootstrap` 会对工具版本不符或锁文件缺失报错，不自动升级系统。

`make test-contract` 在临时目录构建 Go 运行器，驱动真实 Go 编码 → TypeScript 解码／编码及反向路径，并比较共同夹具的准确值。CI 运行同一 `make bootstrap` 和 `make check`。`make test-integration` 必跑真实 PG 与文件 SQLite 同版接纳、回滚、重开、两库修订工作和存储专属故障；必须显式设置专用测试库的 LERNA_TEST_POSTGRES_DSN，缺配置／服务硬失败。配置、隔离与清理见 [PG适配器说明](adapters/postgres/README.md)。CI另有锁定PG18.6服务的两库集成／race任务，真实集成使用 -count=1 禁用测试结果缓存，准确远端状态记录在实现进度。完整旧v1恢复还必须安装psql；本地已实测17.11恢复18.6 dump，CI使用与服务相同固定镜像的18.6客户端并验证其取消生命周期。配置与未知创建资源限制见PG说明。切片02已完整退出，历史未知测试资源与进程故障证据限制仍保留；网络能力留给后续切片。

已生成公共类型纳入 Git；仅编辑 Schema 和生成器，不手工修改生成物。`make check` 检测生成物与当前输入的一致性。合同版本 `1.0.0` 的精度与边界见 [contract/README.md](contract/README.md)。实现提交 `23bac17` 的本地检查及 [GitHub CI](https://github.com/ruipengliu/lerna/actions/runs/37141974245) 全部通过，范围与退出证据见[规格](.scratch/lerna-01-command-contracts/spec.md)。其证据覆盖共同合同；持久恢复另见切片02记录，完整 Application SDK 与网络认证待后续切片。
