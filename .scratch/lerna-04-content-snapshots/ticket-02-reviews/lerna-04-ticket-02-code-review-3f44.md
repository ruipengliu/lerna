# Two-axis review — fixed 3f44

Source qualification only; execution and later modified tests need their own accurate evidence.

## Standards

# Standards — fixed 3f44032

BASE `f05b2f1068958ba6b63e6c1dc58d6cd1684446cc` → HEAD `3f44032ffaaf9cd51fc253399412252767f0f737`: actual 7 commits/49 paths (7155+/62−). Fully reviewed all 15 incremental paths; independently verified all 34 carried paths, standards sources, prior archives and 73 protected existing objects equal by mode/type/blob. Complete metadata/provenance: `/tmp/lerna-04-ticket-02-standards-3f44-scope.json`.

**Hard: 0; worst none.** All own hard findings are CLOSED: original processing deadline checks survive the fix; original three paginated readers preserve Close causes; the remaining `UnindexedVersions`/`PendingChanges` now use named-result readers joining primary, iteration and Close errors (`adapters/postgres/content/management.go:123,239`). Added mechanical driver cases cover early scan/decode errors and EOF Close; they claim SQL-port mechanics, not native PostgreSQL Close faults.

The policy-change completion at `domain/content/management.go:959` now durably hands off the original natural due/deadline even after that due passed; expired processing remains residual. Historical pending holders and exact original cursors are retained.

New expired-old-writer archive: all six SHA digests verified, raw dump 112782 bytes, all79 rows and DDL equal the E-string restore after complete mechanical COPY conversion. Its original `2026-10-04T16:00:30.057895Z` source cutoff matches observation/policy; receipts and independent bytes remain original. Product provenance is the documented stopped1a7 source, with complete producer-source inspection; this review did not independently execute that producer or restore. Prior archive limitations remain explicit.

**Judgement: 1 possible smell; worst P3.**

- **Possible Duplicated Code remains:** four typed page readers (`management.go:71,299,395,439`) repeat “`if len(…) == limit { next = last; break }`”, scan/decode/cursor updates and joined-close handling. Fowler’s shared-shape heuristic suggests a minimal private page consumer. This remains optional maintenance judgement, not a hard extraction requirement.

All twelve assessed: Mysterious Name, Duplicated Code, Feature Envy, Data Clumps, Primitive Obsession, Repeated Switches, Shotgun Surgery, Divergent Change, Speculative Generality, Message Chains, Middle Man, Refused Bequest. Repository closed phases/actions, consumer ports/decorators and frozen provenance override mechanical flags; tooling-enforced format/vet/types/whitespace excluded.

Read-only; no Go/native/build/test/DB. Owner-reported focused normal0 does not replace final race/check/audit/CI. Seven AC remain unresolved; whole04 remains incomplete.

## Spec

Independent SPEC followup: base `f05b2f1068958ba6b63e6c1dc58d6cd1684446cc` → fixed source `3f44032ffaaf9cd51fc253399412252767f0f737`. Actual merge-base equals base: **seven commits, 49 paths, 7155+/62−**. Qualified34 unchanged mode/type/blob entries against my completed8d64 scope; fully reviewed15 fresh objects (1283+/15−), including complete affected source/tests and decoded archive contents. All seven original AC and originating spec/issues/handoff/oracle plus adopted expiry/renewal clarifications covered. Scope: `/tmp/lerna-04-ticket-02-spec-3f44-scope.json`.

**Counts: (a) missing/partial0; (b) scope creep0; (c) implemented wrong0. Worst: none.**

My8d64 P2 is closed. `domain/content/management.go:959–969` hands completed historical propagation to `natural_expiry` using unchanged original ExpiryDue/ExpiryDeadline/watermark even when already due; an expired original natural budget retains residual. Historical qualification remains tied to installation Due, and subsequent natural checks use current exact policy/caps without overwriting historical pending. `content_processing_deadline_test.go:242–292` exercises real public normal/late cases and fixed maintenance identity. This satisfies handoff:59–61 and adopted renewal clarification:18,25. The two earlier57ea P2 closures remain qualified.

The new actual0001 expired-ancestor archive is independently qualified: all six SHA256 entries match; both original objects equal `alpha\n`; all79 COPY rows equal restore INSERT literals, all78 JSON bytea cells decode, and DDL differs only by documented mechanical normalization. Original source ValidUntil `2026-10-04T16:00:30.057895Z`,2099 body caps/receipts, three publication histories, exact direct sources and failed staging remain intact. Producer explicitly aggregates Store/Object and observation-file Sync/Close errors. New public upgrade test verifies partial backfill/reopen, unchanged original due/deadline/receipts, pending published/failed descendant holders, independent original bytes and a legitimate new current-chain control. The first archive’s original unacknowledged deferred closes remain a stated historical limitation.

No reviewer tests/build/DB/native operations or opposite-axis input. Producer/dump provenance and parent’s focused normal9.315/.024 exit0/group-absence are external execution claims, separate from this static qualification. Final whole normal/race/check/audit/CI remain pending at this cutoff. All seven AC remain unchecked; no acceptance, production Grant, physical erasure or whole04 completion is claimed.

Standards: 0 hard, 1 judgement P3; Spec: a0/b0/c0, worst none. Axes retained separately.
