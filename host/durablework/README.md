# Internal durable work Host

`host/durablework` assembles explicit owner, runtime/storage ports and an exact
trusted permission table. `internal/durableworkdemo` owns the demonstration's
record preconditions and consumer Repository interface. This is an internal
same-version Host seam, not an Application SDK, Task or production credential
service. The public 1.0.0 method manifest is unchanged.

`host-durable-work-1 / host / durable_work.record` accepts only `{ "text": string }`
(up to 65536 Unicode scalars), preserving exact input. Success is `applied`: input,
fixed receipt and pending `project` Job committed together. New commands require
accurate revision preconditions. Current permission is checked before original
key comparison; original key comparison precedes a new command's deadline.

Inject finite contexts and short transaction configuration. A transaction token
belongs to an exact adapter/database binding and owner, and expires after its
callback. Repositories reuse it; callbacks may not wait on external I/O or users.
The default PG clock reads database `clock_timestamp()` after original-key locking.
SQLite uses the trusted device UTC wall clock. Tests may replace only the clock boundary with a deterministic shared clock.

The separate processing consumer requires the scheduling storage ports;
admission-only adapters need not implement them. It uses one trusted owner clock shared by all workers, with
PostgreSQL database time as the default. A worker-local wall clock is not an
expiry authority. Claims use microsecond UTC precision, a batch of 1–64 candidates,
and a lease of 1ms–5min. Caller contexts must have finite deadlines.

`Claim` returns immutable stage input and the original Job/worker/revision/epoch/
lease binding. `Project` computes exact UTF-8 SHA256 outside the transaction.
`Renew` returns the new lease token; further renewal/completion must use that token.
After explicit `Start`, `Complete` conditionally commits the projection and only the claimed revision;
newer work remains ready. Expired unreplaced and replaced claims are rejected.
The demonstration has no external actions; leases do not fence external effects.

`Observe` returns input, Job revisions and the optional project observation. `contract.GetCommand` resolves
the exact original owner and returns its fixed receipt with progress `none`.
`none` says nothing about completed work. A lost COMMIT confirmation retains the
original command reference and `query_or_retransmit_original`; pre-COMMIT errors
roll back, while unproven COMMIT errors remain unknown.

Run `make test-integration` with explicitly supplied `LERNA_TEST_POSTGRES_DSN` for
a dedicated PostgreSQL 18.6 test database. Both PostgreSQL and real file SQLite
run the shared admission suite. PG tests own random schemas; SQLite tests own
temporary files. Cleanup is restricted to those ranges. Missing configuration,
services or Linux/CGO support fails. `make check` requires no external database.
See [PG storage](../../adapters/postgres/README.md),
[SQLite storage](../../adapters/sqlite/README.md) and
[v1 upgrade input](../../conformance/fixtures/durable-work/pg-v1/README.md).

Scheduling now uses `NewScheduledWorker` with an explicit, finite trusted worker
allowlist and the complete ScheduleRepository/ScheduleStore bundle. The old
`NewWorker` signature remains for compile compatibility and fails closed without
those explicit permissions. `Start` confirms eligibility and increments the
original revision's durable attempt count; only then may a caller calculate
`Project` and submit strict `Complete`/`Finish`. `Complete` never supplies a missing
Start. `Process`, bounded `Step` and cancellable `Run` organize the same sequence.

A validated fixture policy is selected by exact owner/object at admission and
bound once per input revision, with immutable lane, absolute execution deadline,
finite attempts/backoff and an optional same-owner `gate_revision_at_least`.
Ordinary default: pure project, 5min, 3 starts, 100ms base/5s maximum retry,
1s gate recheck. No payload, object-name convention or environment variable
chooses policy. A newly recorded revision cannot replace a claimed policy.
Trusted `ScheduleControl=true` must be explicitly assembled before `AdvanceGate`
or exact-revision `Stop` can be used; these are internal fixture controls.

`ObserveSchedule` exposes the original revision's policy, adoption source,
deadline, attempt/start registration, durable condition/outcome/due, original
Job progress and effective Claim. Waiting and retry save facts then release the
Claim, worker, connection and transaction. All notifications may be omitted.
`Step` returns a bounded processed count and a future trusted next wake; `Run`
waits on a positive relative timer derived from that same clock, with fallback
at most 1s. A host wall-clock offset cannot turn database-clock wakes into an
immediate polling loop. Healthy work continues while a gate waits.

Completed revision means that responsibility closed, including permanent failure,
expiry or trusted stop. Only a successful observation changes the successful
projection revision/hash; failure preserves prior real success. A closed older
revision leaves newer work ready. Current worker revocation, deadline, stop,
Claim/epoch/lease and prior Start are checked at the actual processing boundaries.
Process shutdown returns Claim best-effort in a finite transaction; an unconfirmed
return retains the original responsibility for lease takeover.

Upgrade requires draining or isolating the old binary. Legacy policy is absent
until first eligible takeover, then binds once at trusted `adopted_at` + 5min;
it never uses old command acceptance time or refreshes on restart. Existing v2
Claims/epochs survive migration and wait until their original lease expires.
No missing policy permits execution. Lane capacity, quotas and fairness remain
spec02 ticket06. Public 1.0.0 and record receipts/progress `none` remain unchanged.

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


Current admission and processing require a durable finite pool. Trusted assembly
sets `PoolControl` only on its configuration/control Host, explicitly installs
`demo.DefaultPool(id, exactOwners)` or a validated shared configuration with an
expected config revision, and then runs qualified workers. DefaultPool is a
proposal, never implicit installation. `NewPoolWorker` composes declared owners
into independent lane loops and trusted no-quota maintenance; its actual dispatch
rechecks database membership/reservation/FIFO at the selected owner's transaction.
See [finite pool behavior and scope limits](../../internal/durableworkdemo/README.md).
Read/replay authorization and original digest decisions remain independent of new
pool registration. This is internal demonstration behavior; public contract
1.0.0 remains command.get only.
