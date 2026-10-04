# Durable rule Decision

This component owns Decision admission, fixed Command receipts and recoverable
Proposal progress. It reads a trusted fixture Snapshot and publishes through
consumer-owned ports. It owns no Task, Content, Grant or provider fact.

`New` requires explicit finite storage, authorization, source, publisher, owner,
component, worker and lease configuration. `Decide`, `Cancel`, `Get` and `GetCommand`
consume strict 1.1.0 bytes and a trusted SubjectBinding. The incomplete
`decision_engine` profile remains unadvertised. The original 1.0.0 codec and
methods remain frozen; `LegacyReader` exposes only losslessly representable
Command facts and returns its existing unavailable result otherwise.

Admission authenticates the current principal before inspecting the original
Command key. That key binds its immutable canonical command digest; a separate
Decision input digest binds the fixed version, Task, Snapshot, component, uses,
limits, deadline and full principal. New commands for the same fixed input
associate with the original Decision. The first accepted receipt, Decision and
actual Decision-FK Job commit in one short owner transaction.

The worker shares finite pool values, FIFO and PG transaction mechanisms with
the earlier durable demo. Pool membership, queue/concurrency and tenant quota
are checked under the same active owner transaction. Claim, Start, prepared
handoff and Finish each recheck their current Claim, pool, deadline and trusted
database time after locks. Expiry visits all registered members, including
nonanchor members at zero ordinary quota. Future wake includes due work, live
leases and all active Decision deadlines; an empty pool waits a positive second.

New fixture-rule/2 fixes one `fixture` unit per durable rule start in its accurate
component lock, manifest and Permission. Start consumes one original
`max_rule_steps` allowance conservatively and adds `rule_starts` and that exact
fee. It does not claim a physical completed rule step. Only one caller per Claim
epoch receives computation permission. Confirmed input bytes, generated output
bytes and actual rule steps accumulate separately; a lost observation remains
visible through `measurements_complete=false`. Model requests remain zero.

All reads take the remaining input budget. The original `/2` rule creates a
candidate result with the exact manifest/material sources and requirement
evidence. The publisher plans its own accurate artifact reference without
publishing. The component assembles the complete Proposal and checks the sum of
artifact and Proposal bytes plus the full 1 MiB public response bound. It then
persists the complete immutable prepared handoff in a short owner transaction.
Publication and independent readback run outside that transaction, always using
the original keys, bytes, digests, sources and permissions. Finish commits only
after both independent publications read back exactly. The accepted receipt
never changes with progress.

Cancellation additionally requires the explicit `ControlAuthority` configuration
port. Current principal/scope access and the immutable Task-owner fixture proof
are separate observations. Current access runs before original Command replay;
the original applied receipt remains readable after its first-admission proof
and command cutoff expire, while current access revocation still prevents replay.
New control binds the exact Decision, Task and original input digest and must be
higher than the real Snapshot floor or prior adopted control. Generic
`expected_revision` is forbidden for this method.

Decision owner migration `0003_decision_stops.sql` adds a separate monotonic stop
fact. An absent Decision can be closed without inventing an original Input or
Snapshot. Active cancellation stops its exact original Job and saves the stop,
cancelled Decision and fixed applied receipt in one short owner transaction;
ordinary quota or saturation does not block it. A terminal Decision remains
frozen: newer stop control appears only in `Get.current_control`, with scope
`local_decision_work`. Every new publication has a current local qualification
check, and a late Finish cannot adopt output after a committed stop. Independent
Source calls already in flight may leave unadopted bytes; cancellation does not
claim publication deletion or cross-owner atomic revocation.

Command metadata version `2` has exactly one typed decide/cancel branch. The
reader retains the original unversioned decide-only metadata and receipts without
rewriting their bytes or digest. Deployment must confirm old writers have exited
before new writers use stop facts or new metadata; mixed old/new writing is not
supported. The unchanged final01 production archive supplies real upgrade states.

`fixture-rule/3` adds five finite Proposal branches and a source-evidence variant. Its artifact digest binds the
exact version string and its config digest binds the exact case string. Each
case uses a separate immutable Snapshot, lock and manifest in its own Source
and Decision scopes. An undefined case fails as `proposal_invalid` after admission,
without falling back to `/2` or changing an old case's meaning.

| Fixed case | Proposal |
| --- | --- |
| `delta_only` | Current condition replacement and `none`, with no artifact |
| `actions_four` | Four independent actions from exact Snapshot bindings |
| `input_request` | Clarification question, answer schema and preview |
| `delta_candidate_result` | Condition replacement plus one actual candidate artifact |
| `candidate_source_evidence` | Candidate artifact plus independently readable Source evidence |
| `cannot_continue` | Precise reason and current missing condition, with no artifact |

`invalid_actions_depends_on` is a distinct fixed private configuration. It
constructs the same four actions and deliberately emits `depends_on` in raw
Proposal bytes. Those bytes pass to the same public decoder and produce the
original Decision's `proposal_invalid`; the accepted receipt and actual
generation measurements remain durable, with no automatic repair job.
`invalid_actions_binding_pair` instead creates a structurally valid Proposal
using an existing binding with another binding's arguments. Exact Snapshot
tuple inclusion rejects it after decoding; individually valid refs confer no
new combination of authority.
`invalid_actions_denied_purpose` keeps every Snapshot tuple intact but its fixed
Source grant permits reading the arguments as rule input and denies the action
purpose. It fails during actual Source consumption, with a legal-purpose normal
case using the same finite action construction.
`invalid_delta_stale_condition` keeps the current condition identity but emits a
different well-formed revision. Current exact RequirementRef inclusion rejects
the replacement; a normal `delta_only` case preserves the current revision.

The finite invalid-output matrix also generates duplicate replacements, omitted
processed sources with full disclosure, foreign disclosure, wrong capability
pairings and future action arguments, duplicate or excessive actions, combined
advance fields, duplicate or unknown raw fields, empty-delta `none`, stale or
purpose-forbidden evidence, confirmation requests and forbidden schema metadata.
Typed and raw faults all enter the same public decoder and semantic consumer;
actual planned artifacts of failed candidates remain unreadable in Source.

Each `/3` evaluation records one confirmed rule step, retains the original
durable-start fee and makes zero model requests. It reads all declared material,
binding argument and answer schema refs without clipping the processed sources.
The same closed public Proposal decoder precedes current revision, source,
capability/binding and purpose checks. Arguments, conditions, question, schema
and previews are read through the real Source with the remaining input budget.
Semantic refusals remain `proposal_invalid`, input budget exhaustion remains
`input_over_limit`, and actual Source read failures retain their cause at the
worker dependency boundary as `snapshot_unavailable`, with measured usage.
The fixture answer schema accepts only a string type, `maxLength` from 1 through
256 and optional `minLength` from 0 through that maximum. It rejects duplicate
or unknown keys and never resolves a URL or `$ref`.

The original `/2` `prepared` fields, bytes, keys and digest domain remain
unchanged. `/3` writes `prepared_v2` with an actual bounded artifact list and
one mandatory Proposal; records with both formats are unavailable. No-artifact
branches publish only their Proposal. Planned artifact identities come from
the publisher, and completion follows exact publication/readback of every
saved body. Neither format changes Task revisions or adopts a proposed delta.

A replacement reuses prepared output without calculating again or charging a
second start. Transient publication failure retains waiting responsibility with
`dependency_unavailable`, retries after 100 ms, and closes after eight publication
attempts or the original deadline. This finite fixture policy is independent of
the demo retry policy. Every I/O context is bounded by the caller, Claim lease,
original Decision deadline and Permission expiry. No cross-owner atomic commit
is claimed. Process-kill coverage is delivered separately by ticket06.

The old fixture-rule/1 fee-at-finish binding is readable but retired for new
execution. The owner migration classifies old observations; normal maintenance closes active work without inventing durable
starts or fees; existing terminal Proposal facts and original receipts survive.
See the [adapter upgrade policy](../../adapters/postgres/decision_engine/README.md).

Real PG conformance is in `conformance/component/durable_decision_*` and the
independent source owner in `conformance/internal/decisionfixture`. Run
`make test-integration` with a dedicated DSN and absolute ownership registry.
These fixtures establish the rule component boundary, not production quality,
real installation, Task orchestration or provider billing.
