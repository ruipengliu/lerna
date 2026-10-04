# Standards — fixed 75202ee

BASE `f05b2f1068958ba6b63e6c1dc58d6cd1684446cc` → HEAD `75202ee0077b5bf12b432b5017ad97a1dfcab681`: actual 8 commits/49 paths (7162+/62−). Fully read the single changed test file and its 50+/43− increment. Verified all 48 carried range paths, all 909 other whole-tree entries, standards sources and 73 protected existing objects equal by mode/type/blob. Product objects and all three archive sets (21 objects) are byte-identical to3f44; its full hash/79-row restore/provenance qualification therefore carries. Scope: `/tmp/lerna-04-ticket-02-standards-752-scope.json`.

The added matrix at `conformance/component/content_processing_deadline_test.go:243` runs both Manager.Step and Content.Step with normal and late controls. Each uses a finite15s context, preserves exact policy identity/original natural due/deadline/watermark, checks late pending holders and five affected actions through management observations, and independently reads normal bytes. The test does not substitute private-table counts or call counts for business facts. No new documented-standard breach or smell from this delta.

All own hard findings remain CLOSED: original processing deadline, original three paginated Close locations, and subsequent UnindexedVersions/PendingChanges early-error Close causes. **Current hard: 0; worst none.**

**Current judgement: 1 possible smell; worst P3.**

- Unchanged **possible Duplicated Code** at `adapters/postgres/content/management.go:71,299,395,439`: four typed page readers repeat “`if len(…) == limit { next = last; break }`”, scan/decode/cursor and joined-close handling. Fowler suggests a minimal shared consumption shape; this remains optional maintenance judgement, not a hard requirement.

All twelve assessed/carried: Mysterious Name, Duplicated Code, Feature Envy, Data Clumps, Primitive Obsession, Repeated Switches, Shotgun Surgery, Divergent Change, Speculative Generality, Message Chains, Middle Man, Refused Bequest. Repository closed cases, consumer ports/decorators and frozen provenance override mechanical flags; tooling-enforced format/vet/types/whitespace excluded.

Read-only; no reviewer native execution. Root-reported3f normal104.650 and owner-reported752 focused2.667/registry0.028/Local4tests0.065 are scope-specific supplied evidence. Final race/check/audit remain pending; seven AC unresolved, whole04 incomplete.
