package development

import (
	"context"
	"github.com/ruipengliu/lerna/adapters/collaboration"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/task"
	"testing"
	"time"
)

// 原 RuleEngine 请求必须由准确 request 版本消费；复用 Session 不改变来源身份。
func TestConfiguredRemoteChildAnswersOriginalRequestAndPreservesSubmission(t *testing.T) {
	verifyConfiguredRemoteChildAnswersOriginalRequestAndPreservesSubmission(t, "sqlite")
}

func verifyConfiguredRemoteChildAnswersOriginalRequestAndPreservesSubmission(t *testing.T, parentDriver string) {
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
	// 原公开 Job 保持准确接纳、输入消费与父方收束的独立责任。
	b.app.Store = &originalInputStoreObserver{Store: b.app.Store, t: t, seen: map[string]bool{}}
	b.app.Memory.Store = b.app.Store
	configuredAgentStep(ctx, t, a, task.JobChildTransfer)
	for ctx.Err() == nil {
		configuredAgentStep(ctx, t, a, collaboration.JobRemoteChildRequestPrepare, collaboration.JobRemoteInputSend, task.JobChildTransfer)
		configuredAgentStep(ctx, t, b, collaboration.JobRemoteInputReceive, task.JobInput)
		actual, err := b.app.Task.Read(ctx, b.app.Store, b.app.Scope, b.app.ServiceAuth, state.Task.TaskID)
		if err != nil {
			t.Fatal(err)
		}
		if actual.GoalRevision == originalGoalRevision+1 {
			parentReceipt, err := a.app.Dispatcher.Lookup(ctx, a.app.UserAuth, originalTransfer.CommandRef.ObjectID)
			if err != nil {
				t.Fatal(err)
			}
			if parentReceipt.Stage == "accepted" {
				time.Sleep(20 * time.Millisecond)
				continue
			}
			if parentReceipt.Stage != "applied" {
				t.Fatalf("original parent transfer decision: %s %v", parentReceipt.Stage, parentReceipt.Error)
			}
			var ack collaboration.RemoteInputAck
			if err = api.Decode(parentReceipt.Output, &ack); err != nil || ack.ReceiverCommandRef == nil || ack.Phase != "consumed" || ack.TransferID != originalTransfer.TransferID {
				t.Fatalf("original transfer receipt mapping: %v", err)
			}
			receiverCommand, err := b.app.Store.LookupCommand(ctx, b.app.Scope, ack.ReceiverCommandRef.ObjectID)
			if err != nil || receiverCommand.Receipt.Stage != "applied" || receiverCommand.Command.Method != "collaboration.input" || receiverCommand.Command.TargetID != actual.TaskID || receiverCommand.Command.CommandID == parentReceipt.CommandID || receiverCommand.Command.ExpiresAt != originalTransfer.ExpiresAt {
				t.Fatalf("genuine receiver consumption: stage=%s error=%v", receiverCommand.Receipt.Stage, err)
			}
			var packet collaboration.RemoteInputPacket
			if err = api.Decode(receiverCommand.Command.Payload, &packet); err != nil || packet.SourceSubmissionRef != originalTransfer.SourceSubmissionRef || packet.Input.ContentRef != amendment || packet.Input.ChildGoalRevision != originalGoalRevision || packet.Input.Kind != "answer_request" || packet.Input.RequestRef == nil || *packet.Input.RequestRef != request.RequestRef {
				t.Fatalf("original receiver input identity: %v", err)
			}
			var settledTransfer task.Transfer
			if _, err = a.app.Store.Read(ctx, a.app.Scope, "task.transfers", originalTransfer.TransferID, 0, &settledTransfer); err != nil {
				t.Fatal(err)
			}
			if settledTransfer.State != "applied" {
				time.Sleep(20 * time.Millisecond)
				continue
			}
			facts, err := b.app.Task.ContextFacts(ctx, b.app.Store, b.app.Scope, b.app.ServiceAuth, actual.TaskID)
			if err != nil {
				t.Fatal(err)
			}
			matched := false
			for _, source := range facts.SourceRefs {
				if source.ContentRef == amendment && source.SubmissionRef != nil && *source.SubmissionRef == request.RequestRef {
					matched = true
				}
			}
			if !matched {
				t.Fatal("original child request source was replaced")
			}
			consumed, err := b.app.Task.InputRequestRead(ctx, b.app.Store, b.app.Scope, b.app.UserAuth, request.Request.RequestID, 0)
			if err != nil || consumed.Request.State != "answered" || consumed.Request.ConsumedBy != receiverCommand.Command.CommandID {
				t.Fatalf("original request consumption: state=%s consumer=%s error=%v", consumed.Request.State, consumed.Request.ConsumedBy, err)
			}
			t.Logf("original parent=%s child=%s delegation=%s session=%s continuation=%s parent_input_command=%s receiver_input_command=%s parent_receipt=%s receiver_receipt=%s goal_revision=%d", parent.TaskID, actual.TaskID, id, handle.ChildSessionRef.ObjectID, continuation.CommandID, parentReceipt.CommandID, receiverCommand.Command.CommandID, parentReceipt.Stage, receiverCommand.Receipt.Stage, actual.GoalRevision)
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("original remote answer did not consume the original request and transfer")
}
