package task_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// 只在系统边界延迟准确执行输入的准备；接纳、命令去重和窗口均使用真实 SQLite。
// Dispatch 的惰性 fallback 重现旧装配的输入出版发生在签窗之后这一故障。
type preparingReceiver struct {
	*operationReceiver
	prepare     func(context.Context, task.OperationIntent) error
	completedAt time.Time
}

func (p *preparingReceiver) PrepareDispatch(ctx context.Context, _ runtime.Scope, intent task.OperationIntent) error {
	if !p.completedAt.IsZero() {
		return nil
	}
	if p.prepare != nil {
		if err := p.prepare(ctx, intent); err != nil {
			return err
		}
	}
	p.completedAt = time.Now().UTC().Truncate(time.Millisecond)
	return nil
}

func (p *preparingReceiver) Dispatch(ctx context.Context, scope runtime.Scope, intent task.OperationIntent, window api.ControlSnapshot) error {
	if err := p.PrepareDispatch(ctx, scope, intent); err != nil {
		return err
	}
	if _, err := p.dispatch.Lookup(ctx, p.auth, intent.CommandID); err == nil {
		return p.operationReceiver.Dispatch(ctx, scope, intent, window)
	} else if !api.IsCode(err, "not_found") {
		return err
	}
	issued, err := api.ParseTime(window.IssuedAt)
	if err != nil {
		return err
	}
	before, err := api.ParseTime(window.StartBefore)
	if err != nil {
		return err
	}
	if issued.Before(p.completedAt) || !time.Now().Before(before) || before.Sub(issued) > 5*time.Second {
		return api.E("expired", "preparation_exhausted_original_control_window")
	}
	return p.operationReceiver.Dispatch(ctx, scope, intent, window)
}

func setupPreparingReceiver(t *testing.T, receiver *preparingReceiver, gate task.LocalGate) (*harness, *localProofFixture) {
	t.Helper()
	proof := &localProofFixture{}
	h := newHarness(t, task.Ports{})
	s, err := task.New(task.Config{Policies: []task.TaskPolicy{h.policy}, ControlWindow: 5 * time.Second, Participants: []string{"task", "platform"}}, task.Ports{Gate: gate, ActionAuthorization: proof, ControlProof: proof, Execution: receiver})
	if err != nil {
		t.Fatal(err)
	}
	h.service = s
	h.dispatch.Registry = runtime.NewRegistry()
	if err = s.Register(h.dispatch.Registry); err != nil {
		t.Fatal(err)
	}
	proof.install(t, h.scope)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "prepared-invoke.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	registry := runtime.NewRegistry()
	if err = registry.Register(runtime.Method{Contract: api.Contract[dispatchPayload, api.ObjectRef]("receiver.invoke", "execution", "command", false, false), Participants: []string{"receiver"}, Apply: func(ctx context.Context, tx runtime.Tx, _ runtime.Auth, c api.Command) (runtime.Outcome, error) {
		var input dispatchPayload
		if err := api.Decode(c.Payload, &input); err != nil {
			return runtime.Outcome{}, err
		}
		if err := tx.Create(ctx, "receiver.invocations", c.TargetID, "", input); err != nil {
			return runtime.Outcome{}, err
		}
		return runtime.Applied(tx.Scope().Ref(c.TargetID, 1)), nil
	}}); err != nil {
		t.Fatal(err)
	}
	receiver.operationReceiver = &operationReceiver{dispatch: &runtime.Dispatcher{Store: store, OwnerID: h.scope.OwnerID, Registry: registry}, auth: h.trusted()}
	return h, proof
}

func admitPreparedOperation(t *testing.T, h *harness, current api.Task) task.PreparedAction {
	t.Helper()
	prepared := h.prepared(current, "0")
	if _, err := h.service.PrepareDecision(context.Background(), h.store, h.scope, h.trusted(), prepared); err != nil {
		t.Fatal(err)
	}
	action := preparedAction(h, "0", "preparation_preserves_original_action")
	action.SafeRequirementCheck = true
	out, err := h.service.ConsumeProposal(context.Background(), h.store, h.scope, h.trusted(), task.Proposal{DecisionID: prepared.DecisionID, Kind: "act", ReasonRef: current.GoalRef, Actions: []task.PreparedAction{action}}, nil)
	if err != nil || out.Outcome != "adopted" {
		t.Fatalf("admit original operation %+v %v", out, err)
	}
	return action
}

func TestOperationInputPreparationCompletesBeforeFiniteControlWindow(t *testing.T) {
	receiver := &preparingReceiver{prepare: func(ctx context.Context, _ task.OperationIntent) error {
		timer := time.NewTimer(5200 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		}
	}}
	h, _ := setupPreparingReceiver(t, receiver, nil)
	current := h.submit(t)
	action := admitPreparedOperation(t, h, current)
	drainKind(t, h, task.JobDispatchOperation)
	receipt, err := receiver.dispatch.Lookup(context.Background(), receiver.auth, action.CommandID)
	var original api.ObjectRef
	if err == nil {
		err = api.Decode(receipt.Output, &original)
	}
	if err != nil || receipt.Stage != "applied" || original.ObjectID != action.OperationID {
		t.Fatalf("prepared input did not reach original operation %+v %v", receipt, err)
	}
	facts, err := h.service.ContextFacts(context.Background(), h.store, h.scope, h.auth, current.TaskID)
	if err != nil || len(facts.Operations) != 1 || facts.Operations[0].Intent.OperationID != action.OperationID || facts.Operations[0].Intent.CommandID != action.CommandID {
		t.Fatalf("preparation changed original action responsibility %+v %v", facts, err)
	}
}

func TestTaskCancellationDuringInputPreparationPreventsOriginalDispatch(t *testing.T) {
	receiver := &preparingReceiver{}
	h, _ := setupPreparingReceiver(t, receiver, nil)
	current := h.submit(t)
	action := admitPreparedOperation(t, h, current)
	receiver.prepare = func(ctx context.Context, _ task.OperationIntent) error {
		latest, err := h.service.Read(ctx, h.store, h.scope, h.auth, current.TaskID)
		if err != nil {
			return err
		}
		r, err := h.dispatch.Command(ctx, h.auth, api.Raw(h.command("task.cancel", current.TaskID, &latest.Revision, task.ControlInput{TaskID: current.TaskID, Reason: "cancel during input publication"})))
		if err != nil {
			return err
		}
		if r.Error != nil {
			return r.Error
		}
		return nil
	}
	drainKind(t, h, task.JobDispatchOperation)
	if _, err := receiver.dispatch.Lookup(context.Background(), receiver.auth, action.CommandID); !api.IsCode(err, "not_found") {
		t.Fatalf("cancellation during preparation still invoked original command: %v", err)
	}
	latest, err := h.service.Read(context.Background(), h.store, h.scope, h.auth, current.TaskID)
	if err != nil || latest.Status != "cancelled" {
		t.Fatalf("preparation overwrote original cancellation %+v %v", latest, err)
	}
}

func TestCredentialRevocationDuringInputPreparationPreventsOriginalDispatch(t *testing.T) {
	receiver := &preparingReceiver{}
	h, _ := setupPreparingReceiver(t, receiver, identityBoundary{})
	h.auth.Roles = []string{"browser"}
	identity := &platform.DevIdentity{Store: h.store, OwnerID: h.scope.OwnerID, SessionTTL: time.Hour, Principals: []platform.Principal{{Auth: h.auth, TokenHash: api.Hash([]byte("preparation-test-credential"))}}}
	if err := identity.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	current := h.submit(t)
	action := admitPreparedOperation(t, h, current)
	receiver.prepare = func(ctx context.Context, _ task.OperationIntent) error {
		return identity.Revoke(ctx, h.auth)
	}
	drainKind(t, h, task.JobDispatchOperation)
	if _, err := receiver.dispatch.Lookup(context.Background(), receiver.auth, action.CommandID); !api.IsCode(err, "not_found") {
		t.Fatalf("revocation during preparation still invoked original command: %v", err)
	}
	facts, err := h.service.ContextFacts(context.Background(), h.store, h.scope, h.auth, current.TaskID)
	if err != nil || len(facts.Operations) != 1 || !facts.Operations[0].Fact.Closed {
		t.Fatalf("known unsent revoked operation retained an unknown effect %+v %v", facts, err)
	}
}

func TestPreparationPublicationLostReplyResumesOriginalDispatchResponsibility(t *testing.T) {
	receiver := &preparingReceiver{}
	h, _ := setupPreparingReceiver(t, receiver, nil)
	content := &contentBridge{}
	configureContent(t, h, content)
	publisher := &replyLostPublisher{contentBridge: content, loseReply: true}
	var preparedRef api.ContentRef
	receiver.prepare = func(ctx context.Context, intent task.OperationIntent) error {
		ref, err := publisher.Publish(ctx, h.scope, derivedID("execution_input", intent.OperationID), "application/json", api.Raw(intent))
		if err == nil {
			preparedRef = ref
		}
		return err
	}
	current := h.submit(t)
	action := admitPreparedOperation(t, h, current)
	drainKind(t, h, task.JobDispatchOperation)
	if _, err := receiver.dispatch.Lookup(context.Background(), receiver.auth, action.CommandID); !api.IsCode(err, "not_found") {
		t.Fatalf("unknown preparation sent an execution command: %v", err)
	}
	originalRef := publisher.published
	if !api.ValidID(originalRef.ContentID) {
		t.Fatal("fault did not follow actual Content publication")
	}
	before, err := h.service.BudgetRead(context.Background(), h.store, h.scope, h.auth, current.TaskID)
	if err != nil || len(before.Reservations) != 2 {
		t.Fatalf("unknown preparation changed admitted reservations %+v %v", before, err)
	}
	time.Sleep(1050 * time.Millisecond)
	drainKind(t, h, task.JobDispatchOperation)
	if !api.Equal(preparedRef, originalRef) {
		t.Fatalf("prepare replay published another input identity %+v %+v", preparedRef, originalRef)
	}
	receipt, err := receiver.dispatch.Lookup(context.Background(), receiver.auth, action.CommandID)
	if err != nil || receipt.Stage != "applied" {
		t.Fatalf("original execution responsibility did not recover %+v %v", receipt, err)
	}
	after, err := h.service.BudgetRead(context.Background(), h.store, h.scope, h.auth, current.TaskID)
	if err != nil || !api.Equal(before.Reservations, after.Reservations) || !api.Equal(before.Budget, after.Budget) {
		t.Fatalf("prepare replay created new budget responsibility %+v %v", after, err)
	}
}

func TestPreparedDispatchLostInvokeReplyNeverRefreshesExpiredOriginalWindow(t *testing.T) {
	receiver := &preparingReceiver{}
	h, _ := setupPreparingReceiver(t, receiver, nil)
	receiver.loseReply = true
	current := h.submit(t)
	action := admitPreparedOperation(t, h, current)
	drainKind(t, h, task.JobDispatchOperation)
	remote := runtime.Scope{TenantID: h.scope.TenantID, OwnerID: h.scope.OwnerID, DatabaseID: receiver.dispatch.Store.ID()}
	var original dispatchPayload
	if _, err := receiver.dispatch.Store.Read(context.Background(), remote, "receiver.invocations", action.OperationID, 1, &original); err != nil {
		t.Fatal(err)
	}
	before, err := api.ParseTime(original.Window.StartBefore)
	if err != nil {
		t.Fatal(err)
	}
	if remaining := time.Until(before) + 20*time.Millisecond; remaining > 0 {
		time.Sleep(remaining)
	}
	drainKind(t, h, task.JobDispatchOperation)
	var recovered dispatchPayload
	if _, err = receiver.dispatch.Store.Read(context.Background(), remote, "receiver.invocations", action.OperationID, 1, &recovered); err != nil || !api.Equal(recovered, original) {
		t.Fatalf("prepared replay changed original invoke or signed window %+v %v", recovered, err)
	}
	receipt, err := receiver.dispatch.Lookup(context.Background(), receiver.auth, action.CommandID)
	if err != nil || receipt.Stage != "applied" {
		t.Fatalf("original invoke receipt lost after preparation replay %+v %v", receipt, err)
	}
}
