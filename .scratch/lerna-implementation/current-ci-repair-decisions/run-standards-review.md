## Standards

Checkpoint: base `75f0429d709d95b6a63137c397fcd9d80203ed5f`, HEAD `3570180514ce0a6cee4daa27430540003574533d`; `git diff 75f0429d709d95b6a63137c397fcd9d80203ed5f...HEAD`. Commits: `0c1f924`, `2f961a0`, `5ebd91d`, `3570180`. HEAD remained fixed; four code files reviewed, remaining paths are evidence archives.

**Hard documented-standard violations: 0.** `Run` retains the original caller, cancels its owned child loops, joins them, and wraps all lane causes with `%w` before `errors.Join`. Adding only the original caller’s nonnil error follows `AGENTS.md`’s context/ownership and “错误必须保留可判断的原因” rules; it does not interpret cancellation as absence of effects. Runtime scheduling, Claim and storage authority remain unchanged, consistent with ADR-0004/0005.

The two injected transaction-entry tests clearly qualify the Host diagnostic seam. The PG fixture blocks actual coordination SQL and uses catalog data for administrative identity/wait provenance; its business tail reads public projections, reservations and original receipts. This respects `AGENTS.md`’s permitted Host storage-mechanism testing while avoiding private facts as business acceptance. Registration timing limitations and UNKNOWN Close remain explicit. Pending affected checks, PG race and shared CI are accurately reported as partial work, not falsely completed delivery; their absence at this checkpoint is not a code-rule violation.

**Optional P3 — possible Duplicated Code (heuristic).** `host/durablework/pool_run_test.go:46` and `pool_run_live_fault_test.go:40` repeat the same `done`, `joined`, `releaseOnce`, cleanup cancellation/release and finite join shape. A small test-only ownership helper could concentrate that setup. Keep each scenario’s release ordering and original-caller assertions explicit; no generic fault framework is warranted. This is non-blocking judgement, not a documented breach.

All twelve Fowler heuristics considered with repository overrides; the intentionally unused embedded repository ports are explicit mechanical assembly, not a Refused Bequest finding. Tool-enforced formatting and archived whitespace excluded. Findings: **0 hard, 1 optional P3**.

FULL read: four current code files, product change, qualification note, ADR-0004/0005 and `docs/agents/domain.md`; relevant existing PG-holder helper and caller-bound sections. Previously FULL-read `AGENTS.md`/`CONTEXT.md` were byte-compared identical and relevant rules reread. Evidence archive inventory only; no archive-wide audit claim. No product edits or Go/Node/PG/formatter/native execution.
