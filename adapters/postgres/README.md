# PostgreSQL durable work storage

This adapter implements runtime Command/Job/Tx/Clock ports and the internal
Host demonstration consumer's Repository. The root Go module locks
`github.com/jackc/pgx/v5 v5.11.0` using its `database/sql` adapter. No repository
or runtime package reads environment configuration.

The host injects `Config` (DSN, schema and finite transaction/statement/lock
limits). Identifiers use restricted internal names; business values use SQL
parameters. Connections run explicit `READ COMMITTED` transactions with
`synchronous_commit=on`, finite local `statement_timeout`/`lock_timeout`, and a
finite context. Tests observe actual effective settings on transaction
connections. The tested server is PostgreSQL 18.6.

Lock order is original command key → input object → Job row. Separate advisory
lock namespaces prevent hash collisions from crossing lock levels. Advisory keys
encode `[schema, kind, tenant_id, owner_id, id]` using the Store's validated schema:
different schemas no longer deterministically alias equal owner/object identities,
while independent Stores and connections sharing a schema still share locks.
The finite `hashtext` space can still collide and serialize unrelated keys; this
does not promise collision-free progress. Unique keys and every lookup
include tenant and owner. A Job is unique per owner/object/phase. New input
advances the same Job without replacing its ID or completed revision.

The schema-bearing advisory key is a runtime coordination protocol change. For
an existing schema, stop and drain all old workers and transactions before starting
this version. Old and new binaries compute different locks, so mixed-version
rolling operation on one schema is not supported. Existing data can reopen normally;
no published SQL migration, command identity or public contract changes. Separate
schemas do not authorize two authoritative ledgers for one production owner.

The immutable migration is `migrations/host/0001_admission.sql`; its SHA-256 is
recorded by `schema_migrations` and checked on rerun. V1 contains no Claim/lease
columns. Input text uses `bytea` to preserve all valid Unicode scalars, including
zero, which PostgreSQL `text` cannot store. Command records retain their digest,
fixed receipt and minimum version/profile/method/target/subject/deadline/revision
metadata without a second copy of the payload text.

Only pgx's explicit `ErrTxCommitRollback` proves a failed COMMIT rolled back.
Other COMMIT failures conservatively return runtime `ErrCommitUnknown`, which
Host maps to the validated public outcome. No new command identity is generated.
The integration wire fixture drops a server-confirmed COMMIT response; it does
not prove SIGKILL, power loss, failover or production durability.

V2 is the real forward migration `0002_claims.sql`: it adds lease/worker/claimed
revision/epoch fields, leased/done states, binding/progress constraints, a stored
scan eligibility time with an indexed owner scope, and the demo projection fields.
`Migrate` applies V1 then V2 transactionally and verifies both stored checksums on
rerun; `MigrationVersions` reports the applied versions/checksums. Published V1,
its historical writer and dump remain unchanged. Full historical-writer upgrade
and recovery acceptance belongs to ticket 07.

`ClaimStore.Scan` uses the owner-scoped eligibility index and a LIMIT of at most
64, without first taking Job locks. The consumer tries the existing object
advisory lock, fixes the input, then conditionally leases its Job using SKIP LOCKED.
Complete and renew take object/input before Job and sample trusted owner time
after acquiring the object lock. Completion updates only completed_revision;
Trigger requires strictly newer work and preserves an active claim. Epoch increase
is guarded at bigint maximum. Expired leases cannot renew or complete; replacement
keeps Job/object identity and increments the epoch. A late claim cannot alter the
current projection. This fences only controlled database writes, not external I/O.

The next forward wait migration adds waiting state, per-input-revision immutable
policy/anchor/deadline, durable start/attempt/outcome/due facts and same-owner gates.
It preserves every existing Claim/lease binding and all published V1/V2 bytes.
Legacy policy binds only at first eligible Claim in the same short transaction;
new admission binds policy with input/Job/receipt. Defer/retry/stop release the
original Claim without closing newer work or inventing successful projection.
NextWake observes the earliest relevant future due/lease/deadline and finite
fallback. All mutating consumer paths preserve input -> Job lock order and use
one trusted owner Clock. Deploy by draining/isolating the old binary; mixed
old/new processing is unsupported. Both real adapters run one shared wait suite,
including frozen actual V1/V2 writer upgrades. No production/external-effect or
fair-quota guarantee is implied.

MaxOpenConnections may explicitly bound the pool to 1–64 (0 selects 16). The wait
suite uses one actual connection to prove waiting releases database resources.

The real retention migration adds independently retained command body tombstones
and input body state without rewriting published v1/v2 migrations. Cleanup writes
zero-length bytea and checks the successfully projected current input and done Job in
the same short transaction. The database enforces gone ⇒ stored length zero;
Host observations additionally validate the bytes actually read. command.get
reads receipt and its command-specific marker consistently; preserved v1 receipts
remain queryable after a migration failure before the new column exists.

The integration entry requires actual psql for the full PG historical dump,
checks both immutable v1 artifact manifests and records client/server versions.
Historical upgrade/failure/retry/reopen and cleanup have a common two-adapter
behavior suite; no database is dropped and checked-in fixtures are never opened
writable. Local SQLite TMPDIR should be a caller-owned local durable filesystem;
CI uses its explicit runner temporary directory.


Native restoration requires executable psql, the dedicated PostgreSQL URI DSN
and the existing Linux/CGO SQLite toolchain; it does not require Docker. The
configured native client is bounded by a 30-second context, its own process
group and a one-second pipe wait limit. A real SQL connection followed by
cancellation verifies the client exits and is reaped.

CI separately runs the explicit `integration,containerpsql` lifecycle suite;
Docker absence is a hard failure for that tool suite. Its fixed-image launcher
uses Docker's owned cidfile: CREATE must return a complete exact ID before
psql is started. Host create/start commands are limited to 15 seconds plus a
2-second kill grace; the container's psql runs under a total 10-second timeout
plus 2-second kill grace, including the original dump's SET timeout=0 commands.
The original dump is not rewritten to pretend those SETs retain PGOPTIONS
limits. libpq connection setup is limited to 5 seconds. The Go caller separately
cleans that exact registered container ID and confirms its absence within a
10-second cleanup context, including after cancellation. No image/name/time
scan is used to guess ownership.

This is a bounded known-container guarantee. If CREATE's reply is unknown and
a complete CID was never recorded, psql is not started, but an unstarted
container can remain without confirmed ownership. That actual empty-cidfile
failure window is retained as a limitation; it is not guessed or deleted and
is not described as proving every cancelled CREATE leaves no container.
