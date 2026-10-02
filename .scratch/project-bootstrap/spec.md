# 工程骨架与本地开发装配

状态：用户于 2026-10-02 确认全部推荐，工程骨架和本机验证完成。

## 已确认选择

1. 在当前 monorepo 根目录落实工程方案，交付可编译、可启动的 Go/TypeScript 骨架、角色入口、健康检查、退出处理和开发脚本；业务状态机随后续切片实现。
2. 默认开发装配为本机网关、应用、两个 worker、执行宿主及 Vite；PostgreSQL 18 通过 Docker Compose 启动。本地 SQLite 单体保留为显式入口。
3. Schema、Proto 和正反向向量迁到根 `contracts/`，规范正文留在 `docs/architecture/contracts/`；同步当前引用与验证器，保持一份机器维护源。

## 实施边界

沿 [工程方案](../../docs/architecture/engineering.md#layout) 建目录。Go module 根据仓库 origin 使用 `github.com/ruipengliu/lerna`；TypeScript 包使用 `@lerna/sdk` 与 `@lerna/web`。角色管理入口只监听本机回环地址，诊断协议不是 harness/1 业务接口。

骨架的存活与依赖探测可用；业务接纳始终关闭，ready 返回未就绪。两名 worker 进程不代表已经实现 JobStore 领取竞争。数据库迁移仍是显式控制动作，业务表尚未建立。当前不运行付费模型或搜索。

仅部署 PostgreSQL；内容字节和文件效果分别使用本地目录，开发固定身份待业务接入。沿既有架构不加入 Redis、MQ、MinIO、IdP、Kubernetes 或监控集群。

## 验收

- Go 构建、vet 及适用生命周期/配置测试；TypeScript 检查与 Vite 构建。
- 所有现有静态契约校验从新路径运行，Proto 编译及封装检查通过。
- Docker PostgreSQL 健康、真实 SQL 和受限应用角色可核查；容器重建后命名卷数据保留。
- 五角色和 Vite 实际启动；存活与依赖探测成功、业务 ready 保持未就绪、SIGTERM/中断能有界退出。
- SQLite 显式单体实际打开，WAL/FULL/外键及单实例锁可核查。

## Comments

本轮没有新增领域术语，沿用根 CONTEXT.md；默认开发入口与机器资产位置写入工程方案和开发手册。

## 实际验证记录（2026-10-02）

环境：macOS 27.0.1 / arm64，Go 1.27.1，Node 25.9.0，pnpm 10.7.0，Python 3.9.6，Docker Engine 29.8.1 / Compose v5.5.1；PG 18.6 容器运行于 linux/arm64。精确依赖见 go.mod/go.sum、pnpm-lock.yaml、tools/requirements.txt 和 Compose 镜像摘要。

| 检查 | 实际结果 |
| --- | --- |
| `make setup` | 冻结安装通过；原本机密码保留，未写入 Git |
| `make build` | 三个 Go 命令、TS SDK 和 React/Vite 构建通过 |
| `make check` | Go race、vet、TS 检查及全套静态契约检查通过 |
| 机器资产迁移 | 86 份 JSON 与 HEAD 原资产逐字节一致；Proto 仅调整 Go 生成包路径 |
| 文档及当前位置 | 89 份 Markdown、2179 个本地链接、112 个 Mermaid 块零结构错误；222 个当前 UML sourceDoc 可解析 |
| Proto 静态封装 | 2 项 RPC 描述符、16 项 Call 请求、22 项响应和 57 项 ChannelFrame JSON 往返通过 |
| 默认多进程 | 网关、应用、两个 worker、执行宿主和 Vite 实际启动；五个 live/startup 均 200，ready 均 503 |
| PostgreSQL 中断 | 五宿主仍 live=200、startup=503，当前数据库依赖不可用；恢复后可连接原库 |
| PostgreSQL 首次初始化 | 隔离的临时新卷通过 SQL 自动初始化并以受限身份连通；该临时项目及卷已清理 |
| PostgreSQL 卷与权限 | 容器 force-recreate 后测试行仍在；应用 DML 可用、公共 schema 建表被拒绝；superuser/建库/建角色/复制/BYPASSRLS 全部为 false |
| SQLite 单体 | 实际启用 WAL/FULL/foreign_keys；第二宿主遭 OS 锁拒绝；重启沿原文件读到测试行 |
| 退出与故障清理 | SIGTERM 整组退出；`make dev-stop` 返回前端口释放；单个 worker 意外退出后五宿主和 Vite 全部清理 |
| 浏览器页面 | IAB 实际显示多进程 5/5、数据库中断 0/5 和 SQLite 单体 1/1；重新加载后无应用 error/warn |

初始化修复：第一次挂载 shell 初始化脚本在 macOS 上因执行权限失败，原数据卷保留；改用 SQL 文件并显式补齐未完成初始化。现有 coverage.md 的 56 个已删除 .draft 来源断链来自 HEAD；来源列改为历史文本，当前维护位置继续链接，原路径/摘要留在 source-map 和 Git 历史。

本轮 PostgreSQL/SQLite 探针表已清理。最终保留默认多进程与 PostgreSQL 运行，使用 `make dev-stop` 和 `make deps-down` 分别停止本机进程及容器。业务状态机、JobStore 竞争、业务网络互操作、数据库故障恢复、Linux 构建和生产验收尚未执行，不以本轮骨架运行宣称通过。
