# Standards — fixed 57ea60c

BASE `f05b2f1068958ba6b63e6c1dc58d6cd1684446cc` → HEAD `57ea60c628f82140f5700503107d3e2fe4a86dec`: all 5 commits/39 paths (4513+/62−). Complete live-code/document review; archives qualified through full bytes, decoded facts, identical DDL, 12 SHA checks and complete COPY→INSERT equivalence. All 73 protected schema/generated/golden/0001 objects retain mode/type/blob. Scope: `/tmp/lerna-04-ticket-02-standards-57ea-scope.json`.

**Hard: 2; worst P2.**

- **P2 — Original processing deadline can expire during a page.** `domain/content/management.go:544` checks `change.Deadline` before blocking `Descendants`/`LockVersion`; :577–594 then advances cursor and writes `complete`/`scheduled` using that old `now`. The fresh :600 clock checks only Claim validity. A page crossing the allowed 5ms WorkBudget, with context/Claim/trusted capability still valid, can commit successful progress after its original deadline rather than residual. Violates `AGENTS.md:125` (“校验授权、预算和期限”) and adopted `ticket-02-handoff.md:59` (“超时保留 pending/residual…不能成功跳过剩余页”). Recheck/bound the original deadline after waits before qualifying page progress.
- **P2 — Discarded SQL close causes.** `adapters/postgres/content/management.go:68,254,302` uses `defer rows.Close()`. Full pages deliberately `break` before exhaustion (:77–79/:264–267/:312–314); `return … rows.Err()` evaluates before that Close, and its returned error is discarded. A drain/Close failure therefore loses its identifiable cause. Violates `AGENTS.md:105` (“错误必须保留可判断的原因…不得吞掉错误”). Preserve Close causes alongside scan/decode/iteration errors.

**Judgement: 1 possible smell; worst P3.**

- **Possible Duplicated Code (P3):** the same three page readers repeat “`if len(…) == limit { next = last; break }`”, decoding, cursor updates and end handling. A small private shared page-consumption shape could keep cursor and close qualification consistent; no generic repository framework is needed.

All twelve assessed: Mysterious Name, Duplicated Code, Feature Envy, Data Clumps, Primitive Obsession, Repeated Switches, Shotgun Surgery, Divergent Change, Speculative Generality, Message Chains, Middle Man, Refused Bequest. Repository closed action cases, consumer ports/decorators and frozen archives override mechanical flags; skip tooling-enforced format/vet/types/whitespace.

Read-only; no native runs. Supplied normal results do not qualify pending race/check/audit. Original producer Close limitations remain historical; seven AC remain unchecked.
