package task_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// Context边界实际出版Content；护栏必须在到达该边界前挡住新编译。
type publishingContextFixture struct {
	harness *harness
	content *contentBridge
}

func (p *publishingContextFixture) Prepare(ctx context.Context, scope runtime.Scope, _ runtime.Auth, current api.Task) (task.PreparedDecision, error) {
	prepared := p.harness.prepared(current, "0")
	ref, err := p.content.Publish(ctx, scope, api.NewID("upload"), "application/json", api.Raw(prepared.Snapshot))
	prepared.SnapshotRef = ref
	return prepared, err
}

func TestNoProgressGuardrailFinishesAdvanceWithoutNewDecisionOrContent(t *testing.T) {
	ctx := context.Background()
	content := &contentBridge{}
	compiler := &publishingContextFixture{content: content}
	h := newHarness(t, task.Ports{})
	configureContent(t, h, content)
	h.policy.NoProgressLimit = 1
	h.policy.ContinuationLimit = 2
	schema := api.Object(map[string]any{"change": api.String()}, "change")
	digest, _ := api.Digest(schema)
	schemaRef := api.ComponentRef{ComponentID: api.NewID("schema"), Version: "1.0.0", Digest: digest}
	s, err := task.New(task.Config{Policies: []task.TaskPolicy{h.policy}, AnswerSchemas: []task.AnswerSchemaDefinition{{Ref: schemaRef, Schema: schema}}}, task.Ports{Context: compiler, Content: content})
	if err != nil {
		t.Fatal(err)
	}
	h.service = s
	h.dispatch.Registry = runtime.NewRegistry()
	if err = s.Register(h.dispatch.Registry); err != nil {
		t.Fatal(err)
	}
	compiler.harness = h
	current := h.submit(t)
	prepared := h.prepared(current, "0")
	if _, err = s.PrepareDecision(ctx, h.store, h.scope, h.trusted(), prepared); err != nil {
		t.Fatal(err)
	}
	out, err := s.ConsumeProposal(ctx, h.store, h.scope, h.trusted(), task.Proposal{DecisionID: prepared.DecisionID, Kind: "invalid_fixture_proposal", ReasonRef: current.GoalRef}, nil)
	if err != nil || out.Outcome != "rejected" {
		t.Fatalf("explicit invalid decision: %+v %v", out, err)
	}
	before, err := os.ReadDir(content.objectRoot)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		drainKind(t, h, task.JobAdvance)
	}
	current, err = s.Read(ctx, h.store, h.scope, h.auth, current.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, reason := range current.WaitReasons {
		found = found || reason.Kind == "dependency" && reason.ResumeCondition == "no_progress_limit: valid new input, goal change, usable result or original unknown resolution"
	}
	if current.Status != "active" || !found {
		t.Fatalf("guardrail did not preserve an accurate recoverable wait: %+v", current)
	}
	after, err := os.ReadDir(content.objectRoot)
	if err != nil || len(after) != len(before) {
		t.Fatalf("blocked advance published new Content: %d -> %d %v", len(before), len(after), err)
	}
	b, err := s.BudgetRead(ctx, h.store, h.scope, h.auth, current.TaskID)
	if err != nil || len(b.Reservations) != 1 {
		t.Fatalf("blocked advance admitted a new Decision: %+v %v", b, err)
	}
	work, _, err := h.store.Claim(ctx, h.scope, api.NewID("worker"), []string{task.JobAdvance}, 1, 30*time.Second)
	if err != nil || len(work) != 0 {
		t.Fatalf("guardrail left a timer-driven retry: %+v %v", work, err)
	}
	request := api.InputRequest{RequestID: api.NewID("request"), TargetRef: h.scope.Ref(current.TaskID, current.Revision), GoalRevision: &current.GoalRevision, Purpose: "clarify_goal", QuestionRef: h.content("provide a concrete missing criterion"), AnswerSchemaRef: schemaRef, PreviewRefs: []api.ContentRef{current.GoalRef}, ExpiresAt: api.Time(time.Now().Add(time.Minute)), State: "pending"}
	var requestRef api.ObjectRef
	status, err := h.store.Within(ctx, h.scope, []string{"task"}, func(tx runtime.Tx) error {
		var err error
		requestRef, err = s.CreateInputTx(ctx, tx, h.trusted(), current.TaskID, request)
		return err
	})
	if err != nil || status != runtime.Committed {
		t.Fatalf("real recovery request: %v %v", status, err)
	}
	answer, err := content.Publish(ctx, h.scope, api.NewID("upload"), "application/json", []byte(`{"change":"concrete new criterion"}`))
	if err != nil {
		t.Fatal(err)
	}
	r, err := h.dispatch.Command(ctx, h.auth, api.Raw(h.command("task.input", current.TaskID, nil, task.InputAnswer{TaskID: current.TaskID, RequestRef: requestRef, GoalRevision: current.GoalRevision, AnswerRef: answer})))
	if err != nil || r.Stage != "accepted" {
		t.Fatalf("valid new input: %+v %v", r, err)
	}
	drainKind(t, h, task.JobInput)
	current, err = s.Read(ctx, h.store, h.scope, h.auth, current.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	second := h.prepared(current, "0")
	if _, err = s.PrepareDecision(ctx, h.store, h.scope, h.trusted(), second); err != nil {
		t.Fatalf("real new input did not reset consecutive no-progress: %v", err)
	}
	drainKind(t, h, task.JobAdvance)
	admitted, err := s.Read(ctx, h.store, h.scope, h.auth, current.TaskID)
	if err != nil || admitted.Status != "active" {
		t.Fatalf("quota end killed the last already-admitted Decision: %+v %v", admitted, err)
	}
	out, err = s.ConsumeProposal(ctx, h.store, h.scope, h.trusted(), task.Proposal{DecisionID: second.DecisionID, Kind: "invalid_fixture_proposal", ReasonRef: admitted.GoalRef}, nil)
	if err != nil || out.Outcome != "rejected" {
		t.Fatalf("original last decision did not retain its consumption: %+v %v", out, err)
	}
	// 第二个Decision耗尽原累计额度；新输入只清连续无进展，没有将累计数重置。
	beforeEnd, err := os.ReadDir(content.objectRoot)
	if err != nil {
		t.Fatal(err)
	}
	drainKind(t, h, task.JobAdvance)
	drainKind(t, h, task.JobAdvance)
	closed, err := s.Read(ctx, h.store, h.scope, h.auth, current.TaskID)
	if err != nil || closed.Status != "failed" || len(closed.WaitReasons) != 1 || closed.WaitReasons[0].ResumeCondition != "goal closed: continuation_limit" {
		t.Fatalf("cumulative guardrail was reset or guessed success: %+v %v", closed, err)
	}
	afterEnd, err := os.ReadDir(content.objectRoot)
	if err != nil || len(beforeEnd) != len(afterEnd) {
		t.Fatalf("cumulative end still compiled Content: %d -> %d %v", len(beforeEnd), len(afterEnd), err)
	}
}

func TestPureLateUsageRevisionCannotClearConsecutiveNoProgress(t *testing.T) {
	ctx := context.Background()
	proof := &localProofFixture{}
	h := newHarness(t, task.Ports{})
	h.policy.NoProgressLimit = 1
	s, err := task.New(task.Config{Policies: []task.TaskPolicy{h.policy}}, task.Ports{ActionAuthorization: proof})
	if err != nil {
		t.Fatal(err)
	}
	h.service = s
	h.dispatch.Registry = runtime.NewRegistry()
	if err = s.Register(h.dispatch.Registry); err != nil {
		t.Fatal(err)
	}
	proof.install(t, h.scope)
	current := h.submit(t)
	prepared := h.prepared(current, "0")
	if _, err = s.PrepareDecision(ctx, h.store, h.scope, h.trusted(), prepared); err != nil {
		t.Fatal(err)
	}
	action := preparedAction(h, "0", "original_safe_check")
	action.SafeRequirementCheck = true
	out, err := s.ConsumeProposal(ctx, h.store, h.scope, h.trusted(), task.Proposal{DecisionID: prepared.DecisionID, Kind: "act", ReasonRef: current.GoalRef, Actions: []task.PreparedAction{action}}, nil)
	if err != nil || out.Outcome != "adopted" {
		t.Fatalf("initial admission: %+v %v", out, err)
	}
	operation := api.Operation{OperationID: action.OperationID, OwnerID: h.scope.OwnerID, TaskRef: h.scope.Ref(current.TaskID, current.Revision), Revision: 1, ExecutionState: "closed", Effect: "not_applied", MayApplyLater: false, Attempts: api.CollectionSummary{CollectionRevision: 1, Complete: true}, EvidenceRefs: []api.ContentRef{h.content("preapproved exact not-applied observation")}, Usage: []api.Amount{{Unit: "USD", Value: "0"}}, UsageFinal: false, NextAction: "none"}
	if err = s.MergeOperation(ctx, h.store, h.scope, h.trusted(), operation); err != nil {
		t.Fatal(err)
	}
	current, err = s.Read(ctx, h.store, h.scope, h.auth, current.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	bad := h.prepared(current, "0")
	if _, err = s.PrepareDecision(ctx, h.store, h.scope, h.trusted(), bad); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ConsumeProposal(ctx, h.store, h.scope, h.trusted(), task.Proposal{DecisionID: bad.DecisionID, Kind: "invalid_fixture_proposal", ReasonRef: current.GoalRef}, nil); err != nil {
		t.Fatal(err)
	}
	// 同一原效果只有费用和revision改变，不是新的目标进展。
	operation.Revision = 2
	operation.Usage = []api.Amount{{Unit: "USD", Value: "1"}}
	operation.UsageFinal = true
	if err = s.MergeOperation(ctx, h.store, h.scope, h.trusted(), operation); err != nil {
		t.Fatal(err)
	}
	current, err = s.Read(ctx, h.store, h.scope, h.auth, current.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PrepareDecision(ctx, h.store, h.scope, h.trusted(), h.prepared(current, "0")); !api.IsCode(err, "invalid_state") {
		t.Fatalf("pure late billing cleared no-progress: %v", err)
	}
}
