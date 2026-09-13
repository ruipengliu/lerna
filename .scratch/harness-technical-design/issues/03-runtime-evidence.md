# 核对持久运行机制与存储的部署约束

Type: research
Interaction: AFK
Labels: wayfinder:research
Status: resolved
Assignee: runtime_research
Blocked by:
Parent: [Harness 技术实现方案决策地图](../map.md)

## Question

LangGraph、Temporal 与自行维护任务日志和检查点的方案，在状态所有权、重试、取消、外部动作结果不明、离线节点和最小部署依赖上有什么可核实差异？

## Scope

以官方文档核对机制和边界，适度比较 SQLite 与 PostgreSQL 对本地和服务端持久化的支持；框架检查点、消息交付和实际外部动作是否成功必须分别评价。

## Expected evidence

给出部署依赖、SDK/语言支持、恢复语义和可验证限制的比较，列出必须原型验证的问题，不替用户锁定选型。

## Output

计划关联文档：`docs/research/harness-durable-runtime-options.md`。

## Comments

研究由 runtime_research 在独立 worktree 完成。研究分支：`research/harness-durable-runtime`；提交：`5c66f8205c2aec87e148c0f76f4a2709eb316268`。

## Answer

已核实图检查点、Workflow 恢复与数据库事务分别提供的机制及部署限制。三种路线都不能仅凭恢复机制保证外部动作只执行一次；需要明确任务状态所有者及执行结果核对。

详细事实和必要故障验证见 [Harness 持久运行机制与存储选项](../../../docs/research/harness-durable-runtime-options.md)。用户在建图阶段进一步指定 Go 与开发期嵌入式依赖，因此研究用于评估机制和替换位置，不再用于选择核心语言，也不支持将常驻外部引擎强制引入开发基线。
