package runtime_test

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

func TestWorkerRenewsOriginalClaimDuringBoundedIO(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "worker.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	scope := runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner"), DatabaseID: store.ID()}
	source := scope.Ref(api.NewID("source"), 1)
	if _, err = store.Within(ctx, scope, []string{"fixture"}, func(tx runtime.Tx) error {
		now, e := tx.Now(ctx)
		if e != nil {
			return e
		}
		_, e = tx.Raise(ctx, "fixture.long_io", "original", source, now)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	reg := runtime.NewRegistry()
	var entered atomic.Int32
	completed := make(chan error, 1)
	if err = reg.RegisterJob("fixture.long_io", func(ctx context.Context, st runtime.Store, s runtime.Scope, w runtime.Work) error {
		entered.Add(1)
		select {
		case <-ctx.Done():
			completed <- ctx.Err()
			return ctx.Err()
		case <-time.After(2200 * time.Millisecond):
		}
		e := runtime.Finish(ctx, st, s, []string{"fixture"}, w, runtime.Done(), func(tx runtime.Tx) error {
			return tx.Create(ctx, "fixture.results", source.ObjectID, "", struct {
				Revision uint64 `json:"revision"`
			}{1})
		})
		completed <- e
		return e
	}); err != nil {
		t.Fatal(err)
	}
	worker := runtime.Worker{Store: store, Registry: reg, Scopes: []runtime.Scope{scope}, Kinds: reg.JobKinds(), Concurrency: 1, Lease: time.Second, Poll: 10 * time.Millisecond}
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	select {
	case e := <-completed:
		if e != nil {
			t.Fatal(e)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	cancel()
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	if entered.Load() != 1 {
		t.Fatal("renewal caused a second invocation")
	}
	var row struct {
		Revision uint64 `json:"revision"`
	}
	if _, e := store.Read(context.Background(), scope, "fixture.results", source.ObjectID, 0, &row); e != nil {
		t.Fatal(e)
	}
}

// 真SQLite已延长租约，但续租提交答复丢失。本地出站资格只能到原确认期限，
// 且取消后仍须等待实际I/O退出才归还容量。
func TestWorkerUnknownRenewalKeepsConfirmedDeadlineAndWaitsForIOExit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "unknown-renewal.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	scope := runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner"), DatabaseID: store.ID()}
	source := scope.Ref(api.NewID("source"), 1)
	if _, err = store.Within(ctx, scope, []string{"fixture"}, func(tx runtime.Tx) error {
		now, e := tx.Now(ctx)
		if e != nil {
			return e
		}
		if _, e = tx.Raise(ctx, "fixture.unknown_renew", "first", source, now); e != nil {
			return e
		}
		_, e = tx.Raise(ctx, "fixture.unknown_renew", "second", source, now)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	wrapped := &lostRenewalStore{Store: store, renewed: make(chan api.Claim, 1)}
	reg := runtime.NewRegistry()
	var entered atomic.Int32
	original := make(chan api.Claim, 1)
	canceled := make(chan time.Time, 1)
	finishAttempt := make(chan error, 1)
	exit := make(chan struct{})
	defer func() {
		select {
		case <-exit:
		default:
			close(exit)
		}
	}()
	if err = reg.RegisterJob("fixture.unknown_renew", func(ctx context.Context, st runtime.Store, s runtime.Scope, w runtime.Work) error {
		entered.Add(1)
		original <- w.Claim
		<-ctx.Done()
		canceled <- time.Now()
		attemptCtx, stop := context.WithTimeout(context.Background(), time.Second)
		err := runtime.Finish(attemptCtx, st, s, []string{"fixture"}, w, runtime.Done(), func(tx runtime.Tx) error {
			return tx.Create(attemptCtx, "fixture.results", source.ObjectID, "", struct {
				Revision uint64 `json:"revision"`
			}{1})
		})
		stop()
		finishAttempt <- err
		<-exit // 独立目标仍在退出，不能释放槽或让Worker.Run提前返回。
		return ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}
	workerCtx, stopWorker := context.WithCancel(ctx)
	defer stopWorker()
	done := make(chan error, 1)
	worker := runtime.Worker{Store: wrapped, Registry: reg, Scopes: []runtime.Scope{scope}, Kinds: reg.JobKinds(), Concurrency: 1, Lease: time.Second, Poll: 10 * time.Millisecond}
	go func() { done <- worker.Run(workerCtx) }()
	var claim, actual api.Claim
	select {
	case claim = <-original:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case actual = <-wrapped.renewed:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	knownUntil, err := api.ParseTime(claim.LeaseUntil)
	if err != nil {
		t.Fatal(err)
	}
	actualUntil, err := api.ParseTime(actual.LeaseUntil)
	if err != nil || !actualUntil.After(knownUntil) {
		t.Fatal("fixture did not actually commit the lost renewal")
	}
	select {
	case at := <-canceled:
		if at.After(knownUntil.Add(150*time.Millisecond)) || !at.Before(actualUntil) {
			t.Fatalf("unknown renewal extended local authority: known=%s canceled=%s actual=%s", knownUntil, at, actualUntil)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err = <-finishAttempt; err == nil {
		t.Fatal("expired locally confirmed claim committed a business fact")
	}
	select {
	case <-time.After(400 * time.Millisecond):
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if entered.Load() != 1 {
		t.Fatal("a second handler entered before actual exit")
	}
	stopWorker()
	select {
	case <-done:
		t.Fatal("worker returned before actual I/O exit")
	case <-time.After(100 * time.Millisecond):
	}
	close(exit)
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	var result struct {
		Revision uint64 `json:"revision"`
	}
	if _, err = store.Read(context.Background(), scope, "fixture.results", source.ObjectID, 0, &result); !api.IsCode(err, "not_found") {
		t.Fatalf("lost renewal allowed a fact: %v", err)
	}
}

type lostRenewalStore struct {
	runtime.Store
	calls   atomic.Int32
	renewed chan api.Claim
}

func (s *lostRenewalStore) Renew(ctx context.Context, scope runtime.Scope, c api.Claim, lease time.Duration) (api.Claim, runtime.CommitStatus, error) {
	if s.calls.Add(1) == 1 {
		next, status, err := s.Store.Renew(ctx, scope, c, lease)
		if err != nil || status != runtime.Committed {
			return next, status, err
		}
		s.renewed <- next
	}
	return c, runtime.CommitUnknown, runtime.ErrCommitUnknown
}
