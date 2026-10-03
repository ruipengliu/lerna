package interaction_test

import (
	"errors"
	"github.com/ruipengliu/lerna/runtime"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/interaction"
	"github.com/ruipengliu/lerna/internal/task"
)

func TestQueuedWithdrawalAndSendingWithdrawalHaveDifferentBusinessEffects(t *testing.T) {
	for _, sending := range []bool{false, true} {
		t.Run(map[bool]string{false: "queued", true: "sending"}[sending], func(t *testing.T) {
			f := newApplication(t)
			r := f.command(t, "session.submit_goal", f.session, nil, interaction.GoalInput{SessionRef: f.scope.Ref(f.session, 1), BranchRef: f.scope.Ref(f.branch, 1), ExpectedBranchRevision: 1, ContentRef: f.upload(t, "撤回候选"), AttachmentRefs: []api.ContentRef{}, PolicyRef: f.policy, Budget: []api.Amount{{Unit: "USD", Value: "20"}}, TaskDeadline: api.Time(time.Now().Add(20 * time.Minute))})
			var output interaction.SubmissionOutput
			if e := api.Decode(r.Output, &output); e != nil {
				t.Fatal(e)
			}
			if sending {
				f.delivery.drop = true
				f.step(t)
			}
			view, e := f.s.ReadSubmission(f.ctx, f.store, f.scope, f.auth, output.SubmissionRef.ObjectID)
			if e != nil {
				t.Fatal(e)
			}
			revision := view.Submission.Revision
			r = f.command(t, "submission.withdraw", output.SubmissionRef.ObjectID, &revision, interaction.WithdrawInput{Reason: "用户撤回"})
			if e = api.Decode(r.Output, &output); e != nil {
				t.Fatal(e)
			}
			if sending {
				if output.State != "sending" || !output.WithdrawalRequested {
					t.Fatalf("sent withdrawal lied %+v", output)
				}
			} else {
				if output.State != "withdrawn" || output.WithdrawalRequested {
					t.Fatalf("queued withdrawal %+v", output)
				}
			}
			f.step(t)
			view, e = f.s.ReadSubmission(f.ctx, f.store, f.scope, f.auth, output.SubmissionRef.ObjectID)
			if e != nil {
				t.Fatal(e)
			}
			if sending {
				if view.Submission.State != "applied" || view.Submission.TaskRef == nil {
					t.Fatalf("lost original mappedTask %+v", view)
				}
				actual, e := f.task.Read(f.ctx, f.store, f.scope, f.auth, view.Submission.TaskRef.ObjectID)
				if e != nil || actual.Status != "active" {
					t.Fatalf("withdraw implicitly canceled Task %+v %v", actual, e)
				}
			} else if f.delivery.sends != 0 {
				t.Fatalf("withdrawn goal sent %d", f.delivery.sends)
			}
		})
	}
}

func TestSteerKeepsTaskOrderAndOriginalGoalRevisionOutsideFollowUpFIFO(t *testing.T) {
	f := newApplication(t)
	taskID := api.NewID("task")
	f.command(t, "task.submit", taskID, nil, task.SubmitInput{OrchestratorID: f.scope.OwnerID, GoalRef: f.upload(t, "活动目标"), PolicyRef: f.policy, Deadline: api.Time(time.Now().Add(20 * time.Minute)), Budget: []api.Amount{{Unit: "USD", Value: "20"}}})
	target := f.scope.Ref(taskID, 1)
	one := uint64(1)
	queue := func(method, body string) interaction.SubmissionOutput {
		t.Helper()
		view, e := f.s.ReadSession(f.ctx, f.store, f.scope, f.auth, f.session, interaction.ReadInput{})
		if e != nil {
			t.Fatal(e)
		}
		branch := view.Branches[0]
		in := interaction.GoalInput{SessionRef: f.scope.Ref(f.session, view.Session.Revision), BranchRef: f.scope.Ref(f.branch, branch.Revision), ExpectedBranchRevision: branch.Revision, ContentRef: f.upload(t, body), AttachmentRefs: []api.ContentRef{}, PolicyRef: f.policy, Budget: []api.Amount{{Unit: "USD", Value: "20"}}, TaskDeadline: api.Time(time.Now().Add(20 * time.Minute))}
		if method == "session.steer" {
			in.TargetTaskRef, in.ExpectedGoalRevision = &target, &one
		} else {
			in.PredecessorTaskRef = &target
		}
		r := f.command(t, method, f.session, nil, in)
		var output interaction.SubmissionOutput
		if e = api.Decode(r.Output, &output); e != nil {
			t.Fatal(e)
		}
		return output
	}
	follow := queue("session.enqueue_goal_after", "前项封闭后的新目标")
	f.step(t)
	first := queue("session.steer", "第一条补充")
	second := queue("session.steer", "第二条仍引用原 GoalRevision=1")
	f.step(t)
	f.step(t)
	firstView, e := f.s.ReadSubmission(f.ctx, f.store, f.scope, f.auth, first.SubmissionRef.ObjectID)
	if e != nil || firstView.Submission.State != "sending" || firstView.Receipt == nil || firstView.Receipt.Stage != "accepted" {
		t.Fatalf("first steer did not bypass follow-up FIFO %+v %v", firstView, e)
	}
	secondView, e := f.s.ReadSubmission(f.ctx, f.store, f.scope, f.auth, second.SubmissionRef.ObjectID)
	if e != nil || secondView.Submission.State != "queued" || f.delivery.sends != 1 || secondView.Command == nil {
		t.Fatalf("later steer bypassed its Task order %+v %v", secondView, e)
	}
	original := *secondView.Command
	f.stepKind(t, task.JobSteer)
	for attempts := 0; attempts < 5; attempts++ {
		works, status, e := f.store.Claim(f.ctx, f.scope, api.NewID("boot"), []string{interaction.JobDispatch}, 16, 30*time.Second)
		if e != nil || status != runtime.Committed {
			t.Fatalf("dispatch ordered steers %s %v", status, e)
		}
		for _, work := range works {
			if e = f.s.Dispatch(f.ctx, f.store, f.scope, work); e != nil {
				t.Fatal(e)
			}
		}
		secondView, e = f.s.ReadSubmission(f.ctx, f.store, f.scope, f.auth, second.SubmissionRef.ObjectID)
		if e != nil {
			t.Fatal(e)
		}
		if secondView.Submission.State == "rejected" {
			break
		}
		time.Sleep(1100 * time.Millisecond)
	}
	if secondView.Submission.State != "rejected" || secondView.Error == nil || secondView.Error.Code != "revision_conflict" || !api.Equal(secondView.Command, &original) || f.delivery.sends != 2 {
		t.Fatalf("stale steer was refreshed or resent %+v", secondView)
	}
	var wire task.SteerInput
	if e = api.Decode(secondView.Command.Payload, &wire); e != nil || wire.BaseGoalRevision != 1 {
		t.Fatalf("original GoalRevision changed %+v %v", wire, e)
	}
	current, e := f.task.Read(f.ctx, f.store, f.scope, f.auth, taskID)
	if e != nil || current.GoalRevision != 2 || current.Status != "active" {
		t.Fatalf("owner's first amendment was not retained %+v %v", current, e)
	}
	waiting, e := f.s.ReadSubmission(f.ctx, f.store, f.scope, f.auth, follow.SubmissionRef.ObjectID)
	if e != nil || waiting.Submission.State != "queued" || waiting.Command != nil {
		t.Fatalf("steer released the separate new-goal FIFO %+v %v", waiting, e)
	}
}

func TestUnknownSendingCommitDoesNotSendUntilOriginalDurableStateIsRechecked(t *testing.T) {
	f := newApplication(t)
	r := f.command(t, "session.submit_goal", f.session, nil, interaction.GoalInput{SessionRef: f.scope.Ref(f.session, 1), BranchRef: f.scope.Ref(f.branch, 1), ExpectedBranchRevision: 1, ContentRef: f.upload(t, "发送准备未知"), AttachmentRefs: []api.ContentRef{}, PolicyRef: f.policy, Budget: []api.Amount{{Unit: "USD", Value: "20"}}, TaskDeadline: api.Time(time.Now().Add(20 * time.Minute))})
	var output interaction.SubmissionOutput
	if e := api.Decode(r.Output, &output); e != nil {
		t.Fatal(e)
	}
	works, status, e := f.store.Claim(f.ctx, f.scope, api.NewID("boot"), []string{interaction.JobDispatch}, 1, 30*time.Second)
	if e != nil || status != runtime.Committed || len(works) != 1 {
		t.Fatalf("claim %s %v", status, e)
	}
	h, _ := f.registry.Job(interaction.JobDispatch)
	f.commitFault.Store(true)
	if e = h(f.ctx, f.store, f.scope, works[0]); !errors.Is(e, runtime.ErrCommitUnknown) {
		t.Fatalf("unknown prepare status lost %v", e)
	}
	if f.delivery.sends != 0 {
		t.Fatal("unknown prepare sent business command")
	}
	saved, e := f.s.ReadSubmission(f.ctx, f.store, f.scope, f.auth, output.SubmissionRef.ObjectID)
	if e != nil || saved.Submission.State != "sending" || saved.Command == nil {
		t.Fatalf("durable unknown intent missing %+v %v", saved, e)
	}
	command := *saved.Command
	if e = h(f.ctx, f.store, f.scope, works[0]); e != nil {
		t.Fatal(e)
	}
	after, e := f.s.ReadSubmission(f.ctx, f.store, f.scope, f.auth, output.SubmissionRef.ObjectID)
	if e != nil || after.Submission.State != "applied" || f.delivery.sends != 1 || !api.Equal(after.Command, &command) {
		t.Fatalf("original prepare did not recover %+v %v", after, e)
	}
}

func TestLateReplyRemainsOnOriginalBranchAfterSelection(t *testing.T) {
	f := newApplication(t)
	r := f.command(t, "session.submit_goal", f.session, nil, interaction.GoalInput{SessionRef: f.scope.Ref(f.session, 1), BranchRef: f.scope.Ref(f.branch, 1), ExpectedBranchRevision: 1, ContentRef: f.upload(t, "原分支目标"), AttachmentRefs: []api.ContentRef{}, PolicyRef: f.policy, Budget: []api.Amount{{Unit: "USD", Value: "20"}}, TaskDeadline: api.Time(time.Now().Add(20 * time.Minute))})
	var out interaction.SubmissionOutput
	if e := api.Decode(r.Output, &out); e != nil {
		t.Fatal(e)
	}
	session, e := f.s.ReadSession(f.ctx, f.store, f.scope, f.auth, f.session, interaction.ReadInput{})
	if e != nil {
		t.Fatal(e)
	}
	newBranch := api.NewID("branch")
	f.command(t, "session.branch.create", f.session, nil, interaction.CreateBranchInput{BranchID: newBranch, SourceBranchRef: f.scope.Ref(f.branch, session.Branches[0].Revision), ExpectedSourceRevision: session.Branches[0].Revision, ConfigRef: f.config})
	session, e = f.s.ReadSession(f.ctx, f.store, f.scope, f.auth, f.session, interaction.ReadInput{})
	if e != nil {
		t.Fatal(e)
	}
	revision := session.Session.Revision
	f.command(t, "session.branch.select", f.session, &revision, interaction.SelectBranchInput{BranchID: newBranch})
	before, e := f.s.ReadSession(f.ctx, f.store, f.scope, f.auth, f.session, interaction.ReadInput{})
	if e != nil {
		t.Fatal(e)
	}
	var newHead string
	for _, b := range before.Branches {
		if b.BranchID == newBranch {
			newHead = b.HeadMessageID
		}
	}
	f.auth.Roles = append(f.auth.Roles, "interaction_reply")
	f.command(t, "session.reply", f.session, nil, interaction.ReplyInput{SubmissionRef: out.SubmissionRef, ContentRef: f.upload(t, "迟到正式回复"), Role: "assistant"})
	after, e := f.s.ReadSession(f.ctx, f.store, f.scope, f.auth, f.session, interaction.ReadInput{})
	if e != nil {
		t.Fatal(e)
	}
	if after.Session.DefaultBranchID != newBranch {
		t.Fatal("late reply changed selected branch")
	}
	for _, b := range after.Branches {
		if b.BranchID == newBranch && b.HeadMessageID != newHead {
			t.Fatal("late reply appended to later selected branch")
		}
		if b.BranchID == f.branch && b.HeadMessageID == newHead {
			t.Fatal("original branch did not retain reply")
		}
	}
}
