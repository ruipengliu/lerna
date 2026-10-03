# Real v2 writer input with an unfinished Claim

This is fake demonstration data written by the actual v2 implementation at
`b1674b2d0252da73f3dfd753857484597dc0a080`. The harness is preserved in
[../v2-writer.go.txt](../v2-writer.go.txt). It compiles against that immutable
root module, calls Host Record, claims revision 1 for one second, and records
revision 2 while the original revision 1 Claim remains leased. It never inserts
old rows or constructs an old schema manually. `writer-observation.json`
contains the actual returned Claim/input snapshot and independent Host observation.

Go 1.27.1, pgx/v5 5.11.0, PostgreSQL/pg_dump 18.6; READ COMMITTED,
synchronous_commit=on; transaction 3s, statement 2s, lock 1s. All writer work uses
one trusted clock at `2026-10-03T20:00:00Z`. The original lease ends one second
later; the original acceptance deadline is in 2101. This is a migration fixture,
not a production clock or timeout recommendation.

`database.sql` is the complete plain pg_dump export: original DDL, constraints,
indexes, migrations and all command/input/Job bytes. It is neither reconstructed
from the observation nor an inserts-only approximation. `writer-schema.txt`
records its exact owned generation namespace; tests substitute only that identifier
and omit CREATE SCHEMA because the fresh random scope is already created and
registered. The driver loader executes all SQL and native COPY, interpreting
only psql `restrict`/`unrestrict` directives as client transport commands. It does
not change facts or table definitions. The fixture loader requires no psql;
the complete integration suite's separate historical-v1 tests retain their
psql prerequisite.

The writer created one random dedicated test schema and reported cleanup failure
as failure. The export occurred before its finite 5s registered-scope cleanup.
No caller database or smoke namespace was dropped. SHA256SUMS includes the
source harness, exact 0001/0002, writer revision, metadata, full dump and observation.
Reproduce into a new output directory with
`scripts/generate-durable-v2-fixtures.sh NEW_OUTPUT_DIRECTORY`, an explicitly
supplied dedicated LERNA_TEST_POSTGRES_DSN and PostgreSQL18.6 pg_dump. The generator
needs Git history only for reproduction; required tests consume these frozen
files and do not build or fetch historical source.

On upgrade the original leased r1/work r2 relationship must survive. A new worker
waits for the original lease; at expiry it claims r2 with a higher epoch and binds
legacy policy at that takeover's trusted time. Late r1 success is rejected. This
proves local database recovery, not physical process termination or external effects.
