# 06: 动作执行与核对声明完整依赖

**What to build:** 原动作接纳、开始、发送、证据、核对与封闭通过完整依赖运行，保存原责任且不因替换 Adapter 意外 panic。

**Blocked by:** None (can start immediately).

**Status:** claimed

- [ ] Execution Manager 全部 Store、Work、observed-work 与核对 Job 能力显式声明，包含历史、封闭、回报及指标。
- [ ] Egress Gate 的必需依赖与明确可选能力分别表达；FILE 检查出口仍按原资格验证，不建立绕过门禁的物理路径。
- [ ] 生产执行域 Work、SQLite 与短租约测试 Adapter 通过消费方编译验证；缺项在构造或连接完成时拒绝。
- [ ] 实现公开完整 Store 的最小有效 Adapter 能完成相应接纳调用；原 ledger.Store 编译后 panic 的复现路径被消除。
- [ ] 通过公共执行、重发、核对、观察、封闭与查询验证原回执、发送身份和独立目标计数。
- [ ] P4/P5、未知效果、迟到可能性、历史版本检查、来源事务和原收尾决定不变；设计及相关普通与故障验证通过。
- [ ] 本票只迁移依赖声明、注入、编译检查和配置验证；规则提取、共同封闭算法、目标创建内聚及恢复编排由各对应票据交付。

## Comments

2026-10-07: Claimed for implementation on the integration branch.
