# Two-axis review — fixed 752

## Standards

# Standards — fixed 75202ee

BASE `f05b2f1068958ba6b63e6c1dc58d6cd1684446cc` → HEAD `75202ee0077b5bf12b432b5017ad97a1dfcab681`: actual 8 commits/49 paths (7162+/62−). Fully read the single changed test file and its 50+/43− increment. Verified all 48 carried range paths, all 909 other whole-tree entries, standards sources and 73 protected existing objects equal by mode/type/blob. Product objects and all three archive sets (21 objects) are byte-identical to3f44; its full hash/79-row restore/provenance qualification therefore carries. Scope: `/tmp/lerna-04-ticket-02-standards-752-scope.json`.

The added matrix at `conformance/component/content_processing_deadline_test.go:243` runs both Manager.Step and Content.Step with normal and late controls. Each uses a finite15s context, preserves exact policy identity/original natural due/deadline/watermark, checks late pending holders and five affected actions through management observations, and independently reads normal bytes. The test does not substitute private-table counts or call counts for business facts. No new documented-standard breach or smell from this delta.

All own hard findings remain CLOSED: original processing deadline, original three paginated Close locations, and subsequent UnindexedVersions/PendingChanges early-error Close causes. **Current hard: 0; worst none.**

**Current judgement: 1 possible smell; worst P3.**

- Unchanged **possible Duplicated Code** at `adapters/postgres/content/management.go:71,299,395,439`: four typed page readers repeat “`if len(…) == limit { next = last; break }`”, scan/decode/cursor and joined-close handling. Fowler suggests a minimal shared consumption shape; this remains optional maintenance judgement, not a hard requirement.

All twelve assessed/carried: Mysterious Name, Duplicated Code, Feature Envy, Data Clumps, Primitive Obsession, Repeated Switches, Shotgun Surgery, Divergent Change, Speculative Generality, Message Chains, Middle Man, Refused Bequest. Repository closed cases, consumer ports/decorators and frozen provenance override mechanical flags; tooling-enforced format/vet/types/whitespace excluded.

Read-only; no reviewer native execution. Root-reported3f normal104.650 and owner-reported752 focused2.667/registry0.028/Local4tests0.065 are scope-specific supplied evidence. Final race/check/audit remain pending; seven AC unresolved, whole04 incomplete.

## Spec

Independent SPEC followup: base `f05b2f1068958ba6b63e6c1dc58d6cd1684446cc` → fixed `75202ee0077b5bf12b432b5017ad97a1dfcab681`. Actual merge-base equals base: **eight commits,49paths,7162+/62−**. Complete prior3f44 seven-AC scope is qualified by actual mode/type/blob equality:48 unchanged reviewed paths; the sole fresh test object is fully read. Independently compared entire trees:909 other entries equal, including all product code, SQL and archives. Scope: `/tmp/lerna-04-ticket-02-spec-752-scope.json`.

**(a) missing/partial0; (b) scope creep0; (c) implemented wrong0. Worst:none.**

The50+/43− test delta extends the original natural-expiry handoff case to both actual public consumers, `Manager.Step` and `Content.Step`, with separate future/late cases. Each uses real PG/object fixture setup and management observations; late cases require the known holder’s pending responsibility, all five affected actions and unchanged original natural Due/Deadline/watermark. Future cases require not_required and successful exact-body Get. Neither internal call counts nor publication work inference replace those observations. This remains within handoff:59–61 and adopted renewal clarification:18,25; my original P2 and both earlier P2 closures remain qualified.

No reviewer tests/build/native/DB/environment operations or opposite-axis input. Parent’s focused2.667/registry.028/Localfull.065 exit0/group-absence is external execution evidence; final race/check/audit/CI remain pending at this cutoff. All seven AC remain unchecked; no acceptance or whole04 completion is claimed.

Standards0hard/1judgementP3; Speca0b0c0/worstnone. Axes retained separately; pending runtime qualification not replaced by source review.
