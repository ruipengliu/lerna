# Whole03 local integration checkpoint

2026-10-04. All six tickets and 42/42 sub-AC are resolved in the integrated
baseline. This records the final local profile/CI-entry implementation and its
actual checks. **Whole03 is not yet exited**: the complete two-axis review,
architecture disposition, formal integration/push and exact final CI remain
root's pending gates. No additional ticket or domain implementation is introduced.

## Exact sources and behavior

Baseline: `384511046bc05e1d9857990a365b5850deb7562f`, containing the formal six-ticket
integration. Tested implementation: `6bf57501a5003ff139e96aec40589ad49fcba1ec` on
`codex/deterministic-harness-whole`, in its own worktree.

Only the three availability flags in `contract/schema/1.1.0/methods.json` change.
Actual `pnpm generate` supplies the Go/TS support metadata; no generated body is
hand-edited. Existing negotiation logic remains unchanged. The public inventory
now accepts exact `1.1.0` descriptors for:

| Profile | Method | Input / output Schema |
| --- | --- | --- |
| command | command.get | CommandGetRequest / CommandGetResponse |
| decision_engine | decision_engine.decide | DecisionDecideRequest / CommandReceipt |
| decision_engine | decision_engine.get | DecisionGetRequest / DecisionGetResponse |
| decision_engine | decision_engine.cancel | DecisionCancelRequest / CommandReceipt |

Go and TS public tests use literal identities and the independently stored
`schema-digests.json` goldens, including the integrated DecisionGetResponse
`sha256:c086cd3aa01c5e468bf68296e7f80a0e0b88db6076895df925267acfb21bb55b`.
Each method rejects a schema-valid wrong version, the other profile, wrong input
digest, wrong output digest, both wrong digests and an absent method. Negotiation
establishes compatibility; actual execution still requires trusted principal,
authorization, budget and deadline checks. It is not network discovery or a
production provider claim.

`make test-integration-race` is the shared local/CI entry. Recovery executes first,
then `scripts/test-component-integration-race.sh` obtains the real native Go
`-list .` result and preserves its exit before parsing. It partitions every
listed Test, Example and Fuzz seed into TestDurable* or the remaining positive
anchored-name union. Duplicate/ambiguous/empty inventories fail, and an empty
group is skipped explicitly rather than running all tests through an empty
selector. Future registered names are discovered automatically; no static102
list or negative regex is maintained. Source executes last. Every actual suite
keeps `-p=1 -count=1 -race -tags=integration -timeout=120s` and runs serially.

## Actual checks

All commands ran strictly serially, with actual native/session exit before the
next command. Locked bootstrap succeeded for this new worktree. Local tools are
Go1.27.1, Node24.19.0, pnpm12.8.1 and TypeScript7.0.2; native PG reports
`18.6 (Debian 18.6-1.pgdg12+2)`.

| Scope | Actual result |
| --- | --- |
| New public Go four-method tracer before flag changes | exit1, package0.035s: command.get succeeds, the three correct Decision requests return unsupported and are not advertised |
| Same tracer plus retained old digest/cache checks after flags and actual generation | exit0, package0.142s |
| Four-method positive +24 inexact public refusals + retained digest/cache normal | exit0, package0.070s |
| Same affected public scope race | exit0, package1.401s |
| TS new negotiation plus existing Decision boundary tests | 5tests, exit0; existing refusal behavior needed no production fix |
| Shell external-tool protocol tests | 5tests, exit0: native discovery/group status propagation, future Test/Example/Fuzz/Unicode membership, empty-group handling and fail-closed inventory |
| `make check GOFLAGS='-mod=readonly -p=1'` | exit0: format/vet/typecheck, generated consistency,25JS/40TS tests, Go normal tests, generator probes, real89+158 fixtures in forward and reverse Go→TS/TS→Go typed exchanges, Go/TS builds |
| Actual new `make test-integration-race GOFLAGS='-mod=readonly -p=1'` | exit0: Recovery79.934s → actual discovered104 Component runnables (60Durable102.204s →44Other11.633s) → Source28.271s |
| Repeat `pnpm generate` at the unchanged tested source | exit0, clean git tree; no additional generated differences |
| Frozen source/payload comparison against baseline |222 protected paths byte-equal,0differences; all158 old and89 new fixture objects unchanged |

The real shared entry includes the original PG/SQLite Recovery package and its
public process stories, all current Component tests and the complete Source
package, including the authentic historical producer upgrades. Race instruments
the current consumer/supervisor; historical producer binaries use their original
normal build, so this is not a claim that original producers were race-built.
These are local fixture/native-process results, not power-loss, provider or
production capacity evidence.

The first TS positive test failed because a decoded expected descriptor had a
null prototype while the public support descriptors had ordinary prototypes.
The test now compares a plain detached expected descriptor, retaining all exact
public fields. This was a test-construction failure, not a negotiation defect or
contrived production red. An initial standalone static audit requested a
nonexistent old fixtures.json; it was corrected to the actual old values,
commands and responses files. The first catalog audit attempted to supply a URI
through PGDATABASE and failed connecting to a local socket before any catalog
query. The corrected audit splits the actual URI into private native PG
environment fields; no DSN or password was printed. No resources were deleted
on either audit failure.

Raw local logs remain `/tmp/lerna-03-whole-{bootstrap,negotiation-red,
negotiation-green,negotiation-refusals,negotiation-race,ts-firstgreen,ts-green,
shell-entry-check,check,integration-race,generate-repeat}.log`.

## Frozen scope and acknowledged resource checkpoint

The [freeze audit](whole-integration-freeze-audit.json) compares all old1.0 Schema,
all shared fixtures, both immutable original producer archives, all SQL,
the old generated outputs, current1.1 Schema and unchanged1.1 aliases/runner
against the integrated baseline. New metadata changes neither Schema digests
nor old payloads, published migrations or Prepared formats.

The [literal cleanup audit](whole-integration-cleanup-audit.json) was taken after
all actual test/build sessions and borrowers exited. It uses only successful
CREATE/Mkdir/process-group acknowledgments from the owned fsynced ledger:
**414 unique PG schemas**, **122 SQLite fixture directories**, **32 target
directories**, **3 historical restore directories** and **6 acknowledged compiler/
producer groups** are all independently absent. The ledger has589 acknowledgment
lines; repeated transfer acknowledgments are not counted as distinct schemas.
No numeric group signal was used by the audit; it observes existence only.

The exact owned overlay `/workspace/lerna-03-whole-dbtmp-l8rawcn4` (device27,
inode431270) remains for sole-fixer review followups. Its only observed remaining
files are197 Node compile-cache files, inventoried by exact path/inode/hash after
all tools exited and fsynced at `/workspace/lerna-03-whole-tool-cache-inventory.json`.
That late tool-file observation is distinct from fixture-time CREATE ACKs.
The worktree and branch remain. Historical unknown scopes, including the
previous3540254111 directory and other agents' overlays, are untouched; this
checkpoint grants no deletion or signal authority over them.

## Followup qualification

Root's whole-baseline diff check found an inherited whitespace-only blank line
in scripts/bounded-build.test.mjs. The sole fixer removed its spaces; Prettier
check passed and the whole diff is clean. This changes template source bytes
without changing its Python behavior; it does not relabel the tested6bf source.
Root also found seven historical app-only absolute links in
process-lifecycle-handoff.md. They now use the same original fixed source
`f96f85987818a5e8dc5c0c104e5731ddf75687df` and original line numbers as GitHub blob
permalinks; labels and historical findings are unchanged. They do not point at
current code or rewrite prior evidence.

The complete reviews are running on fixed6bf; final followup source/docs will be
provided for root's independent disposition. No review-zero or remote-CI result
is claimed here, and no whole status has been marked completed.
