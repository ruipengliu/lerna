# PostgreSQL Content facts

`Open` accepts the existing validated PostgreSQL Config. `Migrate` creates only
this Content owner namespace; its independent numbered checksum is verified on
repeat. Existing Host/Decision published migrations are untouched.

The owner stores exact Content tuples, bounded staging, fixed 1.2 Command
receipts, original Jobs/Claims and explicit fixture policy/command-reader facts.
Consumer interfaces belong to `domain/content`. Transactions reuse the existing
finite Core and durable Job mechanism; object I/O is never part of a PG transaction.
Policy checks hold exact rows and sample trusted database time after lock waits.
Queue capacity counts preparing versions, and byte capacity includes failed
versions' staging until actual later cleanup.

`InstallFixturePolicy` and `InstallFixtureCommandReader` are trusted fixture
management APIs outside all public payloads. They are not production Grant APIs.
Close uses the existing Core's sticky native close lifecycle. Real empty/repeat/
checksum and public behavior tests require configured dedicated PostgreSQL and
recorded exact owned scopes; missing services fail.
