# 02 implementation handoff

Use the [seven-AC evidence](ticket-02-exit-evidence.md) for exact execution pins,
commands, review and limits. Latest integrated baseline is root `949c392`.
No later control implementation or complete execution-profile advertisement is
included in this ticket's delivery.

## Rule and Source boundary

- Keep `/2` + candidate_result and all existing `/2` unknown-label meanings.
  `/3` fixes finite case construction and durable-rule-start billing. Artifact
  digest binds the exact version string; config digest binds the exact case.
- Source.Seed supports the actual existing Snapshot fields for binding arguments
  and answer schemas. It retains original immutable Snapshot/manifest/lock and
  fee tuples; same-ref reseed cannot change case, version or config. No new DDL.
- Normal cases are delta_only, actions_four, input_request,
  delta_candidate_result, cannot_continue, and candidate_source_evidence.
  Each fixture uses its own actual Source/Decision schemas; action bindings in
  one Snapshot do not require multiple Decision-pool bindings.
- The fixed invalid cases are private bounded output generators, consumed by
  the same public codec and semantic checks. No production raw-output or fault
  parameter was added. A refusal persists the original accepted receipt and
  its measured generation; no new repair or charging identity is created.
- All material, binding-argument and answer-schema refs join the complete
  processed set. Every condition/action/evidence/question/schema/preview read
  checks actual Source purpose, remaining byte budget and immutable hash/length.
  Native read errors preserve their cause for snapshot_unavailable; semantic
  ErrForbidden remains proposal_invalid, and ErrInputLimit input_over_limit.
- Fixture answer metadata is finite JSON string schema: required maxLength1–256,
  optional minLength0–max. Duplicate/unknown keys, nested or remote `$ref`
  metadata fail; no network schema resolution.

See `proposal_v3.go`, the component README and the public-boundary cases in
`durable_decision_proposals_test.go` / `durable_decision_proposal_boundary_test.go`.
The fixture builder in those tests gives the actual four-tuple Source example;
it is not a new public scenario registry.

## Prepared union and publication

`ports.go` keeps original Prepared fields byte-for-byte. Original digest and
Validate are unchanged. Record's `prepared_v2` is a separate optional field;
a supported active record has exactly one handoff format. A record containing
both is unavailable. PreparedV2 contains the original start/input digest,
a bounded actual artifact list and one mandatory Proposal plus saved sources
and digest. Domain separation is `lerna-decision-prepared-2` with newline.

No-artifact branches have an empty list, not an invented blank publication.
Candidate artifacts use Publisher.Plan identities; every saved key/ref/body/
source sequence is published and read back before Finish. Reopen uses that
handoff without another evaluation/start/fee. Input/output usage and action
limits are original cumulative allowances, not reset by retry. The five real
reply-loss recovery controls each enforce a one-step/one-fee allowance.

For the later control merge, preserve the common current-qualification/Claim/
Stop publication fence for both formats and each actual publication. The new
handoff is not grounds to bypass the control branch's adopted gate. Root owns
that actual combined-tree verification; this is no hidden ticket02 dependency.

## Frozen producer mechanics

The independent `final01_proposal_writer_test.go.txt` is outside immutable
69-file original production. It requires its actual context.Canceled
interruption for publication-running; do not retune it to old-970 ErrClaim.
`restoreFinal01Writer` accepts a bounded driver, then the shared supervisor
returns a bounded READY frame and actual successful CREATE registry. Consumer
handoffs fsync exact schema acknowledgments before use, register partial
nonnull holders immediately, and retain a sticky unknown-close marker across
all cases before literal RELEASE. Unknown closure cannot be cleared by a later
successful close.

Both archives and SQL remain fixed. Only old-970's fully hash-verified restored
added-driver is transformed according to the [qualification decision](legacy-driver-qualification-decision.md).
Derivative8572B / SHA256 aa7097a937c0defb5ee7e356b4e2ee71b7c069d7f96b514a2bdc07c07db05945.
The limited mechanical conversion/error-stage checks are not native-fault
claims; actual consumer upgrade evidence is recorded separately.
