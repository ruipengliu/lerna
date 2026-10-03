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
