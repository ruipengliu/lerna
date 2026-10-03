# Internal durable work demonstration consumer

This package is the A-stage, same-version Host demonstration consumer. It owns
`durable_work.record` schema validation, exact trusted permissions, input
revision preconditions and the minimal input/observation Repository interface.
It is not a public SDK, Task domain or general business framework.

The current consumers are `host/durablework` and the fixture/real-database harness.
PostgreSQL and SQLite implement the consumer-owned Repository alongside runtime ports;
the host explicitly injects the same owner/database transaction bundle. This
package imports neither host/cmd nor a concrete adapter. It preserves exact
text and creates pending project responsibility. `Worker` consumes its separate
`WorkRepository` and runtime ClaimStore ports. It fixes a project input snapshot
in the claim transaction, then `Project` computes SHA256 of the exact UTF-8 bytes
outside any transaction. Explicit qualified `Start` registers a durable attempt before computation.
Strict `Complete` writes `inputRevision`/`textDigest` and advances
only the claimed Job revision in one short transaction. Derived work never
increments the input revision.

See the [Host seam](../../host/durablework/README.md) for accepted commands,
query authorization and tests, and [storage](../../adapters/postgres/README.md)
and [SQLite storage](../../adapters/sqlite/README.md) for transaction/SQL behavior.
The same Worker runs against PostgreSQL and SQLite through these ports.
Validated per-revision fixture policies, durable gates, finite retry/closure and
Start/Finish consumer rules live here. runtime handles Claim scheduling mechanics;
the host only assembles them. Durable finite pool quotas and fair selection also
live at this internal Host seam. Success projection
and completed responsibility are separate observations; no Task is created.


Cleanup is a separate trusted internal capability, default denied by the exact
SubjectBinding/OwnerRef permission table. `Host.Cleanup(ctx, originalCommandRef,
expectedInputRevision, trustedSubject)` accepts only the original applied record
and its current successfully projected revision. It locks command → input → Job,
requires done with no Claim or newer work, and atomically clears actual text
bytes and marks that input plus that individual command gone. It preserves the
original digest, metadata, fixed receipt, Job ID/revisions and projection. Empty
present input and gone input differ by `Input.BodyGone`; `StoredTextBytes` is
computed from the bytes actually read and a real database constraint rejects
gone with nonzero stored bytes.

Public command.get returns gone for that exact command. Original raw Record
retransmission still returns its fixed receipt; a changed digest still conflicts.
A new command with the exact current expected revision stores a new present
body and adds work to the original Job. Old command tombstones stay gone and
repeated old Cleanup cannot touch the new body. No tombstone collection, history
body table or generic TTL is provided. This clears live records, not forensic
copies in WAL, MVCC pages, backups, replicas or caller memory.


The version 5 migration introduces finite durable pools without changing public
1.0.0 or historical writer artifacts. An explicitly trusted Host with
`PoolControl=true` calls `InstallPool(ctx, config, expectedRevision)` before new
admission/processing; `DefaultPool` proposes a finite configuration but never
installs itself as a nil fallback. Its defaults are ordinary queue/concurrency
64/4 and control/reconciliation 16/1, with matching tenant quotas. Declare
1–64 exact OwnerRefs and three lane limits; each declared tenant has each exact
lane quota, including zero to pause that tenant's starts. Config updates use an
accurate revision. Concurrency cannot shrink below current live Claims; queue
shrink preserves overhang, and unfinished members must drain before removal.

The current schema/file briefly serializes registration and pool transactions
under one registry coordinator (PG schema-qualified advisory lock; SQLite
BEGIN IMMEDIATE). No computation, timer or network runs while holding it.
Admission lock order is command -> coordination -> input -> Job -> schedule;
work/control/maintenance starts at coordination. Original authorized-key replay
and digest conflict precede configuration checks. A new key without config
returns dependency_unavailable without saving any decision; configured queue
backpressure preserves existing precondition/expiry decisions and rolls back
input/schedule/Job/receipt. Active Job revisions reuse one unfinished slot;
done-to-ready needs a new slot. All current Host Worker entry paths enforce the
same durable membership/reservation, including strict Start/Finish/Complete and
Renew. Raw storage mechanisms do not grant consumer processing authority.

`PoolWorker` chooses declared owners through a per-lane persistent tenant FIFO.
Existing eligible waiters keep their order; newly/re-eligible tenants join the
tail, tied by earliest due then stable identity. Global lane saturation pauses
the queue; tenant saturation exits it until eligibility returns. Successful
Claim/reservation/allocation sequence/move-to-tail commit together. The selected
owner rechecks this FIFO in its own transaction. Each page holds at most 64
metadata candidates, with persistent due/Job keysets and a finite round boundary;
input/Job lock skips advance the cursor. An unresolved head stays ahead of
already-served tenants. In a fixed continuously eligible tenant set N<=64,
healthy members get an allocation in N successful lane opportunities; this is
conditional opportunity fairness, not a wall-clock SLA. `ObservePool` reports
config, FIFO, waiting anchors, last allocations, live Claims and unfinished queue.

`PoolWorker.Run` owns three separately cancellable lane loops, so ordinary live
Claims do not prevent actual control/reconciliation projections. The trusted
pool control assembly also provides `Maintain`: one member and at most 64 Job
candidates per service opportunity, with persistent page/high-water metadata.
It can close exact expired/stopped responsibility without execution quota,
Claim, attempt or success hash, fence its matching old Claim, and retain newer
revision responsibility. A different-revision live Claim remains authoritative;
latest terminal work closes after its completion or original lease expiry.
An exhausted last start can close after its live Claim expires, never while that
last permitted start is still valid. Missing config prevents maintenance and
retains responsibility. Only drained/removed members' mechanism cursors are
pruned; command/Job history remains. The fallback is finite, and service/clock/
finite-candidate/no-permanent-lock assumptions apply.

Pool dispatch compares a durable generation nonce plus an explicit tuple of
backend and configured route/schema or validated absolute SQLite path. Same
scope independent PG Stores agree; another DB/schema/file and a complete copied
SQLite file do not acquire this pool's processing permission. The nonce alone
is copied with backups and is not a proof of arbitrary forks' physical identity.
Route aliases are not inferred equivalent, and DNS/server fork/old-connection
fencing still needs trusted deployment/drain; no physical identity probe or
production isolation claim is added. Deploy by draining older binaries without
these gates. Current command fixture assemblers install an explicit finite pool;
historical export scripts still build their immutable V1/V2 source commits.
