# Harness 架构设计

Harness 是服务个人智能应用的任务运行框架。运行内核持续管理任务，记忆、大脑和执行系统分别提供信息、决策与行动能力；交互、授权、协作与运行保障使这些能力能够跨设备、跨等待和故障共同工作。

从[架构正文](main/README.md)开始，依次理解系统整体、子系统、跨模块协作和工程实现。正文中的局部例子已交代各自前提，可以按章节顺序阅读，也可以从组件全景直接进入对应设计。若想先观察一次完整运行，读[任务示例](examples/01-task-lifecycle.md)，再回到相应机制。

## 文档分工

| 入口 | 回答的问题 | 使用方式 |
| --- | --- | --- |
| [架构正文](main/README.md) | 系统为什么这样划分，各部分怎样工作和协作，怎样实现与验证？ | 按四部分顺序阅读；组件全景提供按职责进入的入口。 |
| [任务过程与分支](examples/README.md) | 同一项任务在正常和条件变化时，经历哪些交接、留下什么结果？ | 正常过程连续展开，分支各自说明起点与变化。 |
| [组件详细设计](design/README.md) | 下一层内部结构、算法和数据实现怎样落地？ | 任务运行内核已成文，含 Go 接口、SQLite DDL 和验收规格；其余章节仍在规划。 |
| [契约参考](reference/README.md) | 字段、接口、状态、参数和故障组合的精确定义是什么？ | 按对象或实现问题查阅。 |

## 从全景逐层展开

[目标与边界](main/01-system/01-purpose-and-scope.md) → [系统全貌](main/01-system/02-architecture-overview.md) → [组件全景](main/01-system/03-component-panorama.md) → [系统协作过程](main/01-system/04-system-collaboration.md)，先建立结构与运行关系。随后读[状态归属](main/01-system/05-domain-and-state.md)和[关键取舍](main/01-system/06-key-decisions.md)，再进入[子系统](main/02-subsystems/README.md)、[协作机制](main/03-coordination/README.md)和[工程实现与验证](main/04-engineering/README.md)。

子系统正文维护职责、对外行为、关键过程及成立条件；协作正文解释跨模块交接，引用各方的局部义务。参考资料维护完整规格。示例展示具体条件下的结果，不定义额外接口或通用状态。各篇保留理解当前问题所需的背景，深入链接不代替本篇解释。

详细设计接管某项规则时，先迁入完整内容，再把架构正文改为足够理解其边界的解释和准确链接。同一精确规则不在多处各自修改。图示与正文表达同一设计，一起核对条件、责任和状态含义。

## 设计状态与配套材料

当前交付是架构设计，尚无业务实现或运行验收结果。已确认要求、参考实现建议、教学假设与待验证效果分别标明；图文和阅读页检查不构成运行证据。项目要求见[目标基线](../harness-project-goals.md)，未定义的签名、Schema、实现参数及验证缺口见[待定事项](maintenance/open-questions.md)。

[组件全景 SVG](diagrams/system-panorama.svg)与[可编辑 draw.io](diagrams/system-panorama.drawio)提供结构视图；[机制核查图](diagrams/system-flow-map.html)用于定位具体交接。离线阅读页通过[构建说明](../../scripts/architecture/README.md)生成。历史材料保存在[旧版归档](../archive/architecture-v1/README.md)，来源对应及本轮检查保存在[维护资料](maintenance/reorganization-review.md)。

<a id="example-task"></a>
示例的共同条件与对象标识见[示例说明](examples/README.md#example-task)。
