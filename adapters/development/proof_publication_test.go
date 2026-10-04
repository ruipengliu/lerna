package development

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// 真实公开关闭证明在原上传计划提交后才到期。明确未接纳的原 reserve
// 可以固定发布失败；Job 完成不得伪造 Content 或把失败证明标为 Published。
func TestExpiredUnacceptedClosureProofRecordsFailureAndFinishesOriginalJob(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("requires actual PostgreSQL")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			root := t.TempDir()
			if base := os.Getenv("HARNESS_TEST_PROOF_FIXTURE_ROOT"); base != "" {
				if !filepath.IsAbs(base) {
					t.Fatal("explicit proof evidence root must be absolute")
				}
				var err error
				root, err = os.MkdirTemp(base, "proof-publication-")
				if err != nil {
					t.Fatal(err)
				}
			}
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
			goal, err := a.Publish(ctx, a.Scope, a.UserAuth, api.NewID("content"), "application/json", api.Raw(brain.GoalSpec{Kind: "answer", Body: "Cancel this original Task before any decision or physical action."}), []api.ContentRef{}, []api.ContentRef{})
			if err != nil {
				t.Fatal(err)
			}
			id := api.NewID("task")
			submit := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, CommandID: api.NewID("command"), Method: "task.submit", TargetID: id, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(task.SubmitInput{OrchestratorID: a.Scope.OwnerID, GoalRef: goal, PolicyRef: a.TaskPolicy.PolicyRef, Deadline: api.Time(time.Now().Add(5 * time.Minute)), Budget: []api.Amount{{Unit: "USD", Value: "1"}}})}
			submitReceipt, err := a.Dispatcher.Command(ctx, a.UserAuth, api.Raw(submit))
			if err != nil || submitReceipt.Error != nil || submitReceipt.Stage != "applied" {
				t.Fatalf("original submit: %+v %v", submitReceipt, err)
			}
			current, err := a.Task.Read(ctx, a.Store, a.Scope, a.UserAuth, id)
			if err != nil {
				t.Fatal(err)
			}
			revision := current.Revision
			control := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, CommandID: api.NewID("command"), Method: "task.cancel", TargetID: id, ExpectedRevision: &revision, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(task.ControlInput{TaskID: id, Reason: "retain original cancellation and the separate proof publication responsibility"})}
			controlReceipt, err := a.Dispatcher.Command(ctx, a.UserAuth, api.Raw(control))
			if err != nil || controlReceipt.Error != nil || controlReceipt.Stage != "applied" {
				t.Fatalf("original cancellation: %+v %v", controlReceipt, err)
			}
			current, err = a.Task.Read(ctx, a.Store, a.Scope, a.UserAuth, id)
			if err != nil || current.Status != "cancelled" || current.AccountingOpen {
				t.Fatalf("original cancelled Task: %+v %v", current, err)
			}
			closure, err := a.Task.Closure(ctx, a.Store, a.Scope, a.UserAuth, a.Scope.Ref(id, current.Revision))
			if err != nil || !closure.GoalWorkClosed || !closure.EffectsClosed || closure.AccountingOpen {
				t.Fatalf("actual original closure: %+v %v", closure, err)
			}
			proofID := closure.ProofRef.ContentID
			base := a.Store
			bindings, ok := base.(runtime.QueryBindingStore)
			if !ok {
				t.Fatal("original query binding capability unavailable")
			}
			clock := &proofPlanExpiryStore{Store: base, QueryBindingStore: bindings, proofID: proofID}
			a.Store, a.Memory.Store, a.Dispatcher.Store = clock, clock, clock
			objects := &proofWriteObserver{ObjectStore: a.Memory.Objects}
			a.Memory.Objects = objects
			works, status, err := a.Store.Claim(ctx, a.Scope, api.NewID("worker"), []string{proofJob}, 1, 30*time.Second)
			if err != nil || status != runtime.Committed || len(works) != 1 || works[0].Job.SourceRef.ObjectID != proofID {
				t.Fatalf("original proof Claim: %+v %v", works, err)
			}
			handler, ok := a.Registry.Job(proofJob)
			if !ok {
				t.Fatal("original proof handler unavailable")
			}
			handlerErr := handler(ctx, a.Store, a.Scope, works[0])
			var plan publicationPlan
			if _, err = base.Read(ctx, a.Scope, "platform.publications", proofID, 1, &plan); err != nil || plan.Ref != closure.ProofRef || plan.SubjectID != a.ServiceAuth.SubjectID {
				t.Fatalf("immutable original publication: %+v %v", plan, err)
			}
			reserve, err := base.LookupCommand(ctx, a.Scope, plan.ReserveID)
			if err != nil || reserve.Receipt.Stage != "rejected" || reserve.Receipt.Error == nil || reserve.Receipt.Error.Code != "expired" || reserve.Receipt.Error.Reason != "command_expired" || reserve.Command.ExpiresAt != plan.Deadline || reserve.Command.TargetID != proofID || reserve.PrincipalID != plan.SubjectID {
				t.Fatalf("actual durable original expired reserve: %+v %v", reserve.Receipt, err)
			}
			if _, err = base.LookupCommand(ctx, a.Scope, plan.PutID); !api.IsCode(err, "not_found") {
				t.Fatalf("original PUT must never have been admitted: %v", err)
			}
			if _, err = a.Memory.LookupTransfer(ctx, a.Scope, a.ServiceAuth, plan.TransferID); !api.IsCode(err, "not_found") {
				t.Fatalf("original transfer must never have been admitted: %v", err)
			}
			if _, err = a.ReadContent(ctx, a.Scope, a.ServiceAuth, closure.ProofRef, "task.closure"); !api.IsCode(err, "not_found") {
				t.Fatalf("unpublished proof must not become readable: %v", err)
			}
			location, err := a.Objects.Locate(ctx, closure.ProofRef)
			var nativeMissing *api.Error
			if !errors.As(err, &nativeMissing) || nativeMissing.Code != "gone" || nativeMissing.Reason != "content_bytes_missing" || location.Key == "" {
				t.Fatalf("original object store must report definite byte absence: %v", err)
			}
			if _, err = os.Stat(filepath.Join(cfg.DataRoot, "objects", location.Key)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("independent native proof file absence: %v", err)
			}
			if clock.offset.Load() != int64(31*time.Minute) || objects.writes.Load() != 0 {
				t.Fatalf("real expiry/zero-byte-write premise: offset=%s writes=%d", time.Duration(clock.offset.Load()), objects.writes.Load())
			}
			t.Logf("ORIGINAL_PROOF_EXPIRY task=%s proof=%s job=%s reserve=%s receipt=%s/%s put=%s transfer=%s handler=%v writes=0", id, proofID, works[0].Job.JobID, plan.ReserveID, reserve.Receipt.Stage, reserve.Receipt.Error.Reason, plan.PutID, plan.TransferID, handlerErr)
			if handlerErr != nil {
				t.Fatalf("known original never-admitted proof failure must finish its responsibility: %v", handlerErr)
			}
			var saved proofPublicationAudit
			if _, err = base.Read(ctx, a.Scope, "platform.proofs", proofID, 0, &saved); err != nil || saved.Published || saved.Ref != closure.ProofRef || saved.PublicationFailure == nil || !api.Equal(saved.PublicationFailure.Plan, plan) || !api.Equal(saved.PublicationFailure.ReserveReceipt, reserve.Receipt) || saved.PublicationFailure.RecordedAt == "" {
				t.Fatalf("explicit original publication failure missing or falsely published: ref=%s published=%v error=%v", saved.Ref.ContentID, saved.Published, err)
			}
			if clock.finished.Load() != 1 || !errors.Is(base.CheckClaim(ctx, a.Scope, works[0].Claim), runtime.ErrClaimLost) {
				t.Fatal("original proof Job was not actually finished Done")
			}
			if err = a.Close(); err != nil {
				t.Fatal(err)
			}
			// 重开是独立观察阶段；不刷新原 Task、命令、证明或 Claim 的任何业务期限。
			cancel()
			ctx, reopenCancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer reopenCancel()
			reopenStarted := time.Now()
			reopened, err := OpenApp(ctx, cfg, false)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := reopened.Close(); err != nil {
					t.Error(err)
				}
			})
			t.Logf("original_reopen_observer elapsed=%s", time.Since(reopenStarted))
			recovered, err := reopened.Task.Read(ctx, reopened.Store, reopened.Scope, reopened.UserAuth, id)
			if err != nil || !api.Equal(recovered, current) {
				t.Fatalf("proof failure changed original terminal Task: %v", err)
			}
			after, err := reopened.Store.LookupCommand(ctx, reopened.Scope, plan.ReserveID)
			if err != nil || !api.Equal(after, reserve) {
				t.Fatalf("original expired receipt changed after reopen: %v", err)
			}
			for _, original := range []api.Command{submit, control} {
				receipt, err := reopened.Dispatcher.Command(ctx, reopened.UserAuth, api.Raw(original))
				want := submitReceipt
				if original.CommandID == control.CommandID {
					want = controlReceipt
				}
				if err != nil || !api.Equal(receipt, want) {
					t.Fatalf("original Task receipt changed: %v", err)
				}
			}
			remaining, status, err := reopened.Store.Claim(ctx, reopened.Scope, api.NewID("worker"), []string{proofJob}, 1, 30*time.Second)
			if err != nil || status != runtime.Committed || len(remaining) != 0 {
				t.Fatalf("failed proof created a repeated publication responsibility: %+v %v", remaining, err)
			}
			var durable proofPublicationAudit
			if _, err = reopened.Store.Read(ctx, reopened.Scope, "platform.proofs", proofID, 0, &durable); err != nil || !api.Equal(saved, durable) {
				t.Fatalf("durable failed proof changed after reopen: %v", err)
			}
			t.Logf("ORIGINAL_PROOF_FAILURE_DONE task=%s proof=%s original_job=%s original_reserve=%s published=false same_receipt=true reopen=true", id, proofID, works[0].Job.JobID, plan.ReserveID)
		})
	}
}

// 只读取本 adapter 的发布审计字段，不注入证明、账本或 Job。
type proofPublicationAudit struct {
	sealedProof
	PublicationFailure *struct {
		Plan           publicationPlan `json:"plan"`
		ReserveReceipt api.Receipt     `json:"reserve_receipt"`
		RecordedAt     string          `json:"recorded_at"`
	} `json:"publication_failure,omitempty"`
}

type proofWriteObserver struct {
	memory.ObjectStore
	writes atomic.Int32
}

func (o *proofWriteObserver) Write(ctx context.Context, ref api.ContentRef, body io.Reader) (memory.ObjectLocation, error) {
	o.writes.Add(1)
	return o.ObjectStore.Write(ctx, ref, body)
}

// 仅替换可信事务时钟；实际 SQL/接纳/Claim/Job 与业务事实仍透传。
// 时间在原 plan 的真实提交之后移动，不改 plan/命令/Task 的既有期限。
type proofPlanExpiryStore struct {
	runtime.Store
	runtime.QueryBindingStore
	proofID  string
	offset   atomic.Int64
	finished atomic.Int32
}

func (s *proofPlanExpiryStore) Within(ctx context.Context, scope runtime.Scope, parts []string, fn func(runtime.Tx) error) (runtime.CommitStatus, error) {
	created := false
	status, err := s.Store.Within(ctx, scope, parts, func(tx runtime.Tx) error {
		return fn(proofPlanExpiryTx{Tx: tx, store: s, planCreated: &created})
	})
	if status == runtime.Committed && err == nil && created {
		s.offset.Store(int64(31 * time.Minute))
	}
	return status, err
}

type proofPlanExpiryTx struct {
	runtime.Tx
	store       *proofPlanExpiryStore
	planCreated *bool
}

func (tx proofPlanExpiryTx) Now(ctx context.Context) (time.Time, error) {
	now, err := tx.Tx.Now(ctx)
	return now.Add(time.Duration(tx.store.offset.Load())), err
}

func (tx proofPlanExpiryTx) Create(ctx context.Context, namespace, id, parent string, value any) error {
	err := tx.Tx.Create(ctx, namespace, id, parent, value)
	if err == nil && namespace == "platform.publications" && id == tx.store.proofID {
		*tx.planCreated = true
	}
	return err
}

func (tx proofPlanExpiryTx) Savepoint(ctx context.Context, fn func(runtime.Tx) error) error {
	return tx.Tx.Savepoint(ctx, func(inner runtime.Tx) error {
		return fn(proofPlanExpiryTx{Tx: inner, store: tx.store, planCreated: tx.planCreated})
	})
}

func (tx proofPlanExpiryTx) Finish(ctx context.Context, claim api.Claim, d runtime.Disposition) error {
	err := tx.Tx.Finish(ctx, claim, d)
	if err == nil && d.State == "done" {
		tx.store.finished.Add(1)
	}
	return err
}
