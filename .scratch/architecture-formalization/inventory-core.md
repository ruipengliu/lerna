# 核心执行链资料清单

本清单为正式架构正文的编写准备，范围是核心数据模型、Orchestrator、Brain、Executor、Collaboration，以及五个参考 Agent 的相关研究。它记录应迁入正文的已接受设计及其原文位置，不改变架构、不发布新协议，也不把实现验收规格当成已经通过的实验。

已检查本目录的 `fragments.md` 和 `source-manifest.json`。后者保留全量文件与摘要；本清单补充行为、数据、共同提交范围和恢复责任之间的对应关系。正文组织及读者方案由总体编写任务确认。

## 1. 判断设计是否已接受

优先级为最新 [CONTEXT] 与 [ADR]，其次是当前正式正文中的已接受约束、`.draft` 对该约束的具体展开，再其次是研究中的借鉴建议。研究的“可借鉴”“候选”“按需启用”不能仅因资料完整而升级为默认行为。文档中的逻辑记录不是逐项建表要求，软件模块不是逐项拆服务要求；这些区别在 [CORE] §1、§9 和 [OVERVIEW] §2 已经明确。

本范围直接相关的既有决定如下。

| 依据 | 必须保留的决定 |
| --- | --- |
| [ADR-0001] | 命令去重、取消和终态所需最小身份长期保留；完整正文可以按策略清理，旧身份不能重新创建责任。 |
| [ADR-0002]、[ADR-0003] | Go、WSS/gRPC；生产按接入、应用、工作池及持久权威装配，采用 PostgreSQL 和对象存储。开发可以单体，SQLite 与 PostgreSQL 的 SQL 适配分别实现；不能把生产部署复杂度强加给最小开发装配。 |
| [ADR-0004] | Task 与原命令固定逻辑负责服务。进程扩缩容和发现刷新不移动业务权威，也不重绑原操作。 |
| [ADR-0005] | 机械检查、普通评估和高保证评估分级；停止新评估与已有证据失效分开。可信缺陷影响当前可用性，已终态 Task 追加说明而不改写原 Result。 |
| [ADR-0006] | 条件解释先裁决；真正改变要求时提交新目标修订，消费该提案并使其余行动、计划、完成或失败建议失效。 |
| [ADR-0008] | 受信 Renderer 的准确正文预览与本人决定关联，但预览不能证明本人已阅读；普通文本不能冒充受信确认。 |
| [ADR-0009] | 复用接纳、事务 JobStore、领取及条件完成模板；领域仍裁决发送、效果、费用和业务完成。 |
| [ADR-0010] | 共享契约、生成物和发布兼容性沿单仓库规则维护；不能在模块正文另造同名消息字段。 |

## 2. 已读来源及编写定位

以下模块正文、实现和专题已阅读全文。研究已读各报告的论证正文、比较及 I/O 专题；其中源码链接索引是出处目录，不作为新的架构合同。研究固定的是报告注明的源码版本，本次没有执行上游程序，也没有重新测量其性能。

| 来源 | 应抽取的内容及定位 |
| --- | --- |
| [CORE] | §2 六组对象；§3 Task 内部记录；§4 Decision/Operation 子记录；§5 独立辅助责任；§6 投影与工作记录；§7 配置；§8 按需扩展；§9 共同提交范围。关键表在 L26–35、L42–54、L61–70、L77–105、L112–153。 |
| [O]、[O-I] | 任务主线、控制、完成和预算；实现 §1 模块与公共模板、§2 记录/条件/锁序、§3 接纳、§4 提案与行动、§5–8 控制及预算、§9 清理、§10 部署、§11 RT-01–24。 |
| [O-V]、[O-A]、[O-S] | 验证规则与实现、目标覆盖、证据适用性；八条逻辑访问路径及有限边界；未发布公共协议的 Schedule 设计范围。 |
| [B]、[B-I]、[B-D] | 双系统分工和对外字段；BrainContext、最终请求、记录/发送/输出发布、有限计划、恢复和 BI-01–20；确定性、固定答案类型、通用推理三条路径的资格与升级。 |
| [E]、[E-I]、[E-P] | Invoke/Operation、准确绑定、效果分类、控制和 GUI；最后准入、Attempt、真实出口、覆盖、原结果、资源隔离及 EX-01–31；程序化工具和可复用执行环境的启用边界。 |
| [C]、[C-I] | 内部/外部委派、字段与成功含义；唯一映射、phase 投影、共同事务、外部创建、控制、Closure、账务更正、SDK 等待和 CL-01–22。 |
| [F-T]、[F-W]、[F-O] | 当前正式 Orchestrator 已写入的任务生命周期、可靠交接和主线；迁移时保留其细化，特别是任务输入解释、控制传播和固定设备恢复。 |
| [CONTRACTS]、[METHODS]、[PROTOCOL] | 共同行为及字段定义的入口；公开编码仍以原 Schema 和方法表为准，本清单不是替代协议。 |
| [OVERVIEW]、[UML]、[FLOWS] | 四种边界、核心关系和请求路径。已有关系图及交接表应整合引用，不能写成“现有文档没有对象关系”。 |
| [R-COMP]、[R-BASE]、[R-OPT]、[R-SEM] | 固定源码比较、归档架构基线、优化建议、核心模型的语义覆盖；[R-SEM] §8 的 G-01–07 保留“已补齐/按需/本阶段不交付”的原分类。 |
| [R-IO]、[R-IO-CP]、[R-IO-PC]、[R-IO-D] | 暖 A/B 路径、冷恢复和具体追加/查询/提交/flush/sync 口径；用于解释可以合并什么，不作为速度排名。 |
| [R-CODEX]、[R-PI]、[R-PRIME]、[R-CRUSH]、[R-DEEPSEEK] | 各项目准确对象、调用链、存储和故障语义；借鉴机制及不采用之处见 §8。 |
| [OPT]、[R-VERIFY] | 最新设计采纳位置、冻结对照与 O-01–12/X-01–09；研究检查的范围与历史性。静态检查不证明当前运行系统或生产容量。 |

## 3. 核心模型进入正文时必须讲清的内容

六组核心对象是应用理解责任的组织方式，不是六张表、六个服务或六种状态。[CORE] L26–35 的主定义应保持唯一，其余模块链接该定义并说明自己的写入职责。

| 对象 | 唯一负责方、身份和写入 | 读者及不能合并的事实 |
| --- | --- | --- |
| Session | Interaction 的应用层保存会话/消息顺序、原提交和 Task 关联；Task 初版至多固定一个创建来源 Session。 | 一个会话可关联多个目标；归档不是取消 Task，消息已保存不是 Task 已接纳。 |
| Task | 固定逻辑 Orchestrator 保存原提交、目标修订、要求、计划、控制、准入、预算、完成依据。 | `task.read/result` 组合自己的记录与原 owner 事实；不取得 Executor 的效果裁决权。 |
| Decision | Brain 保存唯一输入摘要、固定配置、可选唯一 ModelCall 和固定 Proposal；Orchestrator 创建 decision_id 并固定输入来源。 | `brain.get` 返回原判断和用量；Brain.completed 与 Task 已消费、Task.succeeded 分开。 |
| Operation | Executor 保存固定 Invoke、Attempt、控制检查、目标事实、效果和用量；O 另有 OperationIntent。 | O 准入与 E 接纳跨域时是两条可恢复记录，不能假装共用一条跨库 Operation 行。 |
| Content | 原内容 owner 保存准确字节及其版本、hash、来源和当前使用条件。 | 引用不是授权；字节不变不代表用途资格不变；正文、关闭、副本清理各有进度。 |
| Grant | 原 Grant owner 裁决许可、原 use 和数值结算。 | 撤销不改写已发生使用；Task 终态不清掉 unknown 费用或一次性使用事实。 |

核心模型还必须保留以下独立责任：[CORE] L77–105。InputSubmission 是交互转交记录；InputRequest 和 Confirmation 由真实业务 owner 创建并随业务改变一次消费；Surface 有自身生命周期，Presentation 是本端显示意图；UseReceipt 不可变，UseSettlement 保存累计结算；Content holder/copy/reference_intent 负责跨库副本；Command/Receipt、Job/Claim、outbox/Delivery 分别表示命令裁决、有限工作领取和交接。它们可以是聚合内部记录，但不能随 Task 或 UI 正文级联消失。

Turn/Run 仅可作为应用展示分组，本方案没有新的业务 Run 权威。Snapshot、列表和搜索可重建；参与完成判断的投影必须追到所需权威水位。token/live frame 可以有界合并或丢弃，正式对象事实及交接责任必须耐久。[CORE] L90–107、L142–154。

## 4. 一条业务链中需要相互定位的交接

以“研究资料并保存报告”为贯穿案例，正文至少应让读者沿下表找到方法、记录、writer、共同提交范围和恢复入口。该链串接现有设计，没有增加业务接口。

| 步骤 | 固定输入和写入者 | 提交、交接与恢复依据 |
| --- | --- | --- |
| 应用提交目标 | 应用固定原目标 ContentRef、目标 Orchestrator、submit command；O 写 Task。 | O 接纳把 Task、原目标/预算、首 decide job、Receipt 共同提交；失答复读原命令。Session 关联跨库另行完成。 |
| 固定一次决策输入 | O 的 SnapshotAssembler 从当前要求/控制/未决全集及获准材料构造 BrainContext。 | 字节预存后，Task 条件事务重查依赖修订并保存 snapshot、decision_id、预留及 dispatch；预存不是跨库原子性。 |
| Brain 决策 | Brain 接纳原 Decision，固定 input_digest；确定性可无 ModelCall。 | 模型准备、用途取得、send_started、供应方调用、输出发布按真实先后；已有 send_started 不透明重发。 |
| 产出正文与提案 | Brain 先固定 publication 局部引用图及 content.put 原命令，逐项取得准确 ContentRef。 | 保存丢答复查询原保存身份，不再次生成；Proposal 终态与收尾责任共同提交。 |
| O 消费提案 | 先查 decision_consumptions；比较 snapshot/goal/control/current qualification；先裁决要求变化。 | 变更要求就提交新 goal_revision 并使本提案余部失效；否则按本次准入保存意图、预算与 dispatch。 |
| 执行保存 | E 接纳 Invoke，固定准确 Capability/Binding、参数、TaskGate、用途和费用关联。 | 接纳不等于目标成功；Attempt 可能已发送就只按能力合同核对/重放原身份，晚到效果由 E 归并。 |
| 读回和验证 | 核验工作通过独立原操作读回准确版本，O 保存条件检查与目标覆盖。 | 执行 applied 不等于条件 pass；检查绑定 goal_revision、Requirement、成果版本和规则/实现，unknown 保留核验。 |
| 任务成功和后续 | O 检查完整要求、当前证据适用性、全部未结效果/委派、账务闭合等完成前提。 | Result 与 succeeded 共同提交；迟到效果/账务、后发现缺陷或内容关闭沿原责任继续，不重开目标。 |

依据：[FLOWS]、[O-I] §3–5、[B-I] §2–4、[E-I] §3–6、[O-V] §3。内容/Grant 的具体协议由相应章节定义；本链只标注其必要交接点。

## 5. Orchestrator 编写素材

### 5.1 职责、字段与记录

CommandHandler 处理认证与原命令，TaskCoordinator 裁决任务，SnapshotAssembler 固定上下文，PlanMaterializer 仅产生完整候选，FactReducer 单调归并原事实，BudgetLedger 写账，JobRunner 调用这些职责。它们是同一模块内部职责，默认共享声明的本地事务范围；生产入口与工作池分装，远端调用在事务外。[O-I] L10–58。

公开 Task 字段包括 `tenant_id, task_id, orchestrator_id, submit_command_id, goal_ref, goal_revision, requirements, policy_ref, revision, control_revision, status, control, wait_reasons, deadline, budget, open_effects, accounting_open, result_ref?`。Requirement 为 `requirement_id, kind, source_ref, rule_ref, required`。OperationIntent 的准入来源三选一：Decision、准确计划步骤、受信条件/覆盖检查；不能由普通调用伪造。ConditionResult 保存准确成果、pass/fail/unknown、依据、证据和 evaluator_ref；Result 只在 succeeded 发布，失败/取消有独立说明。[O] L359–375。

| 逻辑记录 | 主键/唯一性及 writer 必须保存的内容 |
| --- | --- |
| tasks、requirement_versions | Task 身份及原服务固定；要求按 task/goal_revision/requirement_id 保留，旧条件不能覆盖新目标。 |
| goal_coverage、condition_checks | coverage 按目标修订固定完整输入，同输入至多一项未完成责任；检查有 check_id，同 Task/目标/条件/成果只有一个当前选择，历史 verdict 不变。 |
| evaluator_evidence_gates、evidence_defects、result notices | 规则/实现/范围及 gate 修订；缺陷事实和分页影响责任；终态结果追加说明。 |
| task_snapshots、plan_versions、decision_consumptions、plan_step_admissions | 快照不可变；计划指针与安装决定共同保存；每 Decision 至多消费一次，每准确步骤至多准入一次。 |
| operation_intents、received_facts、executor bindings | 固定 Invoke 与原 command；事实以 owner/object/revision 去重并保留 digest，同修订异值冲突。 |
| open_effects/未结委派关联 | 全部已准入意图的权威关联；新增即建立，核清才移除。公开数组上限和通知丢失不缩小此全集。 |
| budget_balances、reservations、allocations | 每任务每单位余额；每物理计费项唯一来源；固定 allocation 不换接收方/单位。 |
| budget_correction_intents、receiver_correction_outbox、incidents | 调用 settle 前固定意图；每用量修订保留唤醒/结算原尝试，可信上调及责任共同提交；保存实际超额与分别可成立的事故原因。 |
| task_policy_acceptances、task_estimate_consents | 受信本人对准确策略、单位、范围、上限和期限的接受事实；Task 固定关联，模型/公开 policy_ref 不证明接受。 |
| jobs、command_receipts、closed_identities | 稳定责任键及 work_revision/lease_epoch；业务决定、责任与回执共同提交；最小身份长期保留。 |

记录依据：[O-I] L68–106；[CORE] L42–54。正式文档应给出逻辑键与共同提交需求，让实现可选合理物理布局，不能机械翻译成每记录一个 CRUD/服务。

### 5.2 事务、完成与恢复算法

1. **固定锁序。** 原命令键 → 根到叶祖先 Task（同层稳定排序）→ 预算/意图/参与领域门禁与业务行 → jobs。条件核验先 gate 后当前条件；事实归并采用同序，不能从 job 反锁领域行。[O-I] L229–236。
2. **接纳。** 原命令/closed identity 先判，再检查期限、路由、容量、有限预算及估算接受；Task、第一责任和原 Receipt 同事务。重复命令不因后来恢复资格而改变原拒绝。[O-I] L238–260。
3. **消费和准入。** 消费前核对当前 Task/快照/控制及领取；要求变化先提交新修订。未变化才安装计划或把完整行动的准确参数、绑定、许可、费用预留和 dispatch 一起保存；之后不改意图。每个后续步骤重新准入，计划不是预授权。[O-I] §4；[B-I] §4.3。
4. **完成依据。** 明确要求不齐全时先覆盖核验；检查绑定准确目标/成果/规则/实现。原 verdict 不可改，当前适用性为 usable/unknown/inapplicable；计划依赖与条件 pass 分别检查。完成时查完整未结集合及当前选中证据，不能遍历截断结果或借模型 complete 直接成功。[O-I] §2.3、§5；[O-V] §3。
5. **缺陷竞争。** 完成对 evaluator gate 取共享锁，登记缺陷取独占锁，登记不锁整批 Task；分页影响处理保留游标。缺陷先提交阻止完成；完成先提交保持原终态并追加说明。只对已受信登记在本地范围内的缺陷作该保证。[O-I] L202–227。
6. **控制与进展。** 目标状态、暂停、等待和新出站资格分开；祖先限制与自身暂停取有效交集，恢复父不抹去子暂停。旧提案、重复反馈、轮询或只换控制身份不重置累计次数、预算、期限及无进展约束；真实新输入按原身份归并。停止推进不结束原 unknown 的核对/结算。[O-I] RT-20–24；[C-I] §6。
7. **费用归并。** 只按唯一物理计费来源的累计差额入账。可信实际超额不截断；`spent+reserved≤limit` 是 strict 准入且提供方履约时的不变量，不能用无条件 DB CHECK 拒绝真实账单。终态后费用修订仍共同保存 outbox/原 settle 工作；JobAck 只证明接纳核对责任，O 还须主动读原账。[O] L242–293；[O-I] §8。

`estimate` 仅限受信本人接受、同本地 Tx 能核验原 Task 关联、未经 allocation 的直接调用；任一费用 Grant 要求硬上限则拒绝。跨域或无受信管理入口只用 strict。固定 allocation 首次封账转预留为实际，迟到更正只追差额，不倒流已释放额度；超额阻止新计费，provider_bound_breach 与 receiver_allocation_breach 独立归因。[O-I] L240–242、§8；[O] L242–283。

### 5.3 访问、保留和验收

[O-A] 已给出八条逻辑访问路径、有限内部扫描/批量边界、锁等待和完成查询设计。公开 100 项不是内部全集上限；SQL LIMIT 也不证明扫描量有界。实际 SQL、DDL、索引、查询计划和负载证据留给实现登记，不应报告成“完全缺少数据设计”。任务列表的稳定上界、游标、授权 epoch、每页物理工作上限和撤权后披露需一并实现。[O-I] §9、§10.1。

RT-01–02 验证接纳原子性和失回执；RT-03–06 验证暂停/取消/父子竞争及晚到事实；RT-07–08、15–19 验证预算、封账和更正；RT-09–12 验证新目标、最小身份、计划缺口和分页；RT-13–14 验证 work_revision/lease_epoch 防旧完成覆盖新责任；RT-20–24 验证有限进展和停止后的收尾。需真实入口、数据库、独立目标事实及断点实验，不能由 JSON 序列代替。[O-I] L593–626。

## 6. Brain 编写素材

### 6.1 输入、路径及持久结构

Decision 是固定输入的一次判断，既不是 Session 回合，也不是 Task 运行实例。确定性路径可不调用模型；固定答案类型仍是一次模型请求；通用推理输出同一 Proposal 合同。选路属于固定策略，固定答案类型的适配器、概率接纳/校准和启用证据是可选能力，不是默认已可运行。[B] §1、§5；[B-D] §1–6。

`DecisionRequest` 固定 decision/task/orchestrator、snapshot_revision、context_ref、准确 capability_refs、model_profile_ref、limits 和 usage_authorization_refs。limits 有 deadline、max_output_tokens、max_actions、max_context_requests、cost_reservation_ref。DecisionRecord 的 accepted/running/completed/failed/cancelled 与 ModelCall 的 prepared/sent/returned/unknown/stopped 分开；后三个 Decision 状态终态不可逆，迟到用量可以增加记录修订。[B] L164–177。

BrainContext 的 `schema_version=brain-context/1`；字段有 task_ref、snapshot/goal/control revisions、goal_ref、完整 requirements、control、plan_ref、facts、assumptions、unresolved_effects、materials、capabilities、gaps、input_manifest。材料区分 evidence/memory/skill/history，文本片段按合法 UTF-8 字节范围，图像引用完整版本。金额、收件人、必要条件和原 unknown operation 从当前权威机械重建；摘要不得替代这些数据。实际读过后再裁掉的材料仍继承处理来源。[B-I] L124–172。

逻辑持久记录：brain_decision/decision_input 一决策一固定摘要；model_call 对 decision_id 唯一；model_attempt_fact 追加原供应方事实；decision_publication 固定 local_id → upload/content.put/准确 ContentRef；decision_output 固定提案或失败；brain_usage 按原计费项保留费用修订；billing_handoff_outbox 每修订唯一；brain_job 责任键 `(tenant, brain_owner, decision_id, kind)`；decision_closure 保留输入及决定摘要。`run_decision`、`reconcile_call`、`billing_handoff` 分工，不把 Decision 终态当作账务结清。[B-I] L99–122、L236–258。

### 6.2 发送与输出的不可合并边界

1. Decision 接纳固定输入、原回执和 run_decision；正文预存后接纳失败的孤立版本有界清理。
2. 准备事务固定 ModelCall、最大输出、计价版本和 use_id；取得全部用途回执，unknown 先查原使用。
3. Adapter 完成所有输入字段、wrapper、日志/诊断及接收方编码；核查实际来源、处理位置、全部真实出口、大小和摘要。门禁事务把这些最小依据与 send_started 共同保存，随后只发已核验的只读编码；门禁后不追加日志披露或换字节。
4. prepared 且没有 send_started 可重核资格后继续原首次发送；已有 send_started 只查原请求或记 unknown。禁用 SDK/代理透明重试，标题、压缩、修复等额外推理需要新 Decision 和费用准入。
5. 生成正文先校验局部引用有向图，固定 publication/原保存命令，取得准确引用，再发布 Proposal；保存失答复不再推理。取消先提交则迟到输出只作仍获准的诊断，费用责任保留。

依据：[B-I] L289–326、§2.4、§4.1。禁止保存正文的临时路径仍须能耐久保存允许的最小来源与披露事实；连这些都不能保存就不能接纳外发。门禁前丢临时字节为 context_incomplete；门禁后丢字节保留可能发送与费用，不能凭摘要重发。

Proposal 五类为 act、need_context、request_input、complete、fail。act 的直接非空 actions 与 `actions=[] + plan_delta` 安装计划互斥。有限计划固定 action_template、depends_on、pass_conditions 和前项输出的 JSON Pointer 绑定；有序依赖完成不等于条件 pass，unknown/缺指针不猜参数，旧计划输出不替换新输入。计划不能执行任意表达式，也没有把自由 instruction 当通用 S1 profile 的合同。[B] L179–199、L222–226；[B-I] §4.3。

### 6.3 恢复与验收

迟到账单按原 ModelCall 追加更高修订，与独立 outbox 和工作唤醒同事务；r2 不覆盖 r1 未交付命令。原命令过期后保留旧未知尝试，为同修订/同摘要建立有限继任命令；原 O 的 JobAck 才能确认交付，但结算须 O 读原账。[B-I] L322–326。

BI-01–10 覆盖原 Decision、调用/用途/取消及并发恢复；BI-11/19/20 覆盖来源、最终日志出口及临时编码；BI-12/13 覆盖上界违约、终态迟到账单；BI-14–16 覆盖产出保存、条件门禁和未来输出；BI-17/18 覆盖多次摘要后硬约束、投影水位、来源变化与最终窗口。恢复断言需实际发送次数、原供应方查询、DB 决定和授权使用；Schema 只验证结构，不能证明字节存在或供应商只处理一次。质量、延迟与费用另做冻结对照。[B-I] §8；[B] B-01–08。

## 7. Executor 与 Collaboration 编写素材

### 7.1 Executor：准确操作、效果与资源

Capability 固定语义身份、版本/digest、完整输入输出 Schema、effect_class、效果谓词及查询证明、有限重试、授权需求与时长/字节/实际请求/费用/互斥上限。Binding 固定 executor、目标、driver 和配置；ready 只是候选可用性，实际入口仍复核。Invoke 固定原 Task/目标修订/control_snapshot、Capability/Binding、最终参数和 intent_hash、许可、预留、deadline；GUI 再带 observation/control_epoch/lease/max_age。[E] L240–262。

Operation 的 execution_state=accepted/started/closed、effect=not_started/applied/not_applied/unknown、may_apply_later=true/false/unknown 分别解释处理、效果和未来可能性；closed 不证明外部停止。保存原 Attempt、目标凭据、证据/result_ref、累计 usage、usage_final 和 next_action。not_started 必须证明从未越过发送边界，不能从超时错误推出。[E] L263–283。

逻辑表包含 capabilities/bindings/operations、计费交回 outbox、attempts、task_gates/gate_entrances、cancellations、resource_states/leases/observations/target_correlations、jobs、closed identities；内部还保存最后准入值/编码、原结果覆盖、来源与完成位置、live 游标等，不因此新增公开字段。[E-I] L86–150。

- 接纳事务把原 Operation/Invoke、回执及工作一起保存；Receipt.applied 表示执行责任已接纳。准备 Attempt 在真实入口前耐久；与 Brain 的单独 send_started 不同，无法证明未交接时就须按可能已发送处理。[E-I] §3、§3.1。
- hook、模板、wrapper 的所有候选变换先完成，再按最终参数做 Schema、意图、Grant、费用、资源与当前 TaskGate 校验。门禁后只允许不改变语义的驱动编码，实际解析对象、DNS/地址、重定向、回调及日志出口均在范围内；分页/轮询/下载也逐实际请求计量。[E-I] §3.0、§3.3。
- read_only、target_idempotent、no_idempotency_guarantee 使用不同恢复分支；幂等性须有准确作用域、有效期、目标原键查询/重放合同。no_idempotency_guarantee 未知时不通过新意图重做。准备失败、目标事实和 effects_pending 都按原 operation/attempt 归并。[E] §2；[E-I] §4。
- 原结果保存实际覆盖、缺页/缺项/限制和来源；工具字符串、合成 interrupted、临时流与摘要不是目标效果证据。最小可信写回执可先证明写已生效，大正文保存失败另留缺口；读取若不满足声明覆盖则不能伪报完整 applied。[E-I] §6.1–6.2。
- 受管文件持久 journal 记录原 operation/attempt、旧文件身份/版本、目标 hash 和临时位置；临时字节及身份先耐久，再原子替换与目录 fsync。恢复还核对稳定文件身份和写者隔离，否则 unknown；符号链接/相同路径不替代实际对象校验。[E-I] L348–350；EX-19/24。
- TaskGate 是有效任务限制，ControlSnapshot 有认证内容及有限启动窗口；ControlReceipt 逐实际入口报告落实修订和在途集合。取消未知 Operation 先留长期墓碑，终态不能被更高 active 修订复活。[E] L268–275；[E-I] §5。
- Job lease、设备 OS 单写锁、ResourceLease/control_epoch、TaskGate 各解决不同竞争。固定设备旧宿主/子进程可能仍发送时先隔离，云工作接替不自动获得新发送入口；资源在原 Operation closed、may_apply_later=false、Attempt 证据可信且身份/epoch 比较一致后才解除在途隔离。人工接管封闭未越入口的旧动作，已越入口的仍核对。[E-I] §7–9。
- Observation 保存 resource/control_epoch、时间窗口、ui_revision、截图/结构、焦点和坐标；atomic 模拟器须把观察比较与动作启动原子化。真实平台仅 best_effort 时明示并单独验收，缺图不伪装成功。[E] L277–283；[E-I] §8。

EX-01–07 验证接纳、可能发送、原效果恢复和控制到达顺序；EX-08–18 验证接管、过期占用、旧进程、失联和备份缺失；EX-19/20 验证文件耐久和迟到账务；EX-21–25 验证最后准入/绑定/真实对象/出口；EX-26–31 验证来源顺序、覆盖、媒体项、临时流、最小回执与请求限额。实验要求真实目标入口和独立断言，静态目录/资源序列不证明 OS 隔离或效果。[E-I] L499–542。

### 7.2 Collaboration：唯一子映射、控制与闭合

内部协作只指同一 Orchestrator 与本地事务范围内的子任务；另一 Orchestrator 即使实现同套 Harness 协议，也是外部委派。Facade/DelegationAdmission/InternalChildFactory/ExternalAgentAdapter、Reducer、ControlPropagator、SettlementCoordinator 复用父 O 的协调和工作模板，不再建立第二套主循环。固定 allocation 只用可信上界，不能放入 estimate-only 的收费能力。[C-I] L5–71。

AgentDescriptor 固定 agent/version/digest、internal/external、goal/result Schema、control_support、permission_ceiling、cost_bound、adapter_version。Delegation 固定父 Task、准确 agent_binding、goal/input refs、收缩约束/deadline、allocation、permission refs、有界祖先链；内部 child_task_id 与外部 endpoint/remote_task_id/creation_key 互斥。进展另保存 remote_revision、result、usage_revision、effects_pending 和 settlement；phase 只是派生读值。[C] L104–125。

逻辑表为 descriptors/bindings/delegations、internal_child_links、external_task_links、incoming_delegations、progress、controls、delegated_inputs、jobs、correction outbox 及 closures。相同远端修订必须相同 digest；旧修订不覆盖、异值报协议冲突、不同 child 映射拒绝。已映射成功的重复查询可用新的本地修订解除查询缺口，不重复计费。[C-I] L73–115、§5。

1. **内部创建。** 先原命令，再祖先根到叶、父预算、delegation，jobs 最后；原回执先于当前状态判定。父预留/allocation、唯一 child、映射、首 job 与回执共用同 Tx 共同提交，取消与创建形成可解释顺序。[C-I] §2.2–3.1。
2. **外部创建。** 第一次出站前持久固定 creation_key、准确 endpoint/输入/allocation 及工作；本地 Receipt.applied 只证明委派责任成立。答复丢失查询原键，暂时 not_found 不是永远未接纳；缺幂等/查询支持时只给有限咨询能力，不能假装可可靠创建副作用任务。[C-I] §4。
3. **Harness 跨域接收。** 验证认证发送者与 task.submit.delegation_context；以发送者/parent_delegation 固定 incoming 唯一映射；查询当前预算和原分配回执，receiver 端分配门禁、child 与首责任同事务。budget.close 先到时对未知 child 也关闭迟到接纳；双方没有跨库事务。[C-I] §4.1。
4. **控制与输入。** 父限制不清子自身暂停；外部不支持 pause 明示仍可能运行，不能报告全树 enforced。无原生修订的远端不乱序发送 pause/resume；父终态不产生新目标动作。回答固定原远端 request/revision、答案和命令，一次消费，普通回答不扩权。[C-I] §6–7。
5. **phase 投影。** 按首个匹配规则：有 durable Closure 为 closed；有具体映射/控制/效果/结算缺口为 reconciling；唯一 child 已知为 active；否则 preparing。普通非最终用量、例行查询、暂停/等待输入本身不构成 reconciling。同 revision 同投影；时间到期先成为业务事实再影响投影。原接纳回执不重新计算。[C-I] L94–115。
6. **Closure。** 需要新目标动作已封闭（或证明创建未且不能再接纳）、全部受管子目标终态、原效果及晚到可能性已核清、接收端新消费封闭且全单位最终费用可核并 settle、无能触发目标动作的输入/控制待处理。子答案或“completed”文字只可预览，不能代替这些依据。[C-I] §8。
7. **迟到费用。** settled 后可信上调保留 closed；接收方按 allocation/usage_revision 保存独立 outbox，父调用 budget.settle 前保存 correction intent，先查原命令，再按合同建立继任。只追累计差额，不重开目标、不倒流释放额度；真实超额全记账，两类违约独立举证。已 closed 的委派扫描仍涵盖更正工作。[C-I] L308–333、L380–403。
8. **读、等与冷恢复。** SDK 薄封装 read/wait/batch；零等待快照，等待超时不取消，通知仅促使重读；父消费进展与下一责任共同提交。冷恢复使用原映射、TaskPolicy、InstallLock 和绑定，再检查当前资格，不能用 latest 代替。深度/活跃数/总尝试/期限有限，控制/查询/结算保留容量。[C-I] §9。

CL-01–05 验证共同提交、取消竞争和外部未知；CL-06–13 验证修订冲突、控制、答案与效果分离、账单和输入；CL-14–17 验证最小映射、升级不可解码及 phase；CL-18–20 验证上界违约和封账后更正；CL-21–22 验证旧 Claim 不覆盖真实新责任。父库、远端库和目标真值须独立观察，不能直接填 phase 代替实验。[C-I] L436–465。

## 8. 研究中的采用、负例与候选

采用判断以最新 [OPT] L125–138 的 O-01–12 和当前模块落点为准；研究项目的名称和机制不能直接转成我们的业务对象。默认不会移植它们的技术栈、Turn/Run 状态机或不等价耐久保证。

| 研究来源 | 已纳入当前设计的机制/问题 | 不能照搬或不能作出的结论 |
| --- | --- | --- |
| [R-CODEX] §2–7、C-01–14 | 历史与模型窗口分开；压缩后机械重建目标/约束；按真实来源固定请求；准确 MCP 绑定与已准备调用；异步子句柄/通知后重读；固定子配置冷恢复。 | JSONL flush 不等于 fsync；SQLite 历史投影与独立 Goal/队列库不能笼统归为缓存；合成中断只是 transcript 形状；并行工具不证明资源不冲突；透明 provider 重试不符合 0..1 ModelCall。 |
| [R-PI] §2–7 | 必要 mutation 共同提交；原工具输入/输出与来源序分离；恢复时同时检查原调用和当前安全资格；extension staged commit/discard；稳定输入/回答关联及后端一致性向量。 | classic AgentSession、AgentHarness 和 pi-durable 是三条不同路径；steering 在正常边界不等于取消已启动工具；dill/sidecar 或历史恢复不恢复外部效果；hook 改参若不重验是负例；QuickJS 沙箱不隔离 host tools。 |
| [R-PRIME] §2–7 | 原 Session/history 与机械 harness 状态分开；保留近期锚点/来源；spawn/collect 的有界等待；持久无进展和使用计数；交互 kernel 的 busy/实际退出区别。 | 文件 mtime 不提供 CAS；持久失败仍 live 显示不能作正式成功；abort 返回不能证明 kernel 退出；cron 锁失败仍执行和保存失败仍 dispatch 是负例；refine 结果不是独立改善证据；不推定每请求固定隐藏 warmup 成本。 |
| [R-CRUSH] §2–7 | 输入接纳序与取消水位、旧 worker compare/delete；授权一次消费；PendingConfig/config generation 的就绪组织；SSE 丢失后重读；同源完成乱序的展示组织。 | PrepareStep 动态目录不是原准确 binding；in-memory AcceptedRun 不是 durable Task；hook allow 不产生 Grant；SQLite NORMAL/FlushAll 错误后完成提示不能作本项目耐久基线；只保存首项媒体是不完整覆盖；child cost best-effort 不是封账。 |
| [R-DEEPSEEK] §2–8、D-02/03/10–13 | 定义→provider→consumer 依赖；事件快照冻结；非空 batch sync/首次物化目录持久化；模型/工具入口前 checkpoint；typed unknown；有界观察游标；最终请求实际日志字段；分离可复用 Session 与一次激活；next-generation 装配后原子替换。 | 只有真实挂载 checkpoint 的 bundle 才有屏障；session-log 可能把 canonical 日志再次外发，surface 压缩不界定真实请求；schedule inbox/receipt 两提交有重复窗口；jobs-local 不是事务 JobStore；内存取消或 cooperative VM 不证明进程隔离；原生 retry 不符合单次模型限制。 |

跨项目已接受方向包括 O-01 输入硬约束/最终编码、O-02 有界进展、O-03 最后准入和准确绑定、O-04 分域恢复和冷恢复资格、O-05 原输出/覆盖、O-06 薄 SDK 与子责任（使用协作时）、O-07 输入竞争/呈现、O-08 受信静态装配优先、O-12 公共故障语料。O-09 经验自动改进、O-10 程序化工具的通用增强、O-11 大目录渐进发现仍是候选/条件能力；X-07/08/09 是独立实验因子，不是默认启用开关。[OPT] §4–7。

I/O 比较必须区分逻辑记录 E、读写调用 R/W、事务 Tx、flush F、sync S、历史规模 H 和回调 K；暖 A=一次模型最终答复，B=两次模型加一次普通工具，冷启动另算。DeepSeek 出厂 sdk-minimal 在报告固定前提下为 9/15 事件、无 checkpoint；纯 core 为 8/13，显式 checkpoint 变体的语义调用为 2M+T，非空批次才 sync，不能将这些数字混成默认路径或物理 I/O。[R-IO-D] §1–6。

对本方案可吸收的是同阶段记录共同提交、准确正文只保存一份、有界派生视图、live token 不逐片建 job、小静态目录不每轮搜索、Memory/委派/评估按实际需要启用。不能因此删掉单独 Goal/Requirement、许可使用、可能发送边界、unknown、账务封闭或目标完成责任；也没有源码证据支持固定吞吐倍数、提交节省比例或延迟承诺。[R-IO] §5–7；[R-IO-D] §7。

## 9. 扩展边界、冲突与待决事项

### 9.1 按需能力不升级成首期承诺

| 范围 | 已确定的语义；尚未交付的部分 |
| --- | --- |
| G-01 Session 分支 | 分支固定准确来源、历史截止和配置，不复制活动 Operation/use/预算或撤销外部世界；完整 fork/branch/revert 公共合同未交付。 |
| G-02 输入队列 | 已有 InputSubmission 绑定 InputRequest，queued 可原子撤回，sending 为 withdrawal_requested 并核对是否已消费。尚缺的是自由输入多 Lane/steering/follow-up/通用 Run 控制，不能说现有系统没有输入撤回。 |
| G-03 可复用子会话 | Session 持续身份与每次 Delegation/Task/allocation 分开；旧目标未闭合不启动冲突新目标。child.send、复用选择、跨父复用和分支公共合同未冻结。 |
| G-04 Schedule | 规则/version、时区、occurrence、missed/overlap、原命令及投递责任独立；触发前持久 occurrence，停用只封闭未来触发。无公共 Schedule Schema/API，不能拿 JobStore 代替用户调度语义。 |
| G-05 可复用执行环境 | 环境 identity/config、busy、实际退出、占用、配额和有限检查点分开；abort 不证明资源释放，计算检查点不恢复外部效果。通用创建/控制/检查点互操作未冻结。 |
| 固定答案类型及任意 S2→S1 | 固定题型的 typed_decision 字段/升级边界已有设计，但需适配器、费用/校准及启用证据；任意子问题还缺有限模板、输入候选绑定、派生答案及计划完成合同，不能隐藏在 instruction 中。 |
| estimate 与证据当前资格 | 同 Tx 受信 estimate 的内部 Registry 已有实现设计；跨域认证资格合同未冻结，所以 strict-only。普通评估仅保证已受信登记的已知缺陷范围；要求更强远端当前证据资格的用途在合同落实前不启用。 |

依据：[R-SEM] L269–277、[CORE] §8、[O-S]、[E-P]、[C-I] §9.3、[B] §6.2、[B-D] §6、[O-I] L227、L240–242。G-06 Memory、G-07 动态安装有其他模块的独立合同；不能因列在“扩展”就取消其被启用时的正式责任。

### 9.2 可按既有依据直接修正的措辞和整合问题

| 问题 | 来源及处理依据 |
| --- | --- |
| Brain 把持久 InputRequest 创建归给 Interaction | [B] L185 与 [CORE] L81、[I] L137、[I-I] L252、[F-T] L280 的实际业务 owner 定义不一致。正式 Brain 应写“提出输入请求建议，任务 InputRequest 由 Orchestrator 创建；Interaction 呈现并转交”，不需要新架构选择。 |
| 同一发送/控制/结果规则出现在多份文档 | [F-T]、[F-W] 与 [B-I]/[E-I] 多处重叠。需要确定 owner 主定义，主线只保留交接前提与链接；不能因重叠而删掉 prepared/send_started、Attempt、最终编码或原结果覆盖的不同条件。 |
| 概念关系已经有，场景到存储的定位仍分散 | [OVERVIEW] L37–65、[UML] L63–144、[CORE] 及四模块实现有对象和边界。正式化补的是场景→方法→记录→writer/Tx→恢复→验收索引，不是重新发明 ER 模型。 |
| 老正式文档有更具体的交互处理 | [F-T] L297–336 已规定受信 task-input handler 保存原消息/身份、规则或受限模型形成具体建议、歧义等待、固定原 method/command/delivery；拒绝的 task.input 不能换身份降级为 revise。整理 draft 时不可丢失。 |
| 物理 DDL/查询计划与逻辑设计的层级 | [O-A] L5、L43、L71–75 明确是逻辑路径，实际 DDL/SQL/计划是实现证据。正式正文应标记待实现验证，不应夸称已测，也不应笼统判断没有数据/访问设计。 |
| 研究和当前设计的时间范围 | [R-BASE]、[R-VERIFY] 的归档基线及历史验证数字不代表当前九模块已运行。保留报告原日期/commit 和验证性质，正式正文只引用得到支持的设计动机。 |

本范围暂未发现必须向用户重新选择的核心架构问题。上述未冻结合同已有“默认禁用/strict-only/按需独立交付”的保守边界，可直接写清；只有总体范围决定要在本轮实现这些扩展时，才需要把对应新合同作为设计议题。用户待确认的正文组织、读者和交付方案由总编写任务集中处理，本清单不重复提问。

## 10. 编写及验证交接要求

每章应从一项可推演行为进入，随后说明参与对象、创建和修改者、字段/唯一键、共同提交点、跨域原命令、阶段成功含义、异常恢复及保留条件。模块内部职责名称可以改变，原身份、写权、提交边界和恢复结果必须一致。字段主定义指向公共契约，模块正文解释为什么和何时读写。

验收应保留运行实验编号和独立观测点；不要为每段描述另造一份仅重复文字的测试。静态 Schema/协议序列、公共存储适配器故障实验、具体领域效果实验、质量/费用实验、生产容量测试是不同证据。此次只完成资料读取和编写清单，没有执行上游源码、领域故障实验或性能测量。

[CONTEXT]: ../../CONTEXT.md
[ADR]: ../../docs/adr/
[ADR-0001]: ../../docs/adr/0001-retain-closed-identities.md
[ADR-0002]: ../../docs/adr/0002-go-wss-grpc.md
[ADR-0003]: ../../docs/adr/0003-production-distributed.md
[ADR-0004]: ../../docs/adr/0004-discovery-fixed-task-routing.md
[ADR-0005]: ../../docs/adr/0005-evaluator-evidence-eligibility.md
[ADR-0006]: ../../docs/adr/0006-adopt-requirements-before-actions.md
[ADR-0008]: ../../docs/adr/0008-trusted-renderer-preview.md
[ADR-0009]: ../../docs/adr/0009-reliable-work-framework.md
[ADR-0010]: ../../docs/adr/0010-monorepo-shared-contract-release.md
[CORE]: ../../docs/architecture/.draft/core-data-model.md
[O]: ../../docs/architecture/.draft/orchestrator/README.md
[O-I]: ../../docs/architecture/.draft/orchestrator/implementation.md
[O-V]: ../../docs/architecture/.draft/orchestrator/verification.md
[O-A]: ../../docs/architecture/.draft/orchestrator/access-paths.md
[O-S]: ../../docs/architecture/.draft/orchestrator/scheduled-triggers.md
[B]: ../../docs/architecture/.draft/brain/README.md
[B-I]: ../../docs/architecture/.draft/brain/implementation.md
[B-D]: ../../docs/architecture/.draft/brain/decision-paths.md
[E]: ../../docs/architecture/.draft/execution/README.md
[E-I]: ../../docs/architecture/.draft/execution/implementation.md
[E-P]: ../../docs/architecture/.draft/execution/programmatic-tools.md
[C]: ../../docs/architecture/.draft/collaboration/README.md
[C-I]: ../../docs/architecture/.draft/collaboration/implementation.md
[I]: ../../docs/architecture/.draft/interaction/README.md
[I-I]: ../../docs/architecture/.draft/interaction/implementation.md
[F-T]: ../../docs/architecture/orchestrator/task-lifecycle.md
[F-W]: ../../docs/architecture/orchestrator/durable-work.md
[F-O]: ../../docs/architecture/orchestrator/README.md
[CONTRACTS]: ../../docs/architecture/.draft/contracts/README.md
[METHODS]: ../../docs/architecture/.draft/contracts/methods.md
[PROTOCOL]: ../../docs/architecture/.draft/contracts/protocol.md
[OVERVIEW]: ../../docs/architecture/.draft/technical-overview.md
[UML]: ../../docs/architecture/.draft/uml-models.md
[FLOWS]: ../../docs/architecture/.draft/request-data-flows.md
[R-COMP]: ../../docs/research/agent-harness-comparison/README.md
[R-BASE]: ../../docs/research/agent-harness-comparison/architecture-baseline.md
[R-OPT]: ../../docs/research/agent-harness-comparison/architecture-optimization.md
[R-SEM]: ../../docs/research/agent-harness-comparison/core-model-semantic-coverage.md
[R-IO]: ../../docs/research/agent-harness-comparison/data-flow-io-comparison.md
[R-IO-CP]: ../../docs/research/agent-harness-comparison/io/codex-prime.md
[R-IO-PC]: ../../docs/research/agent-harness-comparison/io/pi-crush.md
[R-IO-D]: ../../docs/research/agent-harness-comparison/io/deepseek.md
[R-CODEX]: ../../docs/research/codex/README.md
[R-PI]: ../../docs/research/pi/README.md
[R-PRIME]: ../../docs/research/prime-agent/README.md
[R-CRUSH]: ../../docs/research/crush/README.md
[R-DEEPSEEK]: ../../docs/research/deepseek-harness/README.md
[OPT]: ../../docs/architecture/.draft/validation/optimization-evidence.md
[R-VERIFY]: ../../docs/research/agent-harness-comparison/verification.md
