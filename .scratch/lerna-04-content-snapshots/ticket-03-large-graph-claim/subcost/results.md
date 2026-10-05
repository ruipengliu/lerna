# Original B single subcost experiment — actual qualification only

Tracked source `d0ccc114f1255f6118fb94b794a1342c93050fc4`, product `5e8cad7e9d77162fda9773bde747235f8937c9ce`. Single exact binary B run native3142143/start13113594, session51660 actually joined with exit1/groupAbsent true; no pending native and explicit RELEASE sent. Format3139999/start13104390 and standalone race build3140674/start13107291 each actual0/absent. Binary SHA c6449bc1e478266e06db0dff82a25ec2e5215d167617ec399c36f3061b376425, formatted manifest3b0dead588bd381c0b6cae4e5abdf874b4ca372b22a1ceae88365e3a61c4e0d0, map a5a5df470d9670a84ddf853b0aaea808de2791d566bbe49d861dbbcd4804ebf9. Old original five-file overlay unchanged. Caller30/Claim5, original Input limits/read budgets/locks/two transactions/post-I/O actions unchanged; timing/race instrumentation overhead is nonzero.

Raw log `/workspace/lerna-content-03-137311247276/context-final-capacity-b-subcost-diagnostic-race.log` SHA `8f50183586e7746a787833e65ade7defb2335d5ecd0f6206bfad948bd928033c`, 141247 bytes, final mtime_ns 1791162972559517632; wrapper started_ns1791162897599874198 to final-log mtime74.959643434s is wall observation, not a Go package duration (binary invocation prints no package duration). Detailed all-phase success/failure/total/max/bytes source is `large-graph-subcost-results.json`; the raw log remains authoritative.

Actual closure64 FAIL29.52s; closure65 expected-overflow PASS12.43s; static62 FAIL21.98s; Content layer normal/early-refusal PASS10.29s; duplicate-selector refusal PASS0.61s. Both normal scopes recorded178 keys, dropped0, ambiguous0. Expected closure65 has40 keys and no worker invocation. No helper compile failure, no business retry, no product optimization.

## First cause and final gates

Closure64: Step24.367749399→29.465100357s (5.097351893s). Claim DB now01:15:22.122576Z→01:15:27.122576Z exactly5s, epoch1/revision1. Real running sequence1/Prepared=false committed24.526141477s. First actual failure at29.452431154s is Content.worker.pre.CheckPolicy context deadline; caller still has547.533237ms. Final ValidateClaim supplied01:15:27.134384Z is11.808ms past original lease. There is no Prepared save/commit or worker publication. Worker reads23attempts/22successes; final failed read never reached Objects.Read or post-Tx. Compiler65/65 and independent1/1 reads actually completed both original transactions and physical I/O.

Static62: Step16.807450295→21.907123399s (5.099674077s). Claim DB now01:15:56.518014Z→01:16:01.518014Z exactly5s, epoch1/revision1. Real running sequence1/Prepared=false committed17.022062213s. First actual failure at21.896701648s is Current.InputRowScan pgconn timeout; caller still has8.10329318s. Final ValidateClaim supplied01:16:01.526669Z is8.655ms past lease. No Prepared save/commit or worker publication. Worker38/38 processing reads completed both original transactions and physical I/O; the next Current fails before its material read. Compiler65/65 and independent1/1 also completed both transactions and I/O.

## Worker prefix costs

Cells give success/failure counts and inclusive wall total. Current closed-all includes strict parse/decode/EOF; Within includes callback; facade includes pre/post/Objects. Do not add parent and child rows. Scan contains lock/server/network/Scan overhead, not pure SQL CPU. Failed prefix calls are not complete-material means.

| Actual stage | closure64: success/failure; total | static62: success/failure; total |
|---|---|---|
| Current overall | 20/0; 1.041688083s | 35/1; 1.895625801s |
| Current Binding row+Scan | 20/0; 34.87421ms | 36/0; 60.333627ms |
| Current Binding strict parse | 20/0; 316.057142ms | 36/0; 626.601854ms |
| Current Binding typed decode | 20/0; 380.174491ms | 36/0; 697.68542ms |
| Current Binding closed-all (inclusive) | 20/0; 696.716119ms | 36/0; 1.325343142s |
| Current Input row+Scan | 20/0; 15.580959ms | 35/1; 38.03629ms |
| Current Input closed-all (inclusive) | 20/0; 160.647099ms | 35/0; 292.612708ms |
| Current full Input comparison | 20/0; 6.265067ms | 35/0; 12.316113ms |
| Current full Permission comparison | 20/0; 970.437µs | 35/0; 1.697603ms |
| Current trusted clocks | 60/0; 21.090942ms | 106/0; 57.433071ms |
| Current commit | 20/0; 83.005544ms | 35/0; 65.97476ms |
| Content processing facade (inclusive) | 22/1; 3.698027987s | 38/0; 2.785695864s |
| Content pre Within (inclusive) | 22/1; 1.748489397s | 38/0; 1.368322059s |
| Content pre callback (inclusive) | 22/1; 1.648594392s | 38/0; 1.268672774s |
| Content pre CheckPolicy | 365/1; 268.553767ms | 349/0; 254.706856ms |
| Content pre LockVersion | 365/0; 480.264892ms | 349/0; 432.636517ms |
| Content pre Now | 753/0; 276.382934ms | 736/0; 248.818597ms |
| Actual Objects.Read | 22/0; 3.144903ms | 38/0; 5.381774ms |
| Content post Within (inclusive) | 22/0; 1.934175707s | 38/0; 1.391336985s |
| Content post callback (inclusive) | 22/0; 1.852048794s | 38/0; 1.294027286s |
| Content post CheckPolicy | 364/0; 307.325109ms | 349/0; 260.507436ms |
| Content post LockVersion | 364/0; 551.738978ms | 349/0; 449.720279ms |
| Content post Now | 750/0; 320.228997ms | 736/0; 260.760278ms |

Within Current, Binding closed-all dominates:696.716ms of1.041688s for closure64 and1.325343s of1.895626s for static62. Input closed-all additionally160.647/292.613ms; full Input/Permission comparisons are6.265/0.970ms and12.316/1.698ms. Actual object I/O is3.145/5.382ms total in these prefixes, while Content processing facade is3.698/2.786s, mostly two full policy/closure transactions. Actual Content pre/post policy calls366+364 for closure64,349+349 for static62; LockVersion365+364 and349+349; Now753+750 and736+736. These contain full ancestor qualification and are not interchangeable with a cached revision or previously observed authorization.

This establishes substantial strict Binding decoding and repeated Content qualification costs, without proving that a single proposed saving will finish all remaining materials, Prepared and publication within5s. Content unmeasured callback remainder, transaction mechanics and local processing remain mixed costs; do not attribute remainder to hashing or FS. Current/Content tables represent incomplete calculation prefixes; no linear extrapolation into an unvisited publication phase. RollbackAlreadyDone is the actual sql.ErrTxDone result (counted failure at the port), not positive rollback or first business failure. No failure is refunded/retried.

No product decision is preselected. Root/Astra can use this actual table to choose the next single equivalent change, retaining complete current Input/Permission verification, target/ancestor read+process, original locks/clocks/error priority, external physical I/O and fresh post-I/O transaction. Original shared-load B failure and previous exclusive diagnostic failure remain unchanged. Seven AC acceptance and complete large-graph normal qualification remain outstanding.
