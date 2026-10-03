# 22 · 容量、异地灾备与专项验收

Status: ready-for-agent
Phase: F
Implementation: not-started
Depends on: [17](../lerna-17-production-readiness/spec.md)、[20](../lerna-20-device-autonomy/spec.md)、[21](../lerna-21-evaluation-release/spec.md)

## Problem Statement

生产团队需要知道实际开放能力的质量、成本和容量边界，以及整地域故障怎样恢复；注册量、连接数和单个成功案例不能推导系统可承载负载。

## Solution

交付可重复负载、异地灾备与专项质量验收流程和证据包。报告实测可开放范围、失效点、资源配置、未达目标与阻塞项，对最终规模和质量目标分别给出结论。

## User Stories

1. As an operator, I want measured workloads separated, so that connections are not confused with concurrent inference.
2. As an application owner, I want capacity and costs tied to task profiles, so that deployment decisions use reproducible evidence.
3. As a user, I want recovery and control capacity reserved, so that overload does not strand accepted work.
4. As an operator, I want cross-region recovery rehearsed, so that a backup is not mistaken for a working disaster plan.
5. As a user, I want restored ledgers reconciled with external effects, so that restoring old data does not repeat actions.
6. As an evaluator, I want API coverage and success measured separately, so that a large catalog cannot hide incorrect calls.
7. As an application owner, I want unsupported profiles explicit, so that partially implemented capabilities cannot be enabled accidentally.
8. As a reviewer, I want real-platform and simulator evidence distinguished, so that device or protocol support is not overstated.
9. As a maintainer, I want versioned release evidence, so that later changes can be compared against a known baseline.

## Implementation Decisions

- 负载先测直接回答、读取后回答、冷恢复、保存并读回，再扩展获准记忆、有界委派和大量设备连接；连接、活跃 Task、并发模型和工具吞吐分别建模。
- 冻结各阶段任务分布、到达率、突发、期限、根预算、配额、并发和退避；记录事务、同步、WAL、内容字节、模型与队列等待、核对成本及尾延迟。
- 逐级加压找失效点及故障后备用容量；处理率不足时降低新接纳，保留已接纳 Task 的控制和核对能力，不能无限增加内存队列。
- 整地域灾备由平台方案先冻结异地 RPO / RTO 再演练，不能默认异地同步；恢复 owner、去重记录、内容和密钥依赖，隔离旧写端并核对恢复点之后的真实外部效果。
- 最终规模目标为千万注册、百万日活、十万同时在线，API 覆盖目标 1000 个以上；负载与合同目录分别报告，不从用户数直接推算服务器数量或模型吞吐。
- 首次调用正确率至少 90%、有限重试 Task 成功率至少 95% 是最终质量目标；沿 21 预先冻结调用单位、任务范围、真值、次数、费用、期限与统计方法，unknown 不移出分母。
- 联网问答单测正确性、引用支撑、时效、冲突和获取失败。GUI 只有专项合同与驱动存在才进入启用范围，多个有状态模拟设备通过不代表真实手机平台支持。
- Schedule、可复用 Environment、通用 Surface 与 GUI 在本轮无完整实现切片，默认 unsupported；声明支持前另有独立实现规格、机器合同、权限和恢复套件。环境 cell 的 hostcall 崩溃等义务列入总索引，不因未开放而伪报通过。
- 输出运行版本、公开能力目录、证据索引、实测限制和开放建议；模拟、协议符合性、质量、单区耐久和异地灾备分别取证。

## Testing Decisions

从部署后的 Application / Component 公开接口施加负载，用独立目标观察与 21 的统计管道验收。复用 17 故障域设施和 20 设备场景；新的异地故障必须在实际平台验证。

测试只断言公开行为、持久恢复结果和独立目标状态；不依赖私有表布局或内部调用次数。每个故障或拒绝案例必须配允许正常完成的对照。

验收条件：

1. 四类基础任务和当前开放扩展都有可重复负载报告，列出吞吐、尾延迟、队列等待、完整费用和资源配置，能复现主要失效点。
2. 突发与故障后容量下降触发有界背压，新接纳可拒绝，既有 Task 的控制和核对仍有保留容量；同时复核 17 的单区恢复门槛。
3. 整地域切换按预先冻结的异地 RPO / RTO 测量并给出达标或失败证据，旧地域写端受控隔离。
4. 从旧备份恢复后，已在外部发生但本地缺失的动作不盲目重发，沿原身份核对或保持 unknown；去重最小依据和准确内容可核查。
5. API 目录逐项记录声明范围、协议版本、权限与效果探针，覆盖量与成功率分别计算；未达到最终目标明确列缺口，不能用模拟数量补足。
6. 联网问答质量采用独立证据和冻结样本，全部失败与 unknown 计入；有人工代做的样本单列。
7. 未实现的 Schedule / Environment / Surface / GUI 调用明确拒绝；如未来启用，必须先补独立合同、专项反例与真实平台证据。
8. 最终报告区分当前实测可开放范围和最终规模 / 质量达标范围；未取得真实平台或充分样本时相应目标保持未通过，不默认为完成。

## Out of Scope

- 为达目标重写架构、自动在线跨分片迁移或无限资源扩容。
- 在没有专项实现与合同的情况下启用可选 profile。

## Further Notes

22 的实现交付包含验收工具、记录和明确结论；验收工具实现完成不等于最终目标已达成。首次生产 17 的门槛持续有效；异地和最终容量目标只有取得对应证据才关闭。

依赖项表示实现先决条件；`ready-for-agent` 表示规格已明确，不表示依赖已完成或能力已开放。全部验收通过并附准确版本、环境、命令、结果和限制后，才可将本切片记为完成。共同执行与证据规则见[切片索引](../lerna-implementation/README.md)。

依据：[validation](../../docs/architecture/validation.md#性能和生产验收)、[validation](../../docs/architecture/validation.md#质量目标与人工参与)、[deployment](../../docs/architecture/deployment.md#容量模型与背压)、[deployment](../../docs/architecture/deployment.md#可用性与灾备)、[contracts](../../docs/architecture/contracts.md#授权和宿主管理方法)、[ADR-0010](../../docs/adr/0010-evidence-gated-improvement.md)、[ADR-0003](../../docs/adr/0003-stable-owner-distributed-deployment.md)、[ADR-0005](../../docs/adr/0005-explicit-effect-uncertainty.md)、[ADR-0008](../../docs/adr/0008-internal-contracts-external-protocol-adapters.md)。
