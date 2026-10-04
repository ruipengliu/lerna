package development

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// 原AccessScope在新send前真实撤回；原Session/history的可读性不能授权新目标。
func TestConfiguredRemoteSessionClosedAccessScopeRejectsOriginalNewGoal(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			a, b, p := configuredAgentPairWithParentDriver(t, driver)
			permission := approveRemoteDelegationGrant(ctx, t, a, b.app.Scope.OwnerID)
			p = configureRemoteReuseProfile(ctx, t, a, b, p, []api.ObjectRef{permission})
			handle, _ := configureOriginalRemoteSession(ctx, t, a, b, p)
			history := publishRemoteSessionHistoryGoal(ctx, t, b, *handle.ChildSessionRef, "Ordinary Session history does not restore withdrawn authority.")
			goal, parent := configuredAgentOriginalParent(ctx, t, a)
			baseline := remoteSessionWithdrawalTasks(ctx, t, b)
			access := a.config.RemoteAgent.Sessions[0].AccessScopeRef
			if access != handle.AccessScopeRef {
				t.Fatal("original Session AccessScope binding changed")
			}
			remoteSessionWithdrawalClose(ctx, t, a, access, "child.new_goal")
			messages := readRemoteSessionMessages(ctx, t, b, handle.ChildSessionRef.ObjectID)
			if len(messages) != 1 || messages[0].Seq != 1 || messages[0].ContentRef != history {
				t.Fatal("withdrawal replaced original ordinary history")
			}
			send := remoteSessionGoalCommand(a, b, p, handle, parent, goal, permission, 1)
			var input task.ChildSendInput
			if err := api.Decode(send.Payload, &input); err != nil || input.Delegation == nil {
				t.Fatal("original new_goal input unavailable", err)
			}
			requestsA, requestsB := a.requests.Load(), b.requests.Load()
			receipt, err := a.app.Dispatcher.Command(ctx, a.app.UserAuth, api.Raw(send))
			if err != nil || receipt.Stage != "rejected" || receipt.Error == nil {
				t.Fatalf("withdrawn AccessScope did not genuinely reject original send: err=%v receipt=%+v", err, receipt)
			}
			cause := remoteSessionWithdrawalContentCause(t, receipt.Error)
			durable, err := a.app.Dispatcher.Lookup(ctx, a.app.UserAuth, send.CommandID)
			if err != nil || !api.Equal(durable, receipt) {
				t.Fatal("original Runtime rejection was not durable", err)
			}
			if _, err = a.app.Task.DelegationRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, input.Delegation.DelegationID); !api.IsCode(err, "not_found") {
				t.Fatal("withdrawn AccessScope created a new Delegation", err)
			}
			useID := stableID("use", "remote-agent/"+input.Delegation.DelegationID)
			if _, err = a.app.query(ctx, "grant.use.get", useID, governance.IDInput{ID: useID}); !api.IsCode(err, "not_found") {
				t.Fatal("withdrawn AccessScope created new remote once Use", err)
			}
			grant := configuredAgentGrant(ctx, t, a, permission)
			if grant.OnceConsumed || len(grant.Reserved) != 0 {
				t.Fatal("rejected original new_goal consumed or reserved fresh once Grant")
			}
			current, err := a.app.Task.ChildRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, handle.ChildID)
			if err != nil || !api.Equal(current, handle) {
				t.Fatal("rejected original send changed Session handle/mapping", err)
			}
			after := remoteSessionWithdrawalTasks(ctx, t, b)
			if !api.Equal(after.Items, baseline.Items) {
				t.Fatal("rejected original send created receiver work")
			}
			if a.requests.Load() != requestsA || b.requests.Load() != requestsB {
				t.Fatal("rejected local AccessScope gate issued extra peer RPC")
			}
			t.Logf("original access=%s send=%s delegation=%s history=%s rejection=%s:%s no_new_use=true no_new_child=true extra_peer_rpc=0", access.ContentID, send.CommandID, input.Delegation.DelegationID, history.ContentID, cause.Code, cause.Reason)
		})
	}
}

// 原send/Task已经接纳；撤回精确cutoff正文后由真实原Job入口重核当前读许可。
func TestConfiguredRemoteSessionClosedCutoffContentStopsOriginalChildAdvance(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			a, b, p := configuredAgentPairWithParentDriver(t, driver)
			permission := approveRemoteDelegationGrant(ctx, t, a, b.app.Scope.OwnerID)
			p = configureRemoteReuseProfile(ctx, t, a, b, p, []api.ObjectRef{permission})
			handle, _ := configureOriginalRemoteSession(ctx, t, a, b, p)
			history := publishRemoteSessionHistoryGoal(ctx, t, b, *handle.ChildSessionRef, "Only this exact original cutoff message is ordinary history.")
			goal, parent := configuredAgentOriginalParent(ctx, t, a)
			send, d, child := sendRemoteSessionNewGoal(ctx, t, a, b, p, handle, parent, goal, permission, 1)
			meta, err := b.app.RemoteAgent.ChildSessionContext(ctx, b.app.Scope, child)
			if err != nil || meta == nil || meta.SourceCommandRef != a.app.Scope.Ref(send.CommandID, 1) || meta.SessionRef != *handle.ChildSessionRef || meta.HistoryCutoff != 1 || meta.Binding.AccessScopeRef != handle.AccessScopeRef {
				t.Fatal("original accepted history mapping changed", err)
			}
			remoteSessionWithdrawalNoSnapshotOrAction(ctx, t, b, child.TaskID)
			use := remoteSessionWithdrawalUse(ctx, t, a, d, permission)
			remoteSessionWithdrawalClose(ctx, t, b, history, "task.context")
			messages := readRemoteSessionMessages(ctx, t, b, handle.ChildSessionRef.ObjectID)
			if len(messages) != 1 || messages[0].Seq != 1 || messages[0].ContentRef != history {
				t.Fatal("close replaced original public message collection")
			}
			if _, err = b.app.ReadContent(ctx, b.app.Scope, b.app.UserAuth, history, "task.context"); err == nil {
				t.Fatal("original domain actor can still read withdrawn cutoff content")
			}
			remoteSessionWithdrawalContentCause(t, err)
			works, status, err := b.app.Store.Claim(ctx, b.app.Scope, api.NewID("boot"), []string{task.JobAdvance}, 1, time.Minute)
			if err != nil || status != runtime.Committed || len(works) != 1 || works[0].Job.SourceRef.ObjectID != child.TaskID {
				t.Fatalf("exact original child advance claim: status=%s count=%d err=%v", status, len(works), err)
			}
			handler, ok := b.app.Registry.Job(works[0].Job.Kind)
			if !ok {
				t.Fatal("original registered child advance handler missing")
			}
			advanceErr := handler(ctx, b.app.Store, b.app.Scope, works[0])
			if advanceErr == nil {
				t.Fatal("original child advance borrowed revoked history read authority")
			}
			cause := remoteSessionWithdrawalContentCause(t, advanceErr)
			remoteSessionWithdrawalNoSnapshotOrAction(ctx, t, b, child.TaskID)
			grant := configuredAgentGrant(ctx, t, a, permission)
			if !grant.OnceConsumed {
				t.Fatal("withdrawn history restored an already consumed once Grant")
			}
			currentUse := remoteSessionWithdrawalUse(ctx, t, a, d, permission)
			if currentUse.UseID != use.UseID || currentUse.IntentHash != use.IntentHash || currentUse.RequestDigest != use.RequestDigest {
				t.Fatal("history rejection replaced original Use/scope responsibility")
			}
			current, err := a.app.Task.DelegationRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, d.DelegationID)
			if err != nil || current.CommandRef != d.CommandRef || current.AllocationRef != d.AllocationRef || !api.Equal(current.ChildTaskRef, d.ChildTaskRef) {
				t.Fatal("history withdrawal replaced original Task/delegation/allocation mapping", err)
			}
			t.Logf("original session=%s send=%s cutoff=1 source=%s child=%s delegation=%s allocation=%s use=%s advance_rejection=%s:%s snapshot=false operations=0 once_consumed=true", handle.ChildSessionRef.ObjectID, send.CommandID, history.ContentID, child.TaskID, d.DelegationID, d.AllocationRef.ObjectID, use.UseID, cause.Code, cause.Reason)
		})
	}
}

func remoteSessionWithdrawalClose(ctx context.Context, t *testing.T, e *configuredAgentEndpoint, ref api.ContentRef, purpose string) {
	t.Helper()
	// 公开Metadata端口按当前真实主体/DataPolicy取得control revision，不猜1或造holder。
	metadata, err := e.app.Memory.SourcePolicySnapshot(ctx, e.app.Scope, e.app.UserAuth, ref, purpose, "cloud")
	if err != nil || metadata.ContentRef != ref || metadata.ControlRevision == 0 {
		t.Fatal("original current content metadata", err)
	}
	expected := metadata.ControlRevision
	cmd := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: e.app.Scope.OwnerID, CommandID: api.NewID("command"), Method: "content.close", TargetID: ref.ContentID, ExpectedRevision: &expected, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(memory.CloseInput{ContentRef: ref, Reason: "Withdraw this exact original Session source without replacing Goal or authority."})}
	receipt, err := e.app.Dispatcher.Command(ctx, e.app.UserAuth, api.Raw(cmd))
	var output memory.CloseOutput
	if err != nil || receipt.Stage != "applied" || receipt.Error != nil || api.Decode(receipt.Output, &output) != nil || output.State != "closed" || output.ControlRevision != expected+1 {
		t.Fatalf("actual public original content.close: %v %+v", err, receipt)
	}
}

func remoteSessionWithdrawalContentCause(t *testing.T, err error) *api.Error {
	t.Helper()
	var cause *api.Error
	if !errors.As(err, &cause) || cause.Reason == "" || (cause.Code != "forbidden" && cause.Code != "invalid_state" && cause.Code != "gone") {
		t.Fatalf("original current content gate must return typed business cause: %v", err)
	}
	// 输出真实typedcause；此草案不能预先把静态source_closed猜测当行为证据。
	t.Logf("actual original content gate code=%s reason=%s", cause.Code, cause.Reason)
	return cause
}

func remoteSessionWithdrawalTasks(ctx context.Context, t *testing.T, e *configuredAgentEndpoint) api.Page[api.Task] {
	t.Helper()
	page, err := e.app.Task.List(ctx, e.app.Store, e.app.Scope, e.app.UserAuth, task.TaskListInput{Limit: 100})
	if err != nil || !page.Exhausted || page.Partial || len(page.Gaps) != 0 || page.NextCursor != "" {
		t.Fatal("original complete public Task collection", err)
	}
	return page
}

func remoteSessionWithdrawalNoSnapshotOrAction(ctx context.Context, t *testing.T, e *configuredAgentEndpoint, id string) {
	t.Helper()
	found := false
	status, err := e.app.Store.Within(ctx, e.app.Scope, []string{"task"}, func(tx runtime.Tx) error {
		var snapshotErr error
		_, found, snapshotErr = e.app.Task.LatestTaskSnapshotTx(ctx, tx, e.app.ServiceAuth, id)
		return snapshotErr
	})
	if err != nil || status != runtime.Committed || found {
		t.Fatalf("original child published Snapshot after withdrawal: found=%t status=%s err=%v", found, status, err)
	}
	facts, err := e.app.Task.ContextFacts(ctx, e.app.Store, e.app.Scope, e.app.ServiceAuth, id)
	if err != nil || len(facts.Operations) != 0 {
		t.Fatal("original child admitted an action after withdrawal", err)
	}
}

func remoteSessionWithdrawalUse(ctx context.Context, t *testing.T, a *configuredAgentEndpoint, d task.Delegation, permission api.ObjectRef) governance.UseReceipt {
	t.Helper()
	id := stableID("use", "remote-agent/"+d.CreationKey)
	raw, err := a.app.query(ctx, "grant.use.get", id, governance.IDInput{ID: id})
	var use governance.UseReceipt
	if err != nil || api.Decode(raw, &use) != nil || use.UseID != id || use.Decision != "allowed" || use.TargetKind != "delegation" || use.TargetRef != a.app.Scope.Ref(d.DelegationID, 1) || !api.Equal(use.GrantRefs, []api.ObjectRef{permission}) {
		t.Fatal("original actual remote Use/scope identity", err)
	}
	return use
}
