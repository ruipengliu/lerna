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

Final integration with root `949c392` produced clean source `1a9b5c5`.
All `TestDurableProposal` cases passed normal 11.646 s and race 26.934 s,
with each actual session exiting before the next command. This includes all
five independently committed reply-loss branches and the three real immutable
Source reseed refusals. The original 81 shared fixtures remain unchanged.
The first codec invocation failed because this worktree lacked the locked Ajv
dependency; that installation failure is not a contract or behavioral red.
After successful `make bootstrap`, all 83 actual cross-language round trips
passed in both orders at `60c9519`. The reverse subsidiary pure Go test output
was cached; the 83 runner exchanges themselves were freshly executed.

Independent Standards review identified a real error-classification defect:
a native Source material-read failure during Proposal validation was wrongly
reported as `proposal_invalid`. A transparent Source wrapper successfully closes
the real Source only at the declared action-purpose read, delegates the actual
read to that closed native holder, and observes its real failure. Public query
after reopening reproduced the defect (normal 0.557 s, exit 1). The fix preserves
the original read error for the existing worker dependency boundary;
`ErrForbidden` remains `proposal_invalid` and `ErrInputLimit` remains
`input_over_limit`. A private concrete helper consolidates the four bounded
purpose reads while retaining every membership check and observed byte charge.
The same public fault case and related normal, purpose, original-limit and
invalid-output controls passed normal 16.502 s, actual session exit 0. Its fixed
accepted receipt, one fee, measured generation and absence of another runnable
job survive owner reopen. This is a successful native close followed by a real
read failure; it is not an injected Close or COMMIT failure.

The adopted `60c9519` shared restoration guard preserves old-970 schemas on
post-ready EOF/deadline only after verifying every original archive hash. Frozen
production and archived added-driver bytes remain unchanged. Failure-path
retention is static adoption; final actual normal upgrades and race checks
against the updated restoration helper remain pending.

At `3d60b6a`, all Proposal cases passed normal 13.646 s. The corresponding
race run failed after 28.553 s: real owner reopen had already passed the durable
NextWake in two recovery cases, so their mechanical timer received a negative
duration and rejected it with `ErrWorkBounds`. No failed component recovery was
observed in those cases. The test now waits only while that actual NextWake is
still in the future; the original deadline, allowance and business oracle are
unchanged. A new normal/race run is required before claiming this correction.

Checkpoint at `25287d5`: full `TestDurableProposal` normal 12.438 s and race
28.333 s passed. Both actual old-writer consumers passed normal 11.356 s and
race 15.655 s. `make check GOFLAGS='-mod=readonly -p=1'` and fresh base-race
`go test -race -p=1 -count=1 -timeout=120s ./...` passed. The base-race target
was 16.905 s and Component 11.702 s. Full real PG Component/Source packages
passed normal 38.554/13.715 s and race 74.493/19.398 s. The separate complete
`conformance/recovery/...` package passed normal 38.009 s and race 80.735 s,
including the original two databases and previously integrated 06 process
stories. The Component/Source package selection itself does not execute those
06 SIGKILL stories. All commands were strictly serial with actual exit 0 before
the next started; direct Go invocations used count1, p1 and timeout120.
All seven actual frozen manifests passed: 27 old durable-work entries, 71 old
970 entries, 71 FINAL01 entries and three target entries; modules verified.
Current lockfile, generated code, published SQL and archive bytes are unchanged.

Checkpoint exact audit passed with 1580 unique PG schemas, 64 target directories,
244 recovery SQLite directories, 11 archive directories and 19 acknowledged
process groups absent. Duplicate transfer acknowledgments are not counted as
unique scopes. The owned overlay root remains registered with only Node's
compile cache; other unknown historical scopes are untouched. Every test,
build, database holder and child session exited before releasing the slot.

A separately adopted shared-helper correction is still pending: compiler Wait
causes must remain distinguishable, and the old-970 restored added-driver must
require the actual ErrClaim qualification rather than accepting any RunClaim
error. This will narrow the old business oracle without changing frozen
production/archive bytes. The successful old-pin upgrades do not retrospectively
prove that specific error type. The new committed helper requires its own real
normal/race consumer results and review before ticket closure. This ticket's
FINAL01 proposal driver instead deliberately requires context.Canceled, and is
not reinterpreted as an expired-claim case.

Root FULL-read and adopted shared compiler correction `d90962c`, only-picked as
`0f6d23a`, and strict old-driver conversion `4cb3717`, only-picked as `6b98eb0`.
The committed [qualification decision](legacy-driver-qualification-decision.md)
keeps both production archives immutable and explicitly narrows the restored
970 added-driver oracle. The derivative is 8572 bytes / SHA256
`aa7097a937c0defb5ee7e356b4e2ee71b7c069d7f96b514a2bdc07c07db05945`.
Existing errors.Is qualification now also refuses multiple joined causes and
an unbounded unwrap chain. Two explicit post-READY retention exits remain.
The new mechanical checks cover exact bounded source conversion and safe
joined-stage error inspection; they do not reproduce native Wait/group failure.
This ticket has not yet run them or the two related actual upgrade consumers.
No 03 control-driver or moving business implementation was picked.


Final related checks at `8f94f26` passed normal13.311 s and race15.403 s,
each actual session exiting 0 before the next operation. They run this ticket's
own FINAL01 six-state consumer and strict old-970 consumer plus the exact
bounded conversion/safe joined-stage checks. Earlier permissive-oracle results
are not reclassified. Mechanical stage inspection is not native Wait/group
failure evidence, and retention guards remain static qualification.
Final audit found 1628 unique PG schemas, 64 target directories, 244 recovery
SQLite directories, 15 archive directories and 27 acknowledged groups absent.
After all commands/children/native holders/FD/scanners had exited, seven Node
tool-cache files were inventoried with exact inode/hash and fsynced, then only
those files and empty owned directories were removed. The owned root is absent.
Late cache inventory is not represented as fixture-time immediate registration.
The test slot was formally released; no other unknown scope or WT was removed.

Both independent final axes were root FULL-read/adopted at baseline949/head8f,
31 commits/90paths, Standards0hard/0smells and Speca0/b0/c0. The reports are now
committed verbatim; tested product/source remains unchanged. Seven own AC are
resolved with [exit evidence](ticket-02-exit-evidence.md) and
[API handoff](ticket-02-api-handoff.md); root's whole03/CI/actual merged-tree
architecture responsibilities remain open. This final documentation does not
claim a new remote execution or modify any other ticket's status.
