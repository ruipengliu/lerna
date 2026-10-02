# 核心模型收敛的行为验收

[验收总入口](README.md) · [应用／SDK 工作流](../application-workflow.md) · [四类数据流程](../request-data-flows.md) · [规格追踪](core-model-traceability.md)

本页把六组对象的设计收敛接到既有行为验收，不另建测试专用入口。CM-01～17 是可复用的场景组；每组引用已有 HAR、FW 或模块向量，只补充原身份、继续条件和能力范围等断言。文档交付没有运行内核、SDK、数据库或平台实现，**全部运行场景均待实施、未验证**。完整分支、自由聊天输入调度、可复用 child、Schedule 和执行环境还须先完成各自能力合同。

规则以所链接的负责模块为准；本页只组织刺激、观察和证据。设计承载或声明缺口可以完成本轮文档验收，不等于能力已经启用。未启用或缺少必需证据的运行项登记为未启用／未验证，不能计入通过数。

<a id="observation"></a>
## 1. 从应用入口观察

主切面是已规划的应用／SDK 动作：提交新目标或原输入、读取接纳与进度、修订与控制、取得结果及恢复原对象。Session 动作由应用承担；Task／Result、原输入与内容分别用既有 task.read、task.result、interaction.input_read、interaction.request_read 和 content.get 查询。interaction.request_read 读取 InputRequest；interaction.input_read 读取 InputSubmission，两者不能互换。

只有应用结果无法区分时，沿原 brain.get、execution.get、委派、Grant 使用／结算及可靠工作合同补充观察。目标文件、供应方出站记录、资源实际停止及费用来源是独立真值；故障注入可放在存储或提供方适配器，预期仍由这些边界判断。不能用私有函数顺序、表数量、手工填入的状态或模型自评替代结果。

每项运行先固定合法正常路径，再注入故障。正常路径必须成功；全部拒绝不能证明恢复或隔离正确。只记录本例实际产生的对象与使用，不要求每条请求都出现六种对象或全部扩展。

| 固定项 | 运行前记录 |
| --- | --- |
| 输入与能力 | 原目标、历史及来源版本，当前授权、预算和继续条件，准确模型配置、Capability／Binding／InstallLock，开放的可选能力 |
| 原身份 | 原 Command 与逻辑负责服务；已有 Session／Task／InputSubmission／Decision／Operation／Delegation／use 关联 |
| 实验装配 | 实现提交、OS／数据库／持久化和同步配置、提供方或固定回放、独立目标初态、输出分块、注入位置及提交顺序 |
| 判定 | 应成功或拒绝的具体条件、目标真值、实际出站计数、准确结果与来源、费用归属、停止与未知项的可见方式 |
| 证据 | 原查询与回执、可复核目标观测、实现日志中的必要关联、实际断言、未决责任、计量原值及限制；正文仍遵守用途与保留规则 |

<a id="baseline"></a>
## 2. 四类基础请求

A、B、D 使用[请求数据流程的固定条件](../request-data-flows.md#scenarios)：暖 Session、准确 Content 引用、固定模型回放与配置，无标题、压缩、子任务或记忆提取，必要条件可用本地确定规则核验。新增质量评估或其他辅助行动时另列变体，不能为了守住调用数跳过核验。

<a id="cm-01"></a>
### CM-01：A，直接回答

从应用提交一个新目标，沿原接纳结果读取 Task、Result 和成果 Content。正常路径是一个 Task、一个有模型的 Decision、一次物理模型请求和零工具 Operation；最终回答、条件依据及限制属于同一目标修订，只有完成门禁满足后才显示成功。应用无需创建 Snapshot、ModelCall、Attempt 或 Job。

在内容尚未发布和 Task 提交前分别中断，再做“Task 已提交、回执丢失”对照。未接纳时不显示已接受目标，已接纳时沿原命令找回同一 Task；正式成果引用只能指向已经发布的准确内容。默认 goal_ref 仍是 ContentRef，有界内联只适用于既有 Schema 声明的字段。

设计：[A 流程](../request-data-flows.md#scenario-a)、[完成依据](../orchestrator/README.md#completion-basis)。复用 [HAR-04](harness-scenarios.md#har-04)、[RT-01／02](../orchestrator/implementation.md#11-可执行故障实验)、[BI-14](../brain/implementation.md#8-可重复故障实验)。状态：待实现运行。

<a id="cm-02"></a>
### CM-02：B，读取后回答

用户要求读取一个已知短文本文件再回答。第一轮提出读取，第二轮使用实际返回的获准文本作答；正常路径为两次物理模型请求、一个只读 Operation，业务文件读取与 Harness 元数据查询分别计量。通过独立目标内容核对回答所用的版本、范围及准确原结果，不能仅核对工具展示文本。

分别返回合法空文件、完整文件、缺页、截断、旧缓存及结果保存失败。允许的部分读取须显示实际范围；缺失项不能被解释为完整空集。原操作接纳不自动表示读取成功，后续模型和完成仍由 Task 准入；额外读取或模型请求按实际计入变体。

设计：[B 流程](../request-data-flows.md#scenario-b)、[结果来源](../execution/README.md#result-provenance)。复用 [HAR-03](harness-scenarios.md#har-03)、[EX-26～31](../execution/implementation.md#11-故障断点与独立断言)。状态：待实现运行。

<a id="cm-03"></a>
### CM-03：C，冷恢复及恢复后继续

在 A、B、D 的指定断点终止进程：Task 已接纳但 Session 未补关联、输入 queued／sending、Brain 准备后或 send_started 后、文件写入后答复丢失、Task 终态后账单迟到、仅本端呈现缓存丢失。恢复时先读取原服务、命令及业务对象，按[冷恢复表](../request-data-flows.md#scenario-c)分别核对，不把所有断点合为一个“重试请求”。

| 观测段 | 终点与行为断言 | 计量限制 |
| --- | --- | --- |
| C 的重建阶段 | 历史、原关联和未决责任已恢复为可查询，具体缺口可见；截至此点不发起新的模型请求 | 与[历史研究 C](../../research/agent-harness-comparison/data-flow-io-comparison.md#1-统一场景与计数口径)的观测终点一致。读取、Claim 接替、恢复提交、原调用查询及字节按实际单列，不预设零写入或固定 IO |
| C 的恢复后继续阶段 | 在原控制、当前许可、预算及宿主就绪均满足时继续原工作；合法未发送的模型准备可沿原身份首次发送，已可能发送则查询或保留 unknown；原工具写不换身份重做 | 单独记录后续模型／工具请求、效果核对与收尾。不得把继续工作量混进历史研究的冷打开数值，或与 A／B／D 的剩余工作重复相加 |

仅缓存丢失时，重建呈现应为零新增模型和工具行动；这项行为断言不表示恢复存储 IO 为零。原服务不可达、来源不可取或本端原记录损坏时展示具体限制，不能借空列表、超时或新身份推定原工作没有发生。恢复可查询与允许新接纳分别计时。

复用 [HAR-04／11](harness-scenarios.md)、[FW-03／05／07](../reliable-work.md#validation)、[II-29／30](../interaction/implementation.md#input-recovery-validation)；发送和未知效果另复用 CM-08／09。状态：待实现运行；数据库耐久及平台就绪另验。

<a id="cm-04"></a>
### CM-04：D，生成、保存并独立读回

固定三轮模型回放：生成报告并提出保存、保存事实归并后提出读回、读回后给最终说明；分别准入写与读两个 Operation。由独立文件目标读取路径、版本和字节，按原报告及条件比较，再从 task.result 取得完成依据。返回写入参数或 Brain 缓存不算独立读回，存入 Content 也不等于用户指定文件已写入。

分别在文件替换前后、Executor 归并前及 Result 提交前中断，并注入用户改写文件或读回版本不符。只在证据适用时完成；unknown 沿原写 Operation 核对，不能为得到成功而重写。用户后来修改文件不改写“原写已发生”的历史，也不构成永久内容保证。

设计：[D 流程](../request-data-flows.md#scenario-d)、[条件核验](../orchestrator/verification.md)。复用 [SYS-02](README.md#scenarios)、[EX-19](../execution/implementation.md#11-故障断点与独立断言)、[BI-08／16](../brain/implementation.md#8-可重复故障实验)。状态：待实现运行；真实文件耐久另验。

## 3. 原身份与持续责任

<a id="cm-05"></a>
### CM-05：Session、提交与多个目标

在 S1 建立 T1、T2 两个独立目标，T1 等待澄清；回答其准确 InputRequest 只继续 T1，T2 独立推进。另从直接 API 建立无 Session 的 Task。对消息保存后未交付、Task 接纳后关联回写丢失分别重启，应用须区分消息已保存与任务已接纳，并沿原命令补回唯一关联。

归档、关闭、重开 S1 均不取消任务或授予新继续权；保存批注时没有隐式模型请求。T1 的输入、取消及终结事件迟到只更新 T1，不能盖住 T2 视图或清除 T2 的取消句柄。重复原提交不产生第二个目标，结果引用也不产生第二份预算和可写完成状态。

设计：[原提交交接](../application-workflow.md#original-submission)、[Session 分工](../interaction/session-and-task.md)。复用 [HAR-11](harness-scenarios.md#har-11)、[II-21～23／29](../interaction/implementation.md#input-recovery-validation)。状态：待实现运行；Session 仍是应用内部设计。

<a id="cm-06"></a>
### CM-06：每条输入的消费、撤回与投递范围

保持一个目标正在工作，提交绑定原 InputRequest 的回答 B，读取其 input_id。先令 B 保持 queued，再使撤回与 queued→sending 分别先提交；Claim 已取得也不能代替 sending 的业务决定。

| 路径 | 用户与原查询必须观察到 |
| --- | --- |
| queued 撤回胜出 | B 为 withdrawn、原目标发送为零；其他已接纳工作及输入不受影响 |
| sending 先胜出，原消费答复丢失 | 撤回仅为 withdrawal_requested，查询原 target_command_id 确认消费或拒绝；不伪称必然撤回，不新造回答 |
| 两设备回答同一请求或提交旧请求修订 | 原业务一次消费；另一输入返回冲突／明确拒绝，不能同时推进两个目标 |
| 直接 task.input | 按原业务合同消费；它没有 Interaction 队列，不能随后套用 interaction.input_withdraw |
| 无 InputRequest 的聊天 steering／follow-up／多 Lane | 当前记录 G-02 缺口，不假定已有投递或 Run 范围控制。完整开放前另定义身份、插入位置、优先级和撤回竞争；不以 task.cancel 代替只撤回 B |

设计：[输入范围](../application-workflow.md#input-scope)、[输入控制竞争](../interaction/implementation.md#input-control-races)。复用 [HAR-05](harness-scenarios.md#har-05)、[II-01～04／18／20](../interaction/implementation.md#input-recovery-validation)。状态：结构化回答待实现运行；自由聊天调度待独立合同与实现。

<a id="cm-07"></a>
### CM-07：目标状态、继续权、进展与完成

分别令 active 的 Task 已暂停、等用户、额度耗尽、许可不足或原发送者尚未隔离，然后重开会话。原事实仍按获准路径可读；不满足新启动条件时不增加模型费用或工具效果，收尾和已具备依据的完成裁决仍按原规则处理。合法补充输入、解除相应等待或明确控制后，原任务可以在当前门禁允许时继续。

重复交回同一反馈，并在无进展阈值前重启；原累计预算与次数不重置，有效新事实只归并一次。达到有限阈值后给出等待／结束依据及未决责任。并发修订目标或 Requirement，再交回旧提案；条件确有变化时先采纳条件和新修订，本提案余部不准入，旧效果和费用继续可查。

完成读取须包含当前目标、准确成果、所选 ConditionResult 及适用性；助手回复、用户接受或高质量分不能覆盖未知必要效果。成功之后新发现证据缺陷另作说明，固定 Result 和终态不被重写。

设计：[Task 状态](../orchestrator/README.md#state)、[有界推进](../orchestrator/implementation.md#bounded-progress)、[目标覆盖](../orchestrator/verification.md#goal-coverage)。复用 [HAR-02](harness-scenarios.md#har-02)、[RT-03／09／20～24](../orchestrator/implementation.md#11-可执行故障实验)、[核验生命周期实验](fault-experiments.md#verification-lifecycle)。状态：待实现运行；验证器质量另验。

<a id="cm-08"></a>
### CM-08：Decision 输入、模型调用及辅助推理

同一任务分别使用确定性 Decision 与有模型 Decision；前者可无 ModelCall，后者至多一次物理模型请求。开启标题、压缩或修复变体时，每个真实额外请求均有自己的准入、Decision 及计费关联，不能计作透明重试。

在 prepare 后与 send_started 后分别中断。确定未发送且当前门禁满足时，允许原首次发送；可能已发送时仅查原调用或保持 provider_result_unknown，不新建同义 Decision 掩盖不确定性。检查最终完整编码的来源和用途，包括附件、metadata、日志与包装器字段。三次摘要后修订目标并保留一个未知写入，当前硬约束、原效果责任及摘要来源仍可追溯；必需输入缺失不得猜造。

设计：[Brain 恢复](../brain/README.md#model-recovery)、[输入重建](../brain/implementation.md#snapshot-reconstruction)。复用 [HAR-01／04](harness-scenarios.md)、[BI-01～07／10～14／17～20](../brain/implementation.md#8-可重复故障实验)。状态：待实现运行；真实模型质量不由固定回放证明。

<a id="cm-09"></a>
### CM-09：Operation、未知效果、取消与迟到账务

一个写 Operation 在目标已生效后丢答复，恢复只核对原 operation_id、原尝试及目标证据。对具备准确幂等合同和不具备幂等保证的能力分别执行原模块向量；工作进程接替不自动授予重发许可。无法证明时显示 unknown／可能迟到效果，不能因工具报错或合成 interrupted 文本宣称未发生。

取消 Task 后交回原成功事实及更高可信账单，再重放通知。Task 保持原取消决定，原效果更新、费用只按同一来源追累计差额，预留按真实闭合依据处理；取消回执或 Operation.closed 不证明物理停止。查询同一组合视图应能同时解释目标终态、未结效果与账务，不另建一个可写的整体 completed。

设计：[执行恢复](../execution/README.md)、[原账务关系](../orchestrator/implementation.md#accounting-relations)。复用 [SYS-02／03](README.md#scenarios)、[FW-05／07](../reliable-work.md#validation)、[EX-01～03／20／29](../execution/implementation.md#11-故障断点与独立断言)、[RT-17～19](../orchestrator/implementation.md#11-可执行故障实验)。状态：待实现运行。

<a id="cm-10"></a>
### CM-10：Content 当前权限、可信确认与 Grant 结算

先取得合法内容和确认，再分别关闭来源、撤权、修订请求或改动最终意图，读取旧历史、摘要、Result 和旧 Surface。准确引用、hash 或 known_revision 均不延长使用许可；来源缺口可见，获准原效果核对和费用收尾不丢失，也不能借收尾权限处理新正文或执行新动作。

两设备竞争 approve／deny，并在确认消费与业务提交间中断。只有原命令可以一次消费本人决定；普通消息、模型同意和复制 preview_refs 不产生授权，也不证明 Renderer 已呈现。受信界面成功呈现、当前资格有效且原意图一致的正例须正常通过。

两个不同 use 竞争一次额度时仅一个获准；同 use 答复丢失沿原查询恢复。UseReceipt 不可变，结算追原累计费用；零费用或余额释放不返还 once，unknown 保留预留，迟到更正不重开原启动窗口。

设计：[内容与来源](../memory/implementation.md#reference-gate)、[确认竞争](../interaction/implementation.md#confirmation-races)、[Grant 使用](../security/implementation.md#5-一次使用与并发裁决)。复用 [HAR-05](harness-scenarios.md#har-05)、[II-05／10／24～26](../interaction/implementation.md#input-recovery-validation)、[S-I01～05／13～17／19／21／30／31](../security/implementation.md#9-故障实验与观察点)、[MI-07～10／18～22](../memory/validation.md#faults)。状态：待实现运行；Renderer 与真实出口另验。

## 4. 按需能力

本节先核对能力是否开放、负责模块和独立合同。仅声明设计时只完成静态审查；运行启用后仍须下列正反例和各原合同的完整验证，不能以相似字段或已有 JobStore 代填通过。

<a id="cm-11"></a>
### CM-11：历史分支、来源位置与外部世界

原路径在选定历史位置之后写入文件，且还有未决操作。选择旧位置并 fork，核对来源 Session、原条目、截止位置、父链／分支头、配置及摘要覆盖范围；只读历史选择不触发模型或写入，fork 不复制活动 Operation、待发命令、once 使用、未结预算或推进权。

原路径的文件仍保持实际状态；若用户明确要求恢复文件，另准入当前获授权的新操作。并发修改分支头、来源关闭和摘要只覆盖另一分支时，按明确冲突／缺口处理，不能静默拿正文副本补成完整历史。合法获准历史上的新目标可以正常工作，原 Task 仍归原 owner。

设计：[分支边界](../interaction/session-and-task.md#history-branches)、[SM-09／G-01](../../research/agent-harness-comparison/core-model-semantic-coverage.md#sm-09)。复用 [HAR-01／11](harness-scenarios.md)的来源与原任务断言，新增的分支头竞争待该能力切片实现。状态：完整分支与公共合同本阶段不交付。

<a id="cm-12"></a>
### CM-12：准确绑定、扩展就绪与实际隔离

原 Operation 已准入后刷新同名工具、配置或安装组合；原查询和恢复仍解释准确 Capability／Binding／InstallLock，不重绑新实现。当前资格失效时阻止新的相关调用；原绑定独立有效的正常路径不因发现服务暂断就全部拒绝。

先使配置 A 初始化，再切到 B，让 A 迟到成功；只允许当前组合及实例发布 ready，暂存或自检失败项不能派发。Skill 正文、安装包与 allow hook 均不产生 Grant。合法批准下仍注入路径、网络、凭证或子进程越界，真实出口须拒绝越界；批准记录不能代替平台隔离。未启用动态发现、安装或 Skill 的 A 路径不要求创建这些生命周期对象。

设计：[暂存就绪](../extensions/implementation.md#staged-readiness)、[Skill 材料](../extensions/README.md#skill-materials)、[最后工具使用](../security/implementation.md#final-tool-use-check)。复用 [HAR-03／07／10](harness-scenarios.md)、[EX-21～25](../execution/implementation.md#11-故障断点与独立断言)、[X-I03～11／14～16](../extensions/implementation.md#10-故障实验)及 [SEC-02](../security/README.md#boundary-validation)。状态：准确绑定待实现运行；动态扩展按需；隔离需平台证据。

<a id="cm-13"></a>
### CM-13：原父子关系、等待与多次激活

子效果发生、父未消费报告时终止父进程，修改当前插件配置后恢复，交回重复报告及迟到费用。仍是原 Delegation、唯一子 Task、原安装组合；结果与账单只归并一次。零等待、有限等待到期和本地停止等待不取消子任务；父取消也不抹去子效果与未结费用。

在可复用 child 能力开放的变体中，一次回复或激活结束后保留子 Session。旧目标工作已封闭且未知效果不阻碍新行动时，才准入新目标的 Delegation、Task 及受限 allocation，继续引用获准历史；旧激活的控制、结果和费用只回到旧映射。默认一个活动目标，跨父复用须有明确关联许可；原父终态不能自动接纳新目标。新激活不继承旧 once 或未结预算。

设计：[冷恢复](../collaboration/implementation.md#cold-child-recovery)、[有界等待](../collaboration/implementation.md#async-child)、[可复用 child](../collaboration/implementation.md#reusable-child)。复用 [HAR-06](harness-scenarios.md#har-06)、[CL-01～12／15／17／19～22](../collaboration/implementation.md#12-具体故障实验)。状态：单次委派与等待待实现运行；通用 child.send／reuse 和跨父复用待独立合同与实现。

<a id="cm-14"></a>
### CM-14：Schedule、重复触发与规则停用

启用调度能力后，固定规则版本、时区、错过／重叠策略和同一 occurrence，重复到期通知；在 Task 已接纳而调度端未补关联时终止进程。恢复仍查原命令与固定 Orchestrator，只产生一个已接纳目标，不能用 Job 的 due_at 重新生成同义触发。

编辑未来规则、停用并与发送准备交错。旧 occurrence 的准确载荷和已创建 Task 不变；未发送项停止，可能已发送项报告接纳未知并继续原查询，已接纳 Task 的取消另走明确控制。暂停 Task、过期许可、夏令时重复／缺失时刻及长时间停机均不导致越权恢复或无限补发；合法到期触发按冻结策略成功。

设计：[应用调度边界](../orchestrator/scheduled-triggers.md)、[SM-11／G-04](../../research/agent-harness-comparison/core-model-semantic-coverage.md#sm-11)。复用 [FW-01／03／04](../reliable-work.md#validation)及 [RT-01／02](../orchestrator/implementation.md#11-可执行故障实验)的接纳恢复；规则和 occurrence 行为是独立待运行断言。状态：公共 Schedule 协议与 scheduler 本阶段不交付。

<a id="cm-15"></a>
### CM-15：可复用环境、实际停止与检查点

启用有状态环境后，让 cell 返回 aborted 但进程树仍忙，随后申请同环境的冲突 Operation。仍保留原实例的占用与停止核对责任，不能因等待结束、Operation.closed 或 Claim 过期释放物理名额。实际退出证据成立后，合法新 Operation 使用自己的当前授权、来源和额度；新实例不消费旧回调。

在 host-call 接纳映射保存后及外部写入后分别中断，恢复被动数据检查点。只恢复声明的计算数据，查询原子调用而不重放未知效果；不宣称 socket、子进程或设备占用恢复。来源关闭后保留变量不能再次读取或外发，不可信对象反序列化应被拒绝，合法有界纯计算须通过。

设计：[环境生命周期](../execution/programmatic-tools.md#reusable-environment)。复用 [HAR-09](harness-scenarios.md#har-09)、[FW-06](../reliable-work.md#validation)及原平台隔离向量；X-06 与 X-08 分开登记。状态：环境／cell 控制合同、driver 和平台验证待独立交付，普通 Invoke／resource 方法不能代替完整 kernel 互操作。

<a id="cm-16"></a>
### CM-16：Memory 修订、来源关闭与普通纠正

只保存对话或 Content 后，从另一 Task 查询长期记忆，不得自动得到未取得长期保存许可的记录。正常偏好修订沿既有 Memory 授权直接提交；准备 before／after 候选后先更新原记忆，再应用旧候选，expected_revision 冲突不能覆盖新修订。

来源关闭、检索用途撤回、索引落后和批量部分失败分别注入。每项保留来源、范围、当前可用性及具体缺口，旧摘要不能恢复许可，整批不伪称原子成功。软件／Skill 的通用改善主张另按冻结评测和发布批准；普通用户偏好纠正不被强制转为发布流程。未启用跨任务 Memory 的基础 A 路径没有隐式提取与精炼模型请求。

设计：[记忆边界与修订](../memory/README.md)、[经验候选](../memory/implementation.md#experience-candidate)。复用 [HAR-08](harness-scenarios.md#har-08)、[MI-01～05／07～12／18／24～27](../memory/validation.md#faults)及[记忆策略反例](../memory/validation.md#strategy-cases)。状态：按需启用，待实现运行；改善质量与发布证据另验。

<a id="cm-17"></a>
### CM-17：可靠工作、共事务、投影及流呈现

沿 A、B、D 的同一应用行为，在已声明的本地共事务装配与独立负责方交接中分别执行 [FW-01～09](../reliable-work.md#validation)。提交前后中断、提交结果未知、旧 worker 复活、新责任与旧 Finish 竞争、通知全丢及迟到账单均从原业务查询恢复；原身份和累计限制不重置，应用不直接操作 Receipt、Job、Claim 或 outbox。

删去允许重建的呈现缓存并使完成判断投影落后。重建须保留准确来源与缺口，完整事实未取得时不能据“列表为空”完成任务；已用于 Decision 的固定输入不被最新投影改写。丢弃／合并 provisional 片段，丢正式提示并重复旧提示：从原对象和新 Surface 快照恢复，不重跑模型，不倒退新视图，正式提示不早于对应可读事实。

同事务裁决允许多条记录共同提交，跨 owner 不伪造全局原子；模型准备与 send_started、Executor 准备与实际入口等必要屏障保持各自含义。验收不规定表数、私有调用次序或逐 token 提交；实际事务与成本按下节报告。

设计：[事务合并边界](../request-data-flows.md#transaction-boundaries)、[可靠工作](../reliable-work.md)、[呈现发布](../interaction/implementation.md#surface-publication)。复用 [HAR-04／05](harness-scenarios.md)、[II-19／27～31](../interaction/implementation.md#input-recovery-validation)及[框架接入矩阵](fault-experiments.md#reliable-work-framework)。状态：待实现运行；真实 PostgreSQL／SQLite 和平台分别验证。

<a id="static-review"></a>
## 5. 本轮实际检查与变更门槛

以下是文档交付的静态检查入口，结果记录在[当前检查结果](../review.md)。它们不产生运行通过记录。

| 编号 | 静态检查内容与不通过条件 |
| --- | --- |
| CM-S1 对象与入口 | 核对[归属清单](../core-data-model.md)、应用入口和四条数据流程：核心、子记录、辅助责任、投影、配置、扩展及公共执行记录均有负责方与保留理由；把六组当六张表、把状态合成一位 completed 或要求应用逐项 CRUD 均不通过 |
| CM-S2 参考与能力 | 逐项核对[SM-01～16](../../research/agent-harness-comparison/core-model-semantic-coverage.md)及 G-01～07：五项目的固定来源、实际装配、生命周期和恢复差异有据可查；Pi 经典 CLI／AgentHarness／pi-durable、DeepSeek profile、Prime core／daemon 的保证不得拼接。设计／按需／本阶段不交付与运行状态分列 |
| CM-S3 契约差异 | 比较固定 review base 到交付提交的 methods.json、Schema、示例、harness.proto 和相关校验资产；当前仍为 105 个领域方法。无机器差异只登记差异检查，不声称重跑服务互操作；新增字段、方法、回执成功点、控制范围或恢复含义时，必须另交付同版登记、Schema、正反例、序列与互操作验证，未完成项记为缺口 |
| CM-S4 文档与 ADR | 检查规格 US-01～54、实施决策 ID-01～24 无缺号／重号，各项有真实设计及验收入口；运行本目录 check_documents.py、规格目录链接与结构检查、git diff --check。核对 CONTEXT、总览、模块、工程入口与 ADR-0001～0010，无静默替代；正式总览与工程入口保持同一当前规范 |
| CM-S5 证据与计量 | 报告分开设计映射、实际静态结果、运行机制、数据库／平台、模型质量及性能；未运行不填 pass。A／B／D 的模型数、工具数与持久阶段使用冻结条件，C 重建与继续分段；对象数或阶段数不得改写成 SQL／fsync／物理 IO 或下降比例 |

这里的“规格变更”包括故事、实施决策、启用范围、成功点和计量终点改变。变更后同步受影响追踪行、场景及负责模块；只修正文案或映射且机器资产未变时不扩大为运行测试。若与现行 ADR 冲突，先显式记录待替代决策，不以本页覆盖。参考源码索引依固定快照和原研究证据维护，本轮不改写 sources.json 或历史基线。

<a id="evidence"></a>
## 6. 证据状态与计量

| 证据层 | 本轮交付与当前状态 | 后续何时可以登记结果 |
| --- | --- | --- |
| 设计覆盖 | 本页与追踪表给出承载、缺口和预期行为；不代表全部能力开放 | 规则、入口和语义审查有明确依据，遗漏由对应票据补齐 |
| 文档静态 | 以票据所记实际命令、提交范围及输出为准 | 链接／结构、覆盖编号、方法差异和 whitespace 检查实际执行后，只登记相应静态结论 |
| 机制与契约运行 | CM-01～17 全部未实施运行；已有构造向量不替代工作流执行 | 参考实现及独立替换从原入口运行，报告 pass／fail／inconclusive 与实际证据 |
| 数据库耐久与并发 | 未运行 | PostgreSQL／SQLite 适配器在真实故障与提交交错下取得 FW 和领域证据 |
| 平台与生产 | 未运行 | 最小开发装配之外，按[生产验收](../deployment-production.md#6-发布观测与生产验收)取得进程分工、隔离、RPO／RTO、恢复和容量证据 |
| 模型与任务质量 | 未运行 | 沿[验收总入口](README.md)冻结独立真值、样本、分母与停止规则；固定回放不证明真实质量 |
| 性能与成本 | 未测量 | 固定相同输入、历史、输出分块、存储装配和断点，报告实际调用、字节、SQL／追加、事务、flush／sync、WAL、物理 IO 与时延 |

A／B／D 基线分别为 1／2／3 次模型请求、0／1／2 个工具 Operation；Brain 的 4／8／12 个基本持久阶段和 Executor 的 0／3／6 个基本阶段只是[设计分域阶段](../request-data-flows.md#measurement)。Task、内容发布、授权、资源入口、领取、呈现和迟到结算另有实际成本，合法共事务还会改变提交数；不能相加后当作请求总事务数。

每次对照分别报告初始化、稳态、C 重建、C 继续、辅助请求、流更新及收尾。业务目标读取与 Harness 元数据读取分开，远端查询成本、合法合并和多对象写入保留原单位。开发同进程通过不能替代生产分布式、当前权限或真实外部效果验证；测量完成前不填收益百分比。
