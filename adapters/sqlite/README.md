# SQLite durable work storage

This adapter implements the same internal Host admission ports as PostgreSQL:
Tx, Clock, CommandStore, JobStore.Trigger and the demonstration consumer's
Repository, plus the separate ClaimStore and project WorkRepository. It
claims exact stage input, renews bound leases and commits projection progress.
Scheduling, waiting and quotas remain later tickets. The public 1.0.0 contract is unchanged.

The root module locks `github.com/mattn/go-sqlite3 v1.14.52`. Build with Go 1.27.1,
`CGO_ENABLED=1` and a C compiler using the driver's bundled SQLite amalgamation.
Do not use the `libsqlite3` build tag or personal SQLite header/library paths.
Actual tested runtime is SQLite **3.53.4**, source ID
`2026-07-24 19:02:57 bf7c7f30031888f4e796e429ab3978879485813aaca6f641c7b33e4e09459bcc`.
The CLI version is not the product library version.

The host injects a real file path and finite transaction/busy limits. Each
connection's driver DSN explicitly configures WAL, synchronous FULL,
foreign_keys ON, a finite busy_timeout and BEGIN IMMEDIATE. Each transaction
observes and verifies those effective settings before invoking its callback.
SQL values use parameters. Input text is BLOB, preserving valid zero Unicode
scalars. INTEGER revisions retain all positive int64 values. Persisted UTC
created_at, updated_at and due_at use fixed **nine-digit fractional seconds**
(`2006-01-02T15:04:05.000000000Z`), preserving nanosecond instants and permitting
accurate chronological ordering of this uniformly encoded text. All scan, Claim, renew and completion comparisons bind this same encoding,
including exact lease token equality. Driver-default time.Time values are never
bound as SQL time operands. No floating point date conversion is used. The
worker retains the common UTC microsecond Claim precision.

A context-aware single-writer coordinator serializes whole transactions. It
holds no unique work responsibility: input, immutable receipt and Job commit in
one file transaction. Tokens bind exact Store/database, owner and active
transaction lifetime. Every key/query includes tenant and owner; sharing a file
does not authorize an implicit transaction across owners. Reads currently use
the same short transaction path, so they share its contention limits. The trusted
device Clock is the Host's UTC wall clock; it is not database/CLI time or a claim
that device clock tampering is prevented.

Writable Host exclusion currently supports **Linux only**, using nonblocking
kernel flock on the database inode for the Host lifetime. A second writable
Host/process receives ErrWriterActive before database use. This is stronger than
MaxOpenConns(1) or a process-local queue. Closing or process exit releases the
kernel lock; no stale lease or timeout file can grant a second writer. The
supported file is a stable local regular file; database hard links are rejected.
Do not remove or replace an open database or use a network filesystem. Other
platforms return an explicit unsupported error. Actual evidence is Linux amd64,
Debian GCC 14.2.0, local overlayfs; macOS, Windows and network filesystems have
not been validated.

Close cancels active/queued transaction contexts and waits for the **entire
Within callback and transaction** to exit before closing the database and
releasing flock. The wait is bounded by TransactionTimeout. Callbacks must obey
their finite context. If one fails to exit, Close returns ErrCloseTimeout and
retains database ownership; retry Close after the callback exits. This prevents
a replacement writer from overlapping an old callback or late commit. No
unbounded cleanup goroutine or separate lease responsibility is created.

The immutable first migration is `migrations/host/0001_admission.sql`, with its
SHA-256 recorded and checked in schema_migrations. V1 contains no lease/Claim
columns. The real v2 migration rebuilds the ready-only Job table with bound
Claim columns, lease eligibility scan index, progress constraints and projection
columns. Its checksum is verified along with v1 on every migration run; both
apply atomically in a finite transaction. MigrationStatus reports the latest
version and MigrationVersions preserves all applied identities. COMMIT
errors conservatively return ErrCommitUnknown, preserving original identity;
pre-COMMIT cancellation and busy errors roll back without a receipt.

Run `make test-integration` with a dedicated PostgreSQL 18.6 DSN injected as
`LERNA_TEST_POSTGRES_DSN`; both real adapters are mandatory. SQLite uses owned
temporary files, with finite subprocess/cleanup deadlines. Missing PG config,
services, Linux/CGO support or a C compiler fails instead of skipping. Base
`make check` needs no external service. Shared admission assertions cover both
adapters. The same work suite covers exact snapshots, both concurrent commit
orders, bounded batches, all Claim bindings, expiry with no replacement, epoch
replacement, close/reopen and the original fixed receipt. Exact due/lease
boundaries use explicit zero/fractional seconds and microsecond values.
SQLite-specific tests cover effective connection settings, independent
process exclusion/reopen, real SQL busy locks, cancellation, migration identity
and close/drain boundaries. These tests do not prove power-loss durability,
SIGKILL recovery, external effects or production failover.

`scripts/generate-sqlite-v1-fixture.sh IMMUTABLE_GIT_REVISION NEW_OUTPUT_DIRECTORY`
archives that exact Git source into an independent temporary directory, builds
its actual Host writer and retains the closed complete SQLite database, command
corpus, migration, Host observation and exact checksums. It needs no SQLite CLI.
The retained v1 file is copied into a fresh owned file and upgraded through the
actual v2 migration, preserving its original receipt/input/Job and completing
its project Claim. Full migration failure/reopen recovery remains ticket07.
