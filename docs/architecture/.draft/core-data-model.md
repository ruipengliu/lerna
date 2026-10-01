# 核心数据模型：对象归属与持久边界

普通请求用 **Session、Task、Decision、Operation、Content、Grant** 六组对象描述。应用组织会话、提交和控制任务、读取材料与结果；内核管理决策、行动与恢复。六组对象是阅读和接口组合的主干，不是六张表、六个服务或六份可以互相替代的完成状态。

本页是对象归属清单与导航。业务成功、字段、并发裁决和恢复规则仍以各负责模块及[共同契约](contracts/README.md)为准。本轮交付设计收敛，不新增公共 Session／Run 协议、不删改现有 105 个领域方法，也不表示已经存在可运行 SDK 或数据库实现。完整语义的启用范围须由具体能力声明，不能从某字段可以保存数据推导。

## 1. 如何决定保留一个对象

当记录需要独立寻址、并发修改、不同权限或保留规则，或父对象结束后仍有责任时，保留其身份和相应记录。仅描述一个处理步骤的内容优先作为字段、子记录或事件，通过所属接口管理。分类不改变公开合同：内部记录可以通过原聚合查询返回，独立实现者依然可以使用既有领域端口。

| 类别 | 在模型中的作用 | 默认组织方式 |
| --- | --- | --- |
| 核心对象 | 组织对话、目标、决策、行动、材料和授权主线 | 六组稳定查询与操作入口；每组由既有负责模块管理 |
| 内部子记录 | 保存核心对象的准确输入、修订、尝试、依据或消费关系 | 类型化字段或关联记录；普通调用者不逐项 CRUD |
| 独立辅助对象 | 承担受信决定、呈现或独立交接责任 | 保留原身份、查询与生命周期；不能强塞进六个父对象 |
| 派生投影 | 为模型、列表、进度或界面组织已有事实 | 带来源修订与缺口；可重建不等于可在未完整时参与裁决 |
| 配置与绑定 | 固定解释、调用和运行所依赖的准确声明 | 引用准确版本并复查当前资格；配置正文与当前启用事实分开 |
| 可选扩展 | 支撑跨任务或跨调用的专门生命周期 | 按能力启用，不进入每个普通请求的默认流程 |
| 公共执行记录 | 支撑去重、领取、可靠交付与最低限度保留 | 使用公共接纳与 JobStore 模板，不另建业务 Run 状态机 |

下文“同事务”均指已经声明的同一个受信本地事务范围和共同事务句柄；同进程、同产品或对象相互引用都不足以证明可原子提交。所有业务身份携带原租户与负责服务；随机 ID 和读取到一个引用不构成权限。

<a id="core-objects"></a>
## 2. 六组核心对象

| 对象／负责方 | 身份与关联 | 生命周期与必要持久点 | 谁通过什么读取 | 保留与独立存在理由 |
| --- | --- | --- | --- | --- |
| **Session**／Interaction 的应用层 | 会话身份、消息身份与顺序、原提交及 Task 引用；Task 初版至多固定一个创建来源 Session | 建立、追加、归档及正文清理独立于任务；跨事务交接前先保存原命令与交付责任 | 应用读取会话及关联对象；Task／Content 仍各自复核权限 | 对话可跨多个目标存在；归档不取消任务，删正文不删除原提交和未决关联；见[Session](interaction/session-and-task.md) |
| **Task**／固定逻辑 Orchestrator | task_id、submit_command_id、goal_revision；关联 Decision、已准入意图及准确成果 | 接纳、修订、控制、准入、条件选择与完成各沿原短事务；进程接替不改负责方 | 应用／SDK 用 task.read、task.result；内核读取完整关联 | 目标跨交互与工作进程持续；终态后仍有未知效果、费用或证据缺陷说明；见[任务记录](orchestrator/README.md#records) |
| **Decision**／Brain；输入来源由 Orchestrator 固定 | decision_id 对唯一输入摘要；0..1 ModelCall、固定配置和 Proposal | 接纳与推进责任共同提交；可能发送前保存原调用及门禁事实；输出终态不关闭迟到计费 | Orchestrator 通过 brain.get 取原判断和用量，应用取得必要说明 | Task 取消时模型可能已计费；原输入与发送事实须可追溯；见[Brain 对象](brain/implementation.md#data-flow) |
| **Operation**／Executor；准入意图由 Orchestrator 保存 | operation_id 对固定 Invoke，关联 Task、原目标修订、能力与绑定；下辖尝试、效果及结果 | 接纳不等于启动；可能发送前持久标记，效果与用量按原身份归并；closed 不证明外部已停止 | Orchestrator／获准调用者通过 execution.get 查询，目标证据由 Executor 核对 | 外部效果可晚于任务终态，取消与未知不能丢失；见[执行契约](execution/README.md) |
| **Content**／原内容 owner；由记忆与内容端口承载 | ContentRef 固定 owner、content_id、version、hash；其他对象引用准确版本 | 字节先保存，元数据／来源／使用条件后发布；关闭、禁用与副本清理分别记录 | 各模块经原内容读取或受控交付端口，当前用途与来源条件每次复查 | 同一正文可被多个对象引用；正文到期、禁止使用、物理副本清理不同步；见[内容存储](memory/implementation.md#data-flow) |
| **Grant**／原 Grant owner | grant_id 与修订、父链；原 use_id 绑定准确意图及有限业务操作 | 签发、撤销、原使用裁决与数值结算遵守各自成功点；同 owner 的必需许可可原子占用 | 执行组件使用 grant.check、grant.use、grant.use.get 与 grant.use.settle；应用查看授权与限制 | 可跨 Task 存在，撤销不改写已发生的使用；使用与结算不随 Task 终态清理；见[授权对象](security/implementation.md#data-flow) |

六组中的“组织”不等于把全部写权移动到一个 owner：Task 不裁决 Executor 的效果，Decision 不改写 Task 输入快照，Operation 不替 Grant owner 结算许可。应用组合读取这些事实，不创建第七份可独立修改的“整体运行成功”。

<a id="task-records"></a>
## 3. Task 内部记录与跨对象关联

本表的默认负责方是原 Orchestrator；原目标、条件和完整关联必须从[任务存储与恢复](orchestrator/implementation.md#data-flow)取得。表内“子记录”表示由 Task 接口组织，不表示可随父正文级联删除。

| 记录／类别 | 身份与关联 | 生命周期及事务／持久性 | 查询者与保留 | 独立记录或保留语义的理由 |
| --- | --- | --- | --- | --- |
| 目标修订、Requirement／内部 | task_id、goal_revision、requirement_id；准确来源和 rule_ref | 条件真正变化时先提交新目标修订，本提案其余行为失效；当前条件及后续责任共同提交 | Task 读者及 Brain；保留解释已准入行动和 Result 所需的历史 | 并发修订不能让旧提案作用于新目标；遵守[ADR-0006](../../adr/0006-adopt-requirements-before-actions.md) |
| Plan 与步骤准入／内部 | task_id、plan_id、plan_revision、step_id；准确计划正文与目标修订 | 当前计划指针与安装决定一起保存；每步骤仅准入一次，固定对应 operation／delegation | Orchestrator 调度，Brain 读获准投影；保留原步骤和准入关系 | 计划是有界建议的组织，不是另一个调度或完成权威 |
| Snapshot／BrainContext／固定输入投影 | task_id、snapshot_revision、context_ref 与精确依赖摘要；Decision 固定引用 | 从当前事实和获准历史重建；交付决策前固定准确版本，不能原地改写 | Brain 与诊断查询；已调用输入随原决策保留，正文依许可清理 | 组装算法可重跑，已使用的准确输入不能由“最新状态”替换；详见[输入重建](brain/implementation.md#snapshot-reconstruction) |
| 决策消费记录／内部 | task_id、decision_id、输入修订、采纳／失效原因 | 核对当前修订并至多一次消费；和所产生的任务决定及后续工作共同提交 | Orchestrator 恢复及任务解释；保留防重复采纳依据 | Brain.completed 只证明提案形成，不能证明 Task 已采纳 |
| OperationIntent／内部交接记录 | operation_id、Task 与准入来源、固定 Invoke、原 command_id | 固定规范化参数、能力、费用预留及 dispatch 责任后才发出；准入后不改意图 | Orchestrator；Executor 接收原 Invoke；保留至未决效果与账务收束并保留最小身份 | 原意图与 Executor 的接纳是不同负责方的事实，不能被一条跨库“Operation 行”代替 |
| received_facts、open_effects 关联／内部与当前投影 | 原 owner、object_id、revision、摘要；完整准入全集关联 | 接收事实去重；未结集合与 Task 修订在归并事务更新，重建未完成时禁止据此完成任务 | Task 完成判断及进度查询；保留未结对象和事实来源 | 投影可重建，但不能由可丢通知或截断公开数组推断不存在未知效果 |
| ConditionResult、目标覆盖核验／内部 | check_id 或 coverage_revision；原 goal_revision、Requirement、准确成果、规则与实现 | 原判断不可改写；当前选择及适用性与核验责任共同提交 | Orchestrator 选择完成依据；Result 读者查看所选证据及限制 | 同一条件可以多次检查；旧判断与当前适用性不同，不能仅剩 pass 位；见[条件存储](orchestrator/implementation.md#condition-storage) |
| Result 与终态说明／内部 | Task 唯一固定成功 Result、所选条件及准确成果；失败／取消另有结束说明 | 仅成功路径发布 Result；最终裁决与任务终态共同提交，后续账单不改写原结果 | task.result 与应用；正文按 Content 规则，最小终态长期保留 | 回复文本不是完成依据；成功、失败和取消不能共用一个假成功 Result |
| 结果证据失效说明／独立后续记录 | Task、原 check／coverage、defect_id | 受信缺陷命中后追加；不重开已成功 Task、不覆盖固定 Result | 原结果读者与维护者；与被影响证据保留关联 | 任务结束后仍需解释新发现的判断缺陷；遵守[ADR-0005](../../adr/0005-evaluator-evidence-eligibility.md) |
| 控制、等待与有界进展／内部 | task_id、control_revision、具体等待对象；原累计次数／费用／期限 | 目标状态、暂停、等待与出站资格分别保存；新事实与继续决定共同提交 | 应用看控制落实缺口，内核核对当前门禁；跨重启保留约束 | active 不代表可新启动；重复相同事实不重置无进展计数，收尾仍可继续 |
| Budget、reservation、费用归并／内部账务 | Task、计价单位、reservation_id、唯一账单来源与用量修订 | 准入预留与意图可同事务；按原计费来源累计差额入账，费用更正与后续责任共同提交 | Task／运行组件；未结预留、迟到更正与超额说明不随终态消失 | Task 预算是约束和归并，不再生成一笔 Brain／Executor／Grant 已存在的收费 |

TaskPolicy、验证规则、Evaluator 的适用资格是配置与证据门禁，见[配置清单](#configuration)。跨端 allocation 与接收门禁属于使用该能力时的独立交接记录，见[可选扩展](#extensions)；它们不能因为显示在 Task 预算中就失去双方保留责任。

<a id="decision-operation-records"></a>
## 4. Decision 与 Operation 内部记录

| 记录／负责方 | 身份与关联 | 生命周期及事务／持久性 | 查询者与保留 | 独立记录或保留语义的理由 |
| --- | --- | --- | --- | --- |
| DecisionInput／Brain | 原 decision_id，固定 context_ref、输入摘要、配置、限制和来源清单 | 接纳绑定后不可换输入；获准保存的正文引用先有效 | brain.get 及原调用诊断；正文按来源策略，原输入摘要随闭合记录保留 | Brain 取得的准确输入必须和 Task 后续变化分开 |
| ModelCall 与发送事实／Brain | 唯一 model_call_id、原 Decision、供应商号、use_id、实际字段和编码摘要 | prepared 与可能 sent 分开；每 Decision 至多一次物理请求；不明确未发送时不透明重发 | 原 Brain 查询和计费核对；取消后保留未知调用、实际用量及最小终态 | 确定性 Decision 可无 ModelCall；标题／压缩／修复等额外模型请求另行准入、计费，不能隐藏为重试 |
| Proposal、publication／Brain | 原 Decision；固定输出；局部产出到 ContentRef、upload_id、保存命令的映射 | 外部保存前固定映射和恢复责任；正文保存完成后才发布引用；终态固定 | Orchestrator 采纳，应用查看必要解释；产物与原生成关系按许可保留 | 产出成功与提案采纳不同；恢复原保存身份不能生成第二份成果 |
| 模型 usage 与 billing outbox／Brain | model_call_id、计费项、usage_revision、原 Task/use 关联 | 原账单与待交回责任共同保存，任务只记累计差额 | Brain、Grant owner、Orchestrator 按固定账单来源查询；迟到上调继续交回 | 模型终态不证明账务已最终封闭；这些记录是子结构但有独立收尾 |
| Operation 接纳与控制／Executor | operation_id、原 Invoke；TaskGate、控制回执、取消墓碑保留原 Task 与修订 | 接纳记录、回执与工作共同提交；启动检查当前 gate；未知原操作的取消先保留墓碑 | execution.get、control.get；未落实入口和在途集合继续核对 | 取消答复不是物理停止；task.status 与 operation.execution_state 不可合并 |
| Attempt、目标凭据及 Effect／Executor | operation_id 下的实际尝试、目标关联键与证据引用 | 可能外部发送前固定原尝试；效果含 unknown 及 may_apply_later；安全重试按准确能力合同 | 原执行查询及独立证据核对；效果未知时保留可恢复依据 | 工作进程接替不是重试许可；尝试失败不能推出外部动作未发生 |
| 原工具结果、Observation、呈现派生／Executor 与 Content owner | 原 operation／attempt、准确结果 ContentRef；GUI 观察另有 resource 与 control_epoch | 原结果覆盖与来源先固定，再生成有界呈现；观察有适用窗口 | Brain 取得获准输入，用户读准确结果；副本与正文按 Content 规则 | 摘要、截图缺口或工具成功字符串不能替代原结果、目标证据和资源前置条件 |
| 执行 usage、交回责任／Executor | 原 operation、计价项、账单来源、用量修订 | 用量与可靠交回共同保存；normal final 后仍可接收可信上调 | Orchestrator 与 Grant owner 读原账；不因 Operation.closed 丢弃更正 | 执行阶段、效果和计费是独立维度，不需要三个面向应用的 CRUD 入口 |

具体持久顺序分别见[Brain 持久化](brain/implementation.md)、[执行实现](execution/implementation.md)及[可靠发送边界](reliable-work.md#durable-before-send)。同一个聚合查询可以组合这些子结构，但不能用一位 completed 同时表达提案已产出、行动已执行、账务已封闭。

<a id="supporting-objects"></a>
## 5. 不能随六组对象级联消失的辅助责任

| 对象／分类与负责方 | 身份与关联 | 生命周期及事务／持久性 | 查询者与保留 | 为什么保留独立责任 |
| --- | --- | --- | --- | --- |
| Message／Session 内部 | 原会话、消息身份及顺序、准确正文、原命令或输入关联 | 已保存消息与业务接纳分别成立；跨库转交先保存原命令及 outbox | 应用会话查询；正文清理后仍保留未决交接与必要来源关联 | 消息只是提交内容，不能自行证明 Task 接纳或输入消费 |
| InputSubmission／Interaction 辅助记录 | input_id、request_id／revision、target_command_id、answer_ref、preview_refs | queued 接纳后持久转交；sending 可能已经消费，撤回请求不保证阻止消费 | interaction.input_read；未收束输入和目标映射不随 Surface 清理 | 同一 Task 可有多个输入，需独立排队、查询和撤回；当前合同范围见[交互接口](interaction/README.md) |
| InputRequest／实际业务 owner 辅助对象 | request_id、revision、kind、准确问题、候选和允许动作 | owner 创建和修订；与真实业务改变同事务一次消费，旧修订拒绝 | 受信界面、interaction.request_read 与业务查询；保留原消费及去重依据 | 回答任务澄清、接受成果和应用输入有各自业务裁决；不会都归 Session 所有 |
| Confirmation／实际业务 owner 辅助对象 | confirmation_id、consumer_method、原 consumer_command_id、intent_hash、本人决定 | 原命令先固定；受信决定由 owner 保存，并在原业务事务一次消费 | 受信界面与原业务入口；已消费决定和最小身份按原命令保留 | 不是普通 Message，也不能统一移到 Grant owner 跨库消费；[正文预览](../../adr/0008-trusted-renderer-preview.md)不证明用户阅读 |
| Surface、SurfaceSnapshot／应用 owner 辅助对象 | surface_id、app_binding、可选 task_ref、revision；快照记录来源修订 | Surface 可以独立存在；快照提交后发布正式提示，权限变化后重新披露 | interaction.surface_read；未收束输入独立于界面正文保留 | 界面本身有生命周期；展示 Task 的部分才是任务投影，不能把整个 Surface 当 Task 子行 |
| Presentation／设备本端呈现记录 | surface_id、endpoint_id、intent_revision、open、seen_revision | 当前打开／关闭意图按修订覆盖，迟到输出不得重开；遥测可按本端策略清理 | Renderer 与设备恢复；不是本人确认依据 | 关闭窗口不同于归档 Session 或取消 Task |
| Grant use、UseReceipt／Grant owner 辅助账务 | use_id、准确 intent_hash、原有限操作、许可修订与 start_before | 同 owner 核验并占用；回执不可变，答复丢失查询原 use_id | 原调用者用 grant.use.get；消费事实不随 Grant 撤销或 Task 终态删除 | 一次许可已消费而动作未启动是合法状态；once 不因零费用返还 |
| UseSettlement 与用量修订／Grant owner 辅助账务 | 原 use、operation、usage_owner、reserved／spent／held／released | 结算独立于原回执；unknown 保留预留，正常封账与可信迟到更正分别处理 | 调用者、账务查询与 Task 归并；更正继续关联原记录 | 不可变批准与累计费用不同；释放余额不恢复一次性许可 |
| Content 使用条件、来源关联、禁用记录／原 owner | 准确 ContentRef、source_edges、用途与 policy、control_revision | 版本字节不变但当前可用性可收紧；关闭先持久，传播与清理继续 | 全部内容读者及持有者；正文清理后保留最小禁用关系 | 引用不是授权；摘要与派生内容不能脱离来源限制 |
| 内容副本、reference_intent、holder gate／原内容 owner 与登记持有者 | 固定 copy_id、准确原 ContentRef、用途、接收方、保留期限及原登记命令 | 跨库先登记再发布；持有者保存关闭门禁与持续核对责任；停止使用和物理清理分别确认 | 原 owner 及登记持有者；直至原责任收束，残留仍报告 | 内容引用或父 Task 消失不能证明副本已删除；跨库交接不能省略 |

已有 `interaction.input_withdraw` 针对绑定原 InputRequest 的 InputSubmission：queued 可原子撤回，sending 以后保存撤回意图并核对是否已经消费。它不等于 Session 自由输入的多 Lane、steering／follow-up 或通用 Run 撤回协议；后者需要另行定义提交目标、投递时机与控制范围。不能把现有撤回能力写成不存在，也不能将全 Task 取消充当某条自由输入的撤回。

内容用途保存在类型化授权和持有者记录中，不由 Content 正文或 Session 的 metadata 隐式决定。正式快照与暂存 token 也不同：provisional 片段可有界合并或丢弃，不逐 token 保存业务作业；正式结果和快照从原对象读取恢复，见[交互呈现](interaction/implementation.md#surface-publication)。

<a id="projections-and-framework"></a>
## 6. 投影和公共执行记录

| 记录／负责方 | 身份、来源与生命周期 | 持久性、查询与保留 | 保留或不独立建模的理由 |
| --- | --- | --- | --- |
| Turn／Run 展示分组／应用 | 由原提交、输入消费、Task、Decision、回复关联分组，结束事件保留原提交来源 | 可重建；显示接纳、排队、消费和完成各自事实；不新增公开 run_id 或生命周期端口 | 单个 task.status 无法表达多个提交的控制范围，但无需平行任务状态机 |
| 会话摘要、模型历史视图／应用与 Orchestrator | 准确历史来源、截止位置与摘要版本；获准用于某次输入 | 缓存可重建；已用于模型的版本按原 Decision 固定；当前来源许可仍适用 | 压缩改变读取组织，不改写 Requirement、Effect、Grant 或原消息 |
| 列表、集合页、搜索索引／各数据 owner | 原对象修订、有界成员集合、查询身份／期限与完整性缺口 | 按原查询合同保留有限集合和水位，过期重新查询；披露时复查权限 | 不能把截断列表、缓存未命中或提示丢失当作权威不存在 |
| Command 与 Receipt／实际业务 owner；发送者保存原请求 | 原 logical_service、command_id、规范摘要及固定目标；accepted 可向最终阶段推进 | 和业务决定／持久责任共同提交；原回执查询恢复，正文到期保留最低限度身份 | 一次网络请求或一次工作领取不产生新的业务命令；重复传送不重复裁决 |
| Job、Claim／原本地事务范围的 JobStore | 稳定业务责任键、job_id、work_revision、lease_epoch；关联原领域对象 | 事实与新增责任共同提交；Claim 只代表有限处理权，过期不删除业务责任；按域条件回收 | 作业版本用于新责任与旧完成竞争，不能替代目标状态或外部成功判断 |
| outbox、Delivery／原发送者与接收者 | 原目标、原命令／delivery_id、固定 payload 摘要和交付阶段 | 发送前持久保存，丢答复查询原身份；接收回执与业务消费分开；未决持续保留 | 可靠交付只是交接，不需要独立业务 Run，也不能从传输 ACK 推出成功 |
| 去重、取消、终态最小记录／各原 owner | 原标识、域化摘要、决定类别及必要修订 | 长期保留；只有其他终态记录足以拒绝原标识时才能合并；正文按各自政策清理 | 防止旧备份和迟到请求重新创建已关闭责任；遵守[ADR-0001](../../adr/0001-retain-closed-identities.md) |

框架提供持久接纳、有限领取和条件完成的共用机制；领域提供该次工作是否已经完成的判断。详见[公共工作模板](reliable-work.md)与[ADR-0009](../../adr/0009-reliable-work-framework.md)。应用开发者不负责为每次调用创建 Job 或操作 Claim。

<a id="configuration"></a>
## 7. 配置与绑定

| 对象／负责方 | 身份及生命周期 | 持久性、查询与保留 | 独立存在理由 |
| --- | --- | --- | --- |
| TaskPolicy、受信策略接受记录／Orchestrator 配置入口 | 固定 policy_ref、适用主体和范围；估算费用等接受事实绑定原 Task | 策略版本与受信接受依据可核验；准入读取，历史随原使用保留 | 模型或请求正文不能自报已接受风险；配置引用不替代本人决定 |
| Capability、Binding／Executor 与受信能力目录 | capability_id／version／digest、binding_id／revision、准确目标与驱动配置 | 静态装配也固定声明；原 Operation 保留准入组合，新动作复查当前可用性 | 工具同名不等价；刷新目录不重绑原行动，不额外引入公共目录版本类型 |
| ModelProfile／Brain 与宿主装配 | profile_id／version、适配器、模型及处理位置 | Decision 固定准确配置，真正发送前查当前资格；历史配置随原调用保留 | 滚动模型别名不能代替调用版本，配置未变不代表当前许可仍有效 |
| rule、Evaluator 资格与缺陷门禁／任务核验及评测维护者 | 固定规则／实现／适用范围，门禁修订与 defect_id | 资格检查、缺陷事实及影响工作按原协议持久；核验读当前门禁 | 新调用停用和旧证据失效不同；不将评测发布全量加入普通确定性检查 |
| InstallLock、组件绑定和当前 activation／扩展宿主 | 不可变安装清单；绑定、激活决定及实例就绪分别关联准确组合 | 开始前固定原组合；ready 属于当前实例及配置，恢复原 binding 不随意升级 | 任务恢复依赖准确版本；有批准不等于实例 ready；详见[宿主装配](extensions/implementation.md#staged-readiness) |

配置引用是核心调用的必要输入；动态搜索、插件管理和热切换是按需能力。默认小目录可以直接装配，仍保留准确版本与权限检查，见[渐进发现](extensions/progressive-discovery.md)。

<a id="extensions"></a>
## 8. 按需启用的独立扩展

这些对象的生命周期不能压成 Task 的附加文本。表中表示设计归属与保留条件，不表示本轮发布了相应公共协议或运行实现。

| 能力／负责方与对象 | 身份与关联、生命周期 | 持久性、读者与保留 | 独立存在理由及边界 |
| --- | --- | --- | --- |
| 分支历史／Interaction：父消息、分支头及来源截止位置 | 分支引用准确来源、配置与摘要范围；不复制活动 Operation、授权使用、预算或执行责任 | 启用时保存不可变来源及当前分支选择；应用查询；按内容许可保留 | 首版线性消息是明确子集；fork 不是回滚工作区或外部效果，完整支持单独交付 |
| 子 Agent／协作：Delegation、子 Task、可复用子 Session | 固定父子关联、启动配置与原命令；子会话持续身份与一次激活分开 | 委派与子任务接纳按原本地或跨域协议；读／等待／取消各自保留原责任 | 等待超时不取消；一次激活结束不删除可复用 child；见[冷恢复](collaboration/implementation.md#cold-child-recovery) |
| 未来或周期触发／宿主应用调度能力：Schedule、occurrence | 规则、启停、每次触发身份及原命令关联；可产生多个 Task 或输入 | 启用时先保存 occurrence 和待交付命令；查询可区分计划与已接纳工作；旧触发去重依据保留 | JobStore 处理执行责任，不能替代用户可管理的调度语义；本轮无公共 Schedule 协议 |
| 可复用执行环境／Executor：环境身份、占用与检查点 | kernel／后台进程／设备环境可跨多个 Operation；保留实例、配置和当前占用 | 环境登记与实际停止分别核实；Executor 查询；未知退出不释放占用，检查点注明范围 | 逻辑取消不证明资源退出；计算检查点不恢复外部效果，不默认反序列化任意对象；见[程序化工具](execution/programmatic-tools.md) |
| 跨任务 Memory／记忆模块 | memory_id、修订、来源、适用范围、当前使用条件、纠正关系 | 修订与来源／索引推进责任共同提交；获准检索和管理；正文关闭后保留必要影响关系 | Content 保存正文，不能代替长期保存许可和纠正生命周期；普通偏好修订不走软件发布 |
| Skill、Plugin 与安装／扩展模块 | 准确制品、安装锁定清单、加载／启用与实例资格 | 管理命令与后续责任共同保存；宿主查询；活动任务依赖未释放前不删除原组合 | 跨多个任务存在；未使用能力不进入普通调用主线，插件内容不自动获得许可 |
| 改善评测与发布／评测及扩展模块 | Candidate、EvaluationRun、SampleRun、Report、ReleaseApproval、逐目标激活 | 原冻结计划和样本责任可恢复；评测读者按权限取报告；批准与实际激活独立保留 | 一次 Task 成功不证明方案总体改善；软件／Skill 通用改善主张沿原独立评测路径 |
| 跨端预算、离线额度／Orchestrator、Grant owner 与原接收方 | allocation_id／lease_id、接收实例、原使用和累计关闭依据 | 双方各自持久；未确认封账不回收未知额度，更正继续按原身份交回 | 显示在核心预算中也不能合成跨库原子余额；未启用时普通同端路径不需要该交接 |
| 设备身份与占用／权限及资源 owner | endpoint、instance、凭据代次、配对会话、resource lease 与 control_epoch | 认证／撤销和占用均由原 owner 裁决；接入和执行读取当前资格；未知在途继续保留 | 登录会话不是对话 Session；重新配对不继承旧使用与额度，失联不能证明设备空闲 |

<a id="storage-boundaries"></a>
## 9. 默认逻辑聚合与可合并范围

默认应用接口围绕提交、原输入、控制和结果组合现有端口。默认存储让同一次领域裁决涉及的字段和子记录共同提交，不要求每条记录一个事务；独立读取或保留要求可以由关联记录承载。实现仍可为索引、并发和清理拆表，拆表本身不改变原子性。

| 可共同组织的记录 | 合并条件 | 必须保持的边界 |
| --- | --- | --- |
| Session 消息、原提交关联、交付 outbox | 同一应用 owner／本地事务范围 | Task 接纳后再补关联是可恢复交接；跨库不虚构原子提交 |
| Task 修订、条件选择、消费记录、预留、意图和 Job | 该次领域决定需要的记录共享声明的事务句柄 | 目标变化后本提案余部失效；外部请求不放在可重跑事务闭包中 |
| Decision 接纳及子记录、阶段变更和 Brain Job | 原 Brain 本地范围 | 接纳、可能发送、返回和发布仍按真实先后；不能预先保存“已返回”来减少提交 |
| Operation 接纳、原回执、工作；阶段中的尝试／用量／交回责任 | 原 Executor 本地范围 | Orchestrator 的意图、Grant 裁决和外部效果各自有真实边界 |
| Grant 同 owner 的必需使用、计数和结算更新 | 原 owner 的共同本地事务；按对应阶段写入 | 跨 owner 许可只有全部有限依据齐备才可启动；回执不改成可变结算行 |
| 准确正文引用及派生索引 | 原字节已持久且引用发布、持有者登记门禁满足 | 对象存储与数据库无默认原子性，原正文只保存一份不取消副本清理责任 |
| Grant 使用检查与实际启动登记 | 既有协议允许且双方位于同一受信本地事务范围 | 模块边界不强制拆事务；范围之外按有限使用回执交接，不能声称全局原子 |

内存快照、列表缓存、流式片段不新增业务权威。可重建数据若参与当前完成判断，必须从完整原事实重建并验证水位；有缺口时返回待核验。保留准确原事实一次，与保存必要物化索引并不冲突。

最小开发装配可将逻辑模块放在同一进程和权威数据库；生产仍遵守[ADR-0003](../../adr/0003-production-distributed.md)的接入、应用、工作池与持久目标。原命令固定目标及 Task 的逻辑 Orchestrator 不变，见[ADR-0004](../../adr/0004-discovery-fixed-task-routing.md)。本页不预测 SQL 次数、fsync、物理 IO 或时延下降；这些须由实际存储实现测量。

<a id="identity-example"></a>
## 10. 用一次文件任务区分身份

用户在会话 S1 提交“生成报告并保存”，应用保存消息 M1、原提交命令 C1 及目标 Orchestrator。即使接纳答复丢失，仍查询 C1，得到原任务 T1。界面可以将 M1 与后续回复显示为一个 Turn；这个分组不裁决 T1 是否已经完成。

T1 经决策 D1 询问保存目录。用户回答形成新的消息和 InputSubmission，绑定原 InputRequest 的准确版本；业务 owner 一次消费后继续 T1。回答产生新的交互回合，却没有新的目标。普通批注只保存消息，不能隐式生成模型调用或可信确认。

目录明确后，Orchestrator 形成新的固定快照和决策 D2。若 D2 使用模型，Brain 为它保存唯一 ModelCall；若是确定性处理，可以没有模型请求。工具行动以 O1 的准入意图交给 Executor，实际发送尝试保留 O1 的身份。写入后失联，工作进程接替仍核对 O1；不能因为“新 Run”而重发一次无幂等保证的写操作。

独立读回使用自己的获准 Operation 和实际证据，Task 再选择适用于当前目标、成果的 ConditionResult 并发布 Result。此时费用可能仍未最终结算；取消、归档、模型完成、Operation.closed、Task.succeeded 与进程退出都不是同一个事实。用户后来在 S1 提出另一个独立目标，才创建 T2；旧输入、取消与结束事件仍指向各自原对象，不能覆盖 T2。

该场景通过已有应用／SDK 工作流观察原提交、Task／Operation 身份、准确成果和外部效果。当前交付仅有文档结构与语义审查；数据库、真实模型、设备隔离和恢复行为须按[贯穿验收场景](validation/harness-scenarios.md)执行后登记，不能从本清单推导已经验证。
