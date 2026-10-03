package task_test

import (
	"context"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
	"path/filepath"
	"testing"
	"time"
)

func fixturePolicy() task.TaskPolicy {
	return task.TaskPolicy{PolicyRef: api.ComponentRef{ComponentID: api.NewID("policy"), Version: "1.0.0", Digest: api.Hash([]byte("explicit-test-policy"))}, ContinuationLimit: 100, RepairLimit: 3, NoProgressLimit: 5, ContextRoundLimit: 3, SafeAttemptLimit: 2, MaxRequirements: 100, MaxDelegations: 128, MaxDepth: 4, CostMode: "strict", BudgetLimits: []api.Amount{{Unit: "USD", Value: "100"}}, MaxEvidenceStalenessSeconds: 300, MaxDurationSeconds: 3600}
}
func TestRegisteredTaskSubmitRejectsUnknownPayloadFields(t *testing.T) {
	s, err := task.New(task.Config{Policies: []task.TaskPolicy{fixturePolicy()}}, task.Ports{})
	if err != nil {
		t.Fatal(err)
	}
	r := runtime.NewRegistry()
	if err = s.Register(r); err != nil {
		t.Fatal(err)
	}
	m, ok := r.Method("task.submit")
	if !ok {
		t.Fatal("task.submit not registered")
	}
	validator, err := api.NewValidator(m.Contract.InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	if err = validator.Validate([]byte(`{"extra":true}`)); err == nil {
		t.Fatal("undeclared fields accepted")
	}
}

func TestSubmitPreservesOriginalGoalAndDuplicateCommand(t *testing.T) {
	h := newHarness(t, task.Ports{})
	id := api.NewID("task")
	goal := h.content("full original goal with exact save path and readback")
	c := h.command("task.submit", id, nil, task.SubmitInput{OrchestratorID: h.scope.OwnerID, GoalRef: goal, PolicyRef: h.policy.PolicyRef, Deadline: api.Time(time.Now().Add(30 * time.Minute)), Budget: []api.Amount{{Unit: "USD", Value: "20"}}})
	receipt, err := h.dispatch.Command(context.Background(), h.auth, api.Raw(c))
	if err != nil || receipt.Stage != "applied" {
		t.Fatalf("submit: %+v %v", receipt, err)
	}
	again, err := h.dispatch.Command(context.Background(), h.auth, api.Raw(c))
	if err != nil || !api.Equal(receipt, again) {
		t.Fatalf("duplicate did not preserve receipt: %+v %v", again, err)
	}
	read, err := h.query("task.read", id, task.ReadInput{})
	if err != nil {
		t.Fatal(err)
	}
	var got api.Task
	if err = api.Decode(read, &got); err != nil {
		t.Fatal(err)
	}
	if !api.Equal(got.GoalRef, goal) || got.GoalRevision != 1 || got.RequirementsState != "collecting" || len(got.Requirements) != 0 {
		t.Fatalf("original goal/intake changed: %+v", got)
	}
}

type harness struct {
	service  *task.Service
	store    *sqlite.Store
	scope    runtime.Scope
	auth     runtime.Auth
	policy   task.TaskPolicy
	dispatch *runtime.Dispatcher
}

func newHarness(t *testing.T, ports task.Ports, rules ...api.RuleDefinition) *harness {
	t.Helper()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "task.sqlite"))
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
	policy := fixturePolicy()
	s, err := task.New(task.Config{Policies: []task.TaskPolicy{policy}, Rules: rules, Participants: []string{"task", "governance"}}, ports)
	if err != nil {
		t.Fatal(err)
	}
	registry := runtime.NewRegistry()
	if err = s.Register(registry); err != nil {
		t.Fatal(err)
	}
	scope := runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner"), DatabaseID: store.ID()}
	auth := runtime.Auth{TenantID: scope.TenantID, SubjectID: api.NewID("subject"), CredentialGeneration: 1}
	return &harness{service: s, store: store, scope: scope, auth: auth, policy: policy, dispatch: &runtime.Dispatcher{Store: store, OwnerID: scope.OwnerID, Registry: registry}}
}
func (h *harness) content(body string) api.ContentRef {
	return api.ContentRef{TenantID: h.scope.TenantID, OwnerID: h.scope.OwnerID, ContentID: api.NewID("content"), Version: 1, Hash: api.Hash([]byte(body)), MediaType: "text/plain", ByteLength: uint64(len(body))}
}
func (h *harness) command(method, id string, revision *uint64, in any) api.Command {
	return api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: h.scope.OwnerID, CommandID: api.NewID("command"), Method: method, TargetID: id, ExpiresAt: api.Time(time.Now().Add(time.Minute)), ExpectedRevision: revision, Payload: api.Raw(in)}
}
func (h *harness) query(method, id string, in any) ([]byte, error) {
	return h.dispatch.Query(context.Background(), h.auth, api.Raw(api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: h.scope.OwnerID, QueryID: api.NewID("query"), Method: method, TargetID: id, Payload: api.Raw(in)}))
}

func (h *harness) submit(t *testing.T) api.Task {
	t.Helper()
	id := api.NewID("task")
	receipt, err := h.dispatch.Command(context.Background(), h.auth, api.Raw(h.command("task.submit", id, nil, task.SubmitInput{OrchestratorID: h.scope.OwnerID, GoalRef: h.content("exact original target"), PolicyRef: h.policy.PolicyRef, Deadline: api.Time(time.Now().Add(30 * time.Minute)), Budget: []api.Amount{{Unit: "USD", Value: "20"}}})))
	if err != nil || receipt.Stage != "applied" {
		t.Fatalf("submit: %+v %v", receipt, err)
	}
	got, err := h.service.Read(context.Background(), h.store, h.scope, h.auth, id)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
func (h *harness) trusted() runtime.Auth { a := h.auth; a.Roles = []string{"service"}; return a }
func (h *harness) prepared(t api.Task, cost string) task.PreparedDecision {
	return task.PreparedDecision{DecisionID: api.NewID("decision"), CommandID: api.NewID("command"), BrainOwnerID: h.scope.OwnerID, CostBound: []api.Amount{{Unit: "USD", Value: cost}}, SnapshotRef: h.content("fixed complete snapshot"), Snapshot: api.Snapshot{SnapshotID: api.NewID("snapshot"), Revision: 1, TaskRef: h.scope.Ref(t.TaskID, t.Revision), GoalRevision: t.GoalRevision, ControlRevision: t.ControlRevision, GoalRef: t.GoalRef, Requirements: t.Requirements, RequirementsDigest: t.RequirementsDigest, RequirementsState: t.RequirementsState, Purpose: "interpret_requirements", PolicyRef: t.PolicyRef, InstallLockRef: h.policy.PolicyRef, ModelProfileRef: h.policy.PolicyRef, CountMode: "exact", TokenizerRef: h.policy.PolicyRef}}
}
func TestPauseResumeNeverAdoptsProposalFromPreviousControlRevision(t *testing.T) {
	h := newHarness(t, task.Ports{})
	original := h.submit(t)
	prepared := h.prepared(original, "1")
	if _, err := h.service.PrepareDecision(context.Background(), h.store, h.scope, h.trusted(), prepared); err != nil {
		t.Fatal(err)
	}
	current, err := h.service.Read(context.Background(), h.store, h.scope, h.auth, original.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"task.pause", "task.resume"} {
		c := h.command(method, current.TaskID, &current.Revision, task.ControlInput{TaskID: current.TaskID, Reason: "user control"})
		receipt, err := h.dispatch.Command(context.Background(), h.auth, api.Raw(c))
		if err != nil || receipt.Stage != "applied" {
			t.Fatalf("%s: %+v %v", method, receipt, err)
		}
		current, err = h.service.Read(context.Background(), h.store, h.scope, h.auth, current.TaskID)
		if err != nil {
			t.Fatal(err)
		}
	}
	out, err := h.service.ConsumeProposal(context.Background(), h.store, h.scope, h.trusted(), task.Proposal{DecisionID: prepared.DecisionID, Kind: "act", ReasonRef: original.GoalRef, Actions: []task.PreparedAction{}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Outcome != "stale" || len(out.AdmittedOperationIDs) != 0 {
		t.Fatalf("old running state allowed old proposal: %+v", out)
	}
	if current.Control != "running" || current.ControlRevision != 3 {
		t.Fatalf("control revisions lost: %+v", current)
	}
}

func TestDocumentedTaskBudgetAndChildMethodsHaveClosedContracts(t *testing.T) {
	h := newHarness(t, task.Ports{})
	names := []string{"task.submit", "task.pause", "task.resume", "task.cancel", "task.revise", "task.steer", "task.input", "task.accept_result", "task.adjust_budget", "task.attach_evidence", "task.billing_reconcile", "task.control_window", "task.read", "task.result", "task.list", "budget.allocate", "budget.close", "budget.settle", "budget.read", "billing.adjustment.submit", "child.create", "child.send", "child.close", "child.read", "child.list", "child.wait"}
	for _, name := range names {
		m, ok := h.dispatch.Registry.Method(name)
		if !ok {
			t.Fatalf("documented method %s missing", name)
		}
		if m.Contract.InputSchema["additionalProperties"] != false {
			t.Fatalf("%s payload is not closed", name)
		}
	}
}

func TestRepeatedPrepareDecisionKeepsOriginalIdentityAndReservation(t *testing.T) {
	h := newHarness(t, task.Ports{})
	current := h.submit(t)
	prepared := h.prepared(current, "10")
	first, err := h.service.PrepareDecision(context.Background(), h.store, h.scope, h.trusted(), prepared)
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.service.PrepareDecision(context.Background(), h.store, h.scope, h.trusted(), prepared)
	if err != nil {
		t.Fatal(err)
	}
	if !api.Equal(first, second) {
		t.Fatal("decision identity changed")
	}
	budget, err := h.service.BudgetRead(context.Background(), h.store, h.scope, h.auth, current.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if len(budget.Reservations) != 1 || budget.Budget[0].Reserved != "10" {
		t.Fatalf("duplicate reserved twice: %+v", budget)
	}
}
