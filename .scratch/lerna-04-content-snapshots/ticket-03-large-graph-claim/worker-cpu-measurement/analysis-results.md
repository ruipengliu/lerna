# Original28e one CPU sample — seven analysis views

Only the original3299533 sample was analyzed. Its profileSHA427bbc672bad5cf520934ed2e31116f75eca4991572fd5ecd0c3f7c3a4ff31e6 remains unchanged,144298B/dev27/inode560161. Original standard-testing CPU descriptor logicalClose remains UNKNOWN; this artifact is retained. Seven separately supervised locked Go pprof commands completed0/groupAbsent, actual identities and argv/logSHA in large-graph-worker-cpu-overlay/analysis-outcomes.json; tool session47110 fully joined before sole LOCAL release. No second sampling, business test, HTTP or product fix occurred. All seven original text logs were FULL read.

Profile duration78.40s, total sampled CPU62.18s. Tags show closure64=2.83s and static62=3.97s; together6.80s/10.94% of the whole profile. The remaining55.38s/89.06% is unlabelled, including setup/compiler/other cases and potentially asynchronous/runtime samples whose scope is not attributable. Do not allocate unlabelled race/runtime/GC CPU to either worker by assumption. Labels cover the two original Decision.Step calls only, not compiler or owner setup. All printed pprof percentages, even focused views, use whole-profile62.18s as denominator; the focused rows below report raw CPU seconds, not misleading focused percentage.

| Actual raw cumulative CPU node (overlap, never add) | closure64 label | static62 label |
|---|---:|---:|
| Actual Step wall in sample log |3.385588382s|5.091103354s|
| Total CPU with case label |2.83s|3.97s|
| Service.Step Go-stack cumulative |1.11s|1.74s|
| Content.ReadForProcessing cumulative |1.00s|1.25s|
| registeredClosure cumulative |.98s|.97s|
| registeredClosure.visit cumulative |.92s|.86s|
| VersionIdentity cumulative |.54s|.45s|
| v1_2.Encode[ContentRef] cumulative |.51s|.48s|
| PG Store.LockVersion cumulative |.28s|.42s|
| sortRefs cumulative |.22s|not in bounded top30|
| v1_2.Validate cumulative |.23s|not in bounded top30|
| ContextDispatcher.binding cumulative |not in bounded top30|.32s|
| runtime._System cumulative |1.18s|1.48s|

Focused flat samples are dominated by actual race symbols: closure64 __tsan_read .65s, racecall .51s, MemoryAccessRangeT .12s, runtime.raceread .09s; static62 __tsan_read .73s, racecall .71s, MemoryAccessRangeT .27s, futex .16s, racecalladdr .14s, __tsan_write .13s. Whole-profile flat racecall11.65s/18.74%, __tsan_read11.26s/18.11%, MemoryAccessRangeT3.27s/5.26%; whole cumulative Content registeredClosure9.26s/14.89%, VersionIdentity4.77s/7.67%, Encode[ContentRef]5.32s/8.56%. These whole rows include unrelated setup/compile/normal cases, not measured worker-only costs.

The bounded views display fewer tiny nodes: drop thresholds are whole-profile cumulative<=.31s, and nodecount30. Absence of current Input decode/typed clone/GC/specific pgPolicy row decode from a focused top30 is not proof of zero CPU or permission to remove them. Whole validation and JSON nodes are positive, but the existing seven views cannot uniquely attribute every encoder invocation to a callsite. CPU vs wall subtraction cannot establish SQL wait, lock wait, network latency or future optimization savings. Go/race instrumentation CPU and sampled native race stacks are present; their stack attribution differs from Go-frame totals and must not be sum-counted.

This sample's own failure phase changed under nonzero sampling cost: closure64 caller30 expires after actual Step3.385588s and only first Material call, without Prepared/publication; original Claim was still future when admitted, and caller expiry is not rewritten as the earlier5s Claim failure. Static62 retains all62 Material successes, actual Prepared COMMIT, then original Publish/PrepareContent timeout before completed publication. The earlier unprofiled failure/raw remains authoritative for its own phase. Both source-current gates and all original bounds remain fixed28e. No capacity acceptance or optimization is claimed.

Static source correspondence examined after analysis: VersionIdentity first calls full v1_2.Encode(ref), then hashes only the positional tenant/owner/contentID/version identity; closure calls VersionIdentity for target/each visit and sortRefs. sortRefs already computes one identity per element before sorting, so do not claim it re-encodes on each comparator. Repeated identity conversion across different actual uses is a potential measured CPU investigation, but this report selects no product optimization or replacement validation. Root/Astra owns that decision and its exact fresh/current/error-order limits.
