# Task 实施记录

本记录对应 `internal/task` 与工单 03，记录实现和运行证据，不替代 [Orchestrator 合同](README.md)、[账务](../accounting/README.md)、[协作](../collaboration/README.md)或[存储](../data/storage.md)。

## 实现与装配

`task.New(Config, Ports)` 接收明确登记的政策、规则与答案 Schema，`Register` 同版登记闭合方法和 16 类工作。目标原文、完整 GoalDocument、条件及准确语义采纳、Task 控制、预留、累计费用、当前检查选择、不可变 Result 与独立出版责任保存在固定 Task owner 的原库。内部实现只依赖 `api`、`runtime` 和消费方小端口。

公开方法包含 Task 提交、控制、修订、输入、质量接受、预算调整、证据附加、费用通知、控制窗口与查询；额度 allocate/close/settle/read；账单调整提交；ChildHandle create/send/close/read/list/wait，以及准确 InputRequest read/list。`task.input` 的 accepted 表示准确答案已耐久排队，最终 applied 才表示原请求已消费。坏答案不消费请求，修正答案可以重新提交。

16 类工作为 advance、dispatch_decision、dispatch_operation、reconcile_operation、check、coverage、control、billing、publish_result、steer、delegation、allocation、child_prepare、child_transfer、input、adjustment，均以 `task.` 为前缀。外部读取、出版和发送在事务外；Claim 先于出站重核，资格和最终归并在有 Guard 的短事务中重新核验。

受信内部用例 `AdoptRequirements`、`StoreCoverage`、`RecordCheck`、`Complete`、`PrepareDecision`、`ConsumeProposal`、`ContextFacts`、`CheckDecisionTx`、`OperationIntentTx`、`Closure`、`RequestViewTx`、`RecordResultNoticeTx` 等供宿主装配。它们不构成新公开线方法，也不允许 Brain 自报已消费 Grant、原 Operation 身份或已发生效果。

宿主必须显式声明共享数据库与 Tx participants，并提供：

- Content 字节读取及固定出版；Context 编译；Brain、Execution、外部取证及协作端口。
- 同库 `LocalGate`；需要准确证据治理副本时，该 Gate 同时实现 `EvidenceRegistration`。检查和完整覆盖登记及 Result 持有者绑定均在原 Task 事务中完成。
- 原提交者凭据代次与角色冻结在 Task 中；Context 编译沿用该身份。Gate 可同时实现纯 Tx `SubjectGate.CheckSubjectTx`，在当前 Task 与每层祖先正门禁核验撤权。缺少该端口不能宣称验证了当前身份；旧记录缺少代次时新准入关闭，负控制和迟到账务仍保留。
- `ActionAuthorization.AuthorizeAction`。它在封存原 IntentHash 后运行，与整批 ActionConsumption、预留、意图共同提交；整批拒绝会回滚一次授权使用。
- `ControlProofPort`、`ClosureProofPort`，以及后者可实现的 `AllocationProofPort`。seal 只能本地签名并保存准确证明字节和出版意图，不得出站。缺少 seal 不得以 GoalRef 冒充控制或关闭证明。
- 配置协作接收方的本地 `CollaborationAdmission`。未配置协作时，创建方法在新增会话、额度或委派责任之前返回 unsupported；已存责任仍可读取、控制及恢复。

行动批次至多四项，必须独立且资源不冲突。当前完整目标覆盖、准确条件版本、观察期限、全部必要检查及同库证据 gate 共同裁决完成。空条件、旧控制、未知效果及未关闭子目标均不能完成。纯账务未结不阻止目标和效果关闭，迟到费用仍按原源累计差额归并。

完成提案中的准确检查建议同事务变成去重的 CheckRequest 和 Job，并固定原成果、目标及控制版本的 completion intent。实际检查未完成时保留等待，不消费无进展额度或开启新 Decision；RecordCheck 唤醒原完成责任，只有当前完整门禁通过才保存 Result。建议本身不能替代观察。坏建议整批回滚后有限拒绝，目标或控制改变则废止旧完成意图。

控制窗口最多五秒，正控制还截于 Task deadline。原 invoke 答复丢失时重发固定的原窗口、证明、时间及输入；新的窗口通过单独原控制窗口责任取得。负控制在 deadline 之后可以签发有限传播窗口，仍不授予行动入口。关闭视图包含完整有界本方子树、准确关系摘要和依据引用；超出已配置完整性界时明确保留缺口，不截断后宣称关闭。额度关闭先到时保留永久门禁，迟到创建不得重开。

## 已运行证据

Task 测试使用持久 SQLite、真实 PostgreSQL、实际 Memory/ObjectStore、同库 Governance 和真实 ES256；报告、文本语义等非本切片负责的事实由准确、明确预批准夹具提供。它们证明 Task 的消费和原子性，不证明文本质量或供应商行为。

已覆盖原命令去重、完整目标冻结、语义 upsert 保留硬条件、空条件拒绝、旧控制提案拒绝、重复 Decision 原预留、最多四行动及整批回滚、历史终态不隐藏活动容量、答案 Schema 和消费、原 Session 丢回执恢复、旧 Delegation wait、额度关闭先到、原额度真实签名关闭与父预算迟到差额、控制来源签名及 deadline 后负控制、Result 先于出版、出版答复丢失沿原 Content 恢复、旧 Claim 不能写外部字节、真实治理缺陷附注，以及提交答复丢失后重开原库恢复相同回执。

运行入口：`go test ./internal/task -count=1`、`go vet ./internal/task`、`go test -race ./internal/task -count=1`。PostgreSQL 测试只在 `HARNESS_TEST_POSTGRES_DSN` 配置时运行；未配置时明确 skip，不计为 PostgreSQL 通过。密码从运行环境取得，不进仓库或输出。

本轮开发闭环采用受限确定性规则、托管文件真实写入及独立读回；模型物理调用未启用，执行和许可费用明确为零。付费模型、工具和 Grant authority 的真实账户未配置，对应出站不启用。累计费用及退款算法测试使用字面金额夹具，不能作为真实供应商费用证据。

## 证据边界

生产 PostgreSQL 分片、多 owner 远程授权和关闭证明、真实供应商、设备驱动和规模需分别运行对应合同。Task 小端口与持久等待责任不等于这些对端已配置；它们缺失时不会捏造失败、效果或费用终态。Task 自身测试也不替代宿主的报告闭环、真实 Web 交互及全项目故障矩阵。
