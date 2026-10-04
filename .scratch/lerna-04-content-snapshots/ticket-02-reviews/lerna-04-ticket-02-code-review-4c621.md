# Fixed code review — 4c6219f

## Standards

# Standards — fixed 4c6219f

BASE `f05b2f1068958ba6b63e6c1dc58d6cd1684446cc` → HEAD `4c6219fb420cdd63ea67f6db4c0106779ea9ccfa`: actual **10 commits/54 paths**, 8548+/218−. Full-read all three changed fixed files and their129+/7− delta from6a9. My full6a9 review carries through actual mode/type/blob equality for51 range paths, all909 other tree entries, standards sources,73 protected objects and21 archive objects. Archive hashes independently rechecked; full original raw/restore/provenance qualification carries. Scope: `/tmp/lerna-04-ticket-02-standards-4c621-scope.json`.

**Hard: 0; worst none.** Original processing-deadline and all five SQL Close-location findings remain CLOSED; no new documented-standard breach.

`adapters/postgres/content/policy.go:44` uses “`WITH locked_policy AS MATERIALIZED (…) SELECT body,clock_timestamp() FROM locked_policy`”; the outer volatile clock depends on the row obtained by LockRows. Exact subject/ref/purpose, five independent flags, no-row and native error handling remain intact. Domain clocks, final admission gates and same-Tx responsibilities are unchanged. This follows AGENTS.md:105/120/125 and adopted after-policy-group-perf decision§4–5; no earlier statement/transaction clock or independent clock CTE is substituted.

`conformance/component/content_policy_actions_test.go:285` directly exercises real CheckPolicy with finite normal/late lock controls, preventing later domain clocks from masking an early sample. EXPLAIN checks are explicitly mechanical diagnostics, separate from public business oracles. `conformance/internal/contentfixture/policy_lock.go:154` attaches the extra connection's sticky Close to existing World ownership before Ping, registers its backend, strips conditions/filters from plan output and joins Scan/iteration/Close causes. Cleanup stops on infrastructure Close failure. These preserve AGENTS.md:104–105/147; plan strings do not control business decisions.

**Possible smells: 1; worst P3.** Retained optional **Duplicated Code**, `adapters/postgres/content/management.go:71,299,395,439`: typed page readers repeat “`if len(…) == limit { next = last; break }`”, scan/decode/cursor/joined-close shape. Fowler suggests minimal shared consumption; KEEP remains an optional tradeoff, not a hard gate. The exact fixture SQL copy serves the required actual-query-plan consumer.

All12 assessed/carried: Mysterious Name, Duplicated Code, Feature Envy, Data Clumps, Primitive Obsession, Repeated Switches, Shotgun Surgery, Divergent Change, Speculative Generality, Message Chains, Middle Man, Refused Bequest. Repo closed cases, consumer ports/decorators and frozen provenance override heuristics; format/vet/types/whitespace excluded as tool-enforced.

Read-only; no reviewer native execution. Supplied original60/120 full64 race54.75 and focused7.218 passed; explicit early-clock mechanical variant1.069 failed. Final170-runnable inventory/check/audit/source delivery/CI remain pending; seven AC unchecked. Future CI candidate excluded.


## Spec

Independent SPEC followup: base `f05b2f1068958ba6b63e6c1dc58d6cd1684446cc` → fixed source `4c6219fb420cdd63ea67f6db4c0106779ea9ccfa`; merge-base equals base. Full range: **10 commits,54 paths,8548+/218−**. Independently qualified51 previously reviewed paths by exact mode/type/blob equality against my6a9 scope; fully read all three changed objects (129+/7−).909 other tree entries are equal. Origin spec, ticket02/handoff/oracle, adopted requirement decisions and all seven AC remain covered; detailed object/AC qualification is in `/tmp/lerna-04-ticket-02-spec-4c621-scope.json`.

**Findings: (a) missing/partial0; (b) scope creep0; (c) implemented wrong0. Worst:none.**

CheckPolicy now materializes the exact owner/subject/resource/purpose row under FOR SHARE, then samples volatile clock_timestamp in the outer projection consuming that locked row. It preserves complete SubjectBinding, finite independent action intersection, current ValidUntil, exact resource qualification and error handling. Domain clocks and target Get F1 binding/order remain unchanged; later domain gates are not treated as proof of this port clock.

The new direct-port test uses real PG blocking and normal/expired ValidUntil cases: the expired result must be nil before any domain operation. The corresponding public Put wait cases remain. EXPLAIN assertions are explicitly mechanical diagnostics, with finite world-owned SQL connection closure and filtered output, rather than a business oracle. No additional public protocol or methods appear.

My three earlier P2 closures and complete closure/alias/replay, same-Tx work, immutable caps, original deadlines, expiry handoff and recoverable legacy backfill remain qualified. All155 frozen contract/SDK/fixture objects equal base;0001/0002 and three raw archives equal6a9, and all18 archived hash entries were independently rechecked.

No reviewer tests/build/native/DB/environment operations or other-axis input. Parent reports focused7.218 and original64/65/finalGet race54.75 succeeded; the explicit early-clock mechanical variant failed1.069 and was captured after failure, not precompiled. These are external execution statements. Earlier6a9 race60.08 failure remains history; its normal31.931 does not qualify4c normal. Full normal/race suites, inventory/check/audit/CI/delivery remain pending at this cutoff. Seven AC remain unchecked; this source review is not acceptance or whole04 completion.


Standards: 0 hard / 1 optional possible P3; Spec: a0 / b0 / c0. Axis findings remain separate; all final execution/audit/CI/acceptance limitations remain.
