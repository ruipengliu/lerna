# Real SQLite v1 upgrade input

`database.sqlite` is the complete closed file created by the committed actual
v1 Host writer. It includes pending original work and fixed original decisions;
see [provenance](provenance.md) for the exact historical Git source, runtime,
commands and digests. The same fake command corpus as PG is retained verbatim.

Validate here with `sha256sum -c SHA256SUMS`. Reproduce from the repository root:

```bash
scripts/generate-sqlite-v1-fixture.sh f4fb057 /new/local/output-directory
```

Linux, CGO, a C compiler, locked Go/Git/Bash and standard Unix tools are needed;
no SQLite CLI or separately installed SQLite headers are required. The output
directory must not already exist. Never open the checked-in file for writing:
copy it into an owned temporary file before applying migrations or admission.
`make test-integration` verifies checksums and restores that copy through actual
Host/public GetCommand seams. Claim handling uses the real v2 migration.
`migration_test.go` applies v2 and the current retention migration, rejects the
v2 version INSERT with a test-only trigger, verifies rollback and retries the
same copied file. It then claims/completes the original Job, cleans its accurate
input revision and reopens it without recreating the original responsibility.
