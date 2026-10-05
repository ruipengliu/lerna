# Frozen original Content writer

The 75 inert production files in this directory are the complete package closure
used by the independent ticket05 producer from commit
`1a7d910238eb74cddc712d92b0ba4014a72ff507` (466704 bytes). Each original path,
length and SHA256 is recorded in `provenance.json`. Its fixed digest is
`589a151af608daf9accc510c7905d7635528fd3fb87cac64a2cdf8f7070d9c77`.
The current supervisor driver is separately stored as
`../ticket05-producer/main.go.txt`; it imports these frozen packages only.

`World.buildFrozenContent` verifies every manifest entry, materializes a fresh
owned directory, and invokes `go build -mod=readonly` under its finite compiler
supervisor. No external binary variable, personal worktree path or separately
installed producer is required. Compiler startup identity precedes its effect
gate; gate Close, actual Wait and process-group absence precede artifact use.
Original producer Objects/Store Close acknowledgements remain separate from
kernel completion. Unknown Close or completion preserves the owned scope.

The original one-off build script used for the first red is preserved as
`.scratch/lerna-04-content-snapshots/ticket-05-legacy-first-red-build.sh`.
It is a historical execution artifact, not a fixture or CI entry point.
Source provenance does not establish execution or authorize legacy scope import.
