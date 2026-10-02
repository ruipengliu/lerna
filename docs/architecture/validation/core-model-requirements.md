# 核心模型的行为与实施要求

[验收入口](README.md) · [逐项追踪](core-model-traceability.md)

本页保留核心模型设计的 54 条行为要求与 24 项实施选择编号，供验收逐项定位。编号对应设计要求，未表示运行通过；详细规则和字段仍以追踪表链接的负责模块和共同契约为准。

## 1. 应用与运行行为

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

## 2. 实施选择检查

1. **交付范围。** 当前文档落实核心模型与接口组织的设计收敛，产物包括权威架构正文、语义覆盖矩阵、流程与数据归属、兼容性说明及验收场景。运行内核、数据库迁移、SDK 实现和上游功能复刻不属于当前文档交付。

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

17. **调度扩展。** Schedule 在需要用户可管理的未来或周期触发时独立存在，保存规则、启停和 occurrence 身份，并通过原命令关联所产生的 Task 或输入。现有 JobStore 负责可靠执行，不能单独替代用户可见的调度语义。调度能力的设计边界与未实现状态进入覆盖矩阵，不在当前文档新增公共调度协议。

18. **执行环境扩展。** 可复用 kernel、后台进程或有状态设备环境由 Executor 管理独立资源身份、占用、配置和实际停止状态，可以服务多个 Operation。逻辑取消不等于资源退出；计算检查点说明恢复范围，不据此推断 socket、子进程或外部效果已恢复。环境复用不得默认引入任意对象反序列化。

19. **跨任务记忆与扩展生命周期。** Memory、Skill、Plugin、安装及评测发布保留必要的独立来源、版本、当前许可和启用状态。Content 承担正文，不能独自替代索引、纠正、来源关闭或激活规则。普通用户记忆修订沿原授权流程；软件、Skill 或通用改善主张采用既有评测与发布门槛。未启用的扩展不参与普通请求主流程。

20. **参考语义逐项取证。** 覆盖矩阵至少包含会话历史、Turn／Run、输入调度、上下文压缩、模型步骤、工具与未知效果、批准与隔离、持续目标、分支、子 Agent、调度、执行环境、记忆、Skill／插件、流呈现与重连。每项注明项目与固定源码路径、实际装配路径、生命周期、恢复含义及本项目承载。Pi 经典 CLI、AgentHarness 与 pi-durable 分开；同名 Task／Operation／Run 不自动判为等价。

21. **接口兼容。** 首先通过既有应用／SDK 组合和内部记录组织降低使用成本。现有 105 个领域方法与机器契约不因概念收敛而删改，Session 仍是应用内部设计。若某项完整参考语义需要新增或改变公开合同，列出准确差异及独立变更范围，并同步要求 Schema、方法登记、示例与互操作检查；该需求不计为已经实现的覆盖。

22. **持久化与读写。** 为直接回答、读取后回答、保存后读回及冷恢复给出默认数据组织与可合并事务清单。共同提交必须符合已有负责方和本地事务约束；原字节先持久再被权威引用，发送前必要屏障保留。可重建投影不新增权威状态，流式片段不逐 token 建作业或提交。逻辑记录、SQL／追加调用、事务、flush／sync、字节与物理 IO 分开计量，不预报未经实现的固定下降比例。

23. **开发与生产。** 最小开发装配继续采用单进程及少量默认实现验证任务主线；生产从首版保留已确认的接入、应用、工作池职责，以及 PostgreSQL 事实与工作责任、对象存储和部署恢复目标。对象内聚不改变 Task 的固定逻辑 Orchestrator，也不承诺跨库或外部系统原子提交。

24. **文档和决策一致性。** 同步总览、领域词汇、相关模块、应用／SDK 接入说明、工程阶段与验收入口，规则写回原负责模块。新清单负责映射与导航，避免形成另一套相冲突的权威规范。既有 ADR 的身份保留、传输、生产部署、固定路由、核验、条件修订、回退批准、预览、可靠工作和同仓契约决策均保持；确需改变时显式提出替代决策，不以本要求清单覆盖。

