# 12 · 真实研究报告应用闭环

Status: ready-for-agent
Phase: C
Implementation: not-started
Depends on: [10](../lerna-10-model-search-adapters/spec.md)、[11](../lerna-11-application-sdk/spec.md)

## Problem Statement

应用团队需要一条能实际接入的报告流程，验证框架是否支持真实资料、准确保存、用户控制与明确成本，而不只是零散组件演示。

## Solution

交付 React / TypeScript 示例应用和默认报告策略，并由至少一个内部应用使用公开 SDK 接入。用户提交问题、范围、来源、保存位置、预算与期限，最终获得有引用、已读回核验的报告或明确部分结果。

## User Stories

1. As a report user, I want to specify scope and source requirements, so that the report answers my actual question.
2. As a user, I want to choose an authorized save location, so that the artifact is delivered where I need it.
3. As a user, I want visible progress and waiting reasons, so that I know whether input or recovery is required.
4. As a user, I want to clarify and revise requirements, so that the current goal guides subsequent work.
5. As a user, I want pause, resume, and cancel controls, so that long-running research remains controllable.
6. As a user, I want source-backed claims and readback evidence, so that I can assess both content and delivery.
7. As a user, I want costs and limitations shown, so that partial results are not disguised as success.
8. As an internal application developer, I want an integration example using only public contracts, so that I do not depend on framework internals.
9. As a reviewer, I want measured integration steps, so that missing rules and SDK friction can be corrected.

## Implementation Decisions

- 默认流程为原文与条件、真实检索与正文、准确报告 Content、获准保存、独立读回、逐条件核验、Result；沿用有界 ReAct，不增加另一 Task 权威。
- 示例采用设计基线的 React / TypeScript 和 11 的受信 Renderer；应用只使用公开 SDK，不能导入默认组件私有存储。
- 用户输入含比较范围、来源时效要求、保存目标、TaskPolicy、有限预算与期限；缺条件请求澄清，不用示例阈值暗中增加通用要求。
- 界面区分输入保存、Task 接纳、临时进度和正式 Result；失败、取消与效果、费用、清理状态独立呈现。
- 报告逐项关联准确来源与核验证据；开放质量判断显示 assessed 范围，无法回读、缺来源或效果未知时不成功。
- 先采用接入应用硬限制；没有时使用有上限的试运行测基线，再在正式验收前冻结费用、耗时与重试配置。
- 邀请至少一名实际应用接入者只凭 SDK 与文档接入，记录追问、首个有效结果步骤、接入代码量和恢复困难；这些是本切片待取得证据。

## Testing Decisions

通过真实浏览器用户流程与 Application SDK 验收，在真实模型、来源、保存目标和数据库上运行；复用 09 三种中断用例。模拟故障、真实接入与质量统计分别出报告。

测试只断言公开行为、持久恢复结果和独立目标状态；不依赖私有表布局或内部调用次数。每个故障或拒绝案例必须配允许正常完成的对照。

验收条件：

1. 至少一个内部应用完成真实问题到有引用报告、指定位置写入和独立读回的闭环，所有必要条件可追溯。
2. 缺来源、引用不支持结论、读回不符或未知效果时显示未完成原因，不标记 succeeded。
3. 模型响应前退出、目标写入后丢回执、改目标时旧提案返回均正确恢复，不重复效果、不采纳旧行动。
4. 澄清、暂停、恢复、取消和刷新流程均能从界面完成；部分结果与三类收尾状态可理解。
5. 真实任务总费用包含失败、读取、模型和核验，不超过可执行配置；达到限制停止新目标行动。
6. 应用接入者完成独立读者试验并留下实际记录；没有真实参与者时该项保持未验收。
7. 正常成功与故障路径均附准确版本、真实来源、目标观察、实际耗时和限制，不以一次成功宣称整体质量达标。

## Out of Scope

- 正式生产开放、通用办公应用、复杂编辑器。
- 长期记忆自动开启、委派和质量达标统计。

## Further Notes

C 的可用应用不是生产放行凭据；独立进程、真实互操作和首次生产耐久门槛仍由 D 承接。

依赖项表示实现先决条件；`ready-for-agent` 表示规格已明确，不表示依赖已完成或能力已开放。全部验收通过并附准确版本、环境、命令、结果和限制后，才可将本切片记为完成。共同执行与证据规则见[切片索引](../lerna-implementation/README.md)。

依据：[validation](../../docs/architecture/validation.md#首条完整场景)、[validation](../../docs/architecture/validation.md#文档与真实读者验收)、[deployment](../../docs/architecture/deployment.md#技术基线)、[task-lifecycle](../../docs/architecture/task-lifecycle.md#完成判定)、[ADR-0001](../../docs/adr/0001-task-session-separation.md)、[ADR-0002](../../docs/adr/0002-stable-kernel-replaceable-strategies.md)、[ADR-0010](../../docs/adr/0010-evidence-gated-improvement.md)。
