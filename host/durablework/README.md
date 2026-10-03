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

`Observe` returns input and pending Job revisions. `contract.GetCommand` resolves
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
