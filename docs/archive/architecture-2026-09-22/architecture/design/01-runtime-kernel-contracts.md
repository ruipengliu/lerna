# 01A 任务运行内核：接口与类型

本篇固定本地 Go 运行接口、调用合同和关键类型。可解析、可编译的声明在 [runtime-kernel-contracts.go](assets/runtime-kernel-contracts.go)，该文件只包含规格声明及错误格式化，不是运行实现、生成 SDK 或已发布包。实际包路径采用 `runtime/`，示例包名 `runtimespec` 用于隔离文档资产。完整过程见[主设计](01-runtime-kernel.md)。

<a id="types"></a>
## 1. 值、快照与有效输入

| 类型 | 精确定义与校验 |
| --- | --- |
| TaskRef／OperationRef | 分别为 `(namespace, task_key)`、`(namespace, operation_id)`。非空 UTF-8 字符串，按原字节比较；namespace 来自认证上下文，不做大小写或 Unicode 隐式转换。 |
| WorkRef／决策序号 | 工作键与正整数决策序号仅在所属任务内唯一；不用新的全局业务身份。工作键由内核分配、保存后不复用。 |
| Version／Epoch | Go `int64`、SQLite INTEGER；持久版本从 1 开始，增加前检查溢出。版本 0 只表示尚未创建，不能回绕。预期版本为 nil 仅在明确合同中表达“不存在”。 |
| 时间 | 接口为 `time.Time`，持久化为 UTC Unix 毫秒的 int64；入库舍弃亚毫秒精度。外部有效期向保守方向舍入，截止点采用 `now >= deadline` 已到期。零值不得代表无限授权。 |
| 金额／用量 | `int64` 基础单位；维度登记单位和上下限，例如 token、次数、字节、微货币单位。禁止浮点累计，加减检查溢出，未知用量不填 0。 |
| Document | `Schema` 是本地登记的准确版本；`Bytes` 是接纳时固定的规范化 UTF-8 内容，`Digest` 为 SHA-256。缺失、null、空值按该 Schema 分开校验。 |
| Access | 受信入口产生的主体、代理关系和证明。外部 JSON 不得构造或覆盖它；Store 不以结构体可构造性作为认证。 |
| TaskView | 同一任务一致快照，包含状态及规则需要的操作、依赖、预算等记录。大小受限；若分页，必须沿固定快照令牌且有界生命周期。 |

`Document` 是跨子系统已登记载荷的容器，不允许“任意 JSON 变更”。运行内核新增记录的字段由[物理规格](01-runtime-kernel-storage.md)及本篇命令表定义；其他模块的完整类型仍由其契约维护。Schema 未登记、未知必需字段、错误版本、过大载荷在进入规则前拒绝。

TaskView 的集合使用对应完整行 Schema，包含表中所有业务列和 document 扩展字段，不只返回 document 那一列；因此纯规则能够取得工作状态、领取、操作阶段、决策基线及全部预算数值。内部索引、SQLite rowid 和连接句柄不进入快照。返回后的切片／Bytes 视为不可变，Adapter 与调用者不共享可写缓冲区。

内核摘要格式 `runtime-canonical-v1`：按 Schema 解码并校验后，生成仅含固定字段的 JSON；对象键按 UTF-8 字节序排序，无空白；字符串以 UTF-8 输出，仅转义引号、反斜线及控制字符（后者使用小写 `\u00xx`），不正规化 Unicode；整数十进制无前导零，不使用 JSON 浮点。集合先按各自语义键排序，有序数组保持顺序；缺省字段在 Schema 规定后显式展开，optional 缺失与 null 不合并。动态载荷若允许小数，使用其已登记的规范化内容摘要及准确 Schema 引用嵌入，不擅自转换成整数。摘要覆盖 Schema、命令种类、目标、语义输入、身份关联及固定变更前提；不含 trace、网络超时或临时连接 ID。摘要相同仍须核对保存的规范化内容，不只比较调用方自报摘要。

<a id="ports"></a>
## 2. 对外接口及调用顺序

| 接口 | 允许调用者 | 合同 |
| --- | --- | --- |
| Core.Handle | 受信 TaskService Adapter、获准输入／报告入口。 | 接纳版本化业务命令，返回持久业务回执。调用 context 取消只结束等待，不撤销已接纳命令。 |
| Core.Get／LookupOperation | 经授权的应用或恢复方。 | 当前任务可见快照或原操作结果；查不到与永久未发生分开。 |
| RunStore.Load | Core、当前获准 Worker。 | 一致快照；拒绝越 namespace 访问，不通过读暴露内部秘密。 |
| RunStore.Commit | Core 生成的受约束 Change。 | 执行[统一提交算法](01-runtime-kernel.md#commit)，结果明确区分已提交、拒绝和未知。 |
| RunStore.LookupCommit | 原调用者／当前获准恢复者。 | 查所属任务原 change_id；旧 Worker 可失去推进权但获准查询仍可返回原回执。 |
| RunStore.ListRecoverable | 恢复协调入口。 | 稳定分页枚举未决责任，任务终态但仍有可靠交接／用量义务时也在范围内。 |
| WorkStore.ListNamespaces | 宿主授权的调度入口。 | 在可信 SchedulerScope 分配范围内分页枚举仍有活跃工作责任的 namespace；不向普通用户提供跨用户列表。 |
| WorkStore.ScanDue | 调度器。 | 返回有界候选及游标，不取得租约，不承诺候选之后仍合格。 |
| WorkStore.Claim | Worker。 | 在事务内重新检查；DECIDE 同时预留预算、分配决策序号并返回提交后快照。 |
| WorkStore.Renew | 原有效 Worker。 | 检查同一 Worker、claim_epoch、boot_epoch、Owner 和未到期租约；不得复活旧租约。 |
| SchedulerPolicy.Select | 调度协调器。 | 从提供的候选中选择至多 limit 个唯一 WorkRef，不创造新候选或持久状态。 |
| RuntimeMailbox.Persist | 经过认证及协议校验的收件入口。 | 同事务保存 Inbox 与 APPLY_INBOX 工作；重复同内容返回原持久接收结果，不能冒充业务接纳。 |
| RuntimeMailbox.ClaimedInbox | 当前 APPLY_INBOX Worker。 | 返回该有效领取覆盖的原消息、保存的来源／修订和固定正文；Core 按当前权限核验，原 Access 只作来源证据，不能作为今天的调用资格。 |
| RuntimeMailbox.ClaimedOutbox | 当前派发或可靠交接 Worker。 | 仅读取其有效领取覆盖的 READY 消息；HELD 消息必须先经过派发准备提交。 |

RunStore、WorkStore、RuntimeMailbox 的构造器从同一 `RuntimePersistence` 实例暴露。组装检查它们的持久化单元标识一致；不接受三个互不相关的数据库实例伪装成共同事务。接口不开放远程任意数据库写入，外部开发者实现 Adapter 也须通过同一合同测试。

Claim／Renew 的 change_id 和固定请求内容同样进入提交回执。领取结果未知时，先查询原 change_id，不能凭收到过候选开始调用。查到原租约后还要检查现在是否有效；原回执不会续期。普通工作完成、取消领取与下一轮责任都通过 Commit，没有独立 Ack 接口制造责任空窗。

ClaimRequest.Mode 必填：EXECUTE 领取新一轮正常工作；RECOVER 只取得旧工作的核对资格。失效 LEASED 只能按 RECOVER 领取，不新建决策或预留生成预算；原 ACTIVE 决策可存在，先在恢复事务中退休并结束旧 Work，再登记新的 DECIDE。Lease 固定该模式，RECOVER 不能直接作为 ADMIT 或 PREPARE_DISPATCH 的执行凭据。

ClaimRequest.Proofs 携带事务外取得的当前工作适用证明；Claim 检查其绑定、有限有效期与本地修订，不在事务中查询远端。所需证明缺失时明确等待，不能因为 Work 已准入就默认权限永久有效。RuntimeMailbox 只接收已存在且由本权威处理的任务消息；未创建任务的 Submit 由任务接纳入口负责，未知任务明确拒绝或回交原持久路由，不自动建任务。Owner 已封存的源端将新消息交 ConnectionAdapter 的可靠中继，不再创建本地 APPLY_INBOX 责任。

Store 的 `error` 表示调用层错误；若 Commit、Claim 或 Renew 已经发起事务且没有明确 CommitResult，调用者一律按 Unknown 处理。只有可证明事务未开始或已确认回滚的错误，才可作为明确拒绝。Mailbox 持久接收回包丢失时，原消息原样重投，由 Inbox 唯一键去重。

<a id="commands"></a>
## 3. 命令、变更及载荷登记

Core.Command 的 Kind 是封闭登记集合，Body 使用对应 `runtime.<kind>.v1` Schema。以下字段加上 Command 公共任务／操作／预期版本构成必需输入；类型化外部引用仍按其所属模块定义。

| Kind | Body 字段与入口前提 |
| --- | --- |
| SUBMIT | `goal`、`constraints`、`acceptance`、`delivery`、`budget_limits`、`deadline_ms`；可选委派关系；必须不存在原任务，操作接纳窗口有效。 |
| UPDATE | `mode=APPEND_FACT|REVISE_GOAL`、`content`、`source_refs`；预期版本必填，语义要求变更使用 REVISE_GOAL。不能把自然语言要求变化伪装成追加事实绕过冻结。 |
| PROVIDE_INPUT | `interaction_key`、`input`、`source_refs`；等待项仍有效，输入 Schema 相符；新提交与原操作重试分开。 |
| ADJUST_LIMITS | `dimensions[]=(name,new_limit)`、可选 `new_deadline_ms`、`reason`；有专项调整权限、预期版本必填；处置额度补充不得夹带目标维度或隐式延长。 |
| PAUSE | `reason`、`scope=SELF|SUBTREE`（默认 SUBTREE）、原控制来源；预期版本必填。 |
| RESUME | `target_pause_operation_ref`；沿原范围，预期版本必填。 |
| CANCEL | `reason`；沿既定取消入口，不额外要求用户提供任务版本；Core 仍按当前版本原子提交。 |
| APPLY_REPORT | `inbox_message_id`；从持久消息读取内容和受信来源，不能接受调用方临时替换报告。 |

内部 Worker 不经公共 Core.Handle 发出任意变更；使用绑定 Lease 的内部推进函数。它们产生 CREATE、INPUT、CONTROL、LIMITS、CLAIM、RENEW、ADMIT、RETIRE_DECISION、PREPARE_DISPATCH、APPLY_REPORT、ADVANCE_WORK、TRANSFER、CLOSE_CHANGE、PRUNE 这组 `Change.Kind`。其中 PRUNE 受保留前提约束，不接受通用删行指令。

`Mutation` 的封闭类别为 TASK、OPERATION、DEPENDENCY、WORK、DECISION、BUDGET、RESERVATION、CONTROL_SOURCE、WAIT、INBOX_APPLIED、OUTBOX、HANDOFF、RECEIPT、CHANGE_CLOSED、ADMISSION_WINDOW。Value 按类别映射相应记录 Schema；Key 是所属任务内键或规范化复合键，不是列名。Adapter 拒绝 Change.Kind 不允许的类别、跨任务修改、只结束工作但丢失必要后继、无依据释放预留及任意终态覆盖。

每类变更的 `Result` 由 Core 规则产生并与回执共同保存。Adapter 不能执行不可信回调；规则没有自定义 SQL。控制、预算、Owner 和终态相关变更必须来自相应内部函数，并检查完整前提。Store 是受信实现，不把这些内部结构作为扩展或模型能够直接构造的公共端口。

<a id="reviews"></a>
## 4. 提案复核扩展与失联决策

`ProposalReview` 固定 Task、DecisionSequence、BaseVersion、ReviewedOperations 和现有互斥主类型提案。KEEP／DROP 的语义唯一维护在[目标更新过程](01-runtime-kernel.md#updates)。适配器必须明确支持该扩展；旧 Brain 不识别时不能丢弃复核字段或自动解除冻结，任务保留兼容性等待。

复核只允许 ACTION 或 DELEGATE 主类型。只复核时 `new_requests=[]` 合法且 `reviewed_operations` 非空；两者都空则拒绝。WAIT／RESULT 不附复核清单；有冻结工作时，RESULT 不能绕过处置条件确认完整成功。新请求局部键与旧 operation_id 是两种引用；依赖解析必须明确是哪一种，不靠字符串格式猜测。

RETIRE_DECISION 变更必须携带原决策序号、待核对的原准入 change_id 集合／提交范围、预期任务版本和当前恢复领取。在同事务内检查回执和旧资格，封闭旧序号继续准入的能力；不是一个可以任意“忽略未知”的 Force 接口。原变更已提交则返回原准入结果，不能继续应用退休计划。具体串行竞争见[失联恢复](01-runtime-kernel.md#decision)。

<a id="errors"></a>
## 5. 错误、重试与未知

| Fault.Code／结果 | 调用方必须如何处理 |
| --- | --- |
| INVALID_ARGUMENT／SCHEMA_UNSUPPORTED | 修正输入或补齐兼容实现；不得截断必需字段后重试。 |
| ACCESS_DENIED／PROOF_EXPIRED | 取得当前获准证明；不能降级身份或放宽数据位置。 |
| VERSION_CONFLICT | 原 change 已明确未提交时，重新读取并计算新变更；旧提案不能换版本重投。 |
| OWNER_FENCED／WORK_FENCED／BOOT_FENCED／DECISION_RETIRED | 停止直接推进；迟到事实通过受控报告交当前权威核对。 |
| CONTROL_BLOCKED／DEPENDENCY_BLOCKED／GOAL_FROZEN | 保存或保留等待责任，不忙循环，不释放未知占用。 |
| BUDGET_EXHAUSTED／DEADLINE_EXCEEDED | 停止新增目标工作，进入有界处置或申请人工介入。 |
| IDENTITY_CONFLICT | 同操作、消息或 change_id 对应不同内容；拒绝并诊断，不能分配新 ID 掩盖冲突。 |
| BACKPRESSURE／BUSY | 仅在明确未提交时按有界退避重试，RetryAfter 是建议，不延长业务期限。 |
| STORAGE_UNAVAILABLE／COMMIT_UNKNOWN | 查原 change_id；未取得耐久确认不允许后续派发。 |
| RECOVERY_BOUNDARY | 原明细已不可核实或超出接纳窗口；不能重新首次受理旧请求。 |

UNKNOWN 是持久结果确定性，不是“可随意重试”的普通错误。错误诊断只含当前主体可见引用；日志不得输出秘密、完整受控正文或模型内部思维过程。结果披露权限变化时，即使原操作成功也可以只返回受限状态。

## 6. 版本、兼容和实现边界

本地接口与 DDL 版本固定为设计 v1。新增枚举或改变摘要规则需提高格式版本并提供迁移，不能让旧实现忽略必需字段继续写。原任务、原操作和未知占用在升级后保留；不兼容的旧 Brain／Driver 绑定进入恢复限制，不能默默重绑新版。

本篇固定本地 Go 签名和关键运行类型，不发布新的公共 RPC、Protobuf 字段编号或跨语言 SDK。TaskService、Brain、Execution 等网络与载荷完整 Schema 继续在[接口目录实施边界](../reference/02-api-and-message-catalog.md#implementation-boundary)跟踪。SQLite 驱动、精确运行版本及生产数值 profile 在实现锁定与验证时确定，不伪装为本文已运行的依赖。
