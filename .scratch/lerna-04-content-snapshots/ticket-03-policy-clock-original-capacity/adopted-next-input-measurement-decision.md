# 04-03 policy-clock capacity: one next decision

STATIC only, 2026-10-05. **Select one bounded measurement of the existing fresh-Input representation boundary, across its compilation-budget and worker consumers. Do not select a second product optimization yet.** No native, profile, product edit or renewed business attempt was performed by this review.

## Actual current result

Read both complete raw logs in untruncated chunks: normal158 lines SHA `9dea4e1d5513118092b758c6e8cbdd5eda7ea2e9ec4cd43ac93711c376a7dfe0`, race159 lines SHA `0d2f5fac1bf072dc92e5897655200ada428e916d46a453ac9bb0173c55473e94`, under `/workspace/lerna-content-03-137311247276/context-policy-clock-original4b-{normal,race}.log`. Read both result summaries and the relevant reviewed outcome/manifest/command facts.

Normal PID3871345/start16227233 exited0/Wait/group absent. Closure Step4.813785s completed with last Claim274.747ms remaining; static Step3.429235s with1.614317s remaining. Both actually committed Prepared and Completed and performed two Publish/two ReadPublished. Preserve the owner's exact limitation: the original positive helper does not additionally prove a numeric fee, decision-command receipt or reopened positive Decision. Do not retroactively enlarge that oracle.

Race PID3878367/start16258579 exited1/Wait/group absent. Closure's upstream publication ended11.879879s; compile/bind consumed14.056154s. Step began26.770458s with only3.229486s of caller30 remaining. Five material reads succeeded; Current then timed out at30.002906s. This first failure is **caller exhaustion**, not a proven expiry of its newly obtained5s Claim. Static committed Prepared at23.333282s, attempted its first Publish, and its Content.Put returned an unclassified errorString at23.979172s. Only the later Claim validation proves lease expiry, by270.182ms; caller still had5.986603s. The earlier Put's cause and possible effects remain UNKNOWN. No successful worker publication/readback/Completed in either race positive. The original refusal controls and200000 normal counterpart passed their actual tails; no race warning or watchdog timeout occurred.

One normal PASS does not establish stable capacity, and normal/race wall ratios do not identify SQL, pure CPU or instrumentation overhead. Earlier427bbc CPU and9fe9aa6b SQL-cost evidence retain their historical source scopes: the latter still had the now-removed pre-policy clock queries. Neither supplies the current removable cost of the candidate below.

## Exact missing evidence and one measurement

Current source exposes a concrete, shared representation boundary, but its present cost is unmeasured:

- `ContextDispatcher.input` (`context_dispatch.go:161–169`) always performs a fresh complete body `SELECT … FOR UPDATE`, then `closedJSON` (strict parse plus typed decode).
- `binding` calls it on each Current, compares complete current Input with the separately decoded/cloned Binding, then rechecks trusted authority. Existing Binding syntax memo does **not** memoize this input row.
- `currentAttempt` (`context_budget.go:205–243`) calls the same input reader for **each** ReserveRead and ConfirmRead, then marshals the decoded Input with `jsonBytes`, hashes it and compares the original attempt digest/tuple/revision/control. `budgetReader` surrounds every real compiler material/verification read with those durable operations. Compilation contributes materially to the new caller failure, while the worker uses the same input-reader boundary.

This supports measuring a precise possible pure-representation reuse, not declaring it worthwhile or safe to implement yet. The14s compile aggregate includes genuine Content operations, budget transactions and other work; it cannot all be assigned to input parsing. Worker Current1.134s is likewise inclusive, not decoder time.

**Only next diagnostic:** retain the current five coarse observers and add fixed-size aggregate timings at this Input boundary: (a) its original QueryRow/Scan, (b) `closedJSON`, (c) the original `currentAttempt` input `jsonBytes` and digest, separately, and (d) the relevant full-input comparisons. Attribute explicit existing call sites to compilation-budget, worker-binding, or other-input consumers; do not infer roles from elapsed timestamps. Separate the original serial subintervals from inclusive Current/Reserve/Confirm/compiler parents. No per-input logging, body output, extra query, new transaction, SQL-wide recorder, CPU profile, background probe or callback substitution. Record attempts/completions/first typed error and attribution drops; total observer overhead remains UNKNOWN unless actually established.

The measured question is whether repeated **syntactic** Input decoding/re-encoding after an unchanged fresh row read is substantial in these two real consumers. Any later reuse must first prove full-byte equality under exact owner/store/row scope, bounded successful entries and independently owned deep copies; current row locking, full Input/control/digest/tuple checks, authority, original budgets and clocks must remain. This report does **not** adopt such a memo, raw-body-as-canonical-digest substitution, or extension of Binding cache.

Prepare a new byte-reversible overlay against this exact current source; never reuse an old overlay that restores obsolete clock or memo code. Once the sole owner receives its normal execution grant, run **one** original four-B race, count1, unchanged caller30/Claim5/Go+outer120 and original inputs/setup. Stop after the actual outcome. No separate diagnostic matrix, warmup, retry or normal performance sample is selected. Observational output may identify the next candidate; it cannot itself qualify the failed business capacity or authorize an automatic next change.

## Preserved boundaries

Both tests deliberately create real source versions and compile inside their original30s caller. This is source-confirmed (`content_context_closure_test.go`, `content_context_material_reachable_test.go`), not accidental evidence to delete: do not move setup outside it, replace real publication with seeded private rows, or alter the oracle.

Keep all successful full-ref identity memo bounds/lifetime, Binding deep-copy/isolation, every fresh current Input/Permission, both Content qualification transactions, all source actions/full-ref checks, locks, BodySeal/Gone, post-lock/final clocks, real I/O/hash/length, durable compile rounds/read reservations/unknown charge, RuleStarts1, original Prepared/publication recovery and caller/lease intersection. Reject new SQL fusion/batching, authority/closure caches, extra clock deletion, codec shortcuts, pool growth, heartbeat or deadline reset. The pending retained-Now/transport-error coverage gap is not closed by eight semantic passes or this timing observation; keep it explicit for its own required qualification. Do not recreate the deleted unused query merely to fake that coverage.

All historical failures, original profile FD CloseUNKNOWN and the new Prepared/failed-Publish possible effects remain. This is no ticket/whole acceptance or cleanup authorization.

## Exact source/provenance

WT `/tmp/lerna-worktrees/content-snapshots-03`, base HEAD `b28b2e3182d97156c5fe203baf5cf2183631b7f2` plus the explicit current clock/BodySeal WIP. Independently checked **all331 manifest entries** against current bytes: zero mismatches. Manifest SHA `60f92164e164cfe64658f176e5967755370c72aa31a52c39ba44e038965ca3ac`; commands `d45302767f7719b99fb89ef33ed00e1a9df0a15845415d8bb0998f1ec5978bc0`. Reviewed normal/race outcomes: `94adcf08b2e4f1acd6579f0430f751fbf22569adbc2990f79eba1a7c3932db71` / `e9c7256f0ed85afb3f8ad399b92e58c8d251056d3c289a62833aa280e8ac4189`.

| Current file | SHA-256 |
| --- | --- |
| domain/content/closure.go | 2dc73e9e85b9efab3760218da62a56ed90eb8eda2f7d671468cf83e80bf6d1c2 |
| domain/content/processing.go | c01386301858c71432457591b8e122ab03b765d1cb796a5c91c58445f7195c9e |
| adapters/postgres/content/policy.go | 650d0325388217df4c93362da7876f7f62b9789baa7053d594f6a33225596182 |
| adapters/postgres/content/facts.go | e56785902e481eafdaa7904f44fabc44770d77f3d605da93906df827fe8a42d5 |
| conformance/internal/decisionfixture/context_dispatch.go | 5c2b8d472fdbe458b9affb9a9543db3359c85c0dfa90d5357bea843995e624fb |
| conformance/internal/decisionfixture/context_budget.go | b3ad33bb1ed9c89dc079afedb81dc9e43420a0609cb2b87422d1bb009ec9078d |
| conformance/internal/decisionfixture/context_binding_decode.go | 457b065fb760a264f0c687f82275c1dd73af3414191cccee1bba80b527f04779 |
| components/decision_engine/worker.go | 663d0c96eae83a4175c66f2ceaf3b87688d9a8199d3998e647eff4868eb0ff27 |
