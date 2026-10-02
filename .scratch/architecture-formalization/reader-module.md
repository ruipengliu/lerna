# 正式架构冷读：模块实现者

本报告以熟悉接口、数据库事务和后台任务、但不预先理解项目术语的实现工程师为读者。冷读阶段只读取 `docs/architecture/` 的正式入口、领域正文及同版契约资产，没有读取 `.draft`、`docs/research/`、其他 `.scratch` 内容或此前写作记录。第一阶段保留根代理修正前的读后理解与问题定位，所列行号对应冷读时正文；末节记录修订后的定向复核与当前状态。

## 第一阶段：从正式文档推导的实现理解

### 对象怎样建立

1. 应用先固定准确目标正文、目标逻辑服务、完整 `task.submit` 命令以及会话消息关联／交付责任，再发送。原 Orchestrator 的接纳事务建立 Task、原目标与预算、首个 decide Job 和 applied Receipt。答复丢失仍查原服务／原命令；Session 关联可以补齐，不再创建 Task。Task 的逻辑 Orchestrator 不随实例接替而改变。
2. Orchestrator 从当前 Task、完整要求、控制、未结意图、可信事实及获准材料组成 Snapshot。它固定 `decision_id`、配置、准确输入、费用预留和派发责任；Brain 接纳后另在自己的事务范围保存 Decision、输入绑定、回执和推进 Job。Decision 是一次固定输入判断，可以没有 ModelCall，也可以有至多一次物理模型请求。
3. Brain.completed 只证明准确 Proposal 已保存。TaskCoordinator 至多一次消费它，并在当前目标／控制／授权／预算门禁下准入行动。Orchestrator 先保存不可变 OperationIntent、原 Invoke／命令、预留、dispatch Job 和执行端绑定；Executor 接纳后保存同一 `operation_id` 的 Operation、回执及工作。允许的物理发送各有 Attempt；Operation 的接纳、发送、目标效果与账务并非一个状态。

依据：`README.md:9–15,85–98`；`orchestrator/implementation.md:247–311`；`brain/implementation.md:98–119,281–311`；`execution/implementation.md:154–204`。

### 正常推进

报告任务的链是接纳目标 → 提出搜索 → 逐项准入搜索／获取 → 保存准确正文与来源 → 新快照综合准确候选 → 评估该候选 → 独立准入写入 → 根据原写入事实再准入读回 → 核验完整目标覆盖、必要条件和外部效果 → 固定 Result 与 succeeded。搜索命中不等于取得正文；质量通过不等于文件保存；写入回执不等于独立读回。评估 Operation 的 effect=applied 也可以对应条件 verdict=fail。

同一提案确实补全／改变 Requirement 时，先保存新 goal_revision、control_revision 和 Task 修订，消费原 Decision，废弃同提案行动、完成建议和 plan_delta，再基于新快照继续。无条件变化才处理原 kind。少量独立行动可以同轮准入；依赖输出或共享效果的行动需要后续轮次或有限计划逐步实例化。

依据：`walkthrough.md:48–93`；`orchestrator/task-lifecycle.md:111–141`；`orchestrator/implementation.md:285–315`。

### 谁保存哪一种事实

| 事实 | 唯一负责方及本端关联 |
| --- | --- |
| Task 目标、条件、准入、控制、预算归并及最终结果 | 固定逻辑 Orchestrator；保存 Snapshot、DecisionConsumption、OperationIntent、ConditionCheck、GoalCoverage、Result、原事实投影及 Jobs |
| 固定输入判断、实际模型调用、输出与模型费用 | Brain；原 Decision/ModelCall 可查询，Proposal 由 Orchestrator 另行消费 |
| 原操作接纳、Attempt、实际目标效果及执行费用 | Executor；实际资源入口和目标证据另保留自己的责任 |
| 准确字节、来源闭包与当前内容可用性 | 原 Content owner；引用固定 owner/id/version/hash，每次使用仍检查当前用途 |
| Grant 的当前许可、原使用消费与结算 | 原 Grant owner；Task 不创建第二笔相同物理收费 |
| 呈现／回答转交与实际回答消费 | Interaction 保存呈现、InputSubmission 与转交；实际业务 owner 创建 InputRequest，并与业务变化同事务一次消费 |
| 父子交接与子 Task 状态 | Collaboration 保存 Delegation／唯一映射／Closure；子 Task 仍由它的 Orchestrator 裁决 |

模块、owner、进程、数据库事务范围互不等同。同进程和同数据库本身不提供共同提交，只有受信装配明确的共同 Tx 参加者可以原子裁决；独立 owner 各自保存原命令、接纳与恢复责任。

依据：`core-data-model.md:21–35,38–72,144–154`；`technical-overview.md:37–67`；`contracts/README.md:11–17,79–97`。

### 记录、键和事务

- 命令按 `(tenant_id, logical_service_id, command_id)` 去重，固定 method、target、expected_revision、expires_at、payload 的规范摘要；同键异输入拒绝，同键原样恢复返回原决定，换逻辑服务不去重。expires_at 限首次接纳，不限已经接纳的业务处理。
- Task 自己管理不可变目标／条件历史、快照、决策消费与当前核验选择。原事实按 `owner/object_id/revision` 去重；同修订异摘要进入冲突，不以接收时间覆盖。
- OperationIntent 的准入来源三选一：DecisionConsumption、准确计划版本的 StepAdmission、受信检查身份。来源消费、固定意图、预留、派发责任共同提交。原意图／命令在重派时不从 Task 最新字段重建。
- 每次裁决把业务事实、回执和下一责任共同提交。内容字节先耐久再发布准确引用；网络、模型、工具、用户等待均在短事务之外。CommitUnknown 先查原命令／阶段，不能释放未知预留或重跑外部动作。
- 锁序先原命令、根到叶祖先／领域行、预算与门禁，最后稳定顺序的 Job 行；受领取保护的事实和 Finish 在同一个短事务提交。失效领取使整笔受保护事务回滚；真实迟到事实可经独立受信核验入口归并，但不能借它改提案或重新发送。

依据：`contracts/README.md:49–77`；`orchestrator/records.md:32–69`；`orchestrator/implementation.md:237–244,354–365`；`reliable-work.md:69–103,198–245`。

### work_revision 与 lease_epoch

`work_revision` 标识原 Job 上新保存的领域责任；`lease_epoch` 标识一次新的领取者代次。Raise 与新来源事实共同提交，增加前者；领取增加后者，续约不改变两者，通知／重复事实／到期本身不增加责任版本。Claim 固定 `observed_work_revision`，回写时不能刷新为最新值。

有新责任时不抢走有效领取。旧 worker 仍有有效领取但观察版本落后，可以保存仍合法的原事实，Finish 只能释放为 ready 并保留更早到期，不能 done 或旧退避覆盖新责任。若租约、holder、epoch 已失效，整个受保护事务拒绝。Job done 以后可信迟到账单等新责任可以重开同一 Job，而 Task 终态保持不变。

依据：`reliable-work.md:142–177,179–230`。

### prepared 与 send_started 的恢复差别

Brain 的 prepared 与 send_started 是两个提交点。只有原 ModelCall 已准备、没有 send_started，恢复者才能在复查准确输入、当前用途、使用窗口、配置和领取后，继续原首次发送。send_started 表示可能发送而非供应商已收到；存在该记录时只查询原调用，不透明重发。无法查询就固定 provider_result_unknown 并保留费用；再试必须由 Orchestrator 按新 Decision 准入，继续受累计限制。

Executor 不能沿用这个推断：Attempt 准备后若无法证明未交给发送入口，就按可能发送核对。实际入口串行复核 TaskGate、资源 epoch／lease／Observation 及使用窗口，并耐久登记在途责任。原发送者失去 Job 领取不等于外部目标已隔离；重启或换 worker 不提供重试权。安全重放仍取决于准确 Capability 的重复／查询合同。

依据：`orchestrator/durable-work.md:70–84`；`brain/implementation.md:289–304`；`execution/implementation.md:186–255`。

### 终态、副作用、暂停与预算

Task 的 active/succeeded/failed/cancelled、自己的 running/paused、wait_reasons、open_effects 与 accounting_open 分维度保存。暂停停止新决策、评估和行动，原核对／费用继续；既有证据充分仍可成功。取消与成功争用原 Task 事务，先提交的终态固定；迟到执行事实不重开目标。成功 Result 固定目标、成果和所选证据；后续费用更正／判断缺陷只追加关联说明与收尾责任。

严格预算按可信单次上界预留，未知费用继续占预留，可信累计费用只应用新增差额。费用超出声明上界也照实入账并停止新计费，不截断账单。估算只在本人已接受准确策略、费用 Grant owner 与 Task 共受信本地事务范围、没有 allocation 的直接调用启用。一次许可的消费、数值费用与释放余量不同；零费用不返还 once 资格。终态后 Brain／Executor／接收方保存可信上调和 outbox，父方 JobAck 只确认核对责任耐久，账务工作再主动读取原源账结算。

依据：`orchestrator/task-lifecycle.md:5–46`；`orchestrator/verification.md:100–128`；`orchestrator/budget.md:8–39,73–102`；`contracts/protocol.md:120–124`。

### 子任务与委派

内部委派由同一 Orchestrator 在共同事务建立 Delegation、child Task、allocation、唯一映射及首 Job；远端 Brain／Executor 不改变子 Task 归属。子自己的 control 与祖先有效控制分开，父恢复不清子暂停。父取消和子创建按共同事务先后裁决；父目标修订关闭旧目标下活动子树，原效果与账务仍收尾。

外部委派先固定创建键、配置、权限收缩、allocation 和发送责任，沿原键建立唯一 remote_task_id，不以另一个成功任务替换未知创建。创建、目标封闭、效果核清、费用封账分别确认。父成功需要内部子目标终结、外部后续目标行动封闭和副作用核清；仅费用未结可以保守预留继续。Delegation phase 是同一已归并修订的只读投影；完整 Closure 还要求最终账单、预算结算及没有还能产生目标行动的输入／控制转交，closed 不以子 success 或零费用代替。

依据：`collaboration/README.md:16–81,96–123`；`collaboration/implementation.md:75–178,274–335`；`orchestrator/task-lifecycle.md:383–423`。

## 第一阶段：确认的问题

### MOD-01 [P2] 无模板步骤允许展开，但没有把展开结果收束到唯一的计划步骤准入

**定位**：`docs/architecture/brain/implementation.md:448`；`docs/architecture/orchestrator/implementation.md:323–327`；`docs/architecture/orchestrator/implementation.md:308–311`；`docs/architecture/contracts/schemas/protocol.schema.json:11870–11895`。

**正文所允许的输入**：BrainPlanStep 的 action_template 可选，正文明确要求没有模板的步骤交后续 Brain 展开。考虑 A 只有 instruction，B depends_on A，并用 step_output(A) 绑定参数。这个计划合法且有可交 Brain 展开的起始节点。

**实现阻塞**：后续 Brain 返回直接 act 执行 A 时，既有消费规则只按 decision_id 保存 DecisionConsumption；只有 PlanMaterializer 生成的候选按计划版本／step_id 保存 plan_step_admissions。A 没有映射，B 的依赖和 step_output 不能解析。把同一次行动再登记为原 step 的消费会碰到准入来源三选一规则，也会引入重复准入风险。让实现者根据 instruction 猜关联不足以保证冷恢复和并发唯一性。正式正文没有规定“展开只能先修订计划”，也没有定义直接 act 的另一条合法步骤完成路径。

**最小修正**：沿现有 plan_delta 收束，不增加公共字段或双来源。Orchestrator 在原快照／Decision 的内部恢复关联固定准确 plan_ref、选中待展开 step 和原因；该展开 Decision 的 act 只能是 actions=[] 加同 plan_id 下一 revision 的完整 plan_delta，为未准入说明补准确模板／绑定，再由新版本按原 StepAdmission 规则准入。已准入旧步骤保持原 operation/delegation 收尾；新计划复用旧输出明确使用 operation_output，不能跨 revision 偷用 step_output。need_context/request_input/fail 按现有 Task 规则处理且不标原步骤已执行；complete 若允许，也只能按原 Task 最终核验，不成为步骤执行事实。消费事务比较准确 plan 基线及当前门禁，旧展开响应失效。

**影响范围**：可选有限计划中的 instruction-only 正常链及冷恢复；全部已有模板的实例化路径与默认 ReAct 无此缺口。

### MOD-02 [P2] 只读结果 unknown 与 Task 的未知副作用门禁没有统一

**定位**：`docs/architecture/execution/README.md:125,142–148`；`docs/architecture/execution/implementation.md:348`；`docs/architecture/orchestrator/implementation.md:102`；`docs/architecture/orchestrator/verification.md:103,121`。

**正文冲突**：执行正文将 read_only 定义为无目标业务副作用，并明确未知读取可以重取或选择另一来源；文件读取变化无法稳定判定时也可返回 unknown。但 Orchestrator 从全部已准入意图建立 open_effects，仅在核清／封闭后移出，最终完成无条件检查“无未知或仍可能迟到的效果”，没有引用 effect_class 区分未知读取结果与未知业务写副作用。

**实现阻塞**：非必要只读取证 A 的结果永久无法核清，替代来源 B 已提供适用证据、全部必要条件已通过且其他副作用已核清时，按最终门禁字面实现仍可能让 A 阻止成功。若实现者自行忽略全部 read-like 方法，又会把未验证无副作用保证的 GET／咨询能力当成安全读取。两种实现会在相同合同下产生不同 Task 终态。

**最小修正**：定义 open_effects 与最终副作用门禁的集合按原准入固定、可核验的准确 Capability.effect_class 筛选；只有可信 read_only 声明确认整个有界操作无目标业务副作用时，未知读取结果不进入业务副作用集合。未知读结果保持原 Operation unknown、资料缺口和必要条件缺证，未结费用／内容收尾仍保留；其他适用证据真正满足全部必要条件后，可以提交 Task 成功。Requirement 绑定原读取身份时不得自动偷换为替代结果。无法核验准确声明的操作按可能副作用处理，不按名称／HTTP 方法推断。内部子任务终结、外部目标封闭及委派 Closure 的原要求仍保留；Task 终态仍保存控制与收尾责任。

**影响范围**：读取失败后换来源、冗余取证、候选来源弃用等普通成功路径；未知业务写副作用仍应阻止成功，不修改 Operation 的效果枚举，也不删除原核对责任。

## 审查范围与结论

正常 Task／Decision／Operation 主线、对象归属、原命令恢复、工作版本／领取代次、模型发送屏障、终态账务和父子控制可以从正式文档独立重建；未发现需要依赖旧稿才能解释这些基本链路的问题。确认两项 P2 行为缺口，均有沿现有记录和事务的局部修正路径，未确认其他 P1/P0。

本轮重点阅读四篇入口及 orchestrator、brain、execution、collaboration 的相关 README／实现／生命周期／预算／验证／恢复正文、reliable-work 和共同契约／Schema 的相关定义。没有开展第二阶段原材料核验，没有运行数据库、模型、设备或生产故障实验，也不把静态资产当成这些保证的运行证据。没有重复登记正在修正的 APP01 命令编码、APP02 输入发现或 APP03 固定验收命令；`input-answer/1` 新 Schema 待根同步，不作为本轮已知缺口。

## 修订后定向复核

### MOD-01：resolved

当前正式正文已将 instruction-only 的展开限制为既有 `plan_delta` 路径。Snapshot／原 Decision 的内部关联固定准确 plan_ref、step_id 与展开原因；act 必须为 actions=[] 加同 plan_id 下一版完整计划，消费事务重新比较原基线和当前门禁，只安装计划及保存后续 decide 责任。新版本按自己的 StepAdmission 键准入，直接 actions 和其他提案分支不能制造旧步骤完成记录。这为 A 只有说明、B 依赖 A 的正常链提供了唯一恢复路径，没有新增公共字段或双准入来源。

已准入旧步骤继续原身份收尾，新计划不跨 revision 复用 step_output。旧 Operation 输出走明确 operation_output；旧 Delegation 的准确 result_ref 则作为当前获准材料进入新快照，由 Brain 固定所需准确字面值／ContentRef 和完整来源，不能把 delegation_id 当 operation_id。这个区分符合现有两种 OutputSource Schema，也不要求增设公共委派输出类型。

依据：`docs/architecture/orchestrator/implementation.md:81,325–356`；`docs/architecture/brain/implementation.md:448–449`；`docs/architecture/orchestrator/task-lifecycle.md:257–259`。修订后的上述正文相互一致；初始问题已关闭。

静态验证：`PYTHONDONTWRITEBYTECODE=1 python3 docs/architecture/validation/validate_brain.py` 通过。新增展开／依赖向量覆盖完整后继计划接纳、直接行动拒绝、旧基线拒绝、不存在／已有模板步骤拒绝、跨计划修订前项拒绝，以及本版前项输出解析。这里只证明固定静态规则与样本，未执行服务或故障实验。

### MOD-02：resolved

当前 open_effects 和最终效果门禁统一限定为可能业务副作用。只有原准入固定、可核验的准确 Capability.effect_class=read_only 可排除；未核声明和名称猜测仍保守处理。未知只读结果保留原 Operation unknown、资料缺口、费用、停止和内容收尾责任；必要条件依赖它时仍缺证，其他适用证据完整满足要求后才允许成功，准确原读取身份不能被替代来源偷换。内部子任务终结与外部委派目标封闭的原规则也明确保留。

依据：`docs/architecture/orchestrator/implementation.md:102–104`；`docs/architecture/orchestrator/task-lifecycle.md:10`；`docs/architecture/orchestrator/verification.md:103,121`；`docs/architecture/execution/README.md:125`。执行侧 unknown 与任务侧副作用门禁现在有相同边界；初始问题已关闭。

定向检查中另发现、已由根修复一个机器规则问题：被拒绝的同 operation_id 只读 Invoke 曾能覆盖原未知写入的效果分类。当前 `docs/architecture/validation/protocol/traces.py:94–107` 只在成功首次接纳时固定原 task／orchestrator／准确 capability_ref 与 binding_ref，后续冲突不得改写分类。实证反例现返回 result_effect，拒绝成功；新增只读能力也使用独立 binding，符合原 Binding 的固定能力关联。该检查不把 fixture 的能力声明视为现实受信注册验证。

只读定向向量的反例构造也已修正并通过：`docs/architecture/validation/validate_protocol.py:68–72` 原先清空 condition_results，会被 Schema 的非空要求先拒绝；当前保留合法非空结果而换为另一合法 requirement_id，遗漏原必要条件，真正触及 result_conditions 门禁。定向运行从该文件提取只读段并对正式 `16-active-to-result.json` 调用 check_trace，全部断言通过：冗余准确只读 unknown 不阻止完整证明的成功；声明缺失及两种可能写效果分类均阻止成功；缺少原必要条件仍阻止成功；拒绝的重分类不改变原写入责任。该段运行不依赖仍待同步的输入回答 fixture。

### 当前范围

两项正式正文问题均 resolved，定向复核未确认新的正文阻塞。全过程仍未读取旧稿／研究原材料；后续只检查了相应正式正文与机器静态规则。协议全套的最终验收由根在 `input-answer/1` 与关联 fixture 同步后执行，本报告不把该待同步状态当成本轮新增正文问题，也不声明数据库、外部发送屏障或生产恢复已经得到运行证明。
