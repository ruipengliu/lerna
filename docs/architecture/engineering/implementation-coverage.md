# 实施覆盖与证据

2026-10-03：仓库已有可运行参考内核和受信报告闭环，完整项目仍为 **partial**。
本表按[项目目标](../../harness-project-goals.md)、[故障矩阵](../validation/README.md)
和[实施规格](../../../.scratch/full-implementation/spec.md)逐项记录。
真实账户、身份基础设施、设备与规模前提缺失的条目保持 blocked；有限参考实现通过不能关闭这些条目。

## 已取得的证据

公开 Dispatcher/SDK、真实持久 SQLite、本机 PostgreSQL 17.11、Linux 文件介质、
实际 HTTP/TLS/mTLS/WebSocket 出站是当前实现证据。测试明确提供受信用户、预批准规则和
有限目标配置；这些前提不证明生产公司身份、自然语言理解或任意外部组件可用。
ID 按各测试生成随机准确身份，不依赖固定种子，也不以 mock 数据库证明原子性。

以下 JSON 保存在本次执行环境，属于外部验收制品，不在仓库内保存凭据或整份运行日志：

| 制品 | 绑定与实际范围 |
| --- | --- |
| `/workspace/harness-dev-environment/environment.json` | 工具版本、实际 PG/SQLite 连通、生成与编译 smoke；早期环境探针不代替当前全项目回归 |
| `/workspace/harness-dev-environment/storage-verification.json` | commit `1b1cad6`；PG/SQLite 真实原命令、历史、Job/Claim、提交回复丢失、SIGKILL 与恢复，race 通过 |
| `/workspace/harness-dev-environment/query-binding-verification.json` | commit `87de73a`；原 query_id/结果摘要/固定期限、当前权限、PG 锁竞争与 Interaction 分页，真实双库与 race 通过 |
| `/workspace/harness-dev-environment/execution-verification.json` | commit `a5451a3`；公开执行套件双库通过，native 文件、三台模拟设备、实际 TLS/mTLS 与 SDK 原责任恢复；限定单物理 Attempt |
| `/workspace/harness-dev-environment/memory-providers-verification.json` | commit `418ed04` / 修复增量 `0220d89` / 有限快照 `9368334`，模型材料 `8febdd7`；Content/Memory 完整 SQLite 回归、逐页来源过期双库/race、未遍历预算与底层错误保留、200 候选长解释/实际 JSON 上限、模型原 HTTP 字节/回复恢复 |
| `/workspace/harness-dev-environment/model-assembly-verification.json` | commit `c5831ca`，开发宿主迁移 `6031797`；SQLite/PG 原历史/附件、先授权与预留再 POST、USD 0.00024 双方结账；SQLite 原回复丢失/缺凭据零出站。制品已保存实际 PG 原 Profile/Call/Decision 与双方账务结果 |
| `/workspace/harness-web-qa/original-native-6103c2e/original-result.json` | 文件中实际 commit `978cecf`；无代理的原生 WebSocket、持续报告、published Result、两次 heartbeat、无 page error。目录名不替代记录中的 commit |
| `/workspace/harness-web-qa/control-original-a4cb917/report.json` | commit `a4cb917`；原 Task 的真实 pause/resume/cancel、明确旧 CAS 冲突、窄屏/导航/登出责任、实际 CSP，无 page error；只证明该有限 control 流程 |
| `/workspace/harness-web-qa/` 中完整 Web 回归 | **pending**：当前正在验证长报告、回执丢失/reload、受信输入、Surface、管理与窄屏；失败 trace 保留，不能计作通过 |

公开外壳采用 `harness/1`、profile `architecture-2026-10-data1`，gRPC 包为 `harness.v1`。
共同 core Schema SHA256 为
`168c24b7f7a29b4e64b5e35fedb76333b9b259920dd51f2de4f4fdb4d8742eaf`。
具体方法、Profile、编码、模型费率与有限窗口由准确配置固定；不能把一个测试 Profile 的通过
外推至别的供应商或生产身份。运行命令见[根 README](../../../README.md)和对应模块 README。
本文不汇总未经去重的“通过测试数”或以子进程 helper 数量计算能力完成率。

## C1–C9 能力

表中“参考范围”表示指定路径具有实现合同证据；“部分”表示目标还有未实现或未验收内容。
测试链接指出可重复的行为入口，整体结论以对应执行制品为准。

| 目标 | 实际实现与行为检查 | 当前结果与边界 |
| --- | --- | --- |
| C1 理解、规划、决策 | [Task](../../../internal/task/)、[Brain](../../../internal/brain/)、[报告闭环](../../../adapters/development/app_test.go)：准确 GoalDocument、语义条件/覆盖、固定 Snapshot/Decision、≤4 行动批次、当前控制与无进展门禁；[HTTP 模型](../../../conformance/providers/)冻结材料、严格草稿与原用量 | **部分**。闭合 report 模板可保存、执行、读回、核验和导出；可选模型协议已验证。通用自然语言规划、真实模型质量与付费账户尚未验收 |
| C2 记忆与个性化 | [Memory](../../../internal/memory/README.md)，[纠正](../../../internal/memory/records_test.go)、[提取](../../../internal/memory/jobs_test.go)、[期限](../../../internal/memory/retention_test.go)、[分页](../../../internal/memory/query_binding_test.go)、[受控视图](../../../internal/memory/view_test.go)：事实/偏好/推断/经验、来源闭包、候选唯一保存、墓碑、词法排序和连续水位 | **部分**。许可先于读/排序；每页核原查询/text/scope 当前来源期限，不等 timer。默认提取是闭合值导入；自然语言提取、端侧挖掘、跨地点同步及个性化实际改善未验收 |
| C3 获取内容与证据 | [Content](../../../internal/memory/content.go)准确上传/发布/读回、[证据 gate](../../../internal/governance/evidence.go)、[模型材料](../../../conformance/providers/materials_test.go)实际 bytes/hash/length/来源 | **部分/外部 blocked**。授权上传、真实本机目标内容和签名证据可用；真实搜索/网页正文 adapter 及取得时间、来源冲突专项尚未开放 |
| C4 工具与设备 | [Execution](../../../internal/execution/README.md)、[执行集成](../../../conformance/integration/execution_test.go)、[native 文件](../../../adapters/execution/file_test.go)、[模拟设备](../../../adapters/execution/phone_test.go)：真实入口、原 Attempt/Effect、准确资源、独立读回与接管 | **部分**。本机 Linux 文件、三台持久模拟手机、被动 Environment 和受信纯计算有证据；不可信 WASI、真实外部 API、完整 GUI 动作、多 Attempt 安全重试和真机未开放 |
| C5 持续运行与协同 | [Runtime](../../../runtime/)、[存储合同](../../../conformance/contract/)、[输入/触发](../../../internal/interaction/)、[本地协作](../../../internal/task/collaboration_adapter_test.go)：原命令、Job、取消、重启、未知责任、子 Task/预算/原输入转交 | **部分**。本机同 owner/显式同库参考责任可恢复；云端 Task 不由设备缓存完成。独立设备有限授权、外部 Agent、多 owner/跨主机闭环与断网部署未验收 |
| C6 扩展与互操作 | [Go SDK](../../../sdk/go/)、[TS SDK](../../../sdk/ts/README.md)、[WSS](../../../conformance/integration/wss_test.go)、[gRPC](../../../conformance/grpc/)、[扩展](../../../internal/governance/extensions_test.go)：严格同版 JSON/Schema、原 journal、真实 TLS/mTLS、绑定代次、独立批准回退 | **部分**。参考同版合同与 SDK 恢复有证据；三系统完整第二实现、自定义 Skill/外部 Agent、任意不可信插件与生产 gateway/channel 拓扑未全部验收 |
| C7 权限与本人控制 | [授权](../../../internal/governance/governance_test.go)、[离线租约合同](../../../internal/governance/leases_test.go)、[原提交者](../../../internal/task/subject_test.go)、[内容门禁](../../../internal/memory/permissions_test.go)、[Surface](../../../internal/interaction/surface_test.go)：可信确认、用途交集、once 不返还、撤权、删除及设备 epoch | **参考范围通过；完整目标部分**。当前开发身份账本/已注册 ES256 key 可用；真实公司 OIDC、密钥发放、跨 owner 权威/真实设备离线租约部署继续 blocked |
| C8 观测与评测 | [用量与独立证明](../../../conformance/integration/execution_test.go)、[治理评测](../../../internal/governance/evaluation_test.go)、[真实两臂运行器](../../../adapters/governance/reference_test.go)、上述 JSON 运行制品 | **部分**。原责任、实际字节与费用事实可查；严格区分接纳、效果、完成、导出与费用。生产完整遥测、真实供应商账单结清、开放任务质量/时延/成本目标未验收 |
| C9 受控改进 | [安装与回退](../../../internal/governance/extensions_test.go)、[真实内置宿主](../../../adapters/governance/builtin_test.go)、[冻结两臂](../../../adapters/governance/reference_test.go)、[持续缺陷](../../../internal/governance/notices_test.go)：准确 allowlist、真实退出、旧 journal、独立批准与 Result notice | **部分**。仅受信 `reference-rule-file` 模板在独立真实目录按冻结字面真值比较；统计门槛与实际改善分别报告。没有开放自然语言改善、任意代码隔离、真实 holdout/高影响校准或自动发布权限 |

## A1–A4 边界

| 目标 | 实际依据 | 当前结果 |
| --- | --- | --- |
| A1 逻辑分工 | `internal/task/brain/execution/memory/interaction/governance` 各自负责原事实；Runtime 不裁决任务成功，宿主显式声明 Tx participants；[报告闭环](../../../adapters/development/app_test.go)跨真实组件推进 | 参考范围符合；不能由进程共置推断中央账本写权 |
| A2 独立替换 | 消费方小型端口；RuleEngine/HTTP Engine 分别编码与执行；PG/SQLite 独立实现；排名策略固定 ComponentRef | 部分：端口及适用替换有证据，三系统完整第二实现尚未全部提供 |
| A3 端云部署 | PG 云端权威与 SQLite 设备合同分别维护；独立 Gateway/Application/Worker 入口；Executor 不写云端 Task | 部分：本机进程与设备领域行为可运行，独立设备授权/远端 Authority、跨主机端云及生产发现/证书仍未完成部署验收 |
| A4 共同契约 | 唯一 core 源、typed `api.Contract[I,O]`、发现摘要、Go/TS Schema、原 profile/TTL、原 owner、WSS/gRPC 正反例与原回复恢复 | 参考同版方法符合；不覆盖未登记方法、历史 profile 全兼容或任意第三方扩展 |

不为一整项授予笼统 L3/L4：局部故障与治理证据不能证明全部子能力、平台和目标质量。

模型进程装配 commit `c5831ca` 的[公开测试](../../../adapters/development/model_test.go)另已在实际 SQLite/PG 通过（46.907s）：
原 Session 历史、准确附件与当前 TaskContextFacts 冻结进物理请求；
Task 非零预留和准确 allowed Use 均提交后才发一个 POST；
真实测试 HTTP usage 为 prompt=100/cache=40/output=20，Task 与治理两方结清 USD 0.00024。
测试草稿使 Task 明确 failed 且没有 Result；重开同 Decision 不再 POST。
另有实际 HTTP 断连的原 Call、AccountingOpen、非零预留与不二次请求，以及缺凭据零出站正反例。
这建立模型协议与账务装配证据；不建立自然语言质量或真实供应商结算。
该次准确命令为私有环境注入 `HARNESS_DATABASE_DSN` / `HARNESS_TEST_POSTGRES_DSN` 后
运行 `go test -p 2 ./cmd/internal/bootstrap -run '^TestConfiguredModel' -count=1`。
宿主迁移 commit `6031797` 后对应检查入口变为 `./adapters/development`；
[开发宿主](../../../adapters/development/)保留 app/model/governance 参考规则，
`cmd/internal/bootstrap` 仅保留入口装配 shim。

## V1–V2 专项

| 专项 | 已有证据 | 未完成验收 |
| --- | --- | --- |
| V1 联网问答 | Content 原始来源、当前许可/保留期、模型实际声明材料与原 HTTP 字节，严格结果/费用事实 | **blocked**。没有真实 Search/Body 供应商及冻结联网任务集；无答案、冲突、时效、引用支撑和检索失败的整体质量尚未验证 |
| V2 手机 GUI | 三台独立持久模拟手机；观察 → `set_note` → 再观察 → 真实状态核对；旧观察拒绝、epoch 接管和重启门禁 | **partial**。当前动作仅 `set_note/open_notes/press_home/set_wifi`；通用点击、滑动、返回和完整任务专项未全部实现/验收。Android/iOS 真机平台另行验收 |

## F01–F25 故障映射

每行列出本次参考实现的具体行为证据及尚未覆盖的刺激。
“局部”不是整组通过，特别是跨 owner、断网、生产拓扑和外部真值不能由本机测试替代。

| 组 | 实际代码/测试 | 结论与缺口 |
| --- | --- | --- |
| F01 原命令 | [存储崩溃](../../../conformance/contract/storage_crash_test.go)、[Task 丢提交回复](../../../internal/task/persistence_test.go)、[Go 原 journal](../../../sdk/go/client_test.go)、[WSS 丢回执](../../../conformance/integration/wss_test.go) | 双库原子集合、实际 SIGKILL、重开原库与沿原 command 恢复通过；完整 Web reload 当前 pending |
| F02 Job 竞争 | [双库合同](../../../conformance/contract/durable_test.go)、[并发/过期](../../../conformance/contract/storage_concurrency_test.go)、[Worker 续租](../../../runtime/worker_test.go) | 原 epoch/work_revision、新 Raise 与提交前期限复核有真实双库/race 证据 |
| F03 发送未知 | [模型丢回复](../../../conformance/providers/recovery_test.go)、[执行丢回复](../../../conformance/integration/execution_test.go)、[文件原 journal](../../../adapters/execution/file_test.go) | 实际目标/HTTP、持久 send_started、提交未知不出站、原查询不重发通过；远程供应商 CallID 查询与额外物理重试未开放 |
| F04 控制竞争 | [Task 控制](../../../internal/task/service_test.go)、[完成提案](../../../internal/task/check_pipeline_test.go)、[Executor](../../../conformance/integration/execution_test.go)、上述 control Web 制品 | 早取消、pause/resume 旧提案/窗口、迟到效果不重开终态及真实 UI CAS 冲突有证据；完整 UI 控制矩阵 pending |
| F05 完成漏项 | [当前完整覆盖](../../../internal/task/behavior_test.go)、[真实检查流水](../../../internal/task/check_pipeline_test.go)、[闭环](../../../adapters/development/app_test.go) | 空条件/坏证据/未完成检查与旧控制阻止完成，文件必须独立读回；通用任务检查器仍按准确登记范围开放 |
| F06 证据缺陷 | [缺陷与 Result](../../../internal/governance/governance_test.go)、[101 holder 通知](../../../internal/governance/notices_test.go)、[Task gate](../../../internal/task/check_pipeline_test.go) | 同库当前资格、完整依赖、Result notice/Job/游标同 Tx 和跨 100 项续页有证据；生产跨分片竞争未取证 |
| F07 跨域资格 | [签名/epoch/连续导入](../../../internal/governance/evidence_test.go)、[准确 ES256](../../../adapters/platform/proof_test.go) | 有限签名合同、缺口关闭与原 receipt 不续期已验证；真实远端 authority、失信时钟/生产资格缓存部署 blocked |
| F08 授权消费 | [Grant](../../../internal/governance/governance_test.go)、[本人答案](../../../internal/task/input_test.go)、[Surface](../../../internal/interaction/surface_test.go) | 当前父链交集、once 零账单不返还、准确确认/原请求消费有证据；公司本人身份与真实 GUI 全文呈现回归 pending |
| F09 账务 | [累计费用](../../../internal/task/behavior_test.go)、[独立 Usage](../../../conformance/integration/execution_test.go)、[模型费用](../../../conformance/providers/cost_test.go) | 累计差额、迟到更正、固定单位/预留与 USD 上取整有证据；真实供应商后续账单/退款争议未验收 |
| F10 分配关闭 | [ChildHandle/Allocation](../../../internal/task/child_test.go)、[本地协作](../../../internal/task/collaboration_adapter_test.go) | 原子内部子任务、丢答复、先关闭与原 steer/answer 有证据；跨 owner receiver 失联、外部 Agent 额度转移 blocked |
| F11 内容治理 | [准确出版](../../../internal/memory/content_test.go)、[ready/删除恢复](../../../internal/memory/recovery_test.go)、[holder](../../../internal/memory/permissions_test.go) | 实际对象字节/元数据发布、原票据提交未知、cleanup 幂等通过；跨库注册/关闭、真实外部副本/备份物理清除未验收 |
| F12 检索水位 | [连续索引](../../../internal/memory/batch_test.go)、[权限变化](../../../internal/memory/permissions_test.go)、[原查询期限](../../../internal/memory/query_binding_test.go)、[有界长解释](../../../internal/memory/query_test.go) | 200 项之后连续补扫、当前许可先过滤、visibility 扩张拒旧页、固定 TTL、200 长解释的有限快照与实际 JSON 分页通过；生产并行索引/海量扫描未取证 |
| F13 私密派生 | [未披露来源](../../../internal/memory/content_test.go)、[独立派生期限](../../../internal/memory/retention_test.go)、[查询原来源到期](../../../internal/memory/query_binding_test.go)、[实际模型材料](../../../conformance/providers/materials_test.go) | processed 闭包、主动 close、读不延期限、timer 未跑也拒旧页、材料不自动扩展通过；跨位置同步/外部镜像残留为缺口 |
| F14 GUI/文件 | [native 文件](../../../adapters/execution/file_test.go)、[旧观察/接管](../../../adapters/execution/phone_test.go)、[设备领域](../../../conformance/integration/execution_test.go) | os.Root 路径、symlink/hardlink 拒绝、版本 CAS、未知路径占用与三设备 epoch 有证据；完整 GUI 动作/其他平台与掉电未验收 |
| F15 程序环境 | [hostcall/checkpoint/实际退出](../../../conformance/integration/execution_test.go)、[受信计算](../../../internal/execution/compute.go) | 被动数据恢复、原 hostcall、未知映射阻后续 cell 和取消不冒充退出有证据；不可信 `run_cell` 无合格 WASI 隔离，保持关闭 |
| F16 输入与分支 | [queued/sending](../../../internal/interaction/submission_test.go)、[原输入](../../../internal/interaction/input_test.go)、[分支](../../../internal/interaction/session_test.go) | 当前准确消费、撤回竞争及迟到输出归原分支有证据；完整多窗口 UI 故障矩阵 pending |
| F17 Schedule | [未知槽/暂停/恢复](../../../internal/interaction/schedule_test.go)、[锁定 tzdb/DST](../../../internal/interaction/calendar_test.go) | 原 occurrence、未知并发占用、resume 不补发暂停点和 gap/fold 规则有证据；跨主机冷恢复/长期时间异常未验收 |
| F18 集合与订阅 | [原 query binding](../../../conformance/contract/query_binding_test.go)、[Memory list](../../../internal/memory/list_test.go)、[分页影响](../../../internal/memory/batch_test.go)、[Session](../../../internal/interaction/paging_test.go) | 有界持久 cursor、当前 scope、跨页变化和大于一页的数据控制有证据；完整订阅/Change 与大扇出 Task 控制仍受声明容量限制 |
| F19 安装/回退 | [扩展当前代次](../../../internal/governance/extensions_test.go)、[实际宿主](../../../adapters/governance/builtin_test.go) | 迟到初始化、独立旧批准、原进程死亡与真实 fence 有证据；不可信恶意插件/跨宿主接管未开放 |
| F20 实验污染 | [Formal/千样本取消](../../../internal/governance/evaluation_test.go)、[真实独立两臂](../../../adapters/governance/reference_test.go) | formal 次数与分母不返还、曝光/封存/迟到失效、真实字面真值有证据；千样本分页不表示千次 live API，真实 holdout/校准继续 blocked |
| F21 通道恢复 | [真实 WSS](../../../conformance/integration/wss_test.go)、[gRPC 原 Reply/Ack](../../../conformance/grpc/channel_test.go)、[SDK](../../../sdk/go/grpc_reply.go) | 实际 TLS/mTLS、原序号/回执/重绑、普通槽饱和时控制可用有证据；生产 gateway 多实例 stale-output、PG channel SIGKILL 及完整 Web 长运行仍未全部取证 |
| F22 AZ/时间 | [原 database_id](../../../conformance/contract/storage_concurrency_test.go)、[有限期限](../../../conformance/contract/query_binding_test.go) | 仅本机身份/期限合同；三 AZ 同步确认、旧主隔离、UTC 跳变/VM 暂停、RPO/RTO **blocked** |
| F23 洪峰/容量 | [gRPC 控制份额](../../../conformance/grpc/unary_test.go)、[有界 Task 集合](../../../internal/task/bounds_test.go)、[传输队列](../../../adapters/grpc/queue.go) | 局部容量拒绝/控制份额有证据；10 倍租户洪峰、冷重连、单区损失与生产吞吐/积压收敛 **blocked** |
| F24 灾备/增长 | [旧数据库身份拒绝](../../../conformance/contract/storage_concurrency_test.go)、[原墓碑](../../../internal/task/persistence_test.go)、[cleanup](../../../internal/memory/recovery_test.go) | 原身份/未知残留不复活有证据；旧备份最近撤权完整性、磁盘保护、历史增长计划/索引与灾备恢复 **blocked** |
| F25 血缘/条件 | [目标语义 upsert](../../../internal/task/behavior_test.go)、[原答案](../../../internal/task/input_test.go)、[当前覆盖/完整检查](../../../internal/task/check_pipeline_test.go)、[租户/来源](../../../internal/memory/permissions_test.go) | 原文与来源、硬要求不被静默删除、旧修订/空条件/跨租户拒绝与当前覆盖有证据；开放自然语言候选正确率未验收 |

## 未完成前提与收尾

- 真实模型/搜索/正文供应商凭据、原调用能力探针和最终计费对账；V1 真实来源任务集。
- 公司 OIDC、密钥注册/轮换与真实本人身份；跨 owner 权威、独立设备有限 GrantLease 和远端门禁装配。
- Android/iOS 真机及未测操作系统/文件系统；完整模拟 GUI 动作和不可信 WASI 隔离。
- 三 AZ 同步 PG/对象耐久、主库隔离、可信时间异常、备份最近撤权、生产恢复与 RPO/RTO。
- 1000 语义不同 API、首次正确率 ≥90%、有限重试成功率 ≥95%，以及最终用户规模、290/500 Task/s、20 万 WSS 的容量实验。以上均是设计目标，没有本轮达标结果。
- 完整浏览器回归及三角色 PG 报告的最终 checkpoint/结果仍 pending；有限 control Web 和模型协议/账务整链已取得独立制品。

默认适配保持缺前提的能力关闭，返回具体 `unsupported`、`blocked/not_run` 或原未知事实。
工单只在自身范围真实验收完成后关闭；总体状态不能由目录数量、接口可编译或设计模型检查推导。
