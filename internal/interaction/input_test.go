package interaction_test

import (
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/interaction"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

func TestInputBypassesFollowUpQueueAndOnlyOwnerConsumesExactRequest(t *testing.T) {
	f := newApplication(t)
	goal := f.upload(t, "原目标等待保存路径")
	r := f.command(t, "session.submit_goal", f.session, nil, interaction.GoalInput{SessionRef: f.scope.Ref(f.session, 1), BranchRef: f.scope.Ref(f.branch, 1), ExpectedBranchRevision: 1, ContentRef: goal, AttachmentRefs: []api.ContentRef{}, PolicyRef: f.policy, Budget: []api.Amount{{Unit: "USD", Value: "20"}}, TaskDeadline: api.Time(time.Now().Add(20 * time.Minute))})
	var original interaction.SubmissionOutput
	if e := api.Decode(r.Output, &original); e != nil {
		t.Fatal(e)
	}
	f.step(t)
	saved, e := f.s.ReadSubmission(f.ctx, f.store, f.scope, f.auth, original.SubmissionRef.ObjectID)
	if e != nil {
		t.Fatal(e)
	}
	taskFact, e := f.task.Read(f.ctx, f.store, f.scope, f.auth, saved.Submission.TaskRef.ObjectID)
	if e != nil {
		t.Fatal(e)
	}
	version := taskFact.GoalRevision
	request := api.InputRequest{RequestID: api.NewID("request"), TargetRef: *saved.Submission.TaskRef, GoalRevision: &version, Purpose: "clarify_goal", QuestionRef: f.upload(t, "准确保存路径？"), AnswerSchemaRef: f.answerSchema, PreviewRefs: []api.ContentRef{goal}, ExpiresAt: api.Time(time.Now().Add(10 * time.Minute)), State: "pending"}
	var requestRef api.ObjectRef
	trusted := f.auth
	trusted.Roles = append(trusted.Roles, "service")
	status, e := f.store.Within(f.ctx, f.scope, []string{"task", "content", "memory", "interaction"}, func(tx runtime.Tx) error {
		var e error
		requestRef, e = f.task.CreateInputTx(f.ctx, tx, trusted, taskFact.TaskID, request)
		return e
	})
	if e != nil || status != runtime.Committed {
		t.Fatalf("owner creates request %s %v", status, e)
	}
	session, e := f.s.ReadSession(f.ctx, f.store, f.scope, f.auth, f.session, interaction.ReadInput{})
	if e != nil {
		t.Fatal(e)
	}
	branch := session.Branches[0]
	r = f.command(t, "session.enqueue_goal_after", f.session, nil, interaction.GoalInput{SessionRef: f.scope.Ref(f.session, session.Session.Revision), BranchRef: f.scope.Ref(f.branch, branch.Revision), ExpectedBranchRevision: branch.Revision, ContentRef: f.upload(t, "前项封闭后独立新目标"), AttachmentRefs: []api.ContentRef{}, PolicyRef: f.policy, Budget: []api.Amount{{Unit: "USD", Value: "20"}}, TaskDeadline: api.Time(time.Now().Add(20 * time.Minute)), PredecessorTaskRef: saved.Submission.TaskRef})
	var follow interaction.SubmissionOutput
	if e = api.Decode(r.Output, &follow); e != nil {
		t.Fatal(e)
	}
	f.step(t)
	waiting, e := f.s.ReadSubmission(f.ctx, f.store, f.scope, f.auth, follow.SubmissionRef.ObjectID)
	if e != nil || waiting.Submission.State != "queued" || waiting.Command != nil {
		t.Fatalf("active predecessor was bypassed %+v %v", waiting, e)
	}
	session, e = f.s.ReadSession(f.ctx, f.store, f.scope, f.auth, f.session, interaction.ReadInput{})
	if e != nil {
		t.Fatal(e)
	}
	branch = session.Branches[0]
	answer := f.uploadMedia(t, `{"path":"/report.md"}`, "application/json")
	r = f.command(t, "interaction.input", f.session, nil, interaction.InputInput{SessionRef: f.scope.Ref(f.session, session.Session.Revision), BranchRef: f.scope.Ref(f.branch, branch.Revision), ExpectedBranchRevision: branch.Revision, RequestRef: requestRef, AnswerRef: answer, PreviewRefs: request.PreviewRefs})
	var input interaction.SubmissionOutput
	if e = api.Decode(r.Output, &input); e != nil {
		t.Fatal(e)
	}
	ownerView, e := f.task.InputRequestRead(f.ctx, f.store, f.scope, f.auth, requestRef.ObjectID, 0)
	if e != nil || ownerView.Request.State != "pending" {
		t.Fatalf("saved input consumed request %+v %v", ownerView, e)
	}
	f.step(t)
	prepared, e := f.s.ReadSubmission(f.ctx, f.store, f.scope, f.auth, input.SubmissionRef.ObjectID)
	if e != nil || prepared.Submission.State != "sending" || prepared.Receipt == nil || prepared.Receipt.Stage != "accepted" {
		t.Fatalf("input blocked by newGoal FIFO %+v %v", prepared, e)
	}
	f.stepKind(t, task.JobInput)
	receipt, e := f.d.Lookup(f.ctx, f.auth, prepared.Command.CommandID)
	if e != nil || receipt.Stage != "applied" {
		t.Fatalf("owner did not consume %+v %v", receipt, e)
	}
	ownerView, e = f.task.InputRequestRead(f.ctx, f.store, f.scope, f.auth, requestRef.ObjectID, 0)
	if e != nil || ownerView.Request.State != "answered" || ownerView.Request.AnswerRef == nil || *ownerView.Request.AnswerRef != answer {
		t.Fatalf("owner request mapping %+v %v", ownerView, e)
	}
	after, e := f.task.Read(f.ctx, f.store, f.scope, f.auth, taskFact.TaskID)
	if e != nil || after.GoalRevision != 2 {
		t.Fatalf("owner goal did not include answer %+v %v", after, e)
	}
}
