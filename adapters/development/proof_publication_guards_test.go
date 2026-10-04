package development

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// 所有前态由原公开 Task/Closure/上传方法和实际介质产生；故障仅在准确
// Locate、原 Claim 交接或真实提交答复边界，不注入证明/回执/Job 私有记录。
func TestProofPublicationRetainsUnknownNativeClaimAndOriginalUploadResponsibility(t *testing.T) {
	for _, scenario := range []string{"native_exists", "native_read_unknown", "stale_claim", "finish_reply_unknown", "accepted_reserve", "original_put_applied"} {
		t.Run(scenario, func(t *testing.T) {
			for _, driver := range []string{"sqlite", "postgres"} {
				t.Run(driver, func(t *testing.T) {
					if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
						t.Skip("requires actual PostgreSQL")
					}
					runProofPublicationGuard(t, driver, scenario)
				})
			}
		})
	}
}

func runProofPublicationGuard(t *testing.T, driver, scenario string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	root := t.TempDir()
	if base := os.Getenv("HARNESS_TEST_PROOF_FIXTURE_ROOT"); base != "" {
		if !filepath.IsAbs(base) {
			t.Fatal("explicit proof evidence root must be absolute")
		}
		var err error
		root, err = os.MkdirTemp(base, "proof-publication-guard-")
		if err != nil {
			t.Fatal(err)
		}
	}
	configPath := filepath.Join(root, "config.json")
	cfg, err := InitializeConfig(ctx, configPath, root, driver)
	if err != nil {
		t.Fatal(err)
	}
	cfg.TenantID, cfg.OwnerID, cfg.SubjectID = api.NewID("tenant"), api.NewID("owner"), api.NewID("subject")
	if err = SaveConfig(configPath, cfg); err != nil {
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
	goal, err := a.Publish(ctx, a.Scope, a.UserAuth, api.NewID("content"), "application/json", api.Raw(brain.GoalSpec{Kind: "answer", Body: "Retain the original cancelled Task and its independent proof publication responsibility."}), []api.ContentRef{}, []api.ContentRef{})
	if err != nil {
		t.Fatal(err)
	}
	id := api.NewID("task")
	command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, CommandID: api.NewID("command"), Method: "task.submit", TargetID: id, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(task.SubmitInput{OrchestratorID: a.Scope.OwnerID, GoalRef: goal, PolicyRef: a.TaskPolicy.PolicyRef, Deadline: api.Time(time.Now().Add(5 * time.Minute)), Budget: []api.Amount{{Unit: "USD", Value: "1"}}})}
	if receipt, err := a.Dispatcher.Command(ctx, a.UserAuth, api.Raw(command)); err != nil || receipt.Stage != "applied" || receipt.Error != nil {
		t.Fatalf("original Task submit: %s %v", receipt.Stage, err)
	}
	current, err := a.Task.Read(ctx, a.Store, a.Scope, a.UserAuth, id)
	if err != nil {
		t.Fatal(err)
	}
	revision := current.Revision
	command.CommandID, command.Method, command.ExpectedRevision = api.NewID("command"), "task.cancel", &revision
	command.Payload = api.Raw(task.ControlInput{TaskID: id, Reason: "exercise only the original proof publication failure boundary"})
	if receipt, err := a.Dispatcher.Command(ctx, a.UserAuth, api.Raw(command)); err != nil || receipt.Stage != "applied" || receipt.Error != nil {
		t.Fatalf("original Task cancel: %s %v", receipt.Stage, err)
	}
	current, err = a.Task.Read(ctx, a.Store, a.Scope, a.UserAuth, id)
	if err != nil || current.Status != "cancelled" || current.AccountingOpen {
		t.Fatalf("original terminal Task: %v", err)
	}
	closure, err := a.Task.Closure(ctx, a.Store, a.Scope, a.UserAuth, a.Scope.Ref(id, current.Revision))
	if err != nil {
		t.Fatal(err)
	}
	base := a.Store
	bindings, ok := base.(runtime.QueryBindingStore)
	if !ok {
		t.Fatal("original QueryBindingStore required")
	}
	clock := &proofGuardStore{Store: base, QueryBindingStore: bindings, proofID: closure.ProofRef.ContentID, scenario: scenario}
	a.Store, a.Memory.Store, a.Dispatcher.Store = clock, clock, clock
	objects := &proofWriteObserver{ObjectStore: a.Memory.Objects}
	native := &proofGuardObjects{ObjectStore: objects, app: a, base: base, scenario: scenario, proof: closure.ProofRef, fault: errors.New("original native proof locate unavailable")}
	a.Memory.Objects = native
	lease := 30 * time.Second
	if scenario == "stale_claim" {
		lease = time.Second
	}
	works, status, err := base.Claim(ctx, a.Scope, api.NewID("worker"), []string{proofJob}, 1, lease)
	if err != nil || status != runtime.Committed || len(works) != 1 || works[0].Job.SourceRef.ObjectID != closure.ProofRef.ContentID {
		t.Fatalf("original proof Claim: %v", err)
	}
	native.work = works[0]
	handler, ok := a.Registry.Job(proofJob)
	if !ok {
		t.Fatal("original proof handler missing")
	}
	handlerErr := handler(ctx, a.Store, a.Scope, works[0])
	var saved sealedProof
	if _, err = base.Read(ctx, a.Scope, "platform.proofs", closure.ProofRef.ContentID, 0, &saved); err != nil || saved.Ref != closure.ProofRef {
		t.Fatalf("original proof metadata changed: %v", err)
	}
	var plan publicationPlan
	if _, err = base.Read(ctx, a.Scope, "platform.publications", saved.Ref.ContentID, 1, &plan); err != nil {
		t.Fatal(err)
	}
	reserve, err := base.LookupCommand(ctx, a.Scope, plan.ReserveID)
	if err != nil || reserve.Command.ExpiresAt != plan.Deadline || reserve.Command.TargetID != saved.Ref.ContentID {
		t.Fatalf("original reserve identity: %v", err)
	}
	finishKnown := scenario == "finish_reply_unknown"
	published := scenario == "original_put_applied"
	if finishKnown {
		if !errors.Is(handlerErr, runtime.ErrCommitUnknown) || !clock.replyLost.Load() || saved.Published || saved.PublicationFailure == nil || !api.Equal(saved.PublicationFailure.ReserveReceipt, reserve.Receipt) {
			t.Fatalf("actual committed failure reply loss was not preserved: %v", handlerErr)
		}
		if err := handler(ctx, a.Store, a.Scope, works[0]); !errors.Is(err, runtime.ErrClaimLost) {
			t.Fatalf("old Claim rewrote the durably failed publication: %v", err)
		}
	} else if published {
		put, err := base.LookupCommand(ctx, a.Scope, plan.PutID)
		if err != nil || put.Receipt.Stage != "applied" || handlerErr != nil || !saved.Published || saved.PublicationFailure != nil || clock.offset.Load() != int64(31*time.Minute) {
			t.Fatalf("old applied PUT must remain published after original expiry: %v / %v", handlerErr, err)
		}
		// 复用原闭合消费方已获准的 content.read，不为测试新增证明用途。
		body, err := a.ReadContent(ctx, a.Scope, a.ServiceAuth, saved.Ref, "content.read")
		if err != nil || api.Hash(body) != saved.Ref.Hash || uint64(len(body)) != saved.Ref.ByteLength {
			t.Fatalf("actually published original proof bytes: %v", err)
		}
	} else {
		if handlerErr == nil || saved.Published || saved.PublicationFailure != nil {
			t.Fatalf("unknown or accepted original upload falsely finished: %v", handlerErr)
		}
		switch scenario {
		case "native_exists":
			if !api.IsCode(handlerErr, "effect_unknown") || !native.nativeCreated.Load() {
				t.Fatalf("actual native orphan lost its original responsibility: %v", handlerErr)
			}
			if _, err := objects.ObjectStore.Locate(ctx, saved.Ref); err != nil {
				t.Fatalf("native original bytes did not actually exist: %v", err)
			}
		case "native_read_unknown":
			if !api.IsCode(handlerErr, "dependency_unavailable") || !errors.Is(handlerErr, native.fault) || !native.located.Load() {
				t.Fatalf("native dependency cause was lost: %v", handlerErr)
			}
		case "stale_claim":
			if !errors.Is(handlerErr, runtime.ErrClaimLost) || native.replacement == nil || native.replacement.Claim.LeaseEpoch <= works[0].Claim.LeaseEpoch || base.CheckClaim(ctx, a.Scope, native.replacement.Claim) != nil {
				t.Fatalf("actual new holder did not exclude old proof commit: %v", handlerErr)
			}
		case "accepted_reserve":
			if reserve.Receipt.Stage != "applied" || !api.IsCode(handlerErr, "expired") {
				t.Fatalf("accepted original transfer was treated as never admitted: %v", handlerErr)
			}
			if _, err := a.Memory.LookupTransfer(ctx, a.Scope, a.ServiceAuth, plan.TransferID); err != nil {
				t.Fatalf("original accepted transfer responsibility missing: %v", err)
			}
		}
		if scenario != "stale_claim" && base.CheckClaim(ctx, a.Scope, works[0].Claim) != nil {
			t.Fatal("unsettled original publication Job was lost")
		}
	}
	expectedWrites := int32(0)
	if published {
		expectedWrites = 1
	}
	if objects.writes.Load() != expectedWrites {
		t.Fatalf("unexpected publication byte writes: got=%d want=%d", objects.writes.Load(), expectedWrites)
	}
	if finishKnown || published {
		if !errors.Is(base.CheckClaim(ctx, a.Scope, works[0].Claim), runtime.ErrClaimLost) {
			t.Fatal("durable original completion did not finish its Job")
		}
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
	var after sealedProof
	if _, err = reopened.Store.Read(ctx, reopened.Scope, "platform.proofs", saved.Ref.ContentID, 0, &after); err != nil || !api.Equal(after, saved) {
		t.Fatalf("original proof responsibility changed after reopen: %v", err)
	}
	afterReserve, err := reopened.Store.LookupCommand(ctx, reopened.Scope, plan.ReserveID)
	if err != nil || !api.Equal(afterReserve, reserve) {
		t.Fatalf("original reserve receipt changed after reopen: %v", err)
	}
	recovered, err := reopened.Task.Read(ctx, reopened.Store, reopened.Scope, reopened.UserAuth, id)
	if err != nil || !api.Equal(recovered, current) {
		t.Fatalf("proof recovery changed original Task: %v", err)
	}
	t.Logf("ORIGINAL_PROOF_GUARD scenario=%s task=%s proof=%s job=%s reserve=%s stage=%s original_put=%s published=%v failure=%v writes=%d reopen=true", scenario, id, saved.Ref.ContentID, works[0].Job.JobID, plan.ReserveID, reserve.Receipt.Stage, plan.PutID, saved.Published, saved.PublicationFailure != nil, objects.writes.Load())
}

type proofGuardStore struct {
	runtime.Store
	runtime.QueryBindingStore
	proofID   string
	scenario  string
	offset    atomic.Int64
	replyLost atomic.Bool
}

type proofGuardCommit struct{ plan, reserve, put, failure bool }

func (s *proofGuardStore) Within(ctx context.Context, scope runtime.Scope, parts []string, fn func(runtime.Tx) error) (runtime.CommitStatus, error) {
	flags := &proofGuardCommit{}
	status, err := s.Store.Within(ctx, scope, parts, func(tx runtime.Tx) error {
		return fn(proofGuardTx{Tx: tx, store: s, flags: flags})
	})
	if status == runtime.Committed && err == nil {
		if flags.plan && s.scenario != "accepted_reserve" && s.scenario != "original_put_applied" || flags.reserve && s.scenario == "accepted_reserve" || flags.put && s.scenario == "original_put_applied" {
			s.offset.Store(int64(31 * time.Minute))
		}
		if flags.failure && s.scenario == "finish_reply_unknown" && !s.replyLost.Swap(true) {
			return runtime.CommitUnknown, runtime.ErrCommitUnknown
		}
	}
	return status, err
}

type proofGuardTx struct {
	runtime.Tx
	store *proofGuardStore
	flags *proofGuardCommit
}

func (tx proofGuardTx) Now(ctx context.Context) (time.Time, error) {
	now, err := tx.Tx.Now(ctx)
	return now.Add(time.Duration(tx.store.offset.Load())), err
}

func (tx proofGuardTx) Create(ctx context.Context, ns, id, parent string, value any) error {
	err := tx.Tx.Create(ctx, ns, id, parent, value)
	if err == nil && ns == "platform.publications" && id == tx.store.proofID {
		tx.flags.plan = true
	}
	return err
}

func (tx proofGuardTx) Put(ctx context.Context, ns, id string, revision uint64, value any) error {
	err := tx.Tx.Put(ctx, ns, id, revision, value)
	if err == nil && ns == "platform.proofs" && id == tx.store.proofID {
		if proof, ok := value.(sealedProof); ok && proof.PublicationFailure != nil {
			tx.flags.failure = true
		}
	}
	return err
}

func (tx proofGuardTx) SaveCommand(ctx context.Context, command runtime.StoredCommand) error {
	err := tx.Tx.SaveCommand(ctx, command)
	if err == nil && command.Command.TargetID == tx.store.proofID && command.Receipt.Stage == "applied" {
		tx.flags.reserve = tx.flags.reserve || command.Command.Method == "content.upload_reserve"
		tx.flags.put = tx.flags.put || command.Command.Method == "content.put"
	}
	return err
}

func (tx proofGuardTx) Savepoint(ctx context.Context, fn func(runtime.Tx) error) error {
	return tx.Tx.Savepoint(ctx, func(inner runtime.Tx) error {
		return fn(proofGuardTx{Tx: inner, store: tx.store, flags: tx.flags})
	})
}

type proofGuardObjects struct {
	memory.ObjectStore
	app           *App
	base          runtime.Store
	scenario      string
	proof         api.ContentRef
	work          runtime.Work
	fault         error
	located       atomic.Bool
	nativeCreated atomic.Bool
	replacement   *runtime.Work
}

func (o *proofGuardObjects) Locate(ctx context.Context, ref api.ContentRef) (memory.ObjectLocation, error) {
	if ref != o.proof || o.located.Swap(true) {
		return o.ObjectStore.Locate(ctx, ref)
	}
	switch o.scenario {
	case "native_exists":
		var original sealedProof
		if _, err := o.base.Read(ctx, o.app.Scope, "platform.proofs", ref.ContentID, 1, &original); err != nil {
			return memory.ObjectLocation{}, err
		}
		// 独立原生前态：实际落下原已签准确字节，缺 Memory 元数据不意味着无介质。
		observer := o.ObjectStore.(*proofWriteObserver)
		if _, err := observer.ObjectStore.Write(ctx, ref, strings.NewReader(original.Compact)); err != nil {
			return memory.ObjectLocation{}, err
		}
		o.nativeCreated.Store(true)
	case "native_read_unknown":
		return memory.ObjectLocation{}, &api.Error{Code: "dependency_unavailable", Reason: "original_native_proof_unavailable", Scope: "content", Retry: "query_original", Cause: o.fault}
	case "stale_claim":
		until, err := api.ParseTime(o.work.Claim.LeaseUntil)
		if err != nil {
			return memory.ObjectLocation{}, err
		}
		timer := time.NewTimer(time.Until(until.Add(10 * time.Millisecond)))
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return memory.ObjectLocation{}, ctx.Err()
		case <-timer.C:
		}
		works, status, err := o.base.Claim(ctx, o.app.Scope, api.NewID("worker"), []string{proofJob}, 1, 30*time.Second)
		if err != nil || status != runtime.Committed || len(works) != 1 || works[0].Job.JobID != o.work.Job.JobID {
			return memory.ObjectLocation{}, errors.Join(api.E("dependency_unavailable", "original_proof_reclaim_unconfirmed"), err)
		}
		o.replacement = &works[0]
	}
	return o.ObjectStore.Locate(ctx, ref)
}
