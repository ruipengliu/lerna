# 01 exit evidence

Ticket01's nine AC are complete. Final reviewed source/test pin is **696ac49846105a16f33e5de86dc621a3858651b2** on `codex/deterministic-harness-ticket-01`; initial clean base d3f5bc4f93c82d84ed3f775547a6e3b1528c82ed. Latest integration9a06bc1d895e354d29524f6b33622410c719bcc8 was merged at clean2e77f4f. Source range9a06bc1..696 comprises46commits/187paths. Final documentation changes do not change this tested source tree. Root owns merge/push/CI and later whole03 exit; no new remote CI result is claimed here.

Read the [API handoff](ticket-01-api-handoff.md), [Standards review](ticket-01-standards-review.md), [independent Spec review](ticket-01-spec-review.md), [adopted decisions](decisions.md) and [ticket/AC mapping](issues/01-durable-rule-proposal.md). Root FULL-read and adopted both696final reports: Standards0open/0new; Spec a/b/c each0. Preview's five P2s and final historical oneP1/threeP2, including samefinding2 PG/SQLite/pipe residuals and all cause-reporting outlets, are closed. Reviews were read-only and supply no execution evidence by themselves.

## Accurate execution pins

| Pin | Actual necessary verification | Result |
| --- | --- | --- |
| 41cbf7c9af2b9027ca6e81ddccf2ab06836e3810 | `make check` | exit0: strict1.1 generation/refusal/digests/cache/advertisement,81new+158old typed Go↔TS bytes both orders, TS37, JS supervision20, format/vet/build |
| f96f85987818a5e8dc5c0c104e5731ddf75687df | `go test -race -count=1 -timeout=120s ./...` | exit0; component11.207/target2.557/Core1.100/SQLite1.062/Source1.168/demo1.270/pool1.043s |
| f96f859 | `go test -p=1 -tags=integration -count=1 -timeout=120s ./conformance/component ./conformance/internal/decisionfixture` | exit0;25.137/8.208s |
| f96f859 | same packages/flags with `-race` | exit0;42.337/11.485s |
| f96f859 | `go test -tags=integration -count=1 -timeout=120s ./conformance/recovery/...` | actual original PG/SQLite exit0;27.357s |
| f96f859 | same recovery scope/flags with `-race` | exit0;60.405s |
| b43ceba88eafba323abb7ee5eb95302e88e8c971 | `go test -p=1 [-race] -tags=integration -count=1 -timeout=120s ./conformance/internal/decisionfixture -run '^TestFrozenLegacyWriterUpgrade$'` | actual five-state oldwriter/upgrade normal5.760/race9.080s, both0 |
| 696ac49846105a16f33e5de86dc621a3858651b2 | three real prestart-pipe tests with the selection below, normal/race, count1/120s |0.010/1.058s, both0; tagged packagevet0/diffcheck0 |

The three pipe tests use `-run '^TestUpgrade(Pipe|Unstarted|Prestart)'`.

All checks ran sequentially in root's exclusive slot, with original finite deadlines and count1. No skip, missing dependency bypass, memory SQLite, increased timeout or competing database suite. After41cb only relevant Go lifecycle/fixture paths changed: tagged all-package vet/build, mechanical normal/race, final base-race and both actual new/old databases coveredf96. b43 is only historical fixture/prestart helper;696 is test registration only, each actual affected path reverified. Root explicitly avoided repeating unrelated JS/generated checks or unrelated wide DB suites; earlier results retain their accurate pins.

`go mod verify`: all modules verified. Old four manifests: all27entries verified. Frozen historical closure:71entries covering69payload plus provenance/README verified. Original1.0 schema/Go/TS/generated/fixtures and host0001–0005 have zero initialbase diff. Actual970fd90 comparisons of `adapters/postgres/decision_engine/migrations/0001_decisions.sql` and `conformance/internal/decisionfixture/migrations/0001_fixture.sql` are zero. A prior wrapper script accidentally named nonexistent0001_sources; the actual fixture migration was subsequently explicitly checked, not inferred. No generated code was hand-edited.

## Behavioral evidence

Complete normal vertical drives strict1.1 decide through current exact auth, original identities, same-owner accepted+Decision+realFKJob shortTx, actual close/reopen both owners, FIFO Claim/current Start gates, actual bounded source bytes, complete prepared exact references, independent publisher Tx/normal publications/readback, gated Finish, completed get and unchanged original accepted command.get. Separate identity tests exercise concurrent original Command, new Command sameInput association/noJob, samekey differentdigest conflict, Decision mismatch, tenant/owner/delegated auth, inactive/differentStore tokens and rollback.

All-member pool tests cover nonanchor earlier current deadline, quota0 maintenance, live lease and future due, combined tenant quota/two owners, lastqueue-slot concurrent connection admission and positive wake. Source tests exercise original bytes after reopen, strict current purpose/principal/ref/expiry and input bounds. Missing Decision returns closed result_unavailable; actual closed DB returns unavailable/dependency_unavailable. New receipt is typed1.1; old query only exposes lossless facts.

Adopted accounting's eight basic groups are real PG assertions: normal onefee/output; Startwrite-beforecommit rollback and commit-beforecompute unknown window; computed-beforeprepared interruption/cumulative allowance; prepared-beforepub and preparedCOMMIT reply loss; artifactreplyloss/bothpublished staleFinish/FinishCOMMIT replyloss with originalrefs/bytes/reopen; onepermission per sameepoch/concurrentStart and staleepoch fencing; zeroSteps/cost/input and Snapshot/lock/manifest/wholeartifact+Proposal bounds; prepared originaldeadline and immutablepermission expiry with still-current accepted receipt query. Prepared retry does not recompute/recharge or refresh original grant. These are not a substitute for future fullSIGKILL06.

Frozen68real970fd90 production files match bytes/provenance plus one original driver:69payload430396bytes. Normal acceptance builds committed source, not history Git objects. Old accepted, Start-beforecompute running, completed, actual cost-limit failed and two durable publications-beforeFinish running are generated by old public writer, with both oldwriters Close nil/drain before ready. Owner0002 only classifies; current Maintain closes old active work with approved accurate reasons, retains original receipts/input/refs/results/recorded usage lower bounds. New fixture-rule/2 normal counterpart succeeds. No fabricated oldcancel/waiting producer: that source did not implement those behaviors. No /1→/2 manifest or fee retcon.

## Red/green and failures retained

Initial fulltracer471dfe0 failed missing component; real source strictRevision red0.192→0.966green, normal full reopen0.822green. Prepared waiting originally used undefinedpublication_unavailable: red2.896→defineddependency_unavailable green1.020. ConcurrentStart/unknown cumulative usage1.204green; actualclosedread/staleFinish1.865green. Authorityalias/config oldkey red0.782→1.101green; delegated fixtureidentity accidentally reused:0.419conflict→fresh exact Decision0.882green, all input-bearing purposes includingStart1.173green. Old new-command query Target initially oldkey:8.811/9.806red→11.201green. Prepared plannedref incorrectly expected readable/unavailable:2.308red→correct unpublished-forbidden expectation7.202green. AdditionalScenario query still oldkey caused sourcecaps4.126red→accuratebuilder1.233green. Prepared truepermission expiry initially expected Goerror instead of actual closed rejected/forbidden3.293red→3.249green. RealStartrollback/prepared+FinishCOMMIT replyloss0.994green.

Synchronous build draft missed descendant pipe risk; replaced async deadline process-group kill before inheritedstdio wait, bounded output/closure/group absence. Real finite descendant heldpipes verified under independent Linux subreaper/watchdog;20infra tests4.957green. Failed or unconfirmed scope is retained, original errors/Close causes joined. Infrastructure tests do not mirror business state/callcount.

Mechanical actual database/sql.OpenDB driver with **no physicalscope** reproduced Core/Source firstClose error erased/concurrent earlyreturn and SQLite release after unknown: red0.010/0.014/0.007→basicgreen0.072/0.064/0.012s. Expanded matrix initially compile-failed missing test-onlyCloseDone; corrected normal0.061/0.075/0.008 and race1.105/1.162/1.056. Normal/repeat, cancellation, PingFail-CloseSuccess, joined dualcause/partialholder and releasefailure tested. Concurrent joins are locally bounded. These do not claim nativepgx/sqlite3 close faults. Taggedvet exposed inherited second-context cancel closure warning; explicit defercancel fixed it without behavior change.

Prestart pipe allocation refusal is real exec.Cmd.StdoutPipe refusal after actualstdin OS pair (configured stdout, no FD exhaustion): red0.011→three tests0.014/race1.047green. Exact four ends/firstclose cause retained; successful Start/Wait still passes actualoldwriter5.760/9.080. Test-only696 closes the new tests' error-before-cleanup residual, normal0.010/race1.058. True already-closed endpoint is named/logged as such, not a simulated native fault. Source/driver/resource cleanup histories remain separate from business success.

One exec-server transport disconnect rejected before processcreate: no tests/log/session started and no result claimed. Initial only-read psql audit failed because fullURI in PGDATABASE env was not expanded; no mutation; corrected safe URI→individual libpq env fields, audit0. No DSN/config/driver secrets printed.

## Environment and exact cleanup

Linux/CGO, Go1.27.1, Node24.19.0, pnpm12.8.1, actualPG18.6 and psql17.11. Dedicated database `lerna_durable_work_01_20261003`. DSN is read programmatically from mode600 `/tmp/lerna-work-01-dsn`, injected only via environment. Actual durable SQLite uses independently created overlay TMPDIR under /workspace, never /tmp tmpfs. Successful exact CREATE/Mkdir acknowledgements are immediately recorded/fsynced in `/workspace/lerna-03-ticket-01-owned-scopes.jsonl`; all registered generations/children actualexit/Wait/Close before ownercleanup.

Final latest ledger:1074ack lines,818unique PG schemas and256unique FS scopes, **all absent**. Sole empty registered overlay root removed by exactrmdir after every session exited. Query used only exact original ledger names; no namespaceprefix/time/row inference, unknown oldscopes/prototype names or ambiguous CREATE cleanup. No new PG deletion needed during finalaudit. Mechanicalunknown tests own no physicalscope. All sessions are0 and testing slot released.

Working local artifacts preserve full logs/failedhistory: `/tmp/lerna-03-ticket-01-evidence.md`, `/tmp/lerna-03-ticket-01-cleanup-audit.json`, `final-review-input.md`, `final-commits.txt`, `final-paths.txt`, five `lerna-03-final-*-f96f859.log` files, two `prestart-upgrade-*-b43ceba.log`, two `prestart-testguards-*.log` and three independent helper evidence files. Tracked evidence/reviews/handoff above remain available after workspace cleanup.

Only ticket01 is resolved. Later candidate02, cancel03 and fullprocess06 behavior, productionprovider/Task/Content/installation and complete profile/whole03 exit remain unclaimed by this implementation.
