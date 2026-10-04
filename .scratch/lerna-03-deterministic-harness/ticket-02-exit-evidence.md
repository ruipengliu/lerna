# 02 bounded Proposal evidence

Ticket02 remains claimed while its newly adopted shared old-writer helper is
awaiting related execution and review. The seven implementation checks have
passed the complete product verification below; this is not whole03 exit.
Root owns integration, push, remote CI and the actual whole-slice architecture
review. No later ticket's business implementation is an added prerequisite.

Product source is `3d60b6a8c41a82fcce940b396d871f3ecb8db1e5`.
The corrected mechanical recovery test and complete check pin is
`25287d5a08ff01d5265e56b06faf87cd37a09006`, based on the actual root integration
`949c39237fda562e8bda994a8e1454a27232dc72`. The separate shared compiler cause
correction is now `0f6d23a98f099d7b2ba2750389be10a8c5acfcda` (original
`d90962c`). Strict restored-970 qualification and its mechanical checks are
`6b98eb0b59606d990bc6c563e053315bd2527c47` (original `4cb3717`); their related
final execution is pending. Subsequent document changes
do not change the tested product. See [adopted Proposal decisions](proposal-decisions.md),
[progress and failure history](ticket-02-progress.md) and [ticket](issues/02-bounded-proposals.md).

## Seven acceptance checks

| AC | Public behavior and independent durable observations |
| --- | --- |
| 1 | `delta_only` retains exact Decision/Snapshot/goal/control headers, the complete processed set and the current replacement. It completes with `none`, zero artifacts and one actually readable Proposal. `delta_candidate_result` retains both its replacement and its actual candidate rather than treating them as an illegal combination. |
| 2 | Four actions, clarification input, candidate Result and cannot-continue all have literal normal controls and actual independent publication-reply-loss recovery. Input question/schema/preview are read from Source under their declared purposes; confirmation and remote `$ref` schemas fail. Cannot-continue remains a Proposal and candidate Result remains a candidate, with no Task verdict or adoption. |
| 3 | Four distinct local keys use the exact capability/binding/argument/purpose tuples from the immutable Snapshot. Fixed outputs with depends_on, another binding's valid arguments, wrong capability, a future result, duplicate keys or a fifth action fail through the same codec and semantic consumer. Action limits 0–4 retain their original identity and fee allowance. |
| 4 | Current condition replacement succeeds; stale and duplicate replacements fail. Full processed sources cannot be replaced by disclosure; omitted processed and foreign disclosed sources fail. Candidate evidence and action arguments have actual allowed-purpose reads and actual permission-refused controls. All purpose reads charge remaining input budget and verify exact bytes/hash/length. |
| 5 | Existing closed 1.1 Schema/Go/TS codecs are consumed by the component. Two appended raw duplicate/unknown-field fixtures preserve every original 81 object. All 83 new and 158 original fixtures pass actual Go-to-TS and TS-to-Go exchanges in both orders. Source membership, revision and purpose checks remain at the component seam; codec does not query a database. |
| 6 | Wrong outputs come from distinct private cases bound by actual version/config digests and immutable Source Snapshot/lock/manifest. Fifteen typed/raw matrix cases plus four focused semantic faults preserve the original accepted receipt, exact failure, one charging identity, generated-byte observations and no repair job after real owner reopen. Three actual reseed attempts refuse a new case/version/config at the same fixed Snapshot, and the original rule still completes. Actual Source infrastructure read failure remains snapshot_unavailable rather than being falsely called an illegal Proposal. |
| 7 | Normal and refusal paths have finite contexts, durable public queries, exact receipts and independently readable publication bytes. All five actual V2 reply-loss branches resume original prepared output under a one-step/one-fee allowance, preserving input digest, usage and the Publisher's original refs/bytes. The original FINAL01 public producer generates six old states; current owners recover original Prepared v1, keys, fee/measurement facts and receipts without reinterpretation. No branch changes Task revisions or adopts delta. |

The original `/2` case remains candidate_result; old `/2` actions_four and new
`/3` undefined_case fail with their version-specific meaning. The new rule uses
exactly one separate `prepared_v2` format. Its artifact list is actually empty
for no-artifact branches, and actual candidate artifact identities come from
Publisher.Plan. Records containing both prepared formats are unavailable.
Direct source comparison against FINAL01 verifies the original Prepared fields,
digest function and Validate bytes unchanged. Published Decision0001,
Decision0002_rule_start_accounting and Source0001 SQL bytes also match original
FINAL01. No new DDL, wire type, profile advertisement or schema rewrite was
needed for this ticket.

## Actual verification

All direct Go commands below use `-p=1 -count=1 -timeout=120s`; database commands
also use `-tags=integration`. Every command's actual session exited before the
next command started. The worktree used locked tools/dependencies and owned
`/workspace` overlay scopes, with successful CREATE/Mkdir/process acknowledgments
recorded and fsynced. Credentials stayed in private environment variables.

| Pin / selection | Actual result |
| --- | --- |
| `1a9b5c5`, all `^TestDurableProposal` | normal 11.646 s / race 26.934 s, both exit0; before Standards cause fix |
| `60c9519`, locked own-worktree bootstrap then 83-fixture cross-codec forward/reverse | bootstrap exit0, both orders exit0; fresh runner exchanges, some subsidiary pure Go output cached |
| `3d60b6a`, native Source read-failure plus normal/purpose/limits/invalid controls | normal 16.502 s exit0; actual successful Close followed by real read failure, not a Close/COMMIT fault |
| `25287d5`, all `^TestDurableProposal` | normal 12.438 s / race 28.333 s, both exit0 |
| `25287d5`, `^Test(Final01PublicPreparedProposalUpgrade\|FrozenLegacyWriterUpgrade)$` | normal 11.356 s / race 15.655 s, both exit0; old qualification guard predates stricter ErrClaim correction |
| `25287d5`, `make check GOFLAGS='-mod=readonly -p=1'` | exit0: format/vet, generated consistency, JS20, TS37, 83+158 real cross-codec fixtures both orders, Go/TS build |
| `25287d5`, `go test -race -p=1 -count=1 -timeout=120s ./...` | exit0; Component11.702 / target16.905 / Source1.166 s |
| `25287d5`, complete `./conformance/component ./conformance/internal/decisionfixture` | normal38.554/13.715 and race74.493/19.398 s, exit0 |
| `25287d5`, complete `./conformance/recovery/...` | normal38.009 / race80.735 s, exit0; original PG/SQLite and already integrated 06 public SIGKILL stories |
| `25287d5`, modules / seven actual SHA256SUMS | exit0; 27 durable-work + 71 original970 + 71 FINAL01 + 3 target entries |

Raw capture is in `/tmp/lerna-03-ticket-02-final-*.log`; the progress document
retains earlier vertical red/green and failed attempts. The first codec attempt
lacked locked Ajv and failed before business execution. The 3d full race failed
28.553 s because reopening had passed NextWake and the test passed a negative
duration to WallTimer. Both failures are preserved; the later corrected result
does not change their original exit status. There was no timeout increase,
missing-suite bypass or substitute in-memory database.

## Historical producers and exact resource scope

The fixed FINAL01 archive contains 69 original production files / 448760 bytes,
plus original provenance/README, totaling 71 verified checksum entries. All
production bytes match `696ac49846105a16f33e5de86dc621a3858651b2`. Normal builds
restore committed payloads and do not depend on Git objects. This ticket's own
9053-byte driver produces accepted, started running, prepared-waiting,
publication-running, completed and failed using the original public Component
and Source. Both original writer holders close successfully before READY;
current owners migrate and query/read publications, preserve original receipts,
then close/reopen again and leave no runnable job. Started unmeasured work
retains its conservative original start and measurements_complete=false.
Outer race instruments current consumer/supervisor, not the normal original
producer binary.

The separate 970 archive's production and archived added-driver bytes remain
unchanged. Only the current hash-verified restored added-driver receives the
finite retention/qualification correction. EOF/deadline retention is a static
adoption, not a replayed native fault. Earlier successful old-970 upgrades do
not prove that RunClaim returned the newly required specific ErrClaim type.
The adopted [qualification decision](legacy-driver-qualification-decision.md)
permits only six unique fragment changes after the complete original archive
and exact added-driver hash are verified. The restored derivative is 8572 bytes,
SHA256 `aa7097a937c0defb5ee7e356b4e2ee71b7c069d7f96b514a2bdc07c07db05945`.
It distinguishes missing Claim, actual Claim error, wrapped ErrClaim and
additional joined causes with a finite 32-layer bound. Mechanical source
conversion and safe joined-stage checks do not reproduce native Wait/group
failure. Related actual upgraded-consumer verification remains pending.

Checkpoint exact audit at `25287d5` found all **1580 unique PG schemas**, **64
target directories**, **244 recovery SQLite directories**, **11 archive
directories** and **19 acknowledged process groups** absent. Duplicate transfer
acknowledgments are not counted as distinct scopes. Every child/holder/scanner
and test/build session exited before the slot was released. The acknowledged
owned overlay root retains only Node's compile cache for the pending related
check. Other tickets' unknown historical scopes were neither inferred nor
removed. These facts support this declared scope, not all-environment cleanup.

## Independent review and remaining closure

Separate fixed reviews covered baseline949 to head3d, all 25 commits / 87 paths.
Standards initially found one hard error-classification defect and one duplicated
read judgement; both are closed by the private helper and preserved native cause.
The full followup reports 0 hard / 0 smells. Spec separately reports a0/b0/c0.
Both axes rechecked the 252 timer/test-history increment with no new finding.
They ran no tests and do not substitute for the execution rows above.

The later shared compiler Wait-cause correction and strict old-driver
qualification still need their related actual consumer verification and final
independent followup. Ticket02 remains claimed until those necessary changes
are covered. Whole03 support advertisement, cancellation/resource work, actual
merged-tree architecture and remote CI remain root's separate responsibilities;
this evidence does not prove production capacity, power-loss durability,
provider idempotency, Executor Effect or a second business implementation.
