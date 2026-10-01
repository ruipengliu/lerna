# Harness 核心数据模型收敛与参考项目语义覆盖

Status: ready-for-agent
Type: spec
Created: 2026-10-01
Progress: completed

## Problem Statement

应用开发者希望通过少量稳定概念接入一个能够连续对话、调用工具、恢复任务并解释结果的 Harness。目前方案同时展开了业务对象、执行记录、可靠工作机制和可选平台能力。理解一次普通请求，需要经过 Session、Task、Snapshot、Decision、ModelCall、Proposal、OperationIntent、Operation、Attempt、Effect、ConditionResult 和 Result 等多个名称，接入者难以判断哪些必须直接操作，哪些由框架处理。

现有方案已经允许同进程装配、合表和共事务，但尚未把这些原则落实为统一的对象分层、默认应用接口和逐场景的数据组织。若每个逻辑记录都变成独立管理入口、持久状态机或提交阶段，首版实现与验证成本会进一步放大。当前缺少运行实现及实测数据，不能将上述风险表述为已经发生的性能瓶颈。

用户同时要求保留参考项目中有价值的语义。六个核心对象能够组织共同执行主线，但仅靠六种平面记录无法完整表达分支历史、输入排队、持续目标、子 Agent 多次激活、定时触发和可复用执行环境。缩减名称时若删除原提交身份、当前继续权或独立恢复责任，会使简化后的模型失去实际能力。

## Solution

以 Session、Task、Decision、Operation、Content、Grant 六组核心对象组织架构主线，明确内部子记录、可重建投影、能力配置及按需启用的扩展对象。普通应用通过现有架构规划的应用／SDK 入口提交输入、控制任务、取得材料和查看结果；运行内核负责决策、行动及其恢复细节。

| 核心对象 | 面向调用者的含义 | 由所属模块管理的内容 |
| --- | --- | --- |
| Session | 连续对话与任务关联 | 消息、原提交关联、历史来源和回合展示；分支是单独声明的能力 |
| Task | 用户目标及其推进责任 | 目标修订、任务条件、计划、等待、控制、预算、核验选择和完成结果 |
| Decision | 一次基于固定输入的决策 | 输入投影、模型配置、可选 ModelCall、提案、用量与原调用恢复 |
| Operation | 一个获准的有界行动及持续核对责任 | 原意图关联、尝试、目标证据、效果、工具结果与费用收尾 |
| Content | 可按准确版本引用的材料 | 正文、附件、报告、摘要及原结果字节的来源、权限和保留规则 |
| Grant | 对特定资源和用途的授权 | 使用裁决、撤销及结算关联；可信确认仍归实际业务负责方 |

本规格交付架构与契约设计收敛：更新权威说明、对象归属清单、默认应用接口说明、参考语义覆盖矩阵、典型请求读写设计和可执行验收场景。六组核心对象不限定物理表数量，也不撤销九模块的既有职责与独立替换合同。

参考语义覆盖矩阵逐项区分“已有设计承载”“本轮须补齐设计”“按需扩展”“本阶段不交付”，另列运行验证是否存在。对功能缺口给出负责模块、承载对象、用户可见行为和启用条件，不能以任意 metadata 字段或相同名称宣称覆盖。

## User Stories

1. As an application developer, I want a small set of core concepts, so that I can integrate Harness without learning every internal record.
2. As an application developer, I want one documented application entry point for the main workflow, so that I can submit, observe, and control work consistently.
3. As an application developer, I want core objects, internal records, projections, configuration, and extensions to be distinguished, so that I know which contracts my application depends on.
4. As an application developer, I want unsupported capabilities to be explicit, so that I do not mistake a design proposal for an implemented feature.
5. As a user, I want one Session to contain several independent Tasks, so that I can discuss related work without merging their execution responsibilities.
6. As a user, I want answers to clarification requests to continue the original Task, so that each reply does not create a duplicate goal.
7. As a user, I want closing or archiving a Session to preserve ongoing Tasks, so that conversation organization does not cancel my work.
8. As an API consumer, I want to submit a Task without creating a Session, so that non-conversational integrations remain simple.
9. As a user, I want saved messages and accepted Tasks to have distinguishable states, so that I can tell whether my work was actually accepted.
10. As an application developer, I want a lost Session-to-Task acknowledgement to recover the original association, so that retries do not create duplicate Tasks.
11. As a user, I want each submitted input to remain identifiable, so that I can distinguish queued, consumed, rejected, and withdrawn input where that capability is supported.
12. As a user, I want a queued input to be withdrawn without stopping unrelated work, so that controls apply to the intended submission.
13. As a user, I want steering and follow-up input to have explicit delivery rules, so that I know whether an instruction affects current work or later work.
14. As an application developer, I want delayed completion events to retain their original submission identity, so that an old run cannot overwrite a newer view.
15. As a user, I want branching and history selection to preserve their source positions when enabled, so that I can revisit context without losing its origin.
16. As a user, I want a history fork to avoid copying active execution obligations, so that exploring another conversation path does not repeat external actions.
17. As a user, I want summaries and compacted history to remain traceable to original material, so that presentation changes do not silently change task facts.
18. As a user, I want a Task to continue across several interactions, so that a long goal survives clarification and waiting.
19. As a user, I want persistent goal status and permission to continue execution to be separate, so that reopening a conversation does not authorize unexpected actions.
20. As a user, I want progress and continuation limits to survive restart, so that recovery does not reset budgets or create an endless loop.
21. As a user, I want task completion to include its applicable evidence and limitations, so that an assistant reply alone is not treated as proof of success.
22. As a user, I want target and requirement changes to invalidate incompatible pending work, so that actions use my current goal.
23. As an application developer, I want Decision inputs, outputs, and model usage to be managed together, so that I can inspect a decision without coordinating several separate lifecycle APIs.
24. As an operator, I want every actual model request to remain attributable after simplification, so that retries and auxiliary requests cannot hide cost or uncertainty.
25. As a user, I want one stable Operation identity for one admitted action, so that reconnecting does not repeat a write.
26. As a user, I want unknown effects to remain visible after interruption, so that I am not told an action failed when it may have succeeded.
27. As a user, I want cancellation and physical termination to be distinguishable, so that lingering processes and late results remain accountable.
28. As an operator, I want late usage corrections to remain associated with the original work, so that final messages do not prematurely close accounting obligations.
29. As an application developer, I want content references and bounded inline text to follow explicit rules, so that simple messages stay practical while large material is not duplicated.
30. As a user, I want current content permissions to apply to history, summaries, and results, so that an old view cannot disclose material whose use has ended.
31. As a user, I want trusted confirmation to remain distinct from ordinary conversation, so that a note or model-generated sentence cannot authorize an action.
32. As an application developer, I want authorization use and settlement to be managed by the permission module, so that simplifying my integration preserves resource and budget controls.
33. As an extension developer, I want accurate capability and installation bindings, so that a later tool change cannot silently alter an admitted action.
34. As an operator, I want runtime enforcement to remain distinct from permission records, so that an approved request still executes within the declared isolation limits.
35. As a user, I want child work to retain its original parent and child identities, so that recovery does not create duplicate agents or lose their results.
36. As a user, I want a reusable child conversation to survive one activation ending, so that later work can continue from the intended context.
37. As a user, I want waiting for child work to be independent of cancelling it, so that a wait timeout does not silently terminate delegated work.
38. As a user, I want recurring schedules to have their own lifecycle when enabled, so that editing future triggers does not rewrite already-created Tasks.
39. As an operator, I want each scheduled occurrence to be identifiable, so that a delivery retry does not create duplicate work.
40. As a user, I want a reusable execution environment to have explicit ownership and stopping rules, so that one completed tool call does not falsely imply that the environment has exited.
41. As a user, I want execution checkpoints to state what they can restore, so that saved computation state is not confused with restored external effects.
42. As a user, I want long-term memory to retain its own permission and correction lifecycle, so that a stored document does not automatically become reusable memory.
43. As a user, I want ordinary memory corrections to remain a direct authorized action, so that updating a preference does not require a software release workflow.
44. As an extension developer, I want Skill and plugin lifecycle details to remain optional to ordinary task callers, so that extensibility does not enlarge every request workflow.
45. As a maintainer, I want receipt, job, claim, and delivery mechanics encapsulated behind existing interfaces, so that each domain does not reinvent a recovery framework.
46. As a maintainer, I want related records to share transactions where the existing authority model permits, so that semantic distinctions do not force unnecessary commits.
47. As a maintainer, I want durable state and reconstructible projections to be distinguished, so that views cannot become competing sources of task truth.
48. As a maintainer, I want a source-backed semantic mapping for all five reference projects, so that simplification decisions are based on behavior rather than matching names.
49. As a maintainer, I want different runtime paths within one reference repository to be evaluated separately, so that capabilities from incompatible configurations are not combined into a false guarantee.
50. As an application developer, I want existing public contracts to remain compatible, so that a simpler facade does not silently change component interoperability.
51. As an operator, I want local development simplification to preserve the production deployment obligations, so that a convenient prototype does not redefine the reliability target.
52. As a maintainer, I want comparable request scenarios and separate read/write units, so that I can evaluate whether the proposed simplification actually reduces work.
53. As a tester, I want acceptance scenarios exercised through the highest existing workflow interface, so that tests verify behavior rather than record layout.
54. As a reviewer, I want design coverage, static checks, runtime correctness, and measured performance reported separately, so that a documentation delivery is not mistaken for a running system.

## Implementation Decisions

1. **交付范围。** 本轮落实核心模型与接口组织的设计收敛，产物包括权威架构正文、语义覆盖矩阵、流程与数据归属、兼容性说明及验收场景。运行内核、数据库迁移、SDK 实现和上游功能复刻不属于本轮交付。

2. **主干与模块。** 以六组核心对象描述请求主线；保留 Orchestrator、Brain、Executor、权限、交互、记忆、协作、扩展和评测的既有职责。应用／SDK 的组合入口由既有事实负责方提供行为，不成为第二个执行循环或任务完成裁决者。

3. **对象保留标准。** 对当前数据概念逐项记录负责方、身份、生命周期、事务、查询者、保留期限与独立存在理由。需要独立寻址、并发修改、权限、保留规则，或父对象结束后仍有责任时保留相应独立记录；仅描述处理步骤的内容优先作为字段、子记录或事件。不能按名称数量机械删减，也不预先承诺六张表。

4. **Session 与 Task。** 一个 Session 可关联多个 Task；初版每个 Task 至多固定一个创建来源 Session，其他界面可引用原 Task。独立新目标创建 Task，澄清回答消费原 InputRequest，控制沿原 Task 命令执行。批注本身不发起模型或工具调用；需要生成或计费的路径关联适当 Task／Decision。关闭、归档和重开会话不改变原任务责任。

5. **原提交与回合。** 原 Command、InputSubmission、InputRequest 及其关联承担输入身份和消费记录。Turn／Run 优先作为这些事实的展示与查询投影，不另建一套与 Task 竞争的目标状态机。涉及独立排队、撤回或取消范围时，必须保留有身份的提交子记录与明确控制范围；单个 task.status 不能替代这些语义。已有公共合同不能表达的行为列为能力缺口与后续变更要求，不假定原控制方法已经支持。

6. **会话与模型视图。** 消息、配置变化、压缩记录、工具配对和原始内容引用按各自语义保留，模型上下文由获准历史和当前任务事实生成。完整 Session 日志不自动成为模型输入；摘要与合成占位不能改写原 Effect、目标和授权。

7. **分支能力。** 当前首版的线性消息顺序保持为明确能力子集。完整分支设计须说明消息父链、分支头、来源截止位置、配置与摘要适用范围，以及工作区和已有外部效果的处理；仅复制正文或回退界面不代表回滚世界状态。分支／fork 不复制活动 Operation、授权使用、未结预算或执行责任；完整运行支持留在对应功能切片。

8. **Task 内聚。** 目标修订、Requirement、Plan、等待、控制、预算和最终 Result 由 Task 推进接口统一组织。ConditionResult 保留准确目标／成果绑定、判断依据和当前适用性；判定规则与验证器资格仍由原模块维护。将记录归入任务视图不要求同一物理行，也不删除终态后仍需维护的费用、效果和证据缺陷说明。

9. **目标与继续权。** Task 的目标状态、当前控制、实际执行阶段和是否具备新启动条件分别表达。持续目标、一次输入处理和一次工作进程启动具有不同生命周期；恢复按既有授权、控制与有界推进策略裁决，不能从 active 状态直接推断可启动新费用或效果。

10. **Decision 内聚。** Snapshot／BrainContext、可选 ModelCall、Proposal 与实际输入／用量关联由原决策接口管理。确定性决策允许没有 ModelCall；每个 Decision 仍至多一次物理模型请求。标题、摘要、修复或精炼等额外推理分别准入并记录，不能因隐藏内部对象而变成透明调用。

11. **Operation 内聚。** Operation 统一提供行动接纳、查询、控制和恢复视图，Attempt、Effect 与返回内容保持清晰子结构。Orchestrator 保存的 OperationIntent 和 Executor 保存的接纳事实有各自负责方；可以组合读取与在合法范围合并本地事务，不能将跨负责方的交接伪装为一份原子记录。重试仍按原操作与能力声明处理未知效果。

12. **Content 与呈现。** 正文优先保存准确版本及来源并由其他对象引用，既有有界内联文本继续作为声明明确的变体。用户历史、模型输入、工具原结果、任务结果和界面摘要分别保留必要语义，不重复拥有可独立修改的业务真值。provisional 流式片段使用有界缓冲；正式结果与界面快照可按原对象查询恢复。

13. **Grant 与可信输入。** 授权、原使用及费用结算由权限模块统一提供接口；不可变使用回执与后续累计结算分别保留。Confirmation 是绑定原命令的受信业务决定，仍由实际业务负责方保存与消费，不因六组对象而全部归入 Grant。预览与批准遵守既有受信界面合同；沙箱和资源隔离继续由实际执行入口实施。

14. **基础设施封装。** 原命令去重、回执、JobStore、Claim、交付 outbox 和最低限度终态记录保留既有公共模板与成功点。应用只操作必要的业务入口，模块实现和独立实现者仍遵守适用的恢复合同。禁止用通用 Run、事件总线或通用工作流状态机替代领域成功判断。

15. **配置与绑定。** Capability、Binding、模型配置、InstallLock 及当前启用资格是核心调用所需的配置与绑定信息，不因未列入六组对象而删除。小目录可静态装配；动态发现、加载和插件管理属于按需启用的行为。记录准确声明与安装组合，不另造未经定义的公共目录版本类型。

16. **协作扩展。** 子 Task、子 Session、Delegation 和准确启动配置承担稳定父子关联；子会话的持续身份与某次激活分开。接纳句柄、读取、有界等待、取消、完成和账务封闭分别表达。父子关系不等同于历史 fork；可复用 child 不因一次运行结束而失去身份。

17. **调度扩展。** Schedule 在需要用户可管理的未来或周期触发时独立存在，保存规则、启停和 occurrence 身份，并通过原命令关联所产生的 Task 或输入。现有 JobStore 负责可靠执行，不能单独替代用户可见的调度语义。调度能力的设计边界与未实现状态进入覆盖矩阵，不在本轮新增公共调度协议。

18. **执行环境扩展。** 可复用 kernel、后台进程或有状态设备环境由 Executor 管理独立资源身份、占用、配置和实际停止状态，可以服务多个 Operation。逻辑取消不等于资源退出；计算检查点说明恢复范围，不据此推断 socket、子进程或外部效果已恢复。环境复用不得默认引入任意对象反序列化。

19. **跨任务记忆与扩展生命周期。** Memory、Skill、Plugin、安装及评测发布保留必要的独立来源、版本、当前许可和启用状态。Content 承担正文，不能独自替代索引、纠正、来源关闭或激活规则。普通用户记忆修订沿原授权流程；软件、Skill 或通用改善主张采用既有评测与发布门槛。未启用的扩展不参与普通请求主流程。

20. **参考语义逐项取证。** 覆盖矩阵至少包含会话历史、Turn／Run、输入调度、上下文压缩、模型步骤、工具与未知效果、批准与隔离、持续目标、分支、子 Agent、调度、执行环境、记忆、Skill／插件、流呈现与重连。每项注明项目与固定源码路径、实际装配路径、生命周期、恢复含义及本项目承载。Pi 经典 CLI、AgentHarness 与 pi-durable 分开；同名 Task／Operation／Run 不自动判为等价。

21. **接口兼容。** 首先通过既有应用／SDK 组合和内部记录组织降低使用成本。现有 105 个领域方法与机器契约不因概念收敛而删改，Session 仍是应用内部设计。若某项完整参考语义需要新增或改变公开合同，列出准确差异及独立变更范围，并同步要求 Schema、方法登记、示例与互操作检查；该需求不计为已经实现的覆盖。

22. **持久化与读写。** 为直接回答、读取后回答、保存后读回及冷恢复给出默认数据组织与可合并事务清单。共同提交必须符合已有负责方和本地事务约束；原字节先持久再被权威引用，发送前必要屏障保留。可重建投影不新增权威状态，流式片段不逐 token 建作业或提交。逻辑记录、SQL／追加调用、事务、flush／sync、字节与物理 IO 分开计量，不预报未经实现的固定下降比例。

23. **开发与生产。** 最小开发装配继续采用单进程及少量默认实现验证任务主线；生产从首版保留已确认的接入、应用、工作池职责，以及 PostgreSQL 事实与工作责任、对象存储和部署恢复目标。对象内聚不改变 Task 的固定逻辑 Orchestrator，也不承诺跨库或外部系统原子提交。

24. **文档和决策一致性。** 同步总览、领域词汇、相关模块、应用／SDK 接入说明、工程阶段与验收入口，规则写回原负责模块。新清单负责映射与导航，避免形成另一套相冲突的权威规范。既有 ADR 的身份保留、传输、生产部署、固定路由、核验、条件修订、回退批准、预览、可靠工作和同仓契约决策均保持；确需改变时显式提出替代决策，不以本规格覆盖。

## Testing Decisions

1. **主验证切面。** 沿用现有架构规划的应用／SDK 任务工作流：提交目标或原输入、查询接纳与进度、暂停／取消、读取结果及恢复原对象。Session 行为从应用交互入口观察，Task／Operation 结果通过原查询合同取得。以这个完整工作流为主验收切面，必要时使用已有 Brain、Executor、权限及协作契约定位外部行为无法充分区分的情况；不新增测试专用业务入口。

2. **当前交付与运行验证分开。** 本轮实际执行文档结构、链接、术语、对象归属、覆盖矩阵与契约差异检查。下面的运行场景交付为待运行验收规格；只有实现、数据库或平台试验实际执行后才能登记结果。应用／SDK 入口目前是已有架构中的规划接口，不表述为已存在运行系统。

3. **有效断言。** 测试观察用户可见状态、原对象身份、准确结果、外部目标真值、实际出站次数、权限决定和费用归属。无需断言每个名词一张表、私有函数调用顺序、事件数量必须与类数量一致或每条流消息持久化。故障注入可以利用存储或供应方适配器，但预期行为仍从上述主入口与独立目标证据判断。

4. **对象收敛审查。** 对主流程中每个名称检查其分类、负责方、独立存在理由、查询者、恢复和保留责任。应用调用者能够用主接口完成典型流程，内部记录不要求逐项 CRUD；独立实现者所需的公共契约仍有准确入口。Task、Decision、Operation、授权及费用状态之间的合法不同步必须明确可表达。

5. **会话与提交。** 同一 Session 的两个独立目标保持两个 Task；澄清回答继续原 Task；保存消息后交接失败、Task 接纳后关联回写丢失均恢复原命令。归档不取消任务，旧取消或终结事件不影响新提交。对尚未公开的独立撤回能力，验收首先检查设计是否声明缺口，不能用全任务取消代替该语义。

6. **模型与工具。** 直接回答、一次读工具后回答、保存文件后独立读回分别记录实际输入和原身份。模型已可能发送后断线不透明重发；文件已写而答复丢失保持原 Operation，沿证据核对；取消后迟到结果和费用继续归原记录。合法授权的正常路径应成功，不以一律拒绝满足故障断言。

7. **目标、输入与权限。** 覆盖条件修订后旧提案到达、当前继续权不足、重启不重置无进展次数、两个界面竞争确认、当前权限或来源发生变化。核对原目标修订、一次消费与实际出站；普通对话不能变成可信确认，旧摘要不能替代当前限制。

8. **按需能力的语义验收。** 分支保持来源且不重放外部写；同名工具刷新不重绑原 Operation；子会话冷恢复沿原 Delegation；重复 schedule occurrence 不创建新的独立工作；取消 cell 后环境仍忙时不释放占用；长期记忆纠正不覆盖新版本。每项分别记录设计覆盖、启用前置条件和是否具备运行证据，未启用能力不得宣称运行验收通过。

9. **参考对照。** 对五个固定快照逐项检查映射依据与语义差异，使用现有调研中的源码证据和反例。检查会话主日志与投影、内存接纳与持久提交、回合结束与目标完成、计算状态与外部效果四类容易混淆的含义；不能跨装配路径拼接保证，也不能用字段可序列化替代行为设计。

10. **既有测试先例。** 优先复用现有协议正反例、可靠工作 FW 向量、任务 RT 向量、Brain BI 向量、执行 EX 向量、权限与交互向量，以及 HAR-01～11 的贯穿场景。补充简化后暴露的缺口；不为重述对象字段新增镜像测试。机器契约若保持原样，只需确认差异未触及该范围；将来发生实际契约修改时运行对应完整检查。

11. **读写与性能证据。** 沿既有直接回答 A、读取后回答 B、冷恢复 C 口径，加上文件保存读回场景，固定输入、历史、模型回放、输出分块和存储配置。分别记录初始化、稳态、恢复、辅助调用及流更新成本；同次事务合并与多对象写入分别计数。架构阶段提供可复核的阶段与合并依据，实现后才填 SQL、WAL、sync 和时延实测。

12. **交付完成条件。** 六组对象及全部被收敛记录有明确语义归属；参考矩阵无未解释的主线遗漏；默认接口和四类请求流程可连续追踪；现有 ADR 与公开合同无静默改变；普通应用无需管理底层作业；可选能力、当前缺口与运行验证状态明确；受影响文档及链接检查通过。

## Out of Scope

- 本次不实现运行内核、SDK、数据库表、数据迁移、云资源或生产部署，也不把规格发布记为架构改造已完成。
- 不要求首版同时具备五个参考项目的全部功能。完整历史分支、多 Lane、周期调度、可复用 kernel、动态扩展和自动改善按既有工程阶段或后续独立切片交付，本轮明确其语义承载与缺口。
- 不为了缩减对象数量删除未知效果、原调用身份、可信确认、实际费用、来源权限、最小去重／终态记录或独立完成依据。
- 不将全部对象压入一个无类型 metadata 字段，不限定六张表，不新增通用 Run 状态机或另一套调度权威。
- 不自动删减现有 105 个领域方法，不在本轮发布 Session、Schedule 或其他新增公共协议。
- 不重新拉取参考仓库覆盖固定研究快照，不合并不同运行路径的保证，不改写历史研究基线摘要来适配新架构。
- 不将静态设计与检查结果表述为性能改善、生产恢复达标或与参考项目的运行能力等价。

## Further Notes

本规格综合本次会话中关于 Session／Turn／Run 与 Task、数据概念数量、六组核心对象及参考语义覆盖的讨论。它是上一轮架构刷新之后的模型收敛工作，上一轮已完成票据继续作为历史交付记录。

实施时先建立对象归属与参考语义清单，再收敛默认应用接口和四类请求流程，最后更新相关模块与验收入口。对尚未开放的扩展，交付足够清楚的归属、生命周期和启用约束即可；无需以“覆盖参考项目”为由提前实现全量功能。

参考与约束：

- [领域术语](../../CONTEXT.md)、[既有 ADR](../../docs/adr/)。
- [当前 Session 与 Task 设计](../../docs/architecture/.draft/interaction/session-and-task.md)、[最小工程装配](../../docs/architecture/.draft/engineering.md#minimum-profile)。
- [现有架构入口](../../docs/architecture/README.md)、[共同契约](../../docs/architecture/.draft/contracts/README.md)。
- [数据对象与读写频次对照](../../docs/research/agent-harness-comparison/data-flow-io-comparison.md)、[固定研究来源](../../docs/research/agent-harness-comparison/sources.json)。
- [Codex 调研](../../docs/research/codex/README.md)、[Pi 调研](../../docs/research/pi/README.md)、[DeepSeek Harness 调研](../../docs/research/deepseek-harness/README.md)、[Prime Agent 调研](../../docs/research/prime-agent/README.md)、[Crush 调研](../../docs/research/crush/README.md)。
- [既有组合故障场景](../../docs/architecture/.draft/validation/harness-scenarios.md)、[上一轮架构刷新交付](../harness-architecture-refresh/README.md)。

## Comments

- 2026-10-01：按用户显式调用的 to-spec 技能创建并发布本规格，triage 为 ready-for-agent。主验证切面已向用户征询；在收到回复前按既有应用／SDK 工作流编写，未将未回复记录为确认。规格发布不代表运行实现或架构改造完成。
- 2026-10-01：规格静态检查通过：七个模板章节齐全且顺序正确，54 条用户故事连续编号并符合格式，24 项实施决策未嵌入具体文件路径或代码；文档检查通过 15 个本地链接。上述检查只验证规格结构与引用，未运行 Harness、上游项目、数据库或性能测试。
- 2026-10-01：用户调用 implement-spec 实施本目录。已建立[六张实施票据](README.md)，沿用本规格的验证切面，运行场景保持待执行；规格实施范围仍为架构设计交付。

- 2026-10-01：本规格的架构设计交付完成，六张票据均已合入集成分支；双轴审查与统一修复见[审查记录](review.md)，最终静态核对及七个实施工作树归档见[验证记录](verification.md)。54 条故事与 24 项决策有完整追踪；运行内核、SDK、数据库、平台和性能验证继续按本规格范围留待实施。
