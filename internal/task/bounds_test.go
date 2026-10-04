package task_test

import (
	"context"
	"testing"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

func TestReservationCapacityRejectsBeforeLosingCompleteBudgetIndex(t *testing.T) {
	h := newHarness(t, task.Ports{})
	s, err := task.New(task.Config{Policies: []task.TaskPolicy{h.policy}, MaxRelations: 1}, task.Ports{})
	if err != nil {
		t.Fatal(err)
	}
	h.service = s
	h.dispatch.Registry = runtime.NewRegistry()
	if err = s.Register(h.dispatch.Registry); err != nil {
		t.Fatal(err)
	}
	current := h.submit(t)
	first := h.prepared(current, "1")
	if _, err = s.PrepareDecision(context.Background(), h.store, h.scope, h.trusted(), first); err != nil {
		t.Fatal(err)
	}
	current, err = s.Read(context.Background(), h.store, h.scope, h.auth, current.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PrepareDecision(context.Background(), h.store, h.scope, h.trusted(), h.prepared(current, "1")); !isTaskRejection(err, "invalid_state", "decision_pending") {
		t.Fatalf("unconsumed original Decision did not retain serial gate: %v", err)
	}
	// 消费原提案后才观察预留集合容量；拒绝提案不释放仍未知的原预留。
	if out, err := s.ConsumeProposal(context.Background(), h.store, h.scope, h.trusted(), task.Proposal{DecisionID: first.DecisionID, Kind: "refine_requirements", ReasonRef: current.GoalRef}, nil); err != nil || out.Outcome != "rejected" {
		t.Fatalf("original proposal consumption %+v %v", out, err)
	}
	current, err = s.Read(context.Background(), h.store, h.scope, h.auth, current.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PrepareDecision(context.Background(), h.store, h.scope, h.trusted(), h.prepared(current, "1")); !api.IsCode(err, "overloaded") {
		t.Fatalf("accepted responsibility beyond complete reservation index: %v", err)
	}
	b, err := s.BudgetRead(context.Background(), h.store, h.scope, h.auth, current.TaskID)
	if err != nil || len(b.Reservations) != 1 || b.Budget[0].Reserved != "1" {
		t.Fatalf("index not complete after refusal %+v %v", b, err)
	}
}

func TestBudgetContractAcceptsConfiguredFullReservationCollection(t *testing.T) {
	h := newHarness(t, task.Ports{})
	m, ok := h.dispatch.Registry.Method("budget.read")
	if !ok {
		t.Fatal("budget.read missing")
	}
	validator, err := api.NewValidator(m.Contract.OutputSchema)
	if err != nil {
		t.Fatal(err)
	}
	out := task.BudgetReadOutput{Budget: []api.BudgetBalance{}, Reservations: []task.Reservation{}, GrossSpent: []api.Amount{}, CreditTotal: []api.Amount{}, NetCost: []api.Amount{}}
	for i := 0; i < 101; i++ {
		out.Reservations = append(out.Reservations, task.Reservation{ReservationID: api.NewID("reservation"), Revision: 1, TaskID: api.NewID("task"), SourceKind: "brain_decision", SourceRef: h.scope.Ref(api.NewID("decision"), 1), BindingState: "bound", State: "settled", Units: []task.ReservationUnit{}, Incidents: []string{}})
	}
	if err = validator.Validate(api.Raw(task.BudgetReadResponse{Task: &out})); err != nil {
		t.Fatalf("full bounded reservation collection cannot be observed: %v", err)
	}
}
