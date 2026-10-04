# Ticket02 implementation evidence (in progress)

Ticket02 remains **claimed** with all seven acceptance checks open. This record
tracks actual focused results; it is not ticket02 or whole03 exit evidence.
Baseline is `e29675d75f5135995614e4eb2faadf6c2ac1581d`, following the
[final01 handoff](ticket-01-api-handoff.md) and adopted
[proposal decisions](proposal-decisions.md).

Each new case has its own real Source/Decision scope and a fixed `/3` component
binding. Tests submit public `Decide`, consume actual Source permissions and
content, run the worker, reopen both owners and inspect public Decision or
Command facts. Normal completed cases independently read the actual published
Proposal; the candidate also reads its actual artifact. No Task revision changes.

| Vertical | Actual behavioral red | Focused green | Implementation pin |
| --- | --- | --- | --- |
| `delta_only` | .284 s, unsupported binding | .527 s; old `/2` prepared control 1.808 s | `f5f7342` |
| `actions_four` | .450 s, accepted then unsupported case failure | .806 s with no-artifact control | `b5d66bf` |
| `input_request` | .447 s, accepted then unsupported case failure | 1.040 s with earlier normal cases; old `/2` control 1.811 s | `6a002d2` |
| `delta_candidate_result` | .453 s, accepted then unsupported case failure | 1.454 s with earlier normal cases | `299393e` |
| `cannot_continue` | .416 s, failed instead of completed Proposal | 1.580 s with four earlier cases; old `/2` control 1.787 s | `6b489f2` |
| Fixed raw `depends_on` | .521 s, output bytes zero because case had not generated output | .795 s with independent four-action control | `015cdb4` |
| Fixed wrong binding/argument tuple | .551 s, output bytes zero because case had not generated output | .999 s with four-action and affected fixed raw controls | `dff4fa1` |
| Actual argument purpose denied by Source | .646 s, output bytes zero because case had not generated output | 1.176 s with legal-purpose four-action and wrong-pair controls | `6f76995` |
| Condition replacement with wrong revision | .465 s, output bytes zero because case had not generated output | .719 s with current-revision delta control | `6535a7a` |
| 15 fixed invalid-output cases through the same public validation | 3.374 s, each fixed case failed to generate output for validation | 5.507 s with five normal branches; same selection race 12.664 s | `706a0a0` |

Red child processes exited 1; listed green child processes exited 0. Each run
used `-p=1 -tags=integration -count=1 -timeout=120s` and finished before the
next run. The audit after the invalid-output matrix found all 196 unique successful PG
schema acknowledgements absent, with no live test session, child or DB holder.
The exact external registry remains available to the coordinating root. The
purpose test independently confirms the exact argument is readable as
`rule.input` and forbidden for its real binding purpose. Its typed Proposal
retains the full correct tuple; actual Source consumption causes the failure.
The 15-case matrix covers duplicate replacement, omitted processed sources with
full disclosure, foreign disclosure, capability pairing, a future action result,
duplicate action keys, a fifth action, combined advance fields, duplicate and
unknown raw fields, empty-delta `none`, stale candidate evidence, actual Source
evidence purpose denial, authorization confirmation and an actually readable
but forbidden answer schema. A transparent Publisher observer saves only the
real Plan outputs; any planned invalid artifact is independently unreadable
after reopen. No error case is implemented by directly returning its failure.

Two failed green attempts are retained as failures: initial no-artifact bytes
were not canonical (.438 s), corrected by using the same public decoder and
encoder; the first fixed-error test incorrectly expected `command.get` to return
Decision progress (.851 s), corrected to compare the fixed accepted receipt
and separately query the original Decision. Neither is claimed as a behavioral
red or a passing result.

The original `Prepared` fields, digest function and validation function have
been compared byte-for-byte with baseline. A separate 03 driver actually found
missing original `record.schema.json` embed bytes on its first archive build.
The original 68 payloads remain unchanged. The adopted append `ce817bb` adds
those exact 1123 original bytes and derived metadata; `10e335e` fixes the
mechanical compiler cancellation to use its actual process holder.
Independent source checks confirm all 69 production files (448760 bytes) equal
`696ac49846105a16f33e5de86dc621a3858651b2` byte-for-byte and all 71 hash entries
match. Those checks alone were **not** old-writer upgrade results. Mechanical
restore/supervision is shared; this ticket's independent producer and business
oracle now establish the actual compatibility described below. The separate 03
business oracle is not used as this ticket's prepared-format evidence.

A second bounded selection passed normal 4.279 s and race 12.535 s, with both
actual sessions exiting 0 before the next operation. It contains ten original
limit cases (zero steps/cost, actual input budget, total artifact/Proposal output
budget, and action limits 0–4), two real independent publication commits whose
replies are deliberately lost, original `/2` and new `/3` unknown labels, and
allowed Source evidence consumption. The two recovered `/3` handoffs cover no
artifact and an actual candidate artifact. After reopening both owners they
retain the original input digest and measured usage, independently readable
original planned references and bytes, and the fixed accepted receipt. Their
one-step/one-fee allowance would reject any fresh evaluation. These are actual
public Producer/Source and recovery results, not prepared-row fabrication.
The latest exact audit found all 256 successful PG schema acknowledgements
absent and no live test session, child or DB holder.

`TestFinal01PublicPreparedProposalUpgrade` actually compiled the frozen 69-file
FINAL01 source with this ticket's independent 8793-byte driver. The original
public Component and Source generated six states: accepted, durably started
running, prepared waiting before publication, running after actual artifact
publication, completed and failed. The producer read its own real publication
facts and successfully closed both writer holders before reporting ready. New
owners opened the original scopes, migrated through the published ledgers and
used the original `/2` Component/permissions and public inputs. The two original
prepared states completed without another evaluation/start/fee; actual public
Plan/readback verified the original key, reference, bytes and source sequence.
The started-but-unmeasured state conservatively retained its original start and
charged the second bounded start; its unknown measurement gap retains
`measurements_complete=false`. Terminals retain their original facts, while public command query
and Decide replay retain the original accepted receipt. A second new-owner
reopen preserves every terminal and leaves no runnable job.

The first normal run passed 5.058 s. Static lifecycle inspection then made the
new holder counter and close-failure marker shared across all cases, so a later
case cannot overwrite an earlier unknown closure. The final normal passed
4.730 s and race passed 8.143 s, each actual session/child exiting 0 before the
next run. The race invocation instruments the current consumer and supervisor;
the frozen producer is built by its original normal `go test -c`, not claimed
as a race-instrumented old binary. Successful original CREATE acknowledgments
are handed off and fsynced before use. The independent final audit found all
292 exact PG schemas absent, all three acknowledged temporary archive
directories absent and all three acknowledged compiler process groups absent.
The owned overlay root remains registered and retained; no live test session,
old producer, current holder or unjoined scanner remains from these runs.

Remaining work includes validating the extra three prepared-recovery branches,
the two appended shared raw Proposal codec fixtures, root integration,
independent review and any adopted shared mechanical supervision correction.
These completed focused results do not yet close the seven acceptance checks.

Before final integration, the ready code expands the same actual reply-loss
recovery loop to all five branches, adds three actual immutable Source reseed
rejections with the original normal completion, and appends two raw Proposal
fixtures without changing the original 81. These changes have not yet run.
The original producer driver is now 9053 bytes: after ready, EOF or its release
deadline exits without running old schema-drop cleanup; only literal RELEASE
allows normal administrative cleanup. This failure-path retention is currently
static, not claimed as an injected native failure result. Two previously ignored
public Decision encoding errors are also checked explicitly.

Shared mechanical followups `ed57adbb094abffc2da5d62f71d083c35ccd79b2`
(original `8e24c82`) and `ce3e861e81ee2cc5dbfc3e5b316b9db1409ddcb3`
(original `283ca961`) record actual producer/compiler groups before Wait, use
native process holders for cancellation and retain unconfirmed acknowledgments
or scanner exit. They have been adopted by source review; this ticket's final
actual upgrade check against them remains pending. No frozen payload or SQL
migration has been changed.
