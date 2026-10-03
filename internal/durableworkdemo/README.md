# Internal durable work demonstration consumer

This package is the A-stage, same-version Host demonstration consumer. It owns
`durable_work.record` schema validation, exact trusted permissions, input
revision preconditions and the minimal input/observation Repository interface.
It is not a public SDK, Task domain or general business framework.

The current consumers are `host/durablework` and the fixture/real-database harness.
PostgreSQL and SQLite implement the consumer-owned Repository alongside runtime ports;
the host explicitly injects the same owner/database transaction bundle. This
package imports neither host/cmd nor a concrete adapter. It preserves exact
text and creates pending project responsibility. `Worker` consumes its separate
`WorkRepository` and runtime ClaimStore ports. It fixes a project input snapshot
in the claim transaction, then `Project` computes SHA256 of the exact UTF-8 bytes
outside any transaction. `Complete` writes `inputRevision`/`textDigest` and advances
only the claimed Job revision in one short transaction. Derived work never
increments the input revision.

See the [Host seam](../../host/durablework/README.md) for accepted commands,
query authorization and tests, and [storage](../../adapters/postgres/README.md)
and [SQLite storage](../../adapters/sqlite/README.md) for transaction/SQL behavior.
The same Worker runs against PostgreSQL and SQLite through these ports.
Scheduling, waiting and quotas remain later tickets.


Cleanup is a separate trusted internal capability, default denied by the exact
SubjectBinding/OwnerRef permission table. `Host.Cleanup(ctx, originalCommandRef,
expectedInputRevision, trustedSubject)` accepts only the original applied record
and its current successfully projected revision. It locks command → input → Job,
requires done with no Claim or newer work, and atomically clears actual text
bytes and marks that input plus that individual command gone. It preserves the
original digest, metadata, fixed receipt, Job ID/revisions and projection. Empty
present input and gone input differ by `Input.BodyGone`; `StoredTextBytes` is
computed from the bytes actually read and a real database constraint rejects
gone with nonzero stored bytes.

Public command.get returns gone for that exact command. Original raw Record
retransmission still returns its fixed receipt; a changed digest still conflicts.
A new command with the exact current expected revision stores a new present
body and adds work to the original Job. Old command tombstones stay gone and
repeated old Cleanup cannot touch the new body. No tombstone collection, history
body table or generic TTL is provided. This clears live records, not forensic
copies in WAL, MVCC pages, backups, replicas or caller memory.
