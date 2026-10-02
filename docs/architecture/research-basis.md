# 研究依据、采用范围与候选实验

[总览](README.md) · [设计决策](decisions.md) · [优化验收](validation/optimization-evidence.md) · [来源追踪](coverage.md)

报告任务需要真实来源、准确保存和中断恢复。参考项目说明了历史、模型窗口、工具入口和进程接替的具体做法，也暴露了相同名称下不同的成功与耐久含义。本方案按行为吸收这些经验；研究仓库的功能、作者报告的实验和本项目的运行保证分别取证。

研究文件保留原调研日期、固定 commit、论文版本及证据限制。正式正文采用的规则在所属模块唯一维护；候选算法通过冻结对照后才能改变默认装配。本页索引研究如何影响设计，不重新声明上游当前版本或复现实验已经通过。

## 1. 五个 Harness 参考项目

| 固定研究 | 已进入设计的问题与机制 | 正式主定义与保留边界 |
| --- | --- | --- |
| [Codex](../research/codex/README.md) | 历史与模型输入分开；压缩后机械重建目标约束；准确工具绑定；异步子句柄与冷恢复 | [固定输入](brain/implementation.md#snapshot-reconstruction)、[最终工具准入](execution/README.md#final-tool-admission)、[子任务恢复](collaboration/implementation.md#cold-child-recovery)。JSONL flush、合成中断或普通提供方重试不提供本方案的发送和效果保证 |
| [Pi](../research/pi/README.md) | 必要 mutation 共同提交；原工具输入输出；恢复时当前资格；装配的 staged commit／discard | [可靠工作](reliable-work.md)、[原执行结果](execution/implementation.md)、[分阶段就绪](extensions/implementation.md#staged-readiness)。classic CLI、AgentHarness、pi-durable 分开，恢复计算状态不恢复外部效果 |
| [Prime Agent](../research/prime-agent/README.md) | Session 与机械执行状态分开；近期来源锚点；有界 spawn／collect；无进展限额；环境 busy 与实际退出 | [有界进展](orchestrator/implementation.md#bounded-progress)、[子等待](collaboration/implementation.md#async-child)、[程序化工具](execution/programmatic-tools.md)。mtime 不提供 CAS；abort 不证明退出；refine 产物须经过独立改善证据 |
| [Crush](../research/crush/README.md) | 输入接纳序与控制水位；旧工作者按原关联清理；配置代次；变化提示后重读；媒体完整性 | [输入竞争](interaction/implementation.md#input-control-races)、[旧呈现隔离](interaction/implementation.md#surface-generation)、[实例就绪](extensions/implementation.md)、[输出覆盖](execution/implementation.md)。内存接纳不等于持久 Task，hook allow 不构成 Grant |
| [DeepSeek Harness](../research/deepseek-harness/README.md) | 准确依赖与事件快照；非空 batch sync；模型／工具入口前 checkpoint；typed unknown；激活与持续 Session 分开 | [决策发送](brain/implementation.md)、[操作恢复](execution/implementation.md)、[准确装配](extensions/implementation.md)。仅显式安装 checkpoint 的路径具有相应屏障；jobs-local 不等价事务 JobStore，不能跨 bundle 拼接保证 |

跨项目综合采用 O-01～08、O-12 中适用的输入保真、有界推进、最后准入、分域恢复、原结果、薄 SDK、呈现竞争及静态装配机制。O-09 经验精炼、O-10 程序化工具通用增强、O-11 大目录发现仍按能力和实验启用。完整分析见[综合研究](../research/agent-harness-comparison/README.md)与[优化建议](../research/agent-harness-comparison/architecture-optimization.md)；采用位置和 X-01～09 对照因子见[优化验收](validation/optimization-evidence.md)。

[语义覆盖矩阵](../research/agent-harness-comparison/core-model-semantic-coverage.md)逐项解释 16 类行为；G-01 分支、G-02 自由输入队列、G-03 可复用子会话、G-04 Schedule、G-05 通用执行环境尚需独立公共合同。G-06 Memory、G-07 扩展保留已有合同，按需启用。字段可序列化、名称相同和历史可读都不足以证明完整运行语义。

## 2. 数据组织和读写计量

[读写对照](../research/agent-harness-comparison/data-flow-io-comparison.md)及 [Pi／Crush](../research/agent-harness-comparison/io/pi-crush.md)、[Codex／Prime](../research/agent-harness-comparison/io/codex-prime.md)、[DeepSeek](../research/agent-harness-comparison/io/deepseek.md)固定各项目的实际装配。可以吸收同阶段记录共同提交、准确正文复用、有限投影、小目录静态装配和流片段有界缓冲；仍须保存可能发送屏障、当前许可、未知效果和账务责任。

历史计数区分逻辑记录、读取／写入、事务、flush、sync、字节与物理 IO。当前 [A／B／C／D 数据流程](request-data-flows.md)使用自身的受限模板及完整目标覆盖前提；冷恢复重建止于原历史、关联和责任可查询，恢复后续行另计。不能将历史调用分析移作当前完整任务的固定上限，也不能用阶段数推导 SQL、fsync 或时延收益。

## 3. Memory 论文与开源库

[论文研究](../research/agent-memory-papers-2026-09-28.md)支持加强候选保真、新信息覆盖、时间与替代关系、读时整理及全生命周期关闭检验。[开源库研究](../research/agent-memory-libraries-2026-09-28.md)吸收 LangMem 的提取／存储分工、Graphiti 的时间建模与混合召回配方、memU 的合成分工，保留 Letta 和 OpenViking 分层载入作为候选。保留 Go Memory owner、PostgreSQL、准确内容及来源清理合同，不整体移植另一运行时的状态和权限体系。

[Memory 优化](memory/optimization-plan.md)区分默认 B0 字面基线、验证后可启用的 B1 中文词法、默认关闭的 B2 混合候选。首轮向量实验使用本地编码器和既有 PostgreSQL 的精确计算，不新增独立向量库；获准处理集合先于候选计算，索引水位和查询降级显式记录。[Memory 验收](memory/validation.md)固定构建、读取、清理、模型及本地资源完整成本，论文作者结果不等于本项目收益。

## 4. 固定答案类型和模型选择

[双系统模型研究](../research/system-one-models-2026-09-28.md)提供 Jev／AnyJev／TypeLLM 的不同实验路线。当前 [Brain 决策路径](brain/decision-paths.md)先确定规则是否完整覆盖，未覆盖才进入本轮固定模型；规则冲突不靠隐式模型掩盖。固定答案类型判断须经质量、费用和提供方恢复能力验证后启用，拒判升级另建 Decision 且不重置原链额度。研究中缺少可信费用上界、幂等或终结证据的路线不能获得对应硬保证。

## 5. 质量与可验证改进

[AI 报告来源核对](../research/ai-report-2026-09-source-check.md)限定摘要材料与时间窗口；[项目影响分析](../research/ai-report-2026-09-project-implications.md)形成九模块 24 项方向，覆盖目标约束、来源、实际效果、隔离、独立评估和全成本。采用方向已落实为[优化验收](validation/optimization-evidence.md)；实际策略收益须由固定候选、基线和独立测试证据支撑。一般质量评估、专项校准和软件改善发布的证据门槛分别成立。

## 6. 历史形式化与写作研究

[形式方法](../research/formal-methods-for-architecture.md)、[单项交接结果](../research/handoff-verification-results.md)、[全机制覆盖](../research/all-mechanisms-coverage.md)、[验证结果](../research/all-mechanisms-verification-results.md)对应 2026-09-26 归档模型。它们说明有限配置或规则形式化在当时范围内的结果，可提供交接、资格、预算和错误变体问题清单；不能移作当前九模块的组合证明、数据库故障恢复或生产容量证据。

[架构基线](../research/agent-harness-comparison/architecture-baseline.md)、[静态研究验证](../research/agent-harness-comparison/verification.md)、[固定来源](../research/agent-harness-comparison/sources.json)、[研究检查器](../research/agent-harness-comparison/verify-research.py)保留原快照和检查范围。正式文档变化不重写历史摘要来制造“仍然相同”的结论。

[公共技术文档比较](../research/public-technical-docs-comparison-2026-09-25.md)、[Opus 文档样本](../research/opus-5-5-document-samples-2026-09-25.md)、[写作偏好](../research/model-taste-x-2026-09-25.md)和[局部工作流试用](../research/module-design-workflow-check-2026-09-25.md)用于选择场景开篇、概念顺序和独立读者复述。它们是有限样本的写作依据，不提供普遍模型排名或完整系统验证。

当前静态检查、读者审校和待运行边界见[检查结果](review.md)。
