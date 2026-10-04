# Ticket 01 implementation evidence

Worktree `/tmp/lerna-worktrees/content-snapshots-01`, branch `codex/content-snapshots-ticket-01`, initial integration `f706fe4a61e091c4a7d28bd31137a7fc0ffed6e5` (actual clean verification).

Confirmed seams are authenticated public Content put/get and Command get, with independent original bytes and exact local object observations. These are the spec's published Testing Decisions and the root's already authorized handoff; no additional seam confirmation was requested.

The owned overlay root `/workspace/lerna-content-01-d15e5e390c46262d` was created and fsynced with its parent; ACK records dev 27 / inode 431292. `owned-scopes.log` is absolute, mode 0600, and fsynced. DSN comes only from the authorized private file into child environment and is never printed. A finite gated launcher records the actual started process group before release and records native Wait and group absence before the next build/test starts.

Locked `make bootstrap` completed status 0, actual group 1898663 absent.

First runnable red: `go test -p=1 -count=1 -tags=integration -timeout=120s ./conformance/component -run '^TestContentCorrectBytesDurablyAccepted$'` completed exit 1, group absent. It opened real PG, successfully created and registered its exact test schema, and called authenticated public `Service.Put` with finite correct alpha bytes. Failure was `correct finite bytes should be durably accepted: content publication unavailable`; compilation succeeded. Exact schema Drop and native Close completed without cleanup error. This is an initial unavailable skeleton, not a completed implementation or a previously green capability.


First full tracer green: after adopting the root/Astra concrete 1.2 shape,
`TestContentCorrectBytesDurablyAccepted` completed exit 0 and native group absent.
It observes preparing with no body, reopens real PG and os.Root handles, resumes
original durable work, observes published through Content get, reads independent
filesystem bytes `alpha\n`, reopens again, and reads original fixed accepted plus
historical published progress through 1.2 Command get. The original receipt is
byte-for-byte unchanged. All fixture handles closed and exact schema/object scope
cleanup completed without errors. One intermediate compile failure was a test
helper name collision with the frozen component fixture; it was corrected and is
not described as a business red.

The root's formal contract shape is tracked at `contract-shape-decision.md` on
integration `64c6872`. It is the adopted requirement source, not test evidence.

Current save gate: real public accepted → durable fixture Save=false → Step
initially failed the independent OS assertion because bytes were written. After
adding a pre-I/O current gate and retaining the post-I/O gate, this test and the
normal full tracer both pass. Direct-source read/process/save, current retention,
publish deadline and admitted retry ceiling are now checked at the actual stage.
Database time is re-sampled after policy row-lock waits. No cleanup, holder, or
full propagated-source claim is made for later tickets.

Exact decoded body classification: Go and TypeScript each produced a runnable
red (`schema_invalid` for canonical 262145 decoded bytes). Both now return
`input_over_limit`, with normal alpha as control. TS initially had two missing
exports; those were link failures and are not described as business reds.

Local lifetime A/B scan: the partial Open mechanical test first failed because
startup/close causes and an unknown holder were discarded. The local module now
retains nonnil partial holders and every child FD close result; first close unknown
is sticky. Tagged mechanical tests also cover child-read Close unknown with actual
real Close observed separately, an ordinary missing-object error that still closes
normally, and a held invocation whose gate/drain waits time out but whose native
closure is not claimed until actual return. These are injected diagnostics/sync
points, not actual native Close failures. The world retains setup directory/parent
close causes and exact identity before destructive cleanup. All observed normal
runs closed and cleaned exact registered roots.

101 independent shared 1.2 wire fixtures passed real Go↔TS canonical roundtrips
in forward order. The first pass uncovered AJV's default uniqueItems comparator
calling valueOf on strict parser null-prototype objects; that failed the legal
64-source normal case. Version-local canonical JSON equality now supplies exact
uniqueItems semantics for the finite no-number contract subset. Old schemas and
fixtures remain unchanged. Reverse order and broad locked checks are pending.

Root's a5 followup required A1/A2/B1. Two independent runnable public reds
confirmed the source findings: after a mechanical temporary object failure,
post-I/O policy tightening was discarded by Defer; and a scheduler delay after
actual Claim commit permitted new native bytes after original lease expiration.
Both now pass with their normal real-adapter controls. Current retention and
revision are saved in the same transaction before Defer; actual I/O deadline is
bounded by original Claim.LeaseUntil and already-expired contexts never enter
Objects. The two local error branches now let finite owned cleanup join the
actual invocation instead of blocking on a bare receive. The lifetime test passed.

Actual PG policy FOR SHARE waiting was observed through pg_blocking_pids for
our exact owned blocker. Publication waiting across policy expiry writes no
bytes. A separate admission wait produced a runnable red: accept_before used a
pre-wait database clock. Admission now samples trusted time again after its
locks/capacity checks. These tests, retry tests, and normal controls passed with
native exit 0 and confirmed absent native group.

Public exact identity/trace replay/version separation, empty/max-version content,
binary octets, EOF zero range and overflow refusal passed. Independent shared
Command digest and version-identity goldens passed in Go. Real missing/damaged
published objects remain unavailable through repeated full-validation range
queries, keep historical published progress and never repair independent files.
Legacy 1.0/1.1 readers use current durable reader policy and their unchanged closed
codecs: new accepted and unrepresentable new rejection reasons remain unavailable;
representable original expired rejection and not_found roundtrip losslessly. An
initial test wrongly expected old codecs to represent the new integrity reason;
that expectation was corrected, and is not claimed as a business red.

A finite staging test produced a real red because failed versions' retained bytes
were excluded from total capacity. Total staging now counts every retained body;
only the preparing count uses publication=preparing. Bounded normal admission,
preparing refusal and failed retained-staging refusal pass. The combined Content,
digest and local lifetime run completed native exit 0, group absent. No whole-ticket
acceptance, full inherited policy closure, cleanup completion or SIGKILL evidence
is claimed here.

## Locked checks and intermediate boundary (pending read-gate correction)

The first complete `make check GOFLAGS='-mod=readonly -p=1'` reached new legacy CLI
bookkeeping after lint/generated/25 tooling tests/two generator suites/all Go units/
44 TS tests had passed, then failed actual exit 2 with its native group absent.
The cause was this implementer's tool code reading a result from requireBuild's
successful void return, not a business failure. Its exact compiler-only scope
`lerna-contract-WsTf9u` (dev 27/inode 432932) was conservatively retained.
Affected `make test-contract build` subsequently completed actual native exit 0:
158 old1.0, 89 old1.1 and 101 new1.2 shared fixtures ran real Go↔TS roundtrips in
both orders; Go and TS builds completed. A final accurate full `make check`
then completed actual native exit 0/group absent, including every original tail.
Logs are retained under the acknowledged overlay root as `check.log`,
`contract-build.log` and `final-check.log`. No failed run is renamed a success.

Necessary mechanical CLI/generator changes preserve exact root identity, fsynced
scope/actual native-group/producer ACK, all native runner/build close evidence and
short-lived FD action/Close causes. Ownership/ACK failures remain sticky and retain
scopes. The old generator fixture's normal source inputs now include the explicit
new1.2 config; `uniqueItems` remains unsupported in frozen old configs. Old golden
and source bytes are unchanged. This is tooling safety, not a protocol or future
resource framework change. Initial generator/CLI startup unknowns are not washed
away by later success.

Affected race run used `-p=1 -count=1 -race -tags=integration -timeout=120s` for
Content domain/codec/local/world/public Component, including old real Decision
identity/legacy observation control. Actual elapsed package times: domain1.111s,
codec1.946s, local1.118s, world1.235s, Component15.561s; native exit0/group absent.
It includes independent empty/binary/real256KiB publication, exact ranges, explicit
policy actions/direct-source current gates and caps, wait/retry/body faults and
no-clobber controls. Fresh/repeat/checksum migration passed and native reopen worked.
Protected PG/SQLite V1/V2 archives verified every recorded SHA256. Historical PG
and SQLite normal upgrade plus version2 refusal/rollback/original retry passed
in0.851s, native exit0/group absent. No product limits, leases or timeouts were
expanded for these checks.

The retained compiler-only scope was closed only after original ACK identity,
original group2011374 actual absence, original await normal-return proof through
requireBuild's actual Wait/all-pipe/group confirmation, and independent observation
that it contained only `go-values`. The TypeError occurred after that original
successful await and before any producer started. This differs from a native
closure unknown. Exact audit/removal was recorded durably; no prefix, PID/time
heuristic or unrelated historical scope was deleted.

Fresh exact resource audit observed177 registered groups (only the current auditor
excluded, then its native exit0/group absent),69 registered PG schemas all absent,
8 exact lock-holder backend PIDs all absent,160 recorded object/contract/generator
roots all absent. The overlay root and its evidence files are retained. Audits
query only ownership infrastructure, never private business tables as an oracle.
Protected original02/03 unknown resources were neither inspected for deletion nor
touched. Audit output is `resource-audit-after.json` under the owned root.

The root has now adopted a necessary read-gate decision. This clean intermediate
boundary is **not delivery or 8AC acceptance**: successful native object Read must
be followed by a current read/disclose gate, and Get/GetCommand must recheck this
read's AcceptBefore after all blocking locks. The adopted decision is source review,
not injected failure evidence; the sole implementer will run public red→green with
normal controls and fresh affected checks before a final delivery pin.

## Independent intermediate findings: actual correction evidence

The clean intermediate pin remains `92d27992f37f0a425621bc75c6d5b4a073fa06a6`;
initial Spec/Standards reports are preserved separately without rewriting their
fixed pin or findings. Root adopted the two current read gates, direct-source
matrix, association/replay rules and the narrow tooling ownership extraction.

New public target read/disclose revocation during real whole-object Read produced
runnable business red (0.848s, exit1/group absent), then green (0.878s, exit0/group
absent). The finite pause is a mechanical ordering seam after actual native Read,
not a native storage fault. A normal control crosses the admitted AcceptBefore
while context and current policy still permit final disclosure.

Real PG policy/version/reader row locks crossed the initial read cutoff for
not_found/preparing/failed/published/metadata-mismatch and Command found/absent;
reader expiry was independently exercised. Actual runnable red was5.788s/exit1;
the combined corrected target gates/clock suite was6.832s/exit0, tool session91923.
The group was actually absent after each run.

Two actual published sources and a derived target supplied independent byte normals.
Source read/disclose revoked before or during Get and current source retention expiry
produced runnable red in tool session62056 (1.845s/exit1), then green in7934
(2.103s/exit0), groups absent. Source process/save=false while read/disclose remain
permitted is a separate positive control. One preceding helper function-type error
was compile1, not business red. A1.525s intermediate run still returned forbidden
instead of expired for known authorized retention expiry; CheckPolicy now retains
that scoped action policy's cap for the Service's precise expiry decision. Policy
validity/subject/action remain fail closed.

New-command source process/save denials, legal association tightening followed by
actual reopen/policy widening/expiry, and original Command advisory-lock wait past
reader expiry produced runnable red20724 (2.119s/exit1) then green83706
(2.207s/exit0), group absent. Original fixed receipt is unchanged, historical Job
progress is preserved, and independent native object count changes by zero.
Additional normals verify a preparing alias at the full6-byte staging limit and
an eligible failed-version alias after its original publication deadline, without
new responsibility or bytes. The last of two real source-version locks also crossed
Get admission, followed by a successful fresh normal read.

Local Open with a real absent absolute path lost os.ErrNotExist despite a normal
open/close control: actual public red0.016s/exit1 then green0.012s/exit0, groups
absent. Lstat/EvalSymlinks now preserve native causes with ErrUnavailable. Fixture
setup owns ledger/parent FDs immediately; every registration/Sync/Close error seals
destructive cleanup. Mechanical ledger/parent first-close controls independently
observed actual Close, verified repeated World.Cleanup retained the exact root and
original diagnostic, then the fixture removed only its independently closed root.

The ownership module's original9 mechanical interface tests passed native0, then
10 passed with the registry environment absent (including its real default ledger).
An intermediate absent-env run failed a test's external removed-ACK assumption;
it was an expectation/control error, not a native fault or new business red. The
corrected test explicitly owns a second acknowledged external-ledger fixture.
Normal primary-failure cleanup, action+Close causes, write/Sync/parentSync ACK
unknown, original entity missing observation, exact identity mismatch, initialization
failure, remove failure and removed-ACK truth are checked through the interface.
Makefile includes this suite. Five actual consumer entrypoints await the final
locked-check result below; historical entry greens are not substituted.

The first broad affected normal run74708 exited1/group absent: Component23.950s
had only the older source.Read=false Get-failed expectation, now superseded by the
adopted current-read gate. It was corrected to public Get forbidden plus public
Command historical failed; process/save-only denials still permit failed observation.
Fixture0.134s and local0.074s passed. This is recorded as an expectation correction,
not a new implementation business red.

The final affected race run95320 completed exit0/group absent: Component40.887s,
codec1.678s, local1.153s, fixture1.218s. The domain package compiled under race in
1.049s but the selected test pattern had no domain tests; this is not advertised as
an additional domain test. Its existing identity tests run in the full locked check.
Actual output is owned-root `review-fix-race.log`, distinct from earlier tool-only
read-gate/clock observations, for which no nonexistent log path is claimed.

The final modified-source locked `make check GOFLAGS='-mod=readonly -p=1'` completed
actual native0/group absent, tool session33660. Output is owned-root
`review-fix-check.log`. It includes formatting/vet/typechecks, generated equality,
35 Node tooling tests, both generator real success/refusal/frozen-output scenarios,
all Go units,44 TS tests,158 old1.0+89 old1.1+101 new1.2 fixtures in both actual
Go↔TS orders, and the final Go/TS build. This supersedes neither the earlier
interrupted exit2 history nor the initial findings; it establishes the corrected
five consumers' actual complete entry coverage. No protected schema, old golden,
published SQL or archived fixture object differs from integration64c6872.

The product/tool/test source was fixed in a separate commit
`46d6ca2` after these checks. Remaining documentation is separated so the reviewed
source tree and tested source qualification can be checked without claiming a
fresh test run for doc-only changes. Latest integration is still64c6872, verified
clean and already an ancestor of this own branch.

Fresh audit after those native exits registered279 exact groups,230 own schemas,
58 exact blocker PIDs and407 exact object/contract/generator roots. Every recorded
schema/PID/root was absent; every group except the actual running auditor was
absent. Auditor2103477 then completed native0/group absent. Native DB and registry
Close succeeded. Original overlay ACK remains dev27/inode431292. Valid standalone
machine evidence is [ticket-01-resource-audit.json](ticket-01-resource-audit.json):
leading raw JSON was parsed, null empty lists normalized to[], and wrapper native
metadata recorded separately. The earlier raw *.json files with native suffixes
remain historical local artifacts and are not copied as invalid machine JSON.
Own overlay/logs and this worktree remain; original02/03 protected unknown scopes
were not deleted or guessed. No native process remains held by this worker.

Ticket status remains claimed and its8 boxes remain unchecked pending root's
actual independent final Spec/Standards acceptance. Source and executed proof are
ready for that review; this record does not claim full slice04 advertisement,
root merge/push/CI, later closure/cleanup/fence/holder/SIGKILL, or power-loss/S3 proof.

## Final-candidate target policy finding and ordering correction

Root's fixed46/delivery72 architecture review and both independent axes found one
necessary P2: target read/disclose policy refs were not bound to the exact stored
declaration. Candidate72 is preserved as historical and was not accepted. The
fixed reports retain their pins and remaining finding; optional native-result
aggregation duplication remains KEEP, without any tooling delta.

Public hash/media/length wrong trusted policy, each before read and during a finite
pause after actual native whole Read, yielded runnable red54796: Component0.960s,
actual exit1/group absent. Minimal full-ref binding after caller mismatch yielded
green55134:1.122s, actual exit0/group absent. Those six cases included normal exact
policy, correctly authorized incorrect request integrity, unchanged independent
original bytes, fixed receipt/history and restored policy normal read. Exact logs
are owned-root `target-policy-red.log` and `target-policy-green.log`.

Root/Astra then corrected the suggested order: authorization must precede existence
and mismatch observations, using actual Record.Ref if present and request.Ref if
absent. Supplementary public red16987 produced integrity for policy B/request C or
B/actual A and not_found for policy B/request A/no record. Component0.526s, actual
exit1/group absent. Moving the same shared authorization ahead of these observations
produced combined green44631:1.372s, actual exit0/group absent. Correct policy A/
request B/actual A remains integrity; exact authorized absence remains not_found.
Logs are `target-policy-order-red.log` and `target-policy-order-green.log`. The
initial six-case green is not mislabeled as the completed authorization fix.

The corrected authorization-first source was fixed as
`14ead831b8834892da5ff13a9b883494247f327f`, exactly two source paths changed from72:
shared observe and public target-policy tests. No tooling/contract/SQL/source-policy
closure delta was introduced. Fifteen affected public Get groups (including both
new target suites, deadlines/direct sources, reopen, incorrect declaration,
empty/binary/ranges, real256KiB and damaged/missing byte normals) completed affected
race47356: Component22.361s, actual native0/group absent. Log is
`target-policy-race.log`; no unrelated broad PG/race suite was repeated.

The new source's complete locked check46700 then completed actual native0/group
absent, including original35 Node tools,44 TS, both generators and158/89/101
fixtures in both Go↔TS orders plus format/vet/generated checks and final Go/TS
build. Log is `target-policy-check.log`. Some unchanged Go units were cached and
are not relabeled as fresh noncached runs. New integration tests and affected race
above used explicit count1/p1/timeout120 and real PG/local objects.

Independent fixed14 source review then identified two stale plain-English test
comments describing the superseded order. Comment-only follow-up
`4aceecefb45125597d45db75aa757fe1aab96eb9` changes exactly those two comment lines;
no directive, executable code or test data changed. Exact Git diff was checked,
so tested executable qualification carries from14. Literal test-file blob equality
is not claimed; no additional native test was run for this comment-only correction.
Root separately reviews the required documentation accuracy.

Fresh audit after check native completion observed353 original registered groups,
289 exact schemas,65 original blocker PIDs and518 exact object/contract/generator
roots. All live/residual arrays were empty. Actual auditor2147781 then exited0 and
its group was absent; native DB/registry Close succeeded. Current valid standalone
machine record is
[ticket-01-resource-audit-target-policy.json](ticket-01-resource-audit-target-policy.json),
with actual source4ace/tested-executable14 qualification. The older audit record
remains historical, unchanged. Root ACK remains dev27/inode431292. No original02/03
unknown scope was guessed, deleted or reclassified. Latest integration64c6872 was
again actually clean and already an ancestor of the own source branch.

This supersedes72 as the delivery review candidate only. All8 remain claimed and
unchecked until root accepts independent fixed source and final documentation;
no full slice04 advertisement, push/root merge/CI or later ticket completion is
claimed by these results.

2026-10-04，root最终接受8AC并正式合入eba167d，见[首票退出](ticket-01-exit-evidence.md)。原pending/失败按当时状态保留；worker已明确释放LOCAL native槽，未新增native测试或清理。
