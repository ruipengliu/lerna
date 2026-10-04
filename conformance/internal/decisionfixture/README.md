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
The fixture artifact digest hashes the exact rule-version bytes; the fixture
configuration digest hashes the exact Snapshot rule bytes. Seed and lock readback
both verify these bindings against the stored manifest.

Source calls take an explicit remaining byte budget from zero through 1 MiB.
Current authorization runs first. PostgreSQL checks the stored byte length and
returns NULL instead of an oversized body. Material references are checked
against their known lengths before reading; lock and manifest share one combined
budget. A budget violation returns the distinct `ErrInputLimit`.

New scenarios use `fixture-rule/2`. Their immutable manifest, component lock and
Permission fix `durable_rule_start` at one `fixture` unit per durable rule start.
This is test billing, never CPU or actual completed-step billing. Legacy
`fixture-rule/1` metadata remains readable with its original absent billing
basis; readers never supply a new fee. Optional billing fields retain the old
Permission JSON frame and publication identities. The component decides how to
close unsupported legacy execution; this fixture owner does not convert it.

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
`PlanPublication` uses this same reference derivation under a separate current
authorized fixture transaction. It creates no published body or receipt and
does not make its reference readable. Actual `Publish` checks the original key,
bytes, sources and current authorization again, then persists that planned
identity for independent readback.

The owner-local immutable migration `migrations/0001_fixture.sql` has its own
SHA-256 ledger and is verified on rerun. All connection, statement, lock and
transaction work is bounded. Test world setup requires
`LERNA_TEST_POSTGRES_DSN` and an absolute `LERNA_TEST_OWNED_SCOPE_REGISTRY`.
Only acknowledged CREATE names are appended, fsynced and registered. Independent
administrative handles retain deletion authority across source reopening;
cleanup has its own finite context and never guesses ownership from prefixes.
