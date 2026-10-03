# Real PostgreSQL v1 writer input

This directory preserves the actual pre-Claim writer's commands and, in its
provenance follow-up commit, its database export. It is fake demonstration data
in a dedicated test database, never a dump of development smoke data.

`commands.json` submits `applied-original`, submits a new fixed expired command,
and retransmits `applied-original` unchanged. The real Host writer verifies its
receipts and observes input revision1 with the same pending Job, work_revision1,
completed_revision0 and state ready before exporting. It never constructs old
rows with manual INSERTs or lease columns.

After the v1 implementation is committed, generate from its immutable commit:

```bash
# Supply your dedicated test DSN through the environment; do not commit it.
export LERNA_TEST_POSTGRES_DSN='postgres://test-user:test-password@127.0.0.1:5432/dedicated_test?sslmode=disable'
scripts/generate-pg-v1-fixture.sh IMMUTABLE_V1_COMMIT /tmp/pg-v1-export
```

The script archives the exact Git source into a separate temporary directory,
builds the historical root-module writer with locked dependencies, runs it,
and records the writer commit and SHA-256s. Go 1.27.1, Git, Bash, pg_dump 18.6 and standard Unix archive/hash/path tools
are required.
The writer creates only `lerna_test_000000000000000000000001`, fails if it already
exists, exports that exact schema, and cleans only its own created schema with a
separate finite cleanup context. It does not drop the caller's database.

`database.sql` is a full PostgreSQL18.6 plain SQL dump (`--no-owner`,
`--no-privileges`, a fixed psql restrict key). It includes the actual v1 schema,
checksum record and real writer data; `0001_admission.sql` is the exact source
migration. `writer-observation.json` records actual Host observations/settings.
The SQL fixture preserves the original random Job ID and DB timestamps;
reconstruction reproduces behavior but need not be byte-identical.

For later v1→v2 tests, restore into an independently owned empty test scope with
PostgreSQL18.6 `psql -X -v ON_ERROR_STOP=1`. If restoring inside a dedicated
shared test database, replace only the fixed schema identifier throughout the
SQL with a generated isolated schema before executing it. The fixed fake input
contains no such schema identifier. Register the created scope and clean only
that scope. Apply the real later Claim migration after restoring; do not
initialize v2 and manually backfill fake v1 rows. Query original receipts through
`contract.GetCommand`, inspect input/Job relationships through the Host seam,
and continue the original pending responsibility once Claim handling exists.

V1 has no Claim, worker or completed projection. This artifact does not prove
SQLite, SIGKILL, power loss, production recovery or later upgrade completion.
