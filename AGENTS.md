# 项目开发指引

本仓库是 Harness 的 Go/TypeScript monorepo。当前有角色生命周期、诊断 SDK/Web 和内部可靠接纳/持久作业框架；Task、Decision、Operation、WSS/gRPC 与领域恢复仍待后续切片。实际范围见[开发手册](dev/README.md)。

## 开始工作

- 探索代码前阅读根 [CONTEXT.md](CONTEXT.md) 和相关 [ADR](docs/adr/)，沿用领域术语；与既定决策冲突时明确说明。
- 调整目录、接口或依赖方向时阅读[工程方案](docs/architecture/engineering.md)；调整角色装配、事务边界或生命周期时阅读[宿主装配](docs/architecture/deployment.md)。
- 修改领域行为时阅读 [架构入口](docs/architecture/README.md) 中对应模块的设计和验收要求。实现按切片开放能力，未实现入口明确返回不支持。

## 项目目录

完整目录树与依赖图见[工程方案第 4 节](docs/architecture/engineering.md#4-代码目录及依赖方向)。下表固定代码归属；部分目录目前仅有占位说明。

| 位置 | 职责 |
| --- | --- |
| `api/` | 公开 Go 领域值类型与组件 ports |
| `runtime/` | 公开嵌入与装配入口，组合默认实现与替换组件 |
| `cmd/harness/`、`cmd/harnessd/`、`cmd/harness-sim/`、`cmd/harness-migrate/` | CLI、角色宿主、模拟设备、显式迁移；模拟设备待实现 |
| `sdk/go/`、`sdk/ts/` | Go/TypeScript 客户端与扩展辅助 |
| `apps/web/` | React/TypeScript/Vite 参考应用，通过 TS SDK 访问服务 |
| `internal/{orchestrator,brain,execution,memory,security,interaction,collaboration,extensions,evaluation}/` | 九模块的领域规则与所属事实 |
| `internal/{durable,host,wire,transport}/` | 持久原语、宿主生命周期、线协议映射、传输适配 |
| `internal/storage/`、`internal/adapters/`、`internal/platform/` | PostgreSQL/SQLite/内容存储、外部能力驱动、本地及公司平台接入 |
| `contracts/` | Schema、方法登记、Proto、正反向向量的唯一维护源 |
| `migrations/{postgres,sqlite}/` | 按真实本地事务范围组织的两方言 SQL 迁移 |
| `tests/{conformance,integration,faults,quality,capacity,fixtures}/` | 一致性、跨包/跨进程、故障、质量、容量与夹具资产 |
| `dev/`、`packaging/` | 本机配置与进程管理、发行制品与平台装配 |
| `tools/` | 契约校验、生成与工程工具入口 |
| `docs/architecture/`、`docs/adr/`、`CONTEXT.md` | 技术设计、架构决策、领域术语 |
| `.scratch/<feature>/` | 规格、问题与实施记录 |

## 开发规范

### 接口与依赖

- Go 初期只使用根 `go.mod`，`sdk/go` 同属该 module。TS SDK 与 Web 属于 pnpm workspace，分别构建；SDK 保持独立于 React。
- `api` 只公开领域类型与 ports；数据库行、内部 jobs 和生成 Proto 保留在内部实现。公开扩展边界为 `api`、`runtime` 和 SDK，`internal` 不作为稳定扩展 API。
- 领域规则通过声明的 ports 使用存储、网络、平台和供应商能力，具体适配器由装配入口注入。跨领域协作沿用声明的 ports。
- 模块目录、进程角色与事务范围分别设计。同库协调显式传递受限 Tx，跨库沿用持久交接；领域目录不逐一拆成服务。

### 契约与生成

- 修改协议时同步维护根 [contracts](contracts/README.md) 的机器资产与 [架构契约](docs/architecture/contracts/README.md) 的规范正文，补齐适用正反例；使用同版契约验证实现与 SDK。
- DTO、SDK 类型和方法映射从固定契约生成。修改源契约或生成器后重新生成，保持产物可复现；生成文件由生成流程维护。
- 实现线协议入口时，按[协议编码规则](docs/architecture/contracts/protocol.md)在普通反序列化之前校验原始字节；类型生成和 Schema 校验之外的领域规则仍由负责模块裁决。
- 当前工具入口见 [tools/README.md](tools/README.md)。SQL 使用固定 sqlc，修改源后运行 `make sql-generate`；Go 线协议生成待引入，Proto 检查产物在忽略的 `dev/.state/generated/proto/`。

### 持久化与外部调用

- PostgreSQL/SQLite 使用显式 SQL、独立查询与迁移。SQL 生成物保留在对应存储适配器，迁移通过显式受控步骤执行，角色启动不自动执行业务迁移。
- 修改持久框架先读[接入约束](internal/durable/README.md)：领域只取 Tx/Work，Engine 与 SQL handle 留在可信装配/适配器；事实、决定与新责任共同提交，闭包不执行外部动作。
- 提交结果未知或外部答复丢失时，沿原 Command/Task/Operation 身份恢复和核对。数据库驱动及供应商 SDK 的透明重试不得重复可能已提交的业务或外部效果。
- 内容存储与受管文件效果目标分别维护。恢复使用原数据根与原账本；依赖缺失时保持业务不接纳。
- 依赖版本由 manifests、锁文件和 Docker 镜像摘要维护，变更时同步更新。凭据与运行状态保存在忽略的 `dev/.env`、`dev/.state/`，日志仅输出有限诊断。

## 本地开发命令

在仓库根执行，环境要求、配置与端口见[开发手册](dev/README.md)。

```sh
make setup           # 安装固定依赖，首次准备凭据与数据目录
make dev             # 五个本机后端进程 + Vite + Docker PostgreSQL
make dev-single      # 显式单体 + Vite，独立 SQLite；与多进程分别启动
make dev-stop        # 停止当前本机开发进程组
make deps-up         # 只启动 Docker PostgreSQL
make deps-down       # 停止中间件，保留数据卷
make build           # 构建 Go 命令、TS SDK 与 Web
make check           # Go race/vet、TS 类型及静态契约检查
make contracts-check # 单独运行静态契约与 Proto 检查
make durable-check   # 真实 PG/SQLite、独立进程与故障/小负载证据
make sql-check       # 独立 SQL 方言生成差异
make migrate-postgres # 显式升级原 PG；SQLite 用 make migrate-sqlite（先停单体）
```

默认角色为 gateway、application、两个 worker、executor；[topology.json](dev/topology.json) 是脚本与前端代理共用的实例清单。中间件由 Compose 提供 PostgreSQL。

本机管理入口只监听回环地址，使用 `lerna-dev-status/1` 诊断。存活、依赖可用与业务就绪分别判断；当前 `/health/ready` 始终为 503。停止开发进程后数据库继续运行，数据清理须明确区分停容器与删卷。

## 验证要求

- Go 规则单元测试就近放在源码旁，TS 规则测试同样就近组织；跨包、跨进程和验收资产放 `tests/` 对应目录。
- 代码变更完成适用的构建与检查；共用入口为 `make build`、`make check`。涉及事务、领取竞争或恢复时运行 `make durable-check`，取得真实 PostgreSQL/SQLite 及适用多进程证据；普通 Go 检查跳过的 PG 不算通过。
- 契约变更运行 `make contracts-check`。静态向量、Proto 编译与本地探测分别报告，业务互操作、故障恢复和生产容量按对应验收规则取证。
- 文档变更检查本地链接、锚点与 Markdown 结构；架构及当前资产使用 `python3 docs/architecture/validation/check_documents.py`。该脚本尚未扫描根 `AGENTS.md`，修改本文件时另行检查其引用。

## 让输出易于理解和使用

以 ISO 24495-1:2023 的简明语言原则为指导，让目标读者获得所需信息、快速找到信息、准确理解信息，并能据此行动。中文表达可借鉴 ASD-STE100 的清晰写作方法，但不机械套用其英文词典和语法规则。

### 内容与结构

- 先明确读者要解决的问题、已有知识和下一步行动，据此选择内容与细节深度。
- 先给核心结论，再说明关键依据、适用条件、假设和限制。明确区分事实、推断、建议和待验证事项。
- 按读者理解和使用信息的顺序组织内容。先讲整体，再展开细节；使用含义明确的标题，将相关信息放在一起，避免重复和来回跳转。
- 技术设计应让开发者找到模块职责、数据及存储、接口契约、关键流程与时序，以及异常处理。不要让读者自行猜测业务规则和责任边界。

### 语言与术语

- 使用短句、明确主语和具体动词。一句话尽量表达一个主要意思，避免连续嵌套条件和抽象名词堆叠。
- 同一概念使用同一术语。必要术语首次出现时给出定义；区分易混淆的概念，不用多个近义词指代同一对象。
- 保留技术精度、关键前提和例外。简明不等于省略，也不以固定句长或字数代替清晰度判断。
- 删除重复、空泛和无助于理解的内容。通过具体示例解释抽象规则，避免仅用另一组术语解释术语。

### 表达形式

- 根据理解任务选择形式，不为展示能力而增加产物：
  - 结构、关系和流程：优先使用图示，并标明关键角色、方向和边界。
  - 字段、属性和规则对照：使用结构一致的表格。
  - 参数变化、方案比较和探索：交互能明显降低理解成本时，使用小型交互 HTML。
  - 时间变化、操作过程和逐步推导：动态表达明显优于静态图文时，使用动画或讲解视频。
- 简单问题直接用文字回答。复杂问题选择解释效果好、制作和使用成本合理的形式。
- 必要时制作小而专用的解释工具。优先复用现有工具，不为一次说明引入通用框架或改动无关代码。

### 交付与检查

- 图示和解释工具应附简短结论、查看方式及必要操作说明。标明演示中的假设和简化，避免将示意结果当作实际验证结果。
- 检查文字、图示、示例和交互结果是否一致；确保交付物可打开、可阅读，关键操作可用。未验证的部分应明确说明。
- 从读者角度复核：能否找到所需信息、理解关键关系，并完成预期判断或行动？重要文档尽可能通过实际读者反馈验证，而非只检查字数和格式。
- 未经完整评估，不宣称符合某项标准，也不以任意百分比表示合规程度。

## Agent skills

### Issue tracker

创建或读取规格、问题和实施票据时遵循 [issue-tracker](docs/agents/issue-tracker.md)：规格位于 `.scratch/<feature>/spec.md`，票据逐项放在该目录的 `issues/` 下。

### Triage labels

记录问题分诊状态时使用五个固定角色：`needs-triage`、`needs-info`、`ready-for-agent`、`ready-for-human`、`wontfix`；含义见 [triage-labels](docs/agents/triage-labels.md)。

### Domain docs

本仓库使用单一领域上下文：根 `CONTEXT.md` 和 `docs/adr/`。探索、建模或评审领域概念与决策时遵循 [domain](docs/agents/domain.md)。
