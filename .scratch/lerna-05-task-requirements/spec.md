# 05 · 目标接纳、条件与提案消费

Status: ready-for-agent
Phase: B
Implementation: not-started
Depends on: [03](../lerna-03-deterministic-harness/spec.md)、[04](../lerna-04-content-snapshots/spec.md)

## Problem Statement

用户的原目标、可检查条件和模型提案尚无稳定归属；缺少版本控制会使旧提案作用于新要求，或让空条件集合误导完成判断。

## Solution

交付可通过 Application 提交、查询并推进的 Task。规则决策提出条件和下一步，Orchestrator 记录覆盖依据、固定 Snapshot、唯一消费当前提案，并在缺信息时持久等待。

## User Stories

1. As a user, I want my original requirements retained, so that a model cannot silently weaken them.
2. As a user, I want content, effect, and process conditions distinguished, so that completion covers the entire goal.
3. As an application developer, I want stable task ownership, so that reconnecting does not relocate authority.
4. As a user, I want clear clarification requests, so that ambiguous goals can be resolved.
5. As a caller, I want revision conflicts reported, so that concurrent changes do not overwrite one another.
6. As a user, I want stale proposals ignored, so that old decisions cannot act on new requirements.
7. As an operator, I want durable wait conditions, so that waiting tasks survive worker replacement.
8. As a user, I want bounded progress, so that repeating plans cannot consume resources indefinitely.

## Implementation Decisions

- 实现 task.submit、task.get、task.list、task.answer 及必要 Decision 派发与结果消费；Task 固定 owner、原文、TaskPolicy、预算引用和期限。Task 不依赖 Session 存活。
- 维护独立 revision、goal_revision、control_revision；Requirement 记录来源、kind、required、rule_ref 与适用成果，显式覆盖原始要求。
- 增量更新保留未提及条件，同义去重，替换指向准确旧条件；实质变化先提交目标修订，同 Proposal 中的行动和完成建议失效。
- 同 Task 默认只有一个可消费前台 Decision；按 Decision 身份唯一消费，重查 Snapshot、目标、控制与来源资格。迟到提案保留记录与费用但不采纳行动。
- InputRequest 保存准确问题、答案 Schema、目标、修订和期限；task.answer 只消费一次。等待释放 worker，并记录负责方、重查条件与最晚处理时间。
- TaskPolicy 限定时间、调用次数、并发和无进展上限；重复改写计划不算进展，策略或 worker 更换不重置累计计数。
- 本切片只推进条件、规则提案与输入等待；行动准入交给 06，效果与正式成功交给 07–08，未就绪分支明确等待或 unsupported，不伪造成功。

## Testing Decisions

主边界为 Application 的 task.submit / get / answer，规则决策使用公开 Component 接口。复用 01–04 合同、真实 PG 和确定性基线，不通过 Orchestrator 私有函数注入成功。

测试只断言公开行为、持久恢复结果和独立目标状态；不依赖私有表布局或内部调用次数。每个故障或拒绝案例必须配允许正常完成的对照。

验收条件：

1. 同原命令重传只得到一个 Task，原文、owner 和策略保持；不同租户不可查询。
2. 空条件、仅保留易检查条件或删除明确要求时 requirements_ready 不成立，Task 不能成功。
3. 新条件提交增加目标修订，同提案行动不派发；重复等价条件不无谓增加修订。
4. 一个 Decision 的重复或乱序完成最多消费一次；快照过期不产生新行动。
5. 输入等待重启后可恢复；错误答案、过期请求和错误修订拒绝，合法答案只消费一次并唤醒原任务。
6. 强制上下文溢出可通过 TaskView 观察，模型出口没有发送；修正输入后可继续正常规则决策。
7. 无进展或调用次数到上限时停止新推进并暴露原因；不能通过换 worker 重置限额。

## Out of Scope

- 实际收费请求、真实外部写入、完整 Session 与界面。
- 由规则引擎自行宣布 Task 成功。

## Further Notes

05 的演示出口是“原目标→条件→Snapshot→规则提案或持久输入等待”。完整单目标成功出口由 08 汇合，避免在早期用假 Result 代替效果证据。

依赖项表示实现先决条件；`ready-for-agent` 表示规格已明确，不表示依赖已完成或能力已开放。全部验收通过并附准确版本、环境、命令、结果和限制后，才可将本切片记为完成。共同执行与证据规则见[切片索引](../lerna-implementation/README.md)。

依据：[task-lifecycle](../../docs/architecture/task-lifecycle.md#从原始输入建立可检查目标)、[task-lifecycle](../../docs/architecture/task-lifecycle.md#一轮推进的算法)、[task-lifecycle](../../docs/architecture/task-lifecycle.md#防止无效循环)、[contracts](../../docs/architecture/contracts.md#应用接入方法)、[ADR-0001](../../docs/adr/0001-task-session-separation.md)、[ADR-0002](../../docs/adr/0002-stable-kernel-replaceable-strategies.md)、[ADR-0003](../../docs/adr/0003-stable-owner-distributed-deployment.md)。
