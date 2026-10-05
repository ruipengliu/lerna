# 12: 安全重发与保持未知

**What to build:** 对同一尝试标识保证幂等的目标，执行管理按原标识安全重发，并重新通过开始门禁、占用新的发送额度；既不可查询也不幂等的动作超时后一直保持"未知"，命令行明确展示，不自动重试。

**Blocked by:** 11（可查询目标的结果核对）

**Status:** resolved

- [x] 安全重发沿用原动作和原尝试标识，重新通过开始门禁；只在能力声明允许时进行（标注 G1、开始-5）
- [x] 模拟 API 记录的实际收到次数与"安全重发"的预期一致；幂等键过期的情况被识别
- [x] 不可查询且不幂等的超时保持未知，命令行可见（交互适配器 M1 演示 4）
- [x] 超时、进程替换都不改派执行端点，不改写成新任务（标注 G11）

## Comments

Claimed by ticket12 implementer on `codex/m1-ticket12`, integration base `b0a9acd`. Public assembly commands, independently counted simulator effects, and named crash hooks are the spec-confirmed TDD seams.

## Answer

Implemented explicit `PrepareResend` under the original operation, attempt, idempotency key and exact descriptor. Every resend receives a distinct immutable physical-send identity, repeats current P4/P5 gates and consumes an independent send reservation. Historical raw evidence, late bills and closure proofs retain their original send association. The aggregate effect preserves older uncertainty and terminal evidence; closing an unsent resend cannot erase an earlier unknown send.

The trusted reference adapter requires native key, account scope, exact request binding and all-send concurrency deduplication. Current capability semantics must match the original immutable declaration. Finite retention fixes the deadline at the original attempt and requires target-side rejection after expiry; expired keys, backward-clock uncertainty and unproved arrival safety refuse resend. Queryable idempotent targets require a fresh observation of the original attempt first. Opaque non-queryable, non-idempotent timeouts remain `UNKNOWN/MAY_OCCUR` across restart and are visible through the CLI without automatic retry. Model sends remain limited to one; repeated READ support is not enabled.

Capability reads now distinguish exact historical versions from current declarations. Model preparation/admission/confirmation, closure and reconciliation reject obsolete authority while historical model-input snapshots preserve their original facts. Integrated official ticket15 without changing body-digest field9, model preparation/closure gates or model max-sends1.

Validation: public RED→GREEN cases cover original identity, independent fees, late evidence/bills, closure, current capability replacement, finite expiry at prepare/P4/P5, target pending deduplication, query-first, stale replay and old UNKNOWN evidence preserving a current claim. Resend crash tests cover18 before/after/lost-receipt combinations. Complete `make check` passed on frozen tree `e6c9132346b5a9c0960a881d39d44680048f3cd7` on2026-10-05; full fault suite2355.799s, including all storage matrices and actual VFS/native synchronization failures. Full log: `/tmp/lerna-12-repaired-full-check.log`.

The first full run exposed a fault-VFS gap when real resend P4 opened anonymous SQLite SUBJOURNAL0x201e. Root-authorized exact ticket20 repair adds live temporary I/O forwarding and a standalone spill/rollback/write-error regression; actual child P4 remains unchanged and temporary bytes are not misclassified as durable journal data. Narrow resend/dispatch/query sync and temporary-journal race checks passed16.422s before the successful full rerun. The earlier failed run is retained at `/tmp/lerna-12-final-check.log`; it is not counted as a pass.
