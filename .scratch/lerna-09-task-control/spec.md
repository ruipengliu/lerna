# 09 · 目标修订、控制与终态收尾

Status: ready-for-agent
Phase: B
Implementation: not-started
Depends on: [08](../lerna-08-verified-results/spec.md)

## Problem Statement

用户在任务运行中会补充要求、暂停或取消，但这些命令不能抹去已发生效果、迟到费用或未完成清理，也不能让旧提案继续行动。

## Solution

交付完整 task.steer / pause / resume / cancel 和可恢复控制传播。用户能看到目标状态与收尾状态的区别，旧工作保留证据但失去不适用的新行动资格。

## User Stories

1. As a user, I want to revise an active goal, so that future actions follow my current requirements.
2. As a user, I want pause to close new admission, so that the system stops starting new goal work.
3. As a user, I want explicit resume checks, so that expired budgets or permissions are not revived.
4. As a user, I want cancellation recorded immediately, so that I can distinguish my decision from remote cleanup.
5. As a user, I want late effects retained, so that cancellation does not hide actions already sent.
6. As a user, I want a failed or cancelled Result, so that partial artifacts and unresolved responsibilities remain understandable.
7. As an application developer, I want terminal tasks immutable, so that new work is represented by a linked task.
8. As an operator, I want cleanup states separated, so that spending and content removal can finish independently.
9. As a user, I want valid clarification answers consumed once, so that reconnecting does not duplicate decisions.

## Implementation Decisions

- 实现 active、waiting、paused 的允许转换及 succeeded / failed / cancelled 不可复活；worker 退出不直接把 Task 变 failed。
- steer 校验 expected_goal_revision，先收紧旧目标准入并增加控制修订，保存重建目标与传播责任；已暂停任务的新输入不自动恢复。
- pause / cancel 在 Orchestrator 控制事务封新准入并保存向绑定执行端传播的 Job；不能声称所有远端效果已停止。
- failed / cancelled 同事务固定对应 Result，包含部分成果、未满足条件、原因与未结责任。effects_closed、spending_closed、cleanup_complete 分别更新。
- 更高 TaskGate 使可证明未发送的旧操作关闭，已可能发送的继续核对；resume 只开放新一轮准入，不能复活旧墓碑。
- 迟到合法 Effect 和费用按原身份归并，旧 Proposal 不再采纳；无进展、预算或期限达到限制后停止新探索，在有限收尾额度中工作。
- 交互答案绑定原业务请求、修订、主体与期限，普通聊天不是受信授权；终态补充工作创建关联新 Task。

## Testing Decisions

只从 Application 命令和 TaskView 驱动主要场景，配合公开 Executor 查询与独立目标观察。复用 02、03 的故障注入以及 06 的累计计费夹具。

测试只断言公开行为、持久恢复结果和独立目标状态；不依赖私有表布局或内部调用次数。每个故障或拒绝案例必须配允许正常完成的对照。

验收条件：

1. steer 与旧 Proposal 同时到达，只允许当前修订准入；旧提案与已发生费用仍可追溯。
2. 暂停后不再准入目标行动，新输入不恢复；受信 resume 在当前期限、预算和许可有效时才继续。
3. 发送后延迟目标响应再取消，Task 先变 cancelled 并有固定 Result；迟到效果仍归并，收尾标志分别变化。
4. 取消传播乱序或丢回执可恢复；先取消后 invoke 的墓碑持续生效，低版本控制不反转状态。
5. 终态后收到更高累计用量，只结算增量，不改写 Result、不恢复一次性许可。
6. 对 succeeded / failed / cancelled 执行 resume 或 steer 被拒绝；关联新目标生成新 Task。
7. 模型返回前退出、写入后丢回执、改目标时旧提案返回三种中断均可重现，正常场景仍完成。

## Out of Scope

- 跨 owner 子 Agent 的取消传播，留给 19。
- 把用户取消解释为历史动作已撤销或所有数据已删除。

## Further Notes

01–09 的出口是有真实存储、真实受管写入、确定性决策与核验的可控完整任务。此时仍未证明真实模型质量、远程互操作或生产容灾。

依赖项表示实现先决条件；`ready-for-agent` 表示规格已明确，不表示依赖已完成或能力已开放。全部验收通过并附准确版本、环境、命令、结果和限制后，才可将本切片记为完成。共同执行与证据规则见[切片索引](../lerna-implementation/README.md)。

依据：[task-lifecycle](../../docs/architecture/task-lifecycle.md#控制状态与恢复)、[task-lifecycle](../../docs/architecture/task-lifecycle.md#防止无效循环)、[governance](../../docs/architecture/governance.md#预算预留与结算)、[validation](../../docs/architecture/validation.md#故障与并发反例)、[ADR-0001](../../docs/adr/0001-task-session-separation.md)、[ADR-0002](../../docs/adr/0002-stable-kernel-replaceable-strategies.md)、[ADR-0005](../../docs/adr/0005-explicit-effect-uncertainty.md)、[ADR-0006](../../docs/adr/0006-authorization-budget-at-action-boundaries.md)。
