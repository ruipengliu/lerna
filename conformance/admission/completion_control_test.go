package admission_test

import (
	"slices"
	"testing"
	"time"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

// 规则：G2、G4、G6、完成-1、完成-3、完成-7
func TestStaleCompletionAndUnprocessedLatestInputCannotStartRound(t *testing.T) {
	for _, kind := range []string{"MODIFY", "NONBASIS", "PAUSE", "CANCEL", "REQUIREMENTS"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, 100, 80, false)
			p := completeProposal(t, f, nil)
			mutateCompletionBasis(t, f, kind)
			r, e := f.h.Tasks.BeginCompletion(f.ctx, f.caller, &v1.BeginCompletionCommand{Header: header("stale-completion"), TaskId: f.task.Name, ProposalRef: p})
			if e != nil || r.Decision != v1.Decision_DECISION_REJECTED {
				t.Fatalf("stale completion started %v %v", r, e)
			}
			state, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
			if e != nil || state.VerificationFreeze != 0 || state.VerificationRound != 0 {
				t.Fatalf("stale completion froze task %v %v", state, e)
			}
			if kind == "MODIFY" {
				f.suffix = "latest"
				p = completeProposal(t, f, nil)
				r, e = f.h.Tasks.BeginCompletion(f.ctx, f.caller, &v1.BeginCompletionCommand{Header: header("unprocessed-completion"), TaskId: f.task.Name, ProposalRef: p})
				if e != nil || r.Error.GetCode() != "REQUIREMENTS_NOT_ACCEPTED" {
					t.Fatalf("latest input bypassed processing %v %v", r, e)
				}
			}
		})
	}
}

// 规则：G2、G11、完成-7
func TestAcceptedInputAndControlSupersedeRoundWithoutLosingOtherWaits(t *testing.T) {
	for _, kind := range []string{"MODIFY", "NONBASIS", "PAUSE", "CANCEL"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, 100, 80, false)
			p := completeProposal(t, f, nil)
			r, e := f.h.Tasks.BeginCompletion(f.ctx, f.caller, &v1.BeginCompletionCommand{Header: header("basis-begin"), TaskId: f.task.Name, ProposalRef: p})
			accepted(t, r, e)
			old := r.ResultRef
			mutateCompletionBasis(t, f, kind)
			state, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
			if e != nil {
				t.Fatal(e)
			}
			v, e := f.h.Tasks.QueryVerification(f.ctx, f.caller, state.VerificationRef)
			if e != nil || v.Status != "SUPERSEDED" || state.VerificationFreeze != 0 {
				t.Fatalf("orphaned round %v %v", v, e)
			}
			task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
			if e != nil {
				t.Fatal(e)
			}
			if kind == "CANCEL" && (task.Control != v1.TaskControl_TASK_CONTROL_CANCELLING || !slices.Contains(task.WaitingOn, "CANCELLATION_CLOSURE")) {
				t.Fatalf("lost cancel %v", task)
			}
			if kind == "MODIFY" && !slices.Contains(task.WaitingOn, "INPUT_PROCESSING") {
				t.Fatalf("lost input wait %v", task)
			}
			if kind == "PAUSE" && task.Control != v1.TaskControl_TASK_CONTROL_PAUSED {
				t.Fatal("lost pause")
			}
			r, e = f.h.Tasks.RecheckCompletion(f.ctx, f.caller, &v1.RecheckCompletionCommand{Header: header("old-basis-recheck"), VerificationRef: old})
			if e != nil || r.Error.GetCode() != "STALE_VERIFICATION" {
				t.Fatalf("old round closed %v %v", r, e)
			}
		})
	}
}
func mutateCompletionBasis(t *testing.T, f *fixture, kind string) {
	t.Helper()
	if kind == "REQUIREMENTS" {
		scopeRequirement(t, f)
		return
	}
	task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	goal, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("goal").Identity)
	if e != nil {
		t.Fatal(e)
	}
	cmd := &v1.SubmitInputCommand{Header: header("basis-change"), SessionId: goal.Receipt.SessionRef.Name, TaskId: f.task.Name, ContentRef: f.parameters, InputKind: "MODIFY", ExpectedInputVersion: task.InputVersion, ExpectedRequirementsVersion: task.RequirementsVersion}
	switch kind {
	case "PAUSE", "CANCEL":
		cmd.InputKind = "CONTROL"
		cmd.Control = kind
		cmd.ExpectedControlGeneration = task.ControlGeneration
		cmd.ExpectedInputVersion = 0
		cmd.ExpectedRequirementsVersion = 0
	case "NONBASIS":
		r, e := f.h.Sessions.PublishQuestion(f.ctx, f.caller, &v1.PublishQuestionCommand{Header: header("nonbasis-question"), SessionId: goal.Receipt.SessionRef.Name, TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, ContentRef: f.parameters, ChangesBasis: false, ExpiresAtUnixMs: time.Now().Add(time.Minute).UnixMilli()})
		accepted(t, r, e)
		cmd.InputKind = "ANSWER"
		cmd.RequestRef = r.ResultRef
	}
	r, e := f.h.Sessions.SubmitInput(f.ctx, f.caller, cmd)
	accepted(t, r, e)
}

// 规则：G2、G6、完成-2
func TestCompletionRejectsUnsupportedChildAuthorityFields(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	p := completeProposal(t, f, nil)
	cmd := &v1.BeginCompletionCommand{Header: header("unsupported-child"), TaskId: f.task.Name, ProposalRef: p}
	// 未声明的子任务证据字段不得被忽略后当作空子任务清单。
	cmd.ProtoReflect().SetUnknown([]byte{0x52, 0x01, 0x01})
	if _, e := f.h.Tasks.BeginCompletion(f.ctx, f.caller, cmd); e == nil {
		t.Fatal("unsupported authority silently ignored")
	}
	state, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
	if e != nil || state.VerificationFreeze != 0 {
		t.Fatalf("unsupported field changed task %v %v", state, e)
	}
}
