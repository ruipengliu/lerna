# 数据身份权威与存储

每项持久记录只有一个逻辑负责方。跨模块协作使用准确引用，各方只修改自己负责的事实。逻辑对象不等于物理表；实现可以拆表，但唯一键、事务集合和保留责任必须保持。

本章提供字段与存储参考，规则的使用流程见[任务生命周期](task-lifecycle.md)和[可靠运行](runtime.md)。所有表都必须按租户或个人用户隔离。

## 公共类型

| 类型 | 规范与含义 |
| --- | --- |
| ID | 客户端或负责方生成的不可复用不透明标识；不得包含可读个人信息 |
| OwnerRef | `tenant_id + owner_id`，定位逻辑负责方；物理地址由受信发现解析 |
| ObjectRef | OwnerRef、对象类型、对象 ID；需要准确内容时必须另带 revision |
| Revision | 负责方事务内递增的非负整数；不是时间戳，不能跨对象排序 |
| ContentRef | OwnerRef、content_id、version、hash、media_type、byte_length；读取仍需当前权限 |
| ComponentRef | 组件 ID、契约版本、制品摘要、配置摘要与 InstallLock 引用 |
| Amount | `unit + integer_value`；费用用明确币种和最小计费单位，不使用浮点数累计 |
| Time | UTC 绝对时点及固定精度；日历触发另存时区和时区数据库版本 |
| CollectionView | 有界 items、cursor、exhausted、partial、gaps、读取范围与水位；不是全局快照 |

线上整数以可跨 Go/TypeScript 精确解析的十进制字符串表示。用于去重的请求摘要基于同版规范化规则；规范化只处理编码，不能改变金额、路径、收件人或正文的业务含义。Schema 必须拒绝重复 JSON 键、非有限数值和未知枚举。

## 六组核心对象

| 对象组 | 最小必要字段 | 负责方与核心约束 |
| --- | --- | --- |
| Session | session_id、revision、branch_head、state、task_refs | Interaction；不保存另一份权威 Task 状态 |
| Task | task_id、owner、revision、goal_revision、control_revision、status、requirements_ready、goal_ref、policy_ref、budget_ref、deadline、result_ref | Orchestrator；固定 owner，终态不复活 |
| Decision | decision_id、task_ref、snapshot_ref、component_ref、status、proposal_ref、model_call_ref、usage_ref | 决策引擎保存请求与提案；Snapshot 和派发意图归 Orchestrator |
| Operation | operation_id、intent_ref、capability_ref、binding_ref、phase、effect、may_apply_later、usage_ref | Orchestrator 保存 Intent，Executor 保存接纳、Attempt 与 Effect；双方不互改 |
| Content | content_id、version、hash、media_type、byte_length、storage_ref、source_refs、policy_ref、state | Content owner；同一版本字节不可变，物理副本分别登记 |
| Grant | grant_id、revision、subject、resource_scope、actions、purposes、limits、valid_from、expires_at、state | 授权 owner；能力声明或模型文字不构成许可 |

表中引用可能在生命周期早期为空，例如未完成 Task 没有 result_ref。实现 Schema 必须按状态定义可空条件，不能用空字符串代替不存在的对象。

## 策略与能力声明

TaskPolicy 是受信配置的准确版本，创建 Task 时固定。模型可以提出资源使用建议，不能修改策略。策略升级默认只影响新任务；收紧当前任务限制需要受信控制命令，扩大权限或预算需要新的授权与分配。

| 结构 | 必须定义的内容 | 默认约束 |
| --- | --- | --- |
| TaskPolicy | policy_id、revision、allowed_capabilities、allowed_assurance、max_decisions、max_operations、max_inflight、max_no_progress、deadline_policy、budget_units、verification_reserve、settlement_reserve | 计数均有有限上限；max_inflight 不超过实际资源与授权上限 |
| DelegationPolicy | max_depth、max_children、allowed_remote_profiles、shareable_sources、parent_control_behavior | 未启用时不允许委派；开启后子额度与期限受父范围约束 |
| Capability | capability_id、contract_version、input_schema_ref、output_schema_ref、resource_type、effect_predicate、idempotency_mode、query_mode、cancel_mode、cost_bound、required_purposes、platforms | 缺少保证显式声明 unknown 或 unsupported，不能用默认 true 补齐 |
| RuleSpec | rule_id、revision、kind、适用条件和成果类型、所需证据、判断参数、allowed_assurance、缺证据行为 | kind 为 effect、constraint 或 quality；缺证据返回 unknown，必要条件不得跳过 |
| StrategyConfig | strategy_id、revision、组件引用、提示模板引用、材料选择与停止参数 | 不能覆盖 TaskPolicy、Grant 或效果规则 |

能力声明是可审核契约，真正是否允许调用还取决于当前 Binding、许可和策略。RuleSpec 规定“怎样判断”，Evaluator 是实现这个判断的代码或模型，两者必须分别固定版本。

## 任务内部记录

| 记录 | 必要字段 | 约束或索引 |
| --- | --- | --- |
| GoalRevision | task_id、goal_revision、原输入引用、完整目标、有效条件集合、覆盖判断 | `(task_id, goal_revision)` 唯一；历史不覆写 |
| Requirement | requirement_id、revision、statement_ref、source_refs、kind、required、rule_ref、artifact_scope | 替换需绑定旧修订；必要条件不能被平均分抵消 |
| Snapshot | snapshot_id、goal_revision、control_revision、材料与事实引用、来源集合、预算、组件版本、缺口 | 不可变；固定准确输入，允许受控清理正文 |
| DecisionDispatch | decision_id、task_id、snapshot_ref、payload_digest、command_ref、consumed | 同 Task 下 decision_id 唯一消费 |
| OperationIntent | operation_id、task_ref、goal_revision、control_revision、来源键、最终参数、授权使用与预留、binding_ref | `(task_id, source_key)` 唯一；来源包括 Decision 行动位置 |
| ConditionResult | result_id、requirement_ref、goal_revision、artifact_ref、rule_ref、evaluator_ref、verdict、evidence_refs、assurance、validity | 判断不可改写；当前有效性单独维护 |
| Responsibility | task_id、kind、object_ref、state、closure_ref | 派发前登记；按 task_id、state 建完整索引，用于完成门禁 |
| Result | result_id、task_id、goal_revision、outcome、artifact_refs、condition_results、limitations、created_at | Task 最多一个正式终态 Result；后续缺陷另记 |
| WaitCondition | task_id、reason、owner、object_ref、predicate、deadline | 等待必须可检查，不依赖内存 callback |

Task 的结果状态和收尾状态分开。`effects_closed` 由完整 Responsibility 集合和可信关闭依据决定；`spending_closed` 由各实际消费来源决定；`cleanup_complete` 由副本清理决定。它们不得由一个通用 done 字段推导。

## 执行与公共工作记录

| 记录 | 必要字段 | 约束或索引 |
| --- | --- | --- |
| CommandReceipt | command_id、method、target、subject、payload_digest、accept_before、state、object_ref、reason | `(tenant_id, owner_id, command_id)` 唯一；接纳 state 不可变，执行状态归关联对象 |
| Job | job_id、object_ref、phase、work_revision、completed_revision、due_at、state、lease_epoch、lease_until | 按工作池、state、due_at 有界扫描；对象阶段唯一工作键 |
| Attempt | attempt_id、operation_id、ordinal、request_key、input_digest、start_permission、send_state、started_at、observation_refs | `(operation_id, ordinal)` 唯一；请求键跨安全重传保持 |
| EffectObservation | observation_id、operation_id、source、observed_at、effect、evidence_ref、may_apply_later | 只接受已认证的目标证据；冲突观察保留并进入核对 |
| TaskGate | task_ref、control_revision、allowed、valid_until、issuer、control_digest、issuer_proof | Executor 的启动依据；原 owner 签发，修订单调，过期封新启动 |
| ResourceClaim | resource_ref、operation_id、generation、holder、lease_until、fence_evidence | 释放必须核验原 generation 和真实退出条件 |

同一 Operation 可以有多次物理 Attempt，但只有在能力声明与证据支持安全重试时才允许新增。EffectObservation 是观察，当前 Effect 是领域归并结果；不能按不同机器的时间戳取“最后一条”为真。

## 主要生命周期枚举

| 对象 | 状态或阶段 | 终结及异常语义 |
| --- | --- | --- |
| Task.status | active、waiting、paused、succeeded、failed、cancelled | 后三者终态；收尾记录独立更新 |
| CommandReceipt.state | accepted、applied、rejected | 首次决定固定，不随业务执行成功或失败改写 |
| Decision.status | accepted、running、waiting、completed、failed、cancelled | completed 仅表示提案与必要内容已发布；后续不回 running；费用继续核对 |
| Operation.phase | accepted、prepared、sending、reconciling、closed | 进入发送不代表效果发生；closed 要有确定效果且不会迟到 |
| Operation.effect | not_applied、applied、unknown | 与 phase 分开；初始 not_applied 但仍可能随后发生，不算关闭 |
| Job.state | ready、leased、waiting、done | 新 work_revision 可使 done 再次 ready；不能复活终态业务对象 |
| Content.state | preparing、published、restricted、deleted | published 前不得作为已可读产物交付；清理状态单独维护 |
| MemoryRecord.state | active、needs_review、disabled、deleted | 更正产生新修订；deleted 身份不重用 |

状态是领域观察点，完整允许转换必须在该 profile 的机器契约中编码。例如 Operation 超时进入 reconciling，不得直接 closed；Decision 已发送但结果未知进入 waiting，达到策略终止条件可变为 failed 并保留原用量责任。具体转换的触发、门禁和处理遵守各章算法，未定义的转换必须拒绝。

## 记忆授权与扩展记录

| 记录 | 必要字段 | 负责方 |
| --- | --- | --- |
| MemoryRecord | memory_id、revision、type、content_ref、sources、scope、observed_at、valid_interval、confidence、state | Memory |
| ExtractionCandidate | candidate_id、revision、input_refs、values、saving_permission、state、published_memory_ref | Memory；候选只能发布一次 |
| ContentHolder | content_ref、holder_owner、holder_id、purpose、policy_revision、state | 持有者登记；原来源维护受控传播关系 |
| GrantUse | use_id、grant_ref、exact_action_digest、resource、purpose、limit、expires_at、state | Grant owner；一次性消费不可复活 |
| Confirmation | confirmation_id、command_ref、payload_digest、request_revision、subject、decision、expires_at、consumed | 实际消费决定的业务 owner |
| Reservation | reservation_id、root_task_ref、scope、amounts、state、settlement_ref | 预算 owner；子额度从根额度扣减 |
| UseSettlement | source_ref、usage_revision、cumulative_amount、reserved_amount、new_spending_closed、final_usage_known | 原费用 owner；按累计修订结算增量 |
| InstallLock | lock_id、artifact_digest、dependencies、config_digest、contract_profiles、data_formats、platform | 扩展 owner；不可变 |
| BindingHead | target_id、revision、generation、activation_ref、enabled | 扩展 owner；新任务默认选择头，保留的旧 Activation 可以独立服务旧 holder |
| InstanceReadiness | instance_id、binding_ref、generation、config_digest、self_test、approval_ref、expires_at | 扩展 owner；重启不能继承原实例就绪 |
| ReleaseApproval | approval_id、install_ref、purpose、target_scope、evidence_refs、limits、expires_at、state | 受信批准者所属治理 owner |

Subject、资源范围与用途都是结构化值。任意自然语言描述不能直接作为可执行授权范围。确切结构由所开放的资源类型定义并关闭 Schema；未支持的范围返回 unsupported。

## 可选能力记录

| 记录 | 必要字段 | 生命周期要求 |
| --- | --- | --- |
| Delegation | parent_task_ref、parent_goal_revision、child_or_remote_ref、goal_ref、requirements、grant_scope、budget_allocation、deadline、closure | 创建与取消分别可恢复；父子不能循环 |
| Surface 与 InputRequest | surface_id、revision、app_binding；request_id、target、schema_ref、body_ref、expires_at、state | 界面对象可独立于 Task；输入由原业务 owner 消费 |
| Schedule 与 Occurrence | rule_ref、timezone_version、state；planned_at、original_command、task_ref、slot_state | 发生时点唯一，未知创建不释放活动槽 |
| Environment | environment_id、generation、install_ref、namespace_ref、state、resource_limits | 被动变量版本与 hostcall 效果分开 |
| EvaluationRun 与 SampleRun | 冻结计划、样本清单、实验臂、环境、版本、预算；样本与臂的唯一执行责任 | 重试不增加样本，不改变统计分母 |

未启用能力不需要建立对应服务。实现者不得为统一表结构虚构 Task、模型调用或外部效果。

## 事务与数据库装配

默认采用权威关系记录加追加观察与审计记录。当前状态在本地事务中维护，追加记录用于解释、诊断与恢复依据；不要求从全量事件重放重建所有业务状态。

| 事务范围 | 必须共同提交 | 不能包含 |
| --- | --- | --- |
| Orchestrator 所属 PG 分片 | Task 变更、目标与条件、原命令回执、行动意图、责任登记、同库授权和预算使用、后续 Job | 远端 Executor 接纳、对象存储上传、供应商调用 |
| 决策引擎所属库 | Decision 接纳、模型发送阶段、产出发布责任、用量与 Job | 供应商实际响应和 Task 完成 |
| Executor 所属库 | Operation 接纳、Attempt 准备、资源门禁、效果观察、用量与 Job | 外部文件、API 或设备的实际提交 |
| Memory 与 Content 所属库 | 版本发布元数据、来源关系、holder、变更水位、投影或清理 Job | 不可原子参加的对象介质写入 |

云端服务默认使用 PostgreSQL，设备执行账本使用 SQLite，正文和大型产物使用对象存储。相同机器上的两个 owner 不自动共享事务。只有宿主明确装配并声明同库集合时才可共用 Tx。

对象内容发布采用“准备元数据 → 写入准确字节 → 核验摘要与耐久条件 → 事务发布可读引用”。失败留下可恢复上传或孤儿清理责任，不发布指向缺失字节的成功引用。

## 隔离索引与保留

所有主键、唯一键、外键访问和查询都包含租户上下文；数据库行级隔离可以作为第二道保护，不能替代服务授权。索引不得跨租户产生未授权存在性提示。密钥只保存受信引用，不能进入模型上下文或一般遥测。

去重和终态记录长期保留获准的最小身份、摘要、最终决定与拒绝依据。正文和敏感附件按用途及保留策略独立清理。只有仍有可验证规则拒绝所有旧请求时，才允许合并或回收具体墓碑；随机 ID 和“很久没重试”不是证明。

记忆与内容变更水位必须在事务锁定的计数器中随业务提交推进。数据库 sequence 的分配顺序不等于提交顺序，不能直接作为完整增量同步水位。日志缺口或权限变化使原同步视图失效，需要重建快照。

备份恢复必须同时恢复权威记录、最小去重依据与适用内容版本，并核对已经对外发生的效果。将数据库恢复到旧时点不能撤销外部行为；恢复演练必须包含这类差异的隔离与核对。
