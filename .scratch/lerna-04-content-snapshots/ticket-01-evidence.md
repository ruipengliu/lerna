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
