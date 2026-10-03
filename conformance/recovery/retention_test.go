//go:build integration

package recovery_test

import (
	"context"
	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/contract"
	"github.com/ruipengliu/lerna/host/durablework"
	demo "github.com/ruipengliu/lerna/internal/durableworkdemo"
	"github.com/ruipengliu/lerna/runtime"
	"testing"
	"time"
)

func TestPGRetention(t *testing.T)     { retentionBehavior(t, database(t)) }
func TestSQLiteRetention(t *testing.T) { retentionBehavior(t, sqliteDatabase(t)) }
func retentionBehavior(t *testing.T, store workStore) {
	ctx := contextFor(t)
	h := hostFor(store, owner, principal)
	h.Permissions = durablework.NewPermissions([]durablework.Permission{{Subject: principal, Owner: owner, Record: true, Read: true, Cleanup: true}})
	h.Retention = store.(demo.RetentionRepository)
	raw := command("original", "input", "hello", nil, future())
	out, err := h.Record(ctx, raw, &principal)
	original := assertReceived(t, out, err)
	worker := conformanceWorker(t, owner, store, store, store, h.Clock)
	batch, err := worker.Claim(ctx, "worker", 1, time.Minute)
	if err != nil || len(batch) != 1 {
		t.Fatalf("claim: %+v %v", batch, err)
	}
	startWork(t, worker, batch[0])
	if err = worker.Complete(ctx, batch[0].Claim, durablework.Project(batch[0])); err != nil {
		t.Fatal(err)
	}
	ref := contract.CommandRef{Owner: owner, CommandID: "original"}
	result, err := h.Cleanup(ctx, ref, contract.Revision("1"), &principal)
	if err != nil || result != demo.Cleaned {
		t.Fatalf("cleanup: %s %v", result, err)
	}
	got, err := h.Observe(ctx, "input", &principal)
	if err != nil || !got.Input.BodyGone || got.Input.Text != "" || got.Input.StoredTextBytes != 0 || got.Input.Revision != 1 || got.Job.ID != batch[0].Claim.JobID || got.Job.State != "done" || got.Projection.TextDigest != "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Fatalf("retained observation: %+v %v", got, err)
	}
	queried, err := contract.GetCommand(ctx, readWire(ref), &principal, h.Permissions, h, time.Now)
	if _, ok := queried.AsGone(); err != nil || !ok {
		t.Fatalf("gone: %+v %v", queried, err)
	}
	out, err = h.Record(ctx, raw, &principal)
	assertReceiptSame(t, original, assertReceived(t, out, err))
}

func retentionHost(store workStore) *durablework.Host {
	h := hostFor(store, owner, principal)
	h.Permissions = durablework.NewPermissions([]durablework.Permission{{Subject: principal, Owner: owner, Record: true, Read: true, Cleanup: true}})
	return h
}
func retentionComplete(t *testing.T, store workStore, h *durablework.Host) durablework.Work {
	t.Helper()
	ctx := contextFor(t)
	worker := conformanceWorker(t, owner, store, store, store, h.Clock)
	batch, err := worker.Claim(ctx, "normal", 1, time.Minute)
	if err != nil || len(batch) != 1 {
		t.Fatalf("normal claim: %+v %v", batch, err)
	}
	startWork(t, worker, batch[0])
	if err = worker.Complete(ctx, batch[0].Claim, durablework.Project(batch[0])); err != nil {
		t.Fatal(err)
	}
	return batch[0]
}
func retentionQuery(t *testing.T, h *durablework.Host, id contract.ID, gone bool) contract.CommandReceipt {
	t.Helper()
	ref := contract.CommandRef{Owner: owner, CommandID: id}
	got, err := contract.GetCommand(contextFor(t), readWire(ref), &principal, h.Permissions, h, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if gone {
		if value, ok := got.AsGone(); !ok || value.CommandRef != ref {
			t.Fatalf("gone exact reference: %+v", got)
		}
		return contract.CommandReceipt{}
	}
	found, ok := got.AsFound()
	if !ok {
		t.Fatalf("found: %+v", got)
	}
	return found.Receipt
}
func TestPGRetentionNewRevisionAndReopen(t *testing.T) { retentionNewRevision(t, database(t)) }
func TestSQLiteRetentionNewRevisionAndReopen(t *testing.T) {
	retentionNewRevision(t, sqliteDatabase(t))
}
func retentionNewRevision(t *testing.T, store workStore) {
	ctx := contextFor(t)
	h := retentionHost(store)
	raw := command("old", "input", "hello", nil, future())
	out, err := h.Record(ctx, raw, &principal)
	original := assertReceived(t, out, err)
	work := retentionComplete(t, store, h)
	ref := contract.CommandRef{Owner: owner, CommandID: "old"}
	result, err := h.Cleanup(ctx, ref, "1", &principal)
	if err != nil || result != demo.Cleaned {
		t.Fatalf("clean: %s %v", result, err)
	}
	store = retentionReopen(t, store)
	h = retentionHost(store)
	retentionQuery(t, h, "old", true)
	h.Clock = &workClock{now: time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC)}
	out, err = h.Record(ctx, raw, &principal)
	assertReceiptSame(t, original, assertReceived(t, out, err))
	_, err = h.Record(ctx, command("old", "input", "changed", nil, future()), &principal)
	assertReason(t, err, "idempotency_conflict")
	_, err = h.Record(ctx, command("old", "input", "hello", nil, time.Now().Add(2*time.Hour).UTC().Format("2006-01-02T15:04:05.000000Z")), &principal)
	assertReason(t, err, "idempotency_conflict")
	changedRevision := contract.Revision("1")
	_, err = h.Record(ctx, command("old", "input", "hello", &changedRevision, future()), &principal)
	assertReason(t, err, "idempotency_conflict")
	bob := principal
	bob.SubjectID = "bob"
	h.Permissions = durablework.NewPermissions([]durablework.Permission{{Subject: principal, Owner: owner, Record: true, Read: true, Cleanup: true}, {Subject: bob, Owner: owner, Record: true, Read: true}})
	_, err = h.Record(ctx, raw, &bob)
	assertReason(t, err, "idempotency_conflict")
	_, err = h.Cleanup(ctx, ref, "1", &bob)
	assertReason(t, err, "forbidden")
	h.Clock = store
	revision := contract.Revision("1")
	out, err = h.Record(ctx, command("new", "input", "new body", &revision, future()), &principal)
	next := assertReceived(t, out, err)
	result, err = h.Cleanup(ctx, ref, "1", &principal)
	if err != nil || result != demo.AlreadyGone {
		t.Fatalf("old clean: %s %v", result, err)
	}
	out, err = h.Record(ctx, raw, &principal)
	assertReceiptSame(t, original, assertReceived(t, out, err))
	got, err := h.Observe(ctx, "input", &principal)
	if err != nil || got.Input.BodyGone || got.Input.Text != "new body" || got.Input.StoredTextBytes != 8 || got.Input.Revision != 2 || got.Job.ID != work.Claim.JobID || got.Job.WorkRevision != 2 || got.Job.CompletedRevision != 1 || got.Job.State != "ready" {
		t.Fatalf("new preserved: %+v %v", got, err)
	}
	retentionQuery(t, h, "old", true)
	assertReceiptSame(t, next, retentionQuery(t, h, "new", false))
	nextWork := retentionComplete(t, store, h)
	if nextWork.Claim.JobID != work.Claim.JobID || nextWork.Input.Text != "new body" || nextWork.Claim.ClaimedRevision != 2 {
		t.Fatalf("new responsibility: %+v", nextWork)
	}
	store = retentionReopen(t, store)
	h = retentionHost(store)
	retentionQuery(t, h, "old", true)
	assertReceiptSame(t, next, retentionQuery(t, h, "new", false))
	got, err = h.Observe(ctx, "input", &principal)
	if err != nil || got.Job.State != "done" || got.Input.Text != "new body" || got.Projection.InputRevision != 2 {
		t.Fatalf("new after restart: %+v %v", got, err)
	}
}
func TestPGRetentionEmptyPendingAndAuthority(t *testing.T) { retentionEligibility(t, database(t)) }
func TestSQLiteRetentionEmptyPendingAndAuthority(t *testing.T) {
	retentionEligibility(t, sqliteDatabase(t))
}
func retentionEligibility(t *testing.T, store workStore) {
	ctx := contextFor(t)
	h := retentionHost(store)
	clock := &workClock{now: time.Now().UTC().Truncate(time.Microsecond)}
	h.Clock = clock
	out, err := h.Record(ctx, command("empty", "input", "", nil, future()), &principal)
	original := assertReceived(t, out, err)
	got, err := h.Observe(ctx, "input", &principal)
	if err != nil || got.Input.BodyGone || got.Input.StoredTextBytes != 0 {
		t.Fatalf("empty present: %+v %v", got, err)
	}
	ref := contract.CommandRef{Owner: owner, CommandID: "empty"}
	result, err := h.Cleanup(ctx, ref, "1", &principal)
	if err != nil || result != demo.WorkPending {
		t.Fatalf("pending: %s %v", result, err)
	}
	worker := conformanceWorker(t, owner, store, store, store, clock)
	batch, err := worker.Claim(ctx, "first", 1, time.Millisecond)
	if err != nil || len(batch) != 1 {
		t.Fatalf("claim: %+v %v", batch, err)
	}
	for _, expired := range []bool{false, true} {
		if expired {
			clock.Advance(time.Millisecond)
		}
		result, err = h.Cleanup(ctx, ref, "1", &principal)
		if err != nil || result != demo.WorkPending {
			t.Fatalf("unclosed claim expired=%t: %s %v", expired, result, err)
		}
	}
	batch, err = worker.Claim(ctx, "replacement", 1, time.Minute)
	if err != nil || len(batch) != 1 {
		t.Fatalf("replacement: %+v %v", batch, err)
	}
	startWork(t, worker, batch[0])
	if err = worker.Complete(ctx, batch[0].Claim, durablework.Project(batch[0])); err != nil {
		t.Fatal(err)
	}
	denied := hostFor(store, owner, principal)
	_, err = denied.Cleanup(ctx, ref, "1", &principal)
	assertReason(t, err, "forbidden")
	unknown := principal
	unknown.SubjectID = "unknown"
	_, err = h.Cleanup(ctx, ref, "1", &unknown)
	assertReason(t, err, "forbidden")
	delegated := principal
	delegated.DelegationChain = []contract.DelegatedSubject{{TenantID: principal.TenantID, SubjectID: "delegate"}}
	_, err = h.Cleanup(ctx, ref, "1", &delegated)
	assertReason(t, err, "forbidden")
	for _, foreign := range []contract.CommandRef{{Owner: contract.OwnerRef{TenantID: "other", OwnerID: owner.OwnerID}, CommandID: "empty"}, {Owner: contract.OwnerRef{TenantID: owner.TenantID, OwnerID: "other"}, CommandID: "empty"}} {
		_, err = h.Cleanup(ctx, foreign, "1", &principal)
		assertReason(t, err, "forbidden")
	}
	result, err = h.Cleanup(ctx, ref, "2", &principal)
	if err != nil || result != demo.NotApplicable {
		t.Fatalf("wrong applied revision: %s %v", result, err)
	}
	result, err = h.Cleanup(ctx, contract.CommandRef{Owner: owner, CommandID: "missing"}, "1", &principal)
	if err != nil || result != demo.NotApplicable {
		t.Fatalf("missing: %s %v", result, err)
	}
	expired := time.Now().Add(-time.Hour).UTC().Format("2006-01-02T15:04:05.000000Z")
	out, err = h.Record(ctx, command("expired", "other", "body", nil, expired), &principal)
	rejected := assertReceived(t, out, err)
	if _, ok := rejected.AsRejected(); !ok {
		t.Fatal("expired must be rejected")
	}
	result, err = h.Cleanup(ctx, contract.CommandRef{Owner: owner, CommandID: "expired"}, "1", &principal)
	if err != nil || result != demo.NotApplicable {
		t.Fatalf("rejected: %s %v", result, err)
	}
	assertReceiptSame(t, original, retentionQuery(t, h, "empty", false))
	result, err = h.Cleanup(ctx, ref, "1", &principal)
	if err != nil || result != demo.Cleaned {
		t.Fatalf("empty normal cleanup: %s %v", result, err)
	}
	got, err = h.Observe(ctx, "input", &principal)
	if err != nil || !got.Input.BodyGone || got.Input.Text != "" || got.Input.StoredTextBytes != 0 || got.Projection.TextDigest != "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatalf("empty gone: %+v %v", got, err)
	}
}

func retentionReopen(t *testing.T, store workStore) workStore {
	t.Helper()
	if pg, ok := store.(*postgres.Store); ok {
		cfg, ok := configurations.Load(pg)
		if !ok {
			t.Fatal("missing registered postgres scope")
		}
		replacement, err := postgres.Open(contextFor(t), cfg.(postgres.Config))
		if err != nil {
			t.Fatal(err)
		}
		configurations.Store(replacement, cfg)
		t.Cleanup(func() { replacement.Close() })
		return replacement
	}
	sqliteStore := store.(*sqlite.Store)
	cfg, ok := sqliteConfigurations.Load(sqliteStore)
	if !ok {
		t.Fatal("missing registered sqlite file")
	}
	if err := sqliteStore.Close(); err != nil {
		t.Fatal(err)
	}
	replacement, err := sqlite.Open(contextFor(t), cfg.(sqlite.Config))
	if err != nil {
		t.Fatal(err)
	}
	sqliteConfigurations.Store(replacement, cfg)
	t.Cleanup(func() { replacement.Close() })
	return replacement
}

func TestPGRetentionConcurrentRevision(t *testing.T) {
	retentionConcurrentRevision(t, func(t *testing.T) workStore { return database(t) })
}
func TestSQLiteRetentionConcurrentRevision(t *testing.T) {
	retentionConcurrentRevision(t, func(t *testing.T) workStore { return sqliteDatabase(t) })
}
func retentionConcurrentRevision(t *testing.T, newStore func(*testing.T) workStore) {
	for _, first := range []string{"cleanup", "record"} {
		t.Run(first, func(t *testing.T) {
			store := newStore(t)
			h := retentionHost(store)
			ctx := contextFor(t)
			raw := command("old", "input", "hello", nil, future())
			out, err := h.Record(ctx, raw, &principal)
			oldReceipt := assertReceived(t, out, err)
			oldWork := retentionComplete(t, store, h)
			ref := contract.CommandRef{Owner: owner, CommandID: "old"}
			revision := contract.Revision("1")
			gate := &transactionGate{runner: store, ready: make(chan struct{}), release: make(chan struct{})}
			secondGate := &transactionGate{runner: store, requested: make(chan struct{})}
			cleaner := retentionHost(store)
			recorder := retentionHost(store)
			if first == "cleanup" {
				cleaner.Runner = gate
				recorder.Runner = secondGate
			} else {
				recorder.Runner = gate
				cleaner.Runner = secondGate
			}
			cleanAnswer := make(chan struct {
				result demo.CleanupResult
				err    error
			}, 1)
			recordAnswer := make(chan struct {
				out contract.TransportOutcome
				err error
			}, 1)
			clean := func() {
				result, err := cleaner.Cleanup(ctx, ref, "1", &principal)
				cleanAnswer <- struct {
					result demo.CleanupResult
					err    error
				}{result, err}
			}
			record := func() {
				out, err := recorder.Record(ctx, command("new", "input", "new body", &revision, future()), &principal)
				recordAnswer <- struct {
					out contract.TransportOutcome
					err error
				}{out, err}
			}
			if first == "cleanup" {
				go clean()
			} else {
				go record()
			}
			awaitStage(t, ctx, gate.ready)
			if first == "cleanup" {
				go record()
			} else {
				go clean()
			}
			awaitStage(t, ctx, secondGate.requested)
			close(gate.release)
			cleaned := <-cleanAnswer
			recorded := <-recordAnswer
			if cleaned.err != nil {
				t.Fatal(cleaned.err)
			}
			want := demo.Cleaned
			if first == "record" {
				want = demo.RevisionChanged
			}
			if cleaned.result != want {
				t.Fatalf("%s first result: %s", first, cleaned.result)
			}
			nextReceipt := assertReceived(t, recorded.out, recorded.err)
			got, err := h.Observe(ctx, "input", &principal)
			if err != nil || got.Input.Text != "new body" || got.Input.BodyGone || got.Input.Revision != 2 || got.Job.ID != oldWork.Claim.JobID || got.Job.WorkRevision != 2 || got.Job.CompletedRevision != 1 || got.Job.State != "ready" {
				t.Fatalf("concurrent new revision: %+v %v", got, err)
			}
			retentionQuery(t, h, "old", first == "cleanup")
			assertReceiptSame(t, nextReceipt, retentionQuery(t, h, "new", false))
			out, err = h.Record(ctx, raw, &principal)
			assertReceiptSame(t, oldReceipt, assertReceived(t, out, err))
			retentionComplete(t, store, h)
		})
	}
}
func TestPGRetentionConcurrentCompletion(t *testing.T) {
	retentionConcurrentCompletion(t, func(t *testing.T) workStore { return database(t) })
}
func TestSQLiteRetentionConcurrentCompletion(t *testing.T) {
	retentionConcurrentCompletion(t, func(t *testing.T) workStore { return sqliteDatabase(t) })
}
func retentionConcurrentCompletion(t *testing.T, newStore func(*testing.T) workStore) {
	for _, first := range []string{"cleanup", "completion", "claim"} {
		t.Run(first, func(t *testing.T) {
			store := newStore(t)
			h := retentionHost(store)
			ctx := contextFor(t)
			out, err := h.Record(ctx, command("source", "input", "hello", nil, future()), &principal)
			original := assertReceived(t, out, err)
			worker := conformanceWorker(t, owner, store, store, store, h.Clock)
			gate := &transactionGate{runner: store, ready: make(chan struct{}), release: make(chan struct{})}
			secondGate := &transactionGate{runner: store, requested: make(chan struct{})}
			ref := contract.CommandRef{Owner: owner, CommandID: "source"}
			var work durablework.Work
			if first != "claim" {
				batch, err := worker.Claim(ctx, "worker", 1, time.Minute)
				if err != nil || len(batch) != 1 {
					t.Fatalf("claim: %+v %v", batch, err)
				}
				work = batch[0]
				startWork(t, worker, work)
			}
			answer := make(chan struct {
				result demo.CleanupResult
				err    error
			}, 1)
			completed := make(chan error, 1)
			claimed := make(chan []durablework.Work, 1)
			clean := func() {
				result, err := h.Cleanup(ctx, ref, "1", &principal)
				answer <- struct {
					result demo.CleanupResult
					err    error
				}{result, err}
			}
			if first == "cleanup" {
				h.Runner = gate
				worker.Runner = secondGate
				go clean()
			} else {
				worker.Runner = gate
				h.Runner = secondGate
				if first == "completion" {
					go func() { completed <- worker.Complete(ctx, work.Claim, durablework.Project(work)) }()
				} else {
					go func() { batch, err := worker.Claim(ctx, "worker", 1, time.Minute); claimed <- batch; completed <- err }()
				}
			}
			awaitStage(t, ctx, gate.ready)
			if first == "cleanup" {
				go func() { completed <- worker.Complete(ctx, work.Claim, durablework.Project(work)) }()
			} else {
				go clean()
			}
			awaitStage(t, ctx, secondGate.requested)
			close(gate.release)
			result := <-answer
			if result.err != nil {
				t.Fatal(result.err)
			}
			if err := <-completed; err != nil {
				t.Fatal(err)
			}
			want := demo.WorkPending
			if first == "completion" {
				want = demo.Cleaned
			}
			if result.result != want {
				t.Fatalf("%s first cleanup: %s", first, result.result)
			}
			h.Runner = store
			worker.Runner = store
			if first == "claim" {
				batch := <-claimed
				if len(batch) != 1 {
					t.Fatalf("claim competed: %+v", batch)
				}
				startWork(t, worker, batch[0])
				if err = worker.Complete(ctx, batch[0].Claim, durablework.Project(batch[0])); err != nil {
					t.Fatal(err)
				}
			}
			if first != "completion" {
				assertReceiptSame(t, original, retentionQuery(t, h, "source", false))
				result, err := h.Cleanup(ctx, ref, "1", &principal)
				if err != nil || result != demo.Cleaned {
					t.Fatalf("successful control cleanup: %s %v", result, err)
				}
			}
			retentionQuery(t, h, "source", true)
			got, err := h.Observe(ctx, "input", &principal)
			if err != nil || !got.Input.BodyGone || got.Input.StoredTextBytes != 0 || got.Job.State != "done" || got.Projection.InputRevision != 1 {
				t.Fatalf("complete clean: %+v %v", got, err)
			}
		})
	}
}

func TestPGRetentionStorageAndRollback(t *testing.T) { retentionStorageAndRollback(t, database(t)) }
func TestSQLiteRetentionStorageAndRollback(t *testing.T) {
	retentionStorageAndRollback(t, sqliteDatabase(t))
}
func retentionStorageAndRollback(t *testing.T, store workStore) {
	ctx := contextFor(t)
	h := retentionHost(store)
	raw := command("source", "input", "hello", nil, future())
	out, err := h.Record(ctx, raw, &principal)
	original := assertReceived(t, out, err)
	retentionComplete(t, store, h)
	// The approved storage seam must reject a gone flag with actual retained bytes.
	err = store.Within(ctx, owner, func(ctx context.Context, tx runtime.Tx) error {
		input, err := store.LockInput(ctx, tx, owner, "input")
		if err != nil {
			return err
		}
		input.BodyGone = true
		return store.SaveInput(ctx, tx, owner, *input)
	})
	if err == nil {
		t.Fatal("database accepted gone input with nonempty stored bytes")
	}
	got, err := h.Observe(ctx, "input", &principal)
	if err != nil || got.Input.BodyGone || got.Input.Text != "hello" || got.Input.StoredTextBytes != 5 {
		t.Fatalf("failed storage guard changed body: %+v %v", got, err)
	}
	ref := contract.CommandRef{Owner: owner, CommandID: "source"}
	cancelled, cancel := context.WithCancel(ctx)
	gate := &transactionGate{runner: store, ready: make(chan struct{}), release: make(chan struct{})}
	h.Runner = gate
	answer := make(chan error, 1)
	go func() { _, err := h.Cleanup(cancelled, ref, "1", &principal); answer <- err }()
	awaitStage(t, ctx, gate.ready)
	cancel()
	if err = <-answer; err == nil {
		t.Fatal("cancelled cleanup committed")
	}
	h.Runner = store
	assertReceiptSame(t, original, retentionQuery(t, h, "source", false))
	got, err = h.Observe(ctx, "input", &principal)
	if err != nil || got.Input.BodyGone || got.Input.Text != "hello" || got.Job.State != "done" {
		t.Fatalf("cleanup rollback partially committed: %+v %v", got, err)
	}
	_, err = h.Cleanup(context.Background(), ref, "1", &principal)
	assertReason(t, err, "dependency_unavailable")
	_, err = h.Cleanup(nil, ref, "1", &principal)
	assertReason(t, err, "dependency_unavailable")
	_, err = h.Cleanup(cancelled, ref, "1", &principal)
	assertReason(t, err, "dependency_unavailable")
	result, err := h.Cleanup(ctx, ref, "1", &principal)
	if err != nil || result != demo.Cleaned {
		t.Fatalf("cleanup normal control: %s %v", result, err)
	}
	retentionQuery(t, h, "source", true)
}

type retentionReaderGate struct {
	reader                    contract.CommandFactReader
	requested, ready, release chan struct{}
}

func (g *retentionReaderGate) ReadCommand(ctx context.Context, ref contract.CommandRef) (contract.CommandGetResponse, error) {
	if g.requested != nil {
		close(g.requested)
	}
	result, err := g.reader.ReadCommand(ctx, ref)
	if g.ready != nil {
		close(g.ready)
	}
	if g.release != nil {
		select {
		case <-g.release:
		case <-ctx.Done():
			return contract.CommandGetResponse{}, ctx.Err()
		}
	}
	return result, err
}
func TestPGRetentionConcurrentQueryAndReplay(t *testing.T) {
	retentionConcurrentReads(t, func(t *testing.T) workStore { return database(t) })
}
func TestSQLiteRetentionConcurrentQueryAndReplay(t *testing.T) {
	retentionConcurrentReads(t, func(t *testing.T) workStore { return sqliteDatabase(t) })
}
func retentionConcurrentReads(t *testing.T, newStore func(*testing.T) workStore) {
	for _, first := range []string{"cleanup", "query", "replay"} {
		t.Run(first, func(t *testing.T) {
			store := newStore(t)
			h := retentionHost(store)
			ctx := contextFor(t)
			raw := command("source", "input", "hello", nil, future())
			out, err := h.Record(ctx, raw, &principal)
			original := assertReceived(t, out, err)
			work := retentionComplete(t, store, h)
			ref := contract.CommandRef{Owner: owner, CommandID: "source"}
			cleaner := retentionHost(store)
			replayer := retentionHost(store)
			queryHost := retentionHost(store)
			cleanGate := &transactionGate{runner: store, requested: make(chan struct{}), ready: make(chan struct{}), release: make(chan struct{})}
			replayGate := &transactionGate{runner: store, requested: make(chan struct{}), ready: make(chan struct{}), release: make(chan struct{})}
			queryGate := &retentionReaderGate{reader: store, requested: make(chan struct{}), ready: make(chan struct{})}
			cleaner.Runner = cleanGate
			replayer.Runner = replayGate
			queryHost.Reader = queryGate
			cleanAnswer := make(chan struct {
				result demo.CleanupResult
				err    error
			}, 1)
			replayAnswer := make(chan struct {
				out contract.TransportOutcome
				err error
			}, 1)
			queryAnswer := make(chan struct {
				result contract.CommandGetResponse
				err    error
			}, 1)
			clean := func() {
				result, err := cleaner.Cleanup(ctx, ref, "1", &principal)
				cleanAnswer <- struct {
					result demo.CleanupResult
					err    error
				}{result, err}
			}
			replay := func() {
				out, err := replayer.Record(ctx, raw, &principal)
				replayAnswer <- struct {
					out contract.TransportOutcome
					err error
				}{out, err}
			}
			query := func() {
				result, err := contract.GetCommand(ctx, readWire(ref), &principal, queryHost.Permissions, queryHost, time.Now)
				queryAnswer <- struct {
					result contract.CommandGetResponse
					err    error
				}{result, err}
			}
			switch first {
			case "cleanup":
				go clean()
				awaitStage(t, ctx, cleanGate.ready)
				go query()
				go replay()
				awaitStage(t, ctx, queryGate.requested)
				awaitStage(t, ctx, replayGate.requested)
				close(cleanGate.release)
				awaitStage(t, ctx, replayGate.ready)
				close(replayGate.release)
			case "query":
				queryGate.release = make(chan struct{})
				go query()
				awaitStage(t, ctx, queryGate.ready)
				go clean()
				awaitStage(t, ctx, cleanGate.ready)
				go replay()
				awaitStage(t, ctx, replayGate.requested)
				close(queryGate.release)
				close(cleanGate.release)
				awaitStage(t, ctx, replayGate.ready)
				close(replayGate.release)
			case "replay":
				go replay()
				awaitStage(t, ctx, replayGate.ready)
				go clean()
				awaitStage(t, ctx, cleanGate.requested)
				go query()
				awaitStage(t, ctx, queryGate.requested)
				close(replayGate.release)
				awaitStage(t, ctx, cleanGate.ready)
				close(cleanGate.release)
			}
			cleaned := <-cleanAnswer
			replayed := <-replayAnswer
			queried := <-queryAnswer
			if cleaned.err != nil || cleaned.result != demo.Cleaned {
				t.Fatalf("concurrent cleanup: %s %v", cleaned.result, cleaned.err)
			}
			assertReceiptSame(t, original, assertReceived(t, replayed.out, replayed.err))
			if queried.err != nil {
				t.Fatal(queried.err)
			}
			if found, ok := queried.result.AsFound(); ok {
				if found.CommandRef != ref {
					t.Fatal("incorrect concurrent query ref")
				}
				assertReceiptSame(t, original, found.Receipt)
			} else if gone, ok := queried.result.AsGone(); !ok || gone.CommandRef != ref {
				t.Fatalf("query neither original nor gone: %+v", queried.result)
			}
			if first == "query" {
				if _, ok := queried.result.AsFound(); !ok {
					t.Fatal("query that read before cleanup lost fixed decision")
				}
			}
			retentionQuery(t, h, "source", true)
			got, err := h.Observe(ctx, "input", &principal)
			if err != nil || !got.Input.BodyGone || got.Input.StoredTextBytes != 0 || got.Input.Revision != 1 || got.Job.ID != work.Claim.JobID || got.Job.WorkRevision != 1 || got.Job.CompletedRevision != 1 || got.Job.State != "done" {
				t.Fatalf("concurrent query/replay resurrected body/work: %+v %v", got, err)
			}
		})
	}
}
