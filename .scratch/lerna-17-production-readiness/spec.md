# 17 · 首次生产耐久、隔离与运维验收

Status: ready-for-agent
Phase: D
Implementation: not-started
Depends on: [12](../lerna-12-research-report-app/spec.md)、[15](../lerna-15-component-interoperability/spec.md)、[16](../lerna-16-activation-lifecycle/spec.md)

## Problem Statement

本机或单进程成功不能证明首次生产可承受可用区故障、旧主继续写入或工作池饱和。应用需要明确开放门槛与可执行恢复手册。

## Solution

交付首版生产装配、健康与排空、受限遥测、容量配置和真实故障域验收。单地域三可用区条件下，已确认账本 RPO=0，控制与查询恢复不超过 60 秒，取得证据后才能生产开放。

## User Stories

1. As an application owner, I want a concrete production gate, so that a local demo is not released as a resilient service.
2. As a user, I want acknowledged records preserved, so that an availability-zone failure does not lose accepted work.
3. As an operator, I want old writers fenced, so that failover does not create competing authorities.
4. As a user, I want control and queries restored promptly, so that I can observe and cancel accepted tasks after failure.
5. As an operator, I want bounded draining, so that deployment leaves recoverable work rather than lost responsibilities.
6. As a tenant user, I want protected control capacity, so that another tenant's load cannot prevent my cancellation.
7. As an operator, I want privacy-preserving diagnostics, so that recovery does not require logging sensitive content.
8. As an application owner, I want dependencies included in drills, so that database recovery alone is not mistaken for service recovery.
9. As an operator, I want alerts tied to original objects, so that remediation does not duplicate uncertain actions.

## Implementation Decisions

- 装配独立网关、任务应用、分类工作池、执行宿主及管理评测进程，固定逻辑 owner 和存储归属；选择实际平台后锁定镜像、版本及配置。
- 单地域三可用区方案需同步耐久、备用容量、故障检测与旧主隔离共同成立；故障切换先封旧写端，再让新实例恢复。
- 恢复先核对未结效果、控制传播和原提交未知，再接替 Job，最后在当前权限预算满足时开放新发送。
- 发布排空停止新领取、完成短事务、保存阶段并有界退出；退出信号不证明沙箱或外部动作已停止，仍需隔离依据。
- 冻结首次开放的队列、租户配额、模型和设备并发、数据库连接、超时及背压配置；控制与核对保留容量，过载时收紧新接纳。
- 遥测关联 tenant、owner、Task、Decision、Operation、Command、组件版本，不默认记录正文、提示、截图或密钥；unknown 不能按零统计。
- 监控 Job 年龄、控制传播、未知效果时长、未结费用、残留和就绪失败，告警指向原对象、负责方和恢复动作。
- 生产依赖包括 PG、对象存储、身份、密钥、可信时间与网络；首次三可用区演练先于生产开放，整地域灾备和最终规模由 22 扩展。

## Testing Decisions

通过真实部署的 Application / Component 合同和独立目标状态验收；复用 09、12–16 的故障套件，额外注入实际可用区故障。主机重启模拟只能证明对应范围，不能替代故障域证据。

测试只断言公开行为、持久恢复结果和独立目标状态；不依赖私有表布局或内部调用次数。每个故障或拒绝案例必须配允许正常完成的对照。

验收条件：

1. 在实际三可用区配置中注入单区故障，全部故障前已确认账本记录恢复可查，RPO=0；明确确认点与测量方法。
2. 从故障注入起测量控制和查询恢复，均不超过 60 秒；记录检测、隔离、接替各阶段时间与失败样本。
3. 让旧主或旧执行实例迟到返回，不能继续受控写入或冲突输入；恢复外部发送前完成原效果和许可核对。
4. 对象存储、身份或密钥服务随故障域不可用时不假成功，保留原责任并在依赖恢复后继续。
5. 滚动发布超出排空期限后按原阶段恢复，不重复目标效果，旧组件 holder 保持可查询。
6. 高负载下控制与核对仍可执行，积压不收敛时收紧新接纳；限额有实测依据，正常负载报告可完成。
7. 遥测无默认正文和密钥，告警可定位原 Operation 并执行 reconcile；未知数值和限制如实呈现。
8. 形成版本化部署、演练记录与恢复手册；平台或真实故障条件缺失时明确未通过，禁止用本机报告代替。

## Out of Scope

- 最终千万注册、百万日活和十万在线容量声明。
- 凭备份存在或容器自动重启推断 RPO / RTO 达标。

## Further Notes

这是 A–D 首阶段的生产出口，不能推迟到 F。环境和平台资源是执行演练时的前提；本规格可实现，不代表现在已有这些资源或已获生产放行。

依赖项表示实现先决条件；`ready-for-agent` 表示规格已明确，不表示依赖已完成或能力已开放。全部验收通过并附准确版本、环境、命令、结果和限制后，才可将本切片记为完成。共同执行与证据规则见[切片索引](../lerna-implementation/README.md)。

依据：[validation](../../docs/architecture/validation.md#性能和生产验收)、[deployment](../../docs/architecture/deployment.md#可用性与灾备)、[deployment](../../docs/architecture/deployment.md#容量模型与背压)、[deployment](../../docs/architecture/deployment.md#观测与排障)、[ADR-0003](../../docs/adr/0003-stable-owner-distributed-deployment.md)、[ADR-0004](../../docs/adr/0004-transactional-durable-work.md)、[ADR-0009](../../docs/adr/0009-versioned-activation-and-recovery.md)、[ADR-0010](../../docs/adr/0010-evidence-gated-improvement.md)。
