# 10 · 有界模型决策与真实搜索

Status: ready-for-agent
Phase: C
Implementation: not-started
Depends on: [09](../lerna-09-task-control/spec.md)

## Problem Statement

真实报告需要模型与真实来源，但供应商隐式重试、未知用量和不完整工具保证可能绕过已有预算、身份和效果规则。

## Solution

接入至少一个真实模型供应商及真实搜索、正文获取能力，提供有界 ReAct 决策和独立能力探针。准确来源、模型费用、请求未知与能力限制都可通过原记录观察。

## User Stories

1. As a user, I want real retrieved sources, so that a report does not cite invented evidence.
2. As a user, I want one observable model request per Decision, so that hidden retries cannot consume my budget.
3. As an operator, I want provider capability probes, so that idempotency and billing claims have evidence.
4. As a user, I want unknown model outcomes retained, so that a timeout cannot trigger invisible duplicate requests.
5. As a data owner, I want source permissions preserved, so that fetched material cannot expand its own use.
6. As a user, I want malformed proposals bounded, so that repeated repair attempts cannot run forever.
7. As a component author, I want protocol limitations surfaced, so that MCP responses do not imply stronger guarantees.
8. As a user, I want provisional streaming marked, so that a fragment is not mistaken for the final report.
9. As a security reviewer, I want hostile source instructions contained, so that retrieved text cannot authorize actions.

## Implementation Decisions

- 实现默认模型 Decision Engine 与供应商适配器；固定 Snapshot、组件、策略、limits 和 deadline，每 Decision 至多一次物理模型请求，关闭 SDK 隐式重试。
- 完整 Proposal、产物和 processed_source_refs 持久发布后才标记 Decision completed；流片段只作 provisional 展示。
- 未知供应商答复查询原请求；不可查询则保留限制。新 Decision 必须重新准入、计入原费用风险并使旧 Decision 不再可消费。
- 搜索与正文获取均作为受准入 Executor 能力，发布准确 Content；抓取时间、实际来源、引用与原字节关联，公开引用集合不替代处理来源。
- 交付至少一个真实 MCP 服务适配的合同验证；协议版本在实现时固定，映射原请求和远端句柄。能力不足、取消不终止或结果业务错误按事实限制开放。
- 供应商探针记录原查询、幂等窗口、隐式重试、费用硬上界、数据用途及隔离前提。无可执行计费上界的能力不得进入硬预算自动路径。
- 模型只能引用本次提供的能力和准确参数，格式错误消耗原 Decision，修复需新的有限预算；网页和工具正文不能创建授权或修改目标。
- 厂商选择使用可用且获准的账户，通过探针后写入版本配置；不在规格中猜测价格或 API 保证。

## Testing Decisions

通过 Application 报告子场景及公开 Decision Engine / Executor 合同测试。复用 03 的确定性对照与 06–09 的门禁；真实供应商探针与模拟故障报告分开。

测试只断言公开行为、持久恢复结果和独立目标状态；不依赖私有表布局或内部调用次数。每个故障或拒绝案例必须配允许正常完成的对照。

验收条件：

1. 真实搜索获取的正文可按 ContentRef 回读，报告事实引用指向实际取回版本；缺来源不会伪造引用通过。
2. 在模型出口独立观察一个 Decision 最多一次物理请求，断连或 SDK 错误不触发隐式重发。
3. 模型结果未知时保留费用风险与等待；批准新 Decision 后旧提案不能再准入。
4. 非法提案、未知工具名、任意 URL / 密钥引用被拒绝，合法有界提案仍可推进。
5. 恶意网页要求扩权或泄露另一租户内容时无越权实际效果，合法材料处理不被全量禁用。
6. 真实 MCP 服务的断连、过期句柄、业务错误和取消不终止均按公开限制表示；不伪造 not_applied 或零费用。
7. 真实计费上界与最终用量可核对，核验成本计入同根预算；无可信上界的能力明确拒绝自动收费。
8. 流片段丢失不影响持久 Proposal 查询；模型返回前重启后可恢复原 Decision 责任。

## Out of Scope

- 供应商无依据的性能保证、复杂递归推理或多 Agent。
- 1000 个 API 覆盖和 90% / 95% 质量达标声明。

## Further Notes

真实账户、网络许可或可证明费用上界缺失时，记录具体开放阻塞项，模拟探针不能替代本切片真实接入出口；无需为了生成规格先选择供应商。

依赖项表示实现先决条件；`ready-for-agent` 表示规格已明确，不表示依赖已完成或能力已开放。全部验收通过并附准确版本、环境、命令、结果和限制后，才可将本切片记为完成。共同执行与证据规则见[切片索引](../lerna-implementation/README.md)。

依据：[capabilities](../../docs/architecture/capabilities.md#决策引擎只形成下一步提案)、[capabilities](../../docs/architecture/capabilities.md#executor-统一管理实际行动)、[contracts](../../docs/architecture/contracts.md#外部协议适配)、[governance](../../docs/architecture/governance.md#信任来源与执行入口)、[ADR-0002](../../docs/adr/0002-stable-kernel-replaceable-strategies.md)、[ADR-0005](../../docs/adr/0005-explicit-effect-uncertainty.md)、[ADR-0006](../../docs/adr/0006-authorization-budget-at-action-boundaries.md)、[ADR-0008](../../docs/adr/0008-internal-contracts-external-protocol-adapters.md)。
