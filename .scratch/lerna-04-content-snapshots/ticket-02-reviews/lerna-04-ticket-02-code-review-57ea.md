# 04 ticket02 fixed candidate independent review

Base f05b2f1068958ba6b63e6c1dc58d6cd1684446cc → 57ea60c628f82140f5700503107d3e2fe4a86dec. Read-only axes; original seven AC remain unchecked. This candidate is not accepted.

## Standards

# Standards — fixed 57ea60c

BASE `f05b2f1068958ba6b63e6c1dc58d6cd1684446cc` → HEAD `57ea60c628f82140f5700503107d3e2fe4a86dec`: all 5 commits/39 paths (4513+/62−). Complete live-code/document review; archives qualified through full bytes, decoded facts, identical DDL, 12 SHA checks and complete COPY→INSERT equivalence. All 73 protected schema/generated/golden/0001 objects retain mode/type/blob. Scope: `/tmp/lerna-04-ticket-02-standards-57ea-scope.json`.

**Hard: 2; worst P2.**

- **P2 — Original processing deadline can expire during a page.** `domain/content/management.go:544` checks `change.Deadline` before blocking `Descendants`/`LockVersion`; :577–594 then advances cursor and writes `complete`/`scheduled` using that old `now`. The fresh :600 clock checks only Claim validity. A page crossing the allowed 5ms WorkBudget, with context/Claim/trusted capability still valid, can commit successful progress after its original deadline rather than residual. Violates `AGENTS.md:125` (“校验授权、预算和期限”) and adopted `ticket-02-handoff.md:59` (“超时保留 pending/residual…不能成功跳过剩余页”). Recheck/bound the original deadline after waits before qualifying page progress.
- **P2 — Discarded SQL close causes.** `adapters/postgres/content/management.go:68,254,302` uses `defer rows.Close()`. Full pages deliberately `break` before exhaustion (:77–79/:264–267/:312–314); `return … rows.Err()` evaluates before that Close, and its returned error is discarded. A drain/Close failure therefore loses its identifiable cause. Violates `AGENTS.md:105` (“错误必须保留可判断的原因…不得吞掉错误”). Preserve Close causes alongside scan/decode/iteration errors.

**Judgement: 1 possible smell; worst P3.**

- **Possible Duplicated Code (P3):** the same three page readers repeat “`if len(…) == limit { next = last; break }`”, decoding, cursor updates and end handling. A small private shared page-consumption shape could keep cursor and close qualification consistent; no generic repository framework is needed.

All twelve assessed: Mysterious Name, Duplicated Code, Feature Envy, Data Clumps, Primitive Obsession, Repeated Switches, Shotgun Surgery, Divergent Change, Speculative Generality, Message Chains, Middle Man, Refused Bequest. Repository closed action cases, consumer ports/decorators and frozen archives override mechanical flags; skip tooling-enforced format/vet/types/whitespace.

Read-only; no native runs. Supplied normal results do not qualify pending race/check/audit. Original producer Close limitations remain historical; seven AC remain unchecked.

## Spec

Independent SPEC review: `f05b2f1068958ba6b63e6c1dc58d6cd1684446cc` → fixed `57ea60c628f82140f5700503107d3e2fe4a86dec`.

Counts: (a) missing/partial 0; (b) scope creep 0; (c) implemented incorrectly 2. Worst: P2.

[P2, c] Recheck the original propagation deadline after blocking page work. Adopted ticket-02-handoff.md:59 requires “原绝对deadline…超时保留pending/residual/阻塞原因”; :39 requires fresh DB time after blocking locks. domain/content/management.go:544 checks change.Deadline using the pre-page clock; Descendants/LockVersion can then wait (:561–575), after which :577–594 commits completed/scheduled progress. The final fresh clock (:600–613) validates only Claim. A short WorkBudget with a descendant lock crossing its deadline but not the Claim lease can incorrectly complete the change. Recheck that original deadline after waits and before progress commit, preserving the original cursor/known-holder residual instead of successful completion.

[P2, c] Schedule natural ancestor-policy expiry for post-watermark admissions. Handoff:57 requires newly admitted descendants to “单调继承约束”; :61 requires natural expiry maintenance at new admission, independently of queries. management.go:448–469 freezes a source's expiry watermark at source admission. A later derived Put schedules only its target policy (service.go:304–305), with Due using target policy and CurrentRetainUntil (:454). An ancestor's earlier ValidUntil is neither inherited retention nor that target due. At ancestor ValidUntil expiry, Descendants excludes the later generation (adapters/postgres/content/management.go:250). New use correctly fails closed, but that derived holder lacks the required expiry cleanup responsibility until its much later retention deadline. Register the inherited expiry obligation during its admission without widening the frozen old change.

Coverage: independently verified five commits/39 paths,4513 additions/62 deletions, correct merge-base; full originating seven AC and changed scope, including archive data/DDL, reviewed. Complete closure/intermediate dedup, five-action separation, current gates, replay/association and malformed-binding subject/purpose classification are implemented. Frozen0001 and old/new contract trees remain exact. Public tests use real PG/object/reopen and independent bytes; atomic rollback and multipage recovery have real seams. Two old-oracle adjustments retain receipts/history/bytes and fresh-chain normals.

No reviewer tests/build/DB/native execution or opposite-axis input. Parent-reported affected normal success is separate evidence; race/check/audit are not final. No physical Delete, SIGKILL, Grant, Task or whole-profile requirement was imposed. Scope proof: lerna-04-ticket-02-spec-57ea-scope.json.

Standards: 2 hard P2 + 1 optional possible smell P3. Spec: a0/b0/c2, worst P2. No cross-axis ranking or test/acceptance inference. Necessary fixes stay with the sole implementation owner; final new source requires separate axis followups.
