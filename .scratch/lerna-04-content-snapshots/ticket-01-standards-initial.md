Standards — CLEAN INTERMEDIATE

Fixed 64c6872c8ed56ddd66dd51c98dc430046fc9f47d…92d27992f37f0a425621bc75c6d5b4a073fa06a6. Reviewed all four commits and 74 paths, including historical fixes, complete schemas/generated declarations and all 101 fixtures (repeated bytes represented losslessly). Verified 79 frozen old-version objects equal by mode/blob. Scope: /tmp/lerna-04-ticket-01-standards-initial-scope.json. All 12 Fowler heuristics considered with repository overrides; tooling-enforced checks excluded. No native/build/test/DB execution or opposite-axis reports. This is intermediate; pending gates and eight unchecked AC remain unaccepted.

Hard violations: 5; worst P2.

- P2 — domain/content/service.go:357–376: “Objects.Read(bounded, …)” proceeds directly to “NewContentGetResponsePublished”. Revoking read/disclose permission or narrowing retention during object I/O still releases bytes. AGENTS.md:125 and ADR0006 require authorization/deadline checks at actual action boundaries; recheck current policy before disclosure.
- P2 — domain/content/service.go:293–332,405–442: “now.Before(cutoff(request.AcceptBefore))” occurs before blocking policy/version reads. Later checks omit that cutoff; crossing it while waiting can return found/not_found/publication or start object I/O. AGENTS.md:125/ADR0006 require fresh execution-boundary deadlines; recheck after waits, including absent records.
- P2 — adapters/objectstore/local/store_linux.go:58–64: Lstat/EvalSymlinks errors return only “ErrUnavailable”. ENOENT/EACCES causes become uninspectable. AGENTS.md:105 requires preserving distinguishable causes; join the native error.
- P2 — conformance/internal/contentfixture/world.go:112–125: “errors.Join(err, f.Sync(), f.Close())” never records first Close failure in setupCloseErr, although Cleanup:176 checks that field before deleting scopes. Its README:8–10 requires every setup file’s confirmed first Close and retention on unknown closure. Related local/lifetime_test.go:29–43 fatals after dir.Stat/parent.Open failure before registering acquired handles; local README requires explicit lifecycle ownership. Register ownership immediately and retain unknown closure.
- P2 — scripts/test-contract-1_2.mjs:185–198 (also test-contract.mjs:244–257, test-contract-1_1.mjs:243–244): unguarded “record(`producer_exit …`)”, stat/rm can throw before aggregated original failures. test-generator.mjs:249–251,294–296 likewise throws from finally. AGENTS.md:105 forbids swallowing causes; aggregate cleanup/ledger failures with the original error.

Judgement smells: 1; worst P3.

- P3 — possible Duplicated Code: “function useFD(path, flags, action)” repeats across three contract scripts; “function ackGeneratorDirectory(dir)” repeats across both generator scripts. Fowler baseline recommends extracting the shared mechanical FD/ACK/closure shape. Frozen product namespaces justify codec copies, not these shared-tool duplicates.

Archival provenance, added by the sole fixer without modifying the above initial
finding text or reviewed pin: the exact initial reviewer scope JSON is retained at
[ticket-01-standards-initial-scope.json](ticket-01-standards-initial-scope.json).
It binds the four commits, all74 reviewed path modes/blobs and79 frozen objects to
the original Git objects; the earlier /tmp pointer is preserved as historical
provenance, not the only available review artifact. Final independent follow-up
must qualify the new fixed source rather than reusing this initial verdict.
