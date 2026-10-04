# Durable Decision fixture owner

This internal conformance package owns fixture identities and bytes in a separate
PostgreSQL schema. It does not implement production Task, ContextCompiler,
Content, Grant, installation or billing authority. Product packages never import
this package. `Open` takes the existing PostgreSQL connection configuration;
fixture transactions and connections are independent of Decision transactions.

The trusted `Seed` dispatcher atomically saves a closed immutable Snapshot,
accurate material references and bytes, a durable manifest, a
`ComponentFixtureLock`, and finite fixture permission. `ReadSnapshot` returns the
original stored JSON bytes in `Raw`. The lock names its exact manifest ContentRef;
the manifest can be read with `ReadMaterial` and purpose `fixture.lock`.
Every content read checks the exact reference, SHA-256 and decimal byte length.

Every entry checks the durable full principal binding, exact owner, permitted
purpose and database time. `decide`, `get` and `start` require the original
Decision identity. `command.get` uses an explicitly seeded owner read permission
for that exact owner and principal; its request identity does not stand in for a
Decision identity. Passing a ContentRef or altering a Permission grants no access.

Publication uses the original permission and publication key. Same bytes and
source references return the original identity after reopening; different bytes
or sources conflict. Publication, subsequent readback and Decision completion
are separate owner transactions. A publication left behind by an interrupted
Decision is only test fixture data, never evidence of Decision completion.

The owner-local immutable migration `migrations/0001_fixture.sql` has its own
SHA-256 ledger and is verified on rerun. All connection, statement, lock and
transaction work is bounded. Test world setup requires
`LERNA_TEST_POSTGRES_DSN` and an absolute `LERNA_TEST_OWNED_SCOPE_REGISTRY`.
Only acknowledged CREATE names are appended, fsynced and registered. Independent
administrative handles retain deletion authority across source reopening;
cleanup has its own finite context and never guesses ownership from prefixes.
