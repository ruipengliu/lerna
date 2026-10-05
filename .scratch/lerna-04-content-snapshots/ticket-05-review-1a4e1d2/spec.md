# Ticket 05 independent Spec axis

Read-only review of `/tmp/lerna-worktrees/content-snapshots-05`. No code, tests, native effects, PG activity, process cleanup, merges, or repository edits were performed. This file is the authorized review artifact outside the repository. No Standards/Astra/other-axis conclusions were read. No acceptance or aggregation is made here.

Fixed base: `1373112472761d32c22ed6d7603d806d8ef9a3c7`.
Fixed candidate: `1a4e1d2d704261a59491dfc6e0c5d26b80ec5107`.
Exact comparison: `git diff 1373112472761d32c22ed6d7603d806d8ef9a3c7...1a4e1d2d704261a59491dfc6e0c5d26b80ec5107`.
Both refs resolve; comparison is nonempty: 146 paths, 21823 additions, 233 deletions. All candidate source/spec reads were made with `git show 1a4e1d2d704261a59491dfc6e0c5d26b80ec5107:<path>`; source pointers below are candidate one-based lines, not a moving HEAD.

## Axis report (under 400 words)

**One P2 implementation finding; no separate unsolicited-scope finding.**

**[P2] Let eligible work pass expired cleanup jobs.** Ticket AC3 requires “恢复继续原责任” (issues/05-body-cleanup-and-holders.md:11), and AC5 requires “源政策收紧传播完整有限分页，不只第一页推断无holder” (:13). The new `Lifecycle.Step` always requests `Scan(...,64)` (domain/content/lifecycle.go:313), then executes `if !now.Before(record.BodySeal.Deadline) { continue }` (:335–336), leaving expired jobs unchanged. The added publication-consumer hunk likewise skips `body_cleanup` (:831–834 in domain/content/service.go). The actual delegated PG scan orders eligible jobs by `scan_at,job_id LIMIT64` (adapters/postgres/internal/pgstore/jobs.go:26), with no cursor or rotation.

Consequently, 64 earlier ready cleanup jobs whose seals expired occupy every subsequent scan. A later live cleanup duty cannot resume, and a newly accepted version's publication remains preparing indefinitely; its own finite publication deadline cannot even be processed. Reopening preserves the same obstruction. This is a static source trace, not a newly executed failure. Preserve original residual responsibilities/deadlines while providing phase-aware selection, continuation, or parking of expired execution jobs; do not fake erasure or renew their budgets.

Other inspected paths implement trusted sealing, exact original Command/Ref preservation, current metadata qualification through full ancestry, independently confirmed PG/native erasure, distinct secondary residual/ACKs, permanent cross-process key fencing, exact registered attempt pages, and locked unpublished-orphan selection. The 75 frozen `.txt` files are qualification assets for the original producer, not new production scope. Genuine stopped original media adoption is separate from copied archive policy backfill.

Final17 checks, resource audit and fresh CI remain outside this read-only review. Their pending state alone is not a source finding or seven-AC acceptance.

## Originating requirements and supporting context

Primary ticket: `.scratch/lerna-04-content-snapshots/issues/05-body-cleanup-and-holders.md`:

- :3 legal body cleanup with gone/minimal metadata, genuine offline/delete-failed copies preserve responsibility, original identity never resurrects body.
- :9 seal new uses and durably record cleanup through trusted lifecycle seam, no public delete, preserve original Command dedup/exact version/minimal disclosable gaps.
- :10 actually delete PG staging and local body, independently confirm corresponding holder erased/gone; distinguish unauthorized subjects from metadata-only authorization.
- :11 independent real second bytes; offline/failure observable pending/residual, responsible party/deadline; recovery same duty; no global erased without ACK.
- :12 exact original version closure and cross-process durable fence/tombstone under publication/retransmission/late installation; Claim/memory mutex do not substitute for file effect isolation.
- :13 new version not deleted by old cleanup; source tightening full finite pages, not first-page zero-holder inference.
- :14 original registered unpublished objects only, exact key/owner/prestate bounded closure, publication competition protects live refs and other scope.
- :15 Content, trusted holders and independent bytes for normal/failure/reopen acceptance; no WAL/backup forensic erasure, retrieval of disclosed bytes, or production multi-machine guarantee.

Whole04 spec `.scratch/lerna-04-content-snapshots/spec.md`: :29–35 exact publication and ownership, current per-action checks, all-source policy intersection, seal-before-cleanup and honest offline residual; :39–41 public boundary, real DB/object implementation and independent observations; :51 body cleanup minimal metadata/unreadable evidence/failure responsibility; :60–62 fixture-vs-real authorization and evidence-limited completion. Whole04 status remains 15/41, implementation in progress, full 1.2 advertising OFF (:75–77); ticket remains claimed (:7).

Adopted `.scratch/lerna-04-content-snapshots/decisions.md`: :41–47 orphan prestate locking, trusted cleanup/no delete, real PG/final deletion, one real local plus independent second holder sufficient, finite holder pages. :51–57 fixture authorization, fail-closed unknown and complete saving/source basis. :81–85 qualify owner boundaries, not global instantaneous effects.

Frozen machine contract decision `.scratch/lerna-04-content-snapshots/contract-shape-decision.md`: :18–26 immutable full version and declaration, original Command replay and fixed receipts, no failed/gone resurrection or cap raising; :34–38 current action gates and original finite duty; :57–71 full response Ref/current authorization and precise gone != global erased; :85–95 historical publication progress, 1.1 unavailable rather than lossy receipt bridge, only three methods and advertised=false. Actual `contract/v1_2/content.go`:120–162 still validates responses against original full Ref and exact range; `contract/v1_1/readfacts.go`:8–18,46–52 remains read-only facts with strict observation validation. `contract/schema/1.2.0/methods.json` contains only command.get/content.put/content.get, each advertised=false. Exact diff over `contract/schema/1.0.0`, `1.1.0`, `1.2.0`, `contract/v1_1`, `contract/v1_2`, and SDK TypeScript paths is empty.

Architecture: `CONTEXT.md`:51–58 original Command/Job/Claim distinctions, :77–85 exact ContentRef and authorization, :125–130 stable identity, separate cleanup and evidence; `docs/architecture/governance.md`:89–93 full source constraints and visible offline residual, :129–131 upgrade/complete holder coverage; `docs/architecture/data-model.md`:15,31 full Ref/immutable bytes/separate copies, :85 separate cleanup_complete, :91–96 Command/Job/attempt/fence responsibilities; `docs/architecture/contracts.md`:13–15 Host lifecycle vs public Component boundary, :31–33 trusted identity and exact refs, :52–61 fixed receipts/unknown distinct; ADR-0007:10–16 current access/full lineage/cleanup evidence; ADR-0006:8–16 actual checks and honest evidence limits.

## Concrete finding trace

Candidate-introduced hunk: `domain/content/lifecycle.go`:313–336:

```go
jobs, err := l.store.Scan(ctx, tx, now, 64)
// ... same returned slice ...
for _, job := range jobs {
    if job.Phase != "body_cleanup" { continue }
    record, err := l.store.LockObject(ctx, tx, string(job.Object.ID))
    // ... original identity and fresh time qualification ...
    if !now.Before(record.BodySeal.Deadline) { continue }
```

Candidate-added hunk `domain/content/service.go`:831–834:

```go
if job.Phase == "body_cleanup" {
    // Lifecycle owns the physical holder work. This consumer neither
    // claims nor completes its original cleanup responsibility.
    continue
}
```

The surrounding service acquisition remains :817 `Scan(...,64)` and :821–822 sorts only the already-returned bounded page; that sort cannot expose a later publication job.

`adapters/postgres/content/facts.go`:221–222 directly delegates Scan to Core. Core's unchanged actual SQL in `adapters/postgres/internal/pgstore/jobs.go`:26 selects `state <> 'done' AND scan_at <= $3` and `ORDER BY scan_at,job_id LIMIT $4`; it performs no update. Host migration `adapters/postgres/migrations/host/0002_claims.sql`:16–18 makes a ready/waiting job's `scan_at` equal its unchanged `due_at`. `domain/content/lifecycle.go`:225 creates ready cleanup jobs from seal start time; passing deadline does not park/complete them. Replayed Seal keeps the immutable original deadline (:173–178), so replay cannot clear this obstruction. Claims and finite Defer happen only after the skipped expired branch (:374 onward).

Reproducible source scenario: publish 64 original refs, seal each with an original short deadline, allow deadlines to expire before cleanup progresses, then accept a permitted newer version under the same owner. All 64 body jobs precede its publish job by scan_at. Every Service.Step returns only those old bodies and skips them; every Lifecycle.Step returns those old bodies and skips their expired deadline. The new publication job is never selected; a subsequent live cleanup job is also unreachable. Same facts after process/Store reopen. This scenario uses documented legal calls and finite scopes, not private row mutation. No reproduction was run under this read-only assignment.

Suggested correction is a behavior requirement rather than an unsolicited architectural redesign: keep unknown original holder/residual facts and expired deadline intact while making subsequent eligible work reachable using bounded phase selection/continuation or a parked expired execution state.

## Seven-AC coverage and remaining evidence limits

### AC1 — trusted seam / exact original history

`domain/content/lifecycle.go`:102–118 finite explicitly trusted config; :121–165 manager subject authorization, exact owner/ref/purpose/full original Subject, lock; :169–239 same-Tx immutable seal/holder duties/Job and fresh deadline. `domain/content/service.go`:125–151 original same-subject/same-digest Command returns old receipt before new admission/cap checks; :209–220 conflicting declaration or sealed new association refused; :706–800 current authorized historical receipt/progress; :974–1009 seal participates in publication policy. `domain/content/closure.go`:103–105 and :217–219 forbid sealed source/new use. PG `facts.go`:118–133 column/JSON exact consistency and :176–190 monotonic seal/gone/unchanged physical binding. No public wire delete changes.

### AC2 — PG/native body gone, minimal currently authorized metadata

`domain/content/lifecycle.go`:392–398 clears actual staging and staging-holder flag in owner Tx; :433–460 separate post-commit `ObserveStaging`, or native Fence followed by separately acquired Observe; :469–511 exact same seal/ref/current claim and independently qualified staging+primary before BodyGone. `adapters/postgres/content/lifecycle.go`:210–233 new own transaction checks actual `staging IS NULL`; :144–155 primary+staging authoritative ACK; :130–141 all holders separately calculated. `domain/content/service.go`:403–515 separate metadata fallback, exact policy Ref/actual record/full registered ancestry and each ancestor's current metadata deadline, then only gone with evidence_available=false; unknown/absent permission stays rejected. PG metadata policy checks :284–319 preserve nanosecond JSON cutoff while comparing representable microsecond SQL deadline. Terminal original publication is kept separate from availability.

### AC3 — actual independent secondary and honest residual

`domain/content/secondary.go`:39–98 complete original read/save/sync basis; :111–113 physically distinct binding; :127–154 original durable copy ID/ref/full saving Subject/purpose/attempt/deadline before outside-Tx primaryRead/secondaryPut/Read (:163–184); :186–219 final current requalification. PG `secondary.go`:14–22,55–105 exact immutable copy facts/monotonic confirmation and :107–149 bounded deterministic page. Seal incorporates original pending as well as confirmed copies (:202–224). Cleanup nil/offline port or binding mismatch leaves residual (:445–450,492–495), retains responsible/deadline and original copy/effect deadline, defers same claim (:528–535). Full ACK is not equated with primary gone (:499–525). Recovery of duties behind an expired full scan page is the finding above.

Static test support `content_secondary_test.go`:49–59 different file inodes and actual secondary bytes; :61–69 real holder Close/nil active port; :85–121 original duty and primary gone/global pending; :122–159 both Worlds reopen, original deadline unchanged, exact independent secondary absence. `content_secondary_native_failure_test.go` is the actual ENOTEMPTY owned-object case identified by the evidence/API handoff; its scope is native local failure, not remote provider.

### AC4 — irreversible exact closure, late effects and cross-process fence

`adapters/objectstore/local/store_linux.go`:94–119 binds opened original directory FD dev/inode; :202–232 sticky unknown body Close keeps lock descriptor ownership; :253–263 actual Put takes permanent key flock and checks marker; :299–303 checks before final link; :335–348 Read takes shared key lock and respects closure. `erasure_linux.go`:22–77 regular original permanent `.lock` inode + fsync + nonblocking finite flock, actual body Close before release; :79–89 both sealed/pending marker stop new effects; :117–171 durable exact marker install, incomplete marker remains failclosed; :174–224 eraser exact binding/final and explicitly registered attempts under same effect lock; :227–314 independently reacquired observation, marker file+dir sync and actual directory body-name checks, unknown temps residual. No Claim or in-memory mutex substitute.

`domain/content/service.go`:959 exact late successful Put cannot set ObjectHolder=true after BodyGone; :984–985 final gate refuses sealed original and preserves failed history/accepted receipt. Static process tests `erasure_process_test.go`:249–315 actual temp Sync/SIGSTOP, erasure times out on held flock, actual original Put/Close+Wait then erasure/reopen; :318–401 seal-first/late Put refusal, original eraser SIGKILL, own independent reopen observation without manufacturing killed-holder logical Close. Scope is local Linux upgraded protocol; old non-fencing writers must truly stop.

### AC5 — exact version isolation / all policy, holder and attempt pages

VersionIdentity is exact owner/contentID/version; physical deletes match full original Ref/ObjectKey/Binding. `domain/content/policy_cleanup.go`:15–28 reads bounded original page and validates original key; :47–96 original saving basis/ref/current policies and locked original record; :118–144 complete structural ancestry + all source current policy reads; :145–190 definite immutable cap or exact Save=false, restored complete saving basis only then NotRequired; :194–225 page-wide writes/CAS with post-wait original cutoff; :234–252 AdmissionTarget exact shape/key/budget; :255–261 malformed cap cannot authorize destructive cleanup. PG `policy_cleanup.go`:55–118 whole expected original responsibility/seal identity; :151–195 NotRequired whole-body CAS; `management.go`:328–386 normal registration cannot originate erased and cannot wash existing erased history. `Lifecycle.Step`:341–363 paginates all physical holders; :400–404 and :485–491 original registered attempt pages persist cursor, only last page plus exact independently absent physical facts can ACK. PG `PublicationAttempts`:158–207 qualified Ref/key/binding pages; no unknown temp adoption. Queue starvation above remains distinct from these correctly paginated collections.

Static `content_cleanup_pages_test.go`:55–72 four same-basis refs, :73–123 other full delegated saving basis independently protected, :138–163 actual copy, :174–223 propagation and independent responsibility pages with reopen; test has only five original responsibilities, so does not establish progress after a full 64-job obstruction. `content_cleanup_attempt_pages_test.go`:60–82 three real Put arguments and two confirmed-success mechanical reply losses; :117–132 persisted page2 cursor/reopen; :145–169 unknown setup-owned exact inode/bytes preserved, actual registered bodies absent; :171–194 independent setup owner removes its own unknown effect, same original duty restores allACK/V2/receipt. Original unknown owner is not guessed or adopted.

### AC6 — bounded registered original orphan only

`domain/content/orphan.go`:11–14 delegates same seal logic with conditional orphan-only flag. `domain/content/lifecycle.go`:140–161 exact originally locked saving basis, published returns ErrOrphanReferenced, only preparing/failed allowed, no external object scan. Same version lock is used by publication finalization `service.go`:908–969. Native fence closes future installation; late finalization cannot publish or resurrect current holder. Published-first normal/reject/reopen controls and orphan-first actual nativePut-before-Finish with V2/receipt are recorded in evidence :265–293; current source does not treat arbitrary physical temp names as orphan deletion authority.

### AC7 — observations, genuine upgrade, and limits

Recorded evidence remains partial individual qualifiers, not seven-AC acceptance. `ticket-05-evidence.md`:1 and ticket Status claimed deliberately make no acceptance claim; :997–1014 static old oracle updates say affected execution/final suites are pending. `ticket-05-api-handoff.md`:67–78 accurately distinguishes primary gone/all-holder ACK and local Linux/unknown Close; :171–188 separates genuine stopped original scope, provenance and complete actual attempts from imported archives; :270–276 preserve archive bytes/history while refusing policy-backfill adoption.

Genuine stopped-original support: `conformance/internal/contentfixture/stopped_legacy.go`:121–124 new original scope, current migration only after original logical Close/actual Wait; :159 prestart duty; :211 identity before released effect gate; :222–235 accurate full actual Put parameters; :245–259 actual Wait/group absence plus all original Objects/Store/witness/gate Close ACKs and actual FDs; :269–290 original media dev/inode and fixed digest qualification. `domain/content/legacy_binding.go`:17–38 trusted finite immutable original qualification; :77–145 fixed producer SHA/binding/scope/whole record/published/complete explicit attempts; :191–204 actual original byte hash/length; :205–260 whole-record recheck/CAS/attempt writes/fresh cutoff. PG `legacy_binding.go`:14–76 dedicated empty→original immutable binding/provenance CAS; ordinary SaveVersion cannot replace/adopt provenance. `content_legacy_test.go`:75–84,130–143 specifically refuses the copied archive even after complete policy backfill; remaining physical bytes and original receipt/publication are retained. Original frozen `.txt` assets are inert package closure verified before supervised producer compilation, not silently installed new product code.

Evidence read: candidate `ticket-05-evidence.md` sections :7–41 fence, :77–108 PG/gone, :153–208 independent secondary, :209–263 process order and retained unknown killed root, :299–390 current original saving basis, :391–470 terminal holder/erased history, :472–535 real ACK reply loss and complete metadata scope, :536–645 accepted cap/all pages/unknown attempts, :646–705 genuinely stopped producer, :706–874 deadline/CAS/confirmed commit-return reply loss, :903–995 AdmissionTarget cap/fresh time/unknown structure+policy errors. The evidence qualifiers explicitly avoid PG commit_unknown claims from confirmed-commit mechanical reply loss, global forensic deletion, old unknown historical-scope adoption, or production multi-machine guarantees.

Original logs read to cross-check bounded claims only: `/tmp/lerna-04-ticket05-execution/cleanup-pages-first-run.log`, `legacy-stopped-first-green-compile-repair.log`, `inherited-cap-unknown-gates-race.log`, and `crossprocess-seal-first-kill-qualification.log`: recorded successful package/completion ACKs; only the verbose inherited-cap log independently lists its named RUN/PASS. No log was elevated to execution of an unlisted case or final-suite acceptance.

## Captured commit list

Command: `git log 1373112472761d32c22ed6d7603d806d8ef9a3c7..1a4e1d2d704261a59491dfc6e0c5d26b80ec5107 --oneline`.

```
1a4e1d2 test(content): observe exact bodies beside permanent key locks
4735abc test(content): refuse unknown inherited cleanup qualification
c01b4c3 test(content): qualify original admission cleanup lock deadline
301be6d fix(content): qualify original inherited cap admission cleanup
1a52d24 Qualify whole original cleanup page rollback under real CAS contention
331154b Qualify initial legacy cutoff rollback after actual attempt writes
ca9ac81 Qualify legacy confirmed commit reply loss and exact recovery
2402fd7 Qualify stale legacy holder binding rejection and original replay
35df18e Qualify original cleanup deadline across real policy lock waits
bb6e1f2 Bind stopped original legacy Content holders with exact scope evidence
864399c test(content): qualify original attempt pages and unknown residual ownership
1af6593 test(content): resume all original policy and physical holder pages
d849b7a fix(content): consume definite expired accepted retention caps
e5b38a5 test(content): qualify current metadata across derived ancestry
2a28ca8 fix(content): preserve unconfirmed erasure reply and metadata time precision
ff33169 content: preserve terminal erasure history during natural policy registration
0586d51 content: preserve erased holder fact after late successful publication I/O
51e331e content: stop new primary-holder claims after authoritative erasure
641020d content: bind current policy seal to original all-holder cleanup acknowledgement
c2716c0 content: qualify restored saving basis before cancelling old cleanup trigger
3aa948d content: select unpublished orphan under the original version lock
e84c2cd test(content): qualify late process rejection without washing killed holder ownership
815f24e test(content): qualify real cross-process put before key erasure
05b191f test(content): qualify native secondary deletion failure and original duty
25f3f35 content: retain independent secondary copy duties through body cleanup
2b73be2 test(content): qualify original root before first body seal
70d746e content: bind original publication duties to opened physical holder
9bcaeba content: confirm primary erasure before metadata-only gone
8986829 content: persist trusted body seals and original holder duties
1f52ccd content: fence exact object keys across holder reopen
```
