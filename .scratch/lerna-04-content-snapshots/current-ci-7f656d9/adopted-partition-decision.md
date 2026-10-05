# CI finite Content partition — STATIC decision

**Adopt three fixed discovery-order round-robin Content groups as the next minimal tool candidate.** Change only the existing two-array partition into `content_a/b/c`, selected by `index % 3`. Keep the separately discovered closure case, durable and other groups, and execute groups sequentially. At this checkpoint, 97 Content runnables become 33/32/32. This is not a proven runtime bound or a promise that three groups suffice.

## Evidence and scope

Fixed source `7f656d964a1f73aeb333ac1e1217be1ed405e4b8`, CI `37275970295`: normal recovery, all five Component groups and fixture packages passed. Normal Content groups took60.086s/94.440s. Race recovery65.822s, closure31.361s and content_a75.024s passed; content_b hit the package alarm at120.039s. Subsequent durable/other/fixture race suites were not executed.

The running test was printed as `(0s)`, so this does not establish expiry of its own20s context. More precisely than the initial summary, its active stack was `CopyToSecondary → SaveSecondaryCopy:93`; connection-opener creation frames do not prove execution was still only NewPG. Neither that stack nor the package alarm identifies a business correctness defect.

Two groups no longer fit the demonstrated aggregate workload. One additional group is the smallest local partition adjustment. Discovery-order round-robin is reproducible and does not require guessed constant per-test costs, hardcoded test inventories, learned timing files, or a generic scheduler. Do not project measured wall time by division or automatically keep increasing groups after a failure.

## Required preserved behavior

Retain dynamic Go discovery of Test, Example and Fuzz seed runnables; benchmarks remain excluded because the default command does not execute them. Each discovered runnable appears exactly once in the disjoint positive unions, including future Unicode names. Keep literal RE2 escaping and whole-name anchors. Duplicate, empty-total, malformed or failed discovery remains an error. Empty individual groups must produce no Go invocation, never an empty selector. Keep `-p=1`, `-count=1`, `-tags=integration`, `-timeout=120s`; race adds only `-race`. Preserve immediate nonzero propagation and no later-group execution after failure. No test body, lease, permission, deadline, frozen fixture or public contract changes.

AGENTS and02/03 require finite lifecycle, complete real database/recovery coverage and honest limits. Splitting package invocations respects these obligations; it does not extend an individual business deadline or certify canceled-resource cleanup.

## Qualification

Update the existing Node external-tool mechanical controls, not a new framework. Assert exact discovery-order modulo-three membership for small boundary inventories and97 Content names, normal/race identical sets, empty-group safety, Unicode/escaped literals, Example/Fuzz preservation and exact-once union. Inject failure in the new third group and require its exact status with durable/other not launched. Existing discovery and original group-failure controls remain. These prove command selection, not PG behavior.

Then exercise the shared normal and race entry on the fixed new source, recording census, selectors, every group’s actual exit and original resource identities. Recovery/fixture checks retain their shared-entry requirements; unexecuted old race tails cannot be inherited as passes. New CI must qualify the new tool pin. If another timeout occurs, retain the exact incomplete group and stop for its evidence; no blind retry, hidden omission or120s increase. 03 BodySeal, cancellation repair and06 gain no new dependencies. No native execution was performed here.

Bindings: script SHA-256 `eb62be917cb40604c910f1ecc7a5250d967cd5e11c00cd3b4c1af780773eef7c`; mechanical test `32d692c1aebf911066f71fdf4e9d33d4e96b3df433926b7e71722959a17f73c8`; CI log `d83fb423dd55227f4eb95e610ee786c78e400113e40f7ecdee9983cdda072b7f`.
