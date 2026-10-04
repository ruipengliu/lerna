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
