package contract_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

type storageFixture struct {
	store    runtime.Store
	scope    runtime.Scope
	open     func(func(string) error) runtime.Store
	location string
}

func fixture(t *testing.T, backend string, fault func(string) error) storageFixture {
	t.Helper()
	ctx := context.Background()
	var open func(func(string) error) runtime.Store
	var location string
	if backend == "postgres" {
		dsn := os.Getenv("HARNESS_TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("HARNESS_TEST_POSTGRES_DSN must point to an actual PostgreSQL database")
		}
		location = dsn
		open = func(f func(string) error) runtime.Store {
			s, err := postgres.Open(ctx, dsn, postgres.WithCommitFault(func(p postgres.CommitPhase) error {
				if f != nil {
					return f(string(p))
				}
				return nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			if err = s.Migrate(ctx); err != nil {
				s.Close()
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := s.Close(); err != nil {
					t.Error(err)
				}
			})
			return s
		}
	} else {
		path := filepath.Join(t.TempDir(), "original.sqlite")
		location = path
		open = func(f func(string) error) runtime.Store {
			s, err := sqlite.Open(path, sqlite.WithCommitFault(func(p sqlite.CommitPhase) error {
				if f != nil {
					return f(string(p))
				}
				return nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			if err = s.Migrate(ctx); err != nil {
				s.Close()
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := s.Close(); err != nil {
					t.Error(err)
				}
			})
			return s
		}
	}
	s := open(fault)
	return storageFixture{store: s, scope: runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner"), DatabaseID: s.ID()}, open: open, location: location}
}
func commit(t *testing.T, f storageFixture, fn func(runtime.Tx) error) {
	t.Helper()
	status, err := f.store.Within(context.Background(), f.scope, []string{"task"}, fn)
	if err != nil || status != runtime.Committed {
		t.Fatalf("commit: %s %v", status, err)
	}
}
func command(scope runtime.Scope, id, target string) api.Command {
	return api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: scope.OwnerID, CommandID: id, Method: "task.set", TargetID: target, ExpiresAt: api.Time(time.Now().Add(time.Hour)), Payload: api.Raw(testRecord{Value: "original"})}
}
func stored(scope runtime.Scope, id, target, principal string) runtime.StoredCommand {
	c := command(scope, id, target)
	canonical, err := api.Canonical(api.Raw(c))
	if err != nil {
		panic(err)
	}
	digest := api.Hash(canonical)
	return runtime.StoredCommand{Command: c, PrincipalID: principal, Digest: digest, Receipt: api.Receipt{CommandID: id, RequestDigest: digest, Stage: "applied", DecidedAt: api.Time(time.Now()), Output: api.Raw(testRecord{Value: "original"})}}
}

func TestDurableStorageContract(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			t.Run("records history keys scope and bounded relations", func(t *testing.T) {
				f := fixture(t, backend, nil)
				ctx := context.Background()
				id := api.NewID("task")
				parent := api.NewID("task")
				commit(t, f, func(tx runtime.Tx) error {
					if err := tx.Create(ctx, "task.tasks", id, parent, testRecord{Value: "original"}); err != nil {
						return err
					}
					return tx.Bind(ctx, "task.tasks", "source:1", id, "sha256:original")
				})
				commit(t, f, func(tx runtime.Tx) error { return tx.Put(ctx, "task.tasks", id, 1, testRecord{Value: "current"}) })
				var value testRecord
				if rev, err := f.store.Read(ctx, f.scope, "task.tasks", id, 1, &value); err != nil || rev != 1 || value.Value != "original" {
					t.Fatalf("history: %d %+v %v", rev, value, err)
				}
				if rev, err := f.store.Read(ctx, f.scope, "task.tasks", id, 0, &value); err != nil || rev != 2 || value.Value != "current" {
					t.Fatalf("head: %d %+v %v", rev, value, err)
				}
				commit(t, f, func(tx runtime.Tx) error {
					key, err := tx.LookupKey(ctx, "task.tasks", "source:1")
					if err != nil {
						return err
					}
					if key.ObjectID != id || key.Digest != "sha256:original" {
						return fmt.Errorf("original business key changed")
					}
					return tx.Bind(ctx, "task.tasks", "source:1", id, "sha256:original")
				})
				status, err := f.store.Within(ctx, f.scope, []string{"task"}, func(tx runtime.Tx) error { return tx.Bind(ctx, "task.tasks", "source:1", id, "sha256:changed") })
				if status != runtime.RolledBack || !api.IsCode(err, "idempotency_conflict") {
					t.Fatalf("semantic input changed: %s %v", status, err)
				}
				status, err = f.store.Within(ctx, f.scope, []string{"memory"}, func(tx runtime.Tx) error { return tx.Put(ctx, "task.tasks", id, 2, testRecord{Value: "forbidden"}) })
				if status != runtime.RolledBack || !api.IsCode(err, "forbidden") {
					t.Fatalf("participant isolation: %s %v", status, err)
				}
				for _, scope := range []runtime.Scope{{TenantID: api.NewID("tenant"), OwnerID: f.scope.OwnerID, DatabaseID: f.scope.DatabaseID}, {TenantID: f.scope.TenantID, OwnerID: api.NewID("owner"), DatabaseID: f.scope.DatabaseID}} {
					if _, err := f.store.Read(ctx, scope, "task.tasks", id, 0, &value); !errors.Is(err, runtime.ErrNotFound) {
						t.Fatalf("scope leaked: %v", err)
					}
				}
				wrongDB := f.scope
				wrongDB.DatabaseID = api.NewID("database")
				if _, err := f.store.Read(ctx, wrongDB, "task.tasks", id, 0, &value); !api.IsCode(err, "forbidden") {
					t.Fatalf("database scope: %v", err)
				}
				rows, err := f.store.List(ctx, f.scope, "task.tasks", parent, "", 1)
				if err != nil || len(rows) != 1 || rows[0].ID != id || rows[0].Revision != 2 {
					t.Fatalf("relations: %+v %v", rows, err)
				}
				if _, err = f.store.List(ctx, f.scope, "task.tasks", "", "", 0); !api.IsCode(err, "invalid_request") {
					t.Fatalf("unbounded scan: %v", err)
				}
				var escaped runtime.Tx
				commit(t, f, func(tx runtime.Tx) error { escaped = tx; return nil })
				if _, err = escaped.Get(ctx, "task.tasks", id, &value); !api.IsCode(err, "invalid_state") {
					t.Fatalf("escaped transaction remained live: %v", err)
				}
			})

			t.Run("original command is one atomic decision under concurrent delivery", func(t *testing.T) {
				f := fixture(t, backend, nil)
				ctx := context.Background()
				id, target, principal := api.NewID("command"), api.NewID("task"), api.NewID("subject")
				registry := runtime.NewRegistry()
				registry.MustRegister(runtime.Method{Contract: api.MethodContract{Name: "task.set", Owner: "task", Kind: "command", InputSchema: api.SchemaFor[testRecord](), OutputSchema: api.SchemaFor[testRecord]()}, Participants: []string{"task"}, Apply: func(ctx context.Context, tx runtime.Tx, _ runtime.Auth, c api.Command) (runtime.Outcome, error) {
					if err := tx.Create(ctx, "task.tasks", c.TargetID, "", testRecord{Value: "original"}); err != nil {
						return runtime.Outcome{}, err
					}
					now, err := tx.Now(ctx)
					if err != nil {
						return runtime.Outcome{}, err
					}
					if _, err = tx.Raise(ctx, "task.progress", c.TargetID, tx.Scope().Ref(c.TargetID, 1), now); err != nil {
						return runtime.Outcome{}, err
					}
					return runtime.Applied(testRecord{Value: "original"}), nil
				}})
				d := runtime.Dispatcher{Store: f.store, OwnerID: f.scope.OwnerID, Registry: registry}
				auth := runtime.Auth{TenantID: f.scope.TenantID, SubjectID: principal, CredentialGeneration: 1}
				raw := api.Raw(command(f.scope, id, target))
				var wg sync.WaitGroup
				receipts := make(chan api.Receipt, 16)
				failures := make(chan error, 16)
				for i := 0; i < 16; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						receipt, err := d.Command(ctx, auth, raw)
						if err != nil {
							failures <- err
							return
						}
						receipts <- receipt
					}()
				}
				wg.Wait()
				close(receipts)
				close(failures)
				for err := range failures {
					t.Error(err)
				}
				for receipt := range receipts {
					if receipt.Stage != "applied" || receipt.CommandID != id {
						t.Fatalf("different decision: %+v", receipt)
					}
				}
				var value testRecord
				if revision, err := f.store.Read(ctx, f.scope, "task.tasks", target, 0, &value); err != nil || revision != 1 || value.Value != "original" {
					t.Fatalf("duplicate business change: %d %+v %v", revision, value, err)
				}
				original, err := f.store.LookupCommand(ctx, f.scope, id)
				if err != nil || original.PrincipalID != principal || original.Receipt.Stage != "applied" {
					t.Fatalf("durable original: %+v %v", original, err)
				}
				changed := command(f.scope, id, target)
				changed.Payload = api.Raw(testRecord{Value: "changed"})
				if _, err = d.Command(ctx, auth, api.Raw(changed)); !api.IsCode(err, "idempotency_conflict") {
					t.Fatalf("changed input reused original identity: %v", err)
				}
				works, status, err := f.store.Claim(ctx, f.scope, api.NewID("boot"), []string{"task.progress"}, 10, time.Second)
				if err != nil || status != runtime.Committed || len(works) != 1 || works[0].Job.WorkRevision != 1 {
					t.Fatalf("duplicate responsibilities: %+v %s %v", works, status, err)
				}
			})

			t.Run("business savepoint rollback preserves fixed rejection", func(t *testing.T) {
				f := fixture(t, backend, nil)
				ctx := context.Background()
				id, target := api.NewID("command"), api.NewID("task")
				registry := runtime.NewRegistry()
				registry.MustRegister(runtime.Method{Contract: api.MethodContract{Name: "task.set", Owner: "task", Kind: "command", InputSchema: api.SchemaFor[testRecord](), OutputSchema: api.SchemaFor[testRecord]()}, Participants: []string{"task"}, Apply: func(ctx context.Context, tx runtime.Tx, _ runtime.Auth, c api.Command) (runtime.Outcome, error) {
					if err := tx.Create(ctx, "task.tasks", target, "", testRecord{Value: "must roll back"}); err != nil {
						return runtime.Outcome{}, err
					}
					now, err := tx.Now(ctx)
					if err != nil {
						return runtime.Outcome{}, err
					}
					if _, err = tx.Raise(ctx, "task.progress", target, tx.Scope().Ref(target, 1), now); err != nil {
						return runtime.Outcome{}, err
					}
					return runtime.Outcome{}, api.E("invalid_state", "blocked_by_original_control")
				}})
				d := runtime.Dispatcher{Store: f.store, OwnerID: f.scope.OwnerID, Registry: registry}
				auth := runtime.Auth{TenantID: f.scope.TenantID, SubjectID: api.NewID("subject"), CredentialGeneration: 1}
				raw := api.Raw(command(f.scope, id, target))
				for i := 0; i < 2; i++ {
					r, err := d.Command(ctx, auth, raw)
					if err != nil || r.Stage != "rejected" || r.Error.Reason != "blocked_by_original_control" {
						t.Fatalf("rejection: %+v %v", r, err)
					}
				}
				var value testRecord
				if _, err := f.store.Read(ctx, f.scope, "task.tasks", target, 0, &value); !errors.Is(err, runtime.ErrNotFound) {
					t.Fatalf("rejected business escaped: %v", err)
				}
				works, _, err := f.store.Claim(ctx, f.scope, api.NewID("boot"), []string{"task.progress"}, 1, time.Second)
				if err != nil || len(works) != 0 {
					t.Fatalf("rejected work escaped: %+v %v", works, err)
				}
			})

			t.Run("new raise survives old finish and renew keeps observed revision", func(t *testing.T) {
				f := fixture(t, backend, nil)
				ctx := context.Background()
				id := api.NewID("task")
				var job api.Job
				commit(t, f, func(tx runtime.Tx) error {
					if err := tx.Create(ctx, "task.tasks", id, "", testRecord{Value: "v1"}); err != nil {
						return err
					}
					now, err := tx.Now(ctx)
					if err != nil {
						return err
					}
					job, err = tx.Raise(ctx, "task.progress", id, tx.Scope().Ref(id, 1), now)
					return err
				})
				works, _, err := f.store.Claim(ctx, f.scope, api.NewID("boot"), []string{"task.progress"}, 1, time.Second)
				if err != nil || len(works) != 1 {
					t.Fatalf("claim: %+v %v", works, err)
				}
				old := works[0]
				commit(t, f, func(tx runtime.Tx) error {
					now, err := tx.Now(ctx)
					if err != nil {
						return err
					}
					repeat, err := tx.Raise(ctx, "task.progress", id, tx.Scope().Ref(id, 1), now)
					if err != nil {
						return err
					}
					if repeat.WorkRevision != 1 {
						return fmt.Errorf("same source manufactured work")
					}
					if err = tx.Put(ctx, "task.tasks", id, 1, testRecord{Value: "v2"}); err != nil {
						return err
					}
					job, err = tx.Raise(ctx, "task.progress", id, tx.Scope().Ref(id, 2), now)
					return err
				})
				if job.State != "leased" || job.WorkRevision != 2 || job.LeaseEpoch != 1 {
					t.Fatalf("raise lost original claim: %+v", job)
				}
				renewed, status, err := f.store.Renew(ctx, f.scope, old.Claim, 2*time.Second)
				if err != nil || status != runtime.Committed || renewed.ObservedWorkRevision != 1 || renewed.LeaseEpoch != 1 {
					t.Fatalf("renew refreshed observation: %+v %s %v", renewed, status, err)
				}
				old.Claim = renewed
				if err = runtime.Finish(ctx, f.store, f.scope, []string{"task"}, old, runtime.Done(), func(tx runtime.Tx) error {
					return tx.Put(ctx, "task.tasks", id, 2, testRecord{Value: "legitimate old fact"})
				}); err != nil {
					t.Fatal(err)
				}
				works, _, err = f.store.Claim(ctx, f.scope, api.NewID("boot"), []string{"task.progress"}, 1, time.Second)
				if err != nil || len(works) != 1 || works[0].Claim.ObservedWorkRevision != 2 || works[0].Claim.LeaseEpoch != 2 {
					t.Fatalf("old done covered new responsibility: %+v %v", works, err)
				}
				if err = runtime.Finish(ctx, f.store, f.scope, []string{"task"}, works[0], runtime.Done(), func(tx runtime.Tx) error {
					now, err := tx.Now(ctx)
					if err != nil {
						return err
					}
					if err = tx.Put(ctx, "task.tasks", id, 3, testRecord{Value: "v4"}); err != nil {
						return err
					}
					_, err = tx.Raise(ctx, "task.progress", id, tx.Scope().Ref(id, 4), now)
					return err
				}); err != nil {
					t.Fatal(err)
				}
				works, _, err = f.store.Claim(ctx, f.scope, api.NewID("boot"), []string{"task.progress"}, 1, time.Second)
				if err != nil || len(works) != 1 || works[0].Claim.ObservedWorkRevision != 3 {
					t.Fatalf("same transaction raise lost: %+v %v", works, err)
				}
			})

			t.Run("expired worker cannot commit business and newer epoch wins", func(t *testing.T) {
				f := fixture(t, backend, nil)
				ctx := context.Background()
				id := api.NewID("task")
				commit(t, f, func(tx runtime.Tx) error {
					if err := tx.Create(ctx, "task.tasks", id, "", testRecord{Value: "original"}); err != nil {
						return err
					}
					now, err := tx.Now(ctx)
					if err != nil {
						return err
					}
					_, err = tx.Raise(ctx, "task.progress", id, tx.Scope().Ref(id, 1), now)
					return err
				})
				works, _, err := f.store.Claim(ctx, f.scope, api.NewID("boot"), []string{"task.progress"}, 1, 5*time.Millisecond)
				if err != nil || len(works) != 1 {
					t.Fatal(err)
				}
				old := works[0]
				err = runtime.Finish(ctx, f.store, f.scope, []string{"task"}, old, runtime.Done(), func(tx runtime.Tx) error {
					if err := tx.Put(ctx, "task.tasks", id, 1, testRecord{Value: "stale"}); err != nil {
						return err
					}
					time.Sleep(15 * time.Millisecond)
					return nil
				})
				if !errors.Is(err, runtime.ErrClaimLost) {
					t.Fatalf("transaction-start clock extended expired lease: %v", err)
				}
				var value testRecord
				if rev, err := f.store.Read(ctx, f.scope, "task.tasks", id, 0, &value); err != nil || rev != 1 || value.Value != "original" {
					t.Fatalf("stale business committed: %d %+v %v", rev, value, err)
				}
				works, _, err = f.store.Claim(ctx, f.scope, api.NewID("boot"), []string{"task.progress"}, 1, time.Second)
				if err != nil || len(works) != 1 || works[0].Claim.LeaseEpoch != 2 {
					t.Fatalf("replacement: %+v %v", works, err)
				}
				if err = f.store.CheckClaim(ctx, f.scope, old.Claim); !errors.Is(err, runtime.ErrClaimLost) {
					t.Fatalf("old epoch regained authority: %v", err)
				}
			})

			t.Run("hint never reopens completed work", func(t *testing.T) {
				f := fixture(t, backend, nil)
				ctx := context.Background()
				id := api.NewID("task")
				var job api.Job
				commit(t, f, func(tx runtime.Tx) error {
					now, err := tx.Now(ctx)
					if err != nil {
						return err
					}
					job, err = tx.Raise(ctx, "task.progress", id, tx.Scope().Ref(id, 1), now)
					return err
				})
				works, _, err := f.store.Claim(ctx, f.scope, api.NewID("boot"), []string{"task.progress"}, 1, time.Second)
				if err != nil || len(works) != 1 {
					t.Fatal(err)
				}
				if err = runtime.Finish(ctx, f.store, f.scope, []string{"task"}, works[0], runtime.Done(), nil); err != nil {
					t.Fatal(err)
				}
				commit(t, f, func(tx runtime.Tx) error { return tx.Hint(ctx, job.JobID, time.Now().Add(-time.Hour)) })
				works, _, err = f.store.Claim(ctx, f.scope, api.NewID("boot"), []string{"task.progress"}, 1, time.Second)
				if err != nil || len(works) != 0 {
					t.Fatalf("hint reopened done: %+v %v", works, err)
				}
				commit(t, f, func(tx runtime.Tx) error {
					next, err := tx.Raise(ctx, "task.progress", id, tx.Scope().Ref(id, 2), time.Now())
					if err != nil {
						return err
					}
					if next.JobID != job.JobID || next.WorkRevision != 2 || next.State != "ready" {
						return fmt.Errorf("new fact did not reopen same responsibility")
					}
					return nil
				})
			})

			t.Run("bounded parallel claims never share a lease", func(t *testing.T) {
				f := fixture(t, backend, nil)
				ctx := context.Background()
				commit(t, f, func(tx runtime.Tx) error {
					now, err := tx.Now(ctx)
					if err != nil {
						return err
					}
					for i := 0; i < 16; i++ {
						id := api.NewID("task")
						if _, err := tx.Raise(ctx, "task.progress", id, tx.Scope().Ref(id, 1), now); err != nil {
							return err
						}
					}
					return nil
				})
				var wg sync.WaitGroup
				claimed := make(chan runtime.Work, 16)
				failures := make(chan error, 16)
				for i := 0; i < 16; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						works, status, err := f.store.Claim(ctx, f.scope, api.NewID("boot"), []string{"task.progress"}, 1, time.Second)
						if err != nil || status != runtime.Committed || len(works) != 1 {
							failures <- fmt.Errorf("claim batch: %s %d %v", status, len(works), err)
							return
						}
						claimed <- works[0]
					}()
				}
				wg.Wait()
				close(claimed)
				close(failures)
				for err := range failures {
					t.Error(err)
				}
				seen := map[string]bool{}
				for w := range claimed {
					if seen[w.Job.JobID] || w.Claim.LeaseEpoch != 1 || w.Claim.ObservedWorkRevision != 1 {
						t.Fatalf("shared/new lease: %+v", w)
					}
					seen[w.Job.JobID] = true
				}
				if len(seen) != 16 {
					t.Fatalf("claimed %d of 16", len(seen))
				}
			})

			for _, phase := range []string{"before_commit", "after_commit"} {
				t.Run("atomic commit outcome "+phase, func(t *testing.T) {
					var armed atomic.Bool
					failure := errors.New("injected connection loss")
					f := fixture(t, backend, func(p string) error {
						if armed.Load() && p == phase {
							return failure
						}
						return nil
					})
					ctx := context.Background()
					id, target, principal := api.NewID("command"), api.NewID("task"), api.NewID("subject")
					original := stored(f.scope, id, target, principal)
					armed.Store(true)
					status, err := f.store.Within(ctx, f.scope, []string{"task"}, func(tx runtime.Tx) error {
						if _, err := tx.LoadCommand(ctx, id); !errors.Is(err, runtime.ErrNotFound) {
							return fmt.Errorf("unexpected original: %v", err)
						}
						if err := tx.Create(ctx, "task.tasks", target, "", testRecord{Value: "original"}); err != nil {
							return err
						}
						now, err := tx.Now(ctx)
						if err != nil {
							return err
						}
						if _, err = tx.Raise(ctx, "task.progress", target, tx.Scope().Ref(target, 1), now); err != nil {
							return err
						}
						return tx.SaveCommand(ctx, original)
					})
					armed.Store(false)
					if phase == "before_commit" {
						if status != runtime.RolledBack || !errors.Is(err, failure) {
							t.Fatalf("precommit: %s %v", status, err)
						}
					} else {
						if status != runtime.CommitUnknown || !errors.Is(err, runtime.ErrCommitUnknown) {
							t.Fatalf("lost commit ack: %s %v", status, err)
						}
					}
					if err = f.store.Close(); err != nil {
						t.Fatal(err)
					}
					f.store = f.open(nil)
					if f.store.ID() != f.scope.DatabaseID {
						t.Fatal("restart changed original database identity")
					}
					_, commandErr := f.store.LookupCommand(ctx, f.scope, id)
					var value testRecord
					rev, recordErr := f.store.Read(ctx, f.scope, "task.tasks", target, 0, &value)
					works, _, jobErr := f.store.Claim(ctx, f.scope, api.NewID("boot"), []string{"task.progress"}, 10, time.Second)
					if phase == "before_commit" {
						if !errors.Is(commandErr, runtime.ErrNotFound) || !errors.Is(recordErr, runtime.ErrNotFound) || jobErr != nil || len(works) != 0 {
							t.Fatalf("rolled back set was partial: receipt=%v record=%v work=%d error=%v", commandErr, recordErr, len(works), jobErr)
						}
					} else {
						if commandErr != nil || recordErr != nil || rev != 1 || value.Value != "original" || jobErr != nil || len(works) != 1 {
							t.Fatalf("committed set was partial: receipt=%v record=%v/%d work=%d error=%v", commandErr, recordErr, rev, len(works), jobErr)
						}
					}
				})
			}
		})
	}
}
