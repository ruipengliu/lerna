# 03 cancellation and finite-resource evidence

All eight ticket03 checks have complete implementation and execution evidence.
Root accepted the complete evidence and the tracker is **resolved**. This is ticket03
evidence, not whole03, profile advertisement, production-provider or remote-CI exit.
Root owns integration, push, the whole-slice architecture review and final CI.

The worktree started clean at `e29675d75f5135995614e4eb2faadf6c2ac1581d`.
Final tested product/fixtures/tests are exactly
`882e97b596ac2baa034881766041944b0da2e05e`; formal root02/06 integration
`5bcdea8669adb49c341342e5b05deadffa0b8661` was merged as
`b0beb2da45204276029d5dd11bfcec2439d46dfa`. Every final check below ran on
that unchanged882 source. Subsequent changes are documents only.

See the [ticket](issues/03-cancel-and-limits.md), [control decisions](control-decisions.md),
[API handoff](ticket-03-api-handoff.md), [vertical history](ticket-03-progress.md),
[exact combined reviews](ticket-03-review-combined-followup.md),
[race partition](ticket-03-component-race-partition.md) and
[literal cleanup audit](ticket-03-cleanup-audit.json).

## Eight checks and actual public evidence

| AC | Actual behavior, independent observation and recovery |
| --- | --- |
| 1 | Real Source stores current control access separately from immutable owner-issued proof. `TestDurableControlWithoutSnapshotSurvivesSourceReopen` has no fabricated Snapshot or execution grant. Eleven proof mutations bind full principal/delegation, actual fixture Task issuer, Decision/Task/inputdigest and exact proof ref/hash/length. Current access succeeds while invalid proofs refuse; actual closed Source gives dependency_unavailable. Real original Snapshot floor is read independently, and expired proof/admission cannot authorize a fresh stop. |
| 2 | `TestDurableCancelBeforeDecideSurvivesBothOwnersReopen` saves a true nilInput close, exact bound Stop and applied receipt without a Job or fake original request. Same late input is decision_cancelled; different binding is decision_mismatch. Both owner reopens preserve the original closed facts/receipt and no runnable work. Shared machine1.1/Go/TS codecs require exact zero observations and measurements_complete=true for this no-execution state. |
| 3 | Real terminal/revision matrix preserves the entire nested Decision while separate control advances. Lower revisions refuse; same revision/same binding replays and different binding conflicts. Maximum int64 is exercised without substituting it for object revision. Real Snapshot floor and explicit expected_revision refusal remain current-authenticated. Original Command priority precedes fresh proof/binding policy and both directions of decide/cancel method-key reuse conflict correctly. |
| 4 | `TestDurableControlFinishCommitsBeforeConcurrentCancelPreservingOriginalFacts` creates actual overlapping owner transactions: Finish successfully saves completed and completes its Job before held CoreCOMMIT, concurrent public Cancel reaches the same locked pool, then native Finish commits before Cancel acquires it. The other actual publication/readback gate overlaps Cancel before Finish. Old claims cannot write completed or start again; each real V1/V2 publication and save/defer/Finish uses the same Stop/Claim qualification. Exact independent already-published bytes remain, and an unadopted Proposal is never described as completed. The older sequential Finish-before-Cancel matrix remains separately labelled. |
| 5 | completed/failed/cancelled matrices compare full encoded original terminal facts, usage, Proposal/artifact refs and accepted receipt before/after control and both-owner reopen. Only independent Stop/new control receipt changes. Public current_control scope is local_decision_work outside frozen Decision; it does not claim external deletion or physical cleanup. True concurrent Finish-wins independently rereads both Source publications and freezes the original receipt before Cancel. |
| 6 | Ten real byte/step/fee cases measure original Source input and actual artifact+Proposal output, with zero/short/exact/max allowances and legal completion counterparts. Malformed admission and bad public Source reply refuse accurately; exhausted original Decision fails while accepted remains fixed. Exact /3 action-budget0–4 and actual four-action completion are included by the full combined suites. No retry resets a budget or fabricates a model charge. |
| 7 | `TestDurableControlSeparatesAcceptBeforeExecutionAndContextDeadlines` distinguishes expired first admission, execution cutoff and bounded single-call resource context. Before-cutoff legal controls complete; execution and interrupted observations retain exact original identity, cumulative allowance and unknown measurement window through replacement/reopen. Product lease/transaction/execution limits are not extended to make a test pass. |
| 8 | Stop/Decision/Job/receipt share a real owner transaction: rollback and successful COMMIT with lost reply have different durable public results. quota0/full pool cannot prevent local responsibility closure or old-Claim fencing; 65 real non-anchor responsibilities expire across two finite maintenance pages. True old FINAL01 public producers create six states and close/drain before Source0002/Decision0003 upgrade; receipts, original metadata/input/usage/publication identity and current legitimate control survive reopen. Four actual native PG child normal/SIGKILL stories use current migration3 and both-owner recovery; no hidden cancel dependency is delegated to ticket06. |

The named tests live in [Component controls](../../conformance/component/durable_control_test.go),
[proof/floor](../../conformance/component/durable_control_boundary_test.go),
[true Finish-wins](../../conformance/component/durable_control_finish_test.go),
[resource limits](../../conformance/component/durable_control_limits_test.go),
[deadlines](../../conformance/component/durable_control_deadline_test.go),
[capacity](../../conformance/component/durable_control_pool_test.go),
[transaction recovery](../../conformance/component/durable_control_tx_test.go),
[Command identity](../../conformance/component/durable_control_identity_test.go),
[real Source](../../conformance/internal/decisionfixture/control_test.go),
[original control producer upgrade](../../conformance/internal/decisionfixture/final01_control_upgrade_test.go)
and [native child stories](../../conformance/recovery/decision_process_test.go).
No private business-table queries, field-shape mirrors or callback counts serve
as business acceptance evidence.

## Final source882 verification

Direct Go commands use `-p=1 -count=1 -timeout=120s`; DB commands also use
`-tags=integration`. All commands ran sequentially with actual session exit
before the next. Credentials were read from the authorized private file and
injected programmatically; they were never printed. Tools were locked
Go1.27.1/Node24.19/pnpm12.8.1/PG18.6 with CGO and an owned overlay TMPDIR.

| Exact scope | Actual exit/result |
| --- | --- |
| Unchanged standard `make check GOFLAGS='-mod=readonly -p=1'` | 0; wall47.259s: formatting/vet/types, deterministic generation/refusal/goldens, finite JS descendant/runner tests, real Go/TS both-direction89 new+158 old fixtures in both orders and builds. Developing1.1 subsidiary Go checks fresh count1; unchanged pure1.0 reverse subsidiary helpers used cache and are not described as fresh. |
| `go test -race -p=1 -count=1 -timeout=120s ./...` | 0; wall53.914s; Component16.222s/target17.241s and necessary mechanical lifecycle tests included. |
| `go mod verify` | 0; all modules verified, wall0.516s. |
| All seven actual SHA256SUMS manifests | 0; 172 entries =27 original durable-work +71 old970 +71 FINAL01 +3 target. The69 FINAL01 production-payload count is distinct from checksum entries. |
| Full tagged `./conformance/component ./conformance/internal/decisionfixture` normal | 0; Component60.881s/Source29.966s, wall93.913s. Full Source naturally includes the independent970 and both FINAL01 consumer upgrades on combined Source2/V3/control code. |
| Same full tagged race command | **1**; Component whole-package timeout120.073s, while original Unicode-body test was running, with no printed business assertion. Source race28.434s passed. The actual failed command remains failed. |
| Exact Component race partition, actual tagged inventory102 | Both0:60Durable PG110.803s, then42remaining11.781s; disjoint union102 with empty intersection/missing/extra. Each exact anchored-name command keeps count1/p1/integration/120s. No skipped dependency or widened timeout; already green Source was not repeated. |
| Full tagged `./conformance/recovery/...` normal then race | Both0: normal37.201s/wall39.830s, race87.979s/wall92.593s. Original PG/SQLite recovery and current native migration3 Decision stories included. |

Complete captured outputs are `/tmp/lerna-03-ticket-03-combined-*.log`,
`/tmp/lerna-03-ticket-03-component-{pg-business,other}-race.log` and the
corresponding vertical logs. Root owns adapting the remote race entry to the
same [executed finite partition](ticket-03-component-race-partition.md); the
previous single Component120s command cannot be silently called equivalent.
No old remote CI run covers this new882 source or the final89 fixture inventory.

## Failed attempts and later qualified evidence

The [progress record](ticket-03-progress.md) retains actual vertical red/green,
source pins and first-correct normal observations. `/tmp/lerna-03-ticket-03-evidence.md`
retains each fuller local boundary/log pointer. In particular:

- Source no-Snapshot red0.188, nilInput Cancel red0.321, actual next-publication
  gate red0.542, Source dependency classification red0.322, Go/TS no-execution
  usage red0.039/0.550 and fresh original-binding alias red0.422 were actual
  behavioral failures, followed by the recorded minimal green changes.
- Proof/resource setup compilation, invalid test input, strict TS typing and
  canonical/raw JSON ordering failures were harness/input failures, not invented
  product reds. The earlier slash-selector normal3.666s did not execute whole
  V2 tables; the subsequent nonslash2.872s and final full suites do.
- Initial FINAL01 archive68 payloads were byte-equal but lacked original embedded
  record.schema.json. Its build failed0.074s before any producer/DB business.
  Only exact original bytes were appended; complete closure is69 production
  payloads448760B/71checksum entries/72files. The unknown original build window
  remains retained, not guessed clean. Repaired upgrade first cleanup failed6.557s
  at old10-ACK bound despite actual12 ACKs; finite mechanical correction then
  normal6.757/race11.203 passed.
- Old pre-COMMIT public-read assumption failed6.802s. The adopted
  [held-transaction observation](get-held-transaction-decision.md) uses actual
  Source-publication and SaveDecision+Complete checkpoints. Bounded exact
  unavailable/dependency_unavailable under the held lock does not prove a native
  SQLSTATE or Commit outcome; actual reply/Wait/Stop or SIGKILL/both reopen does.
  Affected native stories normal6.946/race19.990 passed; final full recovery also
  passes. Original lease/transaction/child limits remain unchanged.
- The whole Component race timeout120.073s remains failed; the exact102-test
  finite partition above is the complete later verification, not a timeout
  increase or relabelled whole-command result.

## Immutable producers and lifecycle qualifications

Original wire1.0, published Source0001, Decision0001/0002, Host0001–0005 and
frozen sources remain unchanged. New Source0002/Decision0003 are append-only.
Original `/2` Prepared fields/digest/Validate and candidate bytes remain fixed;
actual independent `/3` PreparedV2 has one artifact+Proposal for the candidate
and zero artifacts for delta/actions. List cap16 is not evidence of a producer
with sixteen artifacts.

Original FINAL01 production bytes are from
`696ac49846105a16f33e5de86dc621a3858651b2`; normal builds restore committed
hash-verified files without historical Git availability. This ticket's own
driver creates accepted, started running, prepared waiting, publication-running,
completed and failed using the actual old public Component/Source. Both old
writers close/drain before current migrations and public cancellation; the old
producer has no old cancellation state and none is fabricated. Outer race
instruments the current upgrade consumer, not the normal frozen binary.

The adopted [legacy qualification decision](legacy-driver-qualification-decision.md)
permits only six hash-checked unique conversions in the restored own970
added-driver. Original7862B hash
`266f74ee54198afde427b64dc0468bde8a123ec11a03ddef58c33594c38d42bc`
becomes8572B qualified/release derivative
`aa7097a937c0defb5ee7e356b4e2ee71b7c069d7f96b514a2bdc07c07db05945`.
The error qualification oracle is explicitly strengthened; earlier normal logs
are not retroactively typed ErrClaim proof. Original archive/provenance/hash
payloads stay unchanged. Own FINAL01 control driver uses its archived ErrClaim;
the independent proposal driver retains its distinct actual context.Canceled
oracle. Strict own two-upgrade normal13.085/race17.938 and limited mechanical
conversion/cause normal0.017/race1.058 passed, then final full combined Source
normal29.966/race28.434 covers all consumers again. Mechanical conversion,
safe Wait/group cause handling and RELEASE failure guards are not native fault
injection evidence.

## Exact final resource scope and reviews

Final audit at882, `2026-10-04T09:23:45.047334+00:00`, observed all **1545 unique
successful CREATE PostgreSQL scopes**, **244 recovery SQLite directories** and
**64 target directories** absent. Of **14 exact archive-directory ACKs**, only
the original unknown
`/workspace/lerna-03-ticket-03-dbtmp-qfo17ozy/lerna-03-legacy-upgrade-3540254111`
remains. All **23 actual acknowledged compiler/producer process groups** were
observed absent. The owned parent overlay and observed Node cache remain with
that protected unknown. No PID/group was recorded for its initial failed-build
window, and no prefix/time/process-name guess, retry or numeric group signal is
used to remove it. Unknown other-ticket scopes are untouched.

Audit uses literal successful-ack capabilities and only observes their exact
names. Registry SHA256 is
`6bc4d800318550e16791ebdbc5da7575548b98f049683b9b1cf82b7b049307e9`.
Every current test session/build/child/worker/DB holder exited before explicit
global RELEASE; this does not claim the historical unknown was resolved. See
the [full audit JSON](ticket-03-cleanup-audit.json).

Root read and adopted the independent Standards/Spec reviews for the whole
current base5bc→head882 range: **40 commits /46paths**, independently verified
40 unchanged reviewed objects and six fully read new objects. Final Standards
**0 hard/0 smells**, Spec **a0/b0/c0**, worst none. All original findings have
the same sole fixer and remain closed. Reports are preserved verbatim with
their earlier split-pending cutoff; later110.803/11.781 and recovery results are
separately recorded execution facts. Root-owned whole03 architecture/profile/CI
closure is not inferred from either report or this ticket's eight checks.
