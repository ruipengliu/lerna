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

`DecisionInputDigest(request, trustedSubject)` binds the fixed input using the
`lerna-decision-input-1` prefix. `CommandDigest` retains the original
`lerna-command-digest-1` algorithm and full authenticated command binding.
Neither digest proves authorization, admission, existence of referenced content,
or successful publication.

`contract/schema/1.1.0/methods.json` is the sole inventory. Every method has a
complete reachable input and output Schema digest in `DeclaredMethods`.
`SupportedMethods` and negotiation include only `advertised: true` entries.
Currently `command.get` is advertised; the developing Decision profile remains
unavailable for negotiation until its full implementation is accepted.

Run `pnpm generate`, `pnpm check:generated`, `node scripts/test-generator.mjs`,
and `node scripts/test-contract.mjs` from the repository root. The last command
runs both versions through real typed Go and TypeScript batch runners, in both
directions; append `--reverse` to reverse fixture ordering.
