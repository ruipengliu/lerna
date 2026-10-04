# Standards — fixed 3f44032

BASE `f05b2f1068958ba6b63e6c1dc58d6cd1684446cc` → HEAD `3f44032ffaaf9cd51fc253399412252767f0f737`: actual 7 commits/49 paths (7155+/62−). Fully reviewed all 15 incremental paths; independently verified all 34 carried paths, standards sources, prior archives and 73 protected existing objects equal by mode/type/blob. Complete metadata/provenance: `/tmp/lerna-04-ticket-02-standards-3f44-scope.json`.

**Hard: 0; worst none.** All own hard findings are CLOSED: original processing deadline checks survive the fix; original three paginated readers preserve Close causes; the remaining `UnindexedVersions`/`PendingChanges` now use named-result readers joining primary, iteration and Close errors (`adapters/postgres/content/management.go:123,239`). Added mechanical driver cases cover early scan/decode errors and EOF Close; they claim SQL-port mechanics, not native PostgreSQL Close faults.

The policy-change completion at `domain/content/management.go:959` now durably hands off the original natural due/deadline even after that due passed; expired processing remains residual. Historical pending holders and exact original cursors are retained.

New expired-old-writer archive: all six SHA digests verified, raw dump 112782 bytes, all79 rows and DDL equal the E-string restore after complete mechanical COPY conversion. Its original `2026-10-04T16:00:30.057895Z` source cutoff matches observation/policy; receipts and independent bytes remain original. Product provenance is the documented stopped1a7 source, with complete producer-source inspection; this review did not independently execute that producer or restore. Prior archive limitations remain explicit.

**Judgement: 1 possible smell; worst P3.**

- **Possible Duplicated Code remains:** four typed page readers (`management.go:71,299,395,439`) repeat “`if len(…) == limit { next = last; break }`”, scan/decode/cursor updates and joined-close handling. Fowler’s shared-shape heuristic suggests a minimal private page consumer. This remains optional maintenance judgement, not a hard extraction requirement.

All twelve assessed: Mysterious Name, Duplicated Code, Feature Envy, Data Clumps, Primitive Obsession, Repeated Switches, Shotgun Surgery, Divergent Change, Speculative Generality, Message Chains, Middle Man, Refused Bequest. Repository closed phases/actions, consumer ports/decorators and frozen provenance override mechanical flags; tooling-enforced format/vet/types/whitespace excluded.

Read-only; no Go/native/build/test/DB. Owner-reported focused normal0 does not replace final race/check/audit/CI. Seven AC remain unresolved; whole04 remains incomplete.
