# 架构设计文档

| 日期 | 修订说明 |
| --- | --- |
| 2026-10-04 | 初版：目录骨架、文档层级、命名与链接规则、模块设计模板和文档索引。 |
| 2026-10-04 | 模板增加"M1 范围""测试要点""参考资料"三节；明确 `ports/` 文档与代码的对应关系；M1 文档清单加入交互适配器接口。 |
| 2026-10-04 | 索引登记核心、可替换部分、适配器、平台服务和专题共 28 篇设计草稿。 |
| 2026-10-04 | 分层与模块状态改为已采纳。 |
| 2026-10-04 | 新增[整体方案](overview.md)作为解释性入口，并增加其维护规则。 |
| 2026-10-04 | 改为首页：只保留阅读顺序和文档索引；层级、骨架、命名、写作规则、模板和 M1 文档清单移到[文档规范](conventions.md)。 |
| 2026-10-04 | 索引登记 ADR 0001、0002（来自[架构评审处理记录](../review/archive/round-1/README.md)）。 |
| 2026-10-04 | 索引登记 ADR 0003（来自[第二轮评审处理记录](../review/archive/round-2/disposition.md)）。 |
| 2026-10-05 | 首段的状态概括改为"以索引为准"。 |
| 2026-10-05 | 索引登记新的流程文档"模型调用"。 |
| 2026-10-05 | 模块名称统一为“执行网关”，职责与契约不变。 |
| 2026-10-05 | 模块名称统一为“执行管理”，同步模块简称与图示；存储和连续性语境中的“账本”指其持久执行记录。职责与契约不变。 |
| 2026-10-05 | 模块名称由执行网关改回出口闸门，避免与网关与 SDK 撞名；职责与契约不变。 |
| 2026-10-05 | 文档索引增加 [ADR 0004](../adr/0004-language-and-stack.md) 和[开发规范](../development.md)。 |

本目录存放 Lerna 的现行架构设计：一个面向个人用户、以可靠性契约为核心的 Agent Harness。目前只有设计，尚未实现；各文档的状态以第 2 节索引为准。

## 1 从哪里读起

| 你是 | 建议顺序 |
| --- | --- |
| 新加入的开发者 | [整体方案](overview.md) → [项目目标](project-goals.md) → [分层与模块](layers.md) → 要开发的模块文档 |
| 评审者 | [项目目标](project-goals.md) → [分层与模块](layers.md) → [整体方案](overview.md) 第 5 节 → 关心的模块 |
| 文档作者 | [文档规范](conventions.md)，再读要修改文档的上游 |

上下游关系：项目目标是全部文档的依据，分层与模块是各模块文档的直接上游；整体方案只做解释，不新增要求。术语以[项目目标第 3 节](project-goals.md#3-核心术语)为准。

## 2 文档索引

新增、移动文档或变更状态时，更新此表。层级的含义见[文档规范第 1 节](conventions.md#1-文档层级)。

| 文档 | 层级 | 状态 |
| --- | --- | --- |
| [项目目标](project-goals.md) | 0 目标 | 已采纳 |
| [分层与模块](layers.md) | 1 划分 | 已采纳 |
| [整体方案](overview.md) | 解释 | 草稿 |
| [文档规范](conventions.md) | 规范 | 已采纳 |
| [开发规范](../development.md) | 规范 | 已采纳 |
| [ADR 0001 持久性按部署档位声明](../adr/0001-durability-profiles.md) | 2 决定 | 已采纳 |
| [ADR 0002 授权区分操作权利与处理目的](../adr/0002-processing-purposes.md) | 2 决定 | 已采纳 |
| [ADR 0003 替换资格分三类，公共契约与内部接口分开](../adr/0003-replacement-classes-and-assembly.md) | 2 决定 | 已采纳 |
| [ADR 0004 开发语言与主要技术栈](../adr/0004-language-and-stack.md) | 2 决定 | 已采纳 |
| [ADR 0005 M1 计费来源与结算](../adr/0005-billing-source-identity.md) | 2 决定 | 已采纳 |
| [ADR 0006 M1 基础关联交接与本地诊断映射](../adr/0006-trace-source-handoff-local-mapping.md) | 2 决定 | 已采纳 |

| [ADR 0007 受管理单文件发布与证据交接](../adr/0007-managed-file-publication.md) | 2 决定 | 实施中 |
| [核心契约](core/contracts/README.md) | 3 设计 | 草稿 |
| [持久工作](core/durable/README.md) | 3 设计 | 草稿 |
| [执行管理](core/ledger/README.md) | 3 设计 | 草稿 |
| [任务编排](core/tasks/README.md) | 3 设计 | 草稿 |
| [授权](core/grants/README.md) | 3 设计 | 草稿 |
| [预算](core/budget/README.md) | 3 设计 | 草稿 |
| [会话](core/sessions/README.md) | 3 设计 | 草稿 |
| [内容治理](core/content/README.md) | 3 设计 | 草稿 |
| [运行记录](core/trace/README.md) | 3 设计 | 草稿 |
| [出口闸门](core/egress/README.md) | 3 设计 | 草稿 |
| [推理：接口](ports/reasoner/README.md) | 3 设计 | 草稿 |
| [推理：默认实现](ports/reasoner/default.md) | 3 设计 | 草稿 |
| [记忆策略：接口](ports/memory/README.md) | 3 设计 | 草稿 |
| [记忆策略：默认实现](ports/memory/default.md) | 3 设计 | 草稿 |
| [执行适配器：接口](ports/executor/README.md) | 3 设计 | 草稿 |
| [交互适配器：接口](ports/interaction/README.md) | 3 设计 | 草稿 |
| [API 适配器](adapters/api.md) | 3 设计 | 草稿 |
| [文件适配器](adapters/file.md) | 3 设计 | 草稿 |
| [GUI 适配器](adapters/gui.md) | 3 设计 | 草稿 |
| [外部 Agent 适配器](adapters/agent.md) | 3 设计 | 草稿 |
| [网关与 SDK](platform/gateway/README.md) | 3 设计 | 草稿 |
| [扩展管理](platform/extensions/README.md) | 3 设计 | 草稿 |
| [评测与进化](platform/eval/README.md) | 3 设计 | 草稿 |
| [流程：模型调用](flows/model-call.md) | 3 设计 | 草稿 |
| [数据与存储](topics/data-and-storage.md) | 3 设计 | 草稿 |
| [端云部署与故障域](topics/deployment.md) | 3 设计 | 草稿 |
| [安全与威胁模型](topics/security.md) | 3 设计 | 草稿 |
| [观测与诊断](topics/observability.md) | 3 设计 | 草稿 |
| [SQLite 本地档存储故障验收](verification/local-durability.md) | 5 验证 | 有条件准入，见平台表 |
