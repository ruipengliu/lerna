# 03 task-accounting-collaboration

Status: partial
Blocked by: 工单 11–24 新增本地切片的剩余集成验收，跨 owner控制／当前来源故障、复用Session及生产部署资格；普通远程和独立设备完整报告已有准确双库证据

依据 [实施规格](../spec.md) 与根 AGENTS.md，保留准确身份、负责方、事务、门禁和恢复。实际编译、公开接口行为、正反例及所需平台证据均通过后才关闭。

## Comments

本方 Orchestrator、Accounting、内部协作和 ChildHandle 参考合同已实现，完整架构仍为 partial。实现范围与端口合同见 [Task 实现说明](../../../docs/architecture/orchestrator/implementation-notes.md) 和 [本地协作 adapter](../../../adapters/collaboration/README.md)；总体能力、故障及外部前提以[实施覆盖](../../../docs/architecture/engineering/implementation-coverage.md)为准。

| 已实现责任 | 实际行为与门禁 |
| --- | --- |
| 目标、条件、输入、完成 | 冻结准确目标原文；条件候选按语义 upsert，保留未提及的硬条件；输入按原请求、Schema、goal/control 一次消费。完整当前覆盖、必要检查、证据 gate 和完整关系共同裁决；空条件、旧版本、负检查和未知效果不能产生成功 Result |
| Snapshot、Decision、行动 | 固定 Snapshot、派发意图、原命令和预留；每个目标/控制代次等待原未消费 Decision，编译前及准入短事务双核，原意图重放不新增预留；至多四个独立行动整批准入。GrantUse 与原 IntentHash、ActionConsumption、预算和 Job 同事务；冻结提交者代次与角色，当前门禁核撤权。无进展保存有限等待，确认回滚的 stale Context 或 pending Decision 沿原 Job 重调度，提交未知保留原身份 |
| Accounting、Allocation | 按准确源修订/摘要归并累计差额、原预留、迟到费用和独立退款。内部分配唯一接收、关闭先到永久门禁、签名原 closure 和迟到父预算差额；纯账务未结不等于目标或效果未关闭 |
| 控制、Result、恢复 | 当前 Task/祖先门禁、根到叶及源记录锁序、最多五秒签名控制窗口。输入先准备，再核当前门禁签首窗；原 invoke 重放不刷新窗口、命令或预留。终态 Result 不可变，Content 导出有独立 Job，治理缺陷沿原 Result 保存 notice |
| 内部协作、ChildHandle、受信装配 | 显式同库创建子 Task/额度并核完整有界子树效果；ChildHandle 先准备原 Session 命令，新目标 CAS 需旧目标和效果关闭。实际本地 Session/steer/answer 转交仅据原消费方回执归并。批请求视图先锁全部 Task 再锁准确请求；ContextFacts、冻结 Decision/Snapshot/上界及原意图等 typed 用例供宿主装配，不是新增公开线方法 |

公开方法以闭合 `api.Contract[I,O]` 登记；17 类 Task Job 均有实际 handler，新增 `context_lookup` 不承担隐藏工具行动。原 deadline 唤醒沿独立键使用既有 `task.advance`，不是新 Job kind。跨模块仅使用消费方小端口；同库参与者由宿主显式声明，外部准备、字节读取、发送和取证在 Tx 外。缺少实际端口、签名或当前资格时关闭相应入口或保持准确等待，不以目标正文冒充证明。

## 已运行证据

- `3704590` 锁序修复合入后，完整 `go test ./internal/task ./adapters/collaboration -count=1` PASS 85.517s。Task 默认套件使用持久 SQLite；配置真实 PostgreSQL 后，指定的生命周期/证据/累计账务、反序批请求、账务与决策竞争合同也实际运行，不代表每个用例都运行了两个数据库。受影响 PG、ChildHandle、本地协作与检查范围 race PASS 46.369s。两条真实 PG 反序锁曾 RED SQLSTATE 40P01，修复后按原身份正确提交或明确拒绝 stale Snapshot。
- `3a2405f` 执行准备切片：真实 SQLite 的 5.2 秒准备先 RED（耗尽原五秒窗口），移动准备阶段后 GREEN；准备期间取消、实际 DevIdentity 撤权阻止派发；真实 Memory/ObjectStore 出版后丢准备回执，恢复同一 ContentRef 和预留；原 invoke 丢回执并跨窗口期限后保持准确旧命令/窗口。该范围及 legacy 端口、原凭据/旧 Claim 共八项 race PASS 84.568s；Task/Collaboration/development vet、宿主装配编译与文档检查通过。测试入口见 [执行准备](../../../internal/task/dispatch_preparation_test.go)、[持久恢复](../../../internal/task/persistence_test.go)、[检查流水](../../../internal/task/check_pipeline_test.go) 和 [本地协作](../../../internal/task/collaboration_adapter_test.go)。
- 宿主集成另取得真实文件写入、独立读回、检查、不可变 Result、Content 导出及 DB 重开的 [报告闭环](../../../adapters/development/app_test.go)：SQLite PASS 20.63s、PG PASS 48.05s，总包 68.697s。制品 `/workspace/harness-dev-environment/report-assembly-verification.json` 固定实现 `7232c316`、Task 准备 `3a2405f`、准确 profile/Schema 和运行前提。宿主随后补充 [独立 Application/Gateway/Worker](../../../cmd/internal/runner/services_test.go) PG PASS 75.821s，以及 fresh Web 原 Result 与准确全文呈现通过；这两项是后续集成证据，旧报告制品的 `not_verified` 仍把三角色列为失败/诊断中，尚待宿主同步后续结果，不能当作已更新。
- 本人澄清后的派生目标来源已修复：宿主按冻结 GoalDocument 的完整组件和 Snapshot 的 MaterialRefs/ProcessedSources 双声明，展开 Task 保存的原 `SourceEvidence`，保留最初提交命令、原请求、类别及位置；验证器逐条核完整原依据，包装引用不成为新本人证明。真实 [公开澄清流程](../../../adapters/development/clarification_test.go) 首次提炼曾 RED（goal2、零要求），修复后 JSON 字符串与真正 `text/plain` 均沿原 submit/input、GoalDocument、两项要求、三个真实文件步骤、独立检查、verified Result 出版、原回执与 DB 重开通过。SQLite 两链及来源拒绝合集 PASS 76.539s；PG 原文本 PASS 77.59s、隔离 JSON 链 PASS 86.409s（补充后出版 75.664s）。缺少原组件声明、外来包装、伪造提交/类别/位置及混入外来来源均有真实拒绝反例；旧实现负例 RED，新版 GREEN。原浏览器 failed Task 与其拒绝事实不复活，首次 PG 外层 90s 超时仍保留为失败证据。
- 原始文本 RuleEngine 编码 panic 经 [公开编码与旧编码恢复](../../../internal/brain/rules_encoding_test.go) RED→GREEN。私有 base64 信封保留非 JSON 原字节和原 ContentRef 摘要；合法 JSON 保持旧格式逐字节不变。双表示、缺表示及无效 bytes 被拒绝；真实 SQLite 在 encoded 阶段重开、移除原 Goal 字节且禁止再次 Encode 后仍消费原保存编码和命令。Brain 全包 PASS 12.311s、race PASS 28.996s。没有新增公开协议方法或扩大全局字节/计费限额。
- 宿主 [Schedule 安装锁正反例](../../../adapters/development/schedule_gate_test.go) 通过实际 Dispatcher、未来 timer 与交付 Job，证明正确安装锁的 create/update applied，模型 profile 放入安装锁字段被拒绝且无新 Schedule/触发责任。旧门禁曾 RED（错误模型引用 applied），改核实际 InstallLock 后 SQLite 与 PostgreSQL race PASS 51.286s；合法更新仍复用原 trigger，已冻结 occurrence 沿旧规则/准确锁和原 Task 命令交付。
- [当前 Decision 串行门禁](../../../internal/task/pending_decision_test.go) 两库公开 RED→GREEN：原效果事实唤醒同一 Job 不能在当前原提案待消费时再出版 Snapshot 或预留第二 Decision。五项两库矩阵及 stale/提交未知、原期限重开、护栏与准备取消的受影响 race 实际 exit 0（138.977s）；原 known/unknown 费用、迟到 final 差额、原意图重放、目标/控制改变及取消均保留。证据索引在 `/workspace/harness-dev-environment/task-pending-decision-verification/`，夹具使用真实 Task 数据库和 Content 文件，但未配置物理模型出口。此前 WASI 原 Task `41382c` 的完整 Worker PG 失败和只读诊断仍保留；此项修复不代表完整 WASI 或远端 Agent 验收完成，Task/Command/Control 有限期限没有改变。

development 全包 SQLite race 首次实际运行 376.293s 未通过：原报告与两条新澄清链各耗尽 90s 测试外层 context，没有 race detector 报告。根据公开阶段实际耗时，报告及澄清 fixture 的外层等候调整为有限 180s；Task 的五分钟期限、原命令一分钟期限、五秒控制窗口及浏览器补充后 90s 验收保持原值。之后分别串行隔离的 Report race PASS 109.808s、JSON 澄清加来源拒绝 race PASS 161.932s、原始文本澄清 race PASS 145.002s；原 376.293s 失败不改记通过。两库多段 fixture 使整个 development 包累计超过五分钟，当时检查入口及 CI runner 等待有界设为十分钟；后续新增切片后的当前入口以 `scripts/check` 与同版 CI 为准，完整入口实际运行由集成工单记录。这些均不改变业务期限。

有界条件检查使用准确、预批准规则；默认报告闭环是明确正文/保存路径/读回要求的受信结构化目标，执行及许可费用明确为零。上述行为证明本方 Task 消费、账务和恢复，不证明自然语言规划质量、真实付费供应商账单或所有外部工具。可配置模型的实际 HTTP 协议/非零费用测试属于宿主与供应商合同的另项证据。

## 剩余边界

Search/Body、扩展模拟 GUI、隔离 WASI、独立设备、远端 Agent、Skill/AgentConfig、Channel 和第二实现的新增本地范围分别由工单 11–24 记录实际方法及未完成验收。它们的本地编码范围不能归因于缺生产账户；本工单既有 Task 证据也不能替代这些切片的实际验证。工单 14 的四种只读 resolver、普通材料持久责任/当前撤回门禁和有限偏好实际结果已完成：固定源码 SQLite C2 race PASS 623.820s，三原 Result/回执重开及费用闭合；两库 resolver race 与 PostgreSQL C2 normal 分别记录准确范围。总体实施覆盖仍是完整项目完成状态的依据。

跨 owner Delegation/Allocation、远端 Evidence/Grant authority、外部 Agent、独立设备有限授权和断网端云闭环按各自工单继续取证；当前本地方与远端小端口不能作为对端部署或生产资格证明。未配置对端仍在新增责任之前关闭入口。云端 Task 权威没有移交设备缓存，本机同 owner/同库与三角色 PG 进程的通过不能证明跨主机或独立设备部署。

真实公司身份/密钥基础设施、任意目标的语义条件与检查质量、真实供应商迟到账单/退款、三 AZ、时间异常、生产容量/历史增长及灾备目标继续按实施覆盖保留 blocked/partial。完整浏览器故障矩阵由集成工单继续记录，原 Result/全文单项通过不关闭其全部范围。本工单没有将完整架构标记 resolved。

原Remote r5恢复已真实关闭child Incoming和三层typed Closure，但父仍closing/reservedUSD2、原closure-report终态rejected。原Scope128.773s观察FAIL及随后准确schema_violation单列，正常成功的完整Task/Grant账务仍待正式修复；不得把child关闭或原Result存在当父费结清。

后继同原r5费用恢复17.070813s实际PASS，三层真实Closure、父Task/Grant reserve0、original once不重耗、原Results/actualjoin/LoadConfig-token重开验证通过（/workspace/harness-dev-environment/normal-delegation-accounting-verification/original-r5-legacy-ack-green-ready-20261003T234552825980Z/process-result.json）。旧128.773s/8.852s FAIL和旧report终态rejected仍保留；恢复不替代fresh双owner SQLite/PG完整报告，广泛工单仍partial。

历史限定更正：原r5的17.070813s只核费用／Result／join原cfg-token重开，当时Ack0与部分Job／proof仍在；后继128.633s恢复虽Ack／业务Job已闭，仍因额外unused proof过期而整体FAIL。fresh完整双库103.121656s／109.541635s后来独立通过，不能回填旧轮，后继终态proof正式叶后的原r5同Scope正确恢复17.535538s已实际通过，必要Job／proof和原cfg重开核验见下段；不改旧轮结论。

2026-10-04：正常委派费用正式叶c29b0fd9后，fresh完整SQLite父库／真实PG父库与独立SQLitechild分别103.121656s／109.541635s通过。双方自行3Ops／2verified checks／published Result，真实三层Closure、Task／原Grant reserve0、必要proof字节、相关原Jobs实际DONE及join／原cfg-token-Result重开均通过。准确索引 /workspace/harness-dev-environment/remote-agent-17-fresh-fee-jobs-20261004T001215543877Z/qualified-normal-report-index.json；保留r5旧失败和原unused过期出版拒绝。控制与当前来源故障、Session剩余路径及生产资格仍单列。

历史矩阵原8719PASS／1SQLprepared真实FAIL、8758PASS／1actorSQLFAIL／1actorPGNOTRUN与872 no-child FAIL保留。后继actor两库race、当前parentPause八谓词两库race、NoChild完整Closure／费用／必要Job两库race及Saved最新十项均已有准确资格，限定本地17实现resolved；最终新根完整检查／浏览器／全图审查仍待，不把各source拼成已经执行的新根整套。

原r5同Scope的corrected recovery已实际PASS17.535538s：Root579／895源与binary、原cfg／keys／token／数据库前后稳定，不新Task／Goal／Grant Use或续TTL。真实三层Closure／immutable Ack、父Task／原Grant reserve0、once保持、原双方Result不变；重开前后准确26份Published证明核原bytes／hash，额外旧unused18fce保Published=false／失败不可读，原expired reserve拒绝及无PUT／transfer／native事实不改。101项原必要Job实际DONE（parent44／child57），原publisher f162／f801与delegation／allocation／billing按原责任结束；四个实际handler join在两个Store.Close之前，LoadConfig原凭据重开再核全部断言。过程 /workspace/harness-dev-environment/normal-delegation-accounting-verification/original-r5-proof-set-corrected-ready-20261004T030949030145Z/process-result.json（SHA542916f1…），独立只读资格 /workspace/harness-dev-environment/normal-delegation-accounting-verification/original-r5-proof-terminal-independent-qualified-20261004T032244349699Z/index.json（SHA847ea998…）及 /workspace/harness-dev-environment/normal-delegation-accounting-verification/original-r5-proof-terminal-independent-qualified-20261004T032244349699Z/verification-result.json（SHA9c647396…）。原300.218s／128.773s／typed8.852s／严格128.633s与首次proof-set观察失败均保留，旧report终态rejected不改。只限定此原SQLite双owner恢复，不替代fresh SQL／PG正常报告或完整故障矩阵；原r5阶段观察到Task.Closure新IssuedAt产生另一proof／Job的历史保留；后继Gov三路径限定完整关闭Snapshot复用原首次proof，并有72.632s两库race证据，不扩为所有当前State查询幂等。

无子Task的准确收尾已经实现与验收：原late create的永久拒绝保原CID／Allocation／Delegation／once责任，不制造TaskClosure或伪Task。正式六路径6b471710a7e882eef1f5a1186cb4254a871d96c2已合Root02cb907；原业务create拒绝、无子Task／Op、准确Allocation与Delegation及no-child三层Closure、三份原签名证明实际出版bytes／hash、onceConsumed／reserved0／spent0、所有原必要Job真正DONE、handler／App join先于StoreClose及原cfg-token数据库重开均强断言。完整normal SQL41.293212s／PG41.409049s与后继显式fixture3m＋原observer2m的两库race179.611155s／178.459507s分别实际PASS，业务TTL不变。旧872 SQL120.087144s FAIL／PGNOTRUN、120.200707s零Reservation未闭、9.043083s原Scope恢复FAIL、32.968832s包装器被App重开替换而未观察到原Job的FAIL／PGNOTRUN、旧wholefixture2m race121.446508s在NoChild send之前到期／PGNOTRUN均保留。原unsent Decision零额账务修复三路径145856已在Root7c65，bound0／3及sentunknown两库race18.581903s通过；同原Scope仅ledger恢复22.880616s另记，不能替代完整三证明资格。准确normal／race／旧失败与six-file语义见 /workspace/harness-dev-environment/no-child-confirmed-closure-verification/formal-qualified-no-child-leaf-20261004T070003766699Z/truth.json（SHAa7dd5857…），Root02cb集成索引SHAef6c6e64…仅metadata。

统一阶段说明：当前CODE 58c9898af58babce7170cb37cbbdfc63d693af9e；本文实际通过只按所列原source／binary／selector及数据库制品限定。最新Saved十项、NoChild／Pause／现代Form／Closure后继资格已取得；最后完整检查／浏览器／全图审查尚未结束，旧失败／未跑记录不回填。
