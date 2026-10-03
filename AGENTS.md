# 项目开发规范

Harness 将用户目标推进为可核验的结果。本文件规定代码落位、开发与交付要求；术语、业务语义和字段合同分别以所链接的领域文档为准。约束用语沿用本文的“必须／不得”“建议／不建议”“可选”。

## 开工前先确定依据与范围

- 必须先读根目录 [CONTEXT.md](CONTEXT.md)、[技术设计入口](docs/architecture/README.md)、与改动相关的模块章节和 [ADR](docs/adr/)。术语沿用领域词表，不能把 Session、Task、Decision、Operation 当作同一层状态。
- 代码组织与技术栈遵循 [工程方案](docs/architecture/engineering/README.md)及 [ADR 0010](docs/adr/0010-monorepo-shared-contract-release.md)：单 monorepo、初期一个 Go module，Go/TypeScript SDK 与实现同仓维护、同版验证。拆 module、改协议或改变事实负责方时，必须说明与既有设计的冲突并记录决策。
- 每次实现先在 [实施覆盖清单](docs/architecture/engineering/implementation-readiness.md#coverage)定位范围，明确负责方、原身份、输入输出、原子提交集合、失败恢复和验收证据。业务合同缺失时先补所属模块设计，不得在代码中另选一套语义。
- 当前已有有界参考实现，实际开放方法、验收证据和外部缺口见 [实施覆盖报告](docs/architecture/engineering/implementation-coverage.md)。下列目录树保留目标结构，尚未开放的目录随真实切片创建；目录存在、接口可编译或文档检查通过都不代表完整 profile 或生产能力已实现。

## 项目目录骨架

```text
/
├── AGENTS.md                       开发入口与共同约束
├── CONTEXT.md                      领域术语，不承载实现细节
├── go.mod / go.sum                 根 Go module 与依赖锁定
├── package.json                    前端 workspace 的共同命令入口
├── pnpm-workspace.yaml / pnpm-lock.yaml
├── cmd/                            进程入口、配置及依赖装配
│   ├── gateway/                    WSS 接入
│   ├── application/                Orchestrator 应用入口
│   ├── worker/                     按配置分池的持久工作者
│   ├── executor/                   执行宿主
│   ├── cli/                        命令行客户端
│   ├── migrate/                    独立迁移管理命令
│   └── harness-dev/                当前单进程参考装配
├── api/                            同版公开合同与派生资产
│   ├── schema/                     闭合 JSON Schema
│   ├── methods/                    方法登记与 profile
│   └── proto/                      承载严格 JSON 的 gRPC 外壳
├── runtime/                        接纳与工作模板、Tx、JobStore、Clock、身份端口
├── internal/                       领域实现，按下表细分职责
│   ├── task/                       Task、条件、控制、预算、Result 与协作
│   ├── brain/                      Snapshot 消费、Decision、模型调用与提案
│   ├── execution/                  Operation、Attempt、Effect、资源与环境
│   ├── memory/                     Content、Memory、来源与投影
│   ├── interaction/                Session、Surface、输入转交与 Schedule
│   └── governance/                 Grant、证据治理、扩展发布与正式评测
├── adapters/                       对端口的具体实现
│   ├── postgres/                   pgx/sqlc；queries/ 与 migrations/
│   ├── sqlite/                     database/sql；独立 queries/ 与 migrations/
│   ├── objectstore/                不可变内容介质
│   ├── wss/                        端云连接、帧与重连
│   ├── grpc/                       服务间协议绑定
│   ├── providers/                  模型、搜索、正文获取与外部 Agent 适配
│   ├── execution/                  文件、GUI 等目标驱动
│   └── platform/                   配置、发现、身份密钥、时钟、观测及生命周期
├── sdk/
│   ├── go/                         Go 客户端，随根 module 维护
│   └── ts/                         TypeScript 客户端与持久投递恢复
├── apps/web/                       React/TypeScript/Vite 受信 Renderer 与管理界面
├── conformance/
│   ├── contract/                   同版合同、正反例与跨实现互操作
│   ├── integration/                真实数据库及组件交接
│   ├── fault/                      丢回执、崩溃、旧 worker 等故障验收
│   └── testdata/                   固定、脱敏且可复现的测试输入
├── scripts/                        开发、生成及检查脚本
├── docs/
│   ├── architecture/              技术设计、现有协议资产与设计检查
│   ├── adr/                       有真实取舍的长期决策
│   ├── agents/                    Agent 工作约定
│   └── research/                  研究依据
├── .agents/skills/                 本地技能
└── .scratch/<feature>/             spec.md 及 issues/ 下的独立工单
```

目录是代码组织约定，不是一目录一服务或一目录一数据库。领域内部按实际用例拆包；不得为了对齐目录树制造空实现、统一业务状态机或中央调度服务。生产进程分工及开发装配遵循 [ADR 0003](docs/adr/0003-production-distributed.md)。

### 模块职责不能因归组而合并

| 代码位置 | 必须保留的职责边界 |
| --- | --- |
| `internal/task/` | Orchestrator 固定目标、Snapshot/派发意图、行动准入、任务预算和最终 Result；协作记录归父 owner，子 Task 仍归其固定 owner。Brain 提案不能直接改变这些事实 |
| `internal/brain/` | 保存 Decision、至多一次物理模型请求、提案及原模型用量；不自行准入工具行动或裁决任务成功 |
| `internal/execution/` | 保存接纳、Attempt、Effect、资源门禁及原执行用量；不能把请求已接纳当作效果已发生 |
| `internal/memory/` | Content 的准确字节、来源、副本责任与 Memory 的语义、检索投影分别维护；同目录不赋予交叉写权 |
| `internal/interaction/` | 维护原输入、会话、界面和投递；Confirmation、输入一次消费与成果验收由真正消费它们的业务 owner 裁决 |
| `internal/governance/` | 按需细分授权、证据治理、扩展和评测；Task 当前条件选择及完成仍归 Orchestrator，可信确认留在消费方的原业务事务中 |

账务按负责方落位：Task 预算和预留在 `task`，模型/工具原用量在 `brain`/`execution`，Grant 使用结算归授权 owner。各方只归并自己的责任，不建立可改写所有账本的通用记账入口。完整记录归属见[存储落位表](docs/architecture/data/storage.md#2-全模块落位)。

### 依赖与公开边界

- `cmd` 负责装配领域、Runtime 和 adapter；不得承载业务裁决。`internal/*` 依赖小型端口、显式 repository 与 Runtime 合同，不得导入具体 adapter、SQL 驱动、网络连接对象或前端框架。
- adapter 实现领域/Runtime 所需端口。跨模块通过明确的用例或端口协作，不得直接改写其他 owner 的记录。共享 Tx 必须由宿主显式声明数据库、受信租户和参与者，不能由 Go import 关系推断。
- `runtime` 只共享接纳、领取、条件提交和恢复原语，不依赖具体领域实现；领域保留成功、重试和完成的判断。遵循 [ADR 0009](docs/adr/0009-reliable-work-framework.md)。
- `api` 不依赖 `internal` 或 adapter。SDK 依赖公开合同与客户端传输，不导入服务端领域或数据库实现；`apps/web` 通过 TypeScript SDK 访问业务。
- 只有明确列入发布合同的接口与 SDK 才承担公开兼容承诺。顶层 Go 包可导入不等于稳定 API；公开扩展接口的签名不得要求外部组件引用 `internal` 类型。
- 通用代码仅在出现真实复用时提取，并以职责命名；避免无边界的 `common`、`utils` 或包含全部领域类型的共享包。不得靠跨包循环依赖组织业务流程。

## 开发规范

### 语言、工具链与代码习惯

- 核心、宿主、默认组件与 CLI 使用受支持的稳定 Go；前端使用 React/TypeScript/Vite 与 pnpm workspace。首个对应工程建立时，必须将验证过的工具链版本、生成器及依赖锁定，安装制品按设计记录 InstallLock；不得直接把“最新版”当作兼容保证。
- 初期只维护根 `go.mod`，`sdk/go` 不另建 module。前端依赖由 pnpm 管理并维护统一锁文件，避免混入另一包管理器的锁文件。新增依赖须说明用途及其重试、计费、隔离或恢复行为是否影响合同。
- Go 必须经 `gofmt` 格式化；包名简短小写、按职责命名，公开标识按 Go 惯例书写（如 `TaskID`）。说明与业务注释默认用中文，领域标识沿用词表，线协议字段严格保持 Schema 中的拼写。
- 接口尽量定义在消费方附近并保持小型。错误保留可判别类别与原因，使用 `errors.Is`/`errors.As` 识别；预期业务拒绝不得用 `panic` 表达，也不得吞掉持久化或关闭错误。
- I/O 与长任务传递 `context.Context`；goroutine 必须有明确的所有者、退出和回收路径。并发、队列、扫描、字节与等待必须有界。context 取消仅代表停止信号，释放资源或并发槽前必须观察实际退出。
- 裁决时间、身份生成和外部 I/O 通过明确端口提供；测试可替换时钟与故障点。不得在 `init`、对象构造或恢复反序列化时隐式发送模型、工具请求。
- TypeScript 启用严格类型检查；不可信输入按 `unknown` 接收并校验，类型断言不能替代运行时 Schema 校验。前端呈现接纳、效果、任务完成和交付的独立事实，不能以本地状态代替业务决定。
- 配置、发现、身份密钥和观测通过平台适配接入；凭据使用受控引用，不进入代码、测试夹具或日志。slog/OpenTelemetry 仅输出获准且脱敏的遥测，不替代业务账本。

### 协议与生成资产

- 当前机器合同源位于 [protocol](docs/architecture/protocol/README.md)：`core.schema.json` 是部分结构合同，`harness.proto` 是 gRPC 外壳；业务语义由模块设计和[共同方法合同](docs/architecture/protocol/method-contract.md)规定。`api/` 已保存由此源生成的记录、Schema 快照与协议外壳；生成流程见 [scripts](scripts/README.md)，不能独立手改。
- 迁移到 `api/` 必须在同一变更中更新生成器、检查器和文档引用，只保留一份可手工维护的合同源。不得复制出两份独立演进的 Schema。
- 开放一个方法前，必须同版补齐负责方、profile、请求/响应/错误 Schema、回执阶段、原命令查询、权限/版本前提、Go/TS 类型、SDK 恢复行为及正反例。未开放能力返回 `unsupported`；部分方法实现不得声明整个 profile 已受支持。
- JSON 使用闭合 Schema；原始重复键、Unicode 和数值检查早于普通解码。Go/TS 使用同版规范化与摘要规则，Protobuf 外壳不得重新定义第二套领域字段。
- SDK 首次发送前保存原命令、准确输入和固定 owner；重连、超时和恢复沿原身份查询或重传，不刷新默认值、不换 owner、不制造同义新命令。浏览器使用原生 WebSocket 和 IndexedDB 保留投递责任。
- 生成物必须注明来源及生成方式，不得手改掩盖漂移。当前 `docs/architecture/data/field-reference.md` 由 Schema 生成：修改源后运行 `python docs/architecture/validation/build_field_reference.py`，再用 `--check` 验证。后续生成资产必须同样可复现，并保留未结旧命令所需的原 profile、摘要与解码器。

### 存储、事务与恢复

- 默认云端 Task 权威保存在所属 PostgreSQL 分片；设备 SQLite 只负责本机执行、门禁、恢复和补传。设备缓存不得裁决云端任务完成或断网接管。可选端侧 Orchestrator 只推进首次归属自己的 Task。
- PostgreSQL 使用 pgx/sqlc 显式 SQL；SQLite 使用独立 `database/sql` 适配、WAL/FULL/foreign_keys 与单写队列。各自维护查询和迁移，不得把 PG 行锁或 `SKIP LOCKED` 假设套到 SQLite。
- 业务事实、命令回执和新增 Job 必须在已声明的同库短事务内共同提交。Tx 不跨数据库或租户、不逃逸到 goroutine；外部调用在事务外。跨数据库或未声明共享事务范围的 owner 交接，按“本方持久意图 → 对方持久接纳 → 本方保存依据”分别提交；已显式同库装配的 owner 按规定的共同提交集合处理。
- 必须遵守[共同提交集合与锁序](docs/architecture/data/storage.md#3-同一数据库事务具体包含哪些记录)：原命令键 → 根到叶 Task → 各单位预算 → 领域门禁及对象 → Job，并保留证据 gate、Memory 变更头的专门锁序。新增上游锁时回滚重组，不能靠扩大事务掩盖冲突。
- `Committed`、`RolledBack`、`CommitUnknown` 分别处理。只可有限重试已确认回滚且无外部行为的数据库闭包；提交未知时沿原身份核对，不出站、不释放未知预留、不伪报业务失败。详细规则见 [Runtime](docs/architecture/runtime/README.md)。
- Brain 每个 Decision 至多一次物理模型请求。Executor 的安全重试依据目标幂等合同和效果证据决定；所有供应商、代理及 SDK 隐式重试必须关闭或纳入明确的物理尝试与费用合同。
- 取消、Task 终态、费用结清和数据清理分别维护。未知效果、迟到费用及已消费的一次授权不得因重启或清理而消失；最小去重和终态身份按设计长期保留。
- 迁移由独立管理命令执行，业务副本启动不竞相改表。遵循 expand → 有界 backfill → 切换 → 观察 → contract；旧实例退出、未结责任可恢复且保留期结束后才删旧格式。软件回退不得回滚业务历史。

## 测试与交付

### 按实施切片取得证据

1. 先实现原命令去重、回执、Job/Claim、可信时间和 PG/SQLite 各自事务适配；提交未知与旧 worker 竞争通过后再接外部副作用。
2. 首个闭环覆盖报告生成、保存、独立读回、当前条件核验与 Result，随后再扩展真实模型、交互、多进程及规模。顺序以[开发范围](docs/architecture/engineering/implementation-readiness.md#1-按这个顺序开发)为准。
3. 可选能力按通过验收的方法范围开放。尚未开放的能力不得提前创建新业务对象、预算或常驻工作；已运行能力停用后，仍须保留原责任的恢复、效果/费用核对和清理。原代码不可信时按扩展合同使用合格接管器或保留明确缺口，不得为收尾运行不可信代码。依赖前提缺失时报告具体缺口，不以空实现宣称支持。

| 检查层 | 放置与要求 |
| --- | --- |
| 单元行为 | Go `*_test.go`、TS `*.test.ts(x)` 就近放在所属模块；检查规则与边界行为，不镜像实现细节 |
| 合同与互操作 | `conformance/contract` 复用同版正反例；建议先写 repository 合同测试再实现 adapter，偏离时说明影响 |
| 存储与交接 | `conformance/integration` 使用真实 PG/SQLite 和组件交接；mock 不能证明原子性、唯一约束、锁序或持久恢复 |
| 故障与恢复 | `conformance/fault` 对照 [F01–F25](docs/architecture/validation/README.md#2-关键故障矩阵)选择受影响场景，观察原命令、双方账本、目标真值和真实出站；合法正例与拒绝反例都必须成立 |
| 界面与生产 | Web 交互需真实浏览器验证；平台支持、真实供应商、容量和跨 AZ 恢复分别取证，不能由单元或文档检查代替 |

### 可执行检查与适用范围

在仓库根目录按改动选择检查；只有必要的前置依赖齐备且实际执行通过，才能报告通过。

| 改动范围 | 当前命令 |
| --- | --- |
| 根文档、`docs/`、`.agents/` 的导航与引用 | `python docs/check_documentation.py` |
| 架构正文、Schema、正反例及关系 | `python docs/architecture/validation/check_architecture.py` |
| Job、取消/完成、账单与未知效果模型 | `python docs/architecture/validation/model_checks.py` |
| 输入、条件覆盖与控制模型 | `python docs/architecture/validation/data_flow_checks.py` |
| 已固定业务合同的有限模型 | `python docs/architecture/validation/implementation_contract_checks.py` |
| Schema 与生成字段字典一致性 | `python docs/architecture/validation/build_field_reference.py --check` |
| 架构解释工具的状态函数 | `node docs/architecture/validation/lab_checks.cjs` |

文档检查需要 Python 3.10+ 和 Git，架构检查另需 `jsonschema`，解释工具检查需要 Node。缺本地历史提交导致的 `blocked` 不算通过。现有文档扫描不包含未来源码目录里的所有嵌套文档；新增这些文档时须明确补充检查范围。以上模型检查不运行真实数据库，Node 函数检查也不启动浏览器。完整边界见[验收设计](docs/architecture/validation/README.md#5-本系列附带的检查)。

代码工程的共同检查入口为 `scripts/check`（无参数）；版本与生成器固定在 [toolchain-lock.json](scripts/toolchain-lock.json)，包括 Go 1.26.8、Node 24.19.0、pnpm 11.19.0。入口检查 Go 格式、vet、行为测试，前端格式、lint、严格类型、行为测试和构建，以及核心 Schema、proto/sqlc/TypeScript 的生成漂移；Go 编译另运行 `go build ./...`。并发改动增加受影响包的 `go test -race`。

真实 PostgreSQL 验收读取 `HARNESS_TEST_POSTGRES_DSN`，进程装配读取 `HARNESS_DATABASE_DSN`；只通过环境变量或受控引用提供，不输出或提交值。未配置 PG 时的 skip 不算该数据库通过。日历测试读取固定的 `conformance/testdata/tzdb/2026b`；须同时核版本与原字节摘要。浏览器入口为 `pnpm test:browser`，需要实际运行的后端、前端和 Chromium；严格 CSP 验收使用后端静态服务及 `HARNESS_REQUIRE_CSP=1`。CI 定义在 [.github/workflows/check.yml](.github/workflows/check.yml)，本地通过不表示托管 CI 已执行。真实供应商、设备、规模与容灾仍须分别取证。

### 每次交付必须说明

- 改了什么、为什么、对应哪条合同及实际开放范围。涉及规范冲突时明确引用原 ADR；只为有长期代价的真实取舍新增 ADR，不把目录细分或例行实现重复记录为架构决策。
- 实际执行的命令与结果，未执行或受阻项及原因。纯文档改动运行相关文档检查即可；代码改动须覆盖改变的行为与故障分支，不用无依据的覆盖率数字替代行为证据。
- 运行验收绑定实现 commit、profile/Schema 摘要、配置、数据集、环境、随机种子、故障点与完整结果，写明夹具预置的前提。设计证据、实现证据和生产证据分别报告，不将 RPO/RTO 或质量目标当作已测结论。
- 只修改本次范围内的文件，保留用户已有改动；不得顺手重构、换技术栈、初始化未要求的部署设施或提交凭据。规范与工单遵循下文现有约定。

## 让输出易于理解和使用

以 ISO 24495-1:2023 的简明语言原则为指导，让目标读者获得所需信息、快速找到信息、准确理解信息，并能据此行动。中文可借鉴 ASD-STE100 的清晰写作方法，不机械套用其英文词典和语法规则。

### 内容与结构

- 明确读者要解决的问题、已有知识和下一步行动，据此确定内容与细节深度。
- 先给核心结论，再说明关键依据、适用条件、假设和限制。区分事实、推断、建议和待验证事项。
- 先讲整体，再展开细节。使用含义明确的标题，将相关信息放在一起，避免重复和不必要的跳转。关键前提、风险和例外应紧邻相关结论，不藏在附录中。
- 借鉴 Diátaxis，区分内容用途：解释说明为什么，参考提供精确定义，操作指南说明如何完成任务，教程帮助入门。按需要分节或分篇，不强制每个主题都编写四类文档。
- 技术设计应让开发者找到模块职责、数据及存储、接口契约、关键流程与时序、异常处理。设计理由与字段明细分别组织，并建立直接链接，避免让读者猜测业务规则和责任边界。

### 语言与术语

- 使用短句、明确主语和具体动词。一句话尽量表达一个主要意思，避免嵌套过多条件和堆叠抽象名词。
- 同一概念使用同一术语。必要术语首次出现时给出定义；区分易混淆的概念，避免用多个近义词指代同一对象。
- 保留技术精度、关键前提和例外。简明不等于省略，不以固定句长或字数代替清晰度判断。
- 删除重复、空泛和无助于理解的内容。用具体示例解释抽象规则，避免仅用另一组术语解释术语。

### 规则与约束

- 技术规范统一使用以下约束用语：
  - “必须／不得”：实现必须遵守的要求或禁止事项。
  - “建议／不建议”：默认选择；偏离时应理解影响，并说明理由。
  - “可选”：实现可以选择是否提供的能力。
- 区分强制要求、推荐方案、示例和待定设计，不用含糊的“应该”混合表达不同强度。
- 关键规则写明责任主体、触发条件、预期行为和失败处理。示例不得暗中增加正文未定义的要求。
- 可借鉴 RFC 2119／8174 的要求等级思想；若采用其正式关键词语义，应在文档中明确声明。

### 表达形式

- 根据理解任务选择形式，不为展示能力而增加产物：
  - 结构、关系和流程：优先使用图示，标明关键角色、方向和边界。
  - 字段、属性和规则对照：使用结构一致的表格。
  - 参数变化、方案比较和探索：交互能明显降低理解成本时，使用小型交互 HTML。
  - 时间变化、操作过程和逐步推导：动态表达明显优于静态图文时，使用动画或讲解视频。
- 简单问题直接用文字回答。复杂问题选择解释效果好、制作和使用成本合理的形式。
- 必要时制作小而专用的解释工具。优先复用现有工具，不为一次说明引入通用框架或改动无关代码。

### 交付与检查

- 图示和解释工具应附简短结论、查看方式和必要操作说明。标明假设与简化，不将示意结果当作实际验证结果。
- 检查文字、图示、字段表、示例和交互结果是否一致。确保交付物可打开、可阅读，关键操作可用；明确说明未验证的部分。
- 从读者角度复核：能否找到所需信息、理解关键关系，并完成预期判断或行动？技术设计还应检查开发者是否需要自行猜测关键业务规则。
- 重要文档尽可能通过实际读者反馈验证，不只检查字数、格式或链接。
- 未经完整评估，不宣称符合某项标准，也不以任意百分比表示合规程度。

## Agent skills

### Issue tracker

Issues and specs live as Markdown files under `.scratch/<feature>/`. See `docs/agents/issue-tracker.md`.

### Triage labels

The five canonical triage roles use their default strings: `needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, and `wontfix`. See `docs/agents/triage-labels.md`.

### Domain docs

This is a single-context repo using root `CONTEXT.md` and `docs/adr/`. See `docs/agents/domain.md`.
