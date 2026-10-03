# Harness 项目开发规范

本文件供在本仓库实现、测试和评审的开发者与 coding agent 使用。先按任务找到设计，再按目录落代码，最后交付可复查的证据。本文件规定工程约束，不取代各模块的业务合同。

## 1 开始任务

1. 必须先检查分支、工作区差异和适用的 `AGENTS.md`；保留已有修改，不覆盖无关工作。
2. 必须先读 [CONTEXT.md](CONTEXT.md) 的术语、[工程方案](docs/architecture/engineering/README.md)和[开发范围](docs/architecture/engineering/implementation-readiness.md)，再读相关 [ADR](docs/adr/)。现行实现合同位于 [docs/architecture](docs/architecture/README.md)；历史研究只作依据，不自动增加实现要求。
3. 必须明确本次切片的入口、数据写入负责方（owner）、事务参与者、原身份、失败恢复和验收范围。无法从设计确定时，先报告缺口，暂停依赖该选择的实现；不得自行改写 ADR 或猜测业务语义。
4. 建议先实现一条可恢复闭环，再扩展能力。新依赖、框架、通用接口或独立服务必须说明当前用途；不得用空目录、空实现或成功占位返回代替交付。

约束用语统一为：**必须／不得**是强制规则；**建议／不建议**是默认选择，偏离时必须说明影响；**可选**表示可以不开启，开启后必须完整遵守对应合同。

## 2 项目目录参考

### 当前已有内容

当前仓库以设计资产为主：`docs/architecture/` 保存现行设计、机器合同和设计检查；`docs/adr/` 保存决策；`docs/research/` 保存研究依据；`docs/agents/` 与 `.agents/skills/` 保存协作规则。根目录还有 `AGENTS.md`、`CONTEXT.md` 和 `CLAUDE.md`。

当前没有 `go.mod`、前端 `package.json`、业务代码、数据库迁移或完整 SDK。以下均为**规划目录**，只在实现对应切片时创建；不得因树中列出目录就宣称能力已实现。新增真实入口后，必须同步更新本节与检查说明。

### 代码落位

按 [ADR 0010](docs/adr/0010-monorepo-shared-contract-release.md)，采用一个 monorepo，Go 初期只有一个 module，Go SDK 随该 module 维护。

```text
cmd/                     进程入口与依赖装配
  gateway/               WSS 接入、连接绑定与转发
  application/           短命令、查询与领域入口装配
  worker/                按工作类别装配处理池
  executor/              云端或设备执行宿主
  cli/                   使用公开 SDK 的命令行客户端
api/                     按 profile 固定的机器合同
  schema/ methods/ proto/ 闭合 Schema、方法登记、gRPC 外壳
  gen/                   生成的线协议类型；不放领域逻辑
  ports/                 按需公开、明确版本范围的扩展接口
runtime/                 共用接纳/工作模板、Tx、JobStore、Clock、身份 port
internal/                内部领域实现，不承诺为公开 API
  task/                  目标、条件、准入、控制、任务预算、完成与委派责任
  brain/                 Decision、模型请求、提案发布与原用量
  execution/             Operation、Attempt、Effect、资源与执行环境
  memory/                Content 字节治理与 Memory 语义，各自保留写权
  interaction/           Session、Submission、Surface、Schedule 与输入转交
  governance/            Grant、扩展批准、评测及证据治理，各自保留写权
adapters/                实现领域/runtime port，不拥有新的业务权威
  postgres/ sqlite/      分别实现 repository、SQL、迁移与事务
  objectstore/           不可变字节介质
  wss/ grpc/             传输、严格解码、受信上下文及协议映射
  model/ tools/          模型供应商、API、文件和 GUI 的真实出口
  platform/              配置、身份/密钥、发现、时钟与观测适配
sdk/go/ sdk/ts/          公开客户端、原命令持久化与恢复
apps/web/                React/TypeScript/Vite 受信 Renderer 与管理界面
conformance/             合同向量、跨模块/跨实现、端到端与故障验收
configs/                 有类型约束的配置说明与无秘密样例
deploy/                  本地装配和实际部署的健康、排空与恢复资料
scripts/                 已实现、可复现的生成/检查/开发辅助入口
```

- 模块划分必须服从 [系统职责](docs/architecture/system-model.md)与[数据落位](docs/architecture/data/storage.md#2-全模块落位)。目录、对象组、表、数据库和进程不是一一对应关系。不得仅因逻辑模块或目录存在就新增微服务。
- 领域包建议先按行为组织 `model.go`、`handler.go`、`ports.go` 等文件，测试就近放置。只有实际复杂度需要时才拆子包；不预铺多层空架构，不建立笼统的 `common`、`utils` 或通用 CRUD 层。
- SQL、生成的查询代码和迁移必须归入对应引擎的 `adapters/<engine>/<module>/`；同一数据库的迁移序列由管理入口显式组合。共享事务不改变模块写权。
- 单进程装配仅供开发调试。生产必须保留网关、应用、分类工作池、执行宿主的独立装配；管理迁移独立执行，评测与用户任务资源隔离。详见[生产角色](docs/architecture/production/README.md#1-角色与故障域)。

## 3 依赖与编码规则

允许的主要导入方向：`internal/<领域> → runtime/公开扩展 port`；`adapters → 领域 port/runtime/线协议类型`；`sdk → api`；`apps/web → sdk/ts`；`cmd → 所需领域与适配器的装配`。箭头表示源码依赖，不表示网络调用。

- `runtime` 必须保持领域无关，不得反向导入 `internal/`、`adapters/`、SDK、界面或进程入口。JobStore 是各 owner 采用的共同契约，不是中央作业表、调度服务或统一业务状态机。
- 领域必须通过消费方定义的小型 port 与显式 repository 使用依赖。领域不得导入 SQL 驱动、连接对象、供应商 SDK、传输类型或前端框架。跨领域通过明确接口交接，由装配层接入本进程实现或远端适配；不得越过接口读取或写入对方的内部记录。
- 领域对象、数据库记录、线协议 DTO 和公开读取投影必须分别建模，在适配边界显式转换。生成类型不得直接充当领域实体；数据库结构不得由 API 响应倒推。共享值类型只提取真实共用的最小语义，不把业务规则塞入 `runtime`。
- `cmd` 只做配置、装配和进程生命周期。SDK 不得依赖 `internal/`；Web 不自行裁决授权、效果或 Task 完成。对外扩展接口按需在 `api/ports/` 建包，明确版本与支持范围，不依赖 `internal/` 或传输实现；内部消费方 port 不自动成为稳定 API。
- 核心、宿主和 CLI 使用 Go；PG 使用 pgx/sqlc 显式 SQL；SQLite 使用独立 database/sql 适配；前端使用 React/TypeScript/Vite 与 pnpm workspace。工具链、生成器及依赖版本必须在真实构建与合同测试后锁定，不把研究日期的版本当运行保证。
- Go 代码必须经过 `gofmt`；对外部等待传递 `context`，并通过注入的 Clock 测试期限和租约。context 取消只发停止信号；实际工作退出前不得释放执行槽。TypeScript 必须启用严格类型检查；外部载荷从 `unknown` 经合同校验后再使用。
- 建议围绕真实替换点和可观测行为设计接口；不为每个结构体建接口，不用继承式基础层或反射框架隐藏事务、身份和恢复责任。新增外部依赖必须记录用途、版本及影响范围。

## 4 数据、事务与恢复规则

实现数据或状态变化前，必须同时查[完整字段](docs/architecture/data/module-records.md)、[存储与一致性](docs/architecture/data/storage.md)和[持久工作](docs/architecture/runtime/README.md)。以下规则适用于所有模块：

1. **唯一写权。** Orchestrator 写目标、条件、Snapshot/Intent、采纳和 Result；Brain 写 Decision/模型事实；Executor 写接纳、Attempt/Effect；Interaction 写原输入和转交。Content、Memory、Grant 及评测分别保留自己的事实。Confirmation/InputRequest 的消费归实际业务 owner，不能因代码同目录而互改记录。
2. **默认存储。** 云端 Task、条件/目标版本、预算、Result、控制，以及与 Task 共同裁决的 Grant/Confirmation、证据 gate 必须位于所属同一 PG 分片，按声明共同提交。跨域不得用“远端先检查、本地后提交”冒充原子；estimate 与零陈旧证据遵守同事务限制。设备 SQLite 只保存本机执行、门禁、恢复和补传，必须启用 WAL、FULL、foreign_keys 并使用单写队列。不得把 PG 锁语义搬到 SQLite，不得让端侧缓存接管云端 Task。只有显式启用[端侧独立 Orchestrator](docs/architecture/production/README.md#optional-edge-orchestrator)后，其自有 Task 才在 SQLite 裁决。
3. **原子集合。** Handler 必须声明同库、同租户及 Tx participants，使业务决定/准备责任、原回执和必要 Job/outbox 共同提交。Tx 不得跨数据库或逃逸到 goroutine。锁序与提交集合遵守[存储 §3](docs/architecture/data/storage.md#3-同一数据库事务具体包含哪些记录)；需要补锁更早层级时回滚重组。
4. **事务外效果。** 网络、模型、对象存储和真实目标调用必须在事务之外。只有确认未提交且没有外部行为的事务闭包可有限重试。工具重试须符合目标幂等性、原 Attempt、预算及恢复合同。CommitUnknown 必须沿原键查询；查明前不出站、不释放未知预留、不伪报业务拒绝。
5. **原身份恢复。** 首次发送前必须持久保存原 owner、完整命令、载荷、profile、摘要与期限。重连和换 worker 不换 Command/Decision/Operation，不刷新原 CAS 或期限。同键异内容必须拒绝。跨 owner 按“本方意图 → 对方持久接纳 → 本方记录原回执”分别提交。
6. **持久责任。** 唤醒与内存队列可丢，原 Job 不可丢。Claim 的 holder/epoch/期限及 observed_work_revision 必须参与受保护提交；旧 done/退避不得覆盖新责任。超时、取消、Task 终态和自动重试耗尽均不删除未知效果、费用或清理责任。最小去重/关闭身份不得按日志 TTL 删除。
7. **准入与完成。** Brain 只提议，每个 Decision 至多一个物理模型请求。模型 SDK、代理及认证刷新造成的自动重发必须关闭；send_started 后只能查询原调用。批量行动按设计至多四项、准入全收或全拒，外部效果不保证全成或全败。完成由 Orchestrator 用当前目标、完整未结关系及证据 gate 裁决；原 Result 先固定，Content 导出随后恢复。不得从 UI 空页、缓存或模型自评推断完成。
8. **内容与隔离。** 跨域引用必须携带准确租户、owner、对象/版本及所需摘要；读取旧版本仍检查当前权限。来源限制、正文保留和副本清理遵循[内容合同](docs/architecture/memory/README.md)。租户、主体与权限必须来自受信上下文。每个事务重新设置租户上下文，关联键包含 tenant/owner；PG 使用受限角色与 RLS，后台 worker 不得绕过隔离。

## 5 合同、错误与可观测性

- 实现接口前必须读[共同方法合同](docs/architecture/protocol/method-contract.md)及对应模块的方法表，保持原裁决顺序、CAS/源版本、合法前态、回执阶段和恢复入口。`accepted`、`applied`、实际效果、Task 完成及费用关闭必须分开表达。
- 当前机器定义为 [core.schema.json](docs/architecture/protocol/core.schema.json) 与 [harness.proto](docs/architecture/protocol/harness.proto)，[核心示例](docs/architecture/protocol/examples/core.json)是合同向量；语义以模块合同为准。Schema 仅覆盖部分记录与五条命令，其 fixture wrapper 不是 wire frame，不能生成公开线协议或“完整 SDK”。启用 `api/` 时必须在同一变更中确定机器源位置、更新引用/生成器/检查，禁止两处手写维护同一字段权威。
- 开放方法前必须补齐同版闭合输入/输出 Schema、方法登记、错误、回执、Go/TS 类型、SDK 恢复与正反例。生成物必须标来源与生成入口，禁止手改；[字段字典](docs/architecture/data/field-reference.md)由现有 `build_field_reference.py` 生成。重新生成后必须无非预期差异。
- 边界映射必须无损保留金额的 unit 与精确十进制，禁止浮点计费；修订/计数遵守 JSON 安全整数范围，时间无损往返 UTC RFC3339。未知不得用零值或空列表冒充。
- Schema、方法、生成类型、SDK 和合同向量必须同版校验；业务字段或语义变化必须按[协议版本规则](docs/architecture/protocol/README.md#1-本系列的合同版本)冻结 profile。旧未结命令保留原 profile、摘要和解码器。发现只声明已验收子集；未开放或未支持的能力必须返回 `unsupported`。
- 同进程用 Go 接口，独立服务用 Protobuf 外壳携严格 JSON 的 gRPC，端云用 WSS，发现/认证/大内容字节用 HTTPS。解码前必须检查原始 JSON 的重复键、非法 Unicode 和数字；拒绝未知字段，不以 Protobuf 或宽松 JSON 另立字段语义。
- 错误必须按合同提供稳定 `code/scope/reason/retry`，安全 `detail` 不泄露秘密或无权对象。暂不可达不能写成 `not_found`，正文清理用 `gone`，效果/费用未知指向原记录查询。通用异常或 HTTP/gRPC 状态不能替代领域回执；重试提示不授予第二次副作用。
- 结构化日志用 slog，追踪与指标用 OpenTelemetry；只输出获准遥测。精确 command/task/operation/job ID 可进入受控关联记录，不作高基数指标标签；正文、凭据和完整载荷不得进入通用日志。至少观察未决责任年龄、unknown、重试、控制与核对积压、连接/事务成本；日志不是业务账本。详见[运行指标](docs/architecture/production/README.md#8-运行指标与成本取舍)。

## 6 迁移、配置与安全

- PostgreSQL 与 SQLite 必须分别维护迁移与恢复测试。已使用的迁移不得原地改写；通过新增迁移修正。独立管理命令执行迁移，在线副本启动只检查格式兼容，不竞相改表。
- 数据演进采用 expand → 有界 backfill → 切换 → 观察 → contract。必须保存 migration_id、摘要、检查点和兼容范围；旧实例退出、旧未结责任可恢复且回退期结束后，才可删除旧字段。软件回退不得回滚业务历史，迁移不得破坏全保留期唯一键和关闭记录。见[存储迁移](docs/architecture/data/storage.md#8-容量扩缩与迁移)。
- 配置必须校验类型、范围和依赖能力；错误时拒绝相关能力就绪并报告原因。配置样例只使用占位值；密钥只保留引用，由专用凭据适配器注入准确获准出口。秘密不得进入提示、产物、普通插件上下文或日志；不得提交真实凭据、数据库内容或用户材料到仓库。
- TaskPolicy 的预算、期限、风险与开放能力必须来自已确认配置。安装/运行绑定准确版本和摘要；不得在恢复时读 `latest` 或静默放宽策略。缺少供应商原请求查询、可靠时钟、身份或隔离证据时，关闭依赖它的能力，继续不依赖该前提的切片。
- 开发优先配置文件、静态发现和进程脚本，Compose 可选。初期不得另建公司控制平面、中央事件库、第二持久消息系统或 Redis 权威目录。健康、排空、连接池和恢复必须遵守[生产合同](docs/architecture/production/README.md)，不能把示例容量/SLO 当已测保证。

## 7 按任务实施与验证

### 先读什么、改哪里

| 任务 | 必须先读 | 主要改动与验收 |
| --- | --- | --- |
| 目标、预算、控制或完成 | [编排](docs/architecture/orchestrator/README.md)、[账务](docs/architecture/accounting/README.md)、涉及委派时读[协作](docs/architecture/collaboration/README.md) | `internal/task` 及对应 repository；测原键、条件/控制竞争、费用和完成门禁 |
| 新模型、工具或执行方式 | [Brain](docs/architecture/brain/README.md)、[Execution](docs/architecture/execution/README.md)、[安全](docs/architecture/security/README.md) | 领域 port 与 `adapters/model` 或 `tools`；测真实出站次数、答复丢失、未知效果与取消 |
| 内容、记忆或输入/界面 | [Memory](docs/architecture/memory/README.md)、[Interaction](docs/architecture/interaction/README.md) | 对应领域、存储、SDK/Web；测来源/当前权限、原输入恢复、准确预览与一次消费 |
| 字段、方法或 SDK | [数据总览](docs/architecture/data/README.md)、[方法合同](docs/architecture/protocol/method-contract.md)、所属模块 | 合同源 → 生成物 → 边界映射 → SDK → 合同向量；运行下方全部设计检查及已实现的互操作检查 |
| SQL、恢复或部署 | [存储](docs/architecture/data/storage.md)、[Runtime](docs/architecture/runtime/README.md)、[生产](docs/architecture/production/README.md) | 对应引擎迁移/repository、装配/配置；测提交未知、旧 worker、升级恢复与真实故障域 |
| 扩展、评测或发布 | [扩展](docs/architecture/extensions/README.md)、[评测](docs/architecture/evaluation/README.md)、[开发范围](docs/architecture/engineering/implementation-readiness.md) | `internal/governance` 及适配；测版本/批准、资格失效、隔离、回退与声明范围 |

### 当前可运行的检查

以下命令已存在，从仓库根目录执行。Python 检查需要现有 `jsonschema`；解释工具检查需要 Node.js。依赖缺失必须报告，不把未运行写成通过。

```sh
python docs/architecture/validation/check_links.py
python -m unittest discover -s docs/architecture/validation -p 'test_*.py'
python docs/architecture/validation/check_architecture.py
python docs/architecture/validation/model_checks.py
python docs/architecture/validation/data_flow_checks.py
python docs/architecture/validation/implementation_contract_checks.py
python docs/architecture/validation/build_field_reference.py --check
node docs/architecture/validation/lab_checks.cjs
git diff --check
```

修改本文件或架构设计时必须运行以上检查，并单独核对本文件的新链接、目录现状及命令。修改核心 Schema 后先运行 `python docs/architecture/validation/build_field_reference.py` 更新字典，再执行检查。

链接检查覆盖全仓文档的本地目标与本仓固定提交引用；外部 URL 不联网验证。研究来源元数据和按需源码复核说明见[检查说明](docs/architecture/validation/README.md#6-全仓链接与研究来源维护)。

这些脚本只检验设计资产、有限模型与解释工具函数，不执行真实数据库、服务或浏览器。当前尚无项目级构建、Go/前端测试或代码生成总入口；不得声称不存在的 `make`、Go 或 pnpm 项目命令已经可用。

### 代码落地后的验收门槛

新增代码的开发者必须同时建立其真实构建、格式、静态/类型检查与测试入口，记录命令和依赖，并更新本文件。下表是待实现的验收要求，不是已有可执行命令。

| 层级 | 位置与要求 |
| --- | --- |
| 领域单元测试 | 与代码同包/就近；通过模块接口测试状态、金额、边界、确定时钟和错误，避免绑定私有实现细节 |
| repository 合同与集成 | 合同夹具放 `conformance/`；在各自支持装配中用真实 PG/SQLite 验证事务、唯一键、迁移、锁和恢复，不用内存 fake 代替耐久证据 |
| 协议与 SDK | 用同版正反向量验证 Go/TS、严格解码、原命令持久化、WSS/gRPC 重连与兼容；生成结果必须一致 |
| 端到端与故障 | 按[验收矩阵 F01–F25](docs/architecture/validation/README.md#2-关键故障矩阵)选择受影响项；覆盖重复、乱序、丢回执、提交未知、取消、旧 worker、跨租户和恢复，读取双方账本、真实出站及目标真值 |

必须先取得身份与持久责任的故障证据，再接真实外部副作用。首条闭环按[实施顺序](docs/architecture/engineering/implementation-readiness.md#1-按这个顺序开发)推进；性能比较固定直接回答、读后回答、冷恢复、保存并读回四类请求。数据库耐久、互操作、三 AZ 容灾和任务质量必须分别取证，不能由单测或演示代替。

### 完成一次变更

- 必须在交付前检查最终 diff、生成物和真实检查结果；修复后重跑受影响检查。报告修改路径、实际命令、通过/失败/未运行、具体阻塞和剩余风险，不把未建立的能力记为完成。
- 运行证据必须绑定 commit、profile/Schema/method 摘要、配置、环境、数据集和故障点；有未提交改动时同时注明工作区差异，不能只报 HEAD。生产或质量声明不得超过已验证范围。
- 变更字段、owner、接口或能力范围时，必须同步对应设计、合同及验收。需要改变既有决策时先明确 ADR 冲突并取得决策；不要用实现或新增规范悄悄覆盖原选择。提交、推送与发布按用户授权执行。

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
