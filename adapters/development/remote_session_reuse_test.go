package development

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/collaboration"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/internal/interaction"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// 真实Session消息通过公开submit_goal保存；不插入history、Snapshot、Grant或Effect。
func TestConfiguredRemoteChildHistoryFreezesOriginalSendCutoffInSnapshot(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
			defer cancel()
			a, b, p := configuredAgentPairWithParentDriver(t, driver)
			permission := approveRemoteDelegationGrant(ctx, t, a, b.app.Scope.OwnerID)
			p = configureRemoteReuseProfile(ctx, t, a, b, p, []api.ObjectRef{permission})
			handle, _ := configureOriginalRemoteSession(ctx, t, a, b, p)
			before := publishRemoteSessionHistoryGoal(ctx, t, b, *handle.ChildSessionRef, "Ordinary past text cannot grant a new action or decide Task success.")
			goal, parent := configuredAgentOriginalParent(ctx, t, a)
			original, d, child := sendRemoteSessionNewGoal(ctx, t, a, b, p, handle, parent, goal, permission, 1)
			// 原send后消息增加，不改其已冻结cutoff；不让Interaction投递另一个Task替代本目标。
			later := publishRemoteSessionHistoryGoal(ctx, t, b, *handle.ChildSessionRef, "Later ordinary text must remain outside the original cutoff.")
			messages := readRemoteSessionMessages(ctx, t, b, handle.ChildSessionRef.ObjectID)
			if len(messages) != 2 || messages[0].ContentRef != before || messages[1].ContentRef != later || messages[0].Seq != 1 || messages[1].Seq != 2 {
				t.Fatal("actual public Session message collection changed")
			}
			configuredAgentStep(ctx, t, b, task.JobAdvance)
			snapshot := readRemoteSessionChildSnapshot(ctx, t, b, child.TaskID)
			if snapshot.GoalRef != goal || snapshot.TaskRef.ObjectID != child.TaskID || !remoteSessionContainsRef(snapshot.ProcessedSources, before) || remoteSessionContainsRef(snapshot.ProcessedSources, later) || !remoteSessionContainsRef(snapshot.MaterialRefs, before) || remoteSessionContainsRef(snapshot.MaterialRefs, later) {
				t.Fatal("original new_goal omitted approved history or borrowed later/current history")
			}
			packetCount := 0
			for _, ref := range snapshot.MaterialRefs {
				if ref.MediaType != "application/vnd.harness.child-history+json" {
					continue
				}
				packetCount++
				body, err := b.app.ReadContentBytes(ctx, b.app.Scope, b.app.ServiceAuth, ref, "task.context", "cloud")
				if err != nil {
					t.Fatalf("original ordinary history packet read: %v", err)
				}
				var packet struct {
					Kind          string                             `json:"kind"`
					FormatVersion uint64                             `json:"format_version"`
					Context       collaboration.RemoteSessionContext `json:"context"`
					Messages      []api.Message                      `json:"messages"`
				}
				if err = api.Decode(body, &packet); err != nil {
					t.Fatalf("original complete ordinary history packet decode: %v", err)
				}
				if packet.Kind != "ordinary_session_history/1" || packet.FormatVersion != 1 || packet.Context.ChildRef != a.app.Scope.Ref(handle.ChildID, *original.ExpectedRevision) || !api.Equal(packet.Context.Binding, a.config.RemoteAgent.Sessions[0]) || packet.Context.SourceCommandRef != d.CommandRef || packet.Context.SourceCommandRef != a.app.Scope.Ref(original.CommandID, 1) || packet.Context.SessionRef != *handle.ChildSessionRef || packet.Context.HistoryCutoff != 1 || len(packet.Messages) != 1 || !api.Equal(packet.Messages[0], messages[0]) {
					t.Fatal("ordinary history packet lost exact original Session/send/cutoff")
				}
			}
			if packetCount != 1 {
				t.Fatal("original child Snapshot has no unique ordinary history packet")
			}
			facts, err := b.app.Task.ContextFacts(ctx, b.app.Store, b.app.Scope, b.app.ServiceAuth, child.TaskID)
			if err != nil || len(facts.Operations) != 0 {
				t.Fatal("ordinary history admitted an action before any original proposal")
			}
			for _, source := range facts.SourceRefs {
				if source.ContentRef == before || source.ContentRef == later {
					t.Fatal("ordinary history replaced original Goal SourceEvidence")
				}
			}
			grant := configuredAgentGrant(ctx, t, a, permission)
			if !grant.OnceConsumed || !api.Equal(grant.Reserved, []api.Amount{{Unit: "USD", Value: "2"}}) {
				t.Fatal("history substituted original approved once responsibility")
			}
			replay, err := a.app.Dispatcher.Command(ctx, a.app.UserAuth, api.Raw(original))
			if err != nil || replay.Stage != "applied" {
				t.Fatal("original history send replay changed")
			}
			t.Logf("original session=%s send=%s delegation=%s child=%s snapshot=%s cutoff=1 history=%s later=%s", handle.ChildSessionRef.ObjectID, original.CommandID, d.DelegationID, child.TaskID, snapshot.SnapshotID, before.ContentID, later.ContentID)
		})
	}
}

func TestConfiguredRemoteChildSecondGoalNeedsRealClosureAndFreshOnceAllocation(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			a, b, p := configuredAgentPairWithParentDriver(t, driver)
			firstPermission := approveRemoteDelegationGrant(ctx, t, a, b.app.Scope.OwnerID)
			nextPermission := approveRemoteDelegationGrant(ctx, t, a, b.app.Scope.OwnerID)
			p = configureRemoteReuseProfile(ctx, t, a, b, p, []api.ObjectRef{firstPermission, nextPermission})
			handle, _ := configureOriginalRemoteSession(ctx, t, a, b, p)
			firstHistory := publishRemoteSessionHistoryGoal(ctx, t, b, *handle.ChildSessionRef, "The first goal may read this ordinary history only.")
			goal, parent := configuredAgentOriginalParent(ctx, t, a)
			original, first, firstChild := sendRemoteSessionNewGoal(ctx, t, a, b, p, handle, parent, goal, firstPermission, 1)
			laterHistory := publishRemoteSessionHistoryGoal(ctx, t, b, *handle.ChildSessionRef, "A second goal needs a fresh approved once permission and budget.")
			handle, err := a.app.Task.ChildRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, handle.ChildID)
			if err != nil {
				t.Fatal(err)
			}
			parent, err = a.app.Task.Read(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, parent.TaskID)
			if err != nil {
				t.Fatal(err)
			}
			next := remoteSessionGoalCommand(a, b, p, handle, parent, goal, nextPermission, 2)
			refusal, err := a.app.Dispatcher.Command(ctx, a.app.UserAuth, api.Raw(next))
			if err != nil || refusal.Stage != "rejected" || refusal.Error == nil || refusal.Error.Code != "invalid_state" || refusal.Error.Reason != "previous_goal_open" {
				t.Fatalf("second goal bypassed original closure: %v %+v", err, refusal)
			}
			nextGrant := configuredAgentGrant(ctx, t, a, nextPermission)
			if nextGrant.OnceConsumed || len(nextGrant.Reserved) != 0 {
				t.Fatal("blocked replacement consumed the next once Grant")
			}
			// 公开取消原child只改变工作控制；必须继续取得负责方真实Closure/效果集合证明。
			configuredAgentControl(ctx, t, b, firstChild.TaskID, "cancel")
			for ctx.Err() == nil {
				configuredAgentStep(ctx, t, b, task.JobControl, task.JobAdvance, task.JobBilling, task.JobAllocation)
				state, err := a.app.RemoteAgent.State(ctx, a.app.Scope, first)
				if err != nil {
					t.Fatal(err)
				}
				if state.Fact.GoalWorkClosed && state.Fact.EffectsClosed {
					configuredAgentStep(ctx, t, a, task.JobDelegation)
					first, err = a.app.Task.DelegationRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, first.DelegationID)
					if err != nil {
						t.Fatal(err)
					}
					if first.GoalWorkClosed && first.EffectsClosed {
						break
					}
				}
				select {
				case <-ctx.Done():
					t.Fatal("original goal/effect proof did not close", ctx.Err())
				case <-time.After(20 * time.Millisecond):
				}
			}
			if !first.GoalWorkClosed || !first.EffectsClosed {
				t.Fatal("cancelled status alone cannot replace original closure proof")
			}
			handle, err = a.app.Task.ChildRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, handle.ChildID)
			if err != nil {
				t.Fatal(err)
			}
			parent, err = a.app.Task.Read(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, parent.TaskID)
			if err != nil {
				t.Fatal(err)
			}
			secondSend, second, secondChild := sendRemoteSessionNewGoal(ctx, t, a, b, p, handle, parent, goal, nextPermission, 2)
			if first.DelegationID == second.DelegationID || first.AllocationRef == second.AllocationRef || first.CommandRef == second.CommandRef || firstChild.TaskID == secondChild.TaskID || second.CommandRef != a.app.Scope.Ref(secondSend.CommandID, 1) || !api.Equal(second.PermissionRefs, []api.ObjectRef{nextPermission}) || !api.Equal(second.Budget, []api.Amount{{Unit: "USD", Value: "2"}}) {
				t.Fatal("Session reuse inherited old Task/Delegation/allocation/once authority")
			}
			for _, permission := range []api.ObjectRef{firstPermission, nextPermission} {
				grant := configuredAgentGrant(ctx, t, a, permission)
				if !grant.OnceConsumed {
					t.Fatal("replacement restored/reused an old once authorization")
				}
			}
			for _, pair := range []struct {
				Delegation task.Delegation
				Permission api.ObjectRef
			}{{first, firstPermission}, {second, nextPermission}} {
				useID := stableID("use", "remote-agent/"+pair.Delegation.DelegationID)
				raw, err := a.app.query(ctx, "grant.use.get", useID, governance.IDInput{ID: useID})
				var use governance.UseReceipt
				if err != nil || api.Decode(raw, &use) != nil || use.Decision != "allowed" || use.TargetKind != "delegation" || use.TargetRef != a.app.Scope.Ref(pair.Delegation.DelegationID, 1) || !api.Equal(use.GrantRefs, []api.ObjectRef{pair.Permission}) {
					t.Fatal("reused Session inherited a prior Grant Use or allocation responsibility")
				}
			}
			raw, err := a.app.query(ctx, "budget.read", second.AllocationRef.ObjectID, task.BudgetReadInput{AllocationRef: &second.AllocationRef})
			var budget task.BudgetReadResponse
			if err != nil || api.Decode(raw, &budget) != nil || budget.Allocation == nil || budget.Allocation.AllocationID != second.AllocationRef.ObjectID || budget.Allocation.ParentTaskRef.ObjectID != parent.TaskID || !api.Equal(budget.Allocation.Limits, []api.Amount{{Unit: "USD", Value: "2"}}) {
				t.Fatal("second goal lacks its own original budget allocation")
			}
			raw, err = a.app.query(ctx, "child.wait", handle.ChildID, task.ChildWaitInput{ChildID: handle.ChildID, DelegationRef: a.app.Scope.Ref(first.DelegationID, 1), WaitFor: "goal_closed", TimeoutMS: 0})
			var wait task.ChildWaitOutput
			if err != nil || api.Decode(raw, &wait) != nil || !wait.ConditionMet || wait.Delegation.DelegationID != first.DelegationID || wait.Delegation.ChildTaskRef == nil || wait.Delegation.ChildTaskRef.ObjectID != firstChild.TaskID {
				t.Fatal("current active pointer replaced original wait/send mapping")
			}
			replay, err := a.app.Dispatcher.Command(ctx, a.app.UserAuth, api.Raw(original))
			var oldOutput task.ChildOutput
			if err != nil || replay.Stage != "applied" || api.Decode(replay.Output, &oldOutput) != nil || oldOutput.DelegationRef == nil || oldOutput.DelegationRef.ObjectID != first.DelegationID {
				t.Fatal("old send replay returned replacement mapping")
			}
			current, err := a.app.Task.ChildRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, handle.ChildID)
			if err != nil || current.ActiveDelegationRef == nil || current.ActiveDelegationRef.ObjectID != second.DelegationID || !api.Equal(current.ChildSessionRef, handle.ChildSessionRef) {
				t.Fatal("new goal created a replacement Session or lost current mapping")
			}
			configuredAgentStep(ctx, t, b, task.JobAdvance)
			snapshot := readRemoteSessionChildSnapshot(ctx, t, b, secondChild.TaskID)
			if snapshot.GoalRef != goal || !remoteSessionContainsRef(snapshot.MaterialRefs, firstHistory) || !remoteSessionContainsRef(snapshot.MaterialRefs, laterHistory) {
				t.Fatal("second send did not use its own approved cutoff")
			}
			sessions, err := b.app.Interaction.ListSessions(ctx, b.app.Store, b.app.Scope, b.app.UserAuth, api.ListInput{Limit: 100})
			if err != nil || !sessions.Exhausted || sessions.Partial || len(sessions.Gaps) != 0 {
				t.Fatal("reused Session collection is incomplete")
			}
			count := 0
			for _, s := range sessions.Items {
				if s.SessionID == handle.ChildSessionRef.ObjectID {
					count++
				}
			}
			if count != 1 {
				t.Fatal("reused original Session is not unique")
			}
			t.Logf("original session=%s first_send=%s first_delegation=%s first_task=%s first_allocation=%s second_send=%s second_delegation=%s second_task=%s second_allocation=%s", handle.ChildSessionRef.ObjectID, original.CommandID, first.DelegationID, firstChild.TaskID, first.AllocationRef.ObjectID, secondSend.CommandID, second.DelegationID, secondChild.TaskID, second.AllocationRef.ObjectID)
		})
	}
}

func configureRemoteReuseProfile(ctx context.Context, t *testing.T, a, b *configuredAgentEndpoint, p collaboration.RemoteAgentProfile, permissions []api.ObjectRef) collaboration.RemoteAgentProfile {
	t.Helper()
	p = configureRemoteFileScope(ctx, t, a, b, p, permissions[0])
	values := p.Values
	values.PermissionRefs = append([]api.ObjectRef{}, permissions...)
	// 固定16用途；仅用历史元数据的child.new_goal替换本片不需要的need_context。
	values.MaterialPurposes = []string{"task.goal", "task.submit", "content.write", "brain.input", "task.context", "task.delegate", "task.steer", "task.snapshot", "task.action", "execution.arguments", "brain.output", "task.attach_evidence", "task.complete", "task.input", "child.new_goal", "task.accept_result"}
	var err error
	p, err = collaboration.NewRemoteAgentProfile(api.NewID("agent"), "1.0.0", values)
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
	return p
}
func publishRemoteSessionHistoryGoal(ctx context.Context, t *testing.T, b *configuredAgentEndpoint, session api.ObjectRef, text string) api.ContentRef {
	t.Helper()
	view, err := b.app.Interaction.ReadSession(ctx, b.app.Store, b.app.Scope, b.app.UserAuth, session.ObjectID, interaction.ReadInput{})
	if err != nil {
		t.Fatal(err)
	}
	branches, err := b.app.Interaction.ListBranches(ctx, b.app.Store, b.app.Scope, b.app.UserAuth, session.ObjectID, api.ListInput{Limit: 100})
	if err != nil || !branches.Exhausted || branches.Partial || len(branches.Gaps) != 0 || len(branches.Items) != 1 {
		t.Fatal("original public Session branch is incomplete")
	}
	ref, err := b.app.Publish(ctx, b.app.Scope, b.app.UserAuth, api.NewID("content"), "text/plain", []byte(text), []api.ContentRef{}, []api.ContentRef{})
	if err != nil {
		t.Fatal(err)
	}
	branch := branches.Items[0]
	input := interaction.GoalInput{SessionRef: b.app.Scope.Ref(session.ObjectID, view.Session.Revision), BranchRef: b.app.Scope.Ref(branch.BranchID, branch.Revision), ExpectedBranchRevision: branch.Revision, ContentRef: ref, AttachmentRefs: []api.ContentRef{}, PolicyRef: b.app.TaskPolicy.PolicyRef, Budget: []api.Amount{{Unit: "USD", Value: "2"}}, TaskDeadline: api.Time(time.Now().Add(3 * time.Minute))}
	cmd := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: b.app.Scope.OwnerID, CommandID: api.NewID("command"), Method: "session.submit_goal", TargetID: session.ObjectID, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(input)}
	receipt, err := b.app.Dispatcher.Command(ctx, b.app.UserAuth, api.Raw(cmd))
	if err != nil || receipt.Stage != "applied" || receipt.Error != nil {
		t.Fatalf("actual Session history submission: %v %+v", err, receipt)
	}
	messages := readRemoteSessionMessages(ctx, t, b, session.ObjectID)
	if len(messages) == 0 || messages[len(messages)-1].ContentRef != ref {
		t.Fatal("public submit_goal did not persist original history message")
	}
	return ref
}
func readRemoteSessionMessages(ctx context.Context, t *testing.T, b *configuredAgentEndpoint, id string) []api.Message {
	t.Helper()
	raw, err := b.app.queryAs(ctx, b.app.UserAuth, "session.messages.list", id, api.ListInput{Limit: 100})
	var page api.Page[api.Message]
	if err != nil {
		t.Fatalf("original user Session message query: %v", err)
	}
	if err = api.Decode(raw, &page); err != nil {
		t.Fatalf("original Session message page decode: %v", err)
	}
	if !page.Exhausted || page.Partial || page.NextCursor != "" || len(page.Gaps) != 0 {
		t.Fatalf("original Session message page incomplete: count=%d exhausted=%t partial=%t gaps=%d cursor=%t", len(page.Items), page.Exhausted, page.Partial, len(page.Gaps), page.NextCursor != "")
	}
	sort.Slice(page.Items, func(i, j int) bool { return page.Items[i].Seq < page.Items[j].Seq })
	return page.Items
}
func remoteSessionGoalCommand(a, b *configuredAgentEndpoint, p collaboration.RemoteAgentProfile, h task.ChildHandle, parent api.Task, goal api.ContentRef, permission api.ObjectRef, cutoff uint64) api.Command {
	input := task.DelegateInput{DelegationID: api.NewID("delegation"), ParentTaskRef: a.app.Scope.Ref(parent.TaskID, parent.Revision), ParentGoalRevision: parent.GoalRevision, GoalRef: goal, InputRefs: []api.ContentRef{}, AgentBindingRef: p.Values.AgentBindingRef, PermissionRefs: []api.ObjectRef{permission}, Budget: []api.Amount{{Unit: "USD", Value: "2"}}, Deadline: api.Time(time.Now().Add(3 * time.Minute)), PolicyRef: b.app.TaskPolicy.PolicyRef, ReceiverID: b.app.Scope.OwnerID}
	return api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.app.Scope.OwnerID, CommandID: api.NewID("command"), Method: "child.send", TargetID: h.ChildID, ExpectedRevision: &h.Revision, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(task.ChildSendInput{ChildID: h.ChildID, Mode: "new_goal", ExpectedActiveDelegationRef: h.ActiveDelegationRef, Delegation: &input, HistoryCutoff: cutoff})}
}
func sendRemoteSessionNewGoal(ctx context.Context, t *testing.T, a, b *configuredAgentEndpoint, p collaboration.RemoteAgentProfile, h task.ChildHandle, parent api.Task, goal api.ContentRef, permission api.ObjectRef, cutoff uint64) (api.Command, task.Delegation, api.Task) {
	t.Helper()
	cmd := remoteSessionGoalCommand(a, b, p, h, parent, goal, permission, cutoff)
	receipt, err := a.app.Dispatcher.Command(ctx, a.app.UserAuth, api.Raw(cmd))
	var output task.ChildOutput
	if err != nil || receipt.Stage != "applied" || receipt.Error != nil || api.Decode(receipt.Output, &output) != nil || output.DelegationRef == nil {
		t.Fatalf("original new_goal: %v %+v", err, receipt)
	}
	for ctx.Err() == nil {
		configuredAgentStep(ctx, t, a, task.JobDelegation)
		configuredAgentStep(ctx, t, b, collaboration.JobRemoteCreate)
		d, err := a.app.Task.DelegationRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, output.DelegationRef.ObjectID)
		if err != nil {
			t.Fatal(err)
		}
		// 每次只领取一个原Job；旧委派的收尾仍可先于本次新创建。
		// 等待本方准确新映射，不向尚未接纳本次创建的receiver查询State。
		if d.ChildTaskRef == nil {
			select {
			case <-ctx.Done():
				t.Fatal("original Session child mapping did not settle", ctx.Err())
			case <-time.After(20 * time.Millisecond):
			}
			continue
		}
		state, err := a.app.RemoteAgent.State(ctx, a.app.Scope, d)
		if err != nil {
			t.Fatal(err)
		}
		if d.ChildTaskRef != nil && state.Task != nil && d.ChildTaskRef.ObjectID == state.Task.TaskID {
			return cmd, d, *state.Task
		}
		select {
		case <-ctx.Done():
			t.Fatal("original Session child mapping did not settle", ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Fatal("original new_goal did not settle")
	return api.Command{}, task.Delegation{}, api.Task{}
}
func readRemoteSessionChildSnapshot(ctx context.Context, t *testing.T, b *configuredAgentEndpoint, id string) api.Snapshot {
	t.Helper()
	var view task.TaskSnapshotView
	found := false
	status, err := b.app.Store.Within(ctx, b.app.Scope, []string{"task"}, func(tx runtime.Tx) error {
		var e error
		view, found, e = b.app.Task.LatestTaskSnapshotTx(ctx, tx, b.app.ServiceAuth, id)
		return e
	})
	if err != nil || status != runtime.Committed || !found {
		t.Fatalf("actual original child Snapshot: %v %v", status, err)
	}
	return view.Snapshot
}
func remoteSessionContainsRef(refs []api.ContentRef, want api.ContentRef) bool {
	for _, ref := range refs {
		if ref == want {
			return true
		}
	}
	return false
}
