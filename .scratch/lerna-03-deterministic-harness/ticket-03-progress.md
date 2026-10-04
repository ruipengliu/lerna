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
| `b53542d9ad57be09e4f9d950eddda3c0871e8090` | Finish-before-Cancel and Cancel-before-Finish; completed/failed/cancelled nested terminal bytes and original receipts remain frozen while independent control revisions advance through maximum int64. Normal1.650 / race7.305, first behavior already correct. |
| `19a5b67b62e3ec707165cfdb60c497e441a9d12b` | Actual closed Source after valid access exposes dependency_unavailable, distinct from forbidden proof. Red0.322 → normal1.937 / race3.808. Expired original proof/accept_before replays only while current access remains valid. |
| `b5b80d70a3c3f541a1ea5b6ccbf9332a3e71c085` | Real final01 writer creates six states, drains both old writers, then current Source0002/Decision0003 upgrades preserve old metadata, receipt, usage and publication identities; current legitimate control and both-owner reopen work. Normal6.757 / race11.203. |
| `2a21915b9de57b3226a0e6aa25ac9cb58689cb6e` | Eleven immutable proof-binding mutations refuse without closing a target; actual Snapshot floor and forbidden expected_revision tested. Normal0.728 / race3.216, first behavior already correct. Error taxonomy is tightened at the next checkpoint. |
| `ffa7ffb7ca1b2dc7fc830ccae949210469827e0e` | Ten resource cases with exact independently read input/output sizes, zero/short/max allowances, bad public Source reply and different legal fixture cost unit; malformed admission rejected, allowed counterpart completes, receipt and terminal survive reopen. Precise proof taxonomy/floor included. Normal4.513 / race11.886. |
| `384d36ca8fc2dfa0c42d84defb140d10bb3d7ad9` | Admission cutoff, execution deadline and single-call resource context have separate actual outcomes; replacement retains original cumulative allowance and unknown prior observation. Normal4.990 / race7.202. |
| `238fc15129abafa1a1ae7a8f7c29b0f142ad4f2b` | quota0/full pool/actual Start-before-compute cancellation fences old Claims and releases capacity. Real65 non-anchor responsibilities expire across two bounded maintenance pages at quota0. Original normal5.576; explicit locked Claim/Start Stop checks then normal6.270 / race12.671. |

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

Current known cleanup observation:201 exact acknowledged PG schemas are absent.
All current test sessions, registered workers and child holders exited. The
original build-failure directory
`/workspace/lerna-03-ticket-03-dbtmp-qfo17ozy/lerna-03-legacy-upgrade-3540254111`
remains **unknown and retained**, as does its parent overlay. No PID/group was
recorded for that historical window; no prefix, timestamp or process-name guess
is used to remove it. Three later exact compiler groups have confirmed absence.

Remaining gates: actual Stop transaction rollback/COMMIT reply-loss and two-way
original Command identity matrix; final candidate-format/action-budget overlap
after ticket02 formally exits and root merges it; full affected codec/Go/TS,
frozen-source hashes, historical970 and current PG/old dual-store normal/race;
latest integration merge, independent standards/spec and architecture reviews.
Whole decision_engine profile advertisement remains false. Ticket03 does not
move any hidden cancellation/recovery requirement to ticket06.
