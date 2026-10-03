# 03: 原命令规范化摘要

**What to build:** 调用方和负责方可以通过公开入口，对经过验证的原命令及受信主体绑定计算同版摘要；Go／TypeScript 能判断编码变化与业务请求变化，供后续持久接纳规则复用。

**Blocked by:** 02 — 严格命令解析与验证。

**Status:** resolved

- [x] 固定并记录规范化规则、摘要算法和版本；明确全部字段的处理方式，并用共同预期摘要夹具验证，不能只测试各实现自身的往返。
- [x] 摘要覆盖方法、准确目标、受信认证主体绑定、payload、expected_revision、accept_before 与合同版本；租户、主体及委托链绑定来自受信上下文，不能由 payload 自报身份替代。
- [x] 改变业务参数、认证主体、目标 owner、期限或预期修订时，得到不同摘要；跨版本语义不得默认为同一请求。
- [x] 只改变 trace_context、传输连接或发送次数时，摘要不变；对象键顺序与 JSON 空白的编码差异按同版规则处理，不能变成新的业务请求。
- [x] 规范化只处理编码，不改写正文、路径、收件人、金额单位或整数值；大整数与固定时间精度保持准确，不经浮点数中转，也不擅自归一化业务文本。
- [x] 无效输入先按严格合同拒绝，不通过规范化修补后继续计算；正反例同时覆盖合法边界、摘要不变及摘要必须变化的情况。
- [x] Go／TypeScript 对共享夹具产生准确相同的预期摘要；演示保留原 command_id、owner 和 accept_before 的重传，修改业务内容必须作为新命令处理。

## Scope

本任务提供摘要与身份规则的可执行合同证据，不声称已经实现数据库幂等冲突检测或持久去重。受信上下文可作为显式测试输入，实际查询入口的认证隔离由任务 05 完成。

## Comments

Implemented 2026-10-03 from integration `c0e12f7`, after ticket 02 resolved. Exact contract version is still pre-publication `1.0.0`, generator `1.0.0`. The implementation follows `decisions.md` and `envelope-decisions.md`: `lerna-command-digest-1` SHA-256 domain separation, the RFC 8785 no-number subset, UTF-16 key ordering, original array order and Unicode scalar preservation. Machine Schema generates closed SubjectBinding / DelegatedSubject types; there is no hand-edited generated type or additional dependency.

Public seams: Go `CommandDigest(rawCommand, rawTrustedSubject)` and async TS `commandDigest(rawCommand, rawTrustedSubject)`. Both re-enter the strict raw JSON and Schema boundary before computing any hash. The subject comes from the host's authenticated context. Payload identity-shaped fields remain business payload; they cannot replace that binding. A generic command may retain a future method for hashing, but the method is still unsupported at DecodeCommand. `command_id` remains the original command key and is excluded from the content digest; an edited business request requires a new key at the caller and later durable admission boundary.

Observed red → green: the first shared independently computed golden failed in Go because the public digest entry did not exist and in TS because the public export did not exist. Adding strict input validation, generated identity binding and canonical SHA-256 in each implementation made the original independent golden pass. Regression coverage then expanded without changing the already correct canonical implementation: 46 shared cases contain 31 accepted literal goldens and 15 classified refusals. Each positive expected hash was computed with Python hashlib from a worked canonical literal and is stored independently of either implementation; all literals were independently rehashed during verification. This is not an expected hash derived from either implementation under test.

Evidence includes exact large revisions, expected_revision absence and explicit values, changed version/profile/method/target/owner/tenant/deadline, trusted subject/delegation order, retained command identity with trace changes, JSON whitespace/key order/escape variants, non-BMP versus BMP key ordering, integer-looking keys, prototype-shaped keys, ordered arrays, null/bool, business whitespace/NFC versus NFD/path text, control characters and unescaped HTML/U+2028. Invalid controls cover duplicate decoded keys, raw numbers, missing/null/over-limit/malformed/unknown identity fields, invalid date/revision/Unicode and trailing JSON. The 16-member delegation boundary is accepted and 17 refused.

A full 1,048,576-byte command payload is accepted although its internal canonical identity-bound input has 1,048,599 bytes; adding one wire byte is refused. Internal digest assembly is not a new network body. The trusted binding is independently bounded and validated. The fixture documentation preserves a retransmission's original command_id, target owner and accept_before while trace changes; modified parameters produce a different independently expected digest.

Verification: frozen-lockfile pnpm install, `make check`, dedicated Go / TS public digest suites and Python hashlib literal verification passed with repository-pinned Go 1.27.1, Node 24.19.0, pnpm 12.8.1 and TypeScript 7.0.2. `make check` includes format/vet/typecheck, generation consistency, existing 6 generator refusal probes, all language tests, 109 existing bidirectional Go↔TS codec fixtures plus the 46 independent digest cases and builds. `git diff --check` passed. No concurrency or storage behavior changed.

Limits: this ticket does not implement authentication/signature verification, persisted idempotency conflicts or de-duplication, clock-based admission, network routing or execution authorization. Ticket 05 provides real query-boundary isolation using injected trusted context; later runtime slices preserve original identity durably. Only ticket 03 is resolved; the enclosing spec remains in progress.
