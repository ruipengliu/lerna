# 1.1.0 shared contract fixtures

`fixtures.json` contains positive and negative raw JSON inputs for independent
Go and TypeScript typed codecs. Successful values run Go→TS and TS→Go with
byte-for-byte canonical output comparison. Raw original inputs remain bytes
until the public decoder executes; duplicate keys, wire numbers, Unicode,
body/depth limits, unknown fields, state bindings and Proposal bounds are tested.

`digest-input.json` and `schema-digests.json` are independent literal SHA-256
expectations calculated from the documented domain prefixes and canonical
bundles. Reachable Schema bundles include every transitive `$defs` reference.
Schema numbers are metadata; business integers are decimal strings.

References here name fixture owners, uses, locks and content only. Schema
acceptance does not prove those references exist or their use is authorized.
Cancellation state fixtures verify the contract shape only; cancellation and
other specialized Component behaviors are delivered by their own tickets.

Usage fixtures distinguish exact durable start charges from confirmed physical
measurements. Both `rule_starts` and `measurements_complete` are mandatory;
complete and incomplete observations retain their precise boolean/string wire
values through typed roundtrips. A completed Proposal can retain an earlier
unknown measurement gap. Missing fields, wire numbers, malformed counts and
nonboolean precision flags are rejected in both languages.

Use `node scripts/test-contract-1_1.mjs --usage-only` for the small accounting
contract subset, or append `--reverse` to reverse that subset. The complete shared
contract suite remains the integration gate after the owner implementation is
updated.

Legacy billing fixtures retain original results and recorded measurements.
`usage_unavailable` and `billing_basis_unsupported` are closed asynchronous
failure values. A legacy incomplete cost of zero represents the stored lower
bound; it does not prove actual total charges were zero. Historical start counts
are likewise not fabricated. The accurate Component/manifest basis distinguishes
legacy fee-at-finish records from current durable-start billing events.
