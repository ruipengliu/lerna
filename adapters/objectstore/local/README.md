# Immutable local Linux objects

`Open` requires an existing trusted absolute directory without symlink traversal.
Opaque exact version keys and original Claim attempt names cannot escape os.Root.
The caller owns the root's durable volume and records its exact lifecycle.

`Put` bounds bytes to 256KiB, independently verifies temp bytes, synchronizes the
file, installs with a no-clobber hard link, synchronizes the directory, independently
reads/verifies the final object, and removes the temporary name. Existing bytes
are adopted only if hash and length match. `Read` verifies the whole regular file.

Every native file handle belongs to Store. Partial Open can return both a nonnil
holder and a startup/Close error; the caller must retain that holder. First native
Close errors stay sticky. `CloseContext` seals new calls and bounds the active
invocation drain wait. Native Sync/Close cannot be canceled and remain owned until
real return. A drain timeout permits retry after actual invocation exit; it does
not prove a closed holder. Destructive cleanup requires confirmed closure.

Tagged local lifetime tests use exact recorded overlay roots and mechanical
failure/synchronization seams; normal controls still perform real Linux primitives.
`make test-integration` includes them. This adapter is not a production S3 or
power-loss durability claim, and currently has no physical cleanup/fence method.
