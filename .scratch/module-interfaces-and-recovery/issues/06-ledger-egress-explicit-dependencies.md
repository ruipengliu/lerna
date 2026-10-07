# 06: 动作执行与核对声明完整依赖

**What to build:** 原动作接纳、开始、发送、证据、核对与封闭通过完整依赖运行，保存原责任且不因替换 Adapter 意外 panic。

**Blocked by:** None (can start immediately).

**Status:** resolved

- [x] Execution Manager 全部 Store、Work、observed-work 与核对 Job 能力显式声明，包含历史、封闭、回报及指标。
- [x] Egress Gate 的必需依赖与明确可选能力分别表达；FILE 检查出口仍按原资格验证，不建立绕过门禁的物理路径。
- [x] 生产执行域 Work、SQLite 与短租约测试 Adapter 通过消费方编译验证；缺项在构造或连接完成时拒绝。
- [x] 实现公开完整 Store 的最小有效 Adapter 能完成相应接纳调用；原 ledger.Store 编译后 panic 的复现路径被消除。
- [x] 通过公共执行、重发、核对、观察、封闭与查询验证原回执、发送身份和独立目标计数。
- [x] P4/P5、未知效果、迟到可能性、历史版本检查、来源事务和原收尾决定不变；设计及相关普通与故障验证通过。
- [x] 本票只迁移依赖声明、注入、编译检查和配置验证；规则提取、共同封闭算法、目标创建内聚及恢复编排由各对应票据交付。

## Comments

2026-10-07: Claimed for implementation on the integration branch.

## Answer

2026-10-07: Execution Manager 的 Store 组合全部现有历史、封闭、原始观察、解释、回报、核对、来源事件与指标能力，执行域 Work 明确包含 observed-work 和核对 Job；编译器的纯历史检查为必需能力。生产 SQLite、执行域持久工作与短租约 Adapter 均按消费方接口编译验证。Egress 显式要求文件治理和最终资格复查、取消与非成功封闭能力；可选 ContextCompiler、PreflightIO、CheckedIO 保留原 fallback 或错误，FILE 缺少 CheckedIO 时不会转到普通物理出口。构造函数立即拒绝缺失输入，ledger 的循环连接由 ValidateDependencies 完成检查，包含固定 EvidenceRules，全部覆盖 typed nil；生产装配在保存版本检查及恢复之前完成校验。设计先于实现更新，依赖许可仅增加实际使用的 public durable 根包。

公开 Store 包装器先复现 CompletionSealForOperation 隐藏断言 panic，声明完整后完成原交接接纳、原回执重放、READY 责任和零目标调用。声明 Work 包装器先复现重发缺少 ExecuteObserved 与核对 Job panic，再通过原重发和独立查询断言：原 Attempt 与外部键保持、发送身份独立、两次请求与一次效果，原费用责任和未知历史保留。声明 Egress 包装器通过实际 FILE 字节、原观察、结算与完成结果，以及取消和非成功封闭检查；原临界区、P4/P5、历史版本、来源事务和封闭决定保持。本票只迁移依赖与配置，09 的规则提取作为既有集成输入合入。

实现提交为 15b77f0，共享 helper 为 58acd48；09 同步及 rules 必需连接修订为 005f83c，分支最终 c2cbc5e。合并至含 02、03、09 的集成 e28fd88 无冲突，保留 Content 的 ObservationWork 构造、Trace 的检查构造与 ValidateDependencies，以及固定 API 规则注入。组合受测待提交树为 eca8fca56835bd4749d15632e25ac30a357d4b38（合并提交 79387ff）；`make check-code CHECK_PACKAGES='./core/durable ./core/ledger ./core/egress ./core/content ./core/trace ./infra/rules ./cmd/assembly ./conformance ./conformance/content'` 通过（普通/fault lint 零问题、规则及 race，包括构造/完成、Content、Trace 的公开 Adapter 检查）；代表 API 历史资格、终局证据、429、独立查询以及 Store/Work/Egress 包装器、文件、封闭和 Trace race 切片通过（37.031s）；两类执行器 fault/race 切片通过（8.981s）。54 个核对提交前/后崩溃与回执丢失窗口已通过（443.136s），按记录复用原 ledger 依赖改动源结果，未冒充最终全量验收。完整命令、基线、red/green、版本与合并验证见 `/tmp/lerna-module-interfaces-implementation/ticket-06.md`；完整集成检查由终点验收执行。父 spec 与三个已有用户编辑保持。
