package development

import (
	"context"
	"github.com/ruipengliu/lerna/adapters/collaboration"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
	"testing"
	"time"
)

// 原 RuleEngine 请求必须由准确 request 版本消费；复用 Session 不改变来源身份。
func TestConfiguredRemoteChildStaleRequestSettlesOriginalParentCommand(t *testing.T) {
	verifyConfiguredRemoteChildStaleRequestSettlesOriginalParentCommand(t, "sqlite")
}

func verifyConfiguredRemoteChildStaleRequestSettlesOriginalParentCommand(t *testing.T, parentDriver string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	a, b, p := configuredAgentPairWithParentDriver(t, parentDriver)
	permission := approveRemoteDelegationGrant(ctx, t, a, b.app.Scope.OwnerID)
	p = configureRemoteFileScope(ctx, t, a, b, p, permission)
	values := p.Values
	values.MaterialPurposes = []string{"task.goal", "task.submit", "content.write", "brain.input", "task.context", "task.dispatch", "task.snapshot", "task.action", "execution.arguments", "brain.output", "task.attach_evidence", "task.complete", "execution_intent", "execution_arguments", "task.steer", "task.input"}
	var err error
	p, err = collaboration.NewRemoteAgentProfile(p.ProfileRef.ComponentID, p.ProfileRef.Version, values)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range []*configuredAgentEndpoint{a, b} {
		e.stopRun(t)
		if err = e.app.Close(); err != nil {
			t.Fatal(err)
		}
		e.config.RemoteAgent.Profiles = []collaboration.RemoteAgentProfile{p}
		e.app, err = OpenApp(ctx, e.config, false)
		if err != nil {
			t.Fatal(err)
		}
		e.persistPrivateConfiguration(t)
		e.startRun(t)
	}
	handle, _ := configureOriginalRemoteSession(ctx, t, a, b, p)
	_, parent := configuredAgentOriginalParent(ctx, t, a)
	goal, err := a.app.Publish(ctx, a.app.Scope, a.app.UserAuth, api.NewID("content"), "text/plain", []byte("Please ask me for the exact bounded report goal."), []api.ContentRef{}, []api.ContentRef{})
	if err != nil {
		t.Fatal(err)
	}
	id := api.NewID("delegation")
	input := task.DelegateInput{DelegationID: id, ParentTaskRef: a.app.Scope.Ref(parent.TaskID, parent.Revision), ParentGoalRevision: parent.GoalRevision, GoalRef: goal, InputRefs: []api.ContentRef{}, AgentBindingRef: p.Values.AgentBindingRef, PermissionRefs: []api.ObjectRef{permission}, Budget: []api.Amount{{Unit: "USD", Value: "2"}}, Deadline: api.Time(time.Now().Add(3 * time.Minute)), PolicyRef: b.app.TaskPolicy.PolicyRef, ReceiverID: b.app.Scope.OwnerID}
	send := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.app.Scope.OwnerID, CommandID: api.NewID("command"), Method: "child.send", TargetID: handle.ChildID, ExpectedRevision: &handle.Revision, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(task.ChildSendInput{ChildID: handle.ChildID, Mode: "new_goal", Delegation: &input})}
	receipt, err := a.app.Dispatcher.Command(ctx, a.app.UserAuth, api.Raw(send))
	if err != nil || receipt.Stage != "applied" {
		t.Fatalf("original new_goal: stage=%s error=%v rejection=%v", receipt.Stage, err, receipt.Error)
	}
	configuredAgentStep(ctx, t, a, task.JobDelegation)
	configuredAgentStep(ctx, t, b, collaboration.JobRemoteCreate)
	delegation, err := a.app.Task.DelegationRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, id)
	if err != nil {
		t.Fatal(err)
	}
	state, err := a.app.RemoteAgent.State(ctx, a.app.Scope, delegation)
	if err != nil || state.Task == nil {
		t.Fatalf("original child mapping: %+v %v", state.Fact, err)
	}
	// receiver 已创建 Task 仍不代表父方已消费原 create 回执。
	// 原 Delegation Job 必须耐久合并 child mapping 后才能转交续写。
	for delegation.ChildTaskRef == nil && ctx.Err() == nil {
		configuredAgentStep(ctx, t, a, task.JobDelegation)
		delegation, err = a.app.Task.DelegationRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, id)
		if err != nil {
			t.Fatal(err)
		}
		if delegation.ChildTaskRef == nil {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if delegation.ChildTaskRef == nil || state.Fact.ChildTaskRef == nil || *delegation.ChildTaskRef != *state.Fact.ChildTaskRef {
		t.Fatal("original parent mapping did not settle")
	}
	handle, err = a.app.Task.ChildRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, handle.ChildID)
	if err != nil {
		t.Fatal(err)
	}
	var request task.InputRequestView
	for ctx.Err() == nil {
		configuredAgentStep(ctx, t, b, task.JobAdvance, task.JobDispatchDecision, task.JobCoverage, brain.JobAdvance, proofJob)
		page, e := b.app.Task.InputRequestList(ctx, b.app.Store, b.app.Scope, b.app.UserAuth, task.InputRequestListInput{TaskID: state.Task.TaskID, Limit: 100})
		if e != nil {
			t.Fatal(e)
		}
		if !page.Exhausted || page.Partial || len(page.Gaps) != 0 {
			t.Fatal("original request collection incomplete")
		}
		if len(page.Items) == 1 && page.Items[0].Request.State == "pending" {
			request = page.Items[0]
			break
		}
		if len(page.Items) > 1 {
			t.Fatal("original child has unexpected requests")
		}
		time.Sleep(20 * time.Millisecond)
	}
	originalGoalRevision := state.Task.GoalRevision
	if request.Request.RequestID == "" || request.Request.Purpose != "clarify_goal" || request.Request.GoalRevision == nil || *request.Request.GoalRevision != originalGoalRevision || request.Request.TargetRef.ObjectID != state.Task.TaskID {
		t.Fatal("original rule request identity missing")
	}
	amendment, err := a.app.Publish(ctx, a.app.Scope, a.app.UserAuth, api.NewID("content"), "application/json", api.Raw(brain.GoalSpec{Kind: "report", Title: "Clarified bounded child report", Body: "Use only the originally approved child report path.", SavePath: "reports/parent.md"}), []api.ContentRef{}, []api.ContentRef{})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := api.NewValidator(brain.GoalSchema())
	if err != nil {
		t.Fatal(err)
	}
	if err = validator.Validate(api.Raw(brain.GoalSpec{Kind: "report", Title: "Clarified bounded child report", Body: "Use only the originally approved child report path.", SavePath: "reports/parent.md"})); err != nil {
		t.Fatal(err)
	}
	continuation := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.app.Scope.OwnerID, CommandID: api.NewID("command"), Method: "child.send", TargetID: handle.ChildID, ExpectedRevision: &handle.Revision, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(task.ChildSendInput{ChildID: handle.ChildID, Mode: "continue_existing", ExpectedActiveDelegationRef: handle.ActiveDelegationRef, DelegationRef: handle.ActiveDelegationRef, ParentGoalRevision: parent.GoalRevision, Kind: "answer_request", RequestRef: &request.RequestRef, AnswerRef: &amendment})}
	receipt, err = a.app.Dispatcher.Command(ctx, a.app.UserAuth, api.Raw(continuation))
	if err != nil || receipt.Stage != "applied" {
		t.Fatalf("original continue_existing: stage=%s error=%v rejection=%v", receipt.Stage, err, receipt.Error)
	}
	transfers, err := a.app.Store.List(ctx, a.app.Scope, "task.transfers", handle.ChildID, "", 129)
	if err != nil || len(transfers) != 1 {
		t.Fatalf("original transfer collection: count=%d error=%v", len(transfers), err)
	}
	var originalTransfer task.Transfer
	if err = transfers[0].Decode(&originalTransfer); err != nil {
		t.Fatal(err)
	}
	if originalTransfer.SourceSubmissionRef != a.app.Scope.Ref(continuation.CommandID, 1) {
		t.Fatal("parent transfer replaced the original submission")
	}
	// 正确领取victim原Job，保留其原1m Claim；另一公开继续回答先真实消费同一请求。
	works, status, err := a.app.Store.Claim(ctx, a.app.Scope, api.NewID("boot"), []string{task.JobChildTransfer}, 1, time.Minute)
	if err != nil || status != runtime.Committed || len(works) != 1 || works[0].Job.SourceRef.ObjectID != originalTransfer.TransferID {
		t.Fatal("original queued victim claim missing", status, err)
	}
	victim := works[0]
	handle, err = a.app.Task.ChildRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, handle.ChildID)
	if err != nil {
		t.Fatal(err)
	}
	winner := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.app.Scope.OwnerID, CommandID: api.NewID("command"), Method: "child.send", TargetID: handle.ChildID, ExpectedRevision: &handle.Revision, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(task.ChildSendInput{ChildID: handle.ChildID, Mode: "continue_existing", ExpectedActiveDelegationRef: handle.ActiveDelegationRef, DelegationRef: handle.ActiveDelegationRef, ParentGoalRevision: parent.GoalRevision, Kind: "answer_request", RequestRef: &request.RequestRef, AnswerRef: &amendment})}
	accepted, err := a.app.Dispatcher.Command(ctx, a.app.UserAuth, api.Raw(winner))
	if err != nil || accepted.Stage != "applied" {
		t.Fatalf("genuine request winner queued: stage=%s error=%v rejection=%v", accepted.Stage, err, accepted.Error)
	}
	rows, err := a.app.Store.List(ctx, a.app.Scope, "task.transfers", handle.ChildID, "", 129)
	if err != nil || len(rows) != 2 {
		t.Fatal("two original public transfers missing", err)
	}
	var winnerTransfer task.Transfer
	for _, row := range rows {
		var candidate task.Transfer
		if err = row.Decode(&candidate); err != nil {
			t.Fatal(err)
		}
		if candidate.SourceSubmissionRef == a.app.Scope.Ref(winner.CommandID, 1) {
			winnerTransfer = candidate
		}
	}
	if winnerTransfer.TransferID == "" || winnerTransfer.TransferID == originalTransfer.TransferID || winnerTransfer.CommandRef == originalTransfer.CommandRef {
		t.Fatal("winner replaced victim identity")
	}
	var winnerReceiver api.ObjectRef
	for ctx.Err() == nil {
		configuredAgentStep(ctx, t, a, task.JobChildTransfer, collaboration.JobRemoteChildRequestPrepare, collaboration.JobRemoteInputSend)
		configuredAgentStep(ctx, t, b, collaboration.JobRemoteInputReceive, task.JobInput)
		req, err := b.app.Task.InputRequestRead(ctx, b.app.Store, b.app.Scope, b.app.UserAuth, request.Request.RequestID, 0)
		if err != nil {
			t.Fatal(err)
		}
		if req.Request.State == "answered" {
			rc, err := a.app.Dispatcher.Lookup(ctx, a.app.UserAuth, winnerTransfer.CommandRef.ObjectID)
			if err != nil {
				t.Fatal(err)
			}
			if rc.Stage == "applied" {
				var ack collaboration.RemoteInputAck
				if api.Decode(rc.Output, &ack) != nil || ack.ReceiverCommandRef == nil || ack.Phase != "consumed" || req.Request.ConsumedBy != ack.ReceiverCommandRef.ObjectID {
					t.Fatal("genuine request winner receipt mapping missing")
				}
				winnerReceiver = *ack.ReceiverCommandRef
				var settled task.Transfer
				if _, err = a.app.Store.Read(ctx, a.app.Scope, "task.transfers", winnerTransfer.TransferID, 0, &settled); err != nil {
					t.Fatal(err)
				}
				if settled.State == "applied" {
					break
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if winnerReceiver.ObjectID == "" {
		t.Fatal("genuine original request winner did not consume")
	}
	if err = a.app.Store.CheckClaim(ctx, a.app.Scope, victim.Claim); err != nil {
		t.Fatal("victim original Claim expired before request winner", err)
	}
	handler, ok := a.app.Registry.Job(task.JobChildTransfer)
	if !ok {
		t.Fatal("original transfer handler missing")
	}
	jobErr := handler(ctx, a.app.Store, a.app.Scope, victim)
	if jobErr != nil {
		t.Logf("actual original stale-request handler error=%v", jobErr)
	}
	// 原父命令先接纳，再由其准确 Request 准备 Job 持久决定。
	for ctx.Err() == nil {
		configuredAgentStep(ctx, t, a, collaboration.JobRemoteChildRequestPrepare, task.JobChildTransfer)
		settled, lookupErr := a.app.Dispatcher.Lookup(ctx, a.app.UserAuth, originalTransfer.CommandRef.ObjectID)
		if lookupErr != nil {
			t.Fatal("accepted original stale transfer is not queryable", lookupErr)
		}
		if settled.Stage == "rejected" {
			var transfer task.Transfer
			if _, err = a.app.Store.Read(ctx, a.app.Scope, "task.transfers", originalTransfer.TransferID, 0, &transfer); err != nil {
				t.Fatal(err)
			}
			if transfer.State == "rejected" {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	parentReceipt, err := a.app.Dispatcher.Lookup(ctx, a.app.UserAuth, originalTransfer.CommandRef.ObjectID)
	if err != nil || parentReceipt.CommandID != originalTransfer.CommandRef.ObjectID || parentReceipt.Stage != "rejected" || parentReceipt.Error == nil {
		t.Fatalf("stale queued request lacks genuine original parent rejection: stage=%s lookup_error=%v handler_error=%v", parentReceipt.Stage, err, jobErr)
	}
	var actualTransfer task.Transfer
	if _, err = a.app.Store.Read(ctx, a.app.Scope, "task.transfers", originalTransfer.TransferID, 0, &actualTransfer); err != nil || actualTransfer.State != "rejected" || actualTransfer.RejectedReceipt == nil || !api.Equal(*actualTransfer.RejectedReceipt, parentReceipt) {
		t.Fatal("original stale transfer did not consume true parent receipt", err)
	}
	t.Logf("original request=%s winner_parent=%s winner_receiver=%s stale_parent_command=%s transfer=%s rejection=%s/%s", request.Request.RequestID, winnerTransfer.CommandRef.ObjectID, winnerReceiver.ObjectID, parentReceipt.CommandID, actualTransfer.TransferID, parentReceipt.Error.Code, parentReceipt.Error.Reason)
}
