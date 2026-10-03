package task_test

import (
	"context"
	"fmt"
	"github.com/ruipengliu/lerna/runtime"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/internal/task"
)

func preparedAction(h *harness, cost, key string) task.PreparedAction {
	return task.PreparedAction{OperationID: api.NewID("operation"), ExecutorID: h.scope.OwnerID, CapabilityRef: h.policy.PolicyRef, BindingRef: h.scope.Ref(api.NewID("binding"), 1), InstallLockRef: h.policy.PolicyRef, ArgumentsRef: h.content("prepared exact arguments"), ResourcesRef: h.content("prepared exact resource set"), RequirementRefs: []api.RequirementRef{}, UseIntentRefs: []api.ObjectRef{}, CostBound: []api.Amount{{Unit: "USD", Value: cost}}, LogicalStepKey: key, ProcessedSourceRefs: []api.ContentRef{}, DisclosedSourceRefs: []api.ContentRef{}, ResourceKeys: []string{key}, Independent: true, CommandID: api.NewID("command")}
}
func TestBatchAdmissionIsAtomicAndMaxFourIndependentActions(t *testing.T) {
	rule := fixtureRule()
	bridge := &evidenceBridge{rules: map[string]api.RuleDefinition{rule.RuleRef.ComponentID: rule}}
	proof := &localProofFixture{}
	h := newHarness(t, task.Ports{Gate: bridge, ClosureProof: proof, ActionAuthorization: proof}, rule)
	proof.install(t, h.scope)
	bridge.service = governance.New(h.store, governance.Options{})
	current := readyTask(t, h, rule)
	consume := func(actions []task.PreparedAction) task.Consumption {
		t.Helper()
		var err error
		current, err = h.service.Read(context.Background(), h.store, h.scope, h.auth, current.TaskID)
		if err != nil {
			t.Fatal(err)
		}
		prepared := h.prepared(current, "0")
		prepared.Snapshot.Purpose = "decide"
		prepared.Snapshot.CoverageRef = current.CurrentCoverageRef
		if _, err = h.service.PrepareDecision(context.Background(), h.store, h.scope, h.trusted(), prepared); err != nil {
			t.Fatal(err)
		}
		out, err := h.service.ConsumeProposal(context.Background(), h.store, h.scope, h.trusted(), task.Proposal{DecisionID: prepared.DecisionID, Kind: "act", ReasonRef: current.GoalRef, Actions: actions}, nil)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	denied := consume([]task.PreparedAction{preparedAction(h, "12", "independent_a"), preparedAction(h, "12", "independent_b")})
	if denied.Outcome != "rejected" || len(denied.AdmittedOperationIDs) != 0 {
		t.Fatalf("partially admitted over-budget batch %+v", denied)
	}
	facts, err := h.service.ContextFacts(context.Background(), h.store, h.scope, h.auth, current.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.service.BudgetRead(context.Background(), h.store, h.scope, h.auth, current.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts.Operations) != 0 || b.Budget[0].Reserved != "0" {
		t.Fatalf("batch rollback leaked commitment operations=%d budget=%+v", len(facts.Operations), b.Budget)
	}
	approvalRows, err := h.store.List(context.Background(), h.scope, "task.test_authorized_actions", current.TaskID, "", 10)
	if err != nil || len(approvalRows) != 0 {
		t.Fatalf("batch rollback leaked authorization: %v %d", err, len(approvalRows))
	}
	five := []task.PreparedAction{}
	for i := 0; i < 5; i++ {
		five = append(five, preparedAction(h, "1", string(rune('a'+i))))
	}
	if out := consume(five); out.Outcome != "rejected" {
		t.Fatalf("accepted >4: %+v", out)
	}
	four := five[:4]
	out := consume(four)
	if out.Outcome != "adopted" || len(out.AdmittedOperationIDs) != 4 {
		t.Fatalf("independent legal batch rejected: %+v", out)
	}
	facts, err = h.service.ContextFacts(context.Background(), h.store, h.scope, h.auth, current.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	b, err = h.service.BudgetRead(context.Background(), h.store, h.scope, h.auth, current.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts.Operations) != 4 || b.Budget[0].Reserved != "4" || facts.UnresolvedCollections[0].UnresolvedCount != 4 {
		t.Fatalf("full relation/budget facts lost %+v %+v", facts, b)
	}
	closure, err := h.service.Closure(context.Background(), h.store, h.scope, h.auth, h.scope.Ref(current.TaskID, current.Revision))
	if err != nil {
		t.Fatal(err)
	}
	if closure.EffectsClosed {
		t.Fatal("unconfirmed dispatch intent counted as effect closure")
	}
}

func TestTerminalHistoryCannotHideActiveTaskCapacity(t *testing.T) {
	h := newHarness(t, task.Ports{})
	s, err := task.New(task.Config{Policies: []task.TaskPolicy{h.policy}, MaxTasksPerSubject: 2}, task.Ports{})
	if err != nil {
		t.Fatal(err)
	}
	h.service = s
	h.dispatch.Registry = runtime.NewRegistry()
	if err = s.Register(h.dispatch.Registry); err != nil {
		t.Fatal(err)
	}
	submitID := func(n int) (api.Receipt, error) {
		id := fmt.Sprintf("task_%032x", n)
		return h.dispatch.Command(context.Background(), h.auth, api.Raw(h.command("task.submit", id, nil, task.SubmitInput{OrchestratorID: h.scope.OwnerID, GoalRef: h.content("original"), PolicyRef: h.policy.PolicyRef, Deadline: api.Time(time.Now().Add(10 * time.Minute)), Budget: []api.Amount{{Unit: "USD", Value: "1"}}})))
	}
	for i := 1; i <= 4; i++ {
		r, e := submitID(i)
		if e != nil || r.Stage != "applied" {
			t.Fatalf("history create %+v %v", r, e)
		}
		revision := uint64(1)
		id := fmt.Sprintf("task_%032x", i)
		r, e = h.dispatch.Command(context.Background(), h.auth, api.Raw(h.command("task.cancel", id, &revision, task.ControlInput{TaskID: id, Reason: "historical goal closed"})))
		if e != nil || r.Stage != "applied" {
			t.Fatalf("history close %+v %v", r, e)
		}
	}
	for i := 100; i < 102; i++ {
		r, e := submitID(i)
		if e != nil || r.Stage != "applied" {
			t.Fatalf("legal active create %+v %v", r, e)
		}
	}
	r, e := submitID(102)
	if !api.IsCode(e, "overloaded") && (r.Error == nil || r.Error.Code != "overloaded") {
		t.Fatalf("bounded history scan hid active tasks: %+v %v", r, e)
	}
}
