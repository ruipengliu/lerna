# Internal durable work demonstration consumer

This package is the A-stage, same-version Host demonstration consumer. It owns
`durable_work.record` schema validation, exact trusted permissions, input
revision preconditions and the minimal input/observation Repository interface.
It is not a public SDK, Task domain or general business framework.

The current consumers are `host/durablework` and the fixture/real-PG harness.
PostgreSQL implements the consumer-owned Repository alongside runtime ports;
the host explicitly injects the same owner/database transaction bundle. This
package imports neither host/cmd nor a concrete adapter. It preserves exact
text and creates pending project responsibility without executing a worker.

See the [Host seam](../../host/durablework/README.md) for accepted commands,
query authorization and tests, and [storage](../../adapters/postgres/README.md)
for transaction/SQL behavior. Claim, SQLite and scheduling remain later tickets.
