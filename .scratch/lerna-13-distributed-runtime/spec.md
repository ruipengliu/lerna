# 13 · 分布式传输、设备重连与租户隔离

Status: ready-for-agent
Phase: D
Implementation: not-started
Depends on: [09](../lerna-09-task-control/spec.md)、[11](../lerna-11-application-sdk/spec.md)

## Problem Statement

跨进程、跨设备后会出现断连、乱序、重复和不同数据库；调用方需要保留原 owner 与责任，而不能因为换网关就重建 Task 或认为设备已停止。

## Solution

提供独立网关、应用、工作池与执行宿主装配，打通真实 WSS / gRPC、固定 owner 发现、设备账本补传和用户隔离。同一报告控制与恢复合同可以在分布式环境执行。

## User Stories

1. As an application developer, I want the same semantics remotely, so that deployment does not change task meaning.
2. As a user, I want reconnection to preserve the original owner, so that one goal cannot acquire two authorities.
3. As a device user, I want local execution records retained, so that offline effects can be reconciled later.
4. As a user, I want control synchronized before new work, so that reconnecting does not start revoked actions.
5. As a tenant administrator, I want authenticated discovery and routing, so that another tenant cannot reach my objects.
6. As an operator, I want bounded flow control, so that slow connections cannot consume unbounded memory.
7. As a caller, I want remote grant transfers recoverable, so that network failure cannot duplicate finite authority.
8. As an operator, I want independent worker pools, so that model load cannot prevent cancellation or reconciliation.
9. As a user, I want visible offline limits, so that cancellation is not represented as instantaneous remote stopping.

## Implementation Decisions

- 端云采用真实 WSS，服务间 gRPC 使用 Protobuf 外壳和严格 JSON 正文，同进程沿用直接合同。发现、认证和大字节传输使用 HTTPS。
- 租户受信目录返回 owner 与方法版本能力，新 Task 首次发送前固定映射；网关只切换健康进程，原命令不得改投其他 owner 数据库。
- 跨 owner 先持久保存发送意图和投递 Job，接收方独立接纳，再保存原回执；按 source_owner、object_id、revision 去重归并，乱序不回退。
- 设备使用 SQLite 保存有限许可、TaskGate、Attempt、Effect 和待补传记录；Task 缓存不成为任务权威。失联不接管云端 Task。
- 重连先恢复身份和版本、控制和撤权，再补传原效果及用量，最后开放新行动。时钟或撤销依据不可信时封闭依赖它的启动。
- 远端授权签发先扣有限可用范围，绑定准确行动、期限和用量；原使用身份的释放可恢复。离线最大撤销延迟公开，要求在线核验的能力失联等待。
- WSS 实现有界帧、流控、背压、重连和失效 cursor 快照重建；大内容走 ContentRef，不靠无限缓存保留通知。
- 应用、网关、分类工作池和执行宿主独立进程，开发保留单进程配置；认证、密钥、配置、时钟与健康由平台适配提供，不在业务层硬编码凭据。

## Testing Decisions

从 Application SDK 通过真实 WSS 驱动控制与查询，Component 经真实 gRPC 运行；复用 01 合同、02 存储和 09 中断套件。在不同进程与数据库上注入网络故障，不以函数调用模拟传输通过。

测试只断言公开行为、持久恢复结果和独立目标状态；不依赖私有表布局或内部调用次数。每个故障或拒绝案例必须配允许正常完成的对照。

验收条件：

1. WSS / gRPC 断连、乱序、重复、丢回执后只恢复原命令与对象，原 owner 不变，无重复 Task 或 Operation。
2. 设备离线时已签有界行动遵守原窗口，需新裁决的行动等待；窗口过期、时钟不可信或撤权同步未完成时不新启动。
3. 重连先收紧控制，再补传原效果和累计用量；设备旧时间戳不能覆盖新对象修订。
4. 远端 Grant 签发后本地准入失败并断连，原有限额度不能同时被两侧使用，释放按原身份恢复。
5. 跨租户认证、owner 路由、发现、订阅与内容读取均无越权数据，合法租户正常完成。
6. 慢消费者和突发流量受有界背压，断连后可查询重建；控制与核对不被模型任务完全阻塞。
7. 网关与 worker 分别重启，原 Task、未结效果和费用仍可查；仅替换健康服务进程，不切换逻辑权威。

## Out of Scope

- 生产可用区耐久证明、自动在线跨分片 Task 迁移。
- 设备离线接管云端同一个 Task；独立设备目标由 20 实现。

## Further Notes

D 可以在 C 的真实应用接入并行推进，但 D 的报告整体验收必须汇合 12。第一版分布式要求不等于强制每个逻辑模块各建独立服务。

依赖项表示实现先决条件；`ready-for-agent` 表示规格已明确，不表示依赖已完成或能力已开放。全部验收通过并附准确版本、环境、命令、结果和限制后，才可将本切片记为完成。共同执行与证据规则见[切片索引](../lerna-implementation/README.md)。

依据：[contracts](../../docs/architecture/contracts.md#协议发现版本与传输)、[runtime](../../docs/architecture/runtime.md#跨-owner-交接)、[deployment](../../docs/architecture/deployment.md#默认端云行为)、[deployment](../../docs/architecture/deployment.md#分片与发现)、[governance](../../docs/architecture/governance.md#撤销传播与离线执行)、[ADR-0003](../../docs/adr/0003-stable-owner-distributed-deployment.md)、[ADR-0004](../../docs/adr/0004-transactional-durable-work.md)、[ADR-0006](../../docs/adr/0006-authorization-budget-at-action-boundaries.md)、[ADR-0008](../../docs/adr/0008-internal-contracts-external-protocol-adapters.md)。
