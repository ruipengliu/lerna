# 03 processing / accepted05 BodySeal sync — STATIC decision

**Necessary fix after the authorized integration:** add one direct-target seal rejection inside `ReadForProcessing`’s shared `observe` callback. Preserve the successful pure identity memo and import all accepted05 ancestor, direct-AuthorizeUse, record, PG, and object gates before testing. No performance change is selected here.

## Exact gap and placement

Current processing SHA `4a4440cb921001d3edb3d2e829f49b5e684c60d31e0603bcfe9b76a8d6283c30` checks current policy, locked record, fresh clock, exact policy-to-actual-ref, requested ref/publication and retention, but never `r.BodySeal`. A zero-source published target can therefore remain physically readable after a legitimate Seal commits. Sealing deliberately precedes erasure; valid current read/process flags do not override it.

In that callback, **after** the existing nil/ref/publication rejection, **before** bounds/closure traversal, add:

`if r.BodySeal != nil { return ErrProcessingForbidden }`

This preserves earlier authority/full-ref/error ordering and applies identically before I/O and before returning bytes. Keep the original post-record fresh clock, both final qualification clocks, full ancestor traversal and actual I/O/hash. Do not substitute `BodyGone`, `CleanupPending`, policy flags, or a new API. Both failed observations already return `ProcessingRead{}`; do not expose previously assembled refs/bounds or actual bytes on rejection.

Accepted integration `dadd801cfd11ca038f0496872941097f10ebe507` has the corresponding ancestor guard for nonempty actions and direct `AuthorizeUse` guard. Structural nil-actions traversal remains available for correctly authorized metadata. These guards must survive the memo merge; no broad closure rewrite is needed.

## First real red and qualification

After integration, before the direct-target product fix, create a fresh owned native fixture using the existing 20s testcase context, one fixed Seal deadline within the existing one-minute maintenance budget/trusted validity, and finite joined I/O barriers. Do not reset any deadline on reopen. Existing worker 30/5/120 and RuleStarts/fees remain untouched.

Prove normal real `ReadForProcessing` returns exact `alpha\n` with current read/process permission. Two independent cases isolate the missing gate:

1. Seal the exact zero-source target, using its original full saving Subject/Purpose and holder binding; independently confirm alpha still exists. A new processing read must return `ErrProcessingForbidden` and an entirely zero result.
2. Hold the existing mechanical barrier **after the actual object read**, commit the real target Seal while no observation Tx is held, then release/join. The post-I/O observation must reject identically. Use the existing permitted held-read control, not a fake byte provider or call-count oracle.

An ancestor-seal case may reuse that controlled window to qualify the newly merged actionful-closure guard. It must use an actual registered ancestor and its authorized original saving basis. Prior process-withdrawal controls do not prove seal behavior.

**Oracle correction:** Seal alone is not Gone. Accepted `Get.getMetadata` requires both `BodySeal` and `BodyGone`. Before cleanup, even valid metadata permission must not yield Gone; public body Get remains forbidden and independent alpha persists. After real lifecycle cleanup/holder acknowledgments, exact current metadata permission may yield Gone. Observe original seal/deadline/holders across reopen, fixed Command receipt and unchanged published history; never infer all-holder erasure solely from primary Gone. No private Job queries.

The first red is still unexecuted. After the fix, require these affected normal/race semantic controls before rebinding the single future 026 measurement to the integrated/fixed snapshot. Retain prior failures, unknown resources and profile Close UNKNOWN. This report neither runs native work nor accepts ticket03.

## Additional source bindings

- Current closure: `8897d39b01c1872bf9a5ccf1bbdc9f6105e375e6418781c7ce9574c35b56bab5`.
- Accepted05 closure: `9b7ad1b320dbecabad9d4d6593adc1d4b458bcf9d9fd33452b3338683080d1c0`.
- Accepted05 lifecycle: `0708c4eb51e28ab9b7a3210981e408ae2310511e9728ee2bf46ba87fc0802699`.
- WT inspected: `/tmp/lerna-worktrees/content-snapshots-03`; accepted objects read with `git show`, not assumed already merged.
