package runtime_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// Public Drain 与实际 JobStore 是验收入口；等待跨越原30秒领取期限，
// 没有改变数据库时钟、租期或领域期限，也没有用 mock 代替持久责任。
func TestDrainKeepsOriginalClaimAcrossLongIOAndReopensSameResult(t *testing.T) {
	for _, database := range []string{"sqlite", "postgres"} {
		t.Run(database, func(t *testing.T) {
			if database == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("requires actual PostgreSQL")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
			defer cancel()
			directory := t.TempDir()
			open := func() (runtime.Store, error) {
				if database == "postgres" {
					return postgres.Open(ctx, os.Getenv("HARNESS_TEST_POSTGRES_DSN"))
				}
				return sqlite.Open(filepath.Join(directory, "original.sqlite"))
			}
			store, err := open()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := store.Close(); err != nil {
					t.Error(err)
				}
			})
			if err = store.(interface{ Migrate(context.Context) error }).Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			scope := runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner"), DatabaseID: store.ID()}
			source := scope.Ref(api.NewID("source"), 1)
			var originalJob api.Job
			status, err := store.Within(ctx, scope, []string{"fixture"}, func(tx runtime.Tx) error {
				now, err := tx.Now(ctx)
				if err != nil {
					return err
				}
				originalJob, err = tx.Raise(ctx, "fixture.drain_io", "original", source, now)
				return err
			})
			if err != nil || status != runtime.Committed {
				t.Fatalf("original responsibility: %s %v", status, err)
			}
			registry := runtime.NewRegistry()
			entered := atomic.Int32{}
			started := make(chan runtime.Work, 1)
			exit := make(chan struct{})
			var release atomic.Bool
			releaseIO := func() {
				if release.CompareAndSwap(false, true) {
					close(exit)
				}
			}
			defer releaseIO()
			type result struct {
				JobID                string        `json:"job_id"`
				SourceRef            api.ObjectRef `json:"source_ref"`
				HolderID             string        `json:"holder_id"`
				LeaseEpoch           uint64        `json:"lease_epoch"`
				ObservedWorkRevision uint64        `json:"observed_work_revision"`
			}
			target := filepath.Join(directory, "original-result.txt")
			if err = registry.RegisterJob("fixture.drain_io", func(ctx context.Context, current runtime.Store, currentScope runtime.Scope, work runtime.Work) error {
				entered.Add(1)
				started <- work
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-exit:
				}
				// 原当前资格仍是物理入口与最终事务的必要条件。
				if err := current.CheckClaim(ctx, currentScope, work.Claim); err != nil {
					return err
				}
				if err := os.WriteFile(target, []byte("one original bounded result\n"), 0600); err != nil {
					return err
				}
				return runtime.Finish(ctx, current, currentScope, []string{"fixture"}, work, runtime.Done(), func(tx runtime.Tx) error {
					return tx.Create(ctx, "fixture.results", source.ObjectID, "", result{work.Job.JobID, work.Job.SourceRef, work.Claim.HolderID, work.Claim.LeaseEpoch, work.Claim.ObservedWorkRevision})
				})
			}); err != nil {
				t.Fatal(err)
			}
			drainDone := make(chan error, 1)
			go func() { drainDone <- runtime.Drain(ctx, store, scope, registry, 2) }()
			joined := false
			defer func() {
				cancel()
				releaseIO()
				if !joined {
					select {
					case <-drainDone:
					case <-time.After(5 * time.Second):
						t.Error("original Drain did not join its actual handler")
					}
				}
			}()
			var original runtime.Work
			select {
			case original = <-started:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			t.Logf("original Drain scope=%s/%s job=%s holder=%s epoch=%d observed=%d lease_until=%s", scope.TenantID, scope.OwnerID, original.Claim.JobID, original.Claim.HolderID, original.Claim.LeaseEpoch, original.Claim.ObservedWorkRevision, original.Claim.LeaseUntil)
			until, err := api.ParseTime(original.Claim.LeaseUntil)
			if err != nil {
				t.Fatal(err)
			}
			timer := time.NewTimer(time.Until(until) + 1500*time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				t.Fatal(ctx.Err())
			case <-timer.C:
			}
			// 第二holder通过真实领取入口不能接替仍活跃的原处理器。
			competitors, claimStatus, claimErr := store.Claim(ctx, scope, api.NewID("boot"), registry.JobKinds(), 1, 30*time.Second)
			releaseIO()
			select {
			case err = <-drainDone:
				joined = true
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if err != nil {
				t.Fatalf("original long handler could not finish: claim_lost=%t error=%v", errors.Is(err, runtime.ErrClaimLost), err)
			}
			if claimErr != nil || claimStatus != runtime.Committed || len(competitors) != 0 {
				t.Fatalf("another holder replaced active original work: count=%d status=%s error=%v", len(competitors), claimStatus, claimErr)
			}
			if entered.Load() != 1 || original.Job.JobID != originalJob.JobID || original.Claim.ObservedWorkRevision != originalJob.WorkRevision {
				t.Fatal("Drain replaced original job identity or invoked the handler twice")
			}
			bytes, err := os.ReadFile(target)
			if err != nil || string(bytes) != "one original bounded result\n" {
				t.Fatalf("independent actual target result: %q %v", bytes, err)
			}
			if err = store.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := open()
			if err != nil {
				t.Fatal(err)
			}
			store = reopened
			if store.ID() != scope.DatabaseID {
				t.Fatal("reopen replaced original database")
			}
			var persisted result
			if _, err = store.Read(ctx, scope, "fixture.results", source.ObjectID, 1, &persisted); err != nil {
				t.Fatal(err)
			}
			if persisted.JobID != originalJob.JobID || !api.Equal(persisted.SourceRef, source) || persisted.HolderID != original.Claim.HolderID || persisted.LeaseEpoch != original.Claim.LeaseEpoch || persisted.ObservedWorkRevision != original.Claim.ObservedWorkRevision {
				t.Fatalf("reopen changed original ownership/result: %+v", persisted)
			}
			before := entered.Load()
			if err = runtime.Drain(ctx, store, scope, registry, 2); err != nil || entered.Load() != before {
				t.Fatalf("reopen restarted completed original work: entries=%d error=%v", entered.Load(), err)
			}
		})
	}
}
