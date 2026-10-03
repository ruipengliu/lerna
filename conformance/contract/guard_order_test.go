package contract_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

func TestPostgresGuardAndDomainRaiseCannotFormReverseLockCycle(t *testing.T) {
	f := fixture(t, "postgres", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	id := api.NewID("object")
	status, e := f.store.Within(ctx, f.scope, []string{"test"}, func(tx runtime.Tx) error {
		if e := tx.Create(ctx, "test.records", id, "", map[string]any{"name": "original parent"}); e != nil {
			return e
		}
		_, e := tx.Raise(ctx, "test.work", id, f.scope.Ref(id, 1), time.Now())
		return e
	})
	if e != nil || status != runtime.Committed {
		t.Fatalf("create %s %v", status, e)
	}
	works, status, e := f.store.Claim(ctx, f.scope, api.NewID("boot"), []string{"test.work"}, 1, 10*time.Second)
	if e != nil || status != runtime.Committed || len(works) != 1 {
		t.Fatalf("claim %s %v", status, e)
	}
	guarded, domainLocked := make(chan struct{}), make(chan struct{})
	readDone := make(chan error, 1)
	go func() {
		_, e := f.store.Within(ctx, f.scope, []string{"test"}, func(tx runtime.Tx) error {
			e := tx.Guard(ctx, works[0].Claim)
			close(guarded)
			if e != nil {
				return e
			}
			select {
			case <-domainLocked:
			case <-ctx.Done():
				return ctx.Err()
			}
			var current map[string]any
			_, e = tx.Get(ctx, "test.records", id, &current)
			return e
		})
		readDone <- e
	}()
	<-guarded
	status, e = f.open(nil).Within(ctx, f.scope, []string{"test"}, func(tx runtime.Tx) error {
		var current map[string]any
		if _, e := tx.Get(ctx, "test.records", id, &current); e != nil {
			close(domainLocked)
			return e
		}
		close(domainLocked)
		if e := tx.Put(ctx, "test.records", id, 1, map[string]any{"name": "new parent"}); e != nil {
			return e
		}
		_, e := tx.Raise(ctx, "test.work", id, f.scope.Ref(id, 2), time.Now())
		return e
	})
	firstError := <-readDone
	if e != nil || status != runtime.Committed || firstError != nil {
		t.Fatalf("Guard→domain / domain→Raise lock cycle: writer=%s %v reader=%v", status, e, firstError)
	}
	if e = f.store.CheckClaim(ctx, f.scope, works[0].Claim); e != nil {
		t.Fatalf("new Raise stole current holder %v", e)
	}
}

func TestPostgresRolledBackSavepointFinishCannotBypassFinalGuard(t *testing.T) {
	f := fixture(t, "postgres", nil)
	competingStore := f.open(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	id, proof := api.NewID("object"), api.NewID("object")
	status, e := f.store.Within(ctx, f.scope, []string{"test"}, func(tx runtime.Tx) error {
		_, e := tx.Raise(ctx, "test.work", id, f.scope.Ref(id, 1), time.Now())
		return e
	})
	if e != nil || status != runtime.Committed {
		t.Fatalf("raise %s %v", status, e)
	}
	works, status, e := f.store.Claim(ctx, f.scope, api.NewID("boot"), []string{"test.work"}, 1, 20*time.Second)
	if e != nil || status != runtime.Committed || len(works) != 1 {
		t.Fatalf("claim %s %v", status, e)
	}
	rolledBack, competitorDone := make(chan struct{}), make(chan struct{})
	type result struct {
		status runtime.CommitStatus
		err    error
	}
	done := make(chan result, 1)
	go func() {
		status, err := f.store.Within(ctx, f.scope, []string{"test"}, func(tx runtime.Tx) error {
			if e := tx.Guard(ctx, works[0].Claim); e != nil {
				return e
			}
			businessError := api.E("invalid_request", "business_rejected")
			if e := tx.Savepoint(ctx, func(tx runtime.Tx) error {
				if e := tx.Finish(ctx, works[0].Claim, runtime.Done()); e != nil {
					return e
				}
				return businessError
			}); !errors.Is(e, businessError) {
				return e
			}
			if e := tx.Create(ctx, "test.records", proof, "", map[string]any{"name": "must roll back"}); e != nil {
				return e
			}
			close(rolledBack)
			select {
			case <-competitorDone:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		done <- result{status, err}
	}()
	select {
	case <-rolledBack:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	e = runtime.Finish(ctx, competingStore, f.scope, []string{"test"}, works[0], runtime.Done(), nil)
	close(competitorDone)
	r := <-done
	if e != nil || r.status != runtime.RolledBack || !errors.Is(r.err, runtime.ErrClaimLost) {
		t.Fatalf("savepoint finish bypassed fresh final guard: competing=%v original=%s %v", e, r.status, r.err)
	}
	var value map[string]any
	if _, e = f.store.Read(ctx, f.scope, "test.records", proof, 0, &value); !errors.Is(e, runtime.ErrNotFound) {
		t.Fatalf("lost claim committed protected business fact: %v", e)
	}
}
