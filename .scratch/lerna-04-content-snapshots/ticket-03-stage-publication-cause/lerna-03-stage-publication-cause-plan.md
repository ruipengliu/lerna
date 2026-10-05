# 03 StagePublication: actual cause-preservation tracer plan

STATIC only. Product/Go sources remain clean28e3a468c86d4323d3b497836de23bb311a58952; no test/source mutation or native/DB was executed. This plan adopts only the Standards P2 finding, not another axis's judgement. CPU profiling must compile/run this original product before a cause fix changes it. Root owns LOCAL grants.

## Concrete seam and one tracer

Test name: TestContentContextStagePublicationPreservesCanceledInputLockCause, in conformance/component/content_context_publication_cause_test.go (integration). Drive actual Adapter.Publish, injected fixture dispatcher Access.StagePublication, real PG FOR UPDATE wait, and public Content Command.get/ReadPublished plus actual three-owner Reopen. This is one normal+cancel tracer, not a helper error mirror or fabricated SQL/row-state failure.

A fresh original30s ContextWorld performs actual Compile/Bind. Original Input/deadline/budget/Request, fixture Tx3s/statement2s/lock1s remain untouched. Actual Adapter.Publish of literal `stage cause normal\n` with sources consisting of the actual mandatory ref returns an accurate publication; actual ReadPublished bytes must equal this independent literal. Repeat public read after three-owner Reopen keeps exact ref/hash/body and original permission. This normal is real publication outside a Decision worker and is labelled as such; it does not claim rule-charge/worker capacity qualification.

Fault uses another fresh same-config world and its actual Compile/Bind. Open two actual owned fixture peers with existing OpenCompileBudgetPeer: one real publisher connection and one row-lock holder; this reuse only acquires a known single-connection Store with explicit native/backend identity and Close ledger, not a compile-budget grant. Construct Adapter.New with existing Config Owner/Subject/Purpose/Content and a narrow stage-entry Access decorator embedding the publisher peer's Dispatcher. All original Binding/Current/PrepareContent methods forward unchanged. Only StagePublication signals arrival through buffered channel, waits for a finite caller-bounded forward gate, and then calls the actual peer Dispatcher.StagePublication with exact received ctx/Input/request, once. It does not inject an error, alter request, or touch SQL rows.

Run actual Adapter.Publish in an owned BorrowWorkerExit goroutine under context.WithCancel(original30s ctx). Its two actual Current calls complete before the decorator gate. Main waits for this real StagePublication arrival, starts a registered locker goroutine on the second peer, waits for positive original input-row lock acknowledgement, releases the forward gate, confirms the actual publisher backend waits on that locker, then cancels ONLY the publisher context. Join actual result and actor-exit before releasing/joining locker and positively Close both peers. No arbitrary sleep, deadline renewal or source guard change is used. On early failure cleanup cancels the actor and opens every gate; absent positive joins/Close retains the original owned scope.

Assert actual Publish result preserves errors.Is(err, context.Canceled), and does not classify as engine.ErrForbidden. Current candidate returns ErrForbidden at StagePublication's input Scan, so this should be the actual first red; no error-preservation source fix precedes it. Capture the exact actual ContentPutRequest at the Access seam solely to construct public original Command.get; after the completed actor and after Reopen, the current authorized command reader must return NotFound. Do not query fixture_context_publications or Content private tables as business oracle, infer absence from Forbidden, or claim unbounded future absence. StagePublication error happens before Adapter PrepareContent/Content.Put; whether that tail was reached is qualified by actual original Command.get and the closed actor window, not by private method counts.

## Minimal trusted PG synchronization additions

A fixture test-support file context_publication_probe.go supplies two public trusted infrastructure methods; they never mutate a business row or grant permission.

- HoldContextInputRow(ctx, inputID, held, release): uses the existing Store.within/unchanged trusted clock, locks the actual current `fixture_context_inputs` row with `SELECT input_id FROM ... WHERE input_id=$1 FOR UPDATE`; obtains real `pg_backend_pid()`/clock while holding it; sends a positive backend+inputID observation; waits on release or the same finite ctx, then exits the original short Tx. Cannot turn a row error into an acknowledgement.
- WaitContextInputRowBlocked(ctx, contenderBackendPID, heldBackendPID): bounded by the existing cfg.TransactionTimeout and 5ms timer like existing WaitCompileBudgetLock; reads only native lock machinery: require known blocker in pg_blocking_pids(contender), plus a not-granted pg_locks record on contender. This is an actual PG synchronization observation, not a private business-row correctness oracle. Backend identities come from actual pre-registered opened peers; no guessed process or scope.

The helper is not allowed to disable lock/statement timeout, rewrite data, expose DSN, or produce an error stub. Its lifetime uses existing peer actual native Close ledger and owner cleanup protections. Missing/failed Open outcomes preserve unknown.

## Pin and native sequence

While profiling is pending, do not modify tracked source. If the test/support files are prepared first, place them in a NEW owned temporary overlay, mapping only the two originally nonexistent filenames; record exact original28e product and differing test/support hashes. That binary is original product with new test instrumentation, not an assertion that tracked28e contains the tracer. Format, source provenance, compile and first-red commands require separate root grant and sole LOCAL.

Planned first actual red under original owned wrapper:
`GOTOOLCHAIN=local GOFLAGS=-mod=readonly go test -v -p 1 -count=1 -tags=integration -timeout=120s -overlay=NEW_TEST_OVERLAY/overlay.json ./conformance/component -run '^TestContentContextStagePublicationPreservesCanceledInputLockCause$'`
Each world caller30, original Tx3/statement2/lock1, outer120. Record native PID/PGID/tick, real completion/group absence, normal control and actual cancellation observation. Stop on compile/setup failure and classify correctly; do not call it cause-preservation red or blindly retry. No race invocation before real normal tracer outcome.

Only after real first-red root review, minimal source fix at ContextDispatcher.StagePublication's input-row Scan:
`if errors.Is(err, sql.ErrNoRows) { return engine.ErrForbidden }; if err != nil { return err }`
No new broad wrapper, transaction/current checks/SQL order changes. This preserves exact missing-input denial while retaining actual SQL/context cause. Independent normal/race tracer and the currently relevant exact publication/Prepared recovery tests then require root grants; original complete B capacity remains separately pending and is not repaired by this cause change.
