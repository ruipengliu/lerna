# 开发规范

| 日期 | 修订说明 |
| --- | --- |
| 2026-10-05 | 初版：技术栈、目录骨架、依赖规则、工具链与命令、测试、代码风格、协议与生成代码、Git 工作流、设计文档与代码的关系。 |
| 2026-10-06 | 按[第五轮评审处理记录](review/disposition.md)修订：新增 3.1 核心模块的包结构（调用方声明端口、`core/durable` 声明事务上下文、核心模块之间只允许导入 `core/durable`）（X-06）；骨架增加受信组件的位置 `infra/hosting/`、`infra/rules/`、`infra/egressio/<供应商>/`（X-08）；测试分为契约场景层与进程内装配层，增加故障点标注（X-20、DV-05）；Go 目录命名建议（DV-04）。 |
| 2026-10-07 | 合并第五轮评审与 M1 实施记录：保留调用方声明端口的包结构、外部装配测试白名单，以及实际 fault 包范围和恢复验收入口。 |
| 2026-10-07 | 按变更范围选择检查，增加文档与日常代码检查入口；明确结果复用、先评审后完整验收，以及合并和工作树清理的完成条件。 |

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

外部测试包（`package <模块>_test`）为验证已确认的进程内装配切面，可以导入共享的 `cmd/assembly` 和公共契约。此例外必须在 depguard 中按具体测试文件列出，并用独立白名单约束；不允许导入相邻模块的私有代码。生产文件仍遵守上表。

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
| `make lint` | 普通与 `fault` 构建的 Go 静态检查（含 depguard），以及 buf 检查 |
| `make lint-go` | 普通与 `fault` 构建的 Go 静态检查，不运行故障矩阵 |
| `make lint-proto` | Protobuf 格式、lint 和破坏性变更检查 |
| `make test` | 全部普通构建测试，带 `-race` |
| `make test-code CHECK_PACKAGES='./core/ledger/... ./conformance/admission'` | 日常检查指定包的普通构建测试，带 `-race`；未指定包时运行 `./...` |
| `make test-fault` | 全部实际 `fault` 构建测试包（`conformance/` 含根包与 `admission/`，以及 `infra/sqlite/`） |
| `make gen` | 由 `contracts/proto/` 生成代码 |
| `make check-docs` | 检查已跟踪 Markdown 的未暂存及已暂存差异中的空白错误和冲突标记 |
| `make check-code` | 日常代码检查：Go 格式、Go 静态检查、规则标注、普通构建测试；可用 `CHECK_PACKAGES` 限定测试包 |
| `make check` | 完整集成检查：fmt、lint、全部普通及 `fault` 构建测试、规则标注、生成代码是否最新；测试范围不受 `CHECK_PACKAGES` 影响 |

`Makefile` 和 `.golangci.yml` 在 M1 建立代码骨架时一并创建。

### 4.1 检查范围与完成条件

提交或合并前，先按实际差异选择检查。混合变更合并各类检查；已经通过且仍适用的结果按下文复用。

| 变更范围 | 执行的检查 |
| --- | --- |
| 仅文档、注释说明或票据状态，无可执行代码、配置或生成输入变化 | `make check-docs`；核对修改涉及的链接、锚点和语义一致性。Go 文件中的注释变化另做 `make check-fmt` |
| Go 代码或测试 | `make check-code CHECK_PACKAGES='<受影响包及其调用方／公共契约测试包>'`；无法可靠界定影响时保留默认 `./...` |
| Protobuf 或其生成配置、工具版本 | 在代码检查之外运行 `make lint-proto check-gen`；源文件变化先执行 `make gen`，并验证相关编解码与兼容性行为 |
| 持久事务、迁移、同步、授权、预算、出口或恢复行为 | 在代码检查之外执行相关故障切片，覆盖改变的持久化点和公共行为；影响跨模块或无法可靠界定时执行完整 `make test-fault` |
| 检查脚本、Makefile 或检查配置 | 实际执行改变的检查入口；检查命令选择变化时核对完整入口仍保留原范围，选择受影响测试验证。仅编排变化无需重跑业务故障矩阵 |
| 里程碑最终集成验收或发布验收 | 在评审与修复完成后，对最终候选版本运行 `make check`；专项验收的覆盖和判据仍按上游设计执行 |

`make check-docs` 只检查上述 Git 差异，不自动检查链接、语义、已提交的分支差异或未跟踪文件；这些内容在差异评审中核对。设计规则改变时还要评估现有实现是否需要跟进，文档检查通过不代表新规则已由实现满足。

**检查分工与顺序。**实现者完成受影响测试和必要故障切片；合并者检查冲突处理及合并新增的影响；评审者检查需求与规范。评审修复后，由集成者运行最终完整检查。一次运行满足多个票据的验收时，各票据引用同一记录。提交次数、合并次数和票据数量不决定全量检查次数。

**结果复用与重跑。**通过的记录需注明命令、结果、受测版本及必要的环境信息。受测代码、相关配置、依赖、工具和平台未变化时，复用对应结果；后续只有文档、验收说明或票据状态变化时，补做文档检查。失败、相关输入变化或评审发现新问题时，重跑受影响检查；影响不清楚时扩大范围。故障切片用于开发验证，完整验收记录如实标明是否运行全矩阵。性能测量等待相关契约和语料稳定，后续只对影响测量结论的变化重新测量。

**完成与收尾。**所选检查通过、评审问题已处理、记录的覆盖与限制准确时结束验证；最终验收再满足上游退出标准。保留运行记录和覆盖说明即可，额外复核应对应尚未解决的具体问题。合并使用正常 Git 操作，核对提交关系与工作区状态；工作树清理确认分支保留、改动已合入，干净目录直接移除，需要保留的未提交内容先保存。协调记录保留当前状态、下一步和证据链接，历史过程另行归档。

## 5 测试

**默认测试先行。**先写一个会失败的测试描述期望行为，再写实现让它通过。

**测试放在哪里：**

| 测试 | 位置 | 运行 |
| --- | --- | --- |
| 单元测试 | 与代码同目录的 `_test.go` | `make test` |
| 一致性测试：同一接口的不同实现必须通过同一套用例 | `conformance/` | `make test` |
| 契约场景：只通过公共契约命令驱动，断言持久记录和模拟目标实际收到的调用 | `conformance/` 中与语言无关的场景定义（输入、故障点、期望的责任记录和目标观察），由 Go harness 执行 | `make test-fault` |
| 故障注入：崩溃、回执丢失、竞争、迟到效果 | `conformance/` 各实际公共一致性包，用构建标签 `fault` 隔开；存储矩阵在 `conformance/fault/` | `make test-fault` |

需要在模块目录运行的黑盒一致性测试，使用外部测试包，通过 `cmd/assembly` 提交公共命令、查询持久记录；生产宿主使用同一装配。数据库连接配置测试可以直接核验受信存储的公开设置。

**标注规则编号。**测试函数上方用一行注释写明它验证的规则，格式固定：

```go
// 规则：G1、开始-2
func TestSafeResendSkipsAdmissionGeneration(t *testing.T) { ... }
```

可以标注的编号：不变量 G1–G12、依赖规则 R1–R7、门禁条件（如"准入-6""开始-2""完成-5"，见[核心契约 2.6](architecture/core/contracts/README.md#26-四道门禁)）、专项验收 V1、V3、V4。

故障注入测试另起一行标注**故障点**，格式为 `// 故障点：出口/P5 提交后`，故障点的名字取自[故障注入清单](architecture/verification/fault-injection.md)。故障点是位置，不是规则，两行分开写。

**两层测试。**契约场景层只通过公共契约命令驱动，场景定义（输入、故障点、期望的责任记录和目标观察）与实现语言无关，将来手机端若有第二份核心实现，必须通过同一批场景；进程内装配层用于检查 Go 实现的内部细节。M1 的 harness 先实现前一层，再补后一层。

涉及出口、授权、预算或持久记录的包（`core/egress`、`core/grants`、`core/budget`、`core/durable`、`core/ledger`、`infra/postgres`、`infra/sqlite`），每个包至少要有一个带规则标注的测试，`make check-code` 和 `make check` 的 `check-rules` 会检查。这是[项目目标第 12 节](architecture/project-goals.md#12-如何使用本文)的要求。

**测试的判据。**涉及外部效果的测试，检查持久记录和模拟目标实际收到的调用次数，不只看函数返回值。验收只认运行证据。

### 5.1 持久化故障测试

`cmd/assembly.Open` 在返回前恢复当前已可处理的待办命令。命令行每次启动共用此流程，因此启动可能推进先前受理的命令；`QueryReceipt` 本身仍为只读。

故障构建通过 `sqlite.WithFault(ctx, point, mode)` 给单次调用链配置一次性故障。`CrashBeforeCommit` 在提交前直接退出子进程，`CrashAfterCommit` 在提交成功、回执返回前直接退出，`LoseReceipt` 提交后返回提交结果未知。两种崩溃均跳过 `defer` 和存储关闭，父进程以同一 SQLite 文件重启。 同一调用链多次经过同一点时，故障构建可用 `sqlite.WithFaultOnOccurrence(ctx, point, mode, occurrence)` 选择第几次匹配提交阶段；计数只发生在该模式对应的提交前或提交后阶段，`WithFault` 保持第一次命中的语义。`sqlite.FaultTriggered(ctx)` 只用于验证丢失回执已实际触发。

当前持久化点在 `infra/sqlite/hooks_fault.go` 的 `FaultPoints` 登记：

| 名称 | 原子保存的责任 |
| --- | --- |
| `content.stage` | 输入正文与来源；尚未承诺任务受理 |
| `durable.submit` | SUBMITTED 回执、正文引用与决定待办 |
| `durable.decide` | 会话输入、会话头、任务关联、任务、原决定与工作完成 |
| `durable.jobs` | 领取、续租、工作控制的原决定与围栏 |
| `tasks.planning` | 可信条件集、快照请求或不可变提议与原决定 |
| `grants.configure`、`budget.configure` | 根授权签发（含签发确认消费）或用户／任务额度与原决定 |
| `tasks.confirmation`、`grants.confirmation` | 核心生成的动作确认，或授权草稿与签发确认 |
| `sessions.confirmation` | 可信回应或撤回、不可变确认版本与会话事件 |
| `grants.credential` | 不透明出口凭据与原决定 |
| `grants.revoke`、`grants.revocation_receipt` | 撤销受理与原出口封闭责任，或保存对方原回执后的完成状态 |
| `tasks.admit` | 使用记录、两级预留、提议消费、准入意图、交接 outbox、待办与原决定 |
| `ledger.reconcile_request`、`ledger.reconcile_prepare` | 核对责任与持久调度，或单次查询的核心意图、固定身份与额度上限计数 |
| `ledger.reconcile_confirmation`、`tasks.closure_confirmation` | 查询准入命令的确定确认绑定，或用户可核验的核心查询事项 |
| `tasks.closure_admit`、`ledger.reconcile_admission_ack` | 查询使用与预留、完整准入清单及交接，或保存负责方原准入回执 |
| `ledger.reconcile_control`、`ledger.reconcile_pause` | 修订围栏下暂停、恢复与限制，或失败后保留的查询责任 |
| `tasks.operation_progress`、`ledger.progress_ack` | 按当前原动作状态唤醒任务，或原进度通知的接收回执 |
| `ledger.accept` | 独立执行管理域的原交接回执、动作与执行待办 |
| `tasks.handoff_receipt` | 源域保存对方原回执并在领取围栏下完成待办 |
| `tasks.verification` | 本轮冻结、完整准入清单、准确封闭 outbox、工作与原决定 |
| `ledger.completion_seal` | 执行域准确封闭、迟到意图墓碑、已有动作状态与原回执 |
| `tasks.completion_receipt` | 源域封闭回执与领取围栏下的工作完成 |
| `tasks.completion` | 核验裁决、冻结释放、成功 Result 与任务终态；拒绝轮次的唯一继续请求及工作 |
| `tasks.closing` | 非成功关闭依据、全部历史准入封闭意图、预算逐发送后续责任及持久工作 |
| `ledger.task_closure_seal` | 原出口准确封闭、迟到准入墓碑、原动作及全部历史发送后续责任 |
| `tasks.task_closure_receipt` | 非成功封闭源回执与领取围栏下的工作完成 |
| `tasks.close_final` | 独立非成功 Result 与任务终态 |
| `ledger.followup_completion` | 原动作取得可靠终态后由执行管理完成后续责任 |
| `budget.followup_completion` | 原逐发送费用结清或可靠未发送后由预算完成后续责任 |

新增事务必须传入固定名称、登记到同一表，并补充对应故障用例。故障套件检查未登记的事务和无调用点的登记项，普通构建只编译空边界，不包含故障计划、登记表或配置 API。`make test-fault` 实际执行 `go test -race -timeout 120m -tags fault ./conformance/... ./infra/sqlite/...`，覆盖根包的格式故障和 `admission/` 的保存版本、指标端点、实际必要 WAL 备份用例，也覆盖 `infra/sqlite/` 的真实 occurrence 选择器回归，并保留 `conformance/fault/` 的全部 SQL/native 存储切点与负对照。完整验收使用这一范围，保留全部切点和负对照；开发阶段的切片与最终验收时机按第 4.1 节选择。随着 M1 schema 和来源事件增加，包级超时上限设为 120 分钟。

这些用例验证进程崩溃恢复，不等于通过掉电或存储故障验证。本地档的掉电资格仍由 ADR 0001 的独立验收决定。

### 5.2 本地档存储故障验收

`go test -tags fault ./conformance/fault -run '^TestStorage' -count=1 -v` 通过生产装配采集 SQLite VFS 轨迹，再按故障前缀重建存储镜像。与杀进程用例不同，它丢弃、重排或部分保留未同步写入；确认判据来自独立父进程收到的回执。方法、固定构建、准入假设和复现方式见[本地档验收](architecture/verification/local-durability.md)。普通构建保留严格 macOS 同步包装器，但不包含轨迹采集、SQL 负对照或故障注入入口。

## 6 代码风格

- 标识符用英文。公共对象的字段名、状态名和错误码沿用[核心契约](architecture/core/contracts/README.md)，不另起名字；
- 注释用中文，术语与[项目目标第 3 节](architecture/project-goals.md#3-核心术语)一致；
- 错误按核心契约 3.2 的分类返回，不吞掉"提交结果未知""效果未知"这类结果；
- `context` 取消只是停止信号，不证明 goroutine 或外部动作已经结束；涉及出口和恢复的代码不得把取消当作"未发生"；
- 外部调用不得放在数据库事务内；先持久记录，再执行外部 I/O（G3、出口闸门 4.1）。

## 7 协议与生成代码

- `.proto` 源文件只放在 `contracts/proto/`；生成的代码放 `contracts/gen/go/`，不手改；
- 修改协议后运行 `make gen`，并把生成结果一起提交；`make check-gen`（也包含在完整 `make check` 中）检查生成代码是否最新；
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

准入使用事务内业务保存点：任何参与者的确定拒绝撤回使用、预留、确认消费和意图，再保存不可变拒绝回执。受信模板、能力配置和两级预算由固定 host 身份提供；根授权可由受信用户确认后签发，推理只产生提议。M1 此切片只接纳单步目标动作；记忆、子任务、模型自报准备／收尾类别明确拒绝。金额为 USD_MICRO 整数，持续授权仍建立每动作使用记录；预留在交接后保持，不代表费用已结清。

### M1 授权与可信确认命令

`grant-request --command ID --json GRANT_JSON [--session ID]` 提交授权范围，返回确认引用。单次授权使用 `useMode: SINGLE`、`maxAdmissions: 1`，每条权限必须用 `parameterMode: EXACT` 绑定参数内容引用；持续授权显式选择 `ANY` 或 `EXACT`，`maxAdmissions: 0` 表示不限准入次数。授权主体是同用户的明确任务标识；可以先授权未来任务，不会因此创建任务。

`confirmation ID` 通过 `Sessions.ReadCurrentConfirmation` 临时展示核心生成的事项描述、具体参数、绑定摘要与当前版本；正文读取失败时命令明确失败。持久 `Confirmation.Description` 只保存结构字段，`QueryConfirmation` 和 `QueryCurrentConfirmation` 提供不含参数正文的历史审计。参数描述与出口共用实际发送字节选择：优先使用显式 `RawBody`（包括空字节），否则使用 `Text`；有效 UTF-8 以字符串展示，其他字节以 Base64 展示，并携带媒体类型，不截断。用户阅读后用 `confirm --command ID --confirmation ID --revision N --digest DIGEST --decision APPROVE` 批准，或以 `REJECT` 拒绝。`grant-issue --command ID --confirmation ID --revision N` 消费已批准的签发确认。动作确认由 `admission-confirmation --command ID --json REQUEST_JSON` 请求，JSON 包含任务、提议和授权引用；准入时由任务编排消费。两种确认不能交叉消费；批准后仍须重新核验当前事实。

`withdraw-confirmation --command ID --confirmation ID --revision N` 撤回未消费的批准。`grant ID` 查询当前授权及计算出的剩余次数、过期标记和展示状态；这些诊断值不产生使用许可。`revoke-grant --command ID --grant ID` 撤销授权并返回撤销进度引用，`revocation ID` 查询当前收尾状态。`PENDING` 表示尚未取得全部原出口的封闭回执；已过期、已耗尽的授权仍可撤销。准入使用额度不因撤销或后续失败退还。

这些命令使用宿主认证后的 `host` 或 `local-cli` 身份回应；普通输入与模型输出不会被转换为可信批准。CLI 只呈现公共查询结果和转发结构化命令。

启动恢复与 `recover` 会查询并继续原撤销交接，先读取出口原回执，再保存源端确认。所有宿主实例共用数据库对应的出口文件锁；撤销完成必须等待原实际使用退出。发送前封闭保存未发送证明，发送后封闭保留已有结果与迟到可能性，不会重发。封闭证明与仍待结算的预算预留都保留，后续结算不得依赖并不存在的原始发送证据。


### M1 预算与账单命令

`budget` 查询用户预算，`budget TASK_ID` 查询任务预算；两者是包含关系，不能相加作为总费用。输出包含 `limit`、`settled`、`reserved`、`available`、`deficit` 、`billingBlocked` 和 `ceilingViolation`。`budget-configure COMMAND_JSON` 建立额度；`budget-limit COMMAND_JSON` 使用 `AdjustBudgetLimitCommand` 的精确 `expectedRef` 调整额度，保留旧费用和预留。固定身份 `host` 与 `local-cli` 可提交这两类命令。

`budget-version REF_JSON` 查询不可变预算版本。`billing-source SEND_REF_JSON` 查询该物理发送的当前计费来源；`billing-entry REF_JSON` 和 `billing-conflict REF_JSON` 查询原分录或待核对证据。即时费用在 P7 接收时按受信证据结算，原用量回报与回执不会改写。`bill-import COMMAND_JSON` 接受 `ImportBillCommand`，引用由受信宿主登记的原账单内容；外部身份、原发送和请求键必须一致。重复账单不再计费，矛盾的同版账单进入冲突；退款、贷记和不同版本的更正明确不支持。

`budget-release COMMAND_JSON` 转发 `ReleaseReservationCommand`，需要原预留引用与不可变封闭证明。启动恢复和 `recover` 会继续释放已证明从未发送的预留；取消、关闭、超时及 P5 后的封闭都不证明零费用。实际费用超过原上界仍记录，预算显示缺口和上界失效，并拒绝相关的新准入；显式增额不会清除上界失效。费用更新不会改写已经固定的 Result。

### M1 内容登记与宿主派生

`Content.Register` 保存结构化来源、实际取得时间、可选的任务／动作／尝试关联和暂存责任。`ProcessRegistrations` 查询正文持有方的原接纳回执，再核验实际字节并发布；`QueryRegistration` 只返回责任元数据，暂存载荷不随查询泄露。`Content.Read` 返回已发布版本的精确字节，`command.ContentBytes` 同时供确认展示与出口使用。原目标 `Stage` 入口在完成正文交接后才返回，来源命令保持不变。`Delete` 明确返回 `UNSUPPORTED`。

派生宿主依次调用 `PrepareDerivation`、`ReadDerivationInput`、`SealDerivation`、`CommitDerivation` 和 `ProcessRegistrations`。输入字节必须从 `ReadDerivationInput` 返回值取得；封闭集合必须覆盖全部实际输入，额外报告的来源也必须是可用版本。准备返回的责任可用 `QueryDerivation` 查询，其实例标识和代次绑定后续调用；`TakeoverDerivation` 保留原责任并隔离旧实例。模型和适配器不能登记可信原始观察，外部调用仍须经过准入和出口闸门。结构化的上下文快照保存引用；将它们编成提示、摘要或其他正文时，宿主必须使用派生入口。

正文后端在独立 `body` 事务持久保存接纳回执。`assembly.Harness.Bodies.QueryReceipt` 是持有方的只读回执查询，可与内容源域的交接状态独立比较，不返回未发布正文。新增故障点为 `content.register`、`body.accept`、`content.publish`、`content.derivation`、`content.derivation_input`、`content.derivation_takeover`、`content.derivation_seal`、`content.derivation_commit`。原有 `content.stage` 和 `content.observation` 现在保存登记意图；正文持有责任和发布另有独立提交，原始观察发布完成后才交给执行管理。

### M1 取消控制与端点封闭

`input --command ID --session SESSION --task TASK --kind CONTROL --control CANCEL --control-generation N` 保存取消控制并返回原命令回执。`cancellation TASK_ID` 返回 `CancellationView`：原取消命令和清单、任务当前控制、未取得端点回执的 `pendingClosureRefs`、当前动作及 `unresolvedEffectOperationRefs`、原动作的核对责任。封闭回执齐备不代表效果已知；P5 后的未知和费用仍归原发送。`budget TASK_ID` 与 `billing-source SEND_REF_JSON` 继续展示原任务费用和每个历史发送的计费来源。

启动恢复和 `recover` 继续原取消交接，先查询原端点的命令回执。取消与 P4、P5 的三种窗口、晚到原准入、旧领取、历史发送、迟到原始观察和账单在公共装配边界验证；端点内部未发送证明及精确预算释放见 [ADR 0009](adr/0009-cancellation-closure.md)。

### M1 可查询目标的结果核对

`request-reconciliation --json FILE` 接收 `RequestReconciliationCommand`，指定原动作、查询能力、参数、同一份 READ 与 SAVE 授权和核对策略。`reconciliation OPERATION_ID` 查询当前责任；`reconciliation-query --json REF_FILE` 与 `reconciliation-finding --json REF_FILE` 读取不可变查询及结论。`control-reconciliation --json FILE` 按当前修订号暂停或恢复，可显式更新上限与授权。启动和 `recover` 恢复到期工作；未来的等待时间保留在待办中，下次推进时再次检查。普通工作控制不能替代核对控制。

查询是独立的 CLOSURE 动作，进入同一任务的完整准入清单，单独使用授权、预留费用并经过 P4/P5 出口。目标的 GET 回报同时携带查询本身身份与原动作身份；只有可信的读取终态证明才能结清查询的效果责任，原动作的未生效结论还要求原请求再无迟到生效可能。查询自己丢失终态回报时保持 `QUERY_RESULT_UNKNOWN` 暂停，不递归生成查询。原写请求始终不会因此重发。查询与原写各自按独立发送核验账单；效果已确定也不代表费用为零，缺少最终计费证据时继续保留对应预留。

确认型查询授权会先持久保存核心查询意图并暂停。`closure-confirmation --json FILE` 使用该意图的 `workRef` 创建精确事项，随后沿 `confirmation` / `confirm` 可信回应链批准；恢复命令附带批准引用，准入仍重查并原子消费。缺失或无效的初始确认不会消耗准入身份。弱证据保留未知及迟到可能性；全抖动退避与目标 Retry-After 下限、检查次数、时间和费用上限均持久。原始迟到回报独立接纳，更新原动作并以可恢复通知推进任务完成核验。


### M1 模型调用与恢复

受信宿主先通过 `RequestProposal` 保存 `ProposalRequest` 和 `PROPOSE` 工作，再领取工作。请求记录原快照、核心确定的用途及两个独立上限；M1 调用位置和每动作实际发送上限均为 1。`PrepareModelCall` 接受位置、模型设置和内容引用，从原快照补入事实并经内容派生入口读取实际字节；`AdmitModelCall` 将封存描述、授权使用、预算预留、动作及交接意图一起提交。`RunModelCall` 接续这些公开入口，经过普通执行准备和出口闸门发送，不能作为推理持有授权的接口。

M1 参考适配器为 `model-reference-v1`，模型设置使用 `provider: reference`、`encoderVersion: reference-model-v1` 和 `policyVersion: m1-v1`。参数必须为 JSON 对象，工具模式必须为数组，输出上限为 1–65536 token。重复 JSON 字段被拒绝，数值按原十进制精度保留。封存正文包含设置、能力引用、原快照与不可变事实，以及按输入顺序排列的精确正文；二进制字段使用 Base64。出口再次核验正文 SHA-256，不能用摘要代替正文。

`QueryModelCall` 查询原位置并检查内容当前是否可用；同位置更改设置、能力或输入引用返回 `MODEL_POSITION_CONFLICT`。原编码器、策略或正文不可恢复时返回 `PREPARATION_UNRECOVERABLE`。P5 后恢复只收集原发送的观察，不再发送。`COMPLETED`、`REFUSED`、`INCOMPLETE` 和 `INVALID_OUTPUT` 均保存原结果，不自动修复、换供应商或重采样；传输结果未明保存 `UNKNOWN`，原费用责任继续存在。模型正文不能提供可信费用，费用仍由普通计费证据解释器处理。

`SubmitProposalOutcome` 保存原报告及命令回执，只有当前请求和有效领取可以推进提议。旧工作者的报告保留为迟到事实。`StopProposalRequest` 终止请求推进，保留已有动作、未知效果和费用；通用持久工作命令不能替代这个业务入口封闭 `PROPOSE`。故障点包括 `tasks.model_prepare`、`tasks.model_seal`、`tasks.model_admit`、`tasks.model_result`、`tasks.proposal_outcome` 和 `tasks.proposal_stop`，并复用内容派生、P4 与 P5 的故障点。

### M1 本地受管理文件配置

先由部署宿主建立并完整同步受管理布局，再使用 `lerna --db state.db --user USER --domain DOMAIN --file-root documents=/absolute/managed-root ...` 固定别名映射；多个根重复指定 `--file-root`。布局、权限、平台和同步要求见 [ADR 0007](adr/0007-managed-file-publication.md)。根选项只供本机受信宿主装配，不是来自模型或执行参数的任意路径。重复别名、相对路径、空路径与非规范路径在打开数据库前拒绝；实际原生身份在统一出口内验证，原别名已经绑定后不能改指另一目录。

执行仍使用宿主已取得的完整 `execute START_JSON` 命令与对应出口身份；CLI 不根据文件路径代造准入、凭据、领取或调用描述。`operation ID`、`observation ID` 和原计费来源查询用于核对原动作与原发送。相同命令的进程重放返回原回执，文件创建、替换和读取都由同一受信出口处理。

## 完成拒绝与非成功关闭

必要条件被原动作的可靠证据否定时，当前核验保存 `REJECTED`、缺口和完整旧动作范围并释放本轮冻结。原 P4 已获准但仍未收尾、UNKNOWN 或可能迟到的动作继续阻断补建，包括全部历史发送。取得原出口的可靠封闭及收尾依据后，同一轮只登记一个新的上下文快照、`ProposalRequest` 和 `PROPOSE` 工作。证据缺失保持 `VERIFYING` 及冻结；旧轮次的回报不能释放新轮次冻结。新补建动作仍需重新取得当前准入授权、费用预留、确认和开始凭证。

`close-task --json FILE` 转发 `BeginTaskCloseCommand`，精确绑定当前任务修订及控制代次。受信宿主或本地交互可以用明确的停止原因请求 `FAILED`；模型提议没有关闭权。`task-closing TASK_ID` 查询原关闭依据、当前端点封闭进展和后续责任；它分别展示固定 Result 与原负责方的当前动作及待办。非成功依据不伪造成功核验。取消结果仍须使用正式取消协议的独立依据及原端点证明。

关闭意图与预算逐发送责任保存后，原出口封闭每个历史准入并保存独立原回执。仅在准确回执和负责方持久责任均可核验后固定 Result。UNKNOWN、迟到可能性和费用占用保留；原动作取得可靠终态、原费用结清后，由 ledger 和 budget 推进并完成其持久待办。原迟到回报或账单继续按原尝试及发送身份接纳，固定 Result 的字节及引用不变。generic 工作控制不能丢弃封闭或后续责任，也不能产生新的目标发送。详见 [ADR0010](adr/0010-nonsuccess-task-closing.md)。

真实文件的原历史查询仍由已保存的核对计划执行：失败关闭不重建计划，不把独立查询观察改标成原写观察，也不恢复已经被后继替换的文件。当前原执行与结算待办分别展示，弱读取后的 `UNKNOWN` 不因原写零费用已经结清而完成执行责任；后来可靠证据只改变原负责方事实及待办，固定 Result 保持原字节。


### M1 非成功关闭及当前取消依据

`close-task --json FILE` 转发 `BeginTaskCloseCommand`。可信宿主或本地交互须指定当前准确 TaskRef 和 expectedControlGeneration。FAILED 使用明确无法完成、期限或用户停止原因；CANCELLED 还引用同任务原 Cancellation，当前控制保持 CANCELLING。合法 ANSWER/MODIFY 之后，旧未决定命令因 TaskRef 过期而拒绝；可信调用方可用新命令身份明确关闭当前依据，不把旧取消 ACK 当成新输入的关闭裁决。两种范围的原准确 ACK、端点封闭和实际原准入交接到齐后才固定结果。

`task-closing TASK_ID` 返回固定 Result 和原负责方当前事实。closureIntents、closureSeals 和 awaitingOperationAdmissionRefs 分别展示准确源责任、真实封闭证明及尚待原交接的准入；墓碑没有动作引用时不会伪造 UNKNOWN 动作。原事实缺失或读取失败明确不可用。原效果、逐发送费用及核对仍由其负责方推进，固定 Result 的字节和引用不改变。

对应公共验收入口如下；M1 最终验收时，表中入口应在完整 `make check` 日志中实际出现。开发检查范围按第 4.1 节选择。

| 真实行为 | 测试入口 |
| --- | --- |
| 原取消 ACK 和当前关闭 ACK 必须分别到齐；CLI 和重启无额外发送 | `TestCLICancelledClosingRequiresBothOriginalCancellationAndClosingAcknowledgements` |
| 原 ANSWER/MODIFY/要求替换使旧 Begin 失效；新可信当前 Begin 有合法关闭路径 | `TestCancelledClosingRejectsOldBeginButAllowsFreshTrustedBasisAfterInput` |
| 关闭依据保存后拒绝输入、要求和控制变化 | `TestCancelledClosingRejectsForgedBasisAndFencesLaterInput` |
| 原准入 tombstone 等待实际交接；公开查询及最终裁决遇到原读取失败均不可用 | `TestCancelledClosingViewQualifiesTombstoneAndWaitsForOriginalAdmission`、`TestCancelledClosingCannotFinalizeWhenOriginalCancellationSealReadFails` |
| 原 MODEL、业务 TARGET 和独立 CLOSURE 进入完整当前范围，保留原取消范围及费用 | `TestCancelledClosingCoversTargetModelAndOriginalClosureAdmissions` |
| 两种非成功结果保留真实 UNKNOWN、全部历史发送、物理进行中责任和迟到原账单及效果 | `Test{Failed,Cancelled}ClosingRetainsUnknownAndAcceptsOriginalLateEffectAndBill`、`Test{Failed,Cancelled}ClosingKeepsAllHistoricalSendAndSettlementResponsibilities`、`Test{Failed,Cancelled}ClosingWaitsForActualInFlightTargetUse` |
| 失败、取消两种关闭各自的依据／封闭／源 ACK／固定 Result 和 P5 后工作完成三种故障模式 | `TestTaskClosingCommitBoundariesPreserveOriginalResponsibilities`、`TestCancelledClosingCommitBoundariesPreserveOriginalResponsibilities` |
| 真实受管理文件发布后读回失败，两种关闭保留原历史查询、零费用来源、资源及固定结果 | `Test{Failed,Cancelled}ClosingKeepsOriginalFileHistoryQueryAndFixedResult` |
| 成功结果后的原迟到账单保留费用和固定结果 | `TestClosedTaskAcceptsLateBillWithoutChangingFixedResult` |
| 可靠未满足后新请求／新授权／新动作，拒绝轮次不改写；唯一继续责任的三种故障模式 | `TestRejectedContinuationUsesFreshAuthorityAndCompletesWithNewAction`、`TestRejectedContinuationCommitRecoversOneActualRequest` |

只重放关闭提交和 FILE 原历史的命名故障切片可使用：`go test -v -race -tags fault ./conformance/fault -run '^(TestTaskClosingCommitBoundariesPreserveOriginalResponsibilities|TestCancelledClosingCommitBoundariesPreserveOriginalResponsibilities|TestFailedClosingKeepsOriginalFileHistoryQueryAndFixedResult|TestCancelledClosingKeepsOriginalFileHistoryQueryAndFixedResult)$' -count=1 -timeout=20m`。该命令是开发阶段的切片入口；完整验收和结果复用按第 4.1 节执行。真实原 API 目标和默认 driver 的组合使用后续正式集成接口验收，当前 FILE 与模拟目标证据不作这些组合的完成声明。

### M1 默认推理

受信宿主复用模型调用章节的 `RunModelCallCommand` 绑定，通过 `Tasks.RunReasoner(ctx, caller, run, &reasoner.Reasoner{Settings: run.Preparation.Settings})` 调用 `defaults/reasoner`。入口读取原请求固定的快照、条件和能力版本，把只接受调用位置、模型设置与输入引用的窄接口传给推理；授权、预算和出口仍由宿主持有。`defaults/scripted` 通过相同入口使用预设提议，便于确定性验证。

生产 `cmd/assembly.Harness.RunDefaultReasoner` 装配默认实现；`cmd/lerna` 的 `run-reasoner --json run.json` 调用这个入口。`run.json` 是已经由受信宿主固定的 `RunModelCallCommand`：包含原 `RequestRef`、有效 PROPOSE 领取、能力引用、输入引用、固定模型设置和授权引用。配置能力、预算、授权并建立请求／领取仍使用对应公共宿主接口；该命令不提供自动配置或自主循环。参考模型通过已登记能力的 loopback HTTP 目标运行受控合成供应商，不接受命令行临时 URL、密钥、HTTP 头或自动回退配置。

本地受信部署示例：`lerna --db state.db --user u --domain d --issuer host run-reasoner --json run.json`。`--issuer host` 是受控 M1 宿主配置的信任边界，不是远程身份认证；默认 `local-cli` 不会被此入口提升为 host。命令 JSON 的身份也必须匹配调用方。重复执行保留原文件和请求，返回原 `ProposalOutcome`，不新建请求以重新采样。将结果的 `proposalRef` 保存为引用 JSON 后，`lerna --db state.db --user u --domain d read-proposal --json proposal-ref.json` 读取治理后的完整提议；它不执行或批准提议。

`QueryProposal`、`QueryPlanning` 和 `QueryProposalOutcome` 返回结构记录。需要向用户展示问题、条件变更或草稿时使用 `ReadProposal`，它通过内容治理读取 `BodyContentRef`。`PublishProposalQuestion` 把 QUESTION 或 REQUIREMENTS 载荷发布为会话输入请求；回答经会话关联到原任务，条件变更仍要求真实用户的显式条件列表。主观完成条件先通过 `RequestConditionConfirmation` 固定事项，再由会话确认接口记录批准；模型文本不能替代批准。新主观确认的描述只包含结构引用；`ReadConditionConfirmation` 读取事项绑定的历史条件与证据正文，内容不可用时拒绝展示。

验证入口：`go test ./conformance/reasoner` 检查四类提议接口与确定性裁剪；`go test ./conformance/admission -run 'Reasoner|Default|Subjective|RequirementsProposal|ProposalMetadata|UnavailableProposal|DirectProposal'` 检查真实模拟供应商、治理正文、准入、确认和恢复；`go test -tags fault ./conformance/fault -run '^TestReasonerBodyAndOutcomeCrashRecovery$'` 对正文派生和回报做进程崩溃及回执丢失测试。检查范围与完整验收时机按第 4.1 节选择。

确认展示的治理分离适用于新生成的 M1 记录。曾在 `Description` 保存参数字节的实验数据库不能仅靠重新渲染就视为已治理；历史副本需要受支持的停写迁移，保留原事项、摘要、批准、消费身份及全部 P5、未知效果和费用责任。迁移未受支持时必须明确拒绝旧库，不能通过删除或重建数据库丢弃这些责任。


### M1 固定 API 与平台凭据

受信宿主通过 `assembly.OpenWithOptions` 的 `APIKeychainPath` 显式打开 macOS 已有、已解锁的 file-based Keychain；CLI 对应启动参数为 `--api-keychain ABSOLUTE_PATH`。路径是宿主配置，不能由提议或适配器传入；密钥不通过命令行、环境变量或配置正文传入。生产入口不创建或解锁平台仓库。未装配、缺项、锁定或访问失效时，在 P5 前拒绝真实发送，原责任仍保留。

已审查的 `api-reference-v1` 能力携带 `ApiDescriptor` v1，固定 origin、资源、账户、用户和 `ApiCredentialReference`。参考目标使用 `lerna-reference-api-v1`；参数只接受 `value` 和可选整数 `quantity`，原输入与实际编码正文分别保存摘要。受信出口只执行单次 HTTP 请求；HTTPS 使用宿主系统证书信任，测试可在 `APIRoots` 显式装配合成根。参考 GET 核对独立准入，并携带原查询主体，不重发原业务请求。

429 以原始观察结束时间固定 `ApiWait`，区分 `RATE`、`CONCURRENCY`、`RESOURCE_CONFLICT` 和 `UNKNOWN`。重启与重放读取同一到期时间；通用队列控制不能绕过等待。到期后仍须经过安全重发、P4/P5 的当前控制、授权、预算、正文、版本、绑定、凭据和原键期限门禁。参见 [ADR 0008](adr/0008-fixed-api-identity-and-platform-credentials.md) 与 [API 设计](architecture/adapters/api.md)。

启动在任何负责方自动恢复前纯核验全部原 API 动作，包含未准备、已关闭 UNKNOWN、原发送历史和独立 QUERY。检查原固定协议、完整声明、描述及身份绑定，不重新编译、读取内容、解析平台凭据或发送请求；原键已到期不使合法历史不可读取。平台条目缺失时保留合法历史，新出口以 `CREDENTIAL_UNAVAILABLE` 停在 P5 前。

停机恢复通过 `RestoreBackupWithOptions` 传入原 `APIKeychainPath`、`APIRoots` 和 `FileRoots`。数据库清单不包含或回滚这些外部世界。来源全部关闭后永久保持停止，恢复目录继续使用同一原目标和凭据仓库；迟到效果、原账单和重新恢复的独立查询不能改变已经固定的 Result。
