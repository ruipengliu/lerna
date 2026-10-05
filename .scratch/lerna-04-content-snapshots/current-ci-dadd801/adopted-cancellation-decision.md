# dadd801 CI: packaging and cancellation decision — STATIC

Source: `dadd801cfd11ca038f0496872941097f10ebe507`; run `37274041977` genuinely failed. The fetched logs establish two separate failures. A `gh` 403 is not an external blocker, and this report does not qualify a rerun.

## 1. Preserve evidence; correct both archival suffixes

The contracts log names **two**, not one, unformatted evidence files below `.scratch/lerna-04-content-snapshots/ticket-05-expired-job-scan/`:

- `current-resource-audit/audit.go`, SHA-256 `748ba8db54a22ec1e8583ccf74ff48004c40ff3c0bf5a9a94c68d7fa58b9bf94`.
- `manager-full64-qualification/source-preformat-content_manager_expired_backlog_test.go`, SHA-256 `2b36441f2245057e3cffdc5365d61891eb8f358a9ef6b29a22c0f5d50edb044f`.

Rename these archived copies to `.go.txt`, byte-for-byte, and add a current packaging note mapping original outside execution/source paths and hashes to archive destinations. Correct current archive links/manifests where necessary; preserve historical raw logs and original manifests as evidence of their actual paths. Neither gofmt the originals nor globally exclude `.scratch/*.go`. `scripts/check-go-format.mjs` correctly includes tracked and eligible untracked Go sources; it need not change. Confirm byte equality and normal repository formatting checks after packaging.

## 2. Proven lifecycle defect, limited causal attribution

`pool_test.go:871–941` saturates ordinary work, observes correct control/reconciliation projections, then explicitly cancels Run and awaits it. CI reached line931, returning `driver: bad connection`; recovery failed in25.727s, this test in0.14s. Later integration race/component commands did not execute. Container SQL errors include intentional negative tests and cannot identify this failure’s SQL or prove its driver cause.

`host/durablework.PoolWorker` aliases `internal/durableworkdemo.PoolWorker`. Its Run starts three loops, returns only the first channel error, cancels its child context and joins goroutines, but discards the other two errors and the original caller context status. `StepLane`/`NextWake` propagate PG transaction errors; PG `Within` can return Begin/setup/callback errors directly and preserves commit uncertainty separately. A cancellation-associated driver error may thus win the channel race. The exact CI statement remains unknown.

**Fix the Run ownership boundary**, not PG-wide error rewriting: retain the caller context separately; collect the first lane outcome, cancel the child, wait for all three goroutines, then collect all remaining bounded outcomes and return their joined errors plus the caller’s actual `Err()` if nonnil. Label lane errors and caller cancellation separately. Do not add the internally canceled child’s `Err()` as fabricated caller cancellation, replace errors with context.Canceled, discard driver/commit-unknown causes, retry, or change limits. Sibling cancellation errors are legitimate lane outcomes; `errors.Is(Canceled)` alone is not proof that cancellation caused every accompanying failure.

## 3. Deterministic red and real controls

Before changing Run, use its existing Timer/Runner seams with finite barriers: all three loops register; cancel the real caller; release explicitly injected non-context lane errors, including `driver.ErrBadConn`. Require Run’s returned error to retain caller cancellation and every distinct lane cause. Current first-error-only Run must fail this assertion. Label injected diagnostics mechanical, not a reproduced native driver failure. A caller-live first-fault control must retain its primary cause after internally stopping siblings.

Then run actual PG reserved-progress and bounded blocked-SQL cancellation controls, retaining original contexts/leases/quotas and public projection/ObservePool assertions; accept whichever actual raw cancellation error PG produces, without manufacturing ErrBadConn. Verify finite join, original ordinary reservation and later completion, plus the SQLite consumer and relevant Run/wake normal/race tests. No timer sleeps to force luck, widened budgets, or raw `ctx.Err()!=nil` suppression of arbitrary errors. Cleanup confirmation remains independent from Run’s error classification.

03 BodySeal qualification and06 remain independent. I performed no Go/native/PG execution or product edits.

Logs SHA-256: contracts `3424e63f21fc6d84ee85475e14fb7963109876382fe3a2daa211280ec97d2e5f`; durable `d6e73fd698cc7bbe13def0b9569c1045831b6f4ad1681db7b08c940e87bfb091`.
