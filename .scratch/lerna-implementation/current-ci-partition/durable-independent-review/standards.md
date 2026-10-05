# Standards

Fixed review: `b9db5cc67647a15d6556fe92941757034a1e80d1...d0fcc61264904070ce94e1146f07327a0ef9b2aa`; nonempty 214-path diff, four changed source files. All six recorded commits and complete source hunks reviewed. Standards: root AGENTS.md, CONTEXT.md, docs/agents/domain.md, docs/agents/issue-tracker.md, ADR-0010, and the code-review skill’s Fowler baseline. No nested AGENTS.md applies. Tooling-enforced formatting is excluded.

**Documented breaches: 0.**

- `scripts/test-component-integration-race.sh:85–106`: concrete Content/Durable partitions preserve dynamic discovery, anchored selectors, empty-group handling, original finite package limits and first-failure status. This respects AGENTS.md:149 (“不能跳过必需套件后返回成功”) and :162 (CI reuses established entries). No new domain abstraction or business change conflicts with CONTEXT.md or domain.md.
- `scripts/component-integration-race.test.mjs:191–441` and both `scripts/testdata/component-{content-97,durable-60}.json` files: independent literal expectations, normal/fault counterparts and bounded existing ownership helpers conform to AGENTS.md:135,147. Their provenance explicitly limits the evidence to the mechanical Go-tool seam, consistent with :145 and ADR-0010. Repeated literal inventories are intentional independent oracles, not a duplication finding.
- Current qualification, architecture assessment, exit evidence and repair issue preserve original failures/unknown resource facts and explicitly leave shared normal/race, current checks and new CI pending. They do not claim completion contrary to AGENTS.md:164 or the issue-tracker workflow. Historical Run changes are outside this source review.

**Optional P3 — possible Duplicated Code:** `scripts/component-integration-race.test.mjs:232–239`, `337–344`, `369–376` repeat `for (const flag of [...]) assert.ok(args.includes(flag), flag)` followed by the same race-mode assertion. A small assertion helper could keep command-contract updates consistent; retain independent literal selectors and expected groups. This is a heuristic maintenance suggestion, not a documented breach or required refactor; avoid a generic partition framework (AGENTS.md:108).

Static read-only review only; no native checks or worktree edits. Standards: 0 hard findings, 1 optional P3. Spec is reviewed separately.
