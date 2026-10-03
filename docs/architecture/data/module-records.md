# 模块内部记录字段

本附录补齐[核心字段字典](field-reference.md)之外的既有内部记录和可选模块对象。字段是最低逻辑存储合同。它们不自动成为公共方法；对外开放前还要补齐该方法的闭合 Schema、错误、回执和恢复测试。下列组合字段可物理拆表，但不能丢掉身份、版本或责任。

类型沿用核心字典。`T[]` 是有界集合或完整关系表；可增长集合不直接内嵌无限 JSON。`ContentRef<Schema>` 表示准确内容按登记的闭合 Schema 验证，不能承载任意未版本化业务字段。`?` 表示可选，`条件` 表示满足所述状态时必须存在。时间、金额、主体及范围默认没有隐含值。

所有权威表还带受信 `tenant_id/owner_id`（Id）、首次创建时间 `created_at`（Time）和版本记录时间 `recorded_at`（Time），可变头带 `revision`（Revision）及 `updated_at`（Time）。Task 的请求主体由原 submit CommandReceipt.principal_ref 固定，按主体建立的任务列表索引只保存该关联的投影，不能重新指定请求者。内部键总是带 tenant/owner；表中省略这两个前缀。内容正文引用与元数据分别执行保留策略。物理表、索引及删除顺序见[存储附录](storage.md)。

## 1 应用与呈现

Session、Branch、Message、Submission、InputRequest 的逐字段定义在[核心字典](field-reference.md)。它们由应用或实际输入消费 owner 保存；不能由 UI 任意更新业务消费事实。

### Surface 与 Presentation

Surface 的 owner 是应用。唯一键为 surface_id；准确快照按 (surface_id,revision) 不可变。Presentation 是端点呈现意图；它不是“用户已阅读”的证明。

| 记录.字段 | 类型 | 必填 | 含义 |
| --- | --- | --- | --- |
| Surface.surface_id / revision | Id / Revision | 是 | 稳定界面对象与可见版本 |
| Surface.app_binding_ref | ObjectRef | 是 | 已登记应用事件 Schema、handler 和目标命令绑定 |
| Surface.task_ref | ObjectRef | 否 | 可独立于 Task；关联不授予修改 Task 权限 |
| Surface.snapshot_ref | ContentRef | 是 | 已耐久的准确界面快照 |
| Surface.request_refs | ObjectRef[] | 是 | 引用原业务 InputRequest；可为空 |
| Surface.state | open/closed/deleted | 是 | 应用对象生命周期；不是网络窗口状态 |
| Presentation.presentation_id / surface_ref | Id / ObjectRef | 是 | 呈现责任与准确 Surface |
| Presentation.endpoint_id / instance_id | Id / Id | 是 | 配对端点及本次实例 |
| Presentation.intent / intent_revision | open/close / Revision | 是 | 当前持久呈现意图；较旧回调不能覆盖 |
| Presentation.generation / request_revision | Revision / Revision | 是/条件 | 读取代次；有请求时固定请求版本 |
| Presentation.state / last_receipt_ref | pending/presented/closed/unknown / ObjectRef | 是/否 | 原端点呈现事实；不能推导本人消费 |

### Schedule 与 Occurrence

owner 是应用；TriggerWorker 只领取 Job。Schedule 编辑增加 rule_revision，普通展示修订另用 revision。Occurrence 唯一键为 (schedule_id,rule_revision,planned_at_utc,fold)。已生成实例的模板和首次接纳期限不随编辑变化。

| 记录.字段 | 类型 | 必填 | 含义 |
| --- | --- | --- | --- |
| Schedule.schedule_id / revision / rule_revision | Id / Revision / Revision | 是 | 稳定规则、可见版本、调度规则版本 |
| Schedule.state | enabled/paused/deleted | 是 | 新触发门禁；不是已发 Task 的控制 |
| Schedule.timezone / tzdb_version | string / string | 是 | IANA 时区及固定解释版本 |
| Schedule.spec | ScheduleSpec | 是 | once_at/interval/daily/weekly/monthly 的准确规则 |
| Schedule.template_ref / policy_ref / install_lock_ref | ContentRef / ComponentRef / ComponentRef | 是 | 原目标模板及准确策略/安装 |
| Schedule.task_timeout_seconds / budget | Count / Amount[] | 是 | 每次任务期限与单位上限 |
| Schedule.exhausted | boolean | 是 | 无未来时点；与enabled/paused/deleted门禁分开 |
| Schedule.effective_after / next_due_at | Time / Time | 是/条件 | 编辑切点；仍有未来触发时必须有 next_due |
| Schedule.max_concurrency / misfire_policy / dst_policy | Count / skip / skip_gap_earlier_fold | 是 | 初值1；错过阈值60秒；不静默补跑 |
| ScheduleSkipRange.range_id / schedule_id / rule_revision / first_planned_at / last_planned_at / count / reason | Id / Id / Revision / Time / Time / Count / missed或paused | 是 | 原批次与next_due同事务保存的合法计划时点汇总；按规则版本/原区间去重，不混入Occurrence总数 |
| Occurrence.occurrence_id / schedule_id / rule_revision | Id / Id / Revision | 是 | 原规则实例身份 |
| Occurrence.planned_at_utc / local_slot / fold | Time / string / 0或1 | 是 | 准确计划时点与日历歧义选择 |
| Occurrence.template_ref / history_cutoff | ContentRef / Count | 是 | 本次冻结输入与历史截止 |
| Occurrence.phase | recorded/sending/accepted/skipped/rejected | 是 | 创建与接纳阶段 |
| Occurrence.command_ref / orchestrator_id / accept_before | ObjectRef / Id / Time | 条件 | 首次发送前必须固定，之后不刷新 |
| Occurrence.slot_closed / closure_ref | boolean / ObjectRef | 是/条件 | 原目标与效果均封闭后释放活动槽；关闭时保存准确依据，纯账务不阻塞 |
| Occurrence.task_ref / receipt_ref / skip_reason | ObjectRef / ObjectRef / string | 条件 | accepted 有原 Task/回执；skipped 有原因 |

## 2 任务编排

Task、GoalRevision、Requirement、RequirementAdoption、GoalCoverage、RuleDefinition、ConditionResult、Result 已有核心 Schema。Task的pending_goal_command只在steer准备时存在，与collecting门禁及控制传播共同保存；GoalDocument固定initial_goal_ref、有序amendment_refs和格式版本，不做模型摘要替换。Result是原库中的不可变业务记录；ResultPublication保存result_ref、state=pending/published/unavailable、published时的content_ref、固定upload/command及Job，属于独立导出责任。以下记录归原 Orchestrator，按 Task 锁串行；不能交由 Brain 或任务列表投影写入。

### TaskPolicy 与决策采纳

TaskPolicy 是不可变准确配置，不在任务运行中自行放宽。DecisionConsumption 唯一键为 (task_id,decision_id)，任何 outcome 都占用原来源。

| 记录.字段 | 类型 | 必填 | 含义 |
| --- | --- | --- | --- |
| TaskPolicy.policy_ref | ComponentRef | 是 | 策略准确版本与摘要 |
| TaskPolicy.continuation_limit / repair_limit / no_progress_limit | Count | 是 | 累计续行、修复、连续无进展上限 |
| TaskPolicy.context_round_limit / safe_attempt_limit | Count | 是 | 单轮补上下文和安全物理尝试上限 |
| TaskPolicy.max_requirements / max_delegations / max_depth | Count | 是 | 条件与委派数量/树深上限，不截断 |
| TaskPolicy.cost_mode / budget_limits | strict/estimate / Amount[] | 是 | 严格或已接受估算风险；不同单位分开 |
| TaskPolicy.max_evidence_staleness_seconds / max_duration_seconds | Count | 是 | 证据陈旧窗口和总期限 |
| TaskPolicy.input_policy_ref / rule_registry_ref | ComponentRef / ComponentRef | 是 | 哪些含义须澄清、可用判断规则和校验版本 |
| DecisionConsumption.task_id / decision_id | Id | 是 | 原 Task 与 Brain 决策身份 |
| DecisionConsumption.snapshot_ref / expected_goal_revision / expected_control_revision | ContentRef / Revision / Revision | 是 | 原判断的准确依赖 |
| DecisionConsumption.outcome | adopted/stale/rejected/requirements_changed | 是 | 固定消费决定，不因重投改变 |
| DecisionConsumption.adoption_id / admitted_operation_ids | Id / Id[] | 否/是 | 条件变化关联；本轮实际准入操作集合，可为空 |
| DecisionConsumption.reason_ref / decided_at | ContentRef / Time | 是 | 不含隐藏授权的解释与裁决时点 |

### Plan 与 PlanStepAdmission

Plan 是候选 DAG。其状态不取代 Operation；执行事实沿唯一准入映射读取。只按 (task_id,plan_id,plan_revision,step_id) 创建一次 PlanStepAdmission。

| 记录.字段 | 类型 | 必填 | 含义 |
| --- | --- | --- | --- |
| Plan.plan_id / revision / task_id / goal_revision | Id / Revision / Id / Revision | 是 | 准确计划与目标 |
| Plan.base_plan_ref | ObjectRef | 否 | 修订时用于 CAS；初始计划可省略 |
| Plan.steps | PlanStep[] | 是 | 有界 DAG；step_id 在版本内唯一 |
| PlanStep.step_id / depends_on | string / string[] | 是 | 局部身份及依赖，无环 |
| PlanStep.action_template_ref / pass_condition_refs | ContentRef / RequirementRef[] | 是 | 闭合行动模板及必须通过的当前条件 |
| PlanStep.bindings | ParameterBinding[] | 是 | 固定字面量或前序已核实输出的 JSON Pointer |
| ParameterBinding.source_step_id / source_pointer / target_pointer | string / string / string | 条件 | 引用前序输出时必填；只允许 arguments 或委派 goal/input |
| PlanStepAdmission.operation_ref / delegation_ref | ObjectRef | 二选一 | 已准入原操作或委派，不能重绑定新身份 |
| PlanStepAdmission.input_digest / admitted_at | Digest / Time | 是 | 固定物化输入与准入时点 |

### OperationIntent 与完整关系索引

Intent 按 operation_id 不可变；同键异意图拒绝。它由 Orchestrator 保存，Executor 的 Operation 另存实际事实。operation_id 不是每次发送的新键。use_intent_refs 是发送方内部的原授权需求；对外 execution.invoke.use_refs 只能引用已取得且可核验的原使用回执。使用未取得或部分消费失败时不派发目标动作，保留原交接/结算责任。

| 字段 | 类型 | 必填 | 含义 |
| --- | --- | --- | --- |
| operation_id / task_ref / goal_revision / control_revision | Id / ObjectRef / Revision / Revision | 是 | 原任务及准入依赖 |
| admission_source_kind / admission_source_ref / source_position | decision/plan/check/hostcall / ObjectRef / string | 是 | 唯一来源；hostcall 使用原父 operation 与 call_position |
| admission_purpose | goal_action/requirement_check | 是 | 目标行动须 ready；条件核验例外只可安全有界取证 |
| capability_ref / binding_ref / install_lock_ref | ComponentRef / ObjectRef / ComponentRef | 是 | 准确语义、实现及代码版本 |
| arguments_ref / resources_ref / intent_hash | ContentRef<CapabilityInput> / ContentRef<ResourceSet> / Digest | 是 | 已准备完的参数、规范资源与完整意图摘要 |
| requirement_refs | RequirementRef[] | 是 | 行动针对的条件；核验/纯收尾可为空并注明目的 |
| use_intent_refs / reservation_ref / cost_bound | ObjectRef[] / ObjectRef / Amount[] | 是 | 准入时固定原 use_id、意图摘要及授权需求；不声称已经取得 UseReceipt。原使用回执随后附在派发记录，禁止改不可变 Intent |
| executor_id / command_ref / deadline | Id / ObjectRef / Time | 是 | 固定接收方、原执行命令、领域截止 |
| retry_of_operation_ref / logical_step_key | ObjectRef / string | 否/是 | 明确后续恢复沿原步骤累计计数；新ID不返一次许可或尝试额度 |
| processed_source_refs / disclosed_source_refs | ContentRef[] | 是 | 全部处理与实际外发来源，两者不可混同 |

TaskOperation、TaskDelegation、TaskControlTarget、TaskCheck 是原 Task 的完整关系记录。各带 task_id、target owner/id、关联 revision、purpose、closed/unresolved 标记和 source_revision。唯一键为 (task_id,target owner,target id,关系种类)；更新与 Task 修订和集合计数同事务。标记只是源事实投影，源未知不可标 closed。完成事务直接核完整索引及原门禁，不从公开页推导空集。

## 3 Brain 与固定上下文

Snapshot 由 Orchestrator 创建；DecisionRecord、ModelCall 和产出发布由 Brain 写入。DecisionDispatchIntent 只代表发送方已固定交接，不证明 Brain 已接纳。

| 记录.字段 | 类型 | 必填 | 含义 |
| --- | --- | --- | --- |
| Snapshot.snapshot_id / revision / task_ref | Id / Revision / ObjectRef | 是 | 不可变决策输入身份 |
| Snapshot.goal_revision / control_revision / goal_ref | Revision / Revision / ContentRef | 是 | 准确目标和控制 |
| Snapshot.requirements / requirements_digest / coverage_ref | Requirement[] / Digest / ObjectRef | 是/是/条件 | 全部当前条件；ready 时有当前覆盖 |
| Snapshot.requirements_state / purpose | collecting/awaiting_input/validating/ready / interpret_requirements/decide | 是 | 明确提炼或目标决策，防止空条件继续执行 |
| Snapshot.fact_refs / unresolved_collections | ObjectRef[] / CollectionSummary[] | 是 | 效果、子委派、控制等完整核心事实与缺口 |
| Snapshot.policy_ref / install_lock_ref / model_profile_ref | ComponentRef | 是 | 本轮固定配置，不读 latest |
| Snapshot.capability_refs / binding_refs / material_refs | ComponentRef[] / ObjectRef[] / ContentRef[] | 是 | 实際可用能力与获准材料 |
| Snapshot.selection_report_ref / processed_sources | ContentRef / ContentRef[] | 是 | 选择、裁剪及缺口；含处理后未进入最终提示的来源 |
| Snapshot.input_tokens / reserved_output_tokens / safety_margin_tokens | Count | 是 | 完整编码后的预算 |
| Snapshot.count_mode / tokenizer_ref / encoded_digest | exact/upper_bound/estimate / ComponentRef / Digest | 是 | 计数可信级别与实际出口编码；估算不支持硬上限 |
| ModelCall.call_id / decision_id / provider_request_key | Id / Id / string | 是/是/条件 | Brain 分配的调用身份在首发前固定；供应商支持预分配/幂等查询时才预存其键，回复后取得的键随后保存，无法查询时保留unknown |
| ModelCall.phase / send_started_at | prepared/send_started/result_known/provider_result_unknown / Time | 是/条件 | send_started 提交后不透明重发 |
| ModelCall.encoded_digest / receiver / model_profile_ref | Digest / string / ComponentRef | 是 | 实际接收方与准确输入/模型配置 |
| ModelCall.response_ref / usage_ref | ContentRef / ObjectRef | 否/是 | 原输出与累计计费来源；未知仍保留费用占用 |
| Proposal.kind / reason_ref | refine_requirements/act/need_context/request_input/complete/fail / ContentRef | 是 | 原 Snapshot 上的建议，理由不授予权限 |
| Proposal.requirement_delta | RequirementDelta | 否 | 条件候选，实质采纳后同轮其他建议失效 |
| Proposal.body_ref | ContentRef<ProposalKind> | 是 | 对应 kind 的闭合字段：动作/计划、上下文缺口、问题、成果或失败依据 |
| Publication.publication_id / decision_id / state | Id / Id / preparing/publishing/published/failed | 是 | 原产物发布责任 |
| Publication.outputs | OutputPublication[] | 是 | 每 local_id 的准确 content/version/upload/command 映射及依赖 DAG |
| OutputPublication.local_id / content_id / version / upload_id / command_ref | string / Id / Revision / Id / ObjectRef | 是 | 模型只给local_id；Brain在准备时固定真实发布身份与命令，不猜最终hash |
| OutputPublication.content_ref / phase | ContentRef / prepared/encoded/published/cleanup | 条件/是 | 依赖替换后才形成准确最终字节；encoded/published必须有完整ContentRef。原content.put命令在首次发送前封存最终载荷，不重解释已发送命令 |
| TypedDecisionChain.root_decision_id / spec_ref / upgrade_used | Id / ComponentRef / Count | 是 | 固定有限候选规格、最多一次升级；改名不重置 |

模型提案正文需要 kind 专属闭合 Schema。当前核心 Schema 只形式化 RequirementDelta 和 DecisionRecord 的 proposal_ref，不宣称其余 Proposal/模型编码已可跨实现互操作。

## 4 Executor 与真实目标

Operation 是 Executor 的权威记录，详见核心字典。Effect 是它的独立事实维度，不另建可被上层覆盖的“成功状态”。

### Attempt 与资源

Attempt 唯一键 (operation_id,attempt_no)，有自己的不可复用 attempt_id。外部入口只接受符合当前门禁的原尝试。Job 领取不增加 attempt_no。

| 记录.字段 | 类型 | 必填 | 含义 |
| --- | --- | --- | --- |
| Attempt.attempt_id / operation_id / attempt_no | Id / Id / Revision | 是 | 物理尝试与原操作 |
| Attempt.phase | prepared/possibly_sent/response_known/reconciled | 是 | 可能发送必须保留未知；不猜未发生 |
| Attempt.request_digest / target_request_key / encoded_ref | Digest / string / ContentRef | 是/条件/是 | 准确出口与原编码；目标支持时才有其幂等/查询键，GUI等无键时依独立观察，不伪造查询能力 |
| Attempt.started_at / observed_at / response_ref | Time / Time / ContentRef | 否 | 只填可信已知事实；无回复不填伪造时点 |
| Attempt.effect / may_apply_later / evidence_refs | 与 Operation 同枚举 / ContentRef[] | 是 | 本次物理效果及迟到可能，归并到原 Operation |
| Attempt.use_refs / resource_refs / usage_ref | ObjectRef[] / ObjectRef[] / ObjectRef | 是 | 真实使用、占用及原账单 |
| TaskGate.task_ref / goal_revision / control_revision | ObjectRef / Revision / Revision | 是 | 执行宿主当前收到的最高有效控制 |
| TaskGate.status / control / control_digest | Task 枚举 / Digest | 是 | 最高控制事实；有限窗口另以ControlSnapshot.window_id保存，不得把签发时间并入控制事实比较 |
| ResourceLease.resource_id / holder_id / instance_id | Id | 是 | 稳定资源、占用主体、实际实例 |
| ResourceLease.control_epoch / lease_until / state | Revision / Time / held/releasing/released/unknown | 是 | 设备入口代次与可验证释放事实 |
| ResourceLease.operation_refs | ObjectRef[] | 是 | 全部在途/核对关联；可增长关系表 |
| Observation.observation_id / resource_ref / instance_id / control_epoch | Id / ObjectRef / Id / Revision | 是 | 原设备和观察代次 |
| Observation.content_refs / target_version / observed_at / action_before | ContentRef[] / string / Time / Time | 是 | 截图/树、目标版本和可行动窗口；准确位置从此取 |
| FileJournal.operation_id / resource_id / path_key | Id / Id / string | 是 | 受控根内的规范目标，不是原始字符串前缀 |
| FileJournal.expected_version / temp_identity / before_hash / after_hash | string / string / Digest / Digest | 是/是/否/是 | 前置版本、原临时文件及关联原写入的证据 |
| FileJournal.phase / directory_synced / evidence_refs | prepared/temp_synced/replaced/verified / boolean / ContentRef[] | 是 | 文件介质恢复阶段；摘要相同不独自证明原写发生 |

### Environment 与 hostcall

Environment owner 是 Executor。环境不是 Task，也不持有用户目标完成状态。停止请求与进程真实退出分开。

| 记录.字段 | 类型 | 必填 | 含义 |
| --- | --- | --- | --- |
| Environment.environment_id / revision / config_ref / isolation_digest | Id / Revision / ComponentRef / Digest | 是 | 准确配置和隔离证据 |
| Environment.install_lock_ref / instance_id / generation | ComponentRef / Id / Revision | 是 | 原代码和当前实例；旧回调不覆盖新代次 |
| Environment.phase | preparing/active/closing/closed/destroying/destroyed | 是 | 生命周期；只有实际 ready 才 active |
| Environment.limits / expires_at / processed_sources | Amount[] / Time / ContentRef[] | 是 | CPU/内存/时长等准确单位上限、期限、来源 |
| Environment.namespace_ref / namespace_revision / ready_for_cell | ContentRef / Revision / boolean | 条件/是/是 | 已提交被动命名空间及可运行门禁；active/closed有准确head，cell失败不发布部分变量 |
| Environment.active_operations / stop_residuals | CollectionSummary | 是 | 原完整关系的查询摘要，不是停止证明 |
| Cell.operation_ref / environment_ref / expected_generation | ObjectRef / ObjectRef / Revision | 是 | 每个 cell 就是原 Operation，不再创建平行运行权威 |
| Cell.expected_namespace_revision | Revision | 是 | 运行前固定的已提交变量版本，成功时CAS更新 |
| Cell.code_ref / input_ref / output_schema_ref | ContentRef / ContentRef / ComponentRef | 是 | 代码、输入和闭合结果 Schema |
| HostCall.parent_operation_id / call_position / input_digest | Id / string / Digest | 是 | 唯一原位置键；同位置异参拒绝 |
| HostCall.command_ref / target_kind / target_ref / receipt_ref | ObjectRef / operation或decision或delegation / ObjectRef / ObjectRef | 是/条件/条件/条件 | 原命令；接纳后恰一类准确子责任及原回执；不能只保存工具支路 |
| Checkpoint.checkpoint_id / environment_ref / generation / content_ref | Id / ObjectRef / Revision / ContentRef | 是 | 原环境准确检查点；不代表外部效果回滚 |
| Checkpoint.format_ref / processed_sources / created_at | ComponentRef / ContentRef[] / Time | 是 | 可读格式、继承来源、时间；恢复仍检查当前许可 |

## 5 Content 与 Memory

### 内容版本及副本

ContentRef 是引用；ContentVersion 是其 owner 的元数据。对象存储 key 不直接向任意调用者披露。唯一键 (content_id,version)，来源边发布后不可改。

| 记录.字段 | 类型 | 必填 | 含义 |
| --- | --- | --- | --- |
| ContentVersion.content_ref / object_key / object_version | ContentRef / string / string | 是 | 准确字节与存储定位；不是 latest URL |
| ContentVersion.state / control_revision | prepared/published/closed/deleted / Revision | 是 | 是否可引用及当前使用门禁 |
| ContentVersion.policy_ref / retention_until / published_at | ComponentRef / Time / Time | 是/是/条件 | 保存/用途限制，最晚保留时间，发布时点 |
| ContentVersion.processed_sources / disclosed_sources | ContentRef[] | 是 | 全部实际处理来源与此次披露来源分别存 |
| SourceEdge.derived_ref / source_ref / relation | ContentRef / ContentRef / processed/quoted/transformed | 是 | 准确版本 DAG；引用不是全部 processed 集合 |
| CopyHolder.copy_id / content_ref / holder_ref | Id / ContentRef / ObjectRef | 是 | 哪个主体在哪持有准确副本 |
| CopyHolder.purpose / location / policy_ref | string / string / ComponentRef | 是 | 用途、位置和当前限制 |
| CopyHolder.use_state / cleanup_state / retain_until | allowed/closing/use_stopped / pending/complete/residual/unknown / Time | 是 | 停用与物理删除分开；读取不续期 |
| CopyHolder.last_ack_ref / residual_reason | ObjectRef / string | 否/条件 | 最后原回执；有残留时说明限制 |
| ContentTransfer.transfer_id / kind / command_ref | Id / upload/mirror / ObjectRef | 是 | 原 upload 或 mirror ticket；重复不创建新身份 |
| ContentTransfer.content_ref / source_holder / target_holder | ContentRef / ObjectRef / ObjectRef | 是/条件/是 | 镜像必须有原 holder；上传可无 source |
| ContentTransfer.max_bytes / expires_at / phase | Count / Time / reserved/writing/ready/published/cleanup | 是 | 介质耐久 ready 不等于引用已发布 |
| ContentTransfer.reference_intent_ref / cleanup_job_ref | ObjectRef / ObjectRef | 是/条件 | 元数据交接与孤儿清理责任 |

### 记忆、提取与检索

Memory owner 独立裁决记忆语义。一个 Content 可被多个获准记忆引用；替换记忆增加 revision，不改旧正文。索引只是投影。

| 记录.字段 | 类型 | 必填 | 含义 |
| --- | --- | --- | --- |
| MemoryRecord.memory_id / revision / type | Id / Revision / fact/preference/inference/experience | 是 | 语义记录及类别 |
| MemoryRecord.content_ref / sources / scope_ref / policy_ref | ContentRef / SourceEvidence[] / ContentRef / ComponentRef | 是 | 准确断言、来源、可适用主体/用途范围及保存许可 |
| MemoryRecord.observed_at / valid_from / valid_to | Time | 是/否/否 | 观察时点与事实有效区间，未知不填提取时间 |
| MemoryRecord.confidence / state / recorded_at | number[0,1] / active/needs_review/disabled/deleted / Time | 否/是/是 | 推断置信不是事实保证；deleted 为管理墓碑视图 |
| Extraction.extraction_id / input_refs / extractor_ref / checkpoint_ref | Id / ContentRef[] / ComponentRef / ContentRef | 是/是/是/否 | 有界提取输入、模型或规则、连接器位置 |
| Extraction.limits / deadline / state | Amount[] / Time / active/cancelled/closed | 是 | 总处理上限与自动保存门禁 |
| ExtractionCandidate.candidate_id / revision / extraction_id | Id / Revision / Id | 是 | 候选准确版本；确认不能绑定旧内容 |
| ExtractionCandidate.content_ref / type / sources / scope_ref / policy_ref / expires_at | 同 Memory / Time | 是 | 拟保存的完整语义与许可范围 |
| ExtractionCandidate.state / memory_ref | pending/saved/rejected / ObjectRef | 是/条件 | saved 与唯一 Memory、回执和 Job 共事务 |
| MemoryChange.memory_id / revision / change_seq / kind | Id / Revision / Revision / upsert/restrict/delete | 是 | owner 的提交顺序序列；不是普通数据库 sequence |
| ChangeHead.change_head / registry_version | Revision / Revision | 是 | 连续提交水位与披露 authority 集合版本 |
| Projection.source_ref / strategy_ref / index_generation / purpose | ObjectRef / ComponentRef / Revision / string | 是 | 原版本、分词/向量配置和用途组成完整投影键 |
| Projection.contiguous_watermark / state | Count / building/ready/invalid | 是 | 只推进连续已完成切点；落后报告缺口 |
| QueryView.query_id / subject_ref / query_digest / source_snapshot | Id / ObjectRef / Digest / ContentRef | 是 | 主体、参数、准确候选集合和水位 |
| QueryView.visibility_token / expires_at / cursor / partial / gaps | ContentRef / Time / string / boolean / string[] | 是/是/否/是/是 | 当前披露范围、固定截止与缺口；旧权限恢复不复活游标 |

## 6 授权与费用

Grant、Confirmation、UsageSnapshot、AllocationClosure、BudgetBalance 见核心字典。主体和密钥来自受信身份系统，不得把正文中的用户 ID 当认证。所有权限范围使用已登记 Scope Schema；字符串只表示该固定 schema 的准确资源/用途标识，不得自由文本扩大范围。

### 使用和策略接受

| 记录.字段 | 类型 | 必填 | 含义 |
| --- | --- | --- | --- |
| UseReceipt.use_id / subject_ref / intent_hash / target_ref / target_kind | Id / ObjectRef / Digest / ObjectRef / decision或operation或content_use | 是 | 原使用与准确计费/数据处理责任；模型 Decision 不需虚构工具 Operation |
| UseReceipt.grant_refs / decision / reserved / cost_bound | ObjectRef[] / allowed/denied / Amount[] / Amount[] | 是 | 原许可版本、裁决、数值预留与可信上界 |
| UseReceipt.recipient / location / purposes / issued_at / start_before | string / string / string[] / Time / Time | 是 | 实际出口和有限启动窗口；原回执不可变 |
| UseSettlement.use_id / revision / cumulative_usage / spending_closed / usage_final | Id / Revision / Amount[] / boolean / boolean | 是 | 原使用的结算头；费用为零不恢复 once |
| UseSettlement.source_refs / evidence_refs / remaining_reserved | ObjectRef[] / ContentRef[] / Amount[] | 是 | 原计费源及当前预留，未知不提前释放 |
| PolicyAcceptance.acceptance_id / revision / subject_ref / policy_ref | Id / Revision / ObjectRef / ComponentRef | 是 | 原本人对 estimate 策略的准确接受 |
| PolicyAcceptance.scope_ref / explanation_ref / confirmation_ref / expires_at | ContentRef / ContentRef / ObjectRef / Time | 是 | 能力/任务/单位/预算/预留方法及非硬上限说明 |
| PolicyAcceptance.state | active/revoked | 是 | 只影响后续；原账单继续 |
| GrantLease.lease_id / endpoint_id / instance_id / grant_refs | Id / Id / Id / ObjectRef[] | 是 | 离线额度只给准确端点实例 |
| GrantLease.scope_ref / limits / expires_at / state | ContentRef / Amount[] / Time / open/closed/reconciled | 是 | 不可延长窗口及保守分配；失联不释放 |
| GrantLease.usage_revision / cumulative / closure_ref | Revision / Amount[] / ObjectRef | 是/是/条件 | reconciled 要完整关闭与最终账单证据 |

### 预留、原费用与分配

Reservation 归 Task owner；每原计费源只绑定一个 reservation_id，单位明细按 (reservation_id,unit) 唯一。原费用归实际计费 owner。BillingSource 是身份与累计 UsageSnapshot 的组合，不新增公共结算服务。

| 记录.字段 | 类型 | 必填 | 含义 |
| --- | --- | --- | --- |
| Reservation.reservation_id / task_id | Id / Id | 是 | 一原计费源在本层只对应一个预留头，禁止按单位复制原来源身份 |
| ReservationUnit.reservation_id / unit | Id / string | 是 | 原预留的单位明细；同来源可同时计USD、token等，每单位单独结算 |
| ReservationUnit.original_reserved / remaining_reserved | Decimal | 是 | 原承诺与当前未释放部分 |
| Reservation.source_kind / source_ref / binding_state | brain_decision/execution_operation/grant_use/budget_allocation / ObjectRef / unbound/bound | 是/条件/是 | bound 后不能换源；未绑定仍占用和核对 |
| Reservation.applied_usage_revision / state | Count / open/settled | 是 | 原完整账单已消费修订；所有单位明细同事务归并 |
| ReservationUnit.applied_cumulative | Decimal | 是 | 本单位已记累计值；更正只加原差额 |
| BillingSource.source_id / source_kind / source_owner / task_ref | Id / 同上 / Id / ObjectRef | 是 | 只选择一个实际来源入账；UI 汇总不算源 |
| BillingSource.provider_key / usage / correction_outbox_ref | string / UsageSnapshot / ObjectRef | 条件/是/条件 | 有供应商时固定关联；修订与待交回责任共事务 |
| BillingAdjustment.adjustment_id / original_source_ref / provider_adjustment_key | Id / ObjectRef / string | 是 | (原source,provider key) 唯一 |
| BillingAdjustment.kind / unit / amount / evidence_refs | refund/credit / string / Decimal / ContentRef[] | 是 | 正数退款/贷记；不改 gross_spent、不自动返预算 |
| BillingAdjustment.verified_by / verified_at / state | Id / Time / pending/applied/rejected | 条件/条件/是 | applied 有原可信核验；pending 不改账 |
| Allocation.allocation_id / parent_task_ref / receiver_id / limits / deadline | Id / ObjectRef / Id / Amount[] / Time | 是 | 父方先占 reserved；跨 owner 始终 strict |
| Allocation.command_ref / state / closure_ref | ObjectRef / preparing/open/closing/settled / ObjectRef | 是/是/条件 | settled 要可核原 Closure，TTL 不替代关闭 |
| IncomingAllocation.parent_owner / allocation_id / receiver_id / task_ref | Id / Id / Id / ObjectRef | 是/是/是/条件 | 前三者唯一；本次至多绑定一个子 Task |
| IncomingAllocation.limits / gate / usage_revision / cumulative | Amount[] / open/closing/closed / Revision / Amount[] | 是 | 关闭先于创建也保留 closed 依据；不重新开放 |

## 7 协作和可复用子会话

Delegation 与父目标关系归父 owner；子 Task 和实际效果仍由对应的子任务 owner 保存和裁决。phase 是查询投影，不由外部“成功”消息直接置 closed。

| 记录.字段 | 类型 | 必填 | 含义 |
| --- | --- | --- | --- |
| Delegation.delegation_id / parent_task_ref / parent_goal_revision | Id / ObjectRef / Revision | 是 | 稳定交接与准确父目标 |
| Delegation.goal_ref / input_refs / agent_binding_ref / ancestor_task_refs | ContentRef / ContentRef[] / ObjectRef / ObjectRef[] | 是 | 有界子目标、准确配置、可核真实祖先 |
| Delegation.creation_key / command_ref / allocation_ref / permission_refs / deadline | string / ObjectRef / ObjectRef / ObjectRef[] / Time | 是 | 首发前固定；未知不换子 owner 或额度 |
| Delegation.child_task_ref / phase / closure_ref | ObjectRef / preparing/active/reconciling/closed / ObjectRef | 否/是/条件 | 唯一远端映射；closed 需原完整 Closure |
| DelegationClosure.delegation_id / revision / goal_work_closed / effects_closed | Id / Revision / boolean / boolean | 是 | 目标工作及全部受管后代效果分别封闭 |
| DelegationClosure.allocation_closure_ref / transfers_closed / proof_refs / closed_at | ObjectRef / boolean / ContentRef[] / Time | 是 | 含账务及输入/控制交接全部收束 |
| InputControlTransfer.transfer_id / delegation_id / kind / command_ref | Id / Id / input或control / ObjectRef | 是 | 原交接种类和命令；控制不需要伪造InputRequest |
| InputControlTransfer.request_ref / goal_revision / control_revision | ObjectRef / Revision / Revision | 条件 | input必需原请求及目标版本；control必需对应原控制版本 |
| InputControlTransfer.state / receipt_ref | queued/sending/applied/rejected/closed / ObjectRef | 是/条件 | sending 未知继续核对；applied 固定原回执 |
| ChildHandle.child_id / child_session_ref / agent_binding_ref / install_lock_ref | Id / ObjectRef / ObjectRef / ComponentRef | 是/条件/是/是 | 复用会话和配置，不复用旧 Task/once |
| ChildHandle.revision / access_scope_ref / state / active_delegation_ref | Revision / ContentRef / preparing/open/closed / ObjectRef | 是/是/是/否 | 默认一个活动目标，CAS 裁决 |
| ChildHandle.session_command_ref | ObjectRef | 是 | 创建时固定原Session命令；原接纳后才open，未知不另建会话 |
| Amendment.amendment_id / delegation_id / parent_goal_revision / child_goal_revision | Id / Id / Revision / Revision | 是 | 原有界目标内的准确补充 |
| Amendment.source_submission_ref / content_ref / command_ref | ObjectRef / ContentRef / ObjectRef | 是 | 不可变来源和原发送命令；扩大范围必须新目标 |

## 8 能力、安装与发布

以下记录归准确组件/部署治理 owner。ComponentRef 表示不可变声明；Binding/Activation/Readiness 是可变部署事实。名称相同不表示版本或权限相同。

| 记录.字段 | 类型 | 必填 | 含义 |
| --- | --- | --- | --- |
| Capability.component_ref / input_schema_ref / output_schema_ref | ComponentRef | 是 | 精确语义与闭合结构 |
| Capability.effect_class / effect_rule_ref / evidence_rule_ref | read_only/target_idempotent/no_idempotency_guarantee / ComponentRef / ComponentRef | 是 | 重复语义和能证明什么 |
| Capability.resource_schema_ref / purposes / limits / cost_bound_ref / error_schema_ref | ComponentRef / string[] / Amount[] / ComponentRef / ComponentRef | 是 | 规范资源、请求/费用上界和错误解释 |
| Binding.binding_id / revision / capability_ref / implementation_ref | Id / Revision / ComponentRef / ComponentRef | 是 | 语义到实际实现的准确绑定 |
| Binding.endpoint_ref / resource_refs / config_ref / install_lock_ref / readiness_ref | ObjectRef / ObjectRef[] / ComponentRef / ComponentRef / ObjectRef | 是 | 当前可调用位置、资源、配置和实例资格 |
| InstallLock.component_ref / artifacts / dependency_refs | ComponentRef / ContentRef[] / ComponentRef[] | 是 | 递归闭合代码/配置与摘要 |
| InstallLock.platform_ref / abi / protocol_profile / formats_ref / migration_refs / isolation_refs | ComponentRef / string / string / ContentRef / ComponentRef[] / ContentRef[] | 是 | 平台、读写格式、迁移与信任证据 |
| Skill.component_ref / body_ref / usage_contract_ref | ComponentRef / ContentRef / ContentRef | 是 | 目的、前提、反例、依赖、冲突和退出规则；不授予 Grant |
| AgentConfig.component_ref / brain_ref / capability_refs / control_limits_ref | ComponentRef / ComponentRef / ComponentRef[] / ContentRef | 是 | 允许范围上限，与当前用户许可取交集 |
| BindingHead.target_id / revision / enabled / generation / current_activation_ref / binding_ref | Id / Revision / boolean / Count / ObjectRef / ObjectRef | 是/是/是/是/条件/条件 | 稳定部署槽的唯一CAS头；空槽generation=0，没有当前激活 |
| Activation.activation_id / install_lock_ref / target_scope_ref / generation / revision | Id / ComponentRef / ContentRef / Revision / Revision | 是 | 一次激活与当前代际；历史激活不覆写 |
| Activation.state / readiness_refs / residual_refs | preparing/active/deactivated/disposed / ObjectRef[] / ObjectRef[] | 是 | 真实就绪与残留分开，集合可分页 |
| InstanceReadiness.instance_id / generation / config_digest / artifact_digest | Id / Revision / Digest / Digest | 是 | 本次随机实例，不继承旧实例成功 |
| InstanceReadiness.self_test_ref / approval_ref / expires_at / state | ContentRef / ObjectRef / Time / ready/not_ready | 是 | 核对自检、当前批准与期限后才能 ready |
| ReleaseApproval.approval_id / revision / approver_ref / install_lock_ref / purpose | Id / Revision / ObjectRef / ComponentRef / compatibility/improvement | 是 | 受信批准者对准确代码的决定 |
| ReleaseApproval.scope_ref / evidence_refs / rollout_ref / expires_at / state | ContentRef / ContentRef[] / ContentRef / Time / active/revoked | 是 | 有限 targets/batches/最小观察窗/stop rules，不静默扩批 |
| ReleaseApproval.rollback_install_lock_ref / rollback_approval_ref | ComponentRef / ObjectRef | 条件 | 启用自动回退时固定准确旧版及独立批准；回退时仍核当前有效性与格式兼容 |
| ApprovalUse.use_id / approval_ref / target_ref / issued_at / start_before | Id / ObjectRef / ObjectRef / Time / Time | 是 | 原有限启动窗口，不是证据当前无缺陷证明 |
| InstallHolder.holder_ref / install_lock_ref / state | ObjectRef / ComponentRef / registered/closing/released | 是 | 与准入共同登记；完整集合供卸载核验 |
| Migration.target_id / from_format / to_format | Id / string / string | 是 | 迁移绑定准确部署槽及原读写格式 |
| Migration.migration_id / artifact_digest / phase / checkpoint_ref | Id / Digest / expand/backfill/switch/observe/contract / ContentRef | 是/是/是/否 | 有界迁移；删除旧格式前核旧实例与未结责任 |

## 9 证据治理与实验

ConditionCheck 是完整检查记录；ConditionResult 是原判断加当前适用性的读视图。它们使用同一个 check_id，不是两套互相竞争的判定。GoalCoverage 使用同一资格治理。

| 记录.字段 | 类型 | 必填 | 含义 |
| --- | --- | --- | --- |
| ConditionCheck.check_id / task_id / goal_revision / requirement_id / requirement_revision | Id / Id / Revision / Id / Revision | 是 | 准确目标与条件版本 |
| ConditionCheck.artifact_ref / scope_ref / observed_at / checked_at | ContentRef / ContentRef / Time / Time | 是/是/条件/是 | 准确成果、资源/覆盖/时点；观察未知时省略，usable pass 必填 |
| ConditionCheck.rule_ref / evaluator_ref / config_ref / install_lock_ref | ComponentRef | 是 | 规则与执行实现不混同 |
| ConditionCheck.operation_ref / dependent_check_refs | ObjectRef / ObjectRef[] | 条件/是 | 外部/模型评估有原获准 Operation；组合检查保留 DAG |
| ConditionCheck.verdict / basis / report_ref / evidence_refs | pass/fail/unknown / verified/assessed/user_accepted / ContentRef / ContentRef[] | 是 | 不可变原判断；必要效果不得 user_accepted |
| Applicability.check_id / revision / state / gate_refs / reason_ref | Id / Revision / usable/unknown/inapplicable / ObjectRef[] / ContentRef | 是 | 当前资格投影；变化不改原 verdict |
| EvidenceGate.implementation_ref / gate_revision / authority_epoch / imported_cursor | ComponentRef / Revision / Revision / string | 是/是/是/条件 | 消费方所属库中的完成门禁；跨域游标不得跳缺口 |
| Defect.defect_id / rule_ref / evaluator_ref / scope_ref / evidence_ref / registered_at | Id / ComponentRef / ComponentRef / ContentRef / ContentRef / Time | 是 | 受信确认的缺陷及命中范围；普通退役不是缺陷 |
| EvidenceHolder.holder_id / consumer_task_ref / check_ref / report_hash / dependency_digest / scope_ref | Id / ObjectRef / ObjectRef / Digest / Digest / ContentRef | 是 | 资格authority与检查同事务登记的持续交回责任 |
| EvidenceHolder.authority_epoch / registration_cursor / last_acked_cursor / state | Revision / string / string / active或closed | 是 | 原登记切点与连续交回确认；凭据到期不删除Result缺陷责任 |
| EligibilityReceipt.receipt_id / check_ref / consumer_ref / request_digest | Id / ObjectRef / ObjectRef / Digest | 是 | 原消费方和准确检查请求 |
| EligibilityReceipt.rule_ref / evaluator_ref / report_ref / dependency_digest | ComponentRef / ComponentRef / ContentRef / Digest | 是 | 完整准确依赖 |
| EligibilityReceipt.verdict / authority_epoch / defect_revision / cursor | eligible/ineligible/unknown / Revision / Revision / string | 是 | 当前原 authority 的声明与后续交回切点 |
| EligibilityReceipt.checked_at / issued_at / expires_at / proof_ref | Time / Time / Time / ContentRef | 是 | 查询原回执不续期；远端不能提供零陈旧原子保证 |
| ImprovementPolicy.policy_ref / lineage_id / formal_attempt_limit / stopping_rule_ref | ComponentRef / Id / Count / ComponentRef | 是 | 候选谱系与永久正式次数上限 |
| EvaluationPlan.plan_id / manifest_ref / candidate_ref / baseline_ref | Id / ContentRef / ComponentRef / ComponentRef | 是 | 冻结样本全集、两臂 InstallLock |
| EvaluationPlan.partition_ref / source_group / seed / metrics_ref / budget / attempt_policy_ref | ObjectRef / Id / Count / ComponentRef / Amount[] / ComponentRef | 是 | 分区、来源血缘、统计/阈值/停止规则与资源 |
| EvaluationPlan.observation_cutoff / unknown_outcome_policy / cost_unknown_policy | Time / fail_at_cutoff / bounded_worst_case | 是 | 封存前固定时点与未知归类；没有可信最坏费用界时成本gate为unknown |
| EvaluationPlan.release_request_id / formal_attempt_index / purpose | Id / Revision / formal/exploratory/conformance | 条件/条件/是 | formal 永久占用原发布请求和 holdout，不返还次数 |
| EvaluationRun.run_id / plan_id / revision / state | Id / Id / Revision / prepared/running/cancelling/sealed | 是 | 每 Plan 一个逻辑运行；取消先封新启动 |
| EvaluationRun.samples / environments / report_ref | CollectionSummary / CollectionSummary / ContentRef | 是/是/条件 | 完整索引对应查询摘要；sealed 有原报告 |
| SampleRun.sample_run_id / plan_id / sample_id / arm | Id / Id / Id / candidate或baseline | 是 | (plan,sample,arm) 唯一，尝试不改分母 |
| SampleRun.environment_key / operation_refs / outcome / usage_refs | string / ObjectRef[] / pass/fail/timeout/unknown/not_run / ObjectRef[] | 是 | 环境与真实执行/成本；not_run 仍在分母 |
| Exposure.exposure_id / source_group / actor_ref / kind / occurred_at / recorded_at | Id / Id / ObjectRef / feedback/debug/leak / Time / Time | 是/是/是/是/否/是 | 真实曝光及登记时点；时点不明须明确 unknown 范围 |
| EvaluationReport.report_ref / plan_id / full_denominator / outcomes_ref / cost_ref | ContentRef / Id / Count / ContentRef / ContentRef | 是 | 全样本/所有尝试/缺失与成本，不只成功样本 |
| EvaluationReport.target_attainment / statistical_gate / improvement_gate / sealed_at | pass/fail/unknown / 同左 / 同左 / Time | 是 | 三类结论独立；封存后读取登记 Exposure |

## 10 持久工作、传输和生产目录

Command、Job、Claim、CollectionSummary 见核心字典。表中传输记录仅在对应责任需要耐久时保存。socket 帧、指标标签和普通日志不成为恢复真相。

| 记录.字段 | 类型 | 必填 | 含义 |
| --- | --- | --- | --- |
| CommandReceipt.command_id / principal_ref / request_digest / stage | Id / ObjectRef / Digest / accepted/applied/rejected | 是 | 原受信主体及完整请求摘要；决定不可漂移 |
| CommandReceipt.accepted_at / decided_at / output_ref / error_ref | Time / Time / ContentRef / ContentRef | 条件 | 按阶段必填时点；applied输出与rejected错误互斥 |
| CommandTombstone.command_id / request_digest / principal_binding / decision / expires_at | Id / Digest / Digest / applied或rejected / Time | 是 | 最小身份；gone不表示从未存在，原截止不延长 |
| Outbox.delivery_key / source_ref / destination_ref / command_ref / phase | string / ObjectRef / ObjectRef / ObjectRef / pending/sending/acknowledged | 是 | 领域修订与交回责任共事务；可由领域行+Job实现，无通用消息系统 |
| Delivery.delivery_id / sender_service_id / recipient_endpoint / recipient_instance | Id | 是 | 原目标及当前配对实例 |
| Delivery.kind / request_ref / request_digest / deliver_before / proof_ref | command/query/receipt_lookup / ContentRef / Digest / Time / ContentRef | 是 | 原请求和受信发送证明，网络seq不替代身份 |
| Reply.delivery_id / request_digest / result_ref | Id / Digest / ContentRef | 是 | 原目标回执或查询结果；持久到匹配Ack |
| ReplyAck.delivery_id / request_digest / stored | Id / Digest / boolean | 是 | 本方已耐久接收，才可释放原回复交回责任 |
| Query.query_id / method / target_ref / parameters_digest / subject_ref | Id / string / ObjectRef / Digest / ObjectRef | 是 | 只读查询身份在有限保留期绑定参数与主体 |
| Cursor.snapshot_ref / last_key / permission_vector_ref / expires_at | ObjectRef / string / ContentRef / Time | 是 | 准确集合、稳定排序位置和不可延长期限；仅服务端验证的opaque token |
| SourceDirectory.subject_ref / revision / sources / authority_refs | ObjectRef / Revision / Id[] / ObjectRef[] | 是 | 所有可披露来源完整登记；新增先登记后开放 |
| AuthorityRegistry.registry_version / authority_refs | Revision / ObjectRef[] | 是 | 可影响可见性的完整权威集合；不能只记当前命中来源 |
| OwnerPlacement.owner_id / placement_revision / shard_id / state | Id / Revision / string / preparing/active/fenced | 是 | 逻辑 owner 到物理库；不改变 Task owner |
| EndpointPairing.pairing_id / challenge_digest / expires_at / state | Id / Digest / Time / pending/claimed/revoked | 是 | 短期单用途配对；不持久保存明文秘密 |
| EndpointIdentity.endpoint_id / subject_ref / key_ref / credential_generation / state | Id / ObjectRef / ObjectRef / Revision / active/revoked | 是 | 已批准的设备与密钥引用；秘密由凭据系统保管 |
| ConnectionBinding.connection_id / binding_id / binding_revision / endpoint_ref / owner_id | Id / Id / Revision / ObjectRef / Id | 是 | 内外连接和原逻辑服务分开；旧流不覆盖新代次 |
| ConnectionQuota.scope_ref / limit / reserved / holder_refs | ObjectRef / Count / Count / ObjectRef[] | 是 | 身份分片统一额度，不能按进程复制 |

默认云端 PG 保存 Task 权威，端侧设备 SQLite 保存本机执行、门禁、恢复和补传账本。两方只写各自负责的事实；端侧 Task 缓存不得变成可写权威。端侧自有 Task 仅属于显式启用的独立 Orchestrator 部署。上述身份/目录/连接记录只保存必要治理事实，身份供应商和凭据库继续负责认证秘密。
