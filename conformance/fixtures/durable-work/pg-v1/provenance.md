# PostgreSQL v1 input provenance

Generated 2026-10-03 UTC by the real Host writer from immutable Git source
`988f8b7ec2a8fd3a28db44b11cf5a863af4593b2` (see `writer-revision.txt` for the
authoritative full commit). The implementation was committed before export;
the script used `git archive` into an independent temporary source directory,
built `cmd/durable-work-fixture` there and ran that historical binary. The current
working source did not create fake old rows.

Generation command, run at the implementation repository with the explicitly
injected dedicated test-database DSN (not recorded here):

```bash
LERNA_TEST_POSTGRES_DSN="$DEDICATED_TEST_DSN" \
  scripts/generate-pg-v1-fixture.sh 988f8b7 /tmp/lerna-pg-v1-export-01
```

The historical binary used its checked-in `commands.json` and:

```text
v1-writer -output /tmp/lerna-pg-v1-export-01
pg_dump --format=plain --no-owner --no-privileges --restrict-key=lernav1fixture \
  --strict-names --schema=lerna_test_000000000000000000000001 <injected DSN>
```

Runtime versions: Go 1.27.1, pgx/v5 v5.11.0 (`database/sql` adapter), PostgreSQL
18.6 (Debian 18.6-1.pgdg12+2), pg_dump 18.6 (same distribution build). The local
18.6 dump executable forwards to the pinned service container; no such personal
wrapper path is a repository dependency. The server image is
`postgres@sha256:3725f4e2499eef5134592b3b4ab79a543ed7f8e533b05b5b637af926630f6650`.

The writer observed actual READ COMMITTED, synchronous_commit on,
statement_timeout 2s and lock_timeout 1s under a 3s transaction context. Full
observations are in `writer-observation.json`. The generated schema was in a
new dedicated test database and was removed by its owning writer after export;
no smoke schema, caller database or other tenant data was exported or removed.

The export contains an applied original receipt and input revision 1, a pending
original Job with work_revision 1/completed_revision0/state ready, and a fixed
expired refusal. The third command was a real original-key retransmission;
the writer compared its unchanged receipt and observed no revision increase
before the dump. No lease columns or completed work were constructed.

`SHA256SUMS` records exact source migration, command corpus, dump, observation
and revision-file digests. Migration checksum:
`sha256:f8d04d373b039a425b4f6d0a7b7dd4410971c00faf91cdba3f68a9204579127e`.
Database export digest:
`sha256:1d2195606544b288e14a0e2819844ee187b07e097fe7e96f83513013f1ad8b60`.
Validate with `sha256sum -c SHA256SUMS` from this directory and compare the
migration/corpus with `git show 988f8b7:<source path>` when auditing provenance.
Rebuilding may change Job randomness/timestamps and hence the output dump hash;
it must preserve the behavior and exact historical source/input checksums.

This is retained input for a later actual v1→v2 Claim migration, not evidence
that Claim, SQLite, SIGKILL, process failover or production upgrades passed.
