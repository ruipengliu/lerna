package contract_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

func TestDurableQueryBindingKeepsOriginalResultAndDeadline(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			f := fixture(t, backend, nil)
			scope := f.scope
			q, ok := f.store.(runtime.QueryBindingStore)
			if !ok {
				t.Fatal("adapter does not implement durable query identity")
			}
			ctx := context.Background()
			in := runtime.QueryBindingInput{QueryID: api.NewID("query"), PrincipalID: api.NewID("subject"), CredentialGeneration: 1, RolesDigest: api.Hash([]byte("roles")), QueryDigest: api.Hash([]byte("original exact query")), TTL: time.Minute}
			binding, status, e := q.BindQuery(ctx, scope, in)
			if e != nil || status != runtime.Committed || binding.ResultDigest != "" || !api.ValidID(binding.BindingID) {
				t.Fatalf("bind %+v %s %v", binding, status, e)
			}
			first := binding
			binding, status, e = q.SealQuery(ctx, scope, binding, api.Hash([]byte("current authorized result")))
			if e != nil || status != runtime.Committed {
				t.Fatalf("seal %s %v", status, e)
			}
			in.TTL = 5 * time.Minute
			repeated, status, e := q.BindQuery(ctx, scope, in)
			if e != nil || status != runtime.Committed || repeated.BindingID != first.BindingID || repeated.ExpiresAt != first.ExpiresAt || repeated.ResultDigest != binding.ResultDigest {
				t.Fatalf("repeat refreshed query %+v %s %v", repeated, status, e)
			}
			if _, status, e = q.SealQuery(ctx, scope, repeated, api.Hash([]byte("new snapshot"))); status != runtime.RolledBack || !api.IsCode(e, "cursor_expired") {
				t.Fatalf("query returned new snapshot %s %v", status, e)
			}
			in.QueryDigest = api.Hash([]byte("changed exact input"))
			if _, status, e = q.BindQuery(ctx, scope, in); status != runtime.RolledBack || !api.IsCode(e, "idempotency_conflict") {
				t.Fatalf("changed query reused identity %s %v", status, e)
			}
		})
	}
}

func queryInput() runtime.QueryBindingInput {
	return runtime.QueryBindingInput{QueryID: api.NewID("query"), PrincipalID: api.NewID("subject"), CredentialGeneration: 1, RolesDigest: api.Hash([]byte("trusted roles")), QueryDigest: api.Hash([]byte("exact canonical query")), TTL: time.Minute}
}

func TestQueryIdentityIsSharedAcrossReplicasAndScopedByOwnerAndTenant(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			f := fixture(t, backend, nil)
			first, second := f.store.(runtime.QueryBindingStore), f.open(nil).(runtime.QueryBindingStore)
			a, b := queryInput(), queryInput()
			b.QueryID = a.QueryID
			start := make(chan struct{})
			type outcome struct {
				input   runtime.QueryBindingInput
				binding runtime.QueryBinding
				status  runtime.CommitStatus
				err     error
			}
			outcomes := make(chan outcome, 2)
			var wg sync.WaitGroup
			for i, in := range []runtime.QueryBindingInput{a, b} {
				q := []runtime.QueryBindingStore{first, second}[i]
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					bound, status, e := q.BindQuery(context.Background(), f.scope, in)
					outcomes <- outcome{in, bound, status, e}
				}()
			}
			close(start)
			wg.Wait()
			close(outcomes)
			var winner outcome
			successes, conflicts := 0, 0
			for result := range outcomes {
				if result.err == nil && result.status == runtime.Committed {
					successes++
					winner = result
				} else if api.IsCode(result.err, "idempotency_conflict") && result.status == runtime.RolledBack {
					conflicts++
				} else {
					t.Fatalf("unexpected concurrent query outcome %s %v", result.status, result.err)
				}
			}
			if successes != 1 || conflicts != 1 {
				t.Fatalf("query identity forked: success=%d conflict=%d", successes, conflicts)
			}
			repeated, status, e := second.BindQuery(context.Background(), f.scope, winner.input)
			if e != nil || status != runtime.Committed || !api.Equal(repeated, winner.binding) {
				t.Fatalf("replica changed binding %+v %s %v", repeated, status, e)
			}
			for _, field := range []string{"principal", "generation", "roles", "input"} {
				changed := winner.input
				switch field {
				case "principal":
					changed.PrincipalID = api.NewID("subject")
				case "generation":
					changed.CredentialGeneration++
				case "roles":
					changed.RolesDigest = api.Hash([]byte("different authority"))
				case "input":
					changed.QueryDigest = api.Hash([]byte("different canonical input"))
				}
				if _, status, e = first.BindQuery(context.Background(), f.scope, changed); status != runtime.RolledBack || !api.IsCode(e, "idempotency_conflict") {
					t.Fatalf("changed %s did not conflict %s %v", field, status, e)
				}
			}
			for _, field := range []string{"owner", "tenant"} {
				scope := f.scope
				if field == "owner" {
					scope.OwnerID = api.NewID("owner")
				} else {
					scope.TenantID = api.NewID("tenant")
				}
				bound, status, e := first.BindQuery(context.Background(), scope, b)
				if e != nil || status != runtime.Committed || bound.BindingID == winner.binding.BindingID {
					t.Fatalf("query crossed %s scope %+v %s %v", field, bound, status, e)
				}
			}
			scope := f.scope
			scope.DatabaseID = api.NewID("database")
			if _, status, e = second.BindQuery(context.Background(), scope, winner.input); status != runtime.RolledBack || !api.IsCode(e, "forbidden") {
				t.Fatalf("wrong database accepted query %s %v", status, e)
			}
			digest := api.Hash([]byte("one currently authorized result"))
			sealed, status, e := second.SealQuery(context.Background(), f.scope, winner.binding, digest)
			if e != nil || status != runtime.Committed || sealed.ExpiresAt != winner.binding.ExpiresAt || sealed.ResultDigest != digest {
				t.Fatalf("replica could not seal %+v %s %v", sealed, status, e)
			}
			if _, status, e = first.SealQuery(context.Background(), f.scope, winner.binding, api.Hash([]byte("new snapshot"))); status != runtime.RolledBack || !api.IsCode(e, "cursor_expired") {
				t.Fatalf("replica changed sealed snapshot %s %v", status, e)
			}
		})
	}
}

func TestQueryExpiryHasNoPermanentTombstoneAndOldSealCannotPoisonNewBinding(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			f := fixture(t, backend, nil)
			q := f.store.(runtime.QueryBindingStore)
			in := queryInput()
			in.TTL = time.Second
			old, status, e := q.BindQuery(context.Background(), f.scope, in)
			if e != nil || status != runtime.Committed {
				t.Fatalf("bind %s %v", status, e)
			}
			for i := 0; i < 2; i++ {
				extra := in
				extra.QueryID = api.NewID("query")
				if _, status, e = q.BindQuery(context.Background(), f.scope, extra); e != nil || status != runtime.Committed {
					t.Fatalf("bind sibling %s %v", status, e)
				}
			}
			other := f.scope
			other.OwnerID = api.NewID("owner")
			if _, status, e = q.BindQuery(context.Background(), other, in); e != nil || status != runtime.Committed {
				t.Fatalf("bind other owner %s %v", status, e)
			}
			timer := time.NewTimer(time.Second + 100*time.Millisecond)
			defer timer.Stop()
			<-timer.C
			if _, status, e = q.SealQuery(context.Background(), f.scope, old, api.Hash([]byte("late old result"))); status != runtime.RolledBack || !api.IsCode(e, "cursor_expired") {
				t.Fatalf("expired query sealed result %s %v", status, e)
			}
			in.TTL, in.CredentialGeneration, in.QueryDigest = time.Minute, 2, api.Hash([]byte("new query after expiry"))
			fresh, status, e := q.BindQuery(context.Background(), f.scope, in)
			if e != nil || status != runtime.Committed || fresh.BindingID == old.BindingID || fresh.ResultDigest != "" {
				t.Fatalf("query tombstone blocked new lifetime %+v %s %v", fresh, status, e)
			}
			if _, status, e = q.SealQuery(context.Background(), f.scope, old, api.Hash([]byte("poison new lifetime"))); status != runtime.RolledBack || !api.IsCode(e, "cursor_expired") {
				t.Fatalf("old lifetime contaminated new query %s %v", status, e)
			}
			for _, want := range []int{1, 1, 0} {
				count, status, e := q.PruneQueries(context.Background(), f.scope, 1)
				if e != nil || status != runtime.Committed || count != want {
					t.Fatalf("bounded expiry cleanup got=%d want=%d %s %v", count, want, status, e)
				}
			}
			count, status, e := q.PruneQueries(context.Background(), other, 100)
			if e != nil || status != runtime.Committed || count != 1 {
				t.Fatalf("cleanup lost scoped other owner count=%d %s %v", count, status, e)
			}
			repeated, status, e := q.BindQuery(context.Background(), f.scope, in)
			if e != nil || status != runtime.Committed || !api.Equal(fresh, repeated) {
				t.Fatalf("cleanup modified unexpired binding %+v %s %v", repeated, status, e)
			}
		})
	}
}

func TestQueryBindingCommitLossIsRecoveredByOriginalIdentity(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			var fault atomic.Int32
			injected := errors.New("injected query commit reply loss")
			f := fixture(t, backend, func(phase string) error {
				if phase == "before_commit" && fault.CompareAndSwap(1, 0) {
					return injected
				}
				if phase == "after_commit" && fault.CompareAndSwap(2, 0) {
					return injected
				}
				return nil
			})
			q := f.store.(runtime.QueryBindingStore)
			in := queryInput()
			fault.Store(1)
			if _, status, e := q.BindQuery(context.Background(), f.scope, in); status != runtime.RolledBack || !errors.Is(e, injected) {
				t.Fatalf("before commit status %s %v", status, e)
			}
			fault.Store(2)
			if _, status, e := q.BindQuery(context.Background(), f.scope, in); status != runtime.CommitUnknown || !errors.Is(e, runtime.ErrCommitUnknown) {
				t.Fatalf("bind unknown status %s %v", status, e)
			}
			binding, status, e := q.BindQuery(context.Background(), f.scope, in)
			if e != nil || status != runtime.Committed {
				t.Fatalf("bind recovery %s %v", status, e)
			}
			digest := api.Hash([]byte("current gate rechecked result"))
			fault.Store(2)
			if _, status, e = q.SealQuery(context.Background(), f.scope, binding, digest); status != runtime.CommitUnknown || !errors.Is(e, runtime.ErrCommitUnknown) {
				t.Fatalf("seal unknown status %s %v", status, e)
			}
			repeated, status, e := f.open(nil).(runtime.QueryBindingStore).BindQuery(context.Background(), f.scope, in)
			if e != nil || status != runtime.Committed || repeated.BindingID != binding.BindingID || repeated.ExpiresAt != binding.ExpiresAt || repeated.ResultDigest != digest {
				t.Fatalf("seal recovery changed original %+v %s %v", repeated, status, e)
			}
			if _, status, e = q.SealQuery(context.Background(), f.scope, repeated, digest); e != nil || status != runtime.Committed {
				t.Fatalf("original seal recovery %s %v", status, e)
			}
		})
	}
}

func TestQuerySealRechecksOriginalExpiryAtActualCommit(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			var pause atomic.Bool
			var expires time.Time
			f := fixture(t, backend, func(phase string) error {
				if phase == "before_commit" && pause.Swap(false) {
					timer := time.NewTimer(time.Until(expires) + 20*time.Millisecond)
					defer timer.Stop()
					<-timer.C
				}
				return nil
			})
			q := f.store.(runtime.QueryBindingStore)
			in := queryInput()
			in.TTL = time.Second
			binding, status, e := q.BindQuery(context.Background(), f.scope, in)
			if e != nil || status != runtime.Committed {
				t.Fatalf("bind %s %v", status, e)
			}
			expires, e = api.ParseTime(binding.ExpiresAt)
			if e != nil {
				t.Fatal(e)
			}
			pause.Store(true)
			if _, status, e = q.SealQuery(context.Background(), f.scope, binding, api.Hash([]byte("result before process pause"))); status != runtime.RolledBack || !api.IsCode(e, "cursor_expired") {
				t.Fatalf("query committed beyond original expiry %s %v", status, e)
			}
			fresh, status, e := q.BindQuery(context.Background(), f.scope, in)
			if e != nil || status != runtime.Committed || fresh.BindingID == binding.BindingID || fresh.ResultDigest != "" {
				t.Fatalf("expired paused query poisoned new query %+v %s %v", fresh, status, e)
			}
		})
	}
}

func TestPostgresQueryPruneSkipsAnotherReplicasLockedExpiredBinding(t *testing.T) {
	var hold atomic.Bool
	locked, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseWriter := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseWriter()
	f := fixture(t, "postgres", func(phase string) error {
		if phase == "before_commit" && hold.Swap(false) {
			close(locked)
			<-release
		}
		return nil
	})
	q := f.store.(runtime.QueryBindingStore)
	a, b := queryInput(), queryInput()
	a.TTL, b.TTL = time.Second, time.Second
	first, status, e := q.BindQuery(context.Background(), f.scope, a)
	if e != nil || status != runtime.Committed {
		t.Fatalf("first bind %s %v", status, e)
	}
	second, status, e := q.BindQuery(context.Background(), f.scope, b)
	if e != nil || status != runtime.Committed {
		t.Fatalf("second bind %s %v", status, e)
	}
	type finish struct {
		status runtime.CommitStatus
		err    error
	}
	completed := make(chan finish, 1)
	hold.Store(true)
	go func() {
		_, status, e := q.SealQuery(context.Background(), f.scope, first, api.Hash([]byte("held result")))
		completed <- finish{status, e}
	}()
	select {
	case <-locked:
	case <-time.After(3 * time.Second):
		t.Fatal("writer did not lock original query")
	}
	expires, _ := api.ParseTime(second.ExpiresAt)
	timer := time.NewTimer(time.Until(expires) + 20*time.Millisecond)
	defer timer.Stop()
	<-timer.C
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	count, status, e := q.PruneQueries(ctx, f.scope, 100)
	if e != nil || status != runtime.Committed || count != 1 {
		t.Fatalf("expired peer blocked behind locked query count=%d %s %v", count, status, e)
	}
	releaseWriter()
	select {
	case result := <-completed:
		if result.status != runtime.RolledBack || !api.IsCode(result.err, "cursor_expired") {
			t.Fatalf("locked expired query status %s %v", result.status, result.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("original query writer did not stop")
	}
	count, status, e = q.PruneQueries(context.Background(), f.scope, 100)
	if e != nil || status != runtime.Committed || count != 1 {
		t.Fatalf("unlocked query cleanup count=%d %s %v", count, status, e)
	}
}
