# 17: 启动恢复接入统一宿主入口

**What to build:** 完整装配的宿主用相同恢复 Module 接续原责任，在处理前核验全部旧版本并保留启动等待策略。

**Blocked by:** 08（生产装配完成后才允许资格检查与恢复）、16（CLI 手动推进使用统一宿主入口）.

**Status:** resolved

- [x] 复用 16 的 Module，加明确 Startup 模式；只有 08 的完成装配结果才允许进入资格检查和恢复。
- [x] 先检查 tasks 与 ledger 的全部原兼容性，再按规格的完整启动顺序执行，保留两次 Operation progress 及后续流程。
- [x] 保留启动期限、自然租约到期、新进程领取身份和原命令/请求/位置/准入/发送身份；不持锁等待。
- [x] 普通启动生产者与 driver 的固定主体分别保留；不把普通 caller 升级为 host，不自动签发授权或配置任务。
- [x] 只恢复已保存且启用 driver；UNKNOWN、取消、非法输出及原核验拒绝保持等待或原唯一继续请求。
- [x] 资格拒绝不领取另一 READY 工作，不增加独立目标/供应商/账单次数；完整与部分恢复、丢回执及停机副本场景通过。
- [x] 装配不再维护业务恢复列表，相关设计更新且公开启动验收通过。

## Comments

- 2026-10-07: Claimed on `codex/interfaces-17` after 08 and 16 were resolved in integration.

## Answer

2026-10-07: `infra/hosting.Service` 增加独立 `Startup` 模式及 18 个必需窄端口和固定用户配置。两个模式的 checked `New` 立即拒绝缺项、nil、typed nil；合法 `NewManual` 保留原 Sessions 必需、其余手动能力可选的语义。生产完成门禁额外执行 `ValidateStartupDependencies`，Manual-only 实例不能进入启动；Startup 自身也在任何资格/恢复调用前检查启动配置。装配仅连接具体负责方并委托同一宿主，不再保存业务恢复清单。

启动原 21 个调用完整迁入宿主：Tasks 与 Ledger 历史资格先行，其后按原顺序恢复目标、原交接、内容登记/观察、报告/解释、撤销、取消/完成、两次动作进展及中间核对、任务封闭交付、非成功关闭、预算封闭、执行/结算后续责任、启用 driver 和运行记录。逐项比较原方法及 issuer 字面量完全相同；普通启动固定 `host-recovery`，driver 固定 `host`。ManualProgress 执行体与基线逐字相同，不以手动入口拼接启动、不提升普通 caller 权限、不新建授权或 driver。原 65 秒 context 仍在 Open 的原位置创建，物理格式/身份资格、存储打开、错误清理及 owner 的自然租约/due time/领取身份与事务外等待保持。Core、contracts、SQLite、默认策略和适配器源码未改，原请求、位置、准入、发送、Result 和持久格式均由原 owner 保留。

必要配置 tracer 取得真实 RED：合法 Manual-only 宿主被旧启动当作完整配置，返回 nil 并执行三类历史资格查询和各域恢复。GREEN 返回 `missing required dependency: hosting.startup`，资格/恢复/物理 IO 边界次数均为零；公开 Startup 同样拒绝 Manual-only。18 个启动端口分别覆盖 nil/typed nil，固定用户缺失也明确拒绝。真实模块、SQLite 与 Work 配置切面复用 08 的获准例外；不新增公开测试 hook，不将边界计数冒充独立目标计数。公开行为原本已通过，作为真实 characterization 基线而非制造失败。

候选 `b2a7725d9a9dccc4c153ce41af2e4e2808c3cbc1` 的精确公开 race 通过（assembly 15.150s、conformance 2.763s、admission 64.686s）：Open 原目标、编译 CLI 的默认推理/权限和手动模式增量、原 driver 位置/实际动作、缺权限/UNKNOWN、核验拒绝唯一继续请求、取消、文件 UNKNOWN、完成租约恢复、核对、固定 Result/迟到账单及停机副本。原历史资格故障切片通过（fault 2.323s、admission 68.846s），覆盖 owner 契约、原模型/执行/API 纯资格、禁用原 driver/原配置、完成原 outcome claim 与过期历史；另两例不可变 driver 版本拒绝通过（2.920s），保留另一 READY 原 Job 和独立目标/模型/账单次数。三个原 tasks.start、ledger.dispatch、tasks.model_result 进程崩溃窗口通过（63.911s），原请求/位置/claim/send 与独立供应商次数保持。`make check-code CHECK_PACKAGES='./infra/hosting ./cmd/assembly ./adapters/interaction'` 通过格式、普通/fault 双 lint、规则和 scoped race；文档及差异检查通过。

16 的原 `TestManualProgressUsesOriginalCallerAndKeepsOptionalAbsence` 取消上下文场景在本票组合运行中失败，精确 count=3 在当前与未修改的 `4362ad1` 基线均复现（回执不等或后续未取消调用返回 DEPENDENCY_UNAVAILABLE）。QueryReceipt 可把读取不可用表示为 UNAVAILABLE 加 nil Go error，不能据此声称原事实已丢失/推进。该失败如实保留，不计入 17 的 PASS；16 owner 正按 diagnosing-bugs 做独立精确诊断，必要修复与最终完整候选检查由 18 收尾。本票未改存储、错误分类或弱化原断言，也未执行完整矩阵。

主集成 `4362ad13a2c86fae4faa7b9c87b289fd29bb555d` 合并无冲突。追加本 Answer 前的树 `70004bc50d35e4e6d739c3bda806d4fa7b07d047` 与候选已测树完全相同，逐路径源码比较为空，故复用上述 exact 范围检查。基线、red/green、命令、已知失败及来源记录见 `/tmp/lerna-module-interfaces-implementation/ticket-17.md`；日志包括 `17-public-race.log`、`17-check-code.log`、`17-compatibility-fault.log`、`17-driver-history-fault.log`、`17-driver-crash-fault.log` 及 `17-manual-repeat-current.log`、`17-manual-repeat-baseline.log`。本次仅更新 17 状态、验收项和 Answer，父 spec 与三个用户原编辑不变，未混合 18 实现或诊断修复。
