# Accurate 1.1.0 contract

Import `github.com/ruipengliu/lerna/contract/v1_1` or TypeScript
`@lerna/contract/v1_1`. The existing imports retain the frozen 1.0.0 contract.
SDK package versions are independent of wire contract versions.

The local `DecodeDecide`, `DecodeGet`, and `DecodeCancel` methods return their
own typed request. A decode validates a closed wire value; it does not authorize
execution. Decision execution, trusted control, durable sources, and publication
belong to the Component implementation.

`Decision` is a closed state union. Completed states carry the original fixed
input, Proposal, publication reference, artifacts and fixture usage. Cancelled
states can represent an authenticated cancellation before a decide arrived,
without inventing an input or Snapshot. Proposal uses a mandatory closed advance
union; requirement deltas may accompany one advance, and `none` requires a delta.

`DecisionUsage.rule_starts` records durable rule start billing events under the
original fixed Component and readable fixture manifest. For the current
durable-start policy, these events and their fixture `cost` are exact even if a
process dies before physical computation is confirmed. They are not CPU or
provider billing, and `model_requests` remains zero.

`rule_steps`, `input_bytes`, and `output_bytes` record cumulative physical
observations that have been durably confirmed. `measurements_complete: false`
means confirmed counts may be lower bounds, including after a later successful
completion. **Legacy records also have incomplete billing precision**: their
saved cost can be a lower bound, and recorded zero does not establish zero total
cost. Legacy `rule_starts: "0"` means no confirmed new-style billing event, not
zero historical physical starts. Interpret precision using the original input's
Component/manifest billing basis; the boolean alone does not choose a policy.

Both precision fields are required, including an explicit false value. Earlier
1.1 development bodies missing them are rejected. Updating durable old records
requires an explicit owner migration rather than a permissive codec fallback.
`failed / usage_unavailable` exposes legacy unaccounted execution without
inventing measurements or fees. `failed / billing_basis_unsupported` preserves
an unstarted fixed input whose original billing implementation was retired; it
does not silently move that input to a new charging policy. Existing legacy
terminal states and their Proposal/ref facts remain observable with incomplete
precision where warranted.

The fixed `max_rule_steps` limit controls durable start allowance. The input size
limit covers one complete fixed input (Snapshot, fixture lock, manifest and actual
materials); the output size limit covers its artifact plus Proposal. Public
cumulative byte observations may include confirmed repeated processing. Those
size limits do not silently become cumulative network traffic limits.

`DecisionInputDigest(request, trustedSubject)` binds the fixed input using the
`lerna-decision-input-1` prefix. `CommandDigest` retains the original
`lerna-command-digest-1` algorithm and full authenticated command binding.
Neither digest proves authorization, admission, existence of referenced content,
or successful publication.

`contract/schema/1.1.0/methods.json` is the sole inventory. Every method has a
complete reachable input and output Schema digest in `DeclaredMethods`.
`SupportedMethods` and negotiation include only `advertised: true` entries.
The local inventory advertises `command.get` under profile `command` and
`decision_engine.decide`, `decision_engine.get`, `decision_engine.cancel` under
profile `decision_engine`, all at version `1.1.0`. Negotiation requires the exact
profile, method and both digests; it neither authenticates a caller nor executes
a request. This availability covers the accepted local fixture implementation,
not a production provider or network service.

Run `pnpm generate`, `pnpm check:generated`, `node scripts/test-generator.mjs`,
and `node scripts/test-contract.mjs` from the repository root. The last command
runs both versions through real typed Go and TypeScript batch runners, in both
directions; append `--reverse` to reverse fixture ordering.
