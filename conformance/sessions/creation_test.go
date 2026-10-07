package sessions_test

import (
	"context"
	"testing"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/tasks"
	"google.golang.org/protobuf/proto"
)

// 单一创建端口的消费方编译验证；行为仍经生产会话命令观察。
type initialTaskCreation interface {
	CreateFromGoalInTransaction(context.Context, *v1.Caller, *v1.Ref, *v1.SessionInput, []*v1.Requirement) (*v1.Ref, error)
}

var _ initialTaskCreation = (*tasks.Service)(nil)

// 规则：G2、G3、G4、G12
func TestInitialGoalCreationPreservesDraftAndExplicitBasis(t *testing.T) {
	for _, mode := range []string{"legacy", "draft", "explicit"} {
		t.Run(mode, func(t *testing.T) {
			h := open(t)
			id := session(t, h)
			var receipt *v1.CommandReceipt
			var contentRef *v1.Ref
			identity := header("creation").Identity
			var conditions []*v1.Requirement
			if mode == "legacy" {
				c := &v1.SubmitGoalCommand{Identity: identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "original initial goal", Session: id}
				r, err := h.Sessions.SubmitGoal(ctx, caller, c)
				if err != nil || r.GetPhase() != v1.ReceiptPhase_RECEIPT_PHASE_SUBMITTED || r.TaskRef != nil {
					t.Fatalf("saved legacy goal: %v %v", r, err)
				}
				jobs, err := h.Durable.Pending(ctx, caller)
				if err != nil || len(jobs) != 1 || jobs[0].JobType != "DECIDE_GOAL" || !proto.Equal(jobs[0].Responsibility, identity) {
					t.Fatalf("original decision work: %v %v", jobs, err)
				}
				if err = h.Sessions.ProcessPending(ctx, caller); err != nil {
					t.Fatal(err)
				}
				q, err := h.Durable.QueryReceipt(ctx, caller, identity)
				receipt = accepted(t, q.GetReceipt(), err)
				contentRef = receipt.InputRef
				r, err = h.Sessions.SubmitGoal(ctx, caller, c)
				if err != nil || !proto.Equal(r, receipt) {
					t.Fatalf("original legacy receipt: %v %v", r, err)
				}
			} else {
				contentRef = stage(t, h, "creation-content", "original initial goal")
				if mode == "explicit" {
					conditions = []*v1.Requirement{{ConditionId: "original-condition", DescriptionRef: contentRef, Necessary: true, VerificationRule: "TARGET_RECORD", RuleVersion: 1}}
				}
				c := &v1.SubmitInputCommand{Header: header("creation"), SessionId: id, InputKind: "GOAL", ContentRef: contentRef, ExplicitConditions: conditions}
				r, err := h.Sessions.SubmitInput(ctx, caller, c)
				receipt = accepted(t, r, err)
				r, err = h.Sessions.SubmitInput(ctx, caller, c)
				if err != nil || !proto.Equal(r, receipt) {
					t.Fatalf("original synchronous receipt: %v %v", r, err)
				}
			}
			if receipt.Phase != v1.ReceiptPhase_RECEIPT_PHASE_DECIDED {
				t.Fatalf("creation phase: %v", receipt)
			}
			saved, err := h.Sessions.QuerySession(ctx, caller, id)
			if err != nil || saved.LastCommittedSeq != 1 || len(saved.Inputs) != 1 || len(saved.TaskRefs) != 1 || saved.TaskRefs[0].Revision != 1 {
				t.Fatalf("one original association: %v %v", saved, err)
			}
			input := saved.Inputs[0]
			if input.SessionSeq != 1 || input.TaskInputSeq != 1 || !proto.Equal(input.TaskId, saved.TaskRefs[0].Name) || !proto.Equal(input.CommandIdentity, identity) || !proto.Equal(input.ContentRef, contentRef) {
				t.Fatalf("original initial input: %v", input)
			}
			if mode == "legacy" {
				if input.RoutingStatus != "DELIVERED" || !proto.Equal(receipt.TaskRef, saved.TaskRefs[0]) {
					t.Fatalf("legacy creation references: %v %v", input, receipt)
				}
			} else if input.RoutingStatus != "TASK_ACCEPTED" || !proto.Equal(receipt.ResultRef.Name, input.InputId) {
				t.Fatalf("synchronous input reference: %v %v", input, receipt)
			}
			task, err := h.Tasks.QueryTask(ctx, caller, input.TaskId)
			if err != nil || task.InputVersion != 1 || task.RequirementsVersion != 1 || task.ControlGeneration != 1 || task.PlanningGeneration != 1 || !proto.Equal(task.GoalRef, contentRef) {
				t.Fatalf("original task basis: %v %v", task, err)
			}
			history, err := h.Tasks.QueryInputs(ctx, caller, input.TaskId)
			if err != nil || len(history.Inputs) != 1 {
				t.Fatalf("one initial task input: %v %v", history, err)
			}
			initial := history.Inputs[0]
			if initial.InputVersion != 1 || initial.TaskInputSeq != 1 || !initial.ChangesBasis || !proto.Equal(initial.InputRef.Name, input.InputId) || !proto.Equal(initial.ContentRef, contentRef) {
				t.Fatalf("original task input reference: %v", initial)
			}
			planning, err := h.Tasks.QueryPlanning(ctx, caller, input.TaskId)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "explicit" {
				if task.Revision != 2 || task.RequirementsStatus != v1.RequirementsStatus_REQUIREMENTS_STATUS_ACCEPTED || task.BoundInputVersion != 1 || task.Progress != v1.TaskProgress_TASK_PROGRESS_RUNNING || len(task.WaitingOn) != 0 || initial.ProcessingStatus != "PROCESSED" || !proto.Equal(initial.ProcessingDecision, identity) || !proto.Equal(initial.ExplicitConditions[0], conditions[0]) {
					t.Fatalf("explicit initial task: %v %v", task, initial)
				}
				requirements := planning.Requirements
				if requirements.GetSource() != "USER_EXPLICIT" || requirements.BoundInputVersion != 1 || requirements.RequirementsVersion != 1 || !proto.Equal(requirements.AcceptedBy, identity) || !proto.Equal(requirements.SourceInputRef, initial.InputRef) || !proto.Equal(requirements.Conditions[0], conditions[0]) {
					t.Fatalf("original user basis: %v", requirements)
				}
			} else if task.Revision != 1 || task.RequirementsStatus != v1.RequirementsStatus_REQUIREMENTS_STATUS_DRAFT || task.BoundInputVersion != 0 || task.Progress != v1.TaskProgress_TASK_PROGRESS_WAITING || len(task.WaitingOn) != 1 || task.WaitingOn[0] != "REQUIREMENTS" || initial.ProcessingStatus != "ACCEPTED" || initial.ProcessingDecision != nil || planning.Requirements != nil {
				t.Fatalf("draft initial task: %v %v %v", task, initial, planning)
			}
		})
	}
}

// 规则：G3、G4、R7
func TestLegacyGoalPermanentRejectionCreatesNoTaskSources(t *testing.T) {
	for _, reason := range []string{"expired", "missing-session", "stale-revision", "revision-without-session"} {
		t.Run(reason, func(t *testing.T) {
			h := open(t)
			id := session(t, h)
			before, err := h.Sessions.QuerySession(ctx, caller, id)
			if err != nil {
				t.Fatal(err)
			}
			c := &v1.SubmitGoalCommand{Identity: header("rejected-legacy-goal").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "original rejected goal", Session: id}
			var revision uint64
			code := "INVALID_INPUT"
			switch reason {
			case "expired":
				deadline := int64(0)
				c.AcceptUntilUnixMs = &deadline
			case "missing-session":
				c.Session = proto.Clone(id).(*v1.GlobalName)
				c.Session.LocalId = "missing-session"
			case "stale-revision":
				c.ExpectedRevision = &revision
				code = "REVISION_CONFLICT"
			case "revision-without-session":
				c.Session = nil
				c.ExpectedRevision = &revision
			}
			r, err := h.Sessions.SubmitGoal(ctx, caller, c)
			if err != nil || r.GetPhase() != v1.ReceiptPhase_RECEIPT_PHASE_SUBMITTED {
				t.Fatalf("saved original goal: %v %v", r, err)
			}
			sources, err := h.Trace.QuerySources(ctx, caller)
			if err != nil {
				t.Fatal(err)
			}
			if err = h.Sessions.ProcessPending(ctx, caller); err != nil {
				t.Fatal(err)
			}
			q, err := h.Durable.QueryReceipt(ctx, caller, c.Identity)
			if err != nil || q.GetReceipt().GetPhase() != v1.ReceiptPhase_RECEIPT_PHASE_DECIDED || q.GetReceipt().GetDecision() != v1.Decision_DECISION_REJECTED || q.GetReceipt().GetError().GetCode() != code || q.Receipt.TaskRef != nil {
				t.Fatalf("original permanent rejection: %v %v", q, err)
			}
			r, err = h.Sessions.SubmitGoal(ctx, caller, c)
			if err != nil || !proto.Equal(r, q.Receipt) {
				t.Fatalf("original rejected goal replay: %v %v", r, err)
			}
			after, err := h.Sessions.QuerySession(ctx, caller, id)
			if err != nil || !proto.Equal(before, after) {
				t.Fatalf("partial legacy session creation: %v %v", after, err)
			}
			afterSources, err := h.Trace.QuerySources(ctx, caller)
			if err != nil || len(afterSources) != len(sources) {
				t.Fatalf("partial legacy owner source: %v %v", afterSources, err)
			}
			for i, source := range sources {
				if !proto.Equal(source, afterSources[i]) {
					t.Fatalf("legacy owner source changed: %v", afterSources[i])
				}
			}
		})
	}
}

// 规则：G2、G3、G4、R7
func TestInitialGoalRejectionPreservesSessionAndOwnerSources(t *testing.T) {
	h := open(t)
	id := session(t, h)
	contentRef := stage(t, h, "rejected-creation-content", "original rejected basis")
	before, err := h.Sessions.QuerySession(ctx, caller, id)
	if err != nil {
		t.Fatal(err)
	}
	sources, err := h.Trace.QuerySources(ctx, caller)
	if err != nil {
		t.Fatal(err)
	}
	condition := &v1.Requirement{ConditionId: "duplicate", DescriptionRef: contentRef, Necessary: true, VerificationRule: "TARGET_RECORD", RuleVersion: 1}
	c := &v1.SubmitInputCommand{Header: header("rejected-creation"), SessionId: id, InputKind: "GOAL", ContentRef: contentRef, ExplicitConditions: []*v1.Requirement{condition, proto.Clone(condition).(*v1.Requirement)}}
	r, err := h.Sessions.SubmitInput(ctx, caller, c)
	if err != nil || r.GetDecision() != v1.Decision_DECISION_REJECTED || r.GetError().GetCode() != "INVALID_REQUIREMENTS" || r.ResultRef != nil {
		t.Fatalf("invalid initial conditions: %v %v", r, err)
	}
	rejected := r
	r, err = h.Sessions.SubmitInput(ctx, caller, c)
	if err != nil || !proto.Equal(r, rejected) {
		t.Fatalf("original rejection receipt: %v %v", r, err)
	}
	after, err := h.Sessions.QuerySession(ctx, caller, id)
	if err != nil || !proto.Equal(before, after) {
		t.Fatalf("partial session creation: %v %v", after, err)
	}
	afterSources, err := h.Trace.QuerySources(ctx, caller)
	if err != nil || len(afterSources) != len(sources) {
		t.Fatalf("partial owner source: %v %v", afterSources, err)
	}
	for i, source := range sources {
		if !proto.Equal(source, afterSources[i]) {
			t.Fatalf("owner source changed: %v", afterSources[i])
		}
	}
	c.Header = header("valid-creation-after-rejection")
	c.ExplicitConditions = []*v1.Requirement{condition}
	r, err = h.Sessions.SubmitInput(ctx, caller, c)
	accepted(t, r, err)
	after, err = h.Sessions.QuerySession(ctx, caller, id)
	if err != nil || len(after.TaskRefs) != 1 || len(after.Inputs) != 1 || after.LastCommittedSeq != 1 {
		t.Fatalf("rejection left extra creation: %v %v", after, err)
	}
}
