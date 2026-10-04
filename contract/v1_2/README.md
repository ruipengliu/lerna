# Exact 1.2.0 Content contract

This isolated namespace has closed typed `content.put`, `content.get` and
`command.get` codecs with generated Go/TS types. The source is
`contract/schema/1.2.0`; `pnpm generate` reproduces tracked output. Declared methods
are not advertised while the full Content profile remains incomplete.

Canonical padded base64 has a decoded 256KiB bound inside a 1MiB wire bound.
Sources are capped at 64 unique exact-version identities. Content version is a
positive int64 decimal string. Response codecs bind the original full ref and
exact range; complete published bodies also verify hash/length. Accepted fixes
original ContentRef and first effective retention, without projecting a revision.
Command progress only observes publication history.

Shared 1.2 fixtures and independently computed digest/identity goldens are under
`conformance/fixtures/1.2.0`. `make test-contract` runs exact Go↔TS roundtrips in
both orders alongside unchanged old fixtures. `@lerna/contract/v1_2` is an explicit
SDK entry; old 1.0/1.1 types, schemas, methods and receipts remain unchanged.
