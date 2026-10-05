# Standards axis: frozen ticket 05 candidate

## Scope and independence

Baseline: `1373112472761d32c22ed6d7603d806d8ef9a3c7`.
Candidate: `1a4e1d2d704261a59491dfc6e0c5d26b80ec5107`.
Pinned worktree: `/tmp/lerna-worktrees/content-snapshots-05`.
Exact comparison: `git diff 1373112472761d32c22ed6d7603d806d8ef9a3c7...1a4e1d2d704261a59491dfc6e0c5d26b80ec5107`.
Both refs resolve. Diff is nonempty: 146 paths, 21823 additions, 233 removals.
All source reads use candidate-qualified `git show`; no moving HEAD input.
Read-only independent Standards review. No originating issue/spec or other reviewers' conclusions were used. Implementation handoff/evidence were considered only as changed documentation, not as the specification. No ticket acceptance decision is made.

No native execution, Go commands, tests, PostgreSQL operations, process cleanup, repository edits, merges, or CI actions were performed. Only Git source reads, static inspection, Python hash/provenance comparisons, and this outside-repository evidence write were performed. The owner's final 17 checks are pending evidence at this review boundary; nothing in this report claims they ran or passed. All seven acceptance criteria remain claimed, not accepted.

## Standards sources

Root `AGENTS.md` (only applicable instructions supplied/discovered), `CONTEXT.md`, `docs/agents/domain.md`, `docs/adr/README.md`, `docs/architecture/README.md`, ADR-0004/0005/0006/0007, and architecture `governance.md` / `data-model.md`.

Repository rules override Fowler heuristics. Tool-enforced formatting/vet checks are excluded and left to the runtime owner.

## Findings

### S1 — documented-standard breach, documentation only (P3)

File: `.scratch/lerna-04-content-snapshots/ticket-05-api-handoff.md`, added line 92 (also stale pending statements at 36–37, 160–168, 186–188, 197–198, 210–211, 249–250, 259–260 superseded by later additions).
Rule: `AGENTS.md:173` requires distinguishing facts and pending verification; `:174` requires related information to be grouped with key premises/limitations adjacent; `:208` requires text consistency.
Quote: `目前未实施该回填资格` (the backfill qualification is not implemented yet).
The same current API handoff at lines 171–188 documents `LifecycleConfig.LegacyPrimary`, its actual dedicated whole-Record CAS, and successful normal/race qualification; frozen production code also implements `BindLegacyPrimary`. This is a current implementation-status contradiction, not evidence of missing production code. Likewise line 250 still calls targeted deadline qualification pending while lines 253–260 report it executed. Unlike the chronological evidence ledger, this file is titled a current internal API handoff and does not label the superseded statements as historical.
Action: consolidate the handoff into current API/status sections; move historical progression to the evidence ledger or explicitly label old status statements. Keep final suites/CI and seven-AC acceptance pending until independently established.

### S2 — possible Duplicated Code, optional judgement call

File: `domain/content/legacy_binding.go:163–179` and `:217–233`, both added hunks.
Quote in both branches: `checked, err := l.store.BindLegacyPrimary(ctx, tx, *record, *q)` followed by `l.manager.current`, `l.store.Now`, `if !now.Before(q.ValidUntil) { return refusal("expired") }`, then `observe(checked)`.
The first-transaction already-bound branch and second-transaction concurrent-replay branch duplicate the same binding-confirmation/time/observation sequence. This real shared shape is a reasonable local helper extraction so future final-deadline qualifications cannot drift between replay paths. Preserve both transaction boundaries and the initial unbound write/attempt-registration path. This is not a documented hard violation and does not invalidate current behavior.

No implementation-level documented-standard breach identified in reviewed hunks.

## Protocol-focused source evidence

- Dedicated legacy PG binding: domain binder accepts a finite immutable cloned host qualification, exact original full Ref/Subject/Purpose/key/attempt set, original physical binding and qualification lifetime. Two DB transactions surround bounded original-media Read/hash/length. Replays preserve provenance; stale whole Record is rejected. Adapter checks exact namespace/owner, locks current original row, compares full Record, and uses the actual previously stored JSON operand plus staging/identity/publication/revision/unbound/seal/gone columns in CAS. Ordinary SaveVersion preserves binding/provenance and cannot create that upgrade transition. Source evidence does not prove any original supervisor actually stopped an arbitrary old writer.
- Permanent original key fence: admission records immutable holder Binding from opened directory FD identity. All protocol Put/Read/Erase/Observe acquire permanent original `.lock` inode with flock, honor sealed/pending markers, and use finite contexts. Lock files are never unlinked. Body Close uncertainty remains sticky and retains the lock FD; Close does not infer external effects did not happen. Fence and separate exact-body Observe must both confirm before lifecycle ACK. Unknown same-key temporary files remain residual instead of guessed deletion authority. Non-fencing historical binaries require separately proven stop/upgrade.
- Original duty/cap/basis/CAS/ACK: cleanup selection releases change lock before page qualification; current full saving policies and structural transitive ancestry are locked/validated before original responsibility CAS. Nil/failed policy reads never become save=false. Strict canonical nonzero cap parsing precedes destructive expired-cap qualification. Targeted admission cleanup validates exact original key/target/shape/due/deadline; no renewal of original budgets. All holders, original attempts, and independently registered copies are retained; any row mismatch rolls back the whole page. BodyGone requires authoritative staging+primary ACK; global cleanup requires all holders. Policy erased ACK uses original key/full tuple/deadline and commits with completion only after fresh manager/clock/Claim checks. Natural registration retains terminal erasure history and historical holder facts.
- Production SQL uses explicit parameter binding and tenant/owner-qualified access. New migration is additive owner 0003; original published 0001 and 0002 are unchanged. Object I/O remains outside DB transactions. Domain code depends on small consumer-declared ports, not concrete adapters. Exported internal lifecycle entry points are not treated as public wire API; frozen 1.2 contract has no added delete method.
- New tests use finite caller/effect/gate/join bounds and real normal controls. Fault decorators are explicitly mechanical observation/reply gates around actual underlying operations and also assert public outcome/independent exact physical facts; invocation counts are synchronization/proof-of-fault-execution aids, not the sole business acceptance oracle. Whole-page CAS, actual lock waits, initial qualification cutoff, reply loss, stopped-original legacy namespace, all attempt pages, independent residual ownership, full metadata ancestry, late publication, offline copy and native removal failure cases are represented in source. Their runtime results are unverified by this reviewer.
- Narrow copied-archive changes: legacy policy backfill no longer infers an original physical holder from an archive copied to another root. It requires dependency_unavailable while retaining exact independent archive bodies and publication/receipt/policy-page checks. Genuine stopped-original binding has separate normal tests. Existing filesystem assertions now map exact ContentRef keys to literal bytes/absence and permit only known-key empty regular `.lock` entries; arbitrary metadata, every attempt, unknown file and extra body still fail. These controls have source coverage, not fresh execution coverage.
- Historical one-off build script has personal paths but README explicitly excludes it from fixture/CI/build entry points. Current fixture materializes pinned sources and independently checked driver under owned fresh directories; no personal external producer path is required. No false breach is inferred from this inert historical execution artifact.
- Handoff/evidence expressly retain compile failures, native failures, unknown Close resources, partial source-only qualification, and pending final suites/acceptance. Claims inside these files are historical reported evidence, not observations newly independently executed here.

## Fowler baseline disposition

- Mysterious Name: no actionable finding; local abbreviated values have nearby qualification/context and domain names are explicit.
- Duplicated Code: S2 optional replay shape extraction.
- Feature Envy: orchestration accesses records intentionally as consumer-owned domain rules; no actionable misplaced behavior.
- Data Clumps: identity, seal, copy and legacy qualification already use specific aggregate types; no actionable unbundled group.
- Primitive Obsession: string states/actions match existing persisted internal conventions and fixed wire vocabulary; no mandatory type change inferred.
- Repeated Switches: holder-kind checks serve distinct selection/effect phases; no justified polymorphic abstraction requested.
- Shotgun Surgery: body seal gates touch each actual admission/use/finalization boundary; existing filesystem-oracle edits are consolidated by the new exact-object helper. Necessary breadth is not a smell by itself.
- Divergent Change: lifecycle file owns closely related sealing, holder erasure and its qualification; no unrelated responsibility split found.
- Speculative Generality: added ports have present consumers/replacement seams. No unneeded framework/hook identified from this Standards axis.
- Message Chains: no actionable hidden traversal chain.
- Middle Man: wrappers are narrowly intentional fault/synchronization adapters or named domain entrances; no unnecessary production delegation layer identified.
- Refused Bequest: embedded test-port decorators deliberately intercept small seams while forwarding the rest; no inherited contract rejection found.

## Frozen asset integrity (static, not build/run evidence)

Manifest SHA256: `589a151af608daf9accc510c7905d7635528fd3fb87cac64a2cdf8f7070d9c77`.
Original production commit: `1a7d910238eb74cddc712d92b0ba4014a72ff507`.
All 75 unique entries total exactly 466704 bytes. Each artifact is exact `path + .txt`, matches declared length/SHA256, and matches the original Git blob byte for byte. No unmanifested .txt asset exists in the frozen directory.
Separate supervisor driver SHA256: `3d683e3b66e662b21102a42e0f418d49c1776e52cd4f421bfb3f88da718ee8be`, matching fixture literal.
0001 SHA256: `00363b79dafb6eb1f373be9ef08e915fe23cc3db0fa55345e8ae6c051ce0c1ed`, unchanged from baseline.
0002 SHA256: `99519565ff1146d7cfc468413b449538c6ba71335e86edc8fd447a3bbff922a8`, unchanged from baseline.
The exact historical inert files are not classified as newly authored production code, nor is their old published SQL expected to change.

## Commit list

Captured with `git log 1373112472761d32c22ed6d7603d806d8ef9a3c7..1a4e1d2d704261a59491dfc6e0c5d26b80ec5107 --oneline`:

```
1a4e1d2 test(content): observe exact bodies beside permanent key locks
4735abc test(content): refuse unknown inherited cleanup qualification
c01b4c3 test(content): qualify original admission cleanup lock deadline
301be6d fix(content): qualify original inherited cap admission cleanup
1a52d24 Qualify whole original cleanup page rollback under real CAS contention
331154b Qualify initial legacy cutoff rollback after actual attempt writes
ca9ac81 Qualify legacy confirmed commit reply loss and exact recovery
2402fd7 Qualify stale legacy holder binding rejection and original replay
35df18e Qualify original cleanup deadline across real policy lock waits
bb6e1f2 Bind stopped original legacy Content holders with exact scope evidence
864399c test(content): qualify original attempt pages and unknown residual ownership
1af6593 test(content): resume all original policy and physical holder pages
d849b7a fix(content): consume definite expired accepted retention caps
e5b38a5 test(content): qualify current metadata across derived ancestry
2a28ca8 fix(content): preserve unconfirmed erasure reply and metadata time precision
ff33169 content: preserve terminal erasure history during natural policy registration
0586d51 content: preserve erased holder fact after late successful publication I/O
51e331e content: stop new primary-holder claims after authoritative erasure
641020d content: bind current policy seal to original all-holder cleanup acknowledgement
c2716c0 content: qualify restored saving basis before cancelling old cleanup trigger
3aa948d content: select unpublished orphan under the original version lock
e84c2cd test(content): qualify late process rejection without washing killed holder ownership
815f24e test(content): qualify real cross-process put before key erasure
05b191f test(content): qualify native secondary deletion failure and original duty
25f3f35 content: retain independent secondary copy duties through body cleanup
2b73be2 test(content): qualify original root before first body seal
70d746e content: bind original publication duties to opened physical holder
9bcaeba content: confirm primary erasure before metadata-only gone
8986829 content: persist trusted body seals and original holder duties
1f52ccd content: fence exact object keys across holder reopen
```

## Complete changed-path coverage

Each row is from the exact three-dot diff. `S` = executable/current source or changed-test hunk inspected statically; `D` = documentation/evidence-boundary/structure audit (runtime claims not independently verified); `H` = inert historical build artifact, read as history; `M` = frozen README/manifest audited; `F` = raw inert asset verified against manifest and original Git blob. All 146 paths are covered; no paths omitted.

| Path | +/- | Coverage |
|---|---:|---|
| `.scratch/lerna-04-content-snapshots/ticket-05-api-handoff.md` | 276/0 | D |
| `.scratch/lerna-04-content-snapshots/ticket-05-evidence.md` | 1014/0 | D |
| `.scratch/lerna-04-content-snapshots/ticket-05-legacy-first-red-build.sh` | 21/0 | H |
| `adapters/objectstore/local/erasure_linux.go` | 314/0 | S |
| `adapters/objectstore/local/erasure_process_test.go` | 402/0 | S |
| `adapters/objectstore/local/erasure_test.go` | 67/0 | S |
| `adapters/objectstore/local/store_linux.go` | 79/6 | S |
| `adapters/postgres/content/facts.go` | 31/4 | S |
| `adapters/postgres/content/legacy_binding.go` | 76/0 | S |
| `adapters/postgres/content/lifecycle.go` | 320/0 | S |
| `adapters/postgres/content/management.go` | 25/2 | S |
| `adapters/postgres/content/migrate.go` | 4/1 | S |
| `adapters/postgres/content/migrations/0003_body_cleanup.sql` | 42/0 | S |
| `adapters/postgres/content/policy_cleanup.go` | 196/0 | S |
| `adapters/postgres/content/secondary.go` | 149/0 | S |
| `conformance/component/content_accepted_cap_cleanup_test.go` | 135/0 | S |
| `conformance/component/content_admission_cleanup_deadline_test.go` | 300/0 | S |
| `conformance/component/content_association_test.go` | 2/16 | S |
| `conformance/component/content_body_erasure_test.go` | 91/0 | S |
| `conformance/component/content_body_seal_test.go` | 78/0 | S |
| `conformance/component/content_bounds_test.go` | 1/10 | S |
| `conformance/component/content_cleanup_attempt_pages_test.go` | 196/0 | S |
| `conformance/component/content_cleanup_deadline_wait_test.go` | 227/0 | S |
| `conformance/component/content_cleanup_page_cas_test.go` | 314/0 | S |
| `conformance/component/content_cleanup_pages_test.go` | 393/0 | S |
| `conformance/component/content_clock_test.go` | 3/10 | S |
| `conformance/component/content_closure_test.go` | 1/12 | S |
| `conformance/component/content_direct_read_test.go` | 1/12 | S |
| `conformance/component/content_erasure_reply_loss_test.go` | 151/0 | S |
| `conformance/component/content_faults_test.go` | 7/9 | S |
| `conformance/component/content_holder_binding_test.go` | 131/0 | S |
| `conformance/component/content_holder_fact_test.go` | 215/0 | S |
| `conformance/component/content_identity_test.go` | 1/20 | S |
| `conformance/component/content_inherited_cap_cleanup_test.go` | 333/0 | S |
| `conformance/component/content_inherited_expiry_test.go` | 1/12 | S |
| `conformance/component/content_legacy_binding_cutoff_test.go` | 131/0 | S |
| `conformance/component/content_legacy_binding_race_test.go` | 169/0 | S |
| `conformance/component/content_legacy_binding_reply_test.go` | 139/0 | S |
| `conformance/component/content_legacy_binding_test.go` | 211/0 | S |
| `conformance/component/content_legacy_test.go` | 21/20 | S |
| `conformance/component/content_metadata_scope_test.go` | 164/0 | S |
| `conformance/component/content_objects_oracle_test.go` | 63/0 | S |
| `conformance/component/content_orphan_test.go` | 174/0 | S |
| `conformance/component/content_policy_ack_history_test.go` | 88/0 | S |
| `conformance/component/content_policy_cleanup_test.go` | 210/0 | S |
| `conformance/component/content_policy_test.go` | 1/5 | S |
| `conformance/component/content_post_schedule_test.go` | 4/15 | S |
| `conformance/component/content_publication_test.go` | 2/20 | S |
| `conformance/component/content_read_gate_test.go` | 1/5 | S |
| `conformance/component/content_retry_test.go` | 2/9 | S |
| `conformance/component/content_secondary_native_failure_test.go` | 159/0 | S |
| `conformance/component/content_secondary_test.go` | 159/0 | S |
| `conformance/component/content_target_policy_test.go` | 4/16 | S |
| `conformance/internal/contentfixture/frozen_content_build.go` | 260/0 | S |
| `conformance/internal/contentfixture/legacy_authorization.go` | 30/0 | S |
| `conformance/internal/contentfixture/object_fault.go` | 53/0 | S |
| `conformance/internal/contentfixture/stopped_legacy.go` | 293/0 | S |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/README.md` | 22/0 | M |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/adapters/objectstore/local/store_linux.go.txt` | 326/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/adapters/postgres/commands.go.txt` | 107/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/adapters/postgres/content/facts.go.txt` | 200/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/adapters/postgres/content/migrate.go.txt` | 81/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/adapters/postgres/content/migrations/0001_content.sql.txt` | 56/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/adapters/postgres/content/policy.go.txt` | 163/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/adapters/postgres/content/store.go.txt` | 24/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/adapters/postgres/demo.go.txt` | 74/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/adapters/postgres/internal/pgstore/core.go.txt` | 197/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/adapters/postgres/internal/pgstore/jobs.go.txt` | 179/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/adapters/postgres/internal/pgstore/pool.go.txt` | 402/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/adapters/postgres/migrate.go.txt` | 124/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/adapters/postgres/migrations/host/0001_admission.sql.txt` | 21/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/adapters/postgres/migrations/host/0002_claims.sql.txt` | 26/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/adapters/postgres/migrations/host/0003_retention.sql.txt` | 2/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/adapters/postgres/migrations/host/0004_waits.sql.txt` | 19/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/adapters/postgres/migrations/host/0005_pools.sql.txt` | 10/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/adapters/postgres/pool.go.txt` | 176/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/adapters/postgres/retention.go.txt` | 64/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/adapters/postgres/schedule.go.txt` | 199/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/adapters/postgres/store.go.txt` | 55/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/adapters/postgres/work.go.txt` | 55/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/contract/codec.go.txt` | 188/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/contract/commands.go.txt` | 61/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/contract/digest.go.txt` | 137/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/contract/gen/go/v1_1/values.go.txt` | 1130/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/contract/gen/go/v1_2/values.go.txt` | 687/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/contract/gen/go/values.go.txt` | 517/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/contract/json.go.txt` | 147/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/contract/negotiation.go.txt` | 29/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/contract/query.go.txt` | 93/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/contract/readfacts.go.txt` | 53/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/contract/receipts.go.txt` | 130/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/contract/v1_1/codec.go.txt` | 191/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/contract/v1_1/commands.go.txt` | 64/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/contract/v1_1/decision.go.txt` | 269/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/contract/v1_1/digest.go.txt` | 137/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/contract/v1_1/json.go.txt` | 147/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/contract/v1_1/negotiation.go.txt` | 33/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/contract/v1_1/query.go.txt` | 93/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/contract/v1_1/readfacts.go.txt` | 53/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/contract/v1_1/receipts.go.txt` | 130/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/contract/v1_1/values.go.txt` | 186/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/contract/v1_2/codec.go.txt` | 191/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/contract/v1_2/commands.go.txt` | 64/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/contract/v1_2/content.go.txt` | 163/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/contract/v1_2/digest.go.txt` | 137/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/contract/v1_2/json.go.txt` | 147/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/contract/v1_2/query.go.txt` | 93/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/contract/v1_2/readfacts.go.txt` | 53/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/contract/v1_2/receipts.go.txt` | 79/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/contract/v1_2/values.go.txt` | 130/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/contract/values.go.txt` | 102/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/domain/content/identity.go.txt` | 52/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/domain/content/legacy.go.txt` | 88/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/domain/content/ports.go.txt` | 87/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/domain/content/service.go.txt` | 792/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/go.mod.txt` | 17/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/go.sum.txt` | 32/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/internal/durableworkdemo/maintenance.go.txt` | 140/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/internal/durableworkdemo/permissions.go.txt` | 66/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/internal/durableworkdemo/pool.go.txt` | 109/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/internal/durableworkdemo/pool_worker.go.txt` | 197/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/internal/durableworkdemo/record.go.txt` | 64/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/internal/durableworkdemo/record.schema.json.txt` | 40/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/internal/durableworkdemo/retention.go.txt` | 119/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/internal/durableworkdemo/schedule.go.txt` | 217/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/internal/durableworkdemo/service.go.txt` | 176/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/internal/durableworkdemo/step.go.txt` | 325/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/internal/durableworkdemo/work.go.txt` | 284/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/internal/durableworkdemo/worker_permissions.go.txt` | 40/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/provenance.json` | 474/0 | M |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/runtime/admission.go.txt` | 111/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/runtime/schedule.go.txt` | 44/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/runtime/work.go.txt` | 34/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-frozen-1a7/runtime/workpool/pool.go.txt` | 207/0 | F |
| `conformance/internal/contentfixture/testdata/ticket05-producer/main.go.txt` | 206/0 | S |
| `conformance/internal/contentfixture/unregistered_attempt.go` | 87/0 | S |
| `domain/content/closure.go` | 6/0 | S |
| `domain/content/erasure.go` | 36/0 | S |
| `domain/content/legacy_binding.go` | 263/0 | S |
| `domain/content/lifecycle.go` | 639/0 | S |
| `domain/content/orphan.go` | 15/0 | S |
| `domain/content/policy_cleanup.go` | 271/0 | S |
| `domain/content/policy_cleanup_internal_test.go` | 24/0 | S |
| `domain/content/ports.go` | 33/23 | S |
| `domain/content/secondary.go` | 226/0 | S |
| `domain/content/service.go` | 151/6 | S |

## Per-asset proof

| Original path | Bytes | SHA256 | Original-byte match |
|---|---:|---|---|
| `adapters/objectstore/local/store_linux.go` | 8553 | `24e243c111731b8a52cb56ba5ff0f522938ce17dedf394a665c5790b6185d92c` | yes |
| `adapters/postgres/commands.go` | 4222 | `1aa6421a705adb00d91a67e098f1af41cd26c54e7cb2edea0c95dd77306cb948` | yes |
| `adapters/postgres/content/facts.go` | 8896 | `d1152083f448b82a8e10d0f7e458b77f3d653e24c87ef38a9ade8409167c879f` | yes |
| `adapters/postgres/content/migrate.go` | 2428 | `8a5512c1f145875a0551da22197a5617241f1480197d55c5e5eee92b06b709ff` | yes |
| `adapters/postgres/content/migrations/0001_content.sql` | 3682 | `00363b79dafb6eb1f373be9ef08e915fe23cc3db0fa55345e8ae6c051ce0c1ed` | yes |
| `adapters/postgres/content/policy.go` | 6326 | `f145ce99c45c7cde7542bd93faf90eabc98ea90a3c34ee14911b0a78f47c6f08` | yes |
| `adapters/postgres/content/store.go` | 777 | `e1da8da60b48d85c5d668ac88a0d33da3c824603b93bc01e5dd821def34fa69d` | yes |
| `adapters/postgres/demo.go` | 3845 | `c9f28c385a311765313737b89168cf5456bb835e74953048e47a3736c1a880aa` | yes |
| `adapters/postgres/internal/pgstore/core.go` | 6886 | `6a7a494bcb33bb03e0b3844b33829f2bd4a3954326a3f05d4737e5ba75c19499` | yes |
| `adapters/postgres/internal/pgstore/jobs.go` | 9739 | `feb70b9613269f6b88e87e8cfdd4cd255228edb50e3f6ecd8c93c321efed91e8` | yes |
| `adapters/postgres/internal/pgstore/pool.go` | 15154 | `ca3e395b06d2cda0d995f325b27663782e5c06359d3dde7495ff1574531099f1` | yes |
| `adapters/postgres/migrate.go` | 3704 | `a88140012c2cec4ffb2fb00827557db5d2a691e5f63bb0756f2264c006751232` | yes |
| `adapters/postgres/migrations/host/0001_admission.sql` | 1158 | `f8d04d373b039a425b4f6d0a7b7dd4410971c00faf91cdba3f68a9204579127e` | yes |
| `adapters/postgres/migrations/host/0002_claims.sql` | 1840 | `cdb7dea9f55ee8ac9201a943cecf8096108b48bc208409e1372bbf17295b2297` | yes |
| `adapters/postgres/migrations/host/0003_retention.sql` | 214 | `ec7e5d35deb0c19a1fccc5cd965fba8367b02ab3df3b46d97da8782a0b5f7249` | yes |
| `adapters/postgres/migrations/host/0004_waits.sql` | 1215 | `5cdc0cb11fa3aec15a496c8f19419a83929299db513d98d954f8b0281a7428d5` | yes |
| `adapters/postgres/migrations/host/0005_pools.sql` | 1152 | `5f9356a7b4f58e908069e6038e599de0fd4d31c8f4f025124672eed8e72319e1` | yes |
| `adapters/postgres/pool.go` | 8135 | `4b88ee97ec80a7b7f51c0c45a1199192106ff0d84b0421fa926404cd705a5609` | yes |
| `adapters/postgres/retention.go` | 2839 | `46b67e150b1fe4ee3442a2a58f771065526a56f7744f995c12b88022a4e06225` | yes |
| `adapters/postgres/schedule.go` | 9011 | `1be8cebaae81c7c12614f3bfc3263c9f96922b47276dec65f34a6da9e5fa5766` | yes |
| `adapters/postgres/store.go` | 1840 | `1be157b5ab0938e04915eebdbcaa4d957f904db88c14d594a2efd155e420c3f0` | yes |
| `adapters/postgres/work.go` | 2287 | `028c38fa8f8fea6a6d8dd133b6a5a3a7e9ee64c63e8acee78c171356cf45f8c8` | yes |
| `contract/codec.go` | 5505 | `59ac83d1dc0340362777dff96da2ba4f968de738aec7584d19030cf5bd00fa86` | yes |
| `contract/commands.go` | 2067 | `933469c3665bd3525643f2f0a3313f686da03a8941fb453a76b72d0fde6919a7` | yes |
| `contract/digest.go` | 3597 | `a8db4eb43c1573f745069b1985d76895ce23a9315d5d6b32e28895c4568ccf59` | yes |
| `contract/gen/go/v1_1/values.go` | 76985 | `d00644ae3931e6d5a6ad2469fb6c3d3c1297134426bc5294a5ca350311ca5da6` | yes |
| `contract/gen/go/v1_2/values.go` | 39405 | `dcb5798595cf59370a3dfaf5faca0f5192f81f929b2c9521fb3b4e5ba6c85f72` | yes |
| `contract/gen/go/values.go` | 29758 | `e5e34d5e52a71ad126a40bbfada9aabb92572f79b2d925b09f6959a0546605be` | yes |
| `contract/json.go` | 3651 | `db7ef82bcde147126dc4b116c94896823687fc83f16abc10da3ee70f83cf262f` | yes |
| `contract/negotiation.go` | 1194 | `679c406e705a541cebc1d9a2bbeb35cb0bd629a75b47eb7caa42768102d0e5a8` | yes |
| `contract/query.go` | 3430 | `5b9bf77e09fea8ed49e296b441b9e94ea2642fe4930bde925a203a3d1294d7c3` | yes |
| `contract/readfacts.go` | 1960 | `c2267bfab9413a8b0983da29a931a8b9e723cf845d6ec782413ef632d9f8b003` | yes |
| `contract/receipts.go` | 4113 | `da354c4ebb35146d6059b6915ae355b347d6ffe5a0da733953d6f53543c8a93a` | yes |
| `contract/v1_1/codec.go` | 5578 | `53ba6cf9a5bb9d70d77632cc154be07ef5e5171c0832fc50488fbdd210981167` | yes |
| `contract/v1_1/commands.go` | 2188 | `442ea71f7cca834617c5b09b24ef8bb4fca6672da881a519d183b0fcf3fe8aea` | yes |
| `contract/v1_1/decision.go` | 8410 | `c43d045f03dacb78b1afbd9c1d63e7887293f0a4c9cc83072837b8f16d7e09c0` | yes |
| `contract/v1_1/digest.go` | 3593 | `b4a140c64f9927329f24154bee8d738f600003024be9a589ae43e9848df00a33` | yes |
| `contract/v1_1/json.go` | 3647 | `9c6c14729d3ea6c9ea464ef1a234aa3eea106f1c0478e8b1b9a2ee78ffb8f06a` | yes |
| `contract/v1_1/negotiation.go` | 1425 | `5f24d785e298d357a977d8295fb7e5a620cad08a9bc86ae7db3b9c168958f601` | yes |
| `contract/v1_1/query.go` | 3426 | `5168b38e54045a4b980ca912b29455b47181d07eec663f0cf6b96f466d073e94` | yes |
| `contract/v1_1/readfacts.go` | 1956 | `e4f0c2eb76dd62c514e55846fe9750b38ca94bb11b945378769c7f874054372d` | yes |
| `contract/v1_1/receipts.go` | 4180 | `40d7f2a77f5f643b454f60480526087c60a5f15f0cf6e128b7f638b69b1a677e` | yes |
| `contract/v1_1/values.go` | 9831 | `8c4bb55f287dd0fda745fa305336441b38cc043d094a7e4a4be3a123be1ce795` | yes |
| `contract/v1_2/codec.go` | 5577 | `9075a2645dac383b01877c693dc344e24d4aeb3b7aa502c14c2ca0ff123bf5cb` | yes |
| `contract/v1_2/commands.go` | 2188 | `674e72d8295f9a42ab32bcc09d5411b2e5c5f0f6aaa68827273b3efc146d76e3` | yes |
| `contract/v1_2/content.go` | 4925 | `170d73cd86febb8e07784c93a78c7e5dbfd3bc79d5ea6f372df97ba2c675e9a3` | yes |
| `contract/v1_2/digest.go` | 3593 | `1e51258d3fe5f1487a561ccdb7249ada01fb5a7f88cfe0211d8cb7036cc4bbec` | yes |
| `contract/v1_2/json.go` | 3647 | `c485c8eccd24a57a7347f4096969548bfff1cb34a6dde4f7e0c00232e9ee3afd` | yes |
| `contract/v1_2/query.go` | 3426 | `2a3335f2b77c631cb5038bef67a976dca7fdd2b510ca89ea17a255780939c193` | yes |
| `contract/v1_2/readfacts.go` | 1956 | `c9766a367c01738ef14e8b32c41a02190cd5865361f98327851c9d0156cb73cb` | yes |
| `contract/v1_2/receipts.go` | 2606 | `df3de1ff75f808fcd234f5d5afd7e22d67fd00a7e77724d1a3b7aba71147cd91` | yes |
| `contract/v1_2/values.go` | 6529 | `8cc767ef8855265f38827e39c8f39bfd9f91ad1ca03af85fe807e6154ed71f1b` | yes |
| `contract/values.go` | 4835 | `3ff7bde3b0564e49e779145bdea42571560982451a1e9fcdb87c0043445dfc1c` | yes |
| `domain/content/identity.go` | 1533 | `e093a7918be9af0aeaec08c62b472de1141314812a71e3da386583237bbf60f0` | yes |
| `domain/content/legacy.go` | 3007 | `82a93d2443892f109ef46978b25f1046fb1b94fad8cfb6a24266c5565e41fe41` | yes |
| `domain/content/ports.go` | 3971 | `dca31e058d644d0e453e652b0ec9ac10142edf0823118939fa8f3dda6575b21c` | yes |
| `domain/content/service.go` | 27165 | `c4f73eaa21f944396107b9927f639e1d328f8e36c893bece8cd1696b14be2183` | yes |
| `go.mod` | 444 | `ae25405c4fd98f5962178ff4241c52022a5bff182903337beead57204d39a04a` | yes |
| `go.sum` | 2850 | `67de0a2b012283127841c03932d678a06b9b278ae8b277db6f8cc9f7f3d99b37` | yes |
| `internal/durableworkdemo/maintenance.go` | 4190 | `3d0d13089ac8cf76372a28155f0896757ec8990d034dacefa94956d87ad97800` | yes |
| `internal/durableworkdemo/permissions.go` | 1821 | `0afb3caf2706668ba08fa006ec41ee973a379312d079d86d5ce5e59be8bad72d` | yes |
| `internal/durableworkdemo/pool.go` | 4143 | `9fe6056767bb2deba551ad88c5a85e16e8b2fa72b5945f22f02fd805d3d37ab6` | yes |
| `internal/durableworkdemo/pool_worker.go` | 5930 | `0f28cd85fdb8b1137d2fcfb0014fc2eac5442e31b4af687eb793d4d64da47684` | yes |
| `internal/durableworkdemo/record.go` | 1819 | `7c20e97356187b7d55a84bb67c49efb460c92cfdb2a12f9ca442f42d3afd8fbe` | yes |
| `internal/durableworkdemo/record.schema.json` | 1123 | `44de4165931646d8c687aab23620b1999a3692829d704f985d2fe945062cfea3` | yes |
| `internal/durableworkdemo/retention.go` | 4085 | `b72ff000f06cc2868fa60161d629605d1792407b03a3c461cdbab0faa30ac7db` | yes |
| `internal/durableworkdemo/schedule.go` | 6867 | `60f9041dcd29a65b894fdbc05caf6ee0120bd602d0013d89e76c2363971336cc` | yes |
| `internal/durableworkdemo/service.go` | 7026 | `d1263d975983aae36c347a6baa676d216f6ea9028c259fea7997bea8106914e3` | yes |
| `internal/durableworkdemo/step.go` | 9992 | `a6eaa4ea16c6d249a6dcc40f7e6532ad2615a3786a3d7dd10a947581b0439228` | yes |
| `internal/durableworkdemo/work.go` | 8452 | `be698e802ecc709642436a28ae9871787a673cc0b88149f8e8f75f4681d677b5` | yes |
| `internal/durableworkdemo/worker_permissions.go` | 942 | `e69d6687867d3c5dc2869fff0f936983eeb97ca91751e3961c21c8218fb98e5f` | yes |
| `runtime/admission.go` | 4419 | `9f6136f51196814bdaacd2065b038371d1a1827e9d4c0e0d496cf80c161bad21` | yes |
| `runtime/schedule.go` | 1213 | `d1d72e6cd41bafeb792cca905cf107ef7be5571b54656b32027bce58045a836f` | yes |
| `runtime/work.go` | 1088 | `4c46f07ed1b9dc289e1e146565834f10b2626fa4618ec503474bef7867529ef1` | yes |
| `runtime/workpool/pool.go` | 5540 | `3a656de3f9148528c9234245e551c1fc066f7714e1e06ce7f4dafa773f9cf930` | yes |
