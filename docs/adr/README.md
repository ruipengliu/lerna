# 架构决策记录

本目录归档 [2026-10-03 架构设计](../architecture/README.md)中已经选择、且会影响领域边界或长期工程成本的关键取舍。领域语言和共同约束见根 [CONTEXT.md](../../CONTEXT.md)；方法、字段和算法仍查阅 `docs/architecture`，ADR 说明选择及理由。

以下记录均从既有设计整理。`status: accepted` 表示已作为当前设计基线采用，不表示实现、合同测试、质量评测或生产验证已经完成；`date` 为本次记录日期。待取得的工程证据见 [实施与验证](../architecture/validation.md)，既有文档检查范围见 [验证边界](../architecture/verification.md)。

## 决策索引

| ADR | 决策 | 状态 | 原设计取舍 |
| --- | --- | --- | --- |
| [0001](0001-task-session-separation.md) | Task 与 Session 分离，终态与收尾分离 | accepted | [目标与对话分离](../architecture/decisions.md#目标与对话分离) |
| [0002](0002-stable-kernel-replaceable-strategies.md) | 稳定任务语义，由可替换策略提出建议 | accepted | [任务语义内核与可替换策略分开](../architecture/decisions.md#任务语义内核与可替换策略分开) |
| [0003](0003-stable-owner-distributed-deployment.md) | 固定逻辑 owner，独立伸缩进程和 worker | accepted | [固定逻辑负责方并独立伸缩工作者](../architecture/decisions.md#固定逻辑负责方并独立伸缩工作者) |
| [0004](0004-transactional-durable-work.md) | 用本地事务共同提交事实与持久工作 | accepted | [共用持久工作机制并保留领域阶段](../architecture/decisions.md#共用持久工作机制并保留领域阶段) |
| [0005](0005-explicit-effect-uncertainty.md) | 显式记录未知效果，沿原身份核对 | accepted | [明确记录未知效果](../architecture/decisions.md#明确记录未知效果) |
| [0006](0006-authorization-budget-at-action-boundaries.md) | 准入与真实启动执行授权和预算约束 | accepted | [决策预算可感知且由内核强制执行](../architecture/decisions.md#决策预算可感知且由内核强制执行)、[授权检查](../architecture/governance.md#授权的两个检查点) |
| [0007](0007-versioned-content-memory-snapshots.md) | 分离准确内容、长期记忆和决策快照 | accepted | [使用准确内容引用与可重建检索](../architecture/decisions.md#使用准确内容引用与可重建检索) |
| [0008](0008-internal-contracts-external-protocol-adapters.md) | 公共合同保留内部语义，外部协议经适配器接入 | accepted | [公共协议与外部生态协议分离](../architecture/decisions.md#公共协议与外部生态协议分离) |
| [0009](0009-versioned-activation-and-recovery.md) | 固定在途版本，分别管理安装、批准与就绪 | accepted | [版本发布不改变在途工作的含义](../architecture/decisions.md#版本发布不改变在途工作的含义) |
| [0010](0010-evidence-gated-improvement.md) | 以业务效果和故障证据控制优化与发布 | accepted | [用业务效果和故障证据评价先进性](../architecture/decisions.md#用业务效果和故障证据评价先进性) |

## 维护规则

编号按 `NNNN-slug.md` 递增，不复用旧编号。只有存在真实取舍、改变成本较高且后续读者需要理解理由时才新增 ADR；字段明细、临时限额和普通实现步骤留在设计参考或任务记录中。

状态使用 `proposed`、`accepted`、`deprecated` 或 `superseded by ADR-NNNN`。改变已采纳决定时，新建 ADR 并在旧记录中标明替代关系；保留历史背景、代价和来源。文字修正可以直接修改，语义变更同时核对 `CONTEXT.md` 和相关架构章节，不把实现现状或研究结果倒写成已经取得的验证结论。
