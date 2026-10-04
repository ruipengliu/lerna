package development

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// 原真实 Task 已完全关闭。重复查询及原库重开只能复用同一关闭依据和
// 已出版字节，不能把轮询时间当新领域事实产生另一条出版责任。
func TestClosedTaskQueriesKeepOriginalProofAndPublicationAfterReopen(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("requires actual PostgreSQL")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			root := t.TempDir()
			cfg, err := InitializeConfig(ctx, filepath.Join(root, "config.json"), root, driver)
			if err != nil {
				t.Fatal(err)
			}
			cfg.TenantID, cfg.OwnerID, cfg.SubjectID = api.NewID("tenant"), api.NewID("owner"), api.NewID("subject")
			if err = SaveConfig(filepath.Join(root, "config.json"), cfg); err != nil {
				t.Fatal(err)
			}
			a, err := OpenApp(ctx, cfg, true)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := a.Close(); err != nil {
					t.Error(err)
				}
			})
			goal, err := a.Publish(ctx, a.Scope, a.UserAuth, api.NewID("content"), "application/json", api.Raw(brain.GoalSpec{Kind: "answer", Body: "Cancel before any physical action; retain the original closing proof."}), []api.ContentRef{}, []api.ContentRef{})
			if err != nil {
				t.Fatal(err)
			}
			id := api.NewID("task")
			submit := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, CommandID: api.NewID("command"), Method: "task.submit", TargetID: id, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(task.SubmitInput{OrchestratorID: a.Scope.OwnerID, GoalRef: goal, PolicyRef: a.TaskPolicy.PolicyRef, Deadline: api.Time(time.Now().Add(5 * time.Minute)), Budget: []api.Amount{{Unit: "USD", Value: "1"}}})}
			r, err := a.Dispatcher.Command(ctx, a.UserAuth, api.Raw(submit))
			if err != nil || r.Stage != "applied" || r.Error != nil {
				t.Fatalf("public original submit: %+v %v", r, err)
			}
			current, err := a.Task.Read(ctx, a.Store, a.Scope, a.UserAuth, id)
			if err != nil {
				t.Fatal(err)
			}
			revision := current.Revision
			command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, CommandID: api.NewID("command"), Method: "task.cancel", TargetID: id, ExpectedRevision: &revision, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(task.ControlInput{TaskID: id, Reason: "close before a decision is prepared"})}
			r, err = a.Dispatcher.Command(ctx, a.UserAuth, api.Raw(command))
			if err != nil || r.Stage != "applied" || r.Error != nil {
				t.Fatalf("public cancellation: %+v %v", r, err)
			}
			current, err = a.Task.Read(ctx, a.Store, a.Scope, a.UserAuth, id)
			if err != nil || current.Status != "cancelled" || current.AccountingOpen {
				t.Fatalf("actual closed Task: %+v %v", current, err)
			}
			ref := a.Scope.Ref(id, current.Revision)
			original, err := a.Task.Closure(ctx, a.Store, a.Scope, a.UserAuth, ref)
			if err != nil || !original.GoalWorkClosed || !original.EffectsClosed || original.AccountingOpen {
				t.Fatalf("public original closure: %+v %v", original, err)
			}
			clock := &closureQueryClockStore{Store: a.Store}
			clock.offset.Store(int64(2 * time.Second))
			second, err := a.Task.Closure(ctx, clock, a.Scope, a.UserAuth, ref)
			if err != nil {
				t.Fatal(err)
			}
			third, err := a.Task.Closure(ctx, clock, a.Scope, a.UserAuth, ref)
			if err != nil {
				t.Fatal(err)
			}
			// 受信事务时钟只推进两秒。真实领取时钟不替换，因此等到该
			// 时点后再观察所有已到期责任，避免漏计错误实现新增的未来 Job。
			timer := time.NewTimer(3 * time.Second)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				t.Fatal(ctx.Err())
			}
			works, status, err := a.Store.Claim(ctx, a.Scope, api.NewID("worker"), []string{proofJob}, 8, 30*time.Second)
			if err != nil || status != runtime.Committed {
				t.Fatalf("public original Proof Job Claim: %v %v", status, err)
			}
			t.Logf("CLOSED_TASK_QUERY task=%s revision=%d original=%s second=%s third=%s publication_jobs=%d", id, current.Revision, original.ProofRef.ContentID, second.ProofRef.ContentID, third.ProofRef.ContentID, len(works))
			if !api.Equal(original, second) || !api.Equal(original, third) || len(works) != 1 || works[0].Job.SourceRef.ObjectID != original.ProofRef.ContentID {
				t.Fatalf("unchanged closing snapshot minted another proof or publication responsibility: original=%s second=%s third=%s jobs=%d", original.ProofRef.ContentID, second.ProofRef.ContentID, third.ProofRef.ContentID, len(works))
			}
			handler, ok := a.Registry.Job(proofJob)
			if !ok {
				t.Fatal("actual original Proof handler unavailable")
			}
			if err = handler(ctx, a.Store, a.Scope, works[0]); err != nil {
				t.Fatal(err)
			}
			body, err := a.ReadContent(ctx, a.Scope, a.ServiceAuth, original.ProofRef, "content.read")
			if err != nil || uint64(len(body)) != original.ProofRef.ByteLength || api.Hash(body) != original.ProofRef.Hash {
				t.Fatalf("actual original signed bytes: %v", err)
			}
			if err = a.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := OpenApp(ctx, cfg, false)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := reopened.Close(); err != nil {
					t.Error(err)
				}
			})
			clock = &closureQueryClockStore{Store: reopened.Store}
			clock.offset.Store(int64(4 * time.Second))
			after, err := reopened.Task.Closure(ctx, clock, reopened.Scope, reopened.UserAuth, ref)
			if err != nil || !api.Equal(original, after) {
				t.Fatalf("original closure changed after original DB reopen: original=%s after=%s %v", original.ProofRef.ContentID, after.ProofRef.ContentID, err)
			}
			readback, err := reopened.ReadContent(ctx, reopened.Scope, reopened.ServiceAuth, original.ProofRef, "content.read")
			if err != nil || string(readback) != string(body) {
				t.Fatalf("original signed bytes changed after reopen: %v", err)
			}
			remaining, status, err := reopened.Store.Claim(ctx, reopened.Scope, api.NewID("worker"), []string{proofJob}, 8, 30*time.Second)
			if err != nil || status != runtime.Committed || len(remaining) != 0 {
				t.Fatalf("repeated query reopened original completed responsibility: jobs=%d %v %v", len(remaining), status, err)
			}
			unchanged, err := reopened.Task.Read(ctx, reopened.Store, reopened.Scope, reopened.UserAuth, id)
			if err != nil || !api.Equal(current, unchanged) {
				t.Fatalf("query mutated terminal Task: %v", err)
			}
			t.Logf("CLOSED_TASK_QUERY_REOPEN task=%s proof=%s original_job=%s same_issued_at=true same_bytes=true publication_jobs=0 actual_close_reopen=true", id, original.ProofRef.ContentID, works[0].Job.JobID)
		})
	}
}

// 可信时钟是唯一替换；原 scope/参与者/事务和 Savepoint 完整透传。
type closureQueryClockStore struct {
	runtime.Store
	offset atomic.Int64
}

func (s *closureQueryClockStore) Within(ctx context.Context, scope runtime.Scope, parts []string, fn func(runtime.Tx) error) (runtime.CommitStatus, error) {
	return s.Store.Within(ctx, scope, parts, func(tx runtime.Tx) error { return fn(closureQueryClockTx{Tx: tx, store: s}) })
}

type closureQueryClockTx struct {
	runtime.Tx
	store *closureQueryClockStore
}

func (tx closureQueryClockTx) Now(ctx context.Context) (time.Time, error) {
	now, err := tx.Tx.Now(ctx)
	return now.Add(time.Duration(tx.store.offset.Load())), err
}
func (tx closureQueryClockTx) Savepoint(ctx context.Context, fn func(runtime.Tx) error) error {
	return tx.Tx.Savepoint(ctx, func(inner runtime.Tx) error { return fn(closureQueryClockTx{Tx: inner, store: tx.store}) })
}
