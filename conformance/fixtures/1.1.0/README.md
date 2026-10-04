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
