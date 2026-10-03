# 18 · 记忆提取、更正与来源生命周期

Status: ready-for-agent
Phase: E
Implementation: not-started
Depends on: [10](../lerna-10-model-search-adapters/spec.md)、[14](../lerna-14-memory-core/spec.md)、[16](../lerna-16-activation-lifecycle/spec.md)

## Problem Statement

用户希望跨任务复用事实与偏好，但自动提取可能保存未经许可的轨迹、覆盖冲突事实，或在来源撤权后继续使用旧摘要和副本。

## Solution

在最小 Memory 合同上补齐有界提取、候选核验、一次发布、冲突与时间语义、来源更正及跨 holder 清理。记忆能带准确出处进入后续 Snapshot，并在资格失效时停止新使用。

## User Stories

1. As a user, I want extraction to require a saving purpose, so that task history is not silently turned into memory.
2. As a user, I want candidates reviewed before publication, so that model suggestions are not stored as established facts.
3. As a user, I want extraction resumed from checkpoints, so that a crash does not publish the same candidate twice.
4. As a user, I want conflicting sources preserved, so that uncertainty is not erased by a preferred model answer.
5. As a user, I want time-aware retrieval, so that a historical question does not always receive the newest claim.
6. As a data owner, I want corrections propagated to derived holders, so that stale summaries do not remain authoritative.
7. As a user, I want deletion residuals disclosed, so that offline copies are not described as erased.
8. As an operator, I want extraction and index costs accounted for, so that memory maintenance is not treated as free.
9. As a user, I want evidence invalidation visible, so that tasks cannot keep relying on a corrected source.

## Implementation Decisions

- 新增 memory.extract 及候选查询和发布所需同版合同；固定 extraction_id、输入、策略、saving_mode、数量、费用和期限。只有 complete Schema 与合同测试后才开放。
- 提取候选保存来源、准确内容、类型、观察时间、有效区间与许可；来源与冲突检查后，受信确认或有限预授权才发布。ExtractionCandidate 到 Memory 的发布只允许一次。
- 提取和投影工作沿原身份与 checkpoint 恢复；模型提取仍遵守每 Decision 一次物理请求与累计费用，不透明重跑收费步骤。
- 来源矛盾保留各自观察与适用时间，无法裁决标 needs_review，不仅保留模型偏好的断言；更正产生替代版本，历史不覆写。
- 来源限制传播到摘要、索引、模型产物、缓存及已登记 holder；读取、处理、保存、同步、披露分别核验，默认检索仍先授权再排序。
- 撤权先封新使用，再有界传播和清理；离线 holder 显示 residual / unknown、责任方和期限。索引只能从当前获准来源重建。
- 活动 Task 的旧证据失效，下一次 Snapshot 不再使用；终态 Result 保持不变，附当前证据缺陷通知。
- 词法检索保留为默认，向量与混合检索只作为可选候选；声称改善须通过 21 对照评测，不因实现完成默认开启。

## Testing Decisions

公开 Memory Component 方法与后续 Application Task 使用路径共同验收。复用 14 权限分页和 16 holder 生命周期，真实存储运行提取恢复、来源更正与断连清理；模型质量另记录。

测试只断言公开行为、持久恢复结果和独立目标状态；不依赖私有表布局或内部调用次数。每个故障或拒绝案例必须配允许正常完成的对照。

验收条件：

1. 有提取和保存许可的材料形成候选，经有效确认或预授权发布；仅有读取许可时不能永久保存。
2. 提取或发布中断后恢复原 checkpoint，同一候选最多发布一次，费用不因回执重放重复结算。
3. 冲突来源保留准确时间与 needs_review，历史查询返回适用版本而非无条件最新值。
4. 来源更正或撤权后，活动任务新 Snapshot 不使用失效内容，终态任务显示缺陷通知而不改写 Result。
5. 跨 holder 限制先阻止新使用；离线副本未清除时显示残留，重连后按原责任清理。
6. 分页期间撤权拒绝旧 cursor，索引重建不能恢复已删除或无权材料。
7. 所有提取、检索、索引和维护成本计入有限预算，达到上限停止新工作并保留可恢复缺口。
8. 无记忆和获准记忆的正常任务对照均可运行，不能靠禁用全部记忆通过撤权测试。

## Out of Scope

- 默认向量数据库、无许可全局经验库或训练数据集。
- 未经实验声称记忆提高准确率；自动发布插件代码。

## Further Notes

E 的记忆增强复用 D 的 14，避免建立第二个 Memory 权威。后续若增加检索策略，沿同版合同和 21 的费用、质量证据门槛实施。

依赖项表示实现先决条件；`ready-for-agent` 表示规格已明确，不表示依赖已完成或能力已开放。全部验收通过并附准确版本、环境、命令、结果和限制后，才可将本切片记为完成。共同执行与证据规则见[切片索引](../lerna-implementation/README.md)。

依据：[capabilities](../../docs/architecture/capabilities.md#memory-管理跨任务可复用的信息)、[governance](../../docs/architecture/governance.md#内容来源与数据生命周期)、[data-model](../../docs/architecture/data-model.md#记忆授权与扩展记录)、[ADR-0007](../../docs/adr/0007-versioned-content-memory-snapshots.md)、[ADR-0009](../../docs/adr/0009-versioned-activation-and-recovery.md)、[ADR-0010](../../docs/adr/0010-evidence-gated-improvement.md)。
