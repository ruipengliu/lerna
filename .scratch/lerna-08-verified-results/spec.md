# 08 · 逐条件核验与正式结果

Status: ready-for-agent
Phase: B
Implementation: not-started
Depends on: [05](../lerna-05-task-requirements/spec.md)、[07](../lerna-07-execution-reconciliation/spec.md)

## Problem Statement

用户需要的是满足当前全部要求的成果；模型输出最终文本、文件 API 成功或条件平均高分，都不足以证明目标完成。

## Solution

交付确定性 Evaluator 和 Orchestrator 的成功事务。通过公开 Task 接口完成“固定目标→规则决策→获准写入→独立读回→逐条件检查→正式 Result”的第一条完整路径。

## User Stories

1. As a user, I want every required condition checked, so that a strong answer cannot hide a missing external effect.
2. As a user, I want unknown evidence preserved, so that missing proof cannot become a pass.
3. As an application developer, I want an immutable Result, so that provisional output cannot be confused with completion.
4. As a user, I want current goal revisions verified, so that old evidence cannot finish changed requirements.
5. As an operator, I want pending dispatches included, so that an unacknowledged operation cannot escape the completion gate.
6. As a user, I want save-and-readback completed first, so that required delivery is not deferred until after success.
7. As a user, I want evidence limitations disclosed, so that assessed quality is not presented as calibrated accuracy.
8. As a user, I want later evidence defects visible, so that an immutable result does not conceal new information.

## Implementation Decisions

- 实现 evaluator.check / get，输入绑定准确 Requirement、目标修订、成果、规则和证据；输出 pass / fail / unknown、assurance、适用范围与有效性。
- 确定性规则首先覆盖目标文件位置、摘要及条件覆盖；规则不能在检查时隐式增删用户要求。开放质量只声明已取得的 assessed 范围，不默认准确率已校准。
- 所有核验读取与模型评估同样经过 06–07 的许可和预算，不用 Evaluator 旁路目标出口。
- Orchestrator 在同一事务锁定 Task，复查当前条件、证据资格、目标及输入竞争、完整 Responsibility 集合，再固定唯一 Result 与终态、保存交付 Job。
- 相关效果必须确定且不会迟到改变成果；已准入尚未远端接纳的责任也计入，不用列表第一页或异步计数证明零责任。
- Result 保存准确目标修订、成果、逐条件判断、限制与时间。用户要求的保存必须先完成并读回，后续通知投递可以恢复。
- 成功后证据缺陷以独立通知记录，不改写 Result 或复活 Task；活动任务不得继续使用已失效判断。失败与取消的正式 Result 在 09 完成控制流程。

## Testing Decisions

Application 是主验收入口，Evaluator 的可替换性另用公开 Component 合同验证。复用 03–07 的确定性场景、真实数据库与独立文件读回；无既有产品测试可直接继承。

测试只断言公开行为、持久恢复结果和独立目标状态；不依赖私有表布局或内部调用次数。每个故障或拒绝案例必须配允许正常完成的对照。

验收条件：

1. 正常目标从公开提交推进到唯一 succeeded Result，所有必要条件为当前有效 pass，准确文件可独立读回。
2. 漏掉来源、错误摘要、缺必要条件或 unknown 证据时不能成功；修复证据后正常完成。
3. Evaluator 成功返回但条件 verdict 为 fail，不可被当作通过；高文字评分不能抵消失败效果条件。
4. 故意延迟远端 invoke 接纳回执，Task 侧责任阻止完成，即使 Executor 查询暂为空。
5. 完成事务与新增输入、条件变化或责任登记竞争，旧判断不能完成新目标；重复候选只固定一个 Result。
6. 仅模型最终文本或远端 completed，不能直接改变 Task 终态。
7. 正式结果后通知失败可恢复投递；证据失效通知保留原 Result，并在当前视图显示限制。

## Out of Scope

- 正式质量统计校准、综合分替代必要条件。
- 任意文档内容真实性保证或全能规则系统。

## Further Notes

这是 B 阶段首条可演示成功路径；09 补齐用户控制和失败、取消收尾后，才能宣布 B 的中断恢复出口完成。

依赖项表示实现先决条件；`ready-for-agent` 表示规格已明确，不表示依赖已完成或能力已开放。全部验收通过并附准确版本、环境、命令、结果和限制后，才可将本切片记为完成。共同执行与证据规则见[切片索引](../lerna-implementation/README.md)。

依据：[task-lifecycle](../../docs/architecture/task-lifecycle.md#完成判定)、[capabilities](../../docs/architecture/capabilities.md#evaluator-检查条件但不裁决任务)、[validation](../../docs/architecture/validation.md#首条完整场景)、[data-model](../../docs/architecture/data-model.md#任务内部记录)、[ADR-0001](../../docs/adr/0001-task-session-separation.md)、[ADR-0002](../../docs/adr/0002-stable-kernel-replaceable-strategies.md)、[ADR-0005](../../docs/adr/0005-explicit-effect-uncertainty.md)。
