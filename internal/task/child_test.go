package task_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// 远端Session API探针使用独立SQLite，丢一次答复后仍按原创建命令去重。
type sessionProbe struct {
	store    *sqlite.Store
	loseOnce bool
	last     api.ObjectRef
}

func (p *sessionProbe) CreateSession(ctx context.Context, scope runtime.Scope, h task.ChildHandle) (api.ObjectRef, error) {
	remote := runtime.Scope{TenantID: scope.TenantID, OwnerID: h.SessionOwnerID, DatabaseID: p.store.ID()}
	var out api.ObjectRef
	status, err := p.store.Within(ctx, remote, []string{"remote"}, func(tx runtime.Tx) error {
		var ref api.ObjectRef
		_, e := tx.Get(ctx, "remote.sessions", h.SessionCommandRef.ObjectID, &ref)
		if e == nil {
			out = ref
			return nil
		}
		if !api.IsCode(e, "not_found") {
			return e
		}
		out = remote.Ref(api.NewID("session"), 1)
		return tx.Create(ctx, "remote.sessions", h.SessionCommandRef.ObjectID, h.ChildID, out)
	})
	if err != nil || status != runtime.Committed {
		return out, err
	}
	p.last = out
	if p.loseOnce {
		p.loseOnce = false
		return api.ObjectRef{}, api.E("dependency_unavailable", "reply_lost")
	}
	return out, nil
}
func (*sessionProbe) Create(context.Context, runtime.Scope, task.Delegation, task.Allocation) (task.DelegationFact, error) {
	return task.DelegationFact{}, api.E("dependency_unavailable", "probe_creation_not_run")
}
func (*sessionProbe) Read(context.Context, runtime.Scope, task.Delegation) (task.DelegationFact, error) {
	return task.DelegationFact{}, api.E("dependency_unavailable", "probe_goal_not_run")
}
func (*sessionProbe) Control(context.Context, runtime.Scope, task.Delegation, string) error {
	return nil
}
func (*sessionProbe) CloseAllocation(context.Context, runtime.Scope, task.Allocation) error {
	return nil
}
func (*sessionProbe) ReadAllocation(context.Context, runtime.Scope, api.ObjectRef) (task.Allocation, error) {
	return task.Allocation{}, api.E("dependency_unavailable", "probe_allocation_not_run")
}
func (*sessionProbe) ReadClosure(context.Context, runtime.Scope, api.ObjectRef) (api.AllocationClosure, error) {
	return api.AllocationClosure{}, api.E("dependency_unavailable", "probe_closure_not_run")
}
func (*sessionProbe) Transfer(context.Context, runtime.Scope, task.Transfer) error { return nil }
func newSessionProbe(t *testing.T, lose bool) *sessionProbe {
	t.Helper()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "remote.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return &sessionProbe{store: store, loseOnce: lose}
}
func TestChildSessionLostReplyUsesOriginalSessionAndNewGoalRequiresOldGoalAndEffectsClosure(t *testing.T) {
	probe := newSessionProbe(t, true)
	rule := fixtureRule()
	bridge := &evidenceBridge{rules: map[string]api.RuleDefinition{rule.RuleRef.ComponentID: rule}}
	h := newHarness(t, task.Ports{Collaboration: probe, Gate: bridge}, rule)
	bridge.service = governance.New(h.store, governance.Options{})
	parent := readyTask(t, h, rule)
	childID := api.NewID("child")
	receiver := api.NewID("receiver")
	binding := h.scope.Ref(api.NewID("binding"), 1)
	created := h.command("child.create", childID, nil, task.ChildCreateInput{ChildID: childID, SessionOwnerID: receiver, SessionConfigRef: h.policy.PolicyRef, AgentBindingRef: binding, InstallLockRef: h.policy.PolicyRef, AccessScopeRef: h.content("fixed allowed delegate scope"), PrepareDeadline: api.Time(time.Now().Add(10 * time.Minute))})
	receipt, err := h.dispatch.Command(context.Background(), h.auth, api.Raw(created))
	if err != nil || receipt.Stage != "accepted" {
		t.Fatalf("child prepare %+v %v", receipt, err)
	}
	drainKind(t, h, task.JobChildPrepare)
	preparing, err := h.service.ChildRead(context.Background(), h.store, h.scope, h.auth, childID)
	if err != nil {
		t.Fatal(err)
	}
	if preparing.State != "preparing" || preparing.ChildSessionRef != nil {
		t.Fatal("lost reply guessed session ready")
	}
	time.Sleep(1050 * time.Millisecond)
	drainKind(t, h, task.JobChildPrepare)
	opened, err := h.service.ChildRead(context.Background(), h.store, h.scope, h.auth, childID)
	if err != nil {
		t.Fatal(err)
	}
	if opened.State != "open" || opened.ChildSessionRef == nil || !api.Equal(*opened.ChildSessionRef, probe.last) {
		t.Fatalf("new session after lost reply %+v", opened)
	}
	goal := func() task.DelegateInput {
		return task.DelegateInput{DelegationID: api.NewID("delegation"), ParentTaskRef: h.scope.Ref(parent.TaskID, parent.Revision), ParentGoalRevision: parent.GoalRevision, GoalRef: h.content("bounded child goal"), InputRefs: []api.ContentRef{}, AgentBindingRef: binding, PermissionRefs: []api.ObjectRef{}, Budget: []api.Amount{{Unit: "USD", Value: "3"}}, Deadline: parent.Deadline, PolicyRef: h.policy.PolicyRef, ReceiverID: receiver}
	}
	first := goal()
	send := h.command("child.send", childID, &opened.Revision, task.ChildSendInput{ChildID: childID, Mode: "new_goal", Delegation: &first})
	receipt, err = h.dispatch.Command(context.Background(), h.auth, api.Raw(send))
	if err != nil || receipt.Stage != "applied" {
		t.Fatalf("first goal %+v %v", receipt, err)
	}
	opened, err = h.service.ChildRead(context.Background(), h.store, h.scope, h.auth, childID)
	if err != nil {
		t.Fatal(err)
	}
	parent, err = h.service.Read(context.Background(), h.store, h.scope, h.auth, parent.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	oldMapping := *opened.ActiveDelegationRef
	second := goal()
	retry := func() (api.Receipt, error) {
		return h.dispatch.Command(context.Background(), h.auth, api.Raw(h.command("child.send", childID, &opened.Revision, task.ChildSendInput{ChildID: childID, Mode: "new_goal", ExpectedActiveDelegationRef: &oldMapping, Delegation: &second})))
	}
	r, e := retry()
	if e != nil || r.Stage != "rejected" || r.Error.Reason != "previous_goal_open" {
		t.Fatalf("replaced open goal %+v %v", r, e)
	}
	if err = h.service.MergeDelegation(context.Background(), h.store, h.scope, h.trusted(), task.DelegationFact{DelegationID: first.DelegationID, Revision: 1, GoalWorkClosed: true, EffectsClosed: false, TransfersClosed: true, Gaps: []string{"effect_unknown"}}); err != nil {
		t.Fatal(err)
	}
	r, e = retry()
	if !api.IsCode(e, "effect_unknown") && (r.Error == nil || r.Error.Reason != "previous_effect_unknown") {
		t.Fatalf("replaced unknown effect %+v %v", r, e)
	}
	if err = h.service.MergeDelegation(context.Background(), h.store, h.scope, h.trusted(), task.DelegationFact{DelegationID: first.DelegationID, Revision: 2, GoalWorkClosed: true, EffectsClosed: true, TransfersClosed: true, UsageFinal: false, Gaps: []string{}}); err != nil {
		t.Fatal(err)
	}
	parent, err = h.service.Read(context.Background(), h.store, h.scope, h.auth, parent.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	second.ParentTaskRef = h.scope.Ref(parent.TaskID, parent.Revision)
	r, e = retry()
	if e != nil || r.Stage != "applied" {
		t.Fatalf("pure fees blocked new bounded goal %+v %v", r, e)
	}
	wait, err := h.service.ChildWait(context.Background(), h.store, h.scope, h.auth, task.ChildWaitInput{ChildID: childID, DelegationRef: oldMapping, WaitFor: "goal_closed", TimeoutMS: 0})
	if err != nil || !wait.ConditionMet || wait.Delegation.DelegationID != first.DelegationID {
		t.Fatalf("wait followed new active pointer %+v %v", wait, err)
	}
}
func TestAllocationCloseBeforeCreationNeverReopensIncomingGate(t *testing.T) {
	h := newHarness(t, task.Ports{})
	parentOwner := api.NewID("parent")
	allocationID := api.NewID("allocation")
	parentRef := api.ObjectRef{TenantID: h.scope.TenantID, OwnerID: parentOwner, ObjectID: api.NewID("task"), Revision: 1}
	ref := api.ObjectRef{TenantID: h.scope.TenantID, OwnerID: parentOwner, ObjectID: allocationID, Revision: 1}
	closed, err := h.dispatch.Command(context.Background(), h.trusted(), api.Raw(h.command("budget.close", allocationID, nil, task.AllocationCloseInput{AllocationRef: ref, ParentTaskRef: parentRef, Reason: "parent closed before remote creation"})))
	if err != nil || closed.Stage != "applied" {
		t.Fatalf("early close %+v %v", closed, err)
	}
	status, err := h.store.Within(context.Background(), h.scope, []string{"task"}, func(tx runtime.Tx) error {
		_, e := h.service.ReceiveAllocationTx(context.Background(), tx, h.trusted(), ref, task.Allocation{AllocationID: allocationID, Revision: 1, ParentTaskRef: parentRef, ReceiverID: h.scope.OwnerID, Limits: []api.Amount{{Unit: "USD", Value: "5"}}, Deadline: api.Time(time.Now().Add(time.Hour)), State: "open"})
		return e
	})
	if status != runtime.RolledBack || !api.IsCode(err, "invalid_state") {
		t.Fatalf("late allocation reopened closed gate %s %v", status, err)
	}
	incoming, err := h.service.IncomingRead(context.Background(), h.store, h.scope, h.trusted(), ref)
	if err != nil || incoming.Gate != "closed" || incoming.TaskRef != nil {
		t.Fatalf("closed tombstone lost %+v %v", incoming, err)
	}
}

func TestInternalChildCancellationProducesSignedOriginalAllocationClosure(t *testing.T) {
	rule := fixtureRule()
	gate := &evidenceBridge{rules: map[string]api.RuleDefinition{rule.RuleRef.ComponentID: rule}}
	proof := &localProofFixture{}
	h := newHarness(t, task.Ports{Gate: gate, ClosureProof: proof}, rule)
	proof.install(t, h.scope)
	gate.service = governance.New(h.store, governance.Options{})
	parent := readyTask(t, h, rule)
	in := task.DelegateInput{DelegationID: api.NewID("delegation"), ParentTaskRef: h.scope.Ref(parent.TaskID, parent.Revision), ParentGoalRevision: parent.GoalRevision, GoalRef: h.content("exact bounded child goal"), InputRefs: []api.ContentRef{}, AgentBindingRef: h.scope.Ref(api.NewID("binding"), 1), PermissionRefs: []api.ObjectRef{}, Budget: []api.Amount{{Unit: "USD", Value: "3"}}, Deadline: parent.Deadline, PolicyRef: h.policy.PolicyRef, ReceiverID: h.scope.OwnerID, Internal: true}
	out, err := h.service.Delegate(context.Background(), h.store, h.scope, h.auth, h.command("trusted.delegate", in.DelegationID, nil, in), in)
	if err != nil || out.ChildTaskRef == nil {
		t.Fatalf("internal creation %+v %v", out, err)
	}
	child, err := h.service.Read(context.Background(), h.store, h.scope, h.auth, out.ChildTaskRef.ObjectID)
	if err != nil {
		t.Fatal(err)
	}
	decision := h.prepared(child, "2")
	if _, err = h.service.PrepareDecision(context.Background(), h.store, h.scope, h.trusted(), decision); err != nil {
		t.Fatal(err)
	}
	usage := api.UsageSnapshot{SourceRef: h.scope.Ref(decision.DecisionID, 1), UsageRevision: 1, Cumulative: []api.Amount{{Unit: "USD", Value: "1"}}, SpendingClosed: true, UsageFinal: true, ProofRefs: []api.ContentRef{h.content("accurate literal original paid source fixture")}}
	usage.UsageDigest, _ = task.UsageDigest(usage)
	if _, err = h.service.ReconcileUsage(context.Background(), h.store, h.scope, h.trusted(), "brain_decision", usage); err != nil {
		t.Fatal(err)
	}
	child, err = h.service.Read(context.Background(), h.store, h.scope, h.auth, child.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	r, err := h.dispatch.Command(context.Background(), h.auth, api.Raw(h.command("task.cancel", child.TaskID, &child.Revision, task.ControlInput{TaskID: child.TaskID, Reason: "close bounded original child"})))
	if err != nil || r.Stage != "applied" {
		t.Fatalf("child cancel %+v %v", r, err)
	}
	drainKind(t, h, task.JobDelegation)
	d, err := h.service.DelegationRead(context.Background(), h.store, h.scope, h.auth, in.DelegationID)
	if err != nil || d.Phase != "closed" || !d.GoalWorkClosed || !d.EffectsClosed || d.ClosureRef == nil {
		t.Fatalf("truthful delegation closure %+v %v", d, err)
	}
	a, err := h.service.AllocationRead(context.Background(), h.store, h.scope, h.auth, out.AllocationRef.ObjectID)
	if err != nil || a.State != "settled" || a.ClosureRef == nil {
		t.Fatalf("allocation closure not settled %+v %v", a, err)
	}
	closure, err := h.service.AllocationClosureRead(context.Background(), h.store, h.scope, h.trusted(), *a.ClosureRef)
	if err != nil || api.Equal(closure.ProofRef, child.GoalRef) {
		t.Fatalf("unsigned allocation closure %+v %v", closure, err)
	}
	var sealed sealedSource
	if _, err = h.store.Read(context.Background(), h.scope, "task.test_source_proofs", closure.ProofRef.ContentID, 1, &sealed); err != nil {
		t.Fatal(err)
	}
	body := closure
	body.ProofRef = api.ContentRef{}
	digest, _ := api.Digest(body)
	if digest != sealed.Claims.Digest {
		t.Fatal("allocation proof does not bind exact cumulative usage")
	}
	if _, err = proof.keys.Verify(sealed.JWS, sealed.Claims, time.Now()); err != nil {
		t.Fatal(err)
	}
	b, err := h.service.BudgetRead(context.Background(), h.store, h.scope, h.auth, parent.TaskID)
	if err != nil || b.Budget[0].Spent != "1" || b.Budget[0].Reserved != "0" || b.AccountingOpen {
		t.Fatalf("original parent reservation not released %+v %v", b, err)
	}
	usage.UsageRevision = 2
	usage.SourceRef.Revision = 2
	usage.Cumulative[0].Value = "2"
	usage.UsageDigest, _ = task.UsageDigest(usage)
	if _, err = h.service.ReconcileUsage(context.Background(), h.store, h.scope, h.trusted(), "brain_decision", usage); err != nil {
		t.Fatal(err)
	}
	drainKind(t, h, task.JobAllocation)
	b, err = h.service.BudgetRead(context.Background(), h.store, h.scope, h.auth, parent.TaskID)
	if err != nil || b.Budget[0].Spent != "2" || b.Budget[0].Reserved != "0" || b.AccountingOpen {
		t.Fatalf("receiver late cumulative correction did not add exact parent delta %+v %v", b, err)
	}
}
