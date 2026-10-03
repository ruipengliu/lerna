# PostgreSQL admission storage

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
lock namespaces prevent hash collisions from crossing lock levels; collisions
within a level only serialize unrelated keys. Unique keys and every lookup
include tenant and owner. A Job is unique per owner/object/phase. New input
advances the same Job without replacing its ID or completed revision.

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
