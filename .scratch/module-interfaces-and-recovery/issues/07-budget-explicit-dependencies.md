# 07: 逐发送费用与结算声明完整依赖

**What to build:** 预算预留、每次发送占用、账单接纳、结算、释放和后续责任均通过声明的能力处理。

**Blocked by:** None (can start immediately).

**Status:** resolved

- [x] Budget 全部 Store、Decisions、usage、billing、send、release、version 与 settlement-followup 能力显式声明，不在调用中发现必需能力。
- [x] 生产 SQLite 与测试 Adapter 通过消费方编译验证；必需配置与循环链接缺项明确拒绝。
- [x] 公共准入、实际发送、账单、冲突及后续结算查询保留计费来源身份、原用量和原回执；退款、贷记及更正保留 M1 现有明确不支持的行为。
- [x] 未发送预留只按准确不可变封闭证明释放；P5 来源仍按独立费用终结证据结算并释放未用预留。
- [x] 取消、任务关闭或效果结论不清除费用；超上界、缺口和限额失效行为保持。
- [x] 按 ADR 0005 更新对应设计，原结算与释放普通及故障场景通过。

## Comments

2026-10-07: Claimed for implementation on codex/interfaces-07.

## Answer

2026-10-07: Budget Store 完整组合原用量、逐发送占用、计费来源／别名／分录／冲突、释放、历史版本及结算后续责任的既有持久能力，包含原来匿名发现的 `AllReservations` 与 `LoadBillingSourceVersion`。UsageSource 声明原报告、逐发送执行和记录、Operation 及四类不可变封闭证明查询；所有必需依赖断言改为声明方法。Decisions 保持只消费 Execute，未使用的通用 QueryExecution 不再成为预算替换 Adapter 的要求。SQLite、Durable、Ledger、Content、三类任务封闭依据及声明接口包装器通过消费方编译验证；旧 Store 表面在独立负向编译探针中因缺少 AllReservations 被拒绝。

`New` 返回 Service 与错误，拒绝 Store、Decisions 的 nil／typed nil；`ValidateDependencies` 检查这两项与 UsageSource、BillingEvidence、完成、取消、非成功关闭依据的五个循环链接。生产宿主处理构造错误，并在历史兼容检查和业务恢复前验证连接。只精确放开 Budget 对 `core/durable` 公共配置 helper 的导入。预算设计 3.1 先按 ADR 0005 更新，未改变协议、持久格式、事务域、来源身份、原用量／回执／版本及费用事实写入方。

声明 Store 包装器的公开 AdjustLimit 先复现缺少 LoadBudgetVersion 的 panic；声明 UsageSource 包装器的真实出口发送先复现隐藏 BillingExecution 依赖。修复后原额度版本、原决定回执和单次实际发送保留。代表普通场景通过（admission 25.053s）：首次及重复用量、两次不同发送、丢失回执后迟到超上界账单、别名／同版本冲突、历史查询、CLI 额度调整、准确未发送证明与完成封印释放、P5 未知费用保留、关闭后迟到账单、整数上界、上界失效及失败／取消关闭的历史结算责任。更正、退款与贷记仍返回 M1 原不支持错误；取消、任务关闭和效果结论不抹去费用，固定 Result 不改写。未发送释放不伪造 Usage 或返还次数；P5 来源凭独立最终账单结算和释放未用部分。

实现提交 `2e8edd08dfc1380e27fcc6c2da7f93a840e8caa9` 的故障切片通过（25.545s）：18 个用量交接、额度／导入／释放提交与回执丢失组合，以及 6 个 FAILED／CANCELLED P5 后结算责任完成组合。原余额、来源、回执、固定 Result、结算后续责任与独立目标次数保持。合并前逐文件核对 Budget、相关故障测试、用量交接、出口、开始门禁、API 规则和计费模拟目标与该源一致，复用 24 个故障窗口的证据。额外整包 admission race 已按开发范围停止，未计为通过；最终候选的完整检查仍由终点验收执行。

本次合并集成 `d4cbc44f89e8d3c1fc8d1403a27f79e4643d384a` 与分支 `ab21da201a4e4003eb53eefc4e74c7089bd9b30f`。装配冲突仅是恢复前验证列表追加，保留 Sessions、Grants、Budget 三项，以及既有 Content／Trace／Ledger／Egress checked 构造与 `rules.Fixed`。测试源码树为 `1d0da3507ea766ecc7e63269eb3ffece2bb5de1a`（追加本 Answer 前）。组合后精确公开 race 通过（budget 1.478s、sessions 2.720s、admission 8.811s），覆盖 Budget 声明 Adapter 与构造／完成检查、Session／Grant 构造与完成配置、原输入／问题／单次确认、真实用量、文件零费用、准确未发送释放和失败／取消关闭后迟到费用；`make check-code CHECK_PACKAGES='./core/budget ./core/sessions ./core/grants ./cmd/assembly'` 通过双 lint、格式、规则及 scoped race，文档与差异检查通过。基线、red／green 与各源证据见 `/tmp/lerna-module-interfaces-implementation/ticket-07.md`，组合日志为同目录 `ticket-07-merger-behavior.log`、`ticket-07-merger-check-code.log`。本次只更新 07 状态、验收项与 Answer；父 spec 与三个用户原编辑保持不变。
