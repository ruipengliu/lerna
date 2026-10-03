# 19 · 内部与外部 Agent 有界委派

Status: ready-for-agent
Phase: E
Implementation: not-started
Depends on: [09](../lerna-09-task-control/spec.md)、[13](../lerna-13-distributed-runtime/spec.md)、[16](../lerna-16-activation-lifecycle/spec.md)

## Problem Statement

复杂任务需要独立子目标，但远端 Agent 的 completed、取消回执或费用估计不能保证父目标成功，也不能证明所有子行动已关闭。

## Solution

实现内部跨 owner 子 Task 与外部 A2A 委派，固定父目标修订、子条件、有限权限、根预算分配和证据要求。父任务独立验收，并持续追踪取消、未知效果与未结消费。

## User Stories

1. As a user, I want child goals independently bounded, so that delegation cannot expand my original authority.
2. As a user, I want all child spending counted under one root, so that parallel branches cannot multiply my budget.
3. As a parent-task owner, I want responsibility recorded before remote creation, so that a lost receipt cannot hide active work.
4. As a user, I want parent changes propagated, so that obsolete child goals cannot keep consuming resources.
5. As a user, I want child evidence checked locally, so that remote completion cannot falsely finish my task.
6. As an operator, I want cycles and excessive depth rejected, so that delegation cannot recurse without bounds.
7. As a user, I want unknown remote closure visible, so that cancellation is not mistaken for proven stopping.
8. As a resource owner, I want conflicting child actions coordinated, so that parallel research does not race shared mutable resources.
9. As a user, I want child allocations reclaimed only after closure, so that the same money cannot be spent twice.

## Implementation Decisions

- 实现 delegation.create / get / cancel；Delegation 固定 parent_goal_revision、独立子目标、条件、授权范围、allocation、deadline 和原远端键。
- 父 Task 事务在远端创建前登记 Responsibility、额度分配与投递 Job；子 Task 由自己的 owner 管理，返回结果和证据，不直接改父状态。
- 根侧先扣可用额度，子侧只获得固定有限分配；跨 owner 签发和回收按原身份持久交接。子消费未关闭前原分配不能回收重用。
- 委派前验证子问题可独立验收、材料共享获准、深度和循环限制、可变资源占用；子调用及汇总费用全部计入根预算。
- 父目标修订默认封旧活动委派的新行动资格，在同一控制事务登记取消或收紧传播；跨 owner 旧有限窗口按原期限收尾，公开剩余风险。
- 需要继续的子目标必须先关闭旧目标行动与效果，再由新修订创建新 Delegation；旧结果仅作为需重新核验的候选证据。
- A2A 适配固定声明版本与远端句柄，探针检查创建去重、原查询、取消关闭、费用和产物；缺保证时限制为受限咨询或保留未结，不伪造关闭。
- 父成功等待相关子目标终结及效果关闭，失败或取消可以先固定 Result 并保留收尾；纯远程组件调用不额外虚构 Delegation。

## Testing Decisions

从 Application 父任务控制并通过公开 Delegation / Component 方法观察，实际运行两个 owner 数据库与真实 A2A 协议服务。复用 13 交接故障和 06 预算套件，独立核对远端目标及根账本。

测试只断言公开行为、持久恢复结果和独立目标状态；不依赖私有表布局或内部调用次数。每个故障或拒绝案例必须配允许正常完成的对照。

验收条件：

1. 内部跨 owner 委派完成子目标，父按当前必要条件核验后才成功，子 completed 本身不能完成父目标。
2. 创建已发生但回执丢失，只查询或重传原键，父责任不丢失；无去重保证的远端不自动重复创建。
3. 并行子分配加根消费不突破根额度，重复回执不重复扣费；子侧消费未知时根侧不能回收原额度。
4. 父 steer 时子任务失联，旧委派封新资格且传播持久保存，有限旧窗口可见；旧结果按新条件重新核验。
5. 父 cancel 先固定取消 Result，逐项关闭情况继续更新，不能把自然语言“停止”当效果关闭。
6. 循环、过深、越权资料和冲突可变资源请求被拒绝；合法并行独立研究可完成。
7. 真实 A2A 服务返回 completed 但产物错误、句柄过期或取消不终止时，本地保持 fail / unknown 或明确限制。
8. 所有分支、汇总和迟到费用进入同一根账本，终态后更正仍保留。

## Out of Scope

- 无限自治、任意 DAG 平台、委派自动扩权。
- 缺远端合同仍声称恰好一次或所有远端效果已停止。

## Further Notes

外部服务必须声明可支持的保证；只有协议连通不能算验收。MCP 的工具适配在 10，A2A 的独立 Agent 语义在本切片，二者不共用 Lerna Task 状态机。

依赖项表示实现先决条件；`ready-for-agent` 表示规格已明确，不表示依赖已完成或能力已开放。全部验收通过并附准确版本、环境、命令、结果和限制后，才可将本切片记为完成。共同执行与证据规则见[切片索引](../lerna-implementation/README.md)。

依据：[capabilities](../../docs/architecture/capabilities.md#多-agent-协作)、[contracts](../../docs/architecture/contracts.md#外部协议适配)、[governance](../../docs/architecture/governance.md#预算预留与结算)、[task-lifecycle](../../docs/architecture/task-lifecycle.md#控制状态与恢复)、[ADR-0003](../../docs/adr/0003-stable-owner-distributed-deployment.md)、[ADR-0005](../../docs/adr/0005-explicit-effect-uncertainty.md)、[ADR-0006](../../docs/adr/0006-authorization-budget-at-action-boundaries.md)、[ADR-0008](../../docs/adr/0008-internal-contracts-external-protocol-adapters.md)。
