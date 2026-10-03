# Task 实施记录

本记录对应 `internal/task` 与工单 03，记录实现和运行证据，不替代 [Orchestrator 合同](README.md)、[账务](../accounting/README.md)、[协作](../collaboration/README.md)或[存储](../data/storage.md)。

## 实现与装配

`task.New(Config, Ports)` 接收明确登记的政策、规则与答案 Schema，`Register` 同版登记闭合方法和 17 类工作。目标原文、完整 GoalDocument、条件及准确语义采纳、Task 控制、预留、累计费用、当前检查选择、不可变 Result 与独立出版责任保存在固定 Task owner 的原库。内部实现只依赖 `api`、`runtime` 和消费方小端口。

公开方法包含 Task 提交、控制、修订、输入、质量接受、预算调整、证据附加、费用通知、控制窗口与查询；额度 allocate/close/settle/read；账单调整提交；ChildHandle create/send/close/read/list/wait，以及准确 InputRequest read/list。`task.input` 的 accepted 表示准确答案已耐久排队，最终 applied 才表示原请求已消费。坏答案不消费请求，修正答案可以重新提交。

17 类工作为 advance、dispatch_decision、dispatch_operation、reconcile_operation、check、coverage、control、billing、publish_result、steer、delegation、allocation、child_prepare、child_transfer、input、adjustment、context_lookup，均以 `task.` 为前缀。外部读取、出版和发送在事务外；Claim 先于出站重核，资格和最终归并在有 Guard 的短事务中重新核验。

`need_context` 接纳一次固定批次内的 1–3 项只读查找。每次领取只处理一个原查询，恢复沿原 LookupID、QueryRef、Snapshot、目标/控制与有限期限；累计 calls/bytes/tokens 上界在批次准入时预扣，不因等待、控制或目标变化返还。批内前项与本次新材料共同重核当前资格，全部完成后才进入下一份 Snapshot。普通材料与本人原文 SourceEvidence 分开保存，不提升指令或授权等级；已撤回的准确 Memory 版本不能成为新 Decision 的材料。依赖等待原 Job，确定拒绝保留准确等待原因，不能通过新模型请求探测依赖。宿主 resolver、有限偏好模板及其真实结果验证范围见[工单 14](../../../.scratch/full-implementation/issues/14-context-lookups.md)。

Context 编译完成后若原 Task 已变化，仅在准入闭包明确 `stale_snapshot` 且事务确认回滚时重调度原 advance Job；没有新的 Decision 或执行意图准入。外部编译错误、失去领取及 CommitUnknown 不进入该路径，未知提交沿原身份核查，不能伪报已回滚或另造决策责任。

Task 提交同时保存以 `deadline/<TaskID>` 为键的独立 advance 责任，due 为原 Task deadline。它与立即推进的 `advance/<TaskID>` 分开，后者完成或重调度不能吞掉到期唤醒；仍使用同版 `task.advance` handler，不新增 Job kind。等待输入、暂停和重开均不续期，到期关闭原目标并取消原 Decision，未知效果及费用继续核对。受信维护用例 `RecoverDeadline` 只为已知旧 Task 补原期限责任或执行到期关闭，不扫描或重置目标。

受信内部用例 `AdoptRequirements`、`StoreCoverage`、`RecordCheck`、`Complete`、`PrepareDecision`、`ConsumeProposal`、`ContextFacts`、`CheckDecisionTx`、`DecisionSnapshotTx`、`DecisionCostBoundTx`、`OperationIntentTx`、`Closure`、`RequestViewTx`、`RecordResultNoticeTx` 等供宿主装配。它们不构成新公开线方法，也不允许 Brain 自报已消费 Grant、原 Operation 身份或已发生效果。决策读取从原准入 birth 返回冻结 Snapshot 和原 Reservation 的准确上界；当前账务已结或变化不能重新定价或降低该原上界，当前发送资格仍须单独调用 `CheckDecisionTx`。

同库 `RequestViewsTx` 一次核验至多 20 个准确请求，拒绝重复、跨租户、跨 owner、错误主体与旧请求版本。不可变请求 birth 和准确历史 Task 负责完整根路径路由；全部 Task 按根到叶、同层 ID 锁定之后，再按 ID 锁全部当前请求，输出保留原输入顺序。单个 `RequestViewTx` 复用这条路径，供 Interaction 的共同事务先核请求再写 Surface。

内部子 Task 的创建事务在原 version2 固定祖先根路径；之后读取当前 Task 先由这个不可变版本路由并锁根到叶。Decision、Reservation、Delegation、Allocation 和已绑定子 Task 的 incoming quota，同样由原 birth 路由 Task 之后再锁当前源行。修订仅改变原事实和控制，不改变源所属 Task；路由不完整或变化时拒绝使用。

宿主必须显式声明共享数据库与 Tx participants，并提供：

- Content 字节读取及固定出版；Context 编译；Brain、Execution、外部取证及协作端口。
- Context 可实现纯 Tx `ContextCommitter`，在原 Snapshot/Decision/Reservation 和 Job 意图的共同事务登记准确材料持有者。ContextLookup 可实现 `ContextMaterialGate`，在同库 Memory 变更头及当前来源门禁核验准确材料，不进行网络读取；缺少对端装配不能声称验证了远端当前许可。
- Context 可实现 `CompletionGatePreparer`，在本次原 completion 的准确检查已完成且已知效果关闭之后，先核 Task/控制/原主体元数据，再于 Tx 外为原成果取得当前来源依据。准备不能重建成果、续期或开始新行动；最终仍经原 `CompleteTx` 和 Claim Guard 全量裁决。缺少本次外部依据不能继承磁盘上的旧 positive。
- Execution 可实现 `ExecutionPreparation.PrepareDispatch`，在事务外按原 Operation/Command/IntentHash 冻结和出版准确输入，然后才重新核当前目标、控制及凭据并签发短窗口。准备不能创建 Attempt、调用模型/工具、改变目标效果或追加预算；临时不可用沿原派发 Job 等待，旧端口保持兼容。
- 同库 `LocalGate`；需要准确证据治理副本时，该 Gate 同时实现 `EvidenceRegistration`。检查和完整覆盖登记及 Result 持有者绑定均在原 Task 事务中完成。
- 原提交者凭据代次与角色冻结在 Task 中；Context 编译沿用该身份。Gate 可同时实现纯 Tx `SubjectGate.CheckSubjectTx`，在当前 Task 与每层祖先正门禁核验撤权。缺少该端口不能宣称验证了当前身份；旧记录缺少代次时新准入关闭，负控制和迟到账务仍保留。
- Gate 可实现 `CurrentTaskGate.CheckTaskCurrentTx`，复核远端父范围的准确当前控制和原 incoming allocation；有限签名准备在事务外完成。最终 input 消费与 steer 目标提交在实际字节读取或出版之后重核关闭门禁，不能以较早的 accepted 代替最终消费资格。
- Gate 可实现 `AdvanceGatePreparer`，仅为本次正向 advance 在原主体元数据与 Claim 核验后取得有限当前证明。工厂不执行该端口；终态、过期、暂停及账务收尾保留原处理，不借准备读取新正文或取得行动权。
- Gate 可实现 `DecisionGatePreparer.PrepareTaskDecision`，为本次原正向 dispatch_decision 取得当前父范围证明。原 birth 路由先锁 Task 根链与预算，核原提交者代次和冻结 Snapshot 的目标、控制、政策及派发身份；准备在 Tx 外执行，前后重核 Claim，之后仍执行原派发的完整当前门禁。终态、旧控制、已消费及待输入等分支不作新准备；端口不能刷新原 Decision、Command、预算或期限。此接口是受信宿主装配端口，不能由 Brain 提案代替。
- `ActionAuthorization.AuthorizeAction`。它在封存原 IntentHash 后运行，与整批 ActionConsumption、预留、意图共同提交；整批拒绝会回滚一次授权使用。
- `ControlProofPort`、`ClosureProofPort`，以及后者可实现的 `AllocationProofPort`。seal 只能本地签名并保存准确证明字节和出版意图，不得出站。缺少 seal 不得以 GoalRef 冒充控制或关闭证明。
- 配置协作接收方的本地 `CollaborationAdmission`。未配置协作时，创建方法在新增会话、额度或委派责任之前返回 unsupported；已存责任仍可读取、控制及恢复。

行动批次至多四项，必须独立且资源不冲突。当前完整目标覆盖、准确条件版本、观察期限、全部必要检查及同库证据 gate 共同裁决完成。空条件、旧控制、未知效果及未关闭子目标均不能完成。纯账务未结不阻止目标和效果关闭，迟到费用仍按原源累计差额归并。

同一目标及控制代次已有未消费的原 Decision 时，advance 在当前来源准备和 Context 出版之前结束这次唤醒；原提案消费负责后续推进。最终 PrepareDecision 短事务再次有界核对，新身份返回 `invalid_state/decision_pending`，原身份重放仍返回已保存意图。已确认回滚的 stale Snapshot 或 pending Decision 只重调度原 Job；提交未知沿原记录恢复。目标或控制改变后旧轮不阻止新轮准入，原效果观察、账务核对与独立 deadline 责任继续保留。

完成提案中的准确检查建议同事务变成去重的 CheckRequest 和 Job，并固定原成果、目标及控制版本的 completion intent。实际检查未完成时保留等待，不消费无进展额度或开启新 Decision；RecordCheck 唤醒原完成责任，只有当前完整门禁通过才保存 Result。建议本身不能替代观察。坏建议整批回滚后有限拒绝，目标或控制改变则废止旧完成意图。

受信负检查仍登记准确治理副本并保存原事实，完成资格核验只对可用 pass 执行。实际 fail 可以结束原 CheckRequest/Job，拒绝原完成意图并有限记一次无进展；不能因其不能通过完成门禁而丢弃负事实、反复执行原检查或生成 Result。

连续无进展达到固定上限时，advance 在 Context 编译和 Content 出版之前于短事务保存准确等待原因并完成原 Job，不能靠计时重试持续生成 Decision。实际有效新输入、可用成果或原 unknown 核清可恢复推进；迟到费用、相同已知效果的 revision 更新不能清零连续计数。新 Decision 和新计划步骤分别消费累计续行额度，整批准入不足则原子拒绝；重派原身份不重复计数。累计额度耗尽后保留最后已准入 Decision 或 completion 的原消费责任，核清后结束目标，效果与账务仍沿原身份核对。

同 owner 的 [collaboration adapter](../../../adapters/collaboration/README.md) 已接实际 Session 和内部 Task 转交。它冻结原主体与 Command，在未配置跨 owner 接收方时于准入前关闭入口。原转交只有消费方实际 applied 才记录完成；准确 rejected 保留原回执，不能以暂时读取失败伪造拒绝。

控制窗口最多五秒，正控制还截于 Task deadline。原 invoke 答复丢失时重发固定的原窗口、证明、时间及输入；新的窗口通过单独原控制窗口责任取得。负控制在 deadline 之后可以签发有限传播窗口，仍不授予行动入口。关闭视图包含完整有界本方子树、准确关系摘要和依据引用；超出已配置完整性界时明确保留缺口，不截断后宣称关闭。额度关闭先到时保留永久门禁，迟到创建不得重开。

惰性执行输入准备完成之前不签首个控制窗口。准备后取消或撤权的 Task 不能派发；已经固定的原窗口仍随原 invoke 重放，准备重入不能刷新时间、原命令或预留。Claim 在准备前及后续受保护事务重新核验，准备造成的领取失效不授予执行入口。

## 已运行证据

Task 测试使用持久 SQLite、真实 PostgreSQL、实际 Memory/ObjectStore、同库 Governance 和真实 ES256；报告、文本语义等非本切片负责的事实由准确、明确预批准夹具提供。它们证明 Task 的消费和原子性，不证明文本质量或供应商行为。

已覆盖原命令去重、完整目标冻结、语义 upsert 保留硬条件、空条件拒绝、旧控制提案拒绝、重复 Decision 原预留、最多四行动及整批回滚、历史终态不隐藏活动容量、答案 Schema 和消费、原 Session 丢回执恢复、旧 Delegation wait、额度关闭先到、原额度真实签名关闭与父预算迟到差额、控制来源签名及 deadline 后负控制、Result 先于出版、出版答复丢失沿原 Content 恢复、旧 Claim 不能写外部字节、真实治理缺陷附注，以及提交答复丢失后重开原库恢复相同回执。

护栏回归通过实际 Memory/ObjectStore 出版边界统计新增内容，验证达到无进展上限后多次 drain 不产生新 Decision、新文件或待计时重领 Job；真实 InputRequest 与答案消费能恢复连续计数，而累计续行上限仍保留。纯迟到费用测试使用字面原 Operation 夹具，只证明归并算法不能将账务变化误算为目标进展。

批量请求回归使用真实 PostgreSQL 两个并发事务反序读取两个 Task 的请求：逐个调用的旧实现实际触发 SQLSTATE 40P01；统一完整锁集合后两者均提交，准确原输入顺序和答案 Schema 保留。测试只以有界延迟放大实际行锁交错，未用包装 Tx 替代数据库。

原费用归并与新决策准入的真实 PostgreSQL 竞争也复现并消除 SQLSTATE 40P01；账务准确提交一次，过期的决策快照按原版本门禁拒绝。预批准观察夹具以 SQLite 权威时钟的毫秒精度向下声明时间，避免纳秒墙钟落在同毫秒权威时间之后；未放宽规则或 Task 的有限陈旧期限。

当前待消费 Decision 的[公开回归](../../../internal/task/pending_decision_test.go)在真实 SQLite 和 PostgreSQL 首先 RED：原 Operation 新事实唤醒同一 advance 后，编译次数由一增至二、预留由三笔增至四笔。修复后两库 GREEN（2.581s），五项两库矩阵及受影响恢复 race 实际 exit 0（138.977s），覆盖已知/未知原费用、消费后下一轮、迟到 final 差额、旧目标/控制 fencing、取消、零新正向准备和准备返回前另一 Decision 先提交；既有提交未知、原期限重开和护栏检查也通过。Content 使用真实文件介质，上游费用/效果是明确受信夹具，Brain 只表示原接纳等待，没有物理模型请求。准确源码、原身份及实际进程退出索引位于 `/workspace/harness-dev-environment/task-pending-decision-verification/`。此前完整 WASI Worker PG 的原 Task `41382c` 仍按原期限 failed；这项门禁证据不将该失败改记成功，也不替代后续整链验收。

执行准备回归以真实 SQLite 原 Service/Dispatcher/Job 边界复现实际 5.2 秒输入准备耗尽原五秒窗口，再验证准备完成后的首窗口。期间取消、真实 DevIdentity 撤权均阻止派发；实际 Memory/ObjectStore 出版后丢失准备回执，恢复得到同一 ContentRef 和预留；接纳 invoke 后丢回执并跨原窗口期限重放，准确原命令和窗口仍保留。该准备边界不提供真实模型或工具执行效果。

独立 deadline 回归在 SQLite、PostgreSQL 的公开提交、暂停、输入等待、原命令提交答复丢失及重开边界通过，选定双库 race 实际 exit 0（47.056s）。未知账务不被到期清零，迟到原累计费用按差额关闭。此前偏好测试保留的原 active Task 在修复后沿同一身份到期 failed，原 Goal、Submit applied 回执、已闭操作和无 Result 事实保持；该恢复不是新的偏好成功验收。

完成来源准备的 SQLite 正反例 race 实际 exit 0（18.906s），覆盖本次依据、准备期间取消、暂停 fencing 和无可选端口的原行为。它证明 Task 端口与原完成事务的交接，不能替代独立设备宿主的完整成果链验收。

原决策派发准备的公开 SQLite 回归先保留实际 `remote_parent_scope_required` 拒绝（0.632s），再验证原 Decision/Command 派发、准备期间独立公开取消、终态和旧控制零准备，以及无可选端口的原行为；选定 race 实际 exit 0（13.969s）。准备仍使用原提交者代次和角色，不能增加预算责任。此 Task 小端口夹具只证明原库与最终门禁交接，实际 HTTPS、独立 owner 和父范围签名由协作装配另行验收。源码、日志与原身份索引位于 `/workspace/harness-dev-environment/task-decision-preparation-verification/`。

Source 撤回后的原模型最低账务依据由 Brain 原账本与宿主的受限 accounting Content 端口提供，保持原 CallID、Decision、UseRef、累计金额和回执，不读取或携带已撤回的正文。开发宿主四条路径的最终双库 race 实际 exit 0（375.051s）：已知费用结清、旧 applied 依据重用、丢回复仍未知，以及停用当前模型配置后归并原 Use。原 USD0.00024 及未知预留分别保留；31min 后只能重用原 applied 出版回执，不能刷新原上传责任。固定源码、八个原 Scope/Call/Use 与日志摘要见 `/workspace/harness-dev-environment/model-minimum-invoice-final-1a47477-verification.json`。这不能由 Task 差额算法夹具代替，也不能替代 WASI 整链撤源验收。

工单 14 的四种 resolver/当前材料正反例两库 race 实际通过（SQLite 107.919s、PostgreSQL 108.683s）；有限普通偏好三报告最终 SQLite race 实际通过 623.820s。原偏好、更正、撤回分别影响真实文件的 bullet/plain/default 格式，九项真实操作、六项独立 verified 检查、三次原查询、原 Result/submit 回执重开均完整；退出后的只读原账务核对为 spent/reserved=0、accounting_open=false。原 Task CompletedAt 均早于各自五分钟 deadline。准确源码 477 文件摘要与原引用索引在 `/workspace/harness-dev-environment/context14-preference-final-1a47477-sqlite-race/context14-final-verification-v2.json`；此前 PostgreSQL normal 432.301s 与两次 SQLite 观察失败按各自实现记录，原失败不复活。本轮只增加测试观察者的有限等待，没有放宽 Task、命令、控制窗口或 Lookup 期限。

运行入口：`go test ./internal/task -count=1`、`go vet ./internal/task`、`go test -race ./internal/task -count=1`。PostgreSQL 测试只在 `HARNESS_TEST_POSTGRES_DSN` 配置时运行；未配置时明确 skip，不计为 PostgreSQL 通过。密码从运行环境取得，不进仓库或输出。

本轮开发闭环采用受限确定性规则、托管文件真实写入及独立读回；模型物理调用未启用，执行和许可费用明确为零。付费模型、工具和 Grant authority 的真实账户未配置，对应出站不启用。累计费用及退款算法测试使用字面金额夹具，不能作为真实供应商费用证据。

上段指工单 03 的首个零费用报告夹具。后续工单通过实际 loopback HTTP、设备文件、模拟 GUI、WASI 及其原账务取得的证据分别记录；它们不等于真实供应商账户、物理手机或生产配置已验证。工单 14 的有限普通偏好只在原用户允许的 plain/bullet 选项内改变报告格式，原标题、正文、路径和本人 SourceEvidence 不变，仍需实际文件及独立条件核验。

## 证据边界

生产 PostgreSQL 分片、多 owner 远程授权和关闭证明、真实供应商、设备驱动和规模需分别运行对应合同。Task 小端口与持久等待责任不等于这些对端已配置；它们缺失时不会捏造失败、效果或费用终态。Task 自身测试也不替代宿主的报告闭环、真实 Web 交互及全项目故障矩阵。
