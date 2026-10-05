# 开发规范

| 日期 | 修订说明 |
| --- | --- |
| 2026-10-05 | 初版：技术栈、目录骨架、依赖规则、工具链与命令、测试、代码风格、协议与生成代码、Git 工作流、设计文档与代码的关系。 |

- 状态：已采纳
- 读者：写代码和评审代码的开发者与 Agent
- 上游：[ADR 0004 开发语言与主要技术栈](adr/0004-language-and-stack.md)、[分层与模块](architecture/layers.md)、[ADR 0003 替换资格与装配](adr/0003-replacement-classes-and-assembly.md)

本文规定代码放在哪里、能依赖什么、怎样测试和提交。设计文档怎样写，见[文档规范](architecture/conventions.md)。代码只实现设计文档写明的规则；需要新规则或改变规则时，先改设计文档，再改代码。

## 1 技术栈

| 部分 | 选择 | 开始阶段 |
| --- | --- | --- |
| 核心、受信实现、宿主、命令行 | Go，单一模块 `github.com/ruipengliu/lerna` | M1 |
| 公共契约 | Protobuf，由 buf 检查和生成代码 | M1 |
| 存储 | 云端 PostgreSQL；端侧和 M1 本地 SQLite | M1 |
| SDK、浏览器界面 | TypeScript，pnpm workspace | M3 |

理由和备选见 [ADR 0004](adr/0004-language-and-stack.md)。

## 2 目录骨架

下面是完整骨架。**不提前创建空目录或占位文件**，写到哪个模块，再建哪个目录。

```
go.mod                      单一 Go 模块；用 toolchain 指令固定 Go 版本
Makefile                    常用命令，见第 4 节
.golangci.yml               静态检查与依赖方向规则
contracts/                  公共契约：跨进程命令、稳定引用、可替换部分的接口、线协议
  proto/                    .proto 源文件，结构定义的唯一来源
  gen/go/                   生成的 Go 代码，不手改
core/                       核心模块，一个模块一个目录
  durable/  tasks/  sessions/  grants/  budget/
  ledger/                   执行管理
  content/  trace/
  egress/                   出口闸门
  <模块>/internal/          模块私有代码，其他模块不可导入
defaults/                   默认实现：reasoner/、memory/
adapters/                   执行与交互适配器：api/、file/、gui/、agent/、interaction/
infra/                      受信实现：postgres/、sqlite/、clock/、keys/、egressio/
  <实现>/migrations/        数据库迁移脚本
platform/                   平台服务：gateway/、extensions/、eval/
cmd/                        装配入口，一个可执行程序一个目录
  lernad/                   云端服务
  lerna/                    命令行
conformance/                跨实现的一致性测试
  fault/                    故障注入测试
sdk/typescript/             TypeScript SDK（M3 起）
apps/web/                   浏览器界面（以后）
docs/                       设计文档、ADR、评审与调研
```

模块目录与设计文档目录一一对应，例如 `core/ledger/` 的设计在 `docs/architecture/core/ledger/README.md`。目录名用英文小写，多个单词用短横线。

## 3 依赖规则

依赖方向是[分层与模块 R6](architecture/layers.md#7-依赖与调用规则)的要求，由 golangci-lint 的 depguard 规则在 `make lint` 中自动检查：

| 目录 | 可以依赖 | 不得依赖 |
| --- | --- | --- |
| `contracts/` | 标准库、Protobuf 运行时 | 其他任何本仓库目录 |
| `core/<模块>/` | `contracts/`、标准库、本模块声明的内部接口 | `infra/`、`adapters/`、`defaults/`、`platform/`、`cmd/`；数据库驱动；网络框架；其他模块的 `internal/` |
| `infra/` | `core/` 声明的内部接口、`contracts/`、数据库驱动等 | `adapters/`、`defaults/`、`platform/` |
| `defaults/`、`adapters/` | `contracts/` | `core/` 的内部实现 |
| `platform/` | `contracts/`、`core/` 的公开接口 | 任何模块的私有记录或 `internal/` |
| `cmd/` | 全部；只做装配，不写业务规则 | — |

外部测试包（`package <模块>_test`）为验证已确认的进程内装配切面，可以导入共享的 `cmd/assembly` 和公共契约。此例外必须在 depguard 中按具体测试文件列出，并用独立白名单约束；不允许导入相邻模块的私有代码。生产文件仍遵守上表。

第三方依赖越少越好：

- 新增依赖必须在 PR 中说明理由、替代方案和维护状况；
- `core/` 和 `contracts/` 只依赖标准库和极少数基础库（例如 Protobuf 运行时）；
- 数据库驱动、网络框架、模型供应商 SDK 只能出现在 `infra/`、`adapters/` 和 `cmd/`；
- 不得用通用的表访问或万能事件总线绕过事实的写入方（R3、R6）。

## 4 工具链与常用命令

- Go 版本在 `go.mod` 中用 `toolchain` 指令固定，升级单独提交；
- 格式化用 `gofmt` 和 `goimports`；静态检查用 golangci-lint，配置在 `.golangci.yml`；
- Protobuf 用 buf 做格式检查、破坏性变更检查和代码生成。

| 命令 | 作用 |
| --- | --- |
| `make fmt` | 格式化全部 Go 代码 |
| `make lint` | 静态检查，含依赖方向（depguard）和 buf 检查 |
| `make test` | 单元测试，带 `-race` |
| `make test-fault` | 故障注入测试（`conformance/fault/`） |
| `make gen` | 由 `contracts/proto/` 生成代码 |
| `make check` | 提交前的全部检查：fmt、lint、test、规则标注检查、生成代码是否最新 |

`Makefile` 和 `.golangci.yml` 在 M1 建立代码骨架时一并创建。

## 5 测试

**默认测试先行。**先写一个会失败的测试描述期望行为，再写实现让它通过。

**测试放在哪里：**

| 测试 | 位置 | 运行 |
| --- | --- | --- |
| 单元测试 | 与代码同目录的 `_test.go` | `make test` |
| 一致性测试：同一接口的不同实现必须通过同一套用例 | `conformance/` | `make test` |
| 故障注入：崩溃、回执丢失、竞争、迟到效果 | `conformance/fault/`，用构建标签 `fault` 隔开 | `make test-fault` |

需要在模块目录运行的黑盒一致性测试，使用外部测试包，通过 `cmd/assembly` 提交公共命令、查询持久记录；生产宿主使用同一装配。数据库连接配置测试可以直接核验受信存储的公开设置。

**标注规则编号。**测试函数上方用一行注释写明它验证的规则，格式固定：

```go
// 规则：G1、开始-2
func TestSafeResendSkipsAdmissionGeneration(t *testing.T) { ... }
```

可以标注的编号：不变量 G1–G12、依赖规则 R1–R7、门禁条件（如"准入-6""开始-2""完成-5"，见[核心契约 2.6](architecture/core/contracts/README.md#26-四道门禁)）、专项验收 V1、V3、V4。

涉及出口、授权、预算或持久记录的包（`core/egress`、`core/grants`、`core/budget`、`core/durable`、`core/ledger`、`infra/postgres`、`infra/sqlite`），每个包至少要有一个带规则标注的测试，`make check` 会检查。这是[项目目标第 12 节](architecture/project-goals.md#12-如何使用本文)的要求。

**测试的判据。**涉及外部效果的测试，检查持久记录和模拟目标实际收到的调用次数，不只看函数返回值。验收只认运行证据。

### 5.1 持久化故障测试

`cmd/assembly.Open` 在返回前恢复当前已可处理的待办命令。命令行每次启动共用此流程，因此启动可能推进先前受理的命令；`QueryReceipt` 本身仍为只读。

故障构建通过 `sqlite.WithFault(ctx, point, mode)` 给单次调用链配置一次性故障。`CrashBeforeCommit` 在提交前直接退出子进程，`CrashAfterCommit` 在提交成功、回执返回前直接退出，`LoseReceipt` 提交后返回提交结果未知。两种崩溃均跳过 `defer` 和存储关闭，父进程以同一 SQLite 文件重启。

当前持久化点在 `infra/sqlite/hooks_fault.go` 的 `FaultPoints` 登记：

| 名称 | 原子保存的责任 |
| --- | --- |
| `content.stage` | 输入正文与来源；尚未承诺任务受理 |
| `durable.submit` | SUBMITTED 回执、正文引用与决定待办 |
| `durable.decide` | 会话输入、会话头、任务关联、任务、原决定与工作完成 |
| `durable.jobs` | 领取、续租、工作控制的原决定与围栏 |
| `tasks.planning` | 可信条件集、快照请求或不可变提议与原决定 |
| `grants.configure`、`budget.configure` | 受信根授权或用户／任务额度与原决定 |
| `tasks.admit` | 使用记录、两级预留、提议消费、准入意图、交接 outbox、待办与原决定 |
| `ledger.accept` | 独立执行管理域的原交接回执、动作与执行待办 |
| `tasks.handoff_receipt` | 源域保存对方原回执并在领取围栏下完成待办 |

新增事务必须传入固定名称、登记到同一表，并补充对应故障用例。故障套件检查未登记的事务和无调用点的登记项，普通构建只编译空边界，不包含故障计划、登记表或配置 API。`make test-fault` 仅运行 `conformance/fault/` 中带 `fault` 标签的测试。

这些用例验证进程崩溃恢复，不等于通过掉电或存储故障验证。本地档的掉电资格仍由 ADR 0001 的独立验收决定。

## 6 代码风格

- 标识符用英文。公共对象的字段名、状态名和错误码沿用[核心契约](architecture/core/contracts/README.md)，不另起名字；
- 注释用中文，术语与[项目目标第 3 节](architecture/project-goals.md#3-核心术语)一致；
- 错误按核心契约 3.2 的分类返回，不吞掉"提交结果未知""效果未知"这类结果；
- `context` 取消只是停止信号，不证明 goroutine 或外部动作已经结束；涉及出口和恢复的代码不得把取消当作"未发生"；
- 外部调用不得放在数据库事务内；先持久记录，再执行外部 I/O（G3、出口闸门 4.1）。

## 7 协议与生成代码

- `.proto` 源文件只放在 `contracts/proto/`；生成的代码放 `contracts/gen/go/`，不手改；
- 修改协议后运行 `make gen`，并把生成结果一起提交；`make check` 会检查生成代码是否最新；
- 字段编号和枚举编号永不复用；破坏性变更由 buf 检查拦截，必须按[核心契约 7.6](architecture/core/contracts/README.md)的版本规则处理。

## 8 Git 工作流

- 按功能开分支，经 PR 合并到 `main`；
- 提交说明用 Conventional Commits 前缀（`feat:`、`fix:`、`docs:`、`test:`、`refactor:`、`build:`、`chore:`），正文用英文；
- 一个提交只做一件事；格式化、依赖升级、生成代码的大范围变化单独提交；
- Agent 只在被明确要求时才提交或推送。

## 9 设计文档与代码

- 写一个模块之前，先读它的设计文档和上游（项目目标、分层与模块、核心契约）；
- 代码与设计文档冲突时，以设计文档为准；发现设计文档有问题，先改文档并记录原因，再改代码；
- 跨模块的规则只在权威位置写一处（见[文档规范 5](architecture/conventions.md#5-写作与链接规则)），代码注释引用规则编号，不复述规则全文。

### M1 目标恢复与领取参数

目标工作使用 30 秒租约；持久工作命令允许 1–60000 毫秒，单次最多领取 100 项。宿主每个进程实例使用新身份。共享装配启动先恢复原目标责任：存在尚未过期的领取时等待自然到期，最多等待 65 秒，超时返回错误并保留责任。因此命令行启动可能等待旧工作者的租约，不提前抢占、不要求用户手动执行 recover。等待不持有数据库事务；最终资格仍按取得写锁后的权威时间核验。

持久工作同一后端语义套件位于 `conformance/durable`：`runSemantics` 接收公开服务接口工厂，`leaseTimeAfterLock` 接收同一后端的两个连接和占锁事务接口。后续 PostgreSQL 实现复用这些行为用例。宿主专用的 `JobCommand` 不暴露给可替换适配器；CONTROL 的模块选择来自受信模块调用，不能由外部请求直接转发。任务编排的 DELIVER_HANDOFF 先查询原接收方命令，再按原身份投递；执行管理在独立域事务保存接纳回执、动作和待办，源域最后保存原回执并完成工作。启动恢复等待自然租约到期，不重建动作。通用 PROGRESS／CONTROL 不得完成或封闭尚无回执的交接，也不得封闭尚无决定的 DECIDE_GOAL。

准入使用事务内业务保存点：任何参与者的确定拒绝撤回使用、预留、确认消费和意图，再保存不可变拒绝回执。受信模板、能力配置、根持续授权和两级预算由固定 host 身份提供；推理只产生提议。M1 此切片只接纳单步目标动作；记忆、子任务、模型自报准备／收尾类别，以及需要尚未实现的确认流程的动作明确拒绝。金额为 USD_MICRO 整数，持续授权仍建立每动作使用记录；预留在交接后保持，不代表费用已结清。
