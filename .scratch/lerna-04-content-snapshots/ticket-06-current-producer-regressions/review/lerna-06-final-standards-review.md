# Standards

Reviewed frozen HEAD `c11fd2092875417d039914f8ecbd083a7a846d3a` plus manifest-pinned WIP against `dadd801cfd11ca038f0496872941097f10ebe507`, using both recorded diff commands and all seven commits. All three source hashes and three diff hashes match the manifest; complete source hunks reviewed. Rules: /workspace/lerna/AGENTS.md, CONTEXT.md, docs/agents/domain.md and issue-tracker.md, ADR-0005/0010 and architecture/validation.md. No nested AGENTS applies; docs/agents/testing.md is absent in both root and worktree. Tooling-enforced checks are excluded.

## Documented violation

**P2 — retain setup error causes before sanitizing diagnostics.** AGENTS.md:105 requires “错误必须保留可判断的原因…不得吞掉错误”; :130 prohibits logging credentials, which permits safe classification rather than dropping causes. In `conformance/component/content_process_recovery_test.go:235–260`, failed Getpgid/Open/Read/first Close/ParseUint calls return only new generic errors, e.g. `return 0, 0, "", errors.New("child identity read/first Close unconfirmed")`. Read and first-Close errors are discarded even when both occur. In :309–345, OpenInherited/Receive/Emit/Lstat failures similarly produce only generic fatal text; e.g. `if pipes.Receive(ctx, &cfg) != nil { t.Fatal("child exact-scope descriptor unavailable") }`. These pre-open failures cannot retain cancellation, I/O or decoding causes through the existing safe cause classifier. Preserve underlying errors internally with a sanitized wrapper/Unwrap or join, and report bounded cause classes; do not print raw credentials, paths or protocol contents.

No additional documented violations found in `process_borrow.go` or changed `world.go` hunks: actual Wait/pipe Close remains separate from sticky native-Close UNKNOWN and prevents destructive cleanup.

## Optional heuristic

**P3 — possible Duplicated Code / Repeated Switches.** The holder-observation blocks at test :1027–1052, :1287–1312 and :1601–1626 repeat `seen := map[string]bool{}` plus the same `switch holder.Kind` validation. A small test assertion helper could centralize full holder membership/binding checks while retaining each scenario’s independent deadlines and physical counterpart oracle. This is optional; avoid a generic recovery framework (AGENTS.md:108).

Qualification remains honestly pending; first current normal PASS does not claim remaining14, final checks, audit or integration complete. Static only, no native checks or source edits. Standards: 1 documented P2, 1 optional P3. Spec remains separate.
