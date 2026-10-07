# 04: 输入、确认与授权链声明完整依赖

**What to build:** 会话输入、问题、确认及授权处理通过完整声明的依赖运行，合法替换 Adapter 不在正常调用时 panic。

**Blocked by:** None (can start immediately).

**Status:** resolved

- [x] Sessions 和 Grants 全部支持路径的 Store、Decisions、投递、问题、确认及事实依赖显式声明，含匿名或 checked 必需断言。
- [x] 生产 Adapter 通过消费方编译验证；关键缺项及 typed nil 在构造或该 Module 完成连接时拒绝。
- [x] 通过公共输入、问题、确认、授权签发和撤销流程验证原命令去重、会话顺序、单次确认消费及原授权使用记录。
- [x] 确认与授权消费仍加入原裁决事务；撤销保留源记录、端点停止确认与当前授权门禁。
- [x] 错误与可选能力缺席行为保持，未完整配置不得默认为成功；不扩大宿主或用户权限。
- [x] 相关设计先更新，普通与故障场景保留原事实及独立目标计数。

## Comments

2026-10-07: Claimed for implementation on codex/interfaces-04.

## Answer

2026-10-07: Sessions Store 组合输入投递及原回执、问题、确认和源事件的全部现有能力，所有必需存储断言改为声明方法。确认统一使用原 Store 与 Durable 裁决，移除重复的确认 Store／Decisions 装配。`WithConfirmations` 只连接任务和授权的确认事实。Sessions 与 Grants 的构造返回 Service 与错误，拒绝基础依赖的 nil／typed nil；`ValidateDependencies` 覆盖全部循环连接。Grants 原存储端口已完整，审计未发现额外必需断言。实际 SQLite、Durable、任务、内容、确认及撤销出口 Adapter 均有消费方编译验证。生产装配处理构造失败，并在兼容检查和恢复前验证连接；保留集成分支已有的 Ledger／Egress 构造检查与 `rules.Fixed`。相关会话和授权设计先更新。

声明端口包装器先复现正常输入缺少 `LoadInputDelivery` 的 panic，修复后公开输入、问题回应、确认展示及单次授权签发／消费／撤销保持原命令回执、会话提交序、原问题和输入引用、互斥的确认消费目标与授权历史。旧绕过 Decisions 的授权内部替身测试迁到真实准入入口；只读或撤销授权不产生使用记录、预算预留和目标调用。原裁决事务、当前授权门禁、确认撤回与准入竞争、撤销端点确认及源方回执保持；运行期间权威缺席继续返回原错误并保留责任。Task 创建顺序未移动，不扩大调用方权限，无协议或持久格式变更。

实现提交 `82c3fb0`，分支最终验证源 `0e6fea0fa1e24bc232a1c59e355a0dad60d29594`。该源的故障切片通过（72.127s）：21 个确认提交／回执丢失组合、18 个实际发送前后撤销组合和正文治理失败，检查原事实、单次消费、原端点及源方 ACK、使用／预留原子性和独立目标次数 0／1。合并前逐文件核对 Sessions／Grants、撤销出口和门禁、API 规则及相关故障测试与此源一致，复用这些故障证据。

本次组合为集成 `7a5a56b32aac5dbc94cc545d68069b6c3c266f9b` 与上述分支，无冲突；测试源码树为 `564a82495e28081538a3cb4ccd2e6a68983d8557`（追加本 Answer 前）。在此组合上重跑 `make check-code CHECK_PACKAGES='./core/sessions ./core/grants ./cmd/assembly ./conformance/sessions'` 通过：双 lint 0 issues、规则、格式及 scoped race；`go test -race -count=1 -v ./conformance/sessions ./conformance/admission -run 'Input|Question|Confirmation|Grant|Revocation|Withdrawal|ConfirmedRawPayload'` 通过（sessions 4.573s／admission 37.010s），包括构造与完成验证、原输入／问题、确认／授权、撤销、竞争及展示与发送的原始字节。文档与差异检查通过。基线、red／green 与复用故障证据见 `/tmp/lerna-module-interfaces-implementation/ticket-04.md` 和同目录 `ticket-04-final-fault.log`；合并后检查为 `ticket-04-merger-check-code.log`、`ticket-04-merger-behavior.log`。本次只更新本工单验收状态与 Answer，父 spec 与三个原有用户编辑保持不变；完整检查由终点验收执行。
