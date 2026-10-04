package development

import (
	"context"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/collaboration"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
)

// 首条公开 tracer 固定两个真实 owner，跨 owner child.create 应持久接纳并由原命令创建 Session。
func TestConfiguredRemoteChildHandleCreatesOriginalSession(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	a, b, p := configuredAgentPair(t)
	access, err := a.app.Publish(ctx, a.app.Scope, a.app.UserAuth, api.NewID("content"), "text/plain", []byte("Only the paired child history and exact agent binding."), []api.ContentRef{}, []api.ContentRef{})
	if err != nil {
		t.Fatal(err)
	}
	binding := collaboration.RemoteSessionBinding{ProfileRef: p.ProfileRef, SessionConfigRef: p.Values.PolicyRef, AgentBindingRef: p.Values.AgentBindingRef, InstallLockRef: p.Values.InstallLockRef, AccessScopeRef: access}
	for _, e := range []*configuredAgentEndpoint{a, b} {
		e.stopRun(t)
		if err = e.app.Close(); err != nil {
			t.Fatal(err)
		}
		e.config.RemoteAgent.Sessions = []collaboration.RemoteSessionBinding{binding}
		e.app, err = OpenApp(ctx, e.config, false)
		if err != nil {
			t.Fatal(err)
		}
		e.startRun(t)
	}
	childID := api.NewID("child")
	command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.app.Scope.OwnerID, CommandID: api.NewID("command"), Method: "child.create", TargetID: childID, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(task.ChildCreateInput{ChildID: childID, SessionOwnerID: b.app.Scope.OwnerID, SessionConfigRef: p.Values.PolicyRef, AgentBindingRef: p.Values.AgentBindingRef, InstallLockRef: p.Values.InstallLockRef, AccessScopeRef: access, PrepareDeadline: api.Time(time.Now().Add(time.Minute))})}
	receipt, err := a.app.Dispatcher.Command(ctx, a.app.UserAuth, api.Raw(command))
	if err != nil || receipt.Stage != "accepted" {
		t.Fatalf("paired child.create must persist original preparation: stage=%s error=%v rejection=%v", receipt.Stage, err, receipt.Error)
	}

	for ctx.Err() == nil {
		configuredAgentStep(ctx, t, a, task.JobChildPrepare)
		configuredAgentStep(ctx, t, b, collaboration.JobRemoteSessionCreate)
		h, e := a.app.Task.ChildRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, childID)
		if e != nil {
			t.Fatal(e)
		}
		if h.State == "open" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	handle, err := a.app.Task.ChildRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, childID)
	if err != nil || handle.State != "open" || handle.ChildSessionRef == nil || handle.ChildSessionRef.OwnerID != b.app.Scope.OwnerID {
		t.Fatalf("original Session not open: %+v %v", handle, err)
	}
}

func configureOriginalRemoteSession(ctx context.Context, t *testing.T, a, b *configuredAgentEndpoint, p collaboration.RemoteAgentProfile) (task.ChildHandle, api.Command) {
	t.Helper()
	access, err := a.app.Publish(ctx, a.app.Scope, a.app.UserAuth, api.NewID("content"), "text/plain", []byte("Reuse only this approved child history and exact configured agent."), []api.ContentRef{}, []api.ContentRef{})
	if err != nil {
		t.Fatal(err)
	}
	binding := collaboration.RemoteSessionBinding{ProfileRef: p.ProfileRef, SessionConfigRef: p.Values.PolicyRef, AgentBindingRef: p.Values.AgentBindingRef, InstallLockRef: p.Values.InstallLockRef, AccessScopeRef: access}
	for _, e := range []*configuredAgentEndpoint{a, b} {
		e.stopRun(t)
		if err = e.app.Close(); err != nil {
			t.Fatal(err)
		}
		e.config.RemoteAgent.Sessions = []collaboration.RemoteSessionBinding{binding}
		e.app, err = OpenApp(ctx, e.config, false)
		if err != nil {
			t.Fatal(err)
		}
		e.persistPrivateConfiguration(t)
		e.startRun(t)
	}
	id := api.NewID("child")
	command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.app.Scope.OwnerID, CommandID: api.NewID("command"), Method: "child.create", TargetID: id, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(task.ChildCreateInput{ChildID: id, SessionOwnerID: b.app.Scope.OwnerID, SessionConfigRef: binding.SessionConfigRef, AgentBindingRef: binding.AgentBindingRef, InstallLockRef: binding.InstallLockRef, AccessScopeRef: binding.AccessScopeRef, PrepareDeadline: api.Time(time.Now().Add(time.Minute))})}
	receipt, err := a.app.Dispatcher.Command(ctx, a.app.UserAuth, api.Raw(command))
	if err != nil || receipt.Stage != "accepted" {
		t.Fatalf("original Session preparation: stage=%s error=%v rejection=%v", receipt.Stage, err, receipt.Error)
	}
	for ctx.Err() == nil {
		configuredAgentStep(ctx, t, a, task.JobChildPrepare)
		configuredAgentStep(ctx, t, b, collaboration.JobRemoteSessionCreate)
		h, e := a.app.Task.ChildRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, id)
		if e != nil {
			t.Fatal(e)
		}
		if h.State == "open" && h.ChildSessionRef != nil {
			return h, command
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("original Session preparation did not finish")
	return task.ChildHandle{}, api.Command{}
}

// 公开 continue_existing 是原转交责任，不能由 Session 复用隐式扩大目标。
func TestConfiguredRemoteChildContinueSteersOriginalGoalAndPreservesSubmission(t *testing.T) {
	verifyConfiguredRemoteChildContinueSteersOriginalGoalAndPreservesSubmission(t, "sqlite")
}

func verifyConfiguredRemoteChildContinueSteersOriginalGoalAndPreservesSubmission(t *testing.T, parentDriver string) {
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
	goal, parent := configuredAgentOriginalParent(ctx, t, a)
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
	amendment, err := a.app.Publish(ctx, a.app.Scope, a.app.UserAuth, api.NewID("content"), "text/plain", []byte("Within the same bounded report goal, include the verified source file name."), []api.ContentRef{}, []api.ContentRef{})
	if err != nil {
		t.Fatal(err)
	}
	originalGoalRevision := state.Task.GoalRevision
	continuation := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.app.Scope.OwnerID, CommandID: api.NewID("command"), Method: "child.send", TargetID: handle.ChildID, ExpectedRevision: &handle.Revision, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(task.ChildSendInput{ChildID: handle.ChildID, Mode: "continue_existing", ExpectedActiveDelegationRef: handle.ActiveDelegationRef, DelegationRef: handle.ActiveDelegationRef, ParentGoalRevision: parent.GoalRevision, Kind: "steer", ChildGoalRevision: originalGoalRevision, ContentRef: &amendment})}
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
	// 此处是原 adapter seam；当前恒 unsupported 的缺口必须由实际公开 Job 暴露。
	b.app.Store = &originalInputStoreObserver{Store: b.app.Store, t: t, seen: map[string]bool{}}
	b.app.Memory.Store = b.app.Store
	configuredAgentStep(ctx, t, a, task.JobChildTransfer)
	for ctx.Err() == nil {
		configuredAgentStep(ctx, t, a, collaboration.JobRemoteInputSend, task.JobChildTransfer)
		configuredAgentStep(ctx, t, b, collaboration.JobRemoteInputReceive, task.JobSteer)
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
			if err = api.Decode(receiverCommand.Command.Payload, &packet); err != nil || packet.SourceSubmissionRef != originalTransfer.SourceSubmissionRef || packet.Input.ContentRef != amendment || packet.Input.ChildGoalRevision != originalGoalRevision || packet.Input.Kind != "steer" {
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
				if source.ContentRef == amendment && source.SubmissionRef != nil && *source.SubmissionRef == a.app.Scope.Ref(continuation.CommandID, 1) {
					matched = true
				}
			}
			if !matched {
				t.Fatal("original parent continuation source was replaced")
			}
			t.Logf("original parent=%s child=%s delegation=%s session=%s continuation=%s parent_input_command=%s receiver_input_command=%s parent_receipt=%s receiver_receipt=%s goal_revision=%d", parent.TaskID, actual.TaskID, id, handle.ChildSessionRef.ObjectID, continuation.CommandID, parentReceipt.CommandID, receiverCommand.Command.CommandID, parentReceipt.Stage, receiverCommand.Receipt.Stage, actual.GoalRevision)
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("original remote steer did not consume the original transfer")
}
