# 03 single-read identity: next decision (STATIC)

**Select one bounded measurement, not another speculative product memo:** partition the current worker’s Content qualification time into actual SQL calls and local processing, in one original four-B race execution. No new CPU profile, unchanged-product blind rerun, or product fix is selected now.

## Facts and missing evidence

I read all 158 normal and 155 race log lines in untruncated chunks and inspected their completed outcome metadata. Normal completed both positive chains, including Prepared, two publications/readbacks, Completed and public tails. Race exited 1 with actual Wait/group absence: closure64 completed 61 materials and two Plans but its pre-Prepared Claim check was 67.526ms late; static62 spent 2.499005533s in Snapshot and 1.969329467s in Lock before its first material’s 442.315506ms Content read timed out. Its final Claim was 19.525ms late. Neither race case reached Prepared/publication. These are accumulated original-lease failures, not proof that the last short call caused them. Inclusive durations must not be added.

Current `ReadForProcessing` already shares only successful full-ref identities between its two fresh observations. `registeredClosureWithIdentities` still rebuilds facts and checks every ancestor. PG `LockVersion` independently validates identity, acquires its advisory lock, reads FOR UPDATE, decodes and checks shadow columns. `CheckPolicy` separately validates subject, obtains the locked row/current clock, decodes and evaluates actions. Source Snapshot/Lock read four distinct real objects. There is no demonstrated redundant authority-free result whose next reuse is both worthwhile and currently measured.

The historical 427bbc CPU sample predates both closure changes. Its nested Content weights cannot establish current removable cost. Earlier subcost data measured whole CheckPolicy/LockVersion/Now calls, not their SQL-versus-local partitions; current coarse logs cannot distinguish those costs or transaction overhead. The changed failure prefix does not prove a regression or a speedup.

## Single diagnostic scope

On an exact overlay of the current bytes, retain the existing five phase observers. Add only bounded worker-Content subcost counters, distinguished by testcase, read role (shell/M/lock/manifest/material/publication verification), and pre/post observation. Attribute the same invocation, not inferred timestamps or compiler totals.

Partition serial callback wall time into: LockVersion identity/key preparation, advisory Exec, row QueryRow/Scan, decode/validation; CheckPolicy subject preparation, locked QueryRow/Scan, decode/actions; Now QueryRow/Scan; remaining callback work. Record Within-minus-callback separately as transaction-boundary residual, **not** as proven database wait. Keep object I/O/hash separate. Each child interval belongs to one category; parent totals remain explicitly inclusive. SQL-call wall time includes driver/network/server wait and must not be labelled server execution time. Preserve typed first error and completed/attempted counts.

Use fixed-size aggregate storage, no per-row logging, body output, new SQL, pool changes, or background sampler. Flush after the existing Step returns; record observer overhead and dropped/ambiguous attribution. One original four-B race, original 30s caller/5s Claim/120s aggregate, count1; STOP after its actual outcome. This specifically fills the missing current partition, not a diagnostic matrix. Do not reuse an overlay that overwrites the two current memo changes.

All current Input/control, policy/ref/ancestor gates, fresh clocks, physical reads, RuleStarts1, fees, publication recovery and Close responsibility remain unchanged. Preserve every prior failure and the original CPU descriptor Close UNKNOWN. This report performs no native execution and does not qualify ticket completion.

## Exact bindings (SHA-256)

WT: `/tmp/lerna-worktrees/content-snapshots-03`; HEAD `28e3a468c86d4323d3b497836de23bb311a58952` plus measured WIP.

- closure.go: `8897d39b01c1872bf9a5ccf1bbdc9f6105e375e6418781c7ce9574c35b56bab5`
- processing.go: `4a4440cb921001d3edb3d2e829f49b5e684c60d31e0603bcfe9b76a8d6283c30`
- PG facts.go: `8d53cb773a4711ff9866f5974b399de96eec137d82a92b97886c0825802cb069`
- PG policy.go: `3cb31490a70e0141daa24b4766295ea1a57598194877eda6f19e58371a2d45d9`
- Source source.go: `f3342f0bfefabd1adeb59d8fa8566e4af70dfb64ea41185aa721386a04a3df27`
- Frozen worker.go: `663d0c96eae83a4175c66f2ceaf3b87688d9a8199d3998e647eff4868eb0ff27`
- Normal raw: `532b781aa2bd7884f574670c5cdb99ff92705a8ee76ab409fd58ef1c42d76d63`
- Race raw: `f4f8681e6c57e9d84153f0f82e819df5de757dd20ca6c662c4bd160689b12352`
- businesses-actual-outcome.json: `3fa77f7ea8c92723ce5ce15ccc9c09d60ef26c08dfc498b866dcb91898e170ed`
- Source manifest: `f9492989728dd437749023e766057d2a904c82637120b06233705ed30def89ec`
