# 01B 任务运行内核：持久化与调度

本篇规定默认 SQLite Adapter。完整建表、约束和索引在 [runtime-kernel-v1.sql](assets/runtime-kernel-v1.sql)，不在正文复制 DDL。该资产可在空库执行，仍须实现本篇条件事务和[故障验收](01-runtime-kernel-validation.md)后才能作为运行存储。Go 接口见[01A](01-runtime-kernel-contracts.md)。

<a id="layout"></a>
## 1. 一个持久化单元保存什么

所有任务相关主键和关联都携带 namespace。数据库文件可承载多个用户，但受信访问上下文始终限定操作范围。以下是运行权威记录；执行系统仍有独立 ExecutionStore，即使同机共用文件也不合并模块事务。

| 表 | 写入者、权威内容与生命周期 |
| --- | --- |
| rt_meta | 组装／迁移入口保存格式、活动启动代次及写入口状态。它不能单独证明旧文件或旧进程已隔离。 |
| rt_task | Core 保存任务快照、主状态、控制汇总、Owner、版本、目标修订和决策需求。 |
| rt_admission_window | 受信入口保存本权威获分配的接纳窗口及关闭事实；它承接现有协议，不自行授予跨模块唯一身份。 |
| rt_operation | Core 首次登记或准入的稳定操作、不可变意图、目标适用性和内核交接阶段；执行报告只是受核验的投影。 |
| rt_work | 所属内核经受约束领取／提交保存持久责任、调度时间和租约。active_key 保证同一责任不反复创建活跃工作。 |
| rt_dependency | 内核保存操作之间的固定前置条件；输入、授权、控制等其他 gate 由等待及控制记录表达。 |
| rt_decision | 领取时保存本轮序号、基线、配置与准入资格；退役不删除用量关联。 |
| rt_budget／rt_reservation | Core 随调用和事实结算维护总额与明细，未知占用保持。 |
| rt_control_source／rt_wait | 来源化暂停及关闭修订；正式输入、依赖、人工处置等等待详情。 |
| rt_inbox／rt_outbox | 运行消息端口与 Core 保存接收、消费和交接责任；消息状态不代表外部效果。 |
| rt_commit | 当前持久化单元内原 change_id 的结果或明确封闭记录，支持创建失败前尚无 Task 的核对，因此没有 Task 外键。 |
| rt_handoff | Core 保存 Owner 移交、委派、控制传播及交付的本端进展。远端有独立权威。 |
| rt_journal | 随任务变更追加的允许保留的事实摘要及因果引用。恢复以快照和未决责任为准，不要求全日志重放。 |
| rt_closed_range | 清理前保存关闭范围与原权威，拒绝旧请求复活。不能仅用最小时间戳表示存在缺口的范围。 |

`document`／`payload` BLOB 均为版本化 `Document` 的规范化内容，表对应的登记 Schema 固定字段，不能任意装载 Go 对象。范围／资格／领取／版本／到期／预算和索引字段放在明确列里。大正文只保存受控引用、准确修订及摘要；引用发布前材料必须已在获准位置可读。删除正文后的最小核对事实按现有隐私与保留规则处理。

### 1.1 记录文档的必需字段

下表补足 SQL 标量列以外的固定运行内容。名称使用 snake_case；`*_ref` 沿对象附录类型，文档带 `schema`。只有表中明确可选的字段可以缺省。

| 记录文档 | 必需内容 |
| --- | --- |
| Task | `goal`、`constraints`、`acceptance`、`delivery`、`input_refs[]`、`cancel_sources[]`、`control_progress`、`outcome`（未终态时可 null）、`unresolved_refs[]`。 |
| Operation.intent | `kind`、`target_ref`、`input` 或固定 `input_ref`、`capability_version`（非执行类可 null）、`implementation_binding`、`deadline_ms`、`semantic_constraints`。依赖固化在 rt_dependency；首次意图摘要包括初始依赖。 |
| Operation.result_document | `producer_ref`、`report_digest`、`stage`、`outcome`、`effect_certainty`、`evidence_refs[]`、`usage_refs[]`、`disposition`；尚无报告时整个列为 null。 |
| Work | `target_ref`、`wait_keys[]`、`retry_policy_version`、`retry_deadline_ms`、`last_error`（可 null）、`reservation_refs[]`、`required_proofs[]`、`completion_condition`。 |
| Decision.binding_document | `brain_version`、`model_configuration`、`context_policy_version`、`skill_refs[]`、`generation_limits`、`reservation_refs[]`；context_document 后续保存快照引用、摘要和来源修订。 |
| Reservation | `unit`、`source_ref`、`max_authorized_amount`、`usage_evidence_refs[]`、`unknown_reason`（已知可 null）；usage_revision 约束累计用量。 |
| ControlSource | `trusted_origin_ref`、`accepted_operation_ref`、`accepted_revision`、`progress`、`child_control_refs[]`。取消来源及其传播引用保存在 Task／Handoff，不能误当可清除暂停。 |
| Wait | `reason`、`target_refs[]`、`resolution_condition`、`notification_key`（可 null）、`interaction`（材料收集时含 Schema、可回答者、期限、原问题摘要及消费操作，其他等待可 null）。 |
| Inbox | `authentication_ref`、`admission_window_ref`、`reply_to`（可 null）、`stream_position`（可 null）、`application_result`（未消费可 null）、`quarantine_reason`（正常可 null）。 |
| Outbox | `message_kind`、`lane`、`admission_window_ref`、`delivery_deadline_ms`、`receiver_receipt`（可 null）、`next_responsibility_ref`（仍需业务核对时必填）。 |
| Handoff | `original_operation_ref`、`peer_ref`、`phase`、`checkpoint_ref`（非移交可 null）、`budget_allocation`（非委派可 null）、`pending_message_refs[]`、`evidence_refs[]`；细分 phase 沿所属机制。 |
| Journal | `actor_ref`、`event_kind`、`source_refs[]`、`changed_record_refs[]`、`reason`、`recorded_ms`。不保存模型内部推理。 |
| AdmissionWindow／ClosedRange | `issuer_ref`、`authority_ref`、`format_version`、`covered_scope`、`closed_intervals[]`、`proof_ref`；单点关闭可保存确切原 ID，不用时间猜测 ID 顺序。 |

只把用于选择或约束的状态做成枚举列。Work.kind、Work.state、lane、decision.state、dispatch_phase 和 reservation.state 均由 DDL 穷尽；其他模块的 stage／outcome 枚举按所属合同，不在内核另定义一份。

### 1.2 数据库约束与规则检查各承担什么

数据库用复合外键拒绝跨任务依赖，用部分唯一索引保证一项任务只有一个 ACTIVE 决策、同一 active_key 只有一个未完成 Work，用触发器阻止原操作意图改写。工作 DONE 后可以创建同 active_key 的下一次责任，但使用新的 work_key；旧领取不能作用于新工作。

规则层额外检查依赖无环、冻结传播、权限、终态条件、预算合计和交接资格。SQL 没有外部事实，不能靠外键证明保存成功。对 SQL 有写权限的任意代码不在支持范围内；所有写入经受信 Adapter 的封闭变更合同。

预算 `used_amount` 与 `held_amount` 是同事务维护的计数；分别与预留账本累加一致。新增预留必须满足 `amount <= limit - used - held`，先检查溢出和负值。确认消费转入 used，剩余未知部分保留 held；确定未消费才释放。外部确认实际用量超过预留时如实记录超额、阻止新目标工作并升级，不能以 SQL CHECK 拒绝事实或把真实用量截断。因此 DDL 不强制 `used + held <= limit`，该条件用于新工作准入。

<a id="sqlite"></a>
## 2. 连接、写队列和耐久配置

默认一个节点的持久化单元有一个受控写入口、一个写连接和有界读连接池。共用同一 SQLite 文件的其他模块也通过同一文件写入仲裁器排队，但保持各自事务。多个 Worker 在事务外并发。不同进程需要写同一单元时，通过持有者的受控端口访问；默认不建立若干独立写队列绕开仲裁。

打开本地文件后，在物理连接初始化阶段启用并核查 `foreign_keys=ON`；采用 `journal_mode=WAL`，核对实际模式，写连接固定 `synchronous=FULL`。WAL 单库只有一个写事务，长读会影响 checkpoint，且不适合不同主机共享数据库文件。[SQLite WAL](https://www.sqlite.org/wal.html)、[foreign_keys](https://www.sqlite.org/pragma.html#pragma_foreign_keys)说明这些使用条件。

写入使用 `BEGIN IMMEDIATE`，把读取前提、条件检查和所有更新放入该短事务。开始即遇 BUSY 时尚未进行本次业务写入，按队列期限有界退避。不能在持有写事务时等待模型、网络、设备、另一个模块或人工回答。[SQLite 事务](https://www.sqlite.org/lang_transaction.html)规定 IMMEDIATE 仍可能因其他写事务而失败。

FULL 是耐久确认的配置前提，还依赖 VFS、操作系统、文件系统和存储设备兑现同步。不能用进程内读到回执代替声明故障模型下的耐久确认；NORMAL 不作为本基线已确认派发的配置。[同步模式](https://www.sqlite.org/pragma.html#pragma_synchronous)、[原子提交的底层假设](https://www.sqlite.org/atomiccommit.html)是实现锁定与故障测试依据。

写队列按 CONTROL、DISPOSITION、TARGET 划分有界份额，进行加权轮转并限制单个用户连续占用。提交进入事务前超时可明确拒绝；事务开始后取消等待不证明回滚。磁盘满、WAL 过大、队列年龄超过配置水位时收紧新目标准入，保留控制、事实接收及恢复额度；不能通过删未决回执清空积压。

<a id="transactions"></a>
## 3. 条件事务如何实现

### 3.1 Commit 与变更未知

按照[提交算法](01-runtime-kernel.md#commit)执行，并检查条件 UPDATE 的影响行数。所有快照前提在当前事务中重新读取；任务版本增加用比较更新保证，例如：

```sql
UPDATE rt_task
SET state_version = state_version + 1, updated_ms = :now_ms
WHERE namespace = :ns AND task_key = :task
  AND state_version = :expected
  AND owner_ref = :owner AND owner_epoch = :owner_epoch;
```

这段 SQL 仅展示版本比较，不包含全部资格校验；仍需检查 boot、Owner 管理阶段、控制、决策／领取及预算，然后按固定顺序写入依赖记录和回执。任一前提失败，确认回滚后返回明确拒绝。任务、工作、操作、预算及消息存在循环创建关系时，在同一事务按 Task → Operation／Budget → Work → Decision／Reservation／Inbox／Outbox 的顺序写；不依赖跨事务补齐。

`COMMIT` 返回 BUSY 时，SQLite 可能保持原事务活动，可在原事务内有界重试提交；FULL、IOERR、INTERRUPT 等错误对事务的影响不同。驱动必须能够判断并清理事务状态，不能未确认结束就把连接归还池。确认回滚前不得报告 NOT_COMMITTED；I/O 故障隔离连接与写入口，重新打开、恢复并核对耐久回执。[事务错误规则](https://www.sqlite.org/lang_transaction.html)、[autocommit 状态](https://www.sqlite.org/c3ref/get_autocommit.html)。

`rt_commit` 的 CLOSED_NOT_COMMITTED 是一个持久封闭事实：在当前唯一权威下与原提交串行竞争，原回执不存在且资格已失效时记录；以后相同 change_id 不再首次提交。它不是 Lookup 找不到后自动插入的墓碑。创建 Task 前失败的 change 也可关闭，所以该表不要求已有 Task。Receipt 保留原结果与租约相关响应，重试不产生新租期。

### 3.2 Claim、Renew 与完成

Claim 先核对原 change_id，读取当前任务及 Work；检查到期、控制分类、目标冻结、Owner 和活动启动代次，以及依赖、授权有效证明、期限和当前配额。只有 `PENDING` 或已失效的 `LEASED` 可进入领取；后者被标记为恢复处理，不能直接重做外部动作。BLOCKED 需先有事实解决 gate。

事务更新 claim_epoch 并保存 Worker、boot、owner_epoch、lease_until。DECIDE 额外要求没有 ACTIVE 决策、生成预算可预留；同事务预留、创建决策并推进任务版本。ClaimResult 返回提交后快照的版本，不另读一个已经变化的版本冒充本轮基线。

上述 DECIDE 条件仅适用于 Mode=EXECUTE。失效 LEASED 用 Mode=RECOVER 取得原 Work 的新代次，允许旧 ACTIVE 决策仍在，不分配新决策序号、不预留生成额度；持久记录 claim_mode。恢复领取只能核对／退休旧轮次，不能准入旧输出或直接调用 Brain。RETIRE_DECISION 同事务结束旧 DECIDE Work（保留历史）、退休原资格，并在仍需决策时登记新 Work；此后新 Work 才按 EXECUTE 领取。这样不会因 ACTIVE 记录存在而无法取得退休所需资格。

续租保持 claim_epoch，要求原领取未过期且所有本地资格仍有效。目标控制已禁止继续处理时不延长目标动作资格；Worker 可在原租约尚有效时提交收束事实，失效后通过可靠报告交新权威。Renew 只改变租约及回执，不触发模型输入版本变化。

完成使用同一 Lease 的 Commit，保存本轮进展、预算结算、工作结束及必要后继；清除租约字段。普通异步等待不占住 Worker 直到超时。Worker 崩溃留下 LEASED 时，后继取得新代次后先核对原进展，再安排后续。

### 3.3 派发准备、更新与 DROP

ADMIT 创建 UNSENT 操作、HELD Outbox 和 DISPATCH 工作。PREPARE_DISPATCH 事务检查当前 goal_revision、冻结 gate、控制和领取后，同时将操作改为 MAY_HAVE_SENT、消息改 READY、保存原发送责任。发送器只能在该提交已确认后取 READY 消息。

UPDATE 事务冻结所有 UNSENT 目标操作及对应 Work，并增加这些 Work 的 claim_epoch、清除领取和置 BLOCKED，使旧 Worker 的准备事务失败；已进入 MAY_HAVE_SENT 的操作安排取消／核对。活动决策标记失效需求，旧基线必因任务版本变化而不能准入，随后完成退休。冻结不阻断新的 DECIDE。

DROP 事务要求操作 UNSENT，检查所有关联 Outbox 为 HELD 且无准备记录，隔离原领取，关闭消息及工作，留下操作历史并释放有证据的未消费预留。KEEP 仅更新适用目标修订、冻结 gate 和可处理时间；不触发不可变意图更新，不重新分配预留。依赖图整体校验，不能让已废弃前置满足 SUCCESS_CONFIRMED。

<a id="time"></a>
## 4. 领取时钟及扫描公平性

每次事务从存储宿主的受控时钟取得一次 now_ms，全事务使用该值；Worker 不能传入任意“现在”延长资格。进程内等待与心跳用单调时间，持久截止点用 UTC。SQLite 的 `'now'` 源于 VFS，官方仅保证同一次 step 内一致，不保证跨事务单调。[SQLite 时间函数](https://www.sqlite.org/lang_datefunc.html)。

设计因此要求：检测明显墙钟回拨或超出配置容忍的跳变，暂停新的目标领取并重新核验期限／授权；不因回拨复活过期许可。重启后先建立当前时间和活动启动资格，对旧租约按恢复路径处理。时间异常下无法确认外部许可有效就等待；不能通过强制增加 owner_epoch 绕过授权。租约到期只允许竞争恢复，不证明旧调用停止。

Scheduler 的顺序为：轮转有权限处理的用户 → 按 CONTROL／DISPOSITION／TARGET 配额取该用户一小批候选 → 每通道按 `(due_ms, task_key, work_key)` 选择 → 条件 Claim。用户枚举使用持久任务／工作索引得到的稳定用户游标，不先截取全局最早一页再做用户公平，否则高负载用户可以挤掉其他用户。

用户列表经 WorkStore.ListNamespaces 和受信宿主分配的 SchedulerScope 取得；每项用户处理再使用该 namespace 的 Access 调用 ScanDue／Claim。普通用户不能构造调度范围或用空 namespace 越权扫描。适配器使用 rt_work_namespaces 加速活跃用户枚举，调度器不越过接口直接读数据库。

每次扫描使用固定 cutoff 和复合游标，不用 offset。一轮结束从起点重扫，避免并发插入的较早到期项永远落在游标后方；通知只提示尽早重扫，不能移动关闭水位。另扫描失效 LEASED、未消费 Inbox 和恢复责任。PENDING 索引是候选加速，不是授权依据。

ScanDue 的首个空游标请求由 Adapter 固定 cutoff；后续不透明游标绑定该时间、namespace、lane、处理位置与格式版本。篡改、过期或跨范围游标拒绝，调用者从头扫描。候选的工作状态可能已经变化，Worker 通过当前快照选择 EXECUTE 或 RECOVER，并由 Claim 再次裁决；游标不授予领取资格。

活跃责任按 `active_key` 去重：例如 `decide`、`apply:<message_id>`、`dispatch:<operation_id>`、`reconcile:<operation_id>`。新事实只将任务 decision_needed 置位，不为每个 token 或每条通知创建决策。处理一批正式事实的大小与时间有上限，批末提交新快照；不丢事实，也不无限拖延控制。

首版策略采用按通道和用户的有界轮转、通道内到期顺序；不提供任意用户自报最高优先级。各通道都有正份额与独立上限，目标工作不能被永久饿死，控制不能被长模型调用占满。故障重试使用版本化指数退避和抖动，保存 attempt 与 next due；达到次数、处置期限或额度上限转显式等待。

<a id="retention"></a>
## 5. 清理、备份与后端替换

操作、提交和消息分别保留，不按一个统一 TTL 清空。先持久关闭对应接纳范围，再证明不存在在途效果、未知提交、未应用 Inbox、待发 Outbox、控制传播、迁移引用和用量结算义务，最后才能删明细。关闭记录的权威与覆盖必须能判定旧身份，不允许清理后“查无此项”恢复首次接纳。

删除顺序遵守引用：在保留前提满足后，先清理已结束交接的 Inbox／Outbox、Decision 和依赖，再清理 Work；随后处理预留、预算、操作、等待／控制／Handoff／Journal，最后才删除任务与无剩余操作引用的接纳窗口。回执／关闭范围按各自更长义务保留。每个被删对象的全部引用必须已解除，不能为清理一个任务破坏另一项恢复义务。关键规则见[原子组与保留边界](../reference/01-data-model.md#atomic-retention)。可重建索引先重建并核对覆盖，不能把缺索引当成没有业务记录。

备份获取一致数据库切点与受控材料清单，使用锁定驱动支持的在线备份或停写一致复制；不能只复制 WAL 模式下一个主文件。恢复旧备份时原外部效果可能更晚，先隔离旧权威、核对原操作、授权与删除水位，再开放目标派发；本次不设计新的生产 HA 协议。

SQLite 与 bbolt 采用同一逻辑导出：格式版本、用户范围、记录种类、原键、规范化内容摘要、控制／关闭水位、材料引用与清单。各模块停写后保留既有单任务原子组；目标重建索引，校验唯一性、依赖、预算和全部未决责任。新 boot_epoch 隔离旧进程，原 task／operation／owner_epoch 不因数据库类型变化而改变。双向迁移需要各自的运行证据。

<a id="configuration"></a>
## 6. 配置合同

配置作为不可变版本化 profile 注入，任务及 Work 引用适用版本。以下值均必填、有限且通过关系校验；本设计不宣称已测得生产默认数值。

| 配置组 | 字段与校验 |
| --- | --- |
| 队列与线程 | write_queue_capacity、reader_count、worker_limit、各通道／每用户上限；正整数，各份额合计不超过总容量，控制和处置不能为零。 |
| 领取与调用 | lease_duration、renew_interval、clock_uncertainty、transaction_timeout、call_timeout；续租间隔加事务／时钟余量小于租期，续租不自动延长业务期限。 |
| 扫描与负载 | scan_batch、scan_interval、max_batch_time、max_active_tasks、max_work_per_task、max_dag_edges；均有界，准入前检查规模。 |
| 重试与恢复 | retry_base、retry_cap、jitter_range、max_attempts、disposition_deadline、reopen_watermark；次数／期限／额度任一耗尽均停止自动尝试。 |
| 存储与消息 | max_payload_bytes、max_snapshot_bytes、max_read_duration、max_wal_bytes、free_space_low_watermark、max_inbox／outbox_backlog；超限先背压新负载，不静默丢可靠责任。 |
| 保留 | 各接纳窗口和明细保留配置；不能短于对应未决恢复义务。缺少关闭依据时拒绝清理。 |

开发及正式验收各自保存 profile，固定精确 SQLite／驱动、OS、文件系统、同步及 checkpoint 配置。锁定时检查 SQLite 官方缺陷公告并排除适用的 WAL-reset 缺陷，实际驱动的事务取消和错误映射必须取证。[SQLite WAL 缺陷说明](https://www.sqlite.org/wal.html#the_wal_reset_bug)。参数测量与支持声明见[验证计划](01-runtime-kernel-validation.md)。
