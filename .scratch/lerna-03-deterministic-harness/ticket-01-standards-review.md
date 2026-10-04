# Ticket01 Standards review

Base `9a06bc1d895e354d29524f6b33622410c719bcc8`; final reviewed pin `696ac49846105a16f33e5de86dc621a3858651b2` (46 commits/187 paths). Full original range and complete follow-up increments reviewed against AGENTS, CONTEXT, relevant ADRs, approved accounting/lifecycle decisions and all Fowler heuristics. All 69 frozen payloads read; 68 archived production files matched immutable970fd90 bytes/provenance. Reviewed public seams, transactions, migrations, supervision, scripts and CI. Read-only; no tests/builds/DB/cleanup/worktree edits. Execution acceptance remains separately pending.

**Historical41cbf7c: four documented findings (one P1/three P2).** Repository-relative paths/lines in this paragraph refer to41cb.

1. **P1: portable restore scope.** `conformance/internal/decisionfixture/legacy_upgrade_test.go:660` used MkdirTemp("/workspace",…), unavailable in required ubuntu CI. Rules: `AGENTS.md:109,162` portable dependencies/real required CI entrances. Resolved255fd4a via TMPDIR.

2. **P2: retain failed closure/holders.** `conformance/internal/decisionfixture/world_source.go:133–137` discarded Open’s failed-close holder before Fatal, permitting schema cleanup. Rules: `AGENTS.md:104–105`; fixture README’s owned cleanup. Initial holder loss fixed255fd4a. Shared PG/Source first-close uncertainty, wrapper propagation, SQLite initialization/native-close-before-lock-release and concrete interface holders now fixedf96f859. Retryable SQLite draining remains distinct from sticky final failure; native-close errors do not prove a live socket or lock state.

3. **P2: immediate precise cleanup.** `legacy_upgrade_test.go:660–685` created a directory before registry operations but registered failure cleanup only at686. Failure leaked known owned scope. Rules: `AGENTS.md:104–105`; historical fixture README. Resolved255fd4a.

4. **P2: preserve failure causes.** `legacy_upgrade_test.go:87–89,746–749,776–794` discarded build/file/SQL causes. Rules: `AGENTS.md:105,147`. Resolved throughb1/f79/f96: all identified real reporting/receipt outlets retain safely wrapped causes; bounded diagnostics, Is/As and join semantics preserved.

**Final: zero actionable findings open; no new findings.** Lifecycle finding2’s production pre-start pipe residual (f96: legacy_upgrade_test.go:99–107) closedb43: exact parent/child ends retained before allocation errors, cleanup registered before Fatal, cached joined Close causes, successful Start/Wait handoff and unknown scope retention. Its new-test registration residual (b43: legacy_pipes_test.go:16–22,45–47,68–73) closes696: all three tests register returned cleanup before errors/assertions, deliberate-refusal fallback retains its known stdin pair, and the already-closed test logs the expected original cause and compares the identical cached outcome rather than clearing it. Full one-file final increment reviewed; production helper unchanged.

Preview authorization aliases and JS process cleanup resolved. No additional Fowler judgement findings. Mechanical sql.OpenDB tests own no physical resource and do not establish native-driver failure evidence.
