# 09: API 执行与查询使用受信规则 Adapter

**What to build:** 真实 API 动作及其独立查询通过注入的固定受信规则解释证据，核心继续保存效果和责任。

**Blocked by:** None (can start immediately).

**Status:** resolved

- [x] Execution Manager 声明受信规则 Interface，生产装配注入固定 API Adapter；普通执行 Adapter 无规则注册权。
- [x] API 原动作、查询自身、被查主体及原 429 等待规则一起迁移，保留规则标识和账户、origin、键、尝试及发送绑定。
- [x] 剩余未迁移协议仅按明确固定清单走原实现；未知版本拒绝，不使用通用 fallback 或最新规则。
- [x] 公共真实 POST/GET、弱否定证明、遮蔽/无效正文、202/5xx、429、迟到观察及账单保持原事实与目标次数。
- [x] 兼容检查保持纯检查；闭合 UNKNOWN、历史发送、独立 QUERY、过期原键可读；未知原版本在恢复前拒绝。
- [x] 无 body 读取、凭据解析、重新编译或续期混入资格检查；按 ADR 更新设计并通过相关普通与故障场景。

## Comments

2026-10-07: Claimed for implementation on the integration branch.

## Answer

2026-10-07: 执行管理声明 `EvidenceRules`，宿主独立注入 `infra/rules.API`，普通请求编译 Adapter 无终局规则注册入口。API 原动作、独立查询自身、被查主体及 429 等待解释一并迁移；原规则标识、账户与 origin、键、尝试、发送和主体绑定保持。规则只返回核验后的候选证据，核心继续投影全部发送历史、处理冲突、安排原责任并保存事实，预算仍按原逐发送证据核验费用。未迁移 FILE、模拟目标、模型仅使用明确固定协议清单。恢复资格纯检查原规则版本，原编译器继续独立核验完整已保存声明与绑定；没有重新编译、正文读取、凭据解析、续期或发送。相关执行管理与 API 设计在实现前更新。

原有公共 API 行为基线通过；新增配置检查经历缺失规则及 typed nil 的 red/green，拒绝时原 Operation 与目标次数不变。真实 API 公共回归通过，覆盖 POST/GET、无效或遮蔽正文、弱否定证明、202/5xx、429、迟到观察与账单；过渡 FILE、模型、模拟目标与 Reconciliation race 切片通过。原生 API 执行、429 到期唤醒的崩溃与回执丢失、P5 内容闸门及原历史资格 fault+race 切片通过（fault 456.685s、历史资格 32.356s），包括闭合 UNKNOWN、历史发送、独立查询、过期原键与缺失当前凭据。该较广切片启动于规则提取后、typed nil 检查前，规则解释代码此后未改变。

最终验证源为 `82af6649ecae835ace496eea396d119a5838edfc`，已合入最新集成 `3ea459f5df8cc8e8d8a64b78d012c0ec7df5dd94`。装配冲突保留 Content 的完整依赖构造与验证以及 API 受信规则注入。该合并源的代表性 API 公共行为、配置与纯历史资格 race 切片（58.824s），窄 API 执行器 Driver 与当前 Content 闸门 fault+race 切片（7.401s），`make check-code CHECK_PACKAGES='./core/ledger ./infra/rules ./cmd/assembly'`、`make check-docs`、`git diff --check` 均通过。完整命令、基线、red/green 与受测树见 `/tmp/lerna-module-interfaces-implementation/ticket-09.md` 及同目录 `ticket-09-*.log`。

合并前核对待提交树与最终验证树 `4270b746e2af4005c01a13999fbd091d75ace208` 完全一致，复用已有检查；本次仅额外记录本工单状态、验收项与 Answer。未修改协议、schema、持久格式或父 spec；三个原有用户编辑未提交。完整集成检查由终点验收执行。
