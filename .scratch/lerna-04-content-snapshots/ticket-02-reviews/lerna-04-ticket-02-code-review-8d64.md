# Two-axis review — fixed 8d64

Independent reports are retained separately; source review does not qualify pending native race/check/audit or archive restore.

## Standards

# Standards — fixed 8d64ad1

BASE `f05b2f1068958ba6b63e6c1dc58d6cd1684446cc` → HEAD `8d64ad1ed8eee35d07ee3fc990dd2696729ef9c8`: all 6 commits/42 paths (5887+/62−). Fully reviewed seven incremental paths (1445+/71−); verified all 35 carried paths, standards documents, frozen archives and 73 protected existing objects by actual mode/type/blob equality. Full qualification: `/tmp/lerna-04-ticket-02-standards-8d64-scope.json`.

Original deadline P2 is CLOSED: fresh clocks qualify descendant locks, page progress and every visited original deadline at the final decision; expired work retains original cursor/deadline and residual. Original three pagination Close locations are CLOSED: named results join primary, iteration and Close causes. New phase/current-basis classification and sticky holder facts retain historical pending responsibility; mechanical Rows tests explicitly do not claim native PostgreSQL Close faults.

**Current hard: 1; worst P2.**

- **P2 — Remaining early-error SQL Close causes are discarded.** `adapters/postgres/content/management.go:120,231` still uses “`defer rows.Close()`” in `UnindexedVersions`/`PendingChanges`. If Scan or JSON decoding fails (:125–130/:235–240), these functions return only that primary error, discarding a simultaneous drain/Close error. Normal exhaustion automatically closing Rows does not cover these early-return paths. This is the same documented cause-preservation failure beyond the original three reported pagination locations: `AGENTS.md:105` requires “错误必须保留可判断的原因…不得吞掉错误”. Join Close and iteration causes into the returned primary error on every path.

**Current judgement: 1 possible smell; worst P3.**

- **Possible Duplicated Code remains OPEN:** four typed readers at `management.go:71,289,385,429` repeat “`if len(…) == limit { next = last; break }`”, scan/decode/cursor updates and identical joined-close handling. Naming separate typed functions does not share the shape. A minimal private shared consumption shape remains a possible maintenance improvement, not a hard requirement.

All twelve retained: Mysterious Name, Duplicated Code, Feature Envy, Data Clumps, Primitive Obsession, Repeated Switches, Shotgun Surgery, Divergent Change, Speculative Generality, Message Chains, Middle Man, Refused Bequest. Repository closed phases/actions, consumer ports/decorators and frozen archive provenance override mechanical flags; format/vet/types/whitespace remain tooling exclusions.

Read-only; no reviewer native runs. Supplied controls do not qualify pending race/check/audit or the future expired-old0001 archive delta. Seven AC remain claimed/unchecked.

## Spec

Independent SPEC followup: base `f05b2f1068958ba6b63e6c1dc58d6cd1684446cc` → source `8d64ad1ed8eee35d07ee3fc990dd2696729ef9c8`. Actual merge-base equals base: six commits, 42 paths, 5887+/62−. All seven AC reviewed. Qualified 35 unchanged mode/type/blob entries against my complete57ea scope; fully read all seven fresh paths (1445+/71−), including complete new tests. Origin spec/issues/handoff/oracle and adopted expiry/renewal clarifications covered; detailed scope is `/tmp/lerna-04-ticket-02-spec-8d64-scope.json`.

Counts: **(a) missing/partial 0; (b) scope creep 0; (c) implemented wrong 1. Worst: P2.**

**[P2] Preserve natural-expiry responsibility when initial propagation finishes late.** Origin `ticket-02-handoff.md:61` requires “政策自然到期也不能只靠下一次 query 才建清理责任” and owner work to advance that original-deadline obligation. Adopted renewal clarification:18 separates historical installation qualification from fresh current maintenance; :25 forbids refreshing the original budget. In `domain/content/management.go:956–965`, an initial `policy_change` whose final page completes after `ExpiryDue` but before its original `Deadline` becomes `complete` without a natural-expiry check or pending/residual maintenance. Since :375–378 correctly evaluates historical validity at original `Due`, install a short ValidUntil on an already published A with distant RetainUntil/current cap, then delay its first propagation until after ValidUntil: the historical registration is `not_required`, and the natural obligation disappears. Existing old-revision maintenance may be distant; no query should be needed. Preserve historical facts and, within the unchanged original budget, register the current natural check or retain an explicit unresolved maintenance responsibility instead of closing it. Add a real public allowed/control and delayed-initial case.

Both prior57ea P2 findings are closed: deadlines are requalified after blocking work/final due reads; later descendants now receive immutable ancestor-specific admission obligations, with current renewal qualification and conservative existing-pending merges.

No tests/build/DB/native operations or opposite-axis input. Parent’s focused normal completion is external evidence; new race/check/audit remain pending at this cutoff. Existing2099 archives do not prove expired0001 upgrade. Seven AC remain unchecked; this is source review, not acceptance or whole04 completion.

Standards: 1 hard P2, 1 judgement P3; Spec: a0/b0/c1, worst P2. Axes are not merged or reranked.
