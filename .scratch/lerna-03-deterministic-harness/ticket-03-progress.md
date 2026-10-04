# 03 cancellation and finite resources: implementation checkpoints

Ticket03 remains **claimed**. This is an implementation record, not a ticket or
whole-profile exit. Worktree `deterministic-harness-03` started clean at
`e29675d75f5135995614e4eb2faadf6c2ac1581d`. Root owns integration, external push,
exclusive test slots and final independent reviews. All integration commands use
`-p=1 -tags=integration -count=1 -timeout=120s`; normal and race runs are sequential.

| Actual source checkpoint | Public behavior and verification |
| --- | --- |
| `a666fedbf0c0a894baf02601aaa3ef7f79d61fba` | Real Source control access/proof survives Close/Open without a Snapshot or execution grant. Red0.188 → normal0.246 / race1.776. |
| `87129b94c1d1adbcaf280f1babb136c47565db3a` | Cancel-before-Decide saves a true nil-Input close and fixed receipt; late same input is rejected and no Job appears after both-owner reopen. Red0.321 → normal0.592 / race2.756. Explicit ControlAuthority, Decision0003, Source0002, strict metadata dual read and current_control added. |
| `6228d8d7a1ba72f452e7ef187232ac58690e3c37` | Real artifact publish/readback → Cancel → next planned Proposal publication is blocked; independent artifact bytes survive. Red0.542 → normal0.634 / race2.936. This is a local qualification boundary, not atomic revocation of already in-flight Source I/O. |
| `b53542d9ad57be09e4f9d950eddda3c0871e8090` | Sequential Finish-before-Cancel and true concurrent Cancel-before-Finish; completed/failed/cancelled nested terminal bytes and original receipts remain frozen while independent control revisions advance through maximum int64. Normal1.650 / race7.305, first behavior already correct. |
| `19a5b67b62e3ec707165cfdb60c497e441a9d12b` | Actual closed Source after valid access exposes dependency_unavailable, distinct from forbidden proof. Red0.322 → normal1.937 / race3.808. Expired original proof/accept_before replays only while current access remains valid. |
| `b5b80d70a3c3f541a1ea5b6ccbf9332a3e71c085` | Real final01 writer creates six states, drains both old writers, then current Source0002/Decision0003 upgrades preserve old metadata, receipt, usage and publication identities; current legitimate control and both-owner reopen work. Normal6.757 / race11.203. |
| `2a21915b9de57b3226a0e6aa25ac9cb58689cb6e` | Eleven immutable proof-binding mutations refuse without closing a target; actual Snapshot floor and forbidden expected_revision tested. Normal0.728 / race3.216, first behavior already correct. Error taxonomy is tightened at the next checkpoint. |
| `ffa7ffb7ca1b2dc7fc830ccae949210469827e0e` | Ten resource cases with exact independently read input/output sizes, zero/short/max allowances, bad public Source reply and different legal fixture cost unit; malformed admission rejected, allowed counterpart completes, receipt and terminal survive reopen. Precise proof taxonomy/floor included. Normal4.513 / race11.886. |
| `384d36ca8fc2dfa0c42d84defb140d10bb3d7ad9` | Admission cutoff, execution deadline and single-call resource context have separate actual outcomes; replacement retains original cumulative allowance and unknown prior observation. Normal4.990 / race7.202. |
| `238fc15129abafa1a1ae7a8f7c29b0f142ad4f2b` | quota0/full pool/actual Start-before-compute cancellation fences old Claims and releases capacity. Real65 non-anchor responsibilities expire across two bounded maintenance pages at quota0. Original normal5.576; explicit locked Claim/Start Stop checks then normal6.270 / race12.671. |
| `bba7edcf94ce0ac674e6c02ffe75d4b1e0e3553f` | Actual Stop/Record/Job rollback and successful COMMIT with lost reply, both-owner reopen and both directions of original Command method/binding priority. Normal1.236 / race5.583, first behavior already correct. |
| `eb119152b520e9be3af3cf3b1426dff419115dde` | Shared nilInput cancellation precision: exact zero observations and true completeness in machine1.1 and both typed codecs. Go actual red0.039 / green0.037; TS actual red0.550 / green0.436. All87 real bidirectional fixtures forward3.162 / reverse2.409; affected Component race12.404, generated check0.916 and TS typecheck1.268, all exit0. |

No failed run is converted into a behavioral red when it failed compilation,
setup, test input validation or cleanup. Local evidence and complete outputs are
in `/tmp/lerna-03-ticket-03-evidence.md` and the corresponding exact-selector logs.
Initial proof error-code compile mismatch, malformed resource test values and a
test observation incorrectly using the runtime's one-second polling timer are
recorded; their corrections did not change product limits or billing.

The initially archived68 final01 production files were byte-equal but lacked the
original embedded record schema. The first actual build failed before any old
producer/DB state. The accepted append-only closure correction now contains69
production payloads,448760bytes,71 SHA entries and72 physical files; all original
payload bytes remain frozen. First repaired upgrade cleanup hit the old10-ACK
bound with actual12 ACKs; the shared mechanical fix
`22abf0669e30d47cea152599c124a16d319d3f62` changes only that finite bound.

Historical checkpoint1e752 cleanup observation:217 exact acknowledged PG schemas were absent; the codec-only batch allocated no PG scopes. Updated exact observations are recorded below.
All current test sessions, registered workers and child holders exited. The
original build-failure directory
`/workspace/lerna-03-ticket-03-dbtmp-qfo17ozy/lerna-03-legacy-upgrade-3540254111`
remains **unknown and retained**, as does its parent overlay. No PID/group was
recorded for that historical window; no prefix, timestamp or process-name guess
is used to remove it. Three later exact compiler groups have confirmed absence.

Static mechanical followups `8e24c82b2445d46ea8ff3caf572deb0a4af1c64c`
and `283ca961c5c042cd17a99d4e51f4d998fe8d5337` preserve real native holder
ownership, ACK actual PID/PGID before Wait, avoid numeric group signals and
retain failed ACK scopes. The first followup alone lacked the compiler failed-ACK
retain condition; the second closes that static finding. No failure was
fabricated to label these as native fault evidence.

Restoration-only `7941b9d8c1b0273f1a30b9c700184e5fe651d824` verifies every
original970 payload/provenance hash, then patches only its own added_driver's
two post-READY failure exits to retain acknowledged scopes and native exit2.
Original added-driver SHA256 is
`266f74ee54198afde427b64dc0468bde8a123ec11a03ddef58c33594c38d42bc`;
its guarded own-scope form is8003bytes with SHA256
`94d250151940df0838fd89f54948c2d64b5fa2423e2ceb3e61613f7debc2daa0`.
The original archive and production payloads are unchanged. Separate own
FINAL01 control driver followup `846011a230e88addf0427a59e6f017ba3031c7ff`
has the same two finite supervision exits; its normal public business oracle
is unchanged. At checkpoint1e752 these adopted static changes awaited new-source actual upgrade
normal/race; the later stricter qualification and actual validation are recorded below.

Latest root949 was clean-merged as
`72989c7dc902b753599d43ee0c66b2f51d56d425`: actual BorrowChild and
BorrowWorkerExit both remain at both owners; each must confirm bounded join
before Close/Drop. Current recovery child migration count is adapted by
`0eb3577a283e5357f7091b34a9b969ed67abaf8c` to actual append-only Decision0003,
with no old SQL/archive rewrite. At checkpoint1e752 merge and count adaptation awaited affected actual verification;
root949's previous two-migration CI is not substituted for the new checks below.

Remaining gates: final candidate-format/action-budget overlap after ticket02
formally exits and root merges it; new-source historical970/FINAL01 upgrade,
current PG child stories and all affected current PG/old dual-store normal/race;
final checks and frozen-source hashes; latest integration merge and independent
standards/spec reviews. Whole03 architecture review remains root-owned on the
actual final combined tree; it is not an extra ticket03 acceptance gate.
Whole decision_engine profile advertisement remains false. Ticket03 does not
move any hidden cancellation/recovery requirement to ticket06.


## Independent review and exact followups

Root adopted the two independent reports at base949/head1e752: Standards two
P2 findings and Spec one P2 finding. Their sole fixer remains this ticket owner.
Shared `d90962c2c38ed4e9ef927fd7a53df48d3b5f0d27` preserves real compiler
Wait/group causes through the existing bounded, credential-safe diagnostics,
separately from actual exit confirmation and unknown-scope retention.

Shared `4cb3717c23b7a19ade7b11e7528707219fa7cd34` implements the exact adopted
[legacy driver qualification decision](legacy-driver-qualification-decision.md).
Only the fully hash-verified970 added-driver restore copy is transformed; the
original archives, provenance, manifests and production bytes stay unchanged.
Original7862B driver hash266f74ee54198afde427b64dc0468bde8a123ec11a03ddef58c33594c38d42bc
becomes8572B derivative hash aa7097a937c0defb5ee7e356b4e2ee71b7c069d7f96b514a2bdc07c07db05945.
This includes the earlier two RELEASE failure guards and strengthens the error
qualification oracle; old normal evidence is not retroactively typed ErrClaim
proof. Own FINAL01 control driver
`ea94b2c7868046191992852096934d8e869ed9cb` likewise preserves actual Claim
causes and checks the archived runtime sentinel, without hiding joined causes.

Source `88d234c7a7bfd567b1594e775e1ae3f730c18db9` adds an actual Finish-wins
two-transaction competition. Finish has really saved completed and completed its
Job before a bounded pre-CoreCOMMIT hold; concurrent public Cancel reaches the
actual pool lock and acquires it only after native Finish commit. Public terminal
bytes, cumulative usage, exact independent Source contents and the original
accepted receipt remain fixed through Cancel and both-owner reopen. First
focused normal0.819s was already correct, not an invented red. The original
sequential terminal matrix is preserved separately. Full current03 controls
normal18.804s/race37.741s passed. Limited conversion/cause mechanical checks
normal0.017s/race1.058s passed; these are not native fault evidence. Actual own970
and FINAL01 producer/drain/upgrade oracles under stricter qualification passed
normal13.085s/race17.938s. Historical producers use their normal frozen-source
builds; race instruments the current upgrade implementation.

The first current migration3 four-PG-child run failed6.802s because the old
pre-COMMIT test assumed a nonblocking FoundRunning read while current Get holds
a consistent Decision/Stop lock. Its output only proves non-Found, not an
observed native SQLSTATE. Root adopted the
[held-transaction observation decision](get-held-transaction-decision.md).
Clean `4db62a8e97b01c50165cf6c0529e01f87673dd50` changes only the local recovery
story and tracks that decision: actual Source Proposal Commit/pre-Finish first
provides public Running/full confirmed usage and both original contents; a
separate release reaches actual SaveDecision+Complete/pre-CoreCOMMIT. There Get
must explicitly return bounded unavailable for the exact target with
dependency_unavailable. Normal reply/Wait/Stop and actual SIGKILL/both-owner reopen
then determine the real Commit outcome and original prepared recovery. Neither
the product read lock nor the original lease/transaction/child deadline changes.
Four affected PG stories normal6.946s/race19.990s passed on this source. Every
command actually exited before the next; no Claim renewal or extended product
deadline was introduced.

One additional Standards oracle finding at control_test required an exact
execution denial for control-only access. Sole-fixer
`3d8b876523fba2304e2a40f8a17b5084e597ba3c` requires errors.Is(ErrForbidden),
with a safe stage error retaining other causes instead of treating any failure
as a valid denial. The same real Source/no-Snapshot/reopen selector first
normal0.247s/race1.820s passed; no fabricated red or native SQL fault is claimed.

The latest exact audit is439 unique PG ACK absent,8 directory ACK with only
original3540254111 unknown retained, and11 actual compiler/producer groups
absent. The overlay also contains an observed Node cache; it remains retained
with the protected unknown window. All current sessions/child/worker/DB/build
exited and the global slot was explicitly released. Final necessary broad
checks and actual /3 dualPrepared combination await the formal ticket02 root
integration; no peer moving business tree is picked and no profile is advertised.
