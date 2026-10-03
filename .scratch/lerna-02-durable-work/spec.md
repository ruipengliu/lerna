# 02 · PG / SQLite 持久工作与接替

Status: ready-for-agent
Phase: A
Implementation: in-progress
Depends on: [01](../lerna-01-command-contracts/spec.md)

## Problem Statement

运行进程可能在接纳后退出，或在旧 worker 完成时收到新工作；开发者需要确保已确认责任不会丢失，也不会被迟到提交覆盖。

## Solution

通过内部 Host 合同提供真实 PostgreSQL 与 SQLite 的事务接纳、持久 Job、租约领取和接替。演示程序接纳一个无外部副作用的工作，重启后仍能查询原回执并完成新增修订。

## User Stories

1. As a caller, I want durable original receipts, so that a lost response does not lose my request.
2. As a caller, I want conflicting reuse rejected, so that one identity cannot execute different inputs.
3. As a worker author, I want atomic business and work commits, so that acknowledged work survives a crash.
4. As an operator, I want expired claims recovered, so that worker replacement needs no new business identity.
5. As a worker author, I want stale epochs rejected, so that late workers cannot overwrite current progress.
6. As a caller, I want concurrent new work retained, so that an older completion cannot erase it.
7. As a device integrator, I want durable SQLite behavior, so that local execution records survive restart.
8. As an operator, I want bounded fair scheduling, so that ordinary work cannot starve control and reconciliation.

## Implementation Decisions

- 实现显式 Tx、accept、work、JobStore、Claim、Clock；同库业务变更、原命令回执与必要 Job 共提交，不隐式跨 owner 共用事务。
- 按 tenant、owner、command_id 唯一去重并比较原摘要；新过期命令固定拒绝，已有命令仍返回原决定。提交未知不生成新身份。
- 持久保存 work_revision、completed_revision、claimed_revision、lease_epoch、due_at 与阶段；完成只推进已领取修订，提交检查当前有效 epoch。
- PostgreSQL 采用短事务和有界扫描，显式验证锁序与隔离；SQLite 采用单写队列、WAL 和耐久配置。通知仅加速，扫描负责恢复。
- 网络、模型、对象上传和用户等待都在持锁事务外；等待保存可检查条件并释放 worker。
- 按租户和工作类别配置并发上限、退避、期限；控制与核对保留容量。永久错误不无限重试。
- 保留足够的命令最小身份与墓碑，正文保留期不等于去重保留期；不得因租约过期推断外部动作未发生。

## Testing Decisions

主边界为当前最高可用的内部 Host Tx / JobStore 合同，并通过公开 command.get 观察回执。沿用 01 的夹具，使用真实 PostgreSQL、SQLite 和进程终止；不以内存数据库代替耐久证据。

测试只断言公开行为、持久恢复结果和独立目标状态；不依赖私有表布局或内部调用次数。每个故障或拒绝案例必须配允许正常完成的对照。

验收条件：

1. 提交后丢回执，重启并重传原命令，返回同一决定且业务工作仅有一份；相同键异内容固定冲突。
2. 分别在事务提交前后杀进程：未提交不得出现成功回执；已确认记录和后续 Job 可恢复。
3. 领取修订后加入新工作，再完成旧修订，新工作仍能被领取处理。
4. 租约接替后旧 worker 提交被拒绝；新 worker 可按原 Job 推进，不改变业务对象身份。
5. 丢掉全部唤醒通知后扫描仍恢复工作；延迟任务不到期不忙轮询，等待任务不占长事务。
6. 两种真实数据库通过相同适配器套件；记录 SQLite 耐久设置、PG 隔离级别及并发结果。
7. 普通工作饱和时控制与核对仍可领取；租户配额与有界队列生效，健康工作也能完成。

## Out of Scope

- 外部动作自动重试或恰好一次保证。
- 独立消息队列、全量事件溯源、任意程序栈恢复。

## Further Notes

进程崩溃测试不等于断电或可用区耐久证明。首次生产的同步提交、故障域和旧主隔离在 17 验收。

依赖项表示实现先决条件；`ready-for-agent` 表示规格已明确，不表示依赖已完成或能力已开放。全部验收通过并附准确版本、环境、命令、结果和限制后，才可将本切片记为完成。共同执行与证据规则见[切片索引](../lerna-implementation/README.md)。

依据：[runtime](../../docs/architecture/runtime.md#两个公共模板)、[runtime](../../docs/architecture/runtime.md#命令接纳算法)、[runtime](../../docs/architecture/runtime.md#领取与完成竞争)、[runtime](../../docs/architecture/runtime.md#调度与资源隔离)、[data-model](../../docs/architecture/data-model.md#事务与数据库装配)、[ADR-0003](../../docs/adr/0003-stable-owner-distributed-deployment.md)、[ADR-0004](../../docs/adr/0004-transactional-durable-work.md)。

## 实施启动记录

2026-10-03，前置01已完整退出，准确受测代码 `23bac17` 与远端CI success，退出记录在前置spec。按用户持续授权和决策代理批准的粒度发布八张票据，详见 [ticket-review](ticket-review.md)、[decisions](decisions.md)、[admission-decisions](admission-decisions.md)。首票PG原子接纳开始；本片所有验收仍待真实产品实现和数据库证据，不把环境smoke作为退出证据。
