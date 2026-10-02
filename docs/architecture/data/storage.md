# 物理存储与一致性

本文把[整体数据设计](README.md)中的逻辑记录落到持久介质、事务、索引与恢复边界。字段含义见[字段字典](field-reference.md)，业务状态转换仍由各模块裁决。

这是目标 DDL 与 repository 合同，不是已执行的建表脚本，也不代表数据库、迁移或容灾已经实现。下文表名/记录组可以按访问模式拆合；拆合不得改变唯一写入负责方、原身份、原子提交集合或保留责任。不增加中央事件库、统一调度服务或第二套消息权威。

## 1 存储介质与权威分工

生产采用独立 WSS 网关、任务应用、分类工作池与执行宿主。PostgreSQL 分片保存业务事实和持久责任，对象存储保存不可变正文；端侧 owner 可用独立 SQLite 适配。一个逻辑 owner 同时只有一个可写存储位置；多个进程共享该权威，不各自保留可写真相。完整部署前提见[生产运行](../production/README.md)。

| 介质 | 保存什么 | 不能据此推导什么 |
| --- | --- | --- |
| 原 owner 的 PostgreSQL | 业务记录、版本、准确关联、门禁、命令决定、Job、出入站交接记录及最小墓碑 | 不自动保证外部目标或对象字节已耐久 |
| 原端侧 owner 的 SQLite | 本端拥有的 Task/Brain/Executor/Memory 等账本；设备 TaskGate、资源代次、Attempt、有限离线使用及待交回 Reply | 不是云端同名 owner 的可写副本；本端事务不能提交云端事实 |
| 对象存储或受控本地内容介质 | 准确 Content 版本字节、冻结 manifest、模型产物、报告、截图、检查点及获准安装制品 | 有字节不等于已发布、仍有使用权或已被用户看见 |
| 派生索引/物化投影 | 当前 Task 视图、Memory 检索、来源反向索引、聚合统计等 | 可以重建不等于可以在缺失时宣称全集为空 |
| 有界内存与客户端缓存 | socket/请求等待、可丢唤醒、已授权显示缓存、短期查询页 | 不保存唯一命令决定、费用来源或未结责任 |
| 浏览器 IndexedDB | 首次发送前的原 Command、固定 owner、Submission 与未决投递恢复记录 | 不是 Task 终态或授权权威；本地全部丢失不能按相似目标重新提交 |

SQLite 使用独立事务与迁移适配、WAL/FULL/foreign_keys 和单写队列；不照搬 PostgreSQL 的行锁、SKIP LOCKED 或多写者假设。云端单进程开发装配与本机多进程试验都不能替代跨可用区存储验收。

## 2 全模块落位

下表中的“PG”均指该 owner 被装配到的原 PostgreSQL 分片，不表示每行都需新建数据库或微服务。开启端侧部署的模块可以采用本端 SQLite；权威归属从创建时固定，不能因断网改写。

| 模块/记录组 | 唯一写入负责方与默认介质 | 正文、投影及本地特例 |
| --- | --- | --- |
| Session、Message、Branch、Submission、投递状态、会话任务关联 | Interaction owner 的 PG | Message 正文引用 Content；浏览器保存原提交副本，分支只复制历史引用，不复制 Task/费用/许可 |
| InputRequest、输入一次消费、成果验收 | 真正消费输入的业务 owner 的原库 | Interaction 保存转交，不裁决消费；问题、答案、预览引用 Content |
| Surface、Presentation、应用事件映射 | 应用 owner 的原库；业务请求仍归消费方 | 准确界面快照为 Content；本端显示 generation、窗口句柄和缓存不成为确认事实 |
| Schedule、Occurrence、活动槽与触发 Job | Interaction owner 的同一本地库 | 冻结模板和历史截止引用 Content/版本记录；Occurrence 保存原 Task 创建命令和映射 |
| Task、目标版本、输入关联、条件候选/采纳、Requirement、GoalCoverage、当前检查选择、Result、控制与续行计数 | 固定 Orchestrator 的 PG | 原输入、目标正文、覆盖报告与成果引用 Content；当前行与不可变历史分别保留，Result 由 Orchestrator 裁决 |
| Snapshot、DecisionDispatchIntent、DecisionConsumption、Plan/步骤准入、OperationIntent、TaskGate 传播目标、未结关系 | Orchestrator 原分片 | Snapshot/计划正文可按准确 Content 版本保存；完整关系索引、来源消费键和派发责任必须可事务核验 |
| DecisionRecord、ModelCall、Proposal、发布 local_id 映射、模型用量 | Brain owner 的原库 | 模型正文、封存编码和提案引用 Content；发布身份与费用记录不依赖临时内存、流片段或模型 SDK 缓存 |
| Operation、Attempt、Effect、TaskGate、资源租约/观察、文件 journal | Executor/资源 owner 的原库；端侧使用 SQLite 和原设备/文件介质 | 截图、读回、结果和证据引用 Content；受控文件与 journal 的刷盘/替换保证单独验收，不能只看 PG |
| Environment、cell 关联、hostcall 意图/映射、generation、停止/清理残留 | Executor owner 原库 | 程序、数据输出、检查点为 Content；运行中进程和变量内存不能成为唯一外部调用账本 |
| Content 元数据、准确版本、来源边、holder、副本控制、upload/mirror ticket、清理责任 | 原 Content owner 的原库 | 不可变字节在对象存储或受控本地介质；各 holder 另保存本地持有/停止/清理事实，不能修改源控制 |
| MemoryRecord、ExtractionCandidate、提取进度、change_head、query/view 状态 | Memory owner 的原库 | 记忆正文引用 Content；词法/向量/切片投影单独带版本和水位，检索结果不是权威记录 |
| Grant、UseReceipt、UseSettlement、Confirmation、PolicyAcceptance、离线 GrantLease | 各原业务/授权 owner 的原库；默认本地 Grant 与 Task 同分片 | Confirmation 归实际业务消费方；离线 lease 使用账本归固定 endpoint/instance，云端仅持原分配和结算投影 |
| BudgetBalance、Reservation、计费源绑定、UsageSnapshot、BillingAdjustment、Allocation/IncomingAllocation/Closure | Task 预算归 Orchestrator；费用原事实归计费 owner；父分配与接收账本各归本方 | 原账单/关闭证明引用 Content；同一来源的汇总展示不产生第二次支出 |
| Delegation、ChildHandle、amendment、输入/控制转交、DelegationClosure | 父方 owner 保存交接与聚合；子 Task 仍在子 owner 原库 | 内部子与父同分片时可声明共事务；外部委派必须保存两方记录，不以父投影覆盖子事实 |
| Capability、Binding、InstallLock、Skill/Agent 配置、Activation、InstanceReadiness、ReleaseApproval/ApprovalUse、holder 与迁移进度 | 原扩展/发布 owner 的原库；实例宿主持本次就绪事实 | 准确制品和声明正文引用不可变内容；安装目录和内存 handler 不是批准或持有关系权威 |
| ConditionCheck 原判断、当前适用性、EvidenceGate、Defect、EligibilityReceipt、导入水位/notice | 检查消费方及原验证器治理 owner 分别保存本方事实 | 本地门禁与 Task 完成同事务；远端资格是有时限凭据，不是跨库即时快照 |
| EvaluationPlan/Run、SampleRun/Attempt、ImprovementPolicy、Exposure、报告与取消责任 | 原评测 owner 的 PG | 冻结样本 manifest/报告为 Content；运行环境和目标效果仍归对应 Executor，实验日志不能替代目标真值 |
| Command、Receipt、Job、Claim、outbox/inbox、Delivery、Reply/Ack | 每个业务 owner 在自己的本地库保存 | JobStore 是共同逻辑契约，不是集中表/服务；原 Reply 耐久交回后才结束投递责任 |
| 身份/配对/凭据代次、来源目录、授权 authority 集合、连接额度、逻辑放置 | 受信身份/宿主原负责方的持久库或公司平台权威 | socket/进程地址可以缓存；来源完整性、配对和撤权事实不可只在网关内存中 |
| EndpointChannel 当前绑定与代次、订阅/查询游标 | 当前绑定归原逻辑服务，连接额度归身份权威；网关保存可丢连接状态 | 跨 owner 聚合游标为有界恢复状态，无全局原子快照；缓存失效只能重读原权威 |

正文与领域对象分开：Content owner 只负责准确字节及其治理，不能因保存了一份 Result JSON 就取得 Task 完成裁决权。数据字典必须标清“完整持久记录”“公开读取投影”“不可变正文”三种表示，不能拿一个缩略响应代替完整表设计。

## 3 本地事务具体包含哪些记录

### 3.1 默认共同裁决范围

| 业务提交 | 必须共同提交的最小集合 |
| --- | --- |
| Task 首次接纳 | 原命令判重与决定、Task/初始目标及输入绑定、预算关联、首 Job；内容须已按准确引用发布 |
| 条件/目标实质修订 | 新目标与条件版本、采纳/输入消费记录、旧提案失效依据、goal/control/task 修订、重核和重新决策责任 |
| 固定下一 Decision | Snapshot 及准确引用、DispatchIntent、原 Brain 命令、计数/预留、dispatch Job；Brain 的接纳是另一事务 |
| 行动准入 | 当前 Task/祖先门禁、唯一来源消费、不可变 OperationIntent、原执行命令、reservation、未结关系及 Job |
| 暂停/取消/终态 | Task 决定、control_revision、全部已绑定目标的传播责任；同域活动子树按有界锁集合处理 |
| Task 完成 | 当前目标覆盖/必要检查、完整关系与证据 gate 检查、选择依据、固定 Result、终态/控制和收尾 Job |
| 费用归并 | 原来源修订/摘要、累计差额、余额/预留、事故或更正记录与后续 Job；退款另存原 adjustment 去重身份 |
| 原输入/确认消费 | 请求版本与 approved/pending 等合法前态、一次消费身份、实际业务改变、原回执与后续责任 |
| Executor 接纳/开始 | 原 Operation/取消墓碑、阶段、Attempt/使用依据及效果核对责任；真实发送在确认提交之后 |
| Memory 更新 | 当前头/新版本、来源关联、change_head/change、原回执、索引与影响处理 Job |
| Schedule 到期 | 原规则版本与门禁、唯一 Occurrence、活动槽、原发送责任和 next_due_at |
| 扩展激活/评测门禁 | 原代次或曝光/缺陷门禁、当前实例/正式占用决定、原回执和全部必须继续的责任 |

默认 Task、条件、预算、控制、本地 Grant/Confirmation 和本地证据 gate 共用 Orchestrator 分片。共处一个主机、一个进程或同一 PG 集群都不足以推出共事务；宿主必须显式声明同一数据库、同一受信租户范围与同一 Tx participants。

内部子 Task/分配在同一原分片时共同创建；跨 Orchestrator 的 Task、Brain、Executor、Memory、Content、授权或证据治理均各自提交。estimate 准入仅在策略接受与相关 Grant 可同事务核验时开放；跨域 allocation 与离线仍为 strict。零陈旧证据要求同一权威事务，不得用“先远程检查、后本地提交”冒充。

### 3.2 锁序与受保护提交

共同锁序为：原命令键 → 根到叶 Task（同层按 ID）→ 各计量单位预算 → 领域门禁/对象 → Job。证据 gate 在条件行之前，Memory 变更头在内容门禁之前；隐式外键锁也要纳入测试。发现需要补锁更早层级时回滚并重组锁集合，不临时逆序追加。

Job 行保存当前 work_revision；Claim 固定领取时的 observed_work_revision、holder_id、lease_epoch 和已确认期限。两者是不同值，续租不得把观察值改成当前值。领域提交同时核验领取与版本：失去领取则受保护事务回滚；仍有领取但新责任已到达，可保存合法旧事实，但不能让旧 done/退避覆盖新责任。

数据库只有限重试确认未提交且没有外部行为的闭包。CommitUnknown 时查询原命令/原阶段，不发送、不释放未知预留、不伪报 rejected。真实目标调用、模型调用、对象存储写入和网络传递均在数据库事务之外。细节见[持久工作](../runtime/README.md)。

## 4 类型、主键、索引与约束

### 4.1 共同列规则

- 每条持久记录绑定 tenant_id 与逻辑 owner。认证上下文决定租户，正文/引用里的租户仅供核对；同库关联键包含租户，避免只按裸 ID 连接。跨域 ObjectRef 明确携带 tenant_id、owner_id、object_id 与 revision，不能成为跨租户裸指针，也不能用载荷中的租户选择认证数据域。
- 类型前缀 ID 保存为受约束文本；修订/计数用整数并限制在 JSON 安全整数范围，业务修订从 1 开始。时间保存能无损往返 UTC RFC3339 的类型。期限在裁决处使用可信当前时间，不用事务开始时间延长窗口。
- 金额按 unit 保存精确十进制；PG 使用精确 NUMERIC，SQLite 使用受控十进制字符串或等价精确适配，禁止浮点相加。`spent + reserved <= limit` 是准入检查，不是拒绝真实迟到超支的永久 CHECK。
- 当前头与不可变版本分开。可查询历史记录保留准确 revision、digest、来源命令及必要关联；旧修订不覆盖新头，同修订异摘要必须报冲突。普通权限收紧或证据失效只改变当前适用/控制记录，不重写原事实。
- 身份、外键、门禁、排序与索引字段显式列化。受版本 Schema 约束的有界正文可以用 JSONB/Content 保存，不能用任意 JSON 逃逸业务字段。严格 JSON 原始字节检查先于 JSONB/普通解码；原请求摘要所需规范字节或无损恢复依据单独保留。

### 4.2 必须保持的唯一性与访问索引

以下键均在相应 owner 的受信 tenant 范围内解释；跨 owner 共享物理表时把 owner_id 加入所有键。

| 对象/访问 | 目标唯一键或索引合同 |
| --- | --- |
| 原命令 | `(tenant_id, logical_service_id, command_id)` 唯一，绑定原认证主体、method/target/payload/CAS/期限与摘要；活记录与墓碑合起来只允许一个原决定 |
| 领域对象与历史 | 当前对象身份唯一；历史 `(tenant_id, owner_id, object_id, revision)` 唯一；准确内容为 `(tenant_id, content_owner, content_id, version)`，同键不能换 hash/长度/媒体类型 |
| Task 当前列表 | `(tenant_id, orchestrator_id, task_id)` 主键；用户/权限范围内按 `(created_at, orchestrator_id, task_id)` 稳定翻页；状态/等待热索引不承载完整历史 |
| 目标/条件/核验 | 目标版本按 `(task_id, goal_revision)`；条件定义按 `(task_id, requirement_id, revision)`；目标快照的每个 requirement_id 只选一个准确版本；检查按 check_id 保原记录；当前选择按 Task/目标/条件定位并绑定准确成果与规则，不能“查任意历史 pass” |
| 决策与提案消费 | decision_id 唯一；`(task_id, decision_id)` 唯一消费；每 Decision 至多一个物理 ModelCall；发布 `(decision_id, local_id)` 唯一映射到原 Content/upload/命令 |
| 计划与 hostcall | `(task_id, plan_id, plan_revision, step_id)` 唯一准入；`(parent_operation_id, call_position)` 唯一 hostcall，并绑定 input_digest；不同输入不能用同键 |
| Operation/Attempt | Executor 范围内 operation_id 唯一，取消墓碑也占同一身份；Attempt 保原 Operation 与序号/身份；目标幂等键额外绑定准确目标、作用域及保留期限 |
| 完整关系集合 | Task→Operation/Delegation/控制目标、Environment→活动操作、Content→holder、InstallLock→holder、Run→Sample/环境分别保唯一边、集合修订/计数及完整性标记；新增边与所属关闭门禁串行 |
| Job/Claim | `(tenant_id, owner_id, kind, responsibility_key)` 为唯一责任槽；job_id 不复用。活动扫描索引含 scope/kind/state/due_at/job_id；done 历史退出热点。领取依 holder/epoch/原观察值核验 |
| 应用输入/请求 | submission_id 唯一固定原投递；Session 内 message seq 唯一且 head 用 CAS；请求消费按原 request_id/revision 唯一，不能由两个新 Command 消费同一个请求 |
| 触发 | `(schedule_id, rule_revision, planned_at_utc, fold)` 唯一；活动槽覆盖创建答复未知，不能只用非空 task_ref 计数 |
| 许可使用 | use_id 唯一且绑定 intent_hash；一次性消费在原 Grant 锁/事务内裁决；Confirmation 保存唯一 consumed_by；lease 报告绑定原 endpoint/instance |
| 预算与费用 | 每 Task/unit 一条余额；每 reservation/unit 一条单位明细；`(source_owner, source_kind, source_id)` 唯一绑定一个 reservation 头，修订按 usage_revision/digest；`(source, provider_adjustment_key)` 唯一退款/贷记，净额投影不重复扣费 |
| 分配与委派 | 父 allocation_id 唯一；接收方 `(tenant_id, parent_owner, allocation_id)` 唯一 IncomingAllocation，至多一个子 Task；Delegation 原创建键和远端映射不可覆盖；ChildHandle 活动映射用 CAS |
| Memory/索引 | memory_id/revision 与 candidate_id/revision 分开；候选保存至多一个 Memory；change_head 在原事务行锁下推进，索引段/检查点保证连续水位，不能按 sequence/MAX(id) 猜已提交全集 |
| 来源与清理 | 正向/反向边均含准确源和派生版本；holder/copy 唯一登记；影响扫描索引带稳定 ID/游标、原控制修订与未结状态，未知 holder 不按零副本处理 |
| 扩展与实验 | Activation/实例按原 generation 与 instance_id CAS；`(plan, sample_id, arm)` 唯一 SampleRun；正式 release_request/holdout 占用与 formal_attempt_index 不因失败或换 ID 释放 |
| 传递与订阅 | Delivery 原 delivery_id/request_digest/目标实例唯一，Reply 按原 delivery 幂等保存；游标绑定主体、参数、owner、权限向量、集合版本及不可延长期限 |

同一种计量 unit、同版条件身份及关系边不能靠数组 uniqueItems 的全文比较去重，必须按业务键检查。公开 `CollectionSummary` 的 complete 表示原 owner 索引完整，不代表未结数为零；`unresolved_count <= total_count` 是必要约束，也不能替代内部逐项完成门禁。

### 4.3 外键与分区

同库可以用复合外键证明对象存在、租户一致及准确历史版本。外键不能证明权限、当前状态或外部效果；删除关联默认 RESTRICT/受控归档，不能用级联删除抹掉未结责任、账单、来源或最后墓碑。

远程引用不建伪外键，也不靠另一个库的临时查询冒充同库约束。本方保存准确 owner/object/version/digest、原登记命令、接纳证据和核对 Job；读不到远端时保持 unknown/gap。删除或停用依双方关闭/holder 协议完成，引用可定位不代表依赖当前可用。

时间分区只用于允许按期清理的历史正文/诊断等数据。命令去重、operation 取消、终态、正式试验占用和唯一计费源的约束必须覆盖整个保留期；不能在每个月分区分别唯一后漏掉跨月重放。首版可保留不按时间切分的最小身份/头表，历史明细再分区；具体分区 DDL 需证明全局唯一性和恢复扫描不受影响。

## 5 Job、outbox 与跨库交接

outbox 是本方事务内的“尚需把原事实交给某个 owner”记录，可与原命令发送记录和 Job 合表。它不是消息队列的送达证明，也不增加新服务。inbox 是接收方原命令/来源去重及已接纳依据，可复用同一逻辑账本。

| 阶段 | 本方必须持久保存 | 中断后的恢复 |
| --- | --- | --- |
| 准备 | 原 Command/业务来源键、完整或可准确恢复的载荷、原 profile/摘要、固定逻辑目标、首次期限和 Job | 未提交不发送；提交未知先查原记录 |
| 可能发送 | 原交接/Attempt 身份、发送阶段、费用/效果或回复核对责任 | 不从超时/无回执推导未发生；保持同命令同目标 |
| 对方接纳 | 接收方在本地保存原决定/回执与必要后续责任 | 回执丢失查询原命令；重复交付只回原决定 |
| 本方记账 | 原回执、准确远端对象映射、已完成的交付依据及仍未结的效果/结算责任 | 可以结束纯交付 Job，不能顺带结束领域效果/费用责任 |
| 后续修订 | 原事实修订与 correction/控制/缺陷/清理 outbox 同事务 | 交回重投、乱序与重复按来源修订合并；不得将遗漏隐藏为最终 |

接收方确认“已保存 Reply”之后，端点才结束待交回责任。通知只是加速：业务事务不依赖 NOTIFY 成功；无通知时按有限 due 索引扫描。暂时不可达不返回 not_found，正文已清理但原身份存在返回 gone。

Task 原目标不改投新 owner；内容发布恢复原 upload/publication；模型 send_started 之后不透明重发；allocation 未核清不转给替身；hostcall 未知不从检查点重放。只有费用等显式通知合同允许期限耗尽后换新的投递 command_id，原 source/usage_revision 不变。领域特例见[执行](../execution/README.md)、[账务](../accounting/README.md)与[协作](../collaboration/README.md)。

## 6 内容字节、派生索引与缓存

发布顺序为：保存原 upload/ticket 与恢复责任 → 写不可变字节并验证长度/摘要及介质耐久 → 原 owner 事务登记可发布版本/引用与后续责任。对象键和存储版本保存在元数据，不让调用方提供的任意 URL 变成读取入口。

字节写成但元数据没提交会产生孤儿；按原 upload 身份核对，不能重新生成语义不同的替代正文。孤儿清理与引用发布锁相同门禁，未知发布先核清，不能按“当前没查到引用”立即删除。跨 owner 引用先保存本方 intent，再 register_copy，并按本方 held_copy_gate 提交；双方独立事务的短暂窗口如实保留，新使用仍检查源当前控制。

Memory 检索投影、来源反向扫描、Task 展示汇总可以重建；完成/删除/卸载需要的内部关系完整性不可由未完成重建的缓存推导。每份索引记录准确源版本、策略/embedding/config、用途范围、代次及已确认连续水位。重建采用新代次、追平水位与校验后切换；旧索引继续受当前权限过滤，不能因为重建而读到已关闭资料。

缓存键至少区分租户、owner、准确版本、用途/权限代次、配置和实例代次。来源关闭、权限变化、generation 变化与查询 TTL 使相关缓存失效；命中、读取和重连都不延长 retention_until 或使用窗口。无权资料不得先参与排序/模型处理再过滤。完整规则见[内容与记忆](../memory/README.md)。

## 7 保留、清理与恢复

| 数据类别 | 保留与清理合同 |
| --- | --- |
| 原命令/取消/Task 终态最小身份 | 长期保留最后去重和关闭依据；正文可独立清理。只有更高层关闭记录仍拒绝所有旧请求，才可合并墓碑 |
| 不可变业务历史与准确内容 | 按用途、权限和 retention_until 管理；读取不滑动续期。缺失正文保留明确 gap，不能更改历史效果或伪造证据 |
| 未知效果、费用、资源占用、allocation 与清理 | 未结责任及最小恢复依据不能随 Task 终态或查询次数耗尽删除；转待处置仍保原索引/Job 或可恢复等待 |
| Result 与证据 | 成功后固定原 Result；后来证据缺陷另附 notice/适用性事实。来源关闭与正文删除不抹去曾采用哪些版本的关系 |
| Content/Memory 副本、日志、向量、备份 | 分别记停止使用与物理清理状态；pending/complete/residual/unknown 不混为一个 deleted。保留锁或离线 holder 无法清除时列范围与期限 |
| 临时环境、安装实例与缓存 | 先封新使用、核实际退出/全部 holder、关闭交接，再清字节；unknown 不当零引用，旧回调不能删除新 generation |
| 实验正式身份 | 原正式次数/holdout 占用、Exposure 和封存资格依据保持可核；取消或删除报告正文不返正式机会 |

删除是可恢复工作：先保存控制/墓碑及影响 Job，再分页通知 holder、确认停止、清理实际介质，最后记录范围准确的完成或残留。不使用跨域级联删除，也不让批处理进度先于清理事实提交。

恢复包必须共同定位：数据库一致切点、准确内容版本、原命令/墓碑/Job/未结索引、控制/撤权、owner 放置、用户来源/授权 authority 目录、InstallLock/解码器、密钥恢复依据及外部副作用核对入口。旧备份若缺最近关闭或撤权历史，只能安全诊断，不能以空表或旧 allowed 开放新动作。

生产恢复先隔离旧写者，证明候选含已确认提交并恢复第二耐久副本，再恢复可信时间、控制/查询、原未结责任，最后开放新接纳和发送。目标是单可用区故障下已确认账本 RPO=0、控制与查询恢复≤60秒；必须通过实际实验，PG 复制完成不证明设备文件或对象字节同样恢复。单个原设备/文件根失联只封闭其冲突新动作，不用另一台空设备接管相同身份。

## 8 容量、扩缩与迁移

首版按逻辑 owner 映射到存储分片，新 Task 路由可选择新 owner；已有 Task、命令及未结责任始终回原 owner。开放新来源前登记用户来源与授权 authority 集合，避免只在新分片上有任务却无法完整列举。worker 扩容只增加原库消费者，不复制预算、供应商份额或并发额度。

迁移/缩容既有 owner 不采用双写接管。先停止新准入和发送，排空或保留全部原交接，隔离旧写者，固定包含业务/墓碑/Job/控制的完整切点，复制并核验准确内容与存储格式，更新物理放置，按恢复顺序开放。默认不提供在线 Task 重分片；不因旧 owner 失联把未知额度或设备资源重复分配给新 owner。

迁移采用 expand → 有界 backfill → 切换 → 观察 → contract。保存 migration_id、制品摘要、检查点及原格式兼容范围。旧实例全部退出、旧未结责任可恢复且回退保留期结束后才删旧字段；软件回退不回滚业务历史。准确旧 profile/Schema/解码器随未结命令保留。

容量分别测量当前行、每版本/关系边/墓碑、索引、WAL、备份、正文/截图与副本；不得用“六组对象”推算磁盘。按生产假设，每日500万 Task、每Task200KiB内容约0.931TiB/日，30天约27.94TiB原始字节，尚未含索引/副本；每年18.25亿 Task 身份还要叠加命令与操作墓碑。以上是预算输入，不是实现实测。

热路径只访问当前头、完整未结索引、有限祖先和 due Job；冷历史/重建/清理按有限扫描、字节、时间及恢复游标运行。控制、效果/费用核对有非零专用连接与容量，不能被新目标积压占满。每次接纳要为最坏回执、控制与收尾保留可兑现空间，磁盘低于某百分比本身不是“仍可取消”的证明。

分片扩容、历史分区、压缩或引入其他中间件均须先证明瓶颈与净收益，再验证原唯一性、保留和交接合同。独立缓存/MQ若仅作提示不取得权威；若要替代原账本，必须重新设计一致性与恢复，不能作为透明运维优化。

## 9 DDL 与 repository 验收清单

1. 对同一命令跨分区/跨重启重投、原取消先到、同键异内容，分别观察唯一决定与永久拒绝旧身份
2. 在各原子集合提交前、提交后回复前、跨库接纳后杀进程，恢复后检查业务记录、回执、Job、outbox 和双方映射不孤立
3. 旧 Claim 遇新 Raise、同事务 Raise 后 Finish、过期领取、提交未知，验证旧工作不覆盖新责任
4. 验证复合 FK/RLS、错误 tenant/owner、远程引用缺失、同修订异摘要、重复 unit/条件 ID 和不完整关系索引
5. 将 Content 字节/元数据分阶段中断；注入引用发布与清理竞争、来源关闭、索引迟交与回滚，验证无无权发布或误删
6. 迟到费用/退款与终态并发、allocation 关闭后更正，核对原源、预留、父子汇总和 once 不重复、不复活
7. 长期墓碑增长、磁盘保护、旧备份缺撤权、旧主未隔离、对象/设备介质缺失，验证恢复不会制造新空权威
8. 在 PG 与 SQLite 各自支持的装配跑相同语义用例；生产另做三可用区耐久、容量与恢复实验，报告未覆盖前提

这些检查必须绑定实际 migration/repository commit、协议/Schema 摘要、配置、数据规模、执行计划与故障证据。当前文档和结构示例只能检验设计资产，不能替代[运行验收](../validation/README.md)。
