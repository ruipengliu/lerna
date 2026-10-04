# Local immutable objects

`Open` requires a trusted, existing absolute Linux directory on a volume whose
file and directory `Sync` guarantees the deployment accepts. The adapter holds
an `os.Root` and its directory handle. Keys are opaque SHA-256 identity encodings;
caller paths and symlink files are rejected.

`Put` receives the exact persisted attempt name. It writes at most 256 KiB,
independently checks the staging bytes, syncs the file, installs a hard link
without replacement, syncs the directory, and independently reads and verifies
the installed object. An existing entry is accepted only when it matches the
exact hash and byte length. Temporary removal is also directory-synced.

Every operation needs a finite context. `Close` serializes with native file
operations and retains the first physical close result. An unconfirmed close
must retain the root for operator recovery. This adapter does not repair reads.

Current evidence will cover real Linux files and reopen. Local process kill is
not a power-loss, shared-volume, cloud S3, or cross-region durability test.
