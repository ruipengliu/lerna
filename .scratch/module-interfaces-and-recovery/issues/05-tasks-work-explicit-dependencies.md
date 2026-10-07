# 05: 任务裁决与持久工作声明完整依赖

**What to build:** 任务创建、规划、准入、模型、完成、取消与关闭的支持路径使用明确 Store 和命令能力，原决定与来源仍原子保存。

**Blocked by:** None (can start immediately).

**Status:** claimed

- [ ] 审计 Tasks 与其使用的 Durable、Decisions、Work 全部支持路径，显式声明存储、历史、恢复、指标和 observed-decisions 等必需能力。
- [ ] SQLite、裁决域 Work 和现有窄测试 Adapter 通过消费方 Interface 编译验证；构造与循环连接缺项可明确报告。
- [ ] 通过既有公开任务、准入、模型、driver、完成、取消和关闭场景验证正常路径与原命令重放。
- [ ] 确定拒绝、原决定、Job 与必需源记录保留原事务和保存点语义；暂时失败不变为永久拒绝。
- [ ] CLAIM 围栏、固定 Job 类型及 owner 完成门禁保持；普通工作控制不绕过尚无回执的责任。
- [ ] 不修改共同消息、持久格式或业务状态语义；更新设计并通过相关普通与故障场景。
- [ ] 本票只迁移依赖声明、注入、编译检查和配置验证；规则提取、共同封闭算法、目标创建内聚及恢复编排由各对应票据交付。

## Comments

2026-10-07: Claimed for implementation on codex/interfaces-05.
