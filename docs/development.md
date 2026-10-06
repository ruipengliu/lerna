# 开发规范

| 日期 | 修订说明 |
| --- | --- |
| 2026-10-05 | 初版：技术栈、目录骨架、依赖规则、工具链与命令、测试、代码风格、协议与生成代码、Git 工作流、设计文档与代码的关系。 |
| 2026-10-06 | 按[第五轮评审处理记录](review/disposition.md)修订：新增 3.1 核心模块的包结构（调用方声明端口、`core/durable` 声明事务上下文、核心模块之间只允许导入 `core/durable`）（X-06）；骨架增加受信组件的位置 `infra/hosting/`、`infra/rules/`、`infra/egressio/<供应商>/`（X-08）；测试分为契约场景层与进程内装配层，增加故障点标注（X-20、DV-05）；Go 目录命名建议（DV-04）。 |

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
infra/                      受信实现：postgres/、sqlite/、clock/、keys/、egressio/、hosting/、rules/
  egressio/<供应商>/         出口基本操作与供应商编码（模型、API 的请求编码）
  hosting/                  受信宿主：捕获推理的实际读取、封存模型调用描述
  rules/                    受信解释规则（执行证据的终局）与核验规则（条件是否满足）
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

模块目录与设计文档目录一一对应，例如 `core/ledger/` 的设计在 `docs/architecture/core/ledger/README.md`。目录名用英文小写；Go 代码目录建议用一个简短的英文单词，因为 Go 包名不能含短横线（目录和导入路径本身可以）。

`infra/hosting/`、`infra/rules/` 和 `infra/egressio/` 下的供应商编码属于受信实现（[分层与模块第 2 节](architecture/layers.md#2-划分原则)）：随受审查的版本发布，不随 `defaults/`、`adapters/` 中的第三方实现一起替换。普通适配器只能提交请求描述、引用原始观察和提交解释建议，通过公共契约使用这些组件。

## 3 依赖规则

依赖方向是[分层与模块 R6](architecture/layers.md#7-依赖与调用规则)的要求，由 golangci-lint 的 depguard 规则在 `make lint` 中自动检查：

| 目录 | 可以依赖 | 不得依赖 |
| --- | --- | --- |
| `contracts/` | 标准库、Protobuf 运行时 | 其他任何本仓库目录 |
| `core/<模块>/` | `contracts/`、标准库、`core/durable` 的导出包（事务上下文、命令回执、待办）、本模块声明的端口 | `infra/`、`adapters/`、`defaults/`、`platform/`、`cmd/`；数据库驱动；网络框架；除 `core/durable` 以外的其他核心模块；任何模块的 `internal/` |
| `core/durable/` | `contracts/`、标准库 | 其他任何核心模块 |
| `infra/` | `core/<模块>` 的导出包（为了实现其中声明的端口）、`contracts/`、数据库驱动等 | `adapters/`、`defaults/`、`platform/`；任何模块的 `internal/` |
| `defaults/`、`adapters/` | `contracts/` | `core/` 的内部实现 |
| `platform/` | `contracts/`、`core/<模块>` 导出包中的命令与查询 API | 端口的实现、任何模块的私有记录或 `internal/` |
| `cmd/` | 全部；只做装配，不写业务规则 | — |

### 3.1 核心模块的包结构

[ADR 0003](adr/0003-replacement-classes-and-assembly.md) 的"内部接口"指不进入 SDK、由受信实现实现的接口，与 Go 的 `internal/` 目录不是一回事。代码中这样落实：

| 位置 | 放什么 |
| --- | --- |
| `core/<模块>/`（导出包） | 模块的命令与查询 API；本模块声明、要求别人实现的**端口**（导出的接口类型），包括存储端口和对其他核心模块的依赖端口 |
| `core/<模块>/internal/` | 实现细节，只有本模块可以导入 |
| `core/durable/` | 裁决域唯一的事务上下文类型（只携带事务身份、用户、持久档位和提交钩子，不提供通用的表访问）、命令回执和待办工作的接口 |

规则：

- **调用方声明端口，装配入口注入实现。**例如准入时任务编排需要授权的"核验并占用"、预算的预留、会话的确认消费、内容的使用检查，就在 `core/tasks` 中声明这几个端口，由 `core/grants` 等模块的导出类型实现，在 `cmd/` 中连接；`core/tasks` 不导入 `core/grants`；
- 端口方法以 `core/durable` 的事务上下文为参数，各模块用它加入同一个裁决域事务，不各自开事务；
- 核心模块之间唯一允许的导入是 `core/durable`，因此不会成环；
- `infra/` 实现各模块声明的端口；`platform/` 只调用导出包中的命令与查询 API。

这些规则写进 `.golangci.yml` 的 depguard 配置；违反时 `make lint` 失败，导入他模块 `internal/` 时编译失败。

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
| 契约场景：只通过公共契约命令驱动，断言持久记录和模拟目标实际收到的调用 | `conformance/` 中与语言无关的场景定义（输入、故障点、期望的责任记录和目标观察），由 Go harness 执行 | `make test-fault` |
| 故障注入：崩溃、回执丢失、竞争、迟到效果 | `conformance/fault/`，用构建标签 `fault` 隔开 | `make test-fault` |

**标注规则编号。**测试函数上方用一行注释写明它验证的规则，格式固定：

```go
// 规则：G1、开始-2
func TestSafeResendSkipsAdmissionGeneration(t *testing.T) { ... }
```

可以标注的编号：不变量 G1–G12、依赖规则 R1–R7、门禁条件（如"准入-6""开始-2""完成-5"，见[核心契约 2.6](architecture/core/contracts/README.md#26-四道门禁)）、专项验收 V1、V3、V4。

故障注入测试另起一行标注**故障点**，格式为 `// 故障点：出口/P5 提交后`，故障点的名字取自[故障注入清单](architecture/verification/fault-injection.md)。故障点是位置，不是规则，两行分开写。

**两层测试。**契约场景层只通过公共契约命令驱动，场景定义（输入、故障点、期望的责任记录和目标观察）与实现语言无关，将来手机端若有第二份核心实现，必须通过同一批场景；进程内装配层用于检查 Go 实现的内部细节。M1 的 harness 先实现前一层，再补后一层。

涉及出口、授权、预算或持久记录的包（`core/egress`、`core/grants`、`core/budget`、`core/durable`、`core/ledger`、`infra/postgres`、`infra/sqlite`），每个包至少要有一个带规则标注的测试，`make check` 会检查。这是[项目目标第 12 节](architecture/project-goals.md#12-如何使用本文)的要求。

**测试的判据。**涉及外部效果的测试，检查持久记录和模拟目标实际收到的调用次数，不只看函数返回值。验收只认运行证据。

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
