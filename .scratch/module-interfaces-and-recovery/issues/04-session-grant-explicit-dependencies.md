# 04: 输入、确认与授权链声明完整依赖

**What to build:** 会话输入、问题、确认及授权处理通过完整声明的依赖运行，合法替换 Adapter 不在正常调用时 panic。

**Blocked by:** None (can start immediately).

**Status:** claimed

- [ ] Sessions 和 Grants 全部支持路径的 Store、Decisions、投递、问题、确认及事实依赖显式声明，含匿名或 checked 必需断言。
- [ ] 生产 Adapter 通过消费方编译验证；关键缺项及 typed nil 在构造或该 Module 完成连接时拒绝。
- [ ] 通过公共输入、问题、确认、授权签发和撤销流程验证原命令去重、会话顺序、单次确认消费及原授权使用记录。
- [ ] 确认与授权消费仍加入原裁决事务；撤销保留源记录、端点停止确认与当前授权门禁。
- [ ] 错误与可选能力缺席行为保持，未完整配置不得默认为成功；不扩大宿主或用户权限。
- [ ] 相关设计先更新，普通与故障场景保留原事实及独立目标计数。

## Comments

2026-10-07: Claimed for implementation on codex/interfaces-04.
