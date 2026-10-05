# 04-03 current-cost decision — one policy-clock ownership correction

STATIC decision, 2026-10-05. No native/Go/DB operation or product edit performed. Adopt one change: make the existing `Repository.CheckPolicy` explicitly responsible for its own current owner clock after locking the policy, then remove the redundant immediately preceding `Now` from processing observations and action-bearing closure visits. Do not add a cache, batch query, new clock port, or new framework.

## Exact evidence and limit

Read the complete 408-line original race log and its results; parsed the cost/outcome and inverse-provenance records. Base directory: `/workspace/lerna-content-03-137311247276/large-graph-integrated-qualification-sql-overlay`. Raw SHA `4056da40ced0f8c23c4eb7ca66130d521bdbf7549588e206edbc396724e4a439`; cost JSON `9fe9aa6bdba9f7bf56c0191cedfabc40a335626394dd0e11b89f1663bc2b9d9d`; original outcome `16a4abdb2f0270390a93b8d0389e58f3e722bbca286db34c662eb87f243d8229`; reviewed outcome `a29f6fbf30e296ad5ee32e92f757e636028bf0fef6d93ae646fe29c7af30cf40`. Manifest `d957493c46349aca0351aabc930af80daeb38a5a9a6da2400cfe23dac0ade36e`, formatted inverse `fd47f140a6dcde1b1510d28a70d75db14aafc4ece91d59a47999e1b9e8a57707`.

Actual native 3744673/start15722102 exited 1 with original Wait/group-absence ACK. Closure64 completed 41 Materials; the 42nd Current timed out before Content; final Claim was 11.86ms late. Static62 completed all Materials and two Plans, then final Claim was 67.582ms late. Neither reached Prepared/publication/readback/Completed. These remain real failed capacity evidence; overflow/single-bound/duplicate successes do not close the failed positive tails.

Current measured `Now.sql`: 1822 calls/907.835ms and 1640/801.331ms respectively. Current CheckPolicy and LockVersion counts were 866/754. These totals establish substantial repeated clock-query work; they do **not** identify the exact time saved by removing the pre-policy subset, predict a speedup, or prove the remaining full chain fits Claim5. All Content child stages completed without error. SQL wall includes driver/network/wait; residual is not proven SQL wait; complete measurement overhead remains UNKNOWN.

## Why this exact deletion is sound

At `domain/content/closure.go:91–98`, `Now` supplies only `CheckPolicy`; after `LockVersion` the variable is overwritten by another actual Now before policy/cap or metadata decisions. At `domain/content/processing.go:52–56`, the first Now likewise supplies only CheckPolicy; the post-record Now is the one used for policy bounds and subsequent target/closure checks.

The sole production implementation, `adapters/postgres/content/policy.go:34–65`, does not use the supplied instant: its MATERIALIZED `FOR SHARE` policy CTE returns `body, clock_timestamp()` into `now`. It evaluates current validity from **that locked-row clock**. The earlier independent clock query contributes no qualification value. Preserve that existing SQL byte-for-byte and every full-subject/tuple/action/full-ref check; do not move the volatile clock into the CTE or sample it before the lock.

Concrete interface refinement: remove only the final `time.Time` argument from existing `CheckPolicy`, document locked-current-time ownership and fail-closed clock/query errors, and declare the adapter's scan destination locally. Update all current callers and the explicit `closureTimingRepository` decorator mechanically; no replacement method or optional fallback. Frozen `.txt` producers, schema, migrations, fixtures' original business inputs and archived evidence remain unchanged. Other callers' Now values used for admission, deadlines or other checks must remain. `AuthorizeUse` and other unrelated clock-elimination opportunities are outside this change.

Delete the two obsolete clock-query sites above, including action-bearing visits shared by Put/processing/other current closure consumers. Nil-action structural visits remain clock-free as before. Retain each post-record Now, each entire-closure final Now, both processing transactions, fresh SELECT/locks and rebuilt closure, all original exact-source caps, current policy action sets, target/ancestor BodySeal, Gone/record consistency, actual object read, byte/hash verification, and post-I/O qualification. Initial error returns from retained CheckPolicy, LockVersion and final clocks propagate unchanged; no ignored errors or nil-success fallback.

This is an internal clock-responsibility refinement, not a claim that every old transport-failure schedule is observationally identical: the removed SQL statement can no longer independently fail. It supplied no business authority. Remaining policy SQL/clock failures still stop before metadata/body disclosure; original malformed/current-policy checks keep their order relative to LockVersion/metadata. Never simulate the removed statement's error with a fabricated cause or retain it merely as an artificial delay. No domain constraint or public error vocabulary changes.

## Qualification and rejected alternatives

Use the existing genuine large-graph failure as the performance RED; do not invent a new business failure by asserting a private Now call count. After source review, run the affected finite semantic controls: direct policy-port real lock-wait normal/expired (`TestContentPolicyPortClockFollowsLockedRow`), all logical action intersections/invalid vectors, post-record wait expiry and exact-policy-before-metadata cases, target/ancestor seal before and during actual I/O, and original current Input/control/deadline gates. Preserve fault decorators at the real CheckPolicy boundary and post-record/final clocks; errors must return zero ProcessingRead. Check shared closure consumers' relevant admission/save/cleanup controls because that helper is shared. No private-row or mirrored counter replaces public behavior.

Then rebuild and qualify the original four exact B scenarios normal and race, count1, caller30/Claim5/Go+outer120, unchanged inputs/setup, worker/rule-start budget, and all public positive tails. A failed normal stops the planned race; no blind retries. New source must actually reach Prepared, both publications and independent readbacks, Completed and exact bytes/receipts/usage/reopen tails in each positive case. This decision predicts no green result and grants no acceptance from the measured run. Existing standards-cause fixes remain separately qualified.

Reject advisory+row SQL fusion: a single READ COMMITTED statement may establish its snapshot before waiting for the advisory lock, unlike the present next-statement row read. Reject dropping post-record/final clocks, full-policy/closure caching, shared cross-read identity storage, Source parse memo speculation, codec/schema shortcuts, pool/caller/lease expansion, heartbeat and fee reset. They are unnecessary to eliminate the proven discarded-clock value and would broaden the semantic proof.

## Source binding

WT `/tmp/lerna-worktrees/content-snapshots-03`, measured HEAD `b28b2e3182d97156c5fe203baf5cf2183631b7f2` plus explicit BodySeal processing/test WIP. Independently checked current SHA equality against the measurement's source331 entries:

| File | SHA-256 |
| --- | --- |
| domain/content/closure.go | 97447424e7765960288b878b9b3b0b37496b2e863a21946b769c22e0322e5c3f |
| domain/content/processing.go | b5cafb6ac64f96d7ff4a9bf119aaea7f47e50a2f1d78d4a15471f9b24c4f476a |
| domain/content/ports.go | 51066cd99f2c79e8a5fc3f7b336cb0706f161899361681c99f8e6feff5454f90 |
| adapters/postgres/content/policy.go | 3cb31490a70e0141daa24b4766295ea1a57598194877eda6f19e58371a2d45d9 |
| adapters/postgres/content/facts.go | e56785902e481eafdaa7904f44fabc44770d77f3d605da93906df827fe8a42d5 |
| components/decision_engine/worker.go | 663d0c96eae83a4175c66f2ceaf3b87688d9a8199d3998e647eff4868eb0ff27 |

All prior failures, possible effects, original Prepared facts, and original CPU descriptor CloseUNKNOWN remain; process absence never substitutes for logical resource Close. No ticket/whole exit or new cleanup claim.
