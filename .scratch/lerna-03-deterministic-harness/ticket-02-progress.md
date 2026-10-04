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
| 15 fixed invalid-output cases through the same public validation | 3.374 s, each fixed case failed to generate output for validation | 5.507 s with five normal branches; same selection race 12.664 s | Matrix change accompanying this record |

Red child processes exited 1; listed green child processes exited 0. Each run
used `-p=1 -tags=integration -count=1 -timeout=120s` and finished before the
next run. The latest audit after the matrix found all 196 unique successful PG
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
match. These are source checks, **not** actual old-writer upgrade results. Mechanical
restore/supervision helpers are present; this ticket's own archive driver build,
independent old-producer states and upgrade oracles are still pending. A separate
03 driver has compiled the repaired source and reached its own business oracles;
that does not establish this ticket's prepared-format compatibility.

Remaining work includes other finite wrong-output
and source/revision counterexamples, original action/output/input limits, new
prepared interruption recovery with and without artifacts, final01 public-writer
compatibility, Go/TS raw conformance, root integration and independent review.
