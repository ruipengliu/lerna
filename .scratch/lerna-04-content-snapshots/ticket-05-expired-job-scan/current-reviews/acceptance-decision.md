# Current05 narrow acceptance decision — STATIC

Decision: **KEEP the two scenario-local duty structs; no new necessary product fix in this reviewed delta.** This permits continuing the existing resource audit and seven-AC verification, and does not itself accept the ticket.

The duplicate four fields in `content_delegated_cleanup_backlog_test.go` and `content_manager_expired_backlog_test.go` hold observations, not shared behavior, authority, or lifecycle transitions. Their scenarios differ: complete delegated-subject isolation versus expired cleanup backlog preceding policy propagation. Extracting a shared test type would remove little code while coupling independently understandable scenarios. The deletion test does not establish useful depth or leverage here. Standards’ optional P3 remains a maintenance observation, not a prerequisite.

I independently read the 14-path fixed delta and the relevant complete current sources. `ScanContentPhase` selects the explicit publication or policy-propagation phase before LIMIT; Service retains two bounded pages of 64, while Manager selects its own phase. `ScanBodyCleanup` performs complete-subject and primary-holder selection before LIMIT without completing, renewing, or erasing expired duties. Missing or malformed facts remain error candidates. The original consumer still locks and qualifies the selected record, seal, current clock, cap and deadline; physical erasure still requires all original holder acknowledgments.

The PostgreSQL qualification is deliberately narrow: raw JSON field/type/integer/duplicate-key checks, exact identity and shadow-column consistency, and a fixed canonical stored-seal byte recognizer. Semantic JSONB equality alone is not treated as the original Go marshal-byte proof. The recognizer is confined to this persisted seal representation; unsupported or invalid rows are not silently classified as expired or outside the caller’s scope. The selected-row Go decoding and error/close-cause handling remain visible. No generic schema, scheduler, registry, or new public contract is warranted.

Execution qualification is separate. The two current reviews report no hard findings. Root’s machine audit records 27 successful bounded checks with wait/group-absence evidence and frozen-source qualification; I did not execute them. Existing real business-red history remains immutable. That audit does not turn retained per-resource close uncertainty into successful cleanup. Resource reconciliation and root’s seven-AC acceptance are still pending. Whole04 and profile 1.2 remain unadvertised; this decision adds no whole-slice CI prerequisite.

## Exact checkpoint and provenance

- Worktree: `/tmp/lerna-worktrees/content-snapshots-05`.
- Baseline: `1a4e1d2d704261a59491dfc6e0c5d26b80ec5107`.
- Reviewed source: `8db74e3dfd2677adb37fbdd13bd93e76b61b5eb7`; 14 paths, 1,401 additions / 319 deletions.
- `/tmp/lerna-05-current-standards-review.md` SHA-256: `c6e621c163bef4d2a88719d726e931a0d7e82be190d29d9bd5c9c83f8f7b8f30`.
- `/tmp/lerna-05-current-spec-review.md` SHA-256: `c0af2ca1c32ead970e1a96bd4ed817965929ca99907682dac5ae99c74249335c`.
- `/tmp/lerna-root-current05-audit.json` SHA-256: `fa2090bc6a23688379dba47e49754b3af0fed55f335c3b21a934a291046e629e`.

Only this outside-repository document was written. No native execution, database access, resource cleanup, or product change was performed.
