# Harness 评审后续优化项（2026-09-28）

本记录收录 28 日评审意见。后续设计访谈已确认本项公共框架形状，详细设计和九模块接入已纳入正式文档；运行实现与故障／性能验证仍待完成。

## 1. 统一可靠接纳与持久工作推进框架

**已有依据：**[默认宿主的装配与持久接口](../../docs/architecture/deployment.md#4-默认宿主的装配与持久接口)已定义 transaction、command_store、job_store 及宿主工作领取边界。[Brain 实现](../../docs/architecture/brain/implementation.md)等模块各自保存接纳、处理和恢复责任。原评审建议在既有接口上提炼可复用骨架，当时仅登记候选；本轮已完成选择和文档落实，不声称已经实现。

**已确认公共骨架：**采用可装配的接纳与有界工作模板，统一逻辑 JobStore，保留各提交域的存储布局。沿原命令去重，将领域准备或业务决定、合法回执阶段及下一责任共同保存；有界领取与回写分别核验 lease_epoch 和 work_revision；外部步骤在事务外执行。领域处理器可组织多个短事务，继续裁决业务成功、外部效果与安全重试。选择依据见[评审提案](reliable-work-framework-proposal-2026-09-28.md)及 [ADR](../../docs/adr/0009-reliable-work-framework.md)。

**文档落实：**[框架详细设计](../../docs/architecture/reliable-work.md)集中定义接口、事务句柄、处理器、领取及责任版本、恢复、故障套件和成本口径；[九模块接入](../../docs/architecture/reliable-work.md#integration)分别定义责任键、共同提交内容和领域完成条件。[FW-01～09 与领域验收矩阵](../../docs/architecture/validation/fault-experiments.md#reliable-work-framework)覆盖失答复、接替、新责任竞争、外部未知及恢复放大，仍待真实运行。

**边界：**领域接纳规则、成功判断、外部效果核对和安全重试条件继续归各模块。公共框架不能自动重试未知副作用，也不能绕过领域事务；共享实现不强制同一数据库或工作池，不要求只读或可同步结束的调用全部异步化，不新增通用业务状态机或独立调度服务。原评审登记时的“本轮不修改正式设计”已由本次用户明确要求及两项设计确认推进为正式文档整合，第三方公开方法和回执阶段保持。
