# 来源、覆盖与阅读方式

[入口](README.md) · [待决边界](open-boundaries.md)

本版以 `docs/architecture/.draft` 为主要设计来源，结合正在整理的 `docs/architecture` 正文与现行 CONTEXT、ADR，重写为从任务经过到实现机制的连续叙事。原目录保留，精确协议和验收资产只引用，不建立第二份维护源。

## 本版怎样组织同一套方案

19 篇 Markdown 中，README 提供入口，task-journey 给出完整任务，ownership 解释必要区别；其余主题沿任务推进、取证执行、交互协作、恢复、装配与交付展开。open-boundaries 集中收录尚未闭合的保证和范围，本页用于对照定位。

改写主要调整解释顺序：先给读者一个需要裁决的具体问题，再介绍相关对象和事务；把已接纳、实际效果和完成依据放到同一个例子中比较；把首装依赖、受管目录和失败成本放在使用能力之前。大量精确字段及反例继续保留在原资产，不在每章重复完整清单。

例如迟到账单不再在一段话里同时说明来源版本、投递命令、预算差额和终态保留，而在[授权与预算](authorization-and-budget.md)按三方职责展开。文字更短的部分没有取消 outbox、JobAck、主动查源和差额归并的语义。

## 草稿到 v2 的覆盖映射

以下覆盖 `.draft` 的 55 篇设计与验收 Markdown。表内归并的是叙事位置；原实现篇中的精确表结构、参数、全部用例及机器资产继续原位维护，不声称已经逐字迁移。

| 原来源 | v2 位置与处理 |
| --- | --- |
| [README](../architecture/.draft/README.md)、[technical-overview](../architecture/.draft/technical-overview.md)、[goals](../architecture/.draft/goals.md) | [入口](README.md)、[任务经过](task-journey.md)、[工程目标](engineering.md)、[部署目标](deployment.md) |
| [core-data-model](../architecture/.draft/core-data-model.md)、[uml-models](../architecture/.draft/uml-models.md) | [事实归属](ownership.md)及各主题内对象关系；旧 UML 与图册保留作查阅资产 |
| [application-workflow](../architecture/.draft/application-workflow.md)、[walkthrough](../architecture/.draft/walkthrough.md) | [任务经过](task-journey.md)、[应用](applications.md)、[委派](collaboration.md) |
| [request-data-flows](../architecture/.draft/request-data-flows.md) | [任务经过](task-journey.md)、[Brain](brain.md)、[执行](execution.md)、[工程计量](engineering.md) |
| [decisions](../architecture/.draft/decisions.md) | [取舍](ownership.md)、各主题的代价及下方 ADR 对应；未重开既定决定 |
| [orchestrator/README](../architecture/.draft/orchestrator/README.md)、[implementation](../architecture/.draft/orchestrator/implementation.md) | [任务控制](task-control.md)、[完成判断](verification.md)、[授权预算](authorization-and-budget.md) |
| [orchestrator/verification](../architecture/.draft/orchestrator/verification.md) | [完成、证据与缺陷](verification.md)、[公共边界](open-boundaries.md#result-contract) |
| [orchestrator/access-paths](../architecture/.draft/orchestrator/access-paths.md) | [任务控制](task-control.md)、[持久工作](durable-work.md)、[部署](deployment.md)；精确访问矩阵保留原位 |
| [orchestrator/scheduled-triggers](../architecture/.draft/orchestrator/scheduled-triggers.md) | [应用的定时入口](applications.md)与[后续合同](open-boundaries.md) |
| [brain/README](../architecture/.draft/brain/README.md)、[implementation](../architecture/.draft/brain/implementation.md)、[decision-paths](../architecture/.draft/brain/decision-paths.md) | [Brain 与上下文](brain.md)、[任务控制](task-control.md)、[策略验收](engineering.md) |
| [execution/README](../architecture/.draft/execution/README.md)、[implementation](../architecture/.draft/execution/implementation.md) | [执行与设备](execution.md)、[授权预算](authorization-and-budget.md)、[目标故障域](deployment.md) |
| [execution/programmatic-tools](../architecture/.draft/execution/programmatic-tools.md) | [程序化工具](execution.md)与[合同边界](open-boundaries.md) |
| [memory/README](../architecture/.draft/memory/README.md)、[implementation](../architecture/.draft/memory/implementation.md) | [内容与长期记忆](content-and-memory.md)、[授权](authorization-and-budget.md)、[大内容协议](protocol.md) |
| [memory/optimization-plan](../architecture/.draft/memory/optimization-plan.md)、[validation](../architecture/.draft/memory/validation.md) | [检索候选和冲突](content-and-memory.md)、[实验](evaluation.md)；专项配额与全部反例保留原位 |
| [security/README](../architecture/.draft/security/README.md)、[implementation](../architecture/.draft/security/implementation.md) | [授权与预算](authorization-and-budget.md)、[协议](protocol.md)、[可信时间](deployment.md#clock-adapter) |
| [interaction/README](../architecture/.draft/interaction/README.md)、[implementation](../architecture/.draft/interaction/implementation.md)、[session-and-task](../architecture/.draft/interaction/session-and-task.md) | [应用](applications.md)、[任务经过](task-journey.md)、[公开输入边界](open-boundaries.md) |
| [collaboration/README](../architecture/.draft/collaboration/README.md)、[implementation](../architecture/.draft/collaboration/implementation.md) | [委派](collaboration.md)、[预算](authorization-and-budget.md) |
| [extensions/README](../architecture/.draft/extensions/README.md)、[implementation](../architecture/.draft/extensions/implementation.md)、[progressive-discovery](../architecture/.draft/extensions/progressive-discovery.md) | [装配与生命周期](lifecycle.md)、[首装工程切片](engineering.md) |
| [evaluation/README](../architecture/.draft/evaluation/README.md)、[implementation](../architecture/.draft/evaluation/implementation.md) | [评测与发布](evaluation.md)、[验证器](verification.md)、[实际激活](lifecycle.md) |
| [reliable-work](../architecture/.draft/reliable-work.md) | [持久工作](durable-work.md)，各主题解释领域接入与成功含义 |
| [deployment](../architecture/.draft/deployment.md)、[deployment-production](../architecture/.draft/deployment-production.md)、[storage-and-middleware](../architecture/.draft/storage-and-middleware.md) | [部署与恢复](deployment.md)、[协议](protocol.md)、[工程](engineering.md)；全部容量配置与故障步骤保留原位 |
| [engineering](../architecture/.draft/engineering.md) | [工程与验收](engineering.md)、[首装](lifecycle.md) |
| [contracts/README](../architecture/.draft/contracts/README.md)、[protocol](../architecture/.draft/contracts/protocol.md)、[methods](../architecture/.draft/contracts/methods.md) | [协议与 SDK](protocol.md)；精确 Schema、方法登记和类型继续原位权威 |
| [contracts/transport](../architecture/.draft/contracts/transport.md)、[grpc](../architecture/.draft/contracts/grpc.md) | [协议与 SDK](protocol.md)、[部署](deployment.md)；逐帧约束与参数继续原位权威 |
| [contracts/examples/README](../architecture/.draft/contracts/examples/README.md)、[brain](../architecture/.draft/contracts/examples/brain/README.md)、[protocol](../architecture/.draft/contracts/examples/protocol/README.md)、[transport](../architecture/.draft/contracts/examples/transport/README.md) | [协议资产入口](protocol.md)及[证据分层](engineering.md)；夹具和向量未复制 |
| [validation/README](../architecture/.draft/validation/README.md)、[core-model-scenarios](../architecture/.draft/validation/core-model-scenarios.md)、[harness-scenarios](../architecture/.draft/validation/harness-scenarios.md) | [工程退出条件](engineering.md)，任务及组合场景继续由原测试规格维护 |
| [validation/fault-experiments](../architecture/.draft/validation/fault-experiments.md)、[optimization-evidence](../architecture/.draft/validation/optimization-evidence.md) | [恢复前提](deployment.md)、[比较方法](evaluation.md)、[工程验收](engineering.md)；实验规格不改标实测结果 |

草稿中的 review 文档与其他审阅任务不作为本版依据，未读取其结论。外部项目调研保持为原稿的背景资料，本版没有重新开展或转抄比较研究；原 HTML、drawio、图片与程序资产保留原位。

## 当前正文与 v2 的关系

| 正在整理的正文 | v2 对应 |
| --- | --- |
| [architecture/README](../architecture/README.md) | [入口](README.md)、[任务经过](task-journey.md)、[职责](ownership.md) |
| [ochestrator/README](../architecture/ochestrator/README.md) | [任务控制](task-control.md)、[验证](verification.md)、[Brain](brain.md) |
| [ochestrator/task-lifecycle](../architecture/ochestrator/task-lifecycle.md) | [任务控制](task-control.md)、[执行](execution.md)、[委派](collaboration.md) |
| [ochestrator/durable-work](../architecture/ochestrator/durable-work.md) | [持久工作](durable-work.md)、[部署恢复](deployment.md) |

保留这些来源中的近期整理方向，但不预设正文比草稿完整。需要多章才能闭合的细节统一放在相应 v2 主题，公开合同仍核对机器资产。

<a id="adr-map"></a>
## 已确认决定的承接

| ADR | 在本版如何保持 |
| --- | --- |
| [0001 最小关闭身份](../adr/0001-retain-closed-identities.md) | [执行墓碑](execution.md)、[协议保留](protocol.md)、[长期存储成本](deployment.md) |
| [0002 Go／WSS／gRPC](../adr/0002-go-wss-grpc.md) | [协议绑定与单一字段源](protocol.md)、[工程依赖](engineering.md) |
| [0003 生产分布式](../adr/0003-production-distributed.md) | [角色、三可用区与恢复](deployment.md)、[独立生产门槛](engineering.md) |
| [0004 固定原任务路由](../adr/0004-discovery-fixed-task-routing.md) | [首发与断线](task-journey.md)、[SDK](protocol.md)、[新任务扩容](deployment.md) |
| [0005 评估证据资格](../adr/0005-evaluator-evidence-eligibility.md) | [完成与缺陷](verification.md)、[质量评测](evaluation.md) |
| [0006 先采纳条件变化](../adr/0006-adopt-requirements-before-actions.md) | [任务经过](task-journey.md)、[提案消费](task-control.md) |
| [0007 独立回退批准](../adr/0007-independent-rollback-approval.md) | [生命周期](lifecycle.md)、[发布](evaluation.md) |
| [0008 受信 Renderer 预览](../adr/0008-trusted-renderer-preview.md) | [应用输入](applications.md)、[确认](authorization-and-budget.md) |
| [0009 可靠工作框架](../adr/0009-reliable-work-framework.md) | [模板及领域职责](durable-work.md)、[工程验证要求](open-boundaries.md) |
| [0010 同仓与契约发布](../adr/0010-monorepo-shared-contract-release.md) | [工程组织](engineering.md)、[协议资产](protocol.md) |

## 本次文档检查的范围

检查本目录的相对链接、Markdown 锚点、表格与代码围栏，以及引用方法和重点状态语义；对照上表核查来源覆盖。复用原 `validation/check_documents.py` 的逻辑，将扫描根在调用时指向 v2，不修改检查器或原架构。

原文件以写作前基线摘要核对，工作区原有修改保留。本次没有安装依赖、运行项目、提交或推送。文档静态一致性不证明时钟、数据库、SDK、模型、设备、Mermaid 渲染或生产容量已经运行通过；具体检查结果在交付时报告。

本轮检查结果：19 篇 Markdown、299 个本地链接、5 个 Mermaid 代码块，链接与 Markdown 结构错误为 0；55 篇草稿、4 篇现有正文和 10 份 ADR 均有覆盖映射；12 处代码格式的精确方法名均在现有 105 方法登记中。写作前保存的 1,838 个原文件摘要全部一致。人工交叉核对重点包括接纳与效果、旧提案与新修订、完成与缺陷、领取与迟到事实、激活与重开、迟到账单及恢复计量；这些核对仍不构成运行证明。
