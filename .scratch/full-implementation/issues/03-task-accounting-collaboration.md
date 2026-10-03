# 03 task-accounting-collaboration

Status: partial
Blocked by: C3 搜索/正文获取与 V2 完整手势的本地实现缺口，及跨 owner 权威与交接、外部 Agent、独立设备和生产部署的后续装配及验收

依据 [实施规格](../spec.md) 与根 AGENTS.md，保留准确身份、负责方、事务、门禁和恢复。实际编译、公开接口行为、正反例及所需平台证据均通过后才关闭。

## Comments

本方 Orchestrator、Accounting、内部协作和 ChildHandle 参考合同已实现，完整架构仍为 partial。实现范围与端口合同见 [Task 实现说明](../../../docs/architecture/orchestrator/implementation-notes.md) 和 [本地协作 adapter](../../../adapters/collaboration/README.md)；总体能力、故障及外部前提以[实施覆盖](../../../docs/architecture/engineering/implementation-coverage.md)为准。

| 已实现责任 | 实际行为与门禁 |
| --- | --- |
| 目标、条件、输入、完成 | 冻结准确目标原文；条件候选按语义 upsert，保留未提及的硬条件；输入按原请求、Schema、goal/control 一次消费。完整当前覆盖、必要检查、证据 gate 和完整关系共同裁决；空条件、旧版本、负检查和未知效果不能产生成功 Result |
| Snapshot、Decision、行动 | 固定 Snapshot、派发意图、原命令和预留；至多四个独立行动整批准入。GrantUse 与原 IntentHash、ActionConsumption、预算和 Job 同事务；冻结提交者代次与角色，当前门禁核撤权。无进展保存有限等待，确认回滚的 stale Context 沿原 Job 重调度，提交未知保留原身份 |
| Accounting、Allocation | 按准确源修订/摘要归并累计差额、原预留、迟到费用和独立退款。内部分配唯一接收、关闭先到永久门禁、签名原 closure 和迟到父预算差额；纯账务未结不等于目标或效果未关闭 |
| 控制、Result、恢复 | 当前 Task/祖先门禁、根到叶及源记录锁序、最多五秒签名控制窗口。输入先准备，再核当前门禁签首窗；原 invoke 重放不刷新窗口、命令或预留。终态 Result 不可变，Content 导出有独立 Job，治理缺陷沿原 Result 保存 notice |
| 内部协作、ChildHandle、受信装配 | 显式同库创建子 Task/额度并核完整有界子树效果；ChildHandle 先准备原 Session 命令，新目标 CAS 需旧目标和效果关闭。实际本地 Session/steer/answer 转交仅据原消费方回执归并。批请求视图先锁全部 Task 再锁准确请求；ContextFacts、冻结 Decision/Snapshot/上界及原意图等 typed 用例供宿主装配，不是新增公开线方法 |

公开方法以闭合 `api.Contract[I,O]` 登记；16 类 Task Job 均有实际 handler。跨模块仅使用消费方小端口；同库参与者由宿主显式声明，外部准备、字节读取、发送和取证在 Tx 外。缺少实际端口、签名或当前资格时关闭相应入口或保持准确等待，不以目标正文冒充证明。

## 已运行证据

- `3704590` 锁序修复合入后，完整 `go test ./internal/task ./adapters/collaboration -count=1` PASS 85.517s。Task 默认套件使用持久 SQLite；配置真实 PostgreSQL 后，指定的生命周期/证据/累计账务、反序批请求、账务与决策竞争合同也实际运行，不代表每个用例都运行了两个数据库。受影响 PG、ChildHandle、本地协作与检查范围 race PASS 46.369s。两条真实 PG 反序锁曾 RED SQLSTATE 40P01，修复后按原身份正确提交或明确拒绝 stale Snapshot。
- `3a2405f` 执行准备切片：真实 SQLite 的 5.2 秒准备先 RED（耗尽原五秒窗口），移动准备阶段后 GREEN；准备期间取消、实际 DevIdentity 撤权阻止派发；真实 Memory/ObjectStore 出版后丢准备回执，恢复同一 ContentRef 和预留；原 invoke 丢回执并跨窗口期限后保持准确旧命令/窗口。该范围及 legacy 端口、原凭据/旧 Claim 共八项 race PASS 84.568s；Task/Collaboration/development vet、宿主装配编译与文档检查通过。测试入口见 [执行准备](../../../internal/task/dispatch_preparation_test.go)、[持久恢复](../../../internal/task/persistence_test.go)、[检查流水](../../../internal/task/check_pipeline_test.go) 和 [本地协作](../../../internal/task/collaboration_adapter_test.go)。
- 宿主集成另取得真实文件写入、独立读回、检查、不可变 Result、Content 导出及 DB 重开的 [报告闭环](../../../adapters/development/app_test.go)：SQLite PASS 20.63s、PG PASS 48.05s，总包 68.697s。制品 `/workspace/harness-dev-environment/report-assembly-verification.json` 固定实现 `7232c316`、Task 准备 `3a2405f`、准确 profile/Schema 和运行前提。宿主随后补充 [独立 Application/Gateway/Worker](../../../cmd/internal/runner/services_test.go) PG PASS 75.821s，以及 fresh Web 原 Result 与准确全文呈现通过；这两项是后续集成证据，旧报告制品的 `not_verified` 仍把三角色列为失败/诊断中，尚待宿主同步后续结果，不能当作已更新。
- 本人澄清后的派生目标来源已修复：宿主按冻结 GoalDocument 的完整组件和 Snapshot 的 MaterialRefs/ProcessedSources 双声明，展开 Task 保存的原 `SourceEvidence`，保留最初提交命令、原请求、类别及位置；验证器逐条核完整原依据，包装引用不成为新本人证明。真实 [公开澄清流程](../../../adapters/development/clarification_test.go) 首次提炼曾 RED（goal2、零要求），修复后 JSON 字符串与真正 `text/plain` 均沿原 submit/input、GoalDocument、两项要求、三个真实文件步骤、独立检查、verified Result 出版、原回执与 DB 重开通过。SQLite 两链及来源拒绝合集 PASS 76.539s；PG 原文本 PASS 77.59s、隔离 JSON 链 PASS 86.409s（补充后出版 75.664s）。缺少原组件声明、外来包装、伪造提交/类别/位置及混入外来来源均有真实拒绝反例；旧实现负例 RED，新版 GREEN。原浏览器 failed Task 与其拒绝事实不复活，首次 PG 外层 90s 超时仍保留为失败证据。
- 原始文本 RuleEngine 编码 panic 经 [公开编码与旧编码恢复](../../../internal/brain/rules_encoding_test.go) RED→GREEN。私有 base64 信封保留非 JSON 原字节和原 ContentRef 摘要；合法 JSON 保持旧格式逐字节不变。双表示、缺表示及无效 bytes 被拒绝；真实 SQLite 在 encoded 阶段重开、移除原 Goal 字节且禁止再次 Encode 后仍消费原保存编码和命令。Brain 全包 PASS 12.311s、race PASS 28.996s。没有新增公开协议方法或扩大全局字节/计费限额。
- 宿主 [Schedule 安装锁正反例](../../../adapters/development/schedule_gate_test.go) 通过实际 Dispatcher、未来 timer 与交付 Job，证明正确安装锁的 create/update applied，模型 profile 放入安装锁字段被拒绝且无新 Schedule/触发责任。旧门禁曾 RED（错误模型引用 applied），改核实际 InstallLock 后 SQLite 与 PostgreSQL race PASS 51.286s；合法更新仍复用原 trigger，已冻结 occurrence 沿旧规则/准确锁和原 Task 命令交付。

development 全包 SQLite race 首次实际运行 376.293s 未通过：原报告与两条新澄清链各耗尽 90s 测试外层 context，没有 race detector 报告。根据公开阶段实际耗时，报告及澄清 fixture 的外层等候调整为有限 180s；Task 的五分钟期限、原命令一分钟期限、五秒控制窗口及浏览器补充后 90s 验收保持原值。之后分别串行隔离的 Report race PASS 109.808s、JSON 澄清加来源拒绝 race PASS 161.932s、原始文本澄清 race PASS 145.002s；原 376.293s 失败不改记通过。两库多段 fixture 会使整个 development 包累计超过五分钟，检查入口与 CI 的 Go 单包等待有界设为十分钟，仅是测试 runner 上限，完整入口与托管 CI 实际运行仍由集成工单另行记录。

有界条件检查使用准确、预批准规则；默认报告闭环是明确正文/保存路径/读回要求的受信结构化目标，执行及许可费用明确为零。上述行为证明本方 Task 消费、账务和恢复，不证明自然语言规划质量、真实付费供应商账单或所有外部工具。可配置模型的实际 HTTP 协议/非零费用测试属于宿主与供应商合同的另项证据。

## 剩余边界

C3 尚无可替换的 Search/Body adapter 与其来源/时间合同；V2 模拟手机只开放四项较窄动作，完整 swipe/back 手势仍未实现。这是本地参考能力缺口，不能归因于真实账户或独立设备尚未配置。

跨 owner Delegation/Allocation、远端 Evidence/Grant authority、外部 Agent、独立设备有限授权和断网端云闭环尚未完成装配验收；当前本地协作 adapter 对未配置对端在新增责任前返回 unsupported。云端 Task 权威没有移交设备缓存，本机同 owner/同库与三角色 PG 进程的通过不能证明跨主机或独立设备部署。

真实公司身份/密钥基础设施、任意目标的语义条件与检查质量、真实供应商迟到账单/退款、三 AZ、时间异常、生产容量/历史增长及灾备目标继续按实施覆盖保留 blocked/partial。完整浏览器故障矩阵由集成工单继续记录，原 Result/全文单项通过不关闭其全部范围。本工单没有将完整架构标记 resolved。
