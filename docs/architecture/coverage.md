# 设计、来源与验收追踪

[总览](README.md) · [核心对象](core-data-model.md) · [研究采用](research-basis.md) · [检查结果](review.md)

沿报告任务的任意一步，应当能定位调用、负责记录、共同提交范围、中断后的继续者和验收。本页集中提供定位；各模块和共同契约保有业务规则的主定义。来源链接用于核对历史材料，正式规范不依赖原草稿才能读到字段或算法。

## 1. 交接与实现定位

| 业务交接 | 记录和提交负责方 | 主定义 | 验收定位 |
| --- | --- | --- | --- |
| 消息→原提交→Task | 应用消息／完整Command／outbox同域提交；Task接纳后补原关联 | [应用提交](application-workflow.md#original-submission)、[会话存储](interaction/implementation.md) | [CM-03／05](validation/core-model-scenarios.md#cm-05) |
| Task→Decision | Orchestrator固定输入、decision_id、预留与派发；Brain独立接纳 | [固定输入](brain/implementation.md#snapshot-reconstruction)、[A路径](request-data-flows.md#scenario-a) | [CM-08](validation/core-model-scenarios.md#cm-08)、Brain BI向量 |
| Decision→模型出口 | Brain prepared与send_started分开；实际来源与当前资格固定 | [Brain实现](brain/implementation.md) | [静态生成例证](../../contracts/examples/brain/README.md)、真实发送BI向量 |
| Proposal→当前任务裁决 | Orchestrator一次消费，条件变化先修订并废弃余部 | [提案采纳](orchestrator/implementation.md#proposal-consumption) | [条件变更故障](validation/fault-experiments.md#implementation-clarity) |
| 准入意图→原操作 | Orchestrator意图／预留／派发job同域；Executor接纳／回执／job同域 | [B路径](request-data-flows.md#scenario-b)、[执行实现](execution/implementation.md) | [CM-09](validation/core-model-scenarios.md#cm-09)、执行EX向量 |
| 操作→目标效果→Task | Executor核对原Attempt和目标事实；Orchestrator按原修订归并 | [效果恢复](walkthrough.md#write-recovery)、[D路径](request-data-flows.md#scenario-d) | [CM-04](validation/core-model-scenarios.md#cm-04)、[故障实验](validation/fault-experiments.md) |
| 输入→一次业务消费 | 业务owner保存InputRequest；交互固定目标命令与消费投影 | [输入竞争](interaction/implementation.md#input-control-races)、[应用路径](application-workflow.md#input-scope) | [CM-06](validation/core-model-scenarios.md#cm-06) |
| Grant→使用→累计结算 | 原Grant owner固定use回执，累计结算与交回责任持久保存 | [授权实现](security/implementation.md)、[任务预算](orchestrator/budget.md) | [CM-10](validation/core-model-scenarios.md#cm-10)、[离线更正](validation/validate_lease.py) |
| 准确字节→内容引用→派生关闭 | 原内容owner先存字节再发布，索引与副本各留关闭责任 | [Memory实现](memory/implementation.md)、[存储](storage-and-middleware.md) | [CM-16](validation/core-model-scenarios.md#cm-16)、[Memory验收](memory/validation.md) |
| 父目标→子委派→闭合 | 协作固定原Delegation与唯一子映射，父控制及原账务持续 | [子恢复](collaboration/implementation.md#cold-child-recovery) | [CM-13](validation/core-model-scenarios.md#cm-13)、协作CL向量 |
| 评测→批准→当前实例就绪 | 评测保存冻结run与批准；宿主保存精确激活和当前ready | [评测实现](evaluation/implementation.md)、[宿主就绪](extensions/implementation.md#staged-readiness) | [发布恢复静态](validation/validate_release_recovery.py)、评测／扩展领域向量 |
| 域事实→Job→Claim→条件完成 | 每域短事务与共同JobStore；领域提供可完成判定 | [可靠工作](reliable-work.md)、[领域映射](orchestrator/durable-work.md) | [FW](reliable-work.md#validation)、[CM-17](validation/core-model-scenarios.md#cm-17) |

54 条行为要求和 24 项实施选择保持原编号，完整正文见[行为要求](validation/core-model-requirements.md)，逐条对应设计、CM场景和运行状态见[核心模型追踪](validation/core-model-traceability.md)。九模块24项优化及O/X实验见[优化验收](validation/optimization-evidence.md)；生产PROD向量见[生产部署](deployment-production.md)。这些编号组织待运行断言，不能累加为已经通过的测试。

## 2. 原设计正文的覆盖

来源路径与摘要保留在 source-map 和 Git 历史。各源设计全文的正式对应位置如下；原审查流水留作历史，不把旧检查数抄入当前结果。

[机器来源映射](source-map.json)固定 185 项原材料的摘要及当前维护位置，其中四份既有正式正文另记录原 Git 修订，迁移后仍能核对；54 条行为要求、逐项追踪及 gRPC 封装检查的补充来源也单列。结构化回答的新资产记录读者修订的范围，不冒充原草稿已有规则。

| 来源 | 正式维护位置 | 处理 |
| --- | --- | --- |
| `README.md` | [README.md](README.md) | 总览重写为完整任务入口；详细规则由正式专题维护 |
| `application-workflow.md` | [application-workflow.md](application-workflow.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `brain/README.md` | [brain/README.md](brain/README.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `brain/decision-paths.md` | [brain/decision-paths.md](brain/decision-paths.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `brain/implementation.md` | [brain/implementation.md](brain/implementation.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `collaboration/README.md` | [collaboration/README.md](collaboration/README.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `collaboration/implementation.md` | [collaboration/implementation.md](collaboration/implementation.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `contracts/README.md` | [contracts/README.md](contracts/README.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `contracts/examples/README.md` | [contracts/examples/README.md](../../contracts/examples/README.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `contracts/examples/brain/README.md` | [contracts/examples/brain/README.md](../../contracts/examples/brain/README.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `contracts/examples/protocol/README.md` | [contracts/examples/protocol/README.md](../../contracts/examples/protocol/README.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `contracts/examples/transport/README.md` | [contracts/examples/transport/README.md](../../contracts/examples/transport/README.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `contracts/grpc.md` | [contracts/grpc.md](contracts/grpc.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `contracts/methods.md` | [contracts/methods.md](contracts/methods.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `contracts/protocol.md` | [contracts/protocol.md](contracts/protocol.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `contracts/transport.md` | [contracts/transport.md](contracts/transport.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `core-data-model.md` | [core-data-model.md](core-data-model.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `decisions.md` | [decisions.md](decisions.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `deployment-production.md` | [deployment-production.md](deployment-production.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `deployment.md` | [deployment.md](deployment.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `engineering.md` | [engineering.md](engineering.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `evaluation/README.md` | [evaluation/README.md](evaluation/README.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `evaluation/implementation.md` | [evaluation/implementation.md](evaluation/implementation.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `execution/README.md` | [execution/README.md](execution/README.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `execution/implementation.md` | [execution/implementation.md](execution/implementation.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `execution/programmatic-tools.md` | [execution/programmatic-tools.md](execution/programmatic-tools.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `extensions/README.md` | [extensions/README.md](extensions/README.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `extensions/implementation.md` | [extensions/implementation.md](extensions/implementation.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `extensions/progressive-discovery.md` | [extensions/progressive-discovery.md](extensions/progressive-discovery.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `goals.md` | [goals.md](goals.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `interaction/README.md` | [interaction/README.md](interaction/README.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `interaction/implementation.md` | [interaction/implementation.md](interaction/implementation.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `interaction/session-and-task.md` | [interaction/session-and-task.md](interaction/session-and-task.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `memory/README.md` | [memory/README.md](memory/README.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `memory/implementation.md` | [memory/implementation.md](memory/implementation.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `memory/optimization-plan.md` | [memory/optimization-plan.md](memory/optimization-plan.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `memory/validation.md` | [memory/validation.md](memory/validation.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `orchestrator/README.md` | [orchestrator/README.md](orchestrator/README.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `orchestrator/access-paths.md` | [orchestrator/access-paths.md](orchestrator/access-paths.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `orchestrator/implementation.md` | [orchestrator/implementation.md](orchestrator/implementation.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `orchestrator/scheduled-triggers.md` | [orchestrator/scheduled-triggers.md](orchestrator/scheduled-triggers.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `orchestrator/verification.md` | [orchestrator/verification.md](orchestrator/verification.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `reliable-work.md` | [reliable-work.md](reliable-work.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `request-data-flows.md` | [request-data-flows.md](request-data-flows.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `review.md` | [review.md](review.md) | 原编写流水保留为历史；当前检查结果独立重新登记 |
| `security/README.md` | [security/README.md](security/README.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `security/implementation.md` | [security/implementation.md](security/implementation.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `storage-and-middleware.md` | [storage-and-middleware.md](storage-and-middleware.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `technical-overview.md` | [technical-overview.md](technical-overview.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `uml-models.md` | [uml-models.md](uml-models.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `validation/README.md` | [validation/README.md](validation/README.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `validation/core-model-scenarios.md` | [validation/core-model-scenarios.md](validation/core-model-scenarios.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `validation/fault-experiments.md` | [validation/fault-experiments.md](validation/fault-experiments.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `validation/harness-scenarios.md` | [validation/harness-scenarios.md](validation/harness-scenarios.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `validation/optimization-evidence.md` | [validation/optimization-evidence.md](validation/optimization-evidence.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |
| `walkthrough.md` | [walkthrough.md](walkthrough.md) | 完整迁入并按章节重组；字段、事务、恢复及验收保留 |

## 3. 机器资产与既有正文

公共合同的 Schema、105 方法登记、Protobuf 外壳、领域／传输正反例和校验器都在正式 contracts／validation 内。各资产的原哈希与目标路径逐项保存在[source-map.json](source-map.json)；哈希核对保证来源可追溯，不提供运行正确性证明。图册保存 HTML、draw.io 与概念图，关系属性省略和多重性按[图例](uml-models.md)解释。

已有正式总览按完整任务主线重组，原 ochestrator 三篇迁入正确拼写 orchestrator。任务事实和预算转入专门索引，生命周期与恢复保留既有细化规则，再引用共同框架；原稿与机器合同分别受当前主定义约束。

## 4. 全部研究材料的解释范围

每份研究均有正式解释位置：五项目与综合比较用于执行主线，Memory研究用于治理及算法对照，模型研究用于条件路线，旧形式化与写作研究保留其原证据范围。[研究依据](research-basis.md)给出具体采用、候选和历史边界。

| 原研究 | 正式解释位置 |
| --- | --- |
| [agent-harness-comparison/README.md](../research/agent-harness-comparison/README.md) | [采用与证据范围](research-basis.md) |
| [agent-harness-comparison/architecture-baseline.md](../research/agent-harness-comparison/architecture-baseline.md) | [采用与证据范围](research-basis.md) |
| [agent-harness-comparison/architecture-optimization.md](../research/agent-harness-comparison/architecture-optimization.md) | [采用与证据范围](research-basis.md) |
| [agent-harness-comparison/core-model-semantic-coverage.md](../research/agent-harness-comparison/core-model-semantic-coverage.md) | [采用与证据范围](research-basis.md) |
| [agent-harness-comparison/data-flow-io-comparison.md](../research/agent-harness-comparison/data-flow-io-comparison.md) | [采用与证据范围](research-basis.md) |
| [agent-harness-comparison/io/codex-prime.md](../research/agent-harness-comparison/io/codex-prime.md) | [采用与证据范围](research-basis.md) |
| [agent-harness-comparison/io/deepseek.md](../research/agent-harness-comparison/io/deepseek.md) | [采用与证据范围](research-basis.md) |
| [agent-harness-comparison/io/pi-crush.md](../research/agent-harness-comparison/io/pi-crush.md) | [采用与证据范围](research-basis.md) |
| [agent-harness-comparison/verification.md](../research/agent-harness-comparison/verification.md) | [采用与证据范围](research-basis.md) |
| [agent-memory-libraries-2026-09-28.md](../research/agent-memory-libraries-2026-09-28.md) | [采用与证据范围](research-basis.md) |
| [agent-memory-papers-2026-09-28.md](../research/agent-memory-papers-2026-09-28.md) | [采用与证据范围](research-basis.md) |
| [ai-report-2026-09-project-implications.md](../research/ai-report-2026-09-project-implications.md) | [采用与证据范围](research-basis.md) |
| [ai-report-2026-09-source-check.md](../research/ai-report-2026-09-source-check.md) | [采用与证据范围](research-basis.md) |
| [all-mechanisms-coverage.md](../research/all-mechanisms-coverage.md) | [采用与证据范围](research-basis.md) |
| [all-mechanisms-verification-results.md](../research/all-mechanisms-verification-results.md) | [采用与证据范围](research-basis.md) |
| [codex/README.md](../research/codex/README.md) | [采用与证据范围](research-basis.md) |
| [crush/README.md](../research/crush/README.md) | [采用与证据范围](research-basis.md) |
| [deepseek-harness/README.md](../research/deepseek-harness/README.md) | [采用与证据范围](research-basis.md) |
| [formal-methods-for-architecture.md](../research/formal-methods-for-architecture.md) | [采用与证据范围](research-basis.md) |
| [handoff-verification-results.md](../research/handoff-verification-results.md) | [采用与证据范围](research-basis.md) |
| [model-taste-x-2026-09-25.md](../research/model-taste-x-2026-09-25.md) | [采用与证据范围](research-basis.md) |
| [module-design-workflow-check-2026-09-25.md](../research/module-design-workflow-check-2026-09-25.md) | [采用与证据范围](research-basis.md) |
| [opus-5-5-document-samples-2026-09-25.md](../research/opus-5-5-document-samples-2026-09-25.md) | [采用与证据范围](research-basis.md) |
| [pi/README.md](../research/pi/README.md) | [采用与证据范围](research-basis.md) |
| [prime-agent/README.md](../research/prime-agent/README.md) | [采用与证据范围](research-basis.md) |
| [public-technical-docs-comparison-2026-09-25.md](../research/public-technical-docs-comparison-2026-09-25.md) | [采用与证据范围](research-basis.md) |
| [system-one-models-2026-09-28.md](../research/system-one-models-2026-09-28.md) | [采用与证据范围](research-basis.md) |

研究原文和source-map中的来源摘要保持日期范围。当前正文与静态检查结果另在[审校页](review.md)登记，真实运行须附实际实现、环境和独立效果证据。
