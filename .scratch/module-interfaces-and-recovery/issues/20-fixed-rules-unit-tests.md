# 20: 固定证据规则的单元测试与模拟目标清单去重

**What to build:** 在 `rules.Fixed` 这一执行管理装配的接口上补纯单元测试，覆盖各原协议的资格判断与解释结果；`infra/rules` 内两份模拟目标版本清单合成一份。不改核心、装配或设计。

**Blocked by:** 11（模型、模拟目标与查询完成受信规则迁移）.

**Status:** resolved

- [x] `infra/rules/fixed_test.go` 经 `rules.Fixed` 验证：全部原模拟目标、模型、受管理文件声明被接受；未知标识、版本、动作、改动的协议／判据、缺少 API 描述均以 `PREPARATION_UNRECOVERABLE` 拒绝，`Interpret` 不返回事实。
- [x] 解释结果：模拟写入只有终局协议证据才排除迟到；独立查询分别返回查询自身与原主体的终局，"当前不存在"无否定证明时原主体保持未知；模型与文件在来源不可信、未绑定发送或缺少读回时保持未知。
- [x] `simulatorAdapters` 由规则选择与 `Simulator.CheckSupported` 共用；去掉任一版本使上述测试失败。
- [x] 不改变规则标识、结果、原因或拒绝码。

## Comments

- 2026-10-08: Claimed on `codex/interfaces-20`. Replaces architecture-review candidate 2 ("adapter catalogue"): checked against ADR 0003 §3 and ADR 0011, assembly dispatch by adapter is by design, and both dispatch tables already reject unknown adapters; moving target-protocol knowledge out of core would need layers §2 changes. Only the untested pure rules and the duplicated list remain.

## Answer

2026-10-08: 新增 5 个 `rules.Fixed` 纯测试（14 个受支持声明、13 个拒绝情形、模拟写入 8 种观察、独立查询 5 种原主体证据、模型 2 种、文件 4 种），在原实现上直接通过，属于 characterization，不制造 RED。随后 `fixedRules` 与 `Simulator.CheckSupported` 改用同一个 `simulatorAdapters`；临时删去 `simulator-opaque` 时 7 处断言失败，恢复后通过。

检查（未提交工作树，基于 `8fff641`）：`make check-code CHECK_PACKAGES='./infra/rules ./core/ledger ./cmd/assembly'` 的格式、普通及 fault lint（0 issues）、规则检查与 scoped race 通过；`make check-docs` 通过。规则选择是等价改写，未运行故障矩阵。
