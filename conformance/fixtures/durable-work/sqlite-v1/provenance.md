# SQLite v1 input provenance

Generated 2026-10-03 UTC by the real Host writer from immutable Git source
`f4fb0576bc0a1e3371fb88ab43f729beb9ddf118`, committed before export. The
historical source has actual admission and a genuine v1 migration, with no
Claim/lease columns or prefilled completed work. `writer-revision.txt` records
the authoritative full source revision.

Generation at the implementation repository:

```bash
scripts/generate-sqlite-v1-fixture.sh f4fb057 /workspace/lerna-sqlite-v1-export-02
```

The script archived that exact Git commit into an independent temporary source
directory, built `cmd/sqlite-durable-work-fixture` there with local Go and
read-only locked modules, and ran the historical binary:

```text
v1-writer -output /workspace/lerna-sqlite-v1-export-02
```

The output path was a new, owned directory on local overlayfs. It is an example
of the actual execution path, not a product dependency. Rebuild into any new
local output directory. The writer created and migrated its real database,
executed the checked-in three-command corpus through Host.Record and observed
Host facts. It closed its only connection, checkpointing WAL into the retained
**complete SQLite database file**. It did not query private tables to construct
fake historical rows or require a SQLite CLI/export utility.

Runtime: Go 1.27.1, Linux amd64, GCC 14.2.0 (Debian 14.2.0-19),
`github.com/mattn/go-sqlite3 v1.14.52`, CGO with the driver's bundled SQLite
**3.53.4** (version number 3053004), source ID
`2026-07-24 19:02:57 bf7c7f30031888f4e796e429ab3978879485813aaca6f641c7b33e4e09459bcc`.
No `libsqlite3` tag or personal header/library path was used. The writer observed
WAL, synchronous FULL (2), foreign_keys ON (1), busy_timeout 100ms and migration
version 1/checksum; its transactions had a 3s maximum context.

The corpus intentionally reuses the PG v1 fake-data commands verbatim, including
the literal text `PG v1 portable input 🌍`; that string is payload content, not
a database or runtime label. The first command produced applied revision1 and
pending original Job work_revision1/completed_revision0/ready. The second fixed
an expired rejection without input/Job. The third retransmitted the original
command; its unchanged receipt and pending Job were observed before closing.

Exact migration checksum:
`sha256:324dd9c72a00438095596b59c80bf21e66a02eb53d7182ddba67e4784e2c0203`.
Complete database fixture digest:
`sha256:4d8aafac077e2fab896358e35bc18c19a9c7150b533e3505eb33bf3a644b1900`.
`SHA256SUMS` records the migration, corpus, complete file, Host observations and
writer revision. Audit with `sha256sum -c SHA256SUMS` here; compare migration
and corpus against `git show f4fb057:<source path>`.

The exact historical script was independently rerun into
`/workspace/lerna-sqlite-v1-reexport-02`. Source migration, corpus and revision
matched byte for byte; real applied/pending/expired/retransmit behavior passed.
Fresh Job randomness and timestamps may change the regenerated file digest.
The checked-in file was also copied into an owned temporary local file, opened
with the current adapter and queried/retransmitted through Host and public
GetCommand. Its original Job ID, both fixed receipts and pending revisions
survived. No checked-in fixture was opened in place or mutated.

This is retained input for the later actual v2 Claim migration and upgrade
acceptance. It does not claim Claim, SIGKILL, power-loss durability, production
failover or the complete durable-work slice passed.
