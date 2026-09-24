# 协作任务端点契约

[总览](README.md) · [委派机制](delegation.md) · [消息交付](delivery-and-recovery.md) · [验收](validation.md)

本页集中定义协作端点的内部字段和逻辑接口，不是可发送的 v1 JSON，也不是已实现的 SDK。线上公共字段继续沿用[线格式](../endpoint-cloud-protocol/wire-format.md)；本页跨端映射属于 [CO-P1](validation.md#proposals)。消息交付自身记录和接口集中在[交付设计](delivery-and-recovery.md)，避免复制其状态。

<a id="agent"></a>
## 1. 已安装 Agent 声明

声明由受信启动配置或已获准的扩展安装产物提供，协作端点维护可查询投影。首次默认使用静态配置；不新增远端自注册服务。大脑取得候选不代表可派发，核心须按固定版本重新解析并复核。

| 字段组 | 含义与约束 |
| --- | --- |
| agent_id、revision、implementation_ref | 用户可用的 Agent 标识、不可回绕声明修订、固定配置／实现摘要；同版本内容不能更换 |
| actor_id、host_endpoint、mode | 已登记主体、受信宿主及 internal／external；外部主体绑定遵守[身份契约](../identity-and-authorization/contracts.md#identity) |
| supported_goals、input_contract、result_contract | 有限目标种类、输入和结果验证器版本；说明引用、内容和效果证据要求，不能仅给自然语言能力描述 |
| task_authority、adapter_ref | 本方固定任务权威及接入适配器；external 另固定原系统身份及发现地址的受信配置引用，不接受模型提供任意回调 URL |
| authorization_requirements、data_obligations | 所需动作／用途、允许接收方与处理位置、保存和披露约束；声明只描述可落实能力，不签发许可 |
| recovery_contract | 原委派接纳查询、结果读取、取消与原取消查询、重复请求保证、记录保留及启动期限执行能力；各项须有实现验证记录 |
| budget_contract | 各维度单位、hard／estimated 上限类别、消费限制、累计用量来源、最终结算依据；不能提供硬上限时明确标记 |
| limits、offline_support | 有界并发、子任务数／深度、输入／结果大小和保留策略；默认不支持离线，声明支持也不替代有效离线许可 |

internal 配置选择子任务的大脑策略及可用能力；子生命周期仍由 Harness 核心管理。external 配置保留外部独立决策循环，本方父委派 D 保存合同和协作事实，不另建 Harness 代理子任务；外部不能写 Harness 任务表或直接占用本方 Operation 身份。两种模式均禁止把长期 Agent 冒充一次普通工具调用。

<a id="delegation"></a>
## 2. 固定委派合同与关联

调用上下文复用核心的受信 RequestContext，用户、来源端点、actor 及活动所有者从入口取得。以下合同来自已准入的父 Operation，协作端点不能接受模型绕过核心直接提交的原始计划。

| 字段组 | 提供方与固定语义 |
| --- | --- |
| delegation_operation_id、parent_task_id、parent_owner_epoch | 父核心准入产生的原委派 D、父任务 P 及控制版本；D 已属于用户级内核操作索引，不再拿 D 创建另一条子提交操作 |
| mode、child_task_id、task_authority | mode 取声明的 internal／external；internal 由父核心固定 C 及同用户写权威，C 不因超时重建；external 无 C，task_authority 仍指本方父任务权威，不能填外部服务冒充 Harness 权威 |
| agent_binding | agent_id、声明修订、配置摘要、actor、宿主与适配器；升级只影响新委派，原委派不静默换实现 |
| goal、constraints、acceptance_binding | 子目标、用户约束及受信任务策略／结果条件；父核心保留自己的验收条件，子结果不能改写父条件 |
| input_refs、source_bindings | 必要上下文／内容及完整来源绑定；数据最小化后仍保留派生来源闭包，不共享父任务全部记忆或推理历史 |
| authorization_binding | 父委派依据、固定子许可、子 actor／宿主、允许操作／用途及约束；由授权服务 Delegate 产生，端点只验证和使用 |
| budget_allocation | 父预留引用、各维度整数最小单位的份额与目标／收尾划分、hard／estimated 类别；包括子代消耗，不能再次从用户总额借一份 |
| start_before、task_deadline | 最晚接纳／启动和子任务截止，均不晚于适用父期限与许可限制；截止后仍可在独立有效权限内进行有限收尾 |
| ancestry、max_depth | 核心生成的祖先委派／任务关联及深度约束；internal 不得含 C 或形成循环，获准的外部再委派也计入原份额和深度 |
| retention_binding、intent_binding | 获准查询／保存期限及规范化合同内容绑定；期限不能随重投延长，同键异意图拒绝 |

子许可可以在父准入前由受信路径申请，并将原授权 command_id 固定在准入准备记录中；授权响应未知沿原命令查询，未准入不能启动子工作。已签发但未使用的许可按原期限／撤销清理，不意味着预算已分配。父准入成功后把子许可引用、D、模式对应的关联、份额及派发工作一起保存。单次许可的消费仍在相应资源处理端，不能用一次委派接纳把权限推广为所有子操作可用。

internal 模式第一次接管 D 时，协作端点持久分配 **child_submit_operation_id=S**，并固定 `D → C/S`。S 是新且独立的子提交身份；子核心按 `(user, S)` 唯一约束创建 C。D 与 S 同键会与父 Operation 冲突，必须拒绝。external 模式无 C/S，接管时固定 **external_submission_key**，取得接纳依据后绑定 **foreign_task_ref**（服务身份、原提交键及原生任务 ID）；不能每次 SDK 调用重新生成提交键，也不能用原生 ID 冒充 Harness task_id。

<a id="interfaces"></a>
## 3. 逻辑接口与成功语义

所有查询先检查当前访问和披露权限；变更使用固定键及意图绑定，响应未知不等于明确拒绝。接口名称仅定义模块边界，调用方不直接写表。

| 接口 | 输入 → 输出 | 责任及错误后的继续方式 |
| --- | --- | --- |
| Peer.Find／Resolve | 有界筛选条件；或精确 Agent 与修订 → 候选／准确声明／缺口 | Find 只暴露当前用户可见配置；Resolve 不使用网关消息能力代替 Agent 声明。缺契约不返回可派发候选 |
| Peer.Delegate | RequestContext、固定合同 → recorded／rejected／提交未知 | recorded 表示合同、内部 S 或外部提交键及接纳责任已耐久保存；不是被委派方业务接纳。重试返回原记录，不创建第二工作 |
| Peer.ReadDelegation | 原 D → 接纳状态、模式对应的关联、最新状态／用量及缺口 | 在绑定权威查询；没有记录只返回缺口，不证明从未接纳。当前状态和固定结果分开 |
| Peer.ReadResult | 原 D → 固定成果／外部报告、证据与内容引用／尚无结果／不可用 | 内部用 ReadResult 取得原成果；外部沿原任务读原报告。正文无权或已清理时不以摘要重建，不重新执行 |
| Peer.Cancel | 固定取消 K、目标 D、原因与期限 → recorded／rejected／未知 | 保存控制意图及固定子取消关联；停止含义另查取消结果，原取消重投不生成新尝试 |
| Peer.ReadCancel | 原 K → 原控制接纳与固定答复／未决／不可用 | 本地协作新增的查询能力，不映射为已有 task.query_operation 的完整答复；外部端不具备时保留其缺口 |
| Peer.CheckRecovery | 当前轮次、D、owner、领域依据 → 允许行为及缺口 | 只提供协作领域结论；连接适配器组合适用结论，实际使用仍复核 |
| Peer.ReadPending | 当前用户受信管理身份、过滤及分页游标 → 原 D、接纳／控制阶段、缺口版本、最后核对时间与原权威位置 | 只读视图，不因管理入口自动获得外部私密正文或其他用户信息 |
| Peer.Recheck | 固定管理 command_id、D、预期缺口版本、当前核对权限及剩余恢复额度 → 原核对责任／拒绝／未知 | 命令回执与原对象的有限核对工作一起保存；同键异意图拒绝，无余量不扩额，不创建新任务 |
| Core.Observe | 已绑定端点产生的委派事实 → 已保存／重复／冲突 | 复用现有核心接口；端点在核心持久确认前保留原转交责任 |

子任务接纳复用核心 Submit 的创建事务。协作端点将不可变合同作为**受信服务端配置来源**，为该 C/S 固定策略、Agent、许可与预算份额引用；不是让公网 task.submit 增加任意字段。该份额只能消费父已预留部分，不另占一份根用户额度。此本地装配必须验证核心实际采用这些配置，并返回相同绑定；普通 Submit 若无法应用受限预算／策略，则视为适配器缺失，拒绝启用委派。它落实现有[内部 Agent 子任务要求](../task-kernel/decision-and-work.md)，不扩大客户端提交权限。

创建调用使用父 actor 的任务创建／委派许可，或受信服务自己的获准创建身份；子运行配置另绑定子 actor 与子许可。子资源调用由受信宿主从隔离运行实例建立子 actor 的 RequestContext，不能将父 RequestContext 与子 grant_ref 直接配对传给授权服务。外部路径同样分别验证提交发起者与外部执行主体，只有可证明的代表关系才可代行。

取消也使用分层身份：用户对父任务的取消操作、父核心为每个 D 分配的取消 K、协作端点为 C 分配的子取消 Q 各自独立。K 已属于父侧 Operation 时不能再次用作子 task.cancel；同一次 Q 重投不换键，真实后续尝试用 Q2 并保存前次拒绝或未达成控制目标的依据。

<a id="facts"></a>
## 4. 接纳、结果与用量事实

一个事实包含 D、固定生产者及版本、受信原记录引用、领域修订／事实键和最小获准内容。internal 另含 C；external 始终包含固定服务身份与 external_submission_key，取得可信原生接纳后再要求 foreign_task_ref，之前可缺省且不得阻断 pending／rejected／gap 转交。状态、固定结果、累计用量、取消答复各有自己的版本或身份，不能用消息序号或一次总 revision 覆盖全部事实。

| 事实 | 字段与裁决边界 |
| --- | --- |
| admission | pending／accepted／rejected 及原接纳依据；internal 的 accepted 要求子任务及首次工作已提交，external 要求可查询的原生接纳记录。明确 rejected 须证明同一接纳键已封闭、后续不能晚提交；查询暂未见保持 pending＋缺口 |
| child_snapshot／external_snapshot | internal 保存核心原 revision、任务状态、effects_pending、等待原因；external 保存原生状态、适配器保证及未决项，不改写为 Harness Task.completed |
| child_result／external_result | 固定结果身份、成果引用／摘要、完整来源、结果条件及验证依据；成果当前可取／获准性另查。外部原报告仅作为协作证据，父核心按父验收条件裁决，不由端点生成正式任务终态 |
| usage | D、预算维度／用途、usage_revision、cumulative_confirmed、final、原账单来源；同修订同内容重复忽略，同修订异内容冲突，较低修订不回退已确认累计值 |
| cancellation | 原 K、目标 D、固定子取消尝试关联、控制决定及固定结果；结果区分已保存控制、未开始且已封闭、已停止／结束、停止或效果未知；不改写子任务真实效果 |
| gap | 原对象、所缺证据、查询位置、最后已知版本、后续责任与下次条件；不把 unknown 伪装成未发生或已失败 |

`usage.final=true` 只在所有该份额下的计费来源均已封闭、最终金额确定、不会再产生该份额的新费用时成立；任务完成／取消不构成最终结算。包括晚到账单和有成本收尾，尚未结清时为 false。反悔的最终账单属于契约违例，保存冲突、停止新委派并告警，不能把回退或额外费用静默摊到其他任务。提供方不能承诺最终封账时，必须声明该限制并保留未知占额。

同一 D／维度／用途的更高 usage_revision 也不得降低 cumulative_confirmed；final 只能从 false 变成 true。final 后不能再次增加、降低累计或改回未封账；同值确认可去重，其余保存为冲突。版本和金额的单调性分别检查，不因版本较新就信任其账单。

内部用量由绑定的内核运行账本提供有界只读投影；外部用量由固定适配器验证原账单。此投影是实现适配接口，不是 task.query 的新增字段。子代费用先归直接父委派累计，本方根预算不能再累加其叶操作。

<a id="storage"></a>
## 5. 协作记录与原子边界

首期复用宿主事务存储，按用户及固定权威分区；协作表与运行表可同库，但设计不依赖二者跨模块全局提交。内部代理生命周期复用任务调度；协作扫描只推进交接、查询和转交，不执行第二套大脑循环。

| 记录 | 唯一键与持久内容 | 一起提交的责任 |
| --- | --- | --- |
| Delegation | `(user, D)`；合同、意图绑定、internal 的 C/S 或 external 的提交键、接纳阶段、派发准备、控制版本、缺口 | 首次记录与接纳工作；重复返回原映射，不重新分配预算 |
| ExternalBinding | `(user, D, adapter)`；固定外部提交键、原任务 ID、准确实现版本、原结果／账单出处 | 外部发送准备与查询责任；不可把后来另一任务绑定到同一个 D |
| PeerControl | `(user, K)`；目标 D、固定意图、子取消尝试及原答复 | 控制接纳与取消／核对工作；同 K 不能改绑 D |
| PeerFact | `(user, D, fact_kind, producer_key)`；原内容绑定及来源、应用／冲突状态 | 原事实与转交工作；父核心确认前不能清除责任 |
| PeerWork | `(user, D, responsibility_key)`；类型、not_before、期限、次数、领取代次、结果及 blockers | 处理决定与后继工作／缺口；通知丢失仍可扫描 |
| ManagementReceipt | `(user, command_id)`；Recheck 意图、原 D／缺口版本、核对工作引用及答复 | 管理决定与工作共同提交；重复命令不重置原查询计数或额度 |

索引覆盖未完成工作／到期领取、父任务到委派、C/S 到 D、未决取消、外部原任务及未封账用量。写入使用条件更新和唯一约束；重复提交先检查当前身份、对象归属和原回执，再核验新变更前提。存储返回提交未知时，用原记录键和步骤查询或重交，不凭一次查询无记录换键。

协作端点的接纳工作准备和本地取消决定竞争同一 Delegation 记录，完整顺序见[取消机制](delegation.md#cancel)。父核心自己的取消与派发准备仍由运行存储裁决，不要求这两个数据库同时提交。

保留期由合同及获准策略固定。开放委派、未决效果、未结算用量和未转交事实保留最小恢复依据；不能为新请求腾空间而静默删除。正文到期或撤权时可清理并留下获准的最小关联／缺口；同一旧 D 永不作为新委派重新接纳。重投启动期限必须有限，过期旧合同即使已无全文也拒绝新启动。若策略不允许长期保留去重信息，到期先封闭入口并声明不可恢复，不能声称无限幂等保证。

<a id="errors"></a>
## 6. 错误与调用方后续动作

下表为本地错误类别，不占用 `harness.*` 标准错误命名空间。每次返回说明是否已持久接管、关联键、缺口及下一动作；错误不能只给一个通用 retryable=true。

| 类别 | 处理与后续动作 |
| --- | --- |
| identity／authorization_denied | 拒绝不适用访问或新工作；结果与正文按当前披露权判断，不泄露跨用户对象 |
| contract_unavailable／version_mismatch | 不派发；原委派不能静默换 Agent 或策略，核心重新评估获准候选 |
| identity_conflict | 同键不同合同、目标或账单内容；记录冲突并冻结依赖它的新行动，查询原权威 |
| capacity_exhausted | 接管前明确拒绝或有界退避；已接管记录不能丢弃，继续原责任 |
| admission_unknown／external_unknown | 保留预算与固定关联，查原接纳或外部任务；禁止新建替代副作用任务 |
| expired／cancelled_before_start | 在能证明原接纳未准备且已封闭时拒绝新启动；已准备则继续取消／核对，不能报告未执行 |
| result_unavailable／evidence_gap | 报告缺内容、权限或来源；父核心等待、补证或按策略结束，不重建虚假原成果 |
| recovery_exhausted | 保存可查询缺口并结束主动轮询，获准被动事实仍可到达；管理边界见[人工处置](delegation.md#recovery) |
