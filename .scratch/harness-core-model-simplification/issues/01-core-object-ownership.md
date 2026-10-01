# 01：核心对象与数据归属

Status: ready-for-agent
Progress: pending
Blocked by: None

## Scope

落实[规格](../spec.md)的六组核心对象与概念分类。创建架构主干 `core-data-model.md`，作为对象归属和内部记录的导航；术语准确更新到根 CONTEXT。权威业务规则继续归原模块。

Owned files: `CONTEXT.md`、`docs/architecture/.draft/core-data-model.md`。

## Acceptance

- [ ] 分类覆盖核心对象、内部子记录、派生投影、配置绑定及可选扩展；保留每项负责方、身份／关联、生命周期、事务／持久性、查询者、保留与独立存在理由。
- [ ] 六组对象及 Task／Decision／Operation 主要记录都可从目录追踪；独立 Confirmation、Surface、内容用途、授权使用与结算责任不会被错误并入父对象生命周期。
- [ ] 明确默认逻辑聚合和可合并存储范围，不承诺六张表或跨负责方原子性，不删除既有公共端口。
- [ ] 场景说明提交、任务、回合、模型调用、工具行动与工作进程的不同身份；从根词汇进入正文无术语漂移。
- [ ] 复用已有文档检查，记录检查范围；未运行的数据库及行为验证明确标注。

## Comments

- 2026-10-01：实施入口。先读规格、根 CONTEXT 和有关 ADR；按 TDD 指引评估验证切面，本票为设计文档，使用静态检查和行为规格审查，不制造运行测试或伪造 red/green。
