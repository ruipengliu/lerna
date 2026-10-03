# 06 · 行动准入、授权与预算账本

Status: ready-for-agent
Phase: B
Implementation: not-started
Depends on: [04](../lerna-04-content-snapshots/spec.md)、[05](../lerna-05-task-requirements/spec.md)

## Problem Statement

用户需要保证目标、模型建议和插件声明不会被当作授权，也需要知道收费上限与最终费用是否已经关闭；仅把预算写进提示不能约束执行。

## Solution

交付受信 Grant / Confirmation、行动准入和预算账本。准确行动获准时同时固定许可使用、预留与责任，消费按累计来源结算；未获准或无法提供费用上界的行动不会进入真实发送路径。

## User Stories

1. As a user, I want grants bound to exact resources and purposes, so that a broad goal cannot authorize unrelated actions.
2. As a user, I want confirmations bound to exact parameters, so that approval cannot be reused for a changed action.
3. As a data owner, I want current permissions checked, so that queued actions respect restrictions.
4. As a user, I want enforceable reservations before spending, so that estimates cannot bypass my budget.
5. As a user, I want late usage corrections retained, so that the final bill is not silently understated.
6. As an operator, I want cumulative usage deduplicated, so that repeated receipts do not charge twice.
7. As a user, I want one-time grants to remain consumed, so that cancellation does not restore approval.
8. As a caller, I want action batches admitted atomically, so that partial local admission is visible and controlled.
9. As an operator, I want finite reconciliation reserves, so that cleanup cannot run without bounds.

## Implementation Decisions

- 实现 grant.issue / restrict / revoke、confirmation.decide、budget.reserve / settle，管理入口仅面向受信主体；接入 Content 与 ContextCompiler 当前权限检查。
- 准入事务检查准确参数、当前目标和控制、来源、Grant、Binding、额度与截止；同库 OperationIntent、GrantUse、Reservation、Task Responsibility、派发 Job 一起提交。
- 第一版一批最多四项相互独立行动，本地准入全收或全拒；每项来源键唯一。条件更新优先，串行动作等待前一效果。
- 远端 Grant 先签发绑定准确行动和有限额度的凭据，再本地准入；双方分别持久保存责任，不假设跨库原子提交。基线先实现同库流程，远端交接在 13 扩展。
- Confirmation 绑定原命令、参数摘要、请求修订、主体与有效期；实际业务变更再检查并一次消费。界面预览并不证明用户理解，一次性消费不因失败复活。
- 按可信可执行上界预留费用、调用数等单位，探索、核验、收尾各有有限额度；无法提供上界的能力拒绝硬预算自动计费。
- UseSettlement 按实际来源的累计修订结算增量；仅在无新增消费且最终用量已知时释放余量。终态后更正继续入账，未知费用不当零。
- 输出可由 Executor 核验的准确 GrantUse 和 TaskGate；06 以受控组件接收端验收，07 必须完成真实启动二次检查后才开放外部效果。

## Testing Decisions

通过受信管理合同与 Application 驱动行动提案，读取公开许可、Task 与用量视图。复用 03 的受控执行接收端及真实 PG；06 验证准入与账本，07 验证实际出口不能绕过门禁。

测试只断言公开行为、持久恢复结果和独立目标状态；不依赖私有表布局或内部调用次数。每个故障或拒绝案例必须配允许正常完成的对照。

验收条件：

1. 未授权、错用途、错参数、过期或跨租户请求不产生获准意图；合法准确行动获得固定使用与预留。
2. 并发预留不能超过根额度；四项行动中一项额度不足导致本批全拒，批准批次保留四个独立身份。
3. 确认后改变正文、路径、请求版本或本人身份不能消费原确认；合法原请求只消费一次。
4. 已消费许可在取消、失败、预留释放和重启后不能重用。
5. 重复与乱序累计用量只按有效修订结算增量；迟到更正保留依据，未知费用不释放预留。
6. 缺费用上界、核验额度不足或达到截止时拒绝新行动；已登记责任仍可在有限收尾额度中核对。
7. 准入事务故障后不存在单独扣额度却丢失意图或责任的半提交状态；回执丢失沿原键恢复。

## Out of Scope

- 父子预算转移和分布式撤销传播的完整实现，分别由 19 与 13 承接。
- 模型自报成本、用户批准估算额替代硬上界。

## Further Notes

授权和预算是首次真实动作的前置条件，不是后补治理。本切片无需真实供应商；费用来源可用可核对的确定性计费夹具，真实能力必须在 10 单独取证。

依赖项表示实现先决条件；`ready-for-agent` 表示规格已明确，不表示依赖已完成或能力已开放。全部验收通过并附准确版本、环境、命令、结果和限制后，才可将本切片记为完成。共同执行与证据规则见[切片索引](../lerna-implementation/README.md)。

依据：[governance](../../docs/architecture/governance.md#授权的两个检查点)、[governance](../../docs/architecture/governance.md#用户确认与界面责任)、[governance](../../docs/architecture/governance.md#预算预留与结算)、[task-lifecycle](../../docs/architecture/task-lifecycle.md#行动准入与真实启动)、[ADR-0006](../../docs/adr/0006-authorization-budget-at-action-boundaries.md)、[ADR-0007](../../docs/adr/0007-versioned-content-memory-snapshots.md)。
