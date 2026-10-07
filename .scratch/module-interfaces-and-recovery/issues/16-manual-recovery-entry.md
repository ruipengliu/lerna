# 16: CLI 手动推进使用统一宿主入口

**What to build:** recover 命令通过一个受信宿主 Module 推进原支持工作，保留用户身份、错误与阶段次序。

**Blocked by:** None (can start immediately).

**Status:** resolved

- [x] 建立固定、有界的 ManualProgress 入口，以窄 owner 命令和查询注入；生产 CLI 委托，不编写恢复顺序。
- [x] 保留现有手动阶段顺序、可选能力缺席、caller 身份和 context、超时、返回行为及支持范围；不套用启动期限，不增加 host-only driver 或原未支持扫描。
- [x] 固定类型工作仍由原 owner 裁决并持久写入，入口不提供任意 handler 注册或新执行权限。
- [x] 用生产 CLI 及公共查询验收原回执、观察、报告、解释、撤销、完成/取消、核对与后续责任推进；分别记录宿主 Open 启动阶段和 ManualProgress 的身份、结果与调用增量，不把整个进程计数归因于手动模式。
- [x] 暂时错误保留原责任，未来 due time 和当前租约不提前接管；重放不增加原业务目标次数。
- [x] 入口模式和权限差异先写入设计，相关 CLI 与故障行为测试通过。

## Comments

- 2026-10-07: Claimed for implementation on `codex/interfaces-16`, based on the integration branch.


## Answer

2026-10-07: 已建立固定、有界的 `infra/hosting.Service.ManualProgress`，生产 CLI 的 recover 只转交原 context/caller。原 Sessions 必需，其余能力通过窄 typed owner 端口显式可选；保留原分组、阶段顺序、首错返回、原权限及 owner 事务。入口不增加扫描、host-only driver、启动期限或任意 handler 注册。先更新 hosting、interaction 设计与架构索引；装配在固定 connect 后、start 前 checked 构造宿主，并把 Recovery 纳入票据 08 的完成门禁。

候选 `bcdc31059de0233cf0cf3db94228a022052dce2f` 已同步最新集成 `1eb614efcdf30890e82a3104d91643cca7330345`，包含票据 08 门禁及 15 共享交付。准确测试树为 `28daaf5ff337396b96fb78c164569e8924c006f9`；集成合并后、写入本验收记录前的树与该树完全相同。以下检查全部通过并按同源复用：

- `make check-code CHECK_PACKAGES='./infra/hosting ./adapters/interaction ./cmd/assembly ./cmd/lerna'`：imports/fmt、普通及 fault lint（0 issues）、规则检查和 scoped race 通过；`make check-docs` 通过。
- `go test -race -count=1 -v ./cmd/assembly ./conformance ./conformance/admission -run '^Test(Assembly|CLI|ProductionCLI|ManualProgress|AcceptedRevocationClosesRealStartedExit|OpaqueTimeoutRemainsUnknownInCLI)'`：装配 5.908s、根 conformance 2.243s、admission 20.241s。真实配置负例在历史资格/恢复/IO 前拒绝；原回执、观察/报告/解释、撤销、完成/取消、核对和后续责任、caller/context、租约和未来 due time、固定 Result 及独立目标计数通过。
- `go test -race -count=1 -v -tags fault ./conformance/admission -run '^TestManualProgressLostObservationReceiptKeepsOriginalResponsibility$'`：真实 ledger.observation 丢回执从 CLI 传入，首错停止且原 UNKNOWN/已提交责任保留；原身份重试回放并结算，物理请求/效果/计费不增（2.514s）。
- `go test -race -count=1 -v -tags fault ./conformance/fault -run '^TestReconciliationCrashBoundariesPreserveQueryIdentity$/^(content.observation|ledger.observation|ledger.interpret)$'`：观察和解释的提交前崩溃、提交后崩溃、丢回执共 9 个原查询身份场景通过（50.182s）。

编译后的生产 CLI 验收分别记录了 Open 与 ManualProgress：初始 Open 增量 0，原 P5 POST +1；读查询命令的 Open 以 host-recovery 完成原 GET +1；随后的 recover Open 与 local-cli 手动推进增量 0，原 local-cli 请求身份和回执保持。另由独立已注册观察场景验收手动阶段原观察/报告/解释与账单推进；不把全进程计数归因于手动发送。

结构抽取采用原基线与公共 characterization；新构造接口有编译 RED，新增宿主完成门禁先缺失验证链接失败、补链接后 GREEN。完整 TDD、测试修正、合并冲突处理及同源记录在 `/tmp/lerna-module-interfaces-implementation/ticket-16.md`；最终日志为同目录 `ticket-16-final-public-race.log`、`ticket-16-final-manual-fault-race.log`、`ticket-16-final-existing-fault-race.log`、`ticket-16-final-check-code.log` 和 `ticket-16-final-check-docs.log`。仅解决本票，父 spec 与三处用户未提交修改保持不变；启动入口仍留给票据 17，完整矩阵与最终检查由票据 18 执行。
