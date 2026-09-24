# 设计缺口与冲突记录

本页区分整合中需要裁定的语义冲突和原设计尚未完成的实现事项。目录迁移、补齐已有条款与改写表达不重新批准技术方案。

## 整合中的实质冲突

本轮对内核、记忆、决策、执行、交互、授权、扩展、协作、端云、生产恢复及验收主线的编辑核对，未发现需要用户裁定的直接冲突。已发现的差异是旧版独有细目或仍指向旧稿的维护入口，去向见[内容归属](content-map.md)。这不等于对全部语句、状态组合和接口作了形式化等价证明。

后续发现冲突时，记录两处条款、具体触发条件、不同结果、影响范围和待裁定问题。裁定前不选择一边覆盖另一边，也不把未解决行为写成实现保证。

## 保留的设计与实现待定项

| 事项 | 已有结论 | 尚需完成 | 当前入口 |
| --- | --- | --- | --- |
| 组件详细设计 | 任务运行内核已完成内部规则、Go 接口、SQLite 布局与验收规格；其余章节仍为规划 | 其余组件详细设计，内核实际实现、耐久性与替换验收 | [详细设计目录](../design/README.md)、[内核验证](../design/01-runtime-kernel-validation.md) |
| Schema 与编码 | 标识、原子组与兼容原则已定义；内核关键类型、局部枚举、规范化格式及 SQLite DDL 已固定 | 跨模块完整 Schema、Protobuf 字段编号、其余模块 DDL、bbolt 物理实现和跨语言夹具 | [内核接口](../design/01-runtime-kernel-contracts.md)、[接口实施边界](../reference/02-api-and-message-catalog.md#implementation-boundary) |
| 智能参考策略 | 已有候选、上下文、无进展等建议 | 冻结版本、双语检索配置并实测召回、质量与成本；8／16／3 不升格为默认值 | [决策参考](../reference/decision-details.md) |
| 生产 HA 与容量 | 规模目标及参考拓扑、演算口径已保留 | 版本和故障配置锁定、实际吞吐、复制耐久、隔离、RTO 与成本验证 | [部署细则](../reference/deployment-details.md)、[恢复细则](../reference/recovery-details.md)、[容量模型](../reference/04-capacity-model.md) |
| 业务与优势验收 | 样本、预算、质量门槛及比较方法已有设计 | 实际目录、夹具、调用命令、独立判读、统计适用性及运行报告 | [验收配置](../reference/05-validation-profiles-and-traceability.md)、[架构比较](../main/04-engineering/06-architecture-comparison.md) |

没有运行证据的事项继续保持待验证。来源报告的固定版本不在本次文档整合中更新；改变选型或版本时再核实相应依据。
