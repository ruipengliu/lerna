# Lerna

Lerna 是按明确合同构建的 Agent 执行框架。领域规则见 [CONTEXT.md](CONTEXT.md)，模块及运行设计见 [docs/architecture](docs/architecture/README.md)。按[实现切片](.scratch/lerna-implementation/README.md)逐步交付；切片 01 已完成：当前可执行范围为共同信封、固定回执、command.get 受信注入读取、准确版本协商及 Go / TypeScript 严格编解码，尚无持久运行服务。

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

工作区使用根目录一个 Go module 和 pnpm 工作区，不需要外部凭据、数据库或个人环境脚本。Make 固定 `GOTOOLCHAIN=local` 和只读模块解析；`bootstrap` 会对工具版本不符或锁文件缺失报错，不自动升级系统。

`make test-contract` 在临时目录构建 Go 运行器，驱动真实 Go 编码 → TypeScript 解码／编码及反向路径，并比较共同夹具的准确值。CI 运行同一 `make bootstrap` 和 `make check`。目前未实现数据库／网络能力，故没有 `test-integration` 空目标；后续引入实际能力时一起添加。

已生成公共类型纳入 Git；仅编辑 Schema 和生成器，不手工修改生成物。`make check` 检测生成物与当前输入的一致性。合同版本 `1.0.0` 的精度与边界见 [contract/README.md](contract/README.md)。实现提交 `23bac17` 的本地检查及 [GitHub CI](https://github.com/ruipengliu/lerna/actions/runs/37141974245) 全部通过，范围与退出证据见[规格](.scratch/lerna-01-command-contracts/spec.md)。这不表示完整 Application SDK、持久恢复或网络认证已经实现。
