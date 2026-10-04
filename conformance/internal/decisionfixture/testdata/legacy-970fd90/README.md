This frozen fixture runs the real Decision writer from commit
970fd90260b5c4936cdb5c2b7a8589623126a5c2 before opening the current reader/migrator.
Normal tests require no Git history, Git executable, or network Git fetch.

The 68 archived production files (422,534 bytes) are byte-exact source from a
bounded one-time `git archive` of that commit. The closure comprises the actual
12 module packages reported by `go list -deps -json` for the old fixture package,
including their Go files, embedded SQL/schema files, go.mod and go.sum. External
module dependencies use the pinned old module files. Files have an extra .txt
suffix so repository package discovery/formatting cannot alter frozen source.
provenance.json records destinations, sizes, SHA-256 digests and roles;
SHA256SUMS also covers provenance and this README. The runtime verifier checks
these before restoring a bounded workspace overlay and building the old writer.

legacy_writer_test.go.txt is an ORIGINAL ADDED DRIVER, not archived 970fd90 code.
It uses old NewWorld and public Decide/Claim/Start/Step/Get to produce actual
accepted, running, completed and fee-limit failed facts. Its publication-running
case decorates only the old consumer Publisher seam: it delegates real durable
Publish plus independent ReadPublished, captures exact refs/bytes, and waits
past the actual claim lease before returning the Proposal. Real old Finish then
rejects the expired claim, leaving publicly observable running state and durable
outputs. No business records are constructed or written through private tables.
Old waiting and cancelled cannot be produced by the published 970fd90 service
entry points; those states are explicitly outside this producer's coverage.

The old source and Decision writers are explicitly closed successfully and set
to nil before a <=128 KiB JSON ready frame reports exact CREATE-success schema
pairs, public facts/receipts, migration versions and publication refs/bodies.
Original administrative World handles remain alive until bounded stdin RELEASE.
The parent closes every new writer first, then releases and waits for the old
producer's exact cleanup. Child processes use finite deadlines, process groups,
bounded output and bounded Wait. Fallback cleanup uses only fsynced acknowledged
CREATE names, never discovery or prefix matching. An unconfirmed exit preserves
the registered overlay and exact ledger for investigation. No DSN is transmitted.

The current side verifies immutable owner0001 checksum preservation, owner0002
installation and idempotent repeat migration, public legacy queries/fixed replay,
unchanged numeric lower bounds with zero fabricated starts, original completed
Proposal/refs/receipt and original publication-window outputs. A fresh legacy
binding command is fixed unsupported without a Decision/job; fresh rule-v2
acceptance/completion provides the normal durable-start-charge counterpart.
