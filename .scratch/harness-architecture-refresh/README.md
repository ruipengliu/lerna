# Harness 架构方案优化任务

用户于 2026-10-01 确认[设计范围与依赖](spec.md)。票据的 Status 保留分派标签，Progress 记录 pending、in-progress 或 completed；完成后逐项勾选验收条件并追加结果记录。这里的完成仅指设计文档交付。

11 项设计任务已完成。交付与检查见[验证记录](verification.md)；新增数据对象与读写频次对比已纳入研究目录，未运行上游或本项目的性能测试。

| 编号 | 任务 | Progress |
| --- | --- | --- |
| 01 | [重启后先恢复原责任，再开放新执行](issues/01-restart-readiness.md) | completed |
| 02 | [长任务上下文与最终模型请求的来源核验](issues/02-context-provenance.md) | completed |
| 03 | [最终工具参数准入与完整效果交付](issues/03-tool-admission-results.md) | completed |
| 04 | [持久无进展判断与有界续行](issues/04-bounded-progress.md) | completed |
| 05 | [多端输入一次消费与界面恢复](issues/05-input-surface-recovery.md) | completed |
| 06 | [子 Agent 异步等待、冷恢复与结算](issues/06-child-recovery-settlement.md) | completed |
| 07 | [扩展暂存装配、配置竞争与实例就绪](issues/07-extension-staging.md) | completed |
| 08 | [经验候选形成与受控评测发布](issues/08-experience-candidates.md) | completed |
| 09 | [受限程序化工具的执行与恢复合同](issues/09-programmatic-tools.md) | completed |
| 10 | [渐进式能力发现与 Skill 加载对照](issues/10-progressive-discovery.md) | completed |
| 11 | [贯穿场景验收与架构入口一致性](issues/11-integration-verification.md) | completed |
