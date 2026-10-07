# 07: 逐发送费用与结算声明完整依赖

**What to build:** 预算预留、每次发送占用、账单接纳、结算、释放和后续责任均通过声明的能力处理。

**Blocked by:** None (can start immediately).

**Status:** ready-for-agent

- [ ] Budget 全部 Store、Decisions、usage、billing、send、release、version 与 settlement-followup 能力显式声明，不在调用中发现必需能力。
- [ ] 生产 SQLite 与测试 Adapter 通过消费方编译验证；必需配置与循环链接缺项明确拒绝。
- [ ] 公共准入、实际发送、账单、冲突及后续结算查询保留计费来源身份、原用量和原回执；退款、贷记及更正保留 M1 现有明确不支持的行为。
- [ ] 未发送预留只按准确不可变封闭证明释放；P5 来源仍按独立费用终结证据结算并释放未用预留。
- [ ] 取消、任务关闭或效果结论不清除费用；超上界、缺口和限额失效行为保持。
- [ ] 按 ADR 0005 更新对应设计，原结算与释放普通及故障场景通过。
