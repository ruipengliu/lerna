# 16: CLI 手动推进使用统一宿主入口

**What to build:** recover 命令通过一个受信宿主 Module 推进原支持工作，保留用户身份、错误与阶段次序。

**Blocked by:** None (can start immediately).

**Status:** claimed

- [ ] 建立固定、有界的 ManualProgress 入口，以窄 owner 命令和查询注入；生产 CLI 委托，不编写恢复顺序。
- [ ] 保留现有手动阶段顺序、可选能力缺席、caller 身份和 context、超时、返回行为及支持范围；不套用启动期限，不增加 host-only driver 或原未支持扫描。
- [ ] 固定类型工作仍由原 owner 裁决并持久写入，入口不提供任意 handler 注册或新执行权限。
- [ ] 用生产 CLI 及公共查询验收原回执、观察、报告、解释、撤销、完成/取消、核对与后续责任推进；分别记录宿主 Open 启动阶段和 ManualProgress 的身份、结果与调用增量，不把整个进程计数归因于手动模式。
- [ ] 暂时错误保留原责任，未来 due time 和当前租约不提前接管；重放不增加原业务目标次数。
- [ ] 入口模式和权限差异先写入设计，相关 CLI 与故障行为测试通过。

## Comments

- 2026-10-07: Claimed for implementation on `codex/interfaces-16`, based on the integration branch.
