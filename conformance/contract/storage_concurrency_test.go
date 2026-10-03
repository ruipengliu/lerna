package contract_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

func TestPostgresOpenRequiresBoundedNonzeroConnectionPool(t *testing.T) {
	for _, limit := range []int{0, -1, 129} {
		_, err := postgres.Open(context.Background(), "host=127.0.0.1 port=1 user=harness dbname=harness sslmode=disable", postgres.WithMaxConnections(limit))
		if !api.IsCode(err, "invalid_request") {
			t.Fatalf("invalid pool %d reached connection instead of configuration rejection: %v", limit, err)
		}
	}
}

func TestPostgresClaimSkipsAnotherWritersLockedJob(t *testing.T) {
	f := fixture(t, "postgres", nil)
	ctx := context.Background()
	var first, second api.Job
	commit(t, f, func(tx runtime.Tx) error {
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		first, err = tx.Raise(ctx, "task.progress", "first", tx.Scope().Ref(api.NewID("task"), 1), now.Add(-time.Hour))
		if err != nil {
			return err
		}
		second, err = tx.Raise(ctx, "task.progress", "second", tx.Scope().Ref(api.NewID("task"), 1), now)
		return err
	})
	locked, release, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		_, err := f.store.Within(ctx, f.scope, []string{"task"}, func(tx runtime.Tx) error {
			if err := tx.Hint(ctx, first.JobID, time.Now().Add(-2*time.Hour)); err != nil {
				return err
			}
			close(locked)
			<-release
			return nil
		})
		finished <- err
	}()
	var releaseOnce sync.Once
	releaseWriter := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseWriter()
	select {
	case <-locked:
	case <-time.After(2 * time.Second):
		t.Fatal("writer failed to hold original job")
	}
	claimCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	works, status, err := f.store.Claim(claimCtx, f.scope, api.NewID("boot"), []string{"task.progress"}, 1, time.Second)
	if err != nil || status != runtime.Committed || len(works) != 1 || works[0].Job.JobID != second.JobID {
		t.Fatalf("ready peer was blocked behind locked job: %+v %s %v", works, status, err)
	}
	releaseWriter()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("original writer did not finish")
	}
}

func TestDurableOriginalSemanticBindingWinsConcurrentCreation(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			f := fixture(t, backend, nil)
			ctx := context.Background()
			a, b := api.NewID("task"), api.NewID("task")
			commit(t, f, func(tx runtime.Tx) error {
				if err := tx.Create(ctx, "task.tasks", a, "", testRecord{Value: "a"}); err != nil {
					return err
				}
				return tx.Create(ctx, "task.tasks", b, "", testRecord{Value: "b"})
			})
			start := make(chan struct{})
			results := make(chan error, 2)
			var wg sync.WaitGroup
			for _, id := range []string{a, b} {
				wg.Add(1)
				go func(id string) {
					defer wg.Done()
					<-start
					_, err := f.store.Within(ctx, f.scope, []string{"task"}, func(tx runtime.Tx) error { return tx.Bind(ctx, "task.tasks", "one original source", id, id) })
					results <- err
				}(id)
			}
			close(start)
			wg.Wait()
			close(results)
			committed, conflicts := 0, 0
			for err := range results {
				if err == nil {
					committed++
				} else if api.IsCode(err, "idempotency_conflict") {
					conflicts++
				} else {
					t.Errorf("storage exception leaked as wrong business category: %v", err)
				}
			}
			if committed != 1 || conflicts != 1 {
				t.Fatalf("one original mapping required: commits=%d conflicts=%d", committed, conflicts)
			}
		})
	}
}

func TestUncertainClaimUsesRetainedOriginalAndRenewalDoesNotInventAuthority(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			var armed atomic.Bool
			failure := errors.New("lost lease commit acknowledgement")
			f := fixture(t, backend, func(phase string) error {
				if armed.Load() && phase == "after_commit" {
					return failure
				}
				return nil
			})
			ctx := context.Background()
			id := api.NewID("task")
			commit(t, f, func(tx runtime.Tx) error {
				now, err := tx.Now(ctx)
				if err != nil {
					return err
				}
				_, err = tx.Raise(ctx, "task.progress", id, tx.Scope().Ref(id, 1), now)
				return err
			})
			armed.Store(true)
			works, status, err := f.store.Claim(ctx, f.scope, api.NewID("boot"), []string{"task.progress"}, 1, 100*time.Millisecond)
			armed.Store(false)
			if status != runtime.CommitUnknown || !errors.Is(err, runtime.ErrCommitUnknown) || len(works) != 1 {
				t.Fatalf("unknown claim candidate: %+v %s %v", works, status, err)
			}
			original := works[0].Claim
			if err = f.store.CheckClaim(ctx, f.scope, original); err != nil {
				t.Fatalf("original retained claim not independently confirmed: %v", err)
			}
			forged := original
			forged.HolderID = api.NewID("boot")
			if err = f.store.CheckClaim(ctx, f.scope, forged); !errors.Is(err, runtime.ErrClaimLost) {
				t.Fatalf("invented holder obtained authority: %v", err)
			}
			armed.Store(true)
			renewed, status, err := f.store.Renew(ctx, f.scope, original, time.Second)
			armed.Store(false)
			if status != runtime.CommitUnknown || !errors.Is(err, runtime.ErrCommitUnknown) || renewed.ObservedWorkRevision != original.ObservedWorkRevision || renewed.LeaseEpoch != original.LeaseEpoch {
				t.Fatalf("uncertain renewal changed original identity: %+v %s %v", renewed, status, err)
			}
			until, _ := api.ParseTime(original.LeaseUntil)
			time.Sleep(time.Until(until) + 5*time.Millisecond)
			if err = f.store.CheckClaim(ctx, f.scope, original); !errors.Is(err, runtime.ErrClaimLost) {
				t.Fatalf("old confirmed deadline silently extended after uncertain renewal: %v", err)
			}
			if err = f.store.CheckClaim(ctx, f.scope, renewed); err != nil {
				t.Fatalf("exact renewed candidate could not be independently confirmed: %v", err)
			}
		})
	}
}

func TestPermanentCommandTombstoneCannotReopenAfterRestart(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			f := fixture(t, backend, nil)
			ctx := context.Background()
			id, target, principal := api.NewID("command"), api.NewID("task"), api.NewID("subject")
			original := stored(f.scope, id, target, principal)
			commit(t, f, func(tx runtime.Tx) error { return tx.SaveCommand(ctx, original) })
			commit(t, f, func(tx runtime.Tx) error {
				old, err := tx.LoadCommand(ctx, id)
				if err != nil {
					return err
				}
				old.Tombstone = true
				old.Receipt.Output = nil
				return tx.SaveCommand(ctx, old)
			})
			if err := f.store.Close(); err != nil {
				t.Fatal(err)
			}
			f.store = f.open(nil)
			d := runtime.Dispatcher{Store: f.store, OwnerID: f.scope.OwnerID, Registry: runtime.NewRegistry()}
			auth := runtime.Auth{TenantID: f.scope.TenantID, SubjectID: principal, CredentialGeneration: 1}
			if _, err := d.Command(ctx, auth, api.Raw(original.Command)); !api.IsCode(err, "gone") {
				t.Fatalf("old original became a new command: %v", err)
			}
			if _, err := d.Lookup(ctx, auth, id); !api.IsCode(err, "gone") {
				t.Fatalf("collected original body treated as absent: %v", err)
			}
			status, err := f.store.Within(ctx, f.scope, nil, func(tx runtime.Tx) error { return tx.SaveCommand(ctx, original) })
			if status != runtime.RolledBack || !api.IsCode(err, "gone") {
				t.Fatalf("tombstone reused: %s %v", status, err)
			}
		})
	}
}
