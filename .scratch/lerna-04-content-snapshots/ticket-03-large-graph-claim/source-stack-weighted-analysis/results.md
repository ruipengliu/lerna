# Existing CPU Source trace weights (one actual offline view)

Actual pprof exit0, PID3507329/start14701529; actual Wait/groupabsence and later recheck absent. Sole LOCAL STOP/RELEASE. No new sample, business, DB, rebuild, source change, second view, or data-race qualification. Original profile427bbc and original binary963905 identity/hash unchanged; this is not0514 current-source race.

Full native raw6058lines/472556B SHA f610d853b0a43883351415deedb0f90a33b6620c37d01dae70ea84f7babfc26e. Exact raw format qualified after execution: 189 sample rows, each10000000ns, phase=decision_step and original case labels, complete leaf-to-root stacks; no unknown case/root/phase, fractional weight, unparsed weighted block, or output bound exceeded. Each row allocated once. Final wrapper ACK is a separate empty block after final trace separator and was not treated as a frame. Raw remains unchanged.

| Case | Source | Inclusive sampled CPU ms | Old Content subtree ms | Current Access subtree ms | Pure representation sampled ms | Unresolved sampled ms |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| closure64 | ReadSnapshot | 500 | 440 | 20 | 40 | 0 |
| closure64 | ReadFixtureLock | 520 | 490 | 20 | 10 | 0 |
| static62 | ReadSnapshot | 460 | 420 | 10 | 30 | 0 |
| static62 | ReadFixtureLock | 410 | 390 | 10 | 10 | 0 |

Total1.89s matched CPU: Content1.74s (92.06%), Access60ms (3.17%), pure90ms (4.76%), unresolved0 matching weights. These percentages use only retained Source samples, not total62.18s profile/78.40s wall. Missing/outside-source/unlabelled weights are not reassigned. Off-CPU and original Claim/caller timeline cost remain unknown. A sampled0 bucket means no matching sample, never proof of zero work.

Pure sampled branches: closure64 Snapshot mandatory typed decode10ms, mandatory EncodeMandatory strict output reparse10ms, snapshot→ToRule20ms; Lock snapshot→ToRule10ms. static62 Snapshot mandatory typed decode10ms and snapshot→ToRule20ms; Lock snapshot→ToRule10ms. These are9 timer-weighted profile rows, not a confidence estimate or independent-run sample count. closed strict/typed parse, mandatory ValidateInput/CanonicalInput/canonical write, direct CanonicalInput/ToRule, other mandatory encode and projection glue have no retained matching row; their real costs cannot be inferred as zero. No parent/child cumulative total added.

The single mandatory strict-output row displays contract/v1_1.parseJSON directly under EncodeMandatory→DecodeMandatory (ParseJSON wrapper inlined). Its actual leaf runtime.mallocgc / slicebytetostring / Decoder.Token / parseValue path is retained. Post-output classifier changed only exact scoped parseJSON alias; original parser7c191... saved separately, actual1c383... SHA recorded. Access closedJSON and Content codec parse paths stay in their own branches.

Eight Source/assembly/ports/mandatory/types/codec objects match old28e byte-for-byte. Current4WIP includes a different domain closure map; old Content1.74s contains pre-map closure/identity work, and cannot quantify current residual cost or removed work. Pure90ms/9rows is limited evidence of existing work, insufficient to establish a large current perf benefit or predict Claim5 success. No product optimization selected or authorized by these results; root/Astra decides next necessary action. Existing original normal/race failures, static Prepared/possible Put/unknown resources remain retained; no AC/whole04/CI acceptance.

Original standard-testing CPU descriptor logicalClose UNKNOWN forever. Actual native Wait/absence, later artifact reads, complete parse and separate new raw-outputFD flush/fsync/Close ACK do not provide originalCloseACK. No artifacts cleaned.

Exact artifacts: ../context-final-capacity-b-worker-cpu-source-traces.log, prelaunch-registration.json, analysis-outcome.json, analysis-outcome-before-parser-cutoff.json, weighted-source-stacks.json, classify-traces.py, classify-traces-pre-output-format.py, commands-static.json. All full sample paths are in weighted-source-stacks.json.
