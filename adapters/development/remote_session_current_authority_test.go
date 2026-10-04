package development

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/collaboration"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// 已接纳原send不能永久授权后续Job；parent AccessScope close重核当前来源。
func TestConfiguredRemoteSessionParentAccessCloseStopsAcceptedChildAdvance(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			original := remoteSessionCurrentWithdrawalOriginal(ctx, t, driver)
			a, b := original.parent, original.receiver
			remoteSessionWithdrawalClose(ctx, t, a, original.meta.Binding.AccessScopeRef, "child.new_goal")
			if _, err := a.app.ReadContent(ctx, a.app.Scope, a.app.UserAuth, original.meta.Binding.AccessScopeRef, "child.new_goal"); err == nil {
				t.Fatal("original parent actor still reads withdrawn AccessScope")
			} else {
				remoteSessionWithdrawalContentCause(t, err)
			}
			// 正文仍是同一可读普通历史；此次失去的是原parent AccessScope的当前许可。
			messages := readRemoteSessionMessages(ctx, t, b, original.meta.SessionRef.ObjectID)
			if len(messages) != 1 || messages[0].Seq != 1 || messages[0].ContentRef != original.history {
				t.Fatal("parent withdrawal replaced original Session history")
			}
			if _, err := b.app.ReadContent(ctx, b.app.Scope, b.app.UserAuth, original.history, "task.context"); err != nil {
				t.Fatal("parent AccessScope case changed local ordinary history readability", err)
			}
			cause := remoteSessionCurrentWithdrawalCause(t, remoteSessionCurrentWithdrawalAdvance(ctx, t, b, original.child.TaskID), "accepted child current parent/source gate")
			remoteSessionWithdrawalNoSnapshotOrAction(ctx, t, b, original.child.TaskID)
			remoteSessionCurrentWithdrawalUnchanged(ctx, t, original)
			t.Logf("original session=%s send=%s access=%s source=%s child=%s allocation=%s use=%s current_parent_entry_rejection=%s:%s snapshot=false operations=0", original.meta.SessionRef.ObjectID, original.send.CommandID, original.meta.Binding.AccessScopeRef.ContentID, original.history.ContentID, original.child.TaskID, original.delegation.AllocationRef.ObjectID, original.use.UseID, cause.Code, cause.Reason)
		})
	}
}

// 当前domainActor撤回后，仍有效的service不能替原User读Session或启动原Task。
func TestConfiguredRemoteSessionRevokedOriginalActorStopsHistoryAndChildAdvance(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			original := remoteSessionCurrentWithdrawalOriginal(ctx, t, driver)
			b := original.receiver
			actor := b.app.UserAuth
			if err := b.app.Identity.Revoke(ctx, actor); err != nil {
				t.Fatal("actual original receiver Identity.Revoke", err)
			}
			if err := b.app.Identity.CheckCurrent(ctx, b.app.ServiceAuth); err != nil {
				t.Fatal("actor-only withdrawal changed independent service credential", err)
			}
			_, sessionErr := b.app.queryAs(ctx, actor, "session.messages.list", original.meta.SessionRef.ObjectID, api.ListInput{Limit: 100})
			remoteSessionCurrentWithdrawalActorProbe(t, sessionErr, "original user public Session query")
			_, contentErr := b.app.ReadContent(ctx, b.app.Scope, actor, original.history, "task.context")
			remoteSessionCurrentWithdrawalActorProbe(t, contentErr, "original user exact history Content read")
			_, historyActor, historyErr := b.app.RemoteAgent.ChildSessionHistoryAuth(ctx, b.app.Scope, original.child)
			remoteSessionCurrentWithdrawalActorProbe(t, historyErr, "original current domain actor history-auth port")
			if !api.Equal(historyActor, runtime.Auth{}) {
				t.Error("revoked original actor history port returned substitute service identity")
			}
			// 本次注册入口可在checkSubmitterTx先拒；不宣称已经经过HistoryCompiler正文读取。
			advanceCause := remoteSessionCurrentWithdrawalCause(t, remoteSessionCurrentWithdrawalAdvance(ctx, t, b, original.child.TaskID), "original revoked-actor child advance entry")
			remoteSessionWithdrawalNoSnapshotOrAction(ctx, t, b, original.child.TaskID)
			remoteSessionCurrentWithdrawalUnchanged(ctx, t, original)
			t.Logf("original actor=%s generation=%d session=%s send=%s source=%s child=%s use=%s session_query_error=%v content_error=%v history_actor_error=%v advance_entry_rejection=%s:%s snapshot=false operations=0", actor.SubjectID, actor.CredentialGeneration, original.meta.SessionRef.ObjectID, original.send.CommandID, original.history.ContentID, original.child.TaskID, original.use.UseID, sessionErr, contentErr, historyErr, advanceCause.Code, advanceCause.Reason)
		})
	}
}

type remoteSessionCurrentWithdrawal struct {
	parent, receiver *configuredAgentEndpoint
	permission       api.ObjectRef
	history          api.ContentRef
	send             api.Command
	delegation       task.Delegation
	child            api.Task
	meta             collaboration.RemoteSessionContext
	sources          []api.SourceEvidence
	use              governance.UseReceipt
}

func remoteSessionCurrentWithdrawalOriginal(ctx context.Context, t *testing.T, driver string) remoteSessionCurrentWithdrawal {
	t.Helper()
	a, b, p := configuredAgentPairWithParentDriver(t, driver)
	permission := approveRemoteDelegationGrant(ctx, t, a, b.app.Scope.OwnerID)
	p = configureRemoteReuseProfile(ctx, t, a, b, p, []api.ObjectRef{permission})
	handle, _ := configureOriginalRemoteSession(ctx, t, a, b, p)
	history := publishRemoteSessionHistoryGoal(ctx, t, b, *handle.ChildSessionRef, "This exact ordinary history confers no continuing identity or Scope authority.")
	goal, parent := configuredAgentOriginalParent(ctx, t, a)
	send, d, child := sendRemoteSessionNewGoal(ctx, t, a, b, p, handle, parent, goal, permission, 1)
	meta, err := b.app.RemoteAgent.ChildSessionContext(ctx, b.app.Scope, child)
	if err != nil || meta == nil || meta.SourceCommandRef != a.app.Scope.Ref(send.CommandID, 1) || meta.SessionRef != *handle.ChildSessionRef || meta.HistoryCutoff != 1 || meta.Binding.AccessScopeRef != handle.AccessScopeRef {
		t.Fatal("accepted original Session/cutoff/send mapping", err)
	}
	facts, err := b.app.Task.ContextFacts(ctx, b.app.Store, b.app.Scope, b.app.ServiceAuth, child.TaskID)
	if err != nil {
		t.Fatal("original public SourceEvidence projection", err)
	}
	found := false
	for _, source := range facts.SourceRefs {
		if source.SourceKind == "user_input" && source.ContentRef == goal && source.SubmissionRef != nil && *source.SubmissionRef == a.app.Scope.Ref(send.CommandID, 1) {
			found = true
		}
	}
	if !found {
		t.Fatal("child Goal lost original immutable SourceSubmissionRef")
	}
	remoteSessionWithdrawalNoSnapshotOrAction(ctx, t, b, child.TaskID)
	use := remoteSessionWithdrawalUse(ctx, t, a, d, permission)
	remoteSessionCurrentWithdrawalIncoming(ctx, t, b, child.TaskID, d.AllocationRef)
	return remoteSessionCurrentWithdrawal{a, b, permission, history, send, d, child, *meta, append([]api.SourceEvidence{}, facts.SourceRefs...), use}
}

func remoteSessionCurrentWithdrawalIncoming(ctx context.Context, t *testing.T, b *configuredAgentEndpoint, id string, want api.ObjectRef) {
	t.Helper()
	var source api.ObjectRef
	found := false
	status, err := b.app.Store.Within(ctx, b.app.Scope, []string{"task"}, func(tx runtime.Tx) error {
		var readErr error
		source, found, readErr = b.app.Task.ReadIncomingSourceTx(ctx, tx, b.app.ServiceAuth, id)
		return readErr
	})
	if err != nil || status != runtime.Committed || !found || source != want {
		t.Fatal("original public incoming allocation identity", err)
	}
}

func remoteSessionCurrentWithdrawalAdvance(ctx context.Context, t *testing.T, b *configuredAgentEndpoint, id string) error {
	t.Helper()
	works, status, err := b.app.Store.Claim(ctx, b.app.Scope, api.NewID("boot"), []string{task.JobAdvance}, 1, time.Minute)
	if err != nil || status != runtime.Committed || len(works) != 1 || works[0].Job.SourceRef.ObjectID != id {
		t.Fatalf("exact original child Job claim: status=%s count=%d err=%v", status, len(works), err)
	}
	handler, ok := b.app.Registry.Job(works[0].Job.Kind)
	if !ok {
		t.Fatal("original registered child TaskAdvance handler missing")
	}
	return handler(ctx, b.app.Store, b.app.Scope, works[0])
}

func remoteSessionCurrentWithdrawalCause(t *testing.T, err error, boundary string) *api.Error {
	t.Helper()
	var cause *api.Error
	if !errors.As(err, &cause) || cause.Reason == "" || (cause.Code != "forbidden" && cause.Code != "invalid_state" && cause.Code != "gone") {
		t.Fatalf("%s must genuinely deny with typed current authority cause: %v", boundary, err)
	}
	t.Logf("actual boundary=%s rejection=%s:%s", boundary, cause.Code, cause.Reason)
	return cause
}

func remoteSessionCurrentWithdrawalUnchanged(ctx context.Context, t *testing.T, original remoteSessionCurrentWithdrawal) {
	t.Helper()
	a, b := original.parent, original.receiver
	child, err := b.app.Task.Read(ctx, b.app.Store, b.app.Scope, b.app.ServiceAuth, original.child.TaskID)
	if err != nil || child.TaskID != original.child.TaskID || child.GoalRef != original.child.GoalRef || child.GoalRevision != original.child.GoalRevision || child.ControlRevision != original.child.ControlRevision || child.PolicyRef != original.child.PolicyRef || child.Deadline != original.child.Deadline {
		t.Fatal("withdrawal replaced original Task/Goal/control/policy/deadline", err)
	}
	meta, err := b.app.RemoteAgent.ChildSessionContext(ctx, b.app.Scope, child)
	if err != nil || meta == nil || !api.Equal(*meta, original.meta) {
		t.Fatal("withdrawal replaced original immutable Session/send metadata", err)
	}
	facts, err := b.app.Task.ContextFacts(ctx, b.app.Store, b.app.Scope, b.app.ServiceAuth, original.child.TaskID)
	if err != nil || !api.Equal(facts.SourceRefs, original.sources) || len(facts.Operations) != 0 {
		t.Fatal("withdrawal replaced original SourceSubmission or admitted work", err)
	}
	remoteSessionCurrentWithdrawalIncoming(ctx, t, b, child.TaskID, original.delegation.AllocationRef)
	current, err := a.app.Task.DelegationRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, original.delegation.DelegationID)
	if err != nil || current.CommandRef != original.delegation.CommandRef || current.AllocationRef != original.delegation.AllocationRef || !api.Equal(current.ChildTaskRef, original.delegation.ChildTaskRef) || !api.Equal(current.PermissionRefs, original.delegation.PermissionRefs) {
		t.Fatal("withdrawal replaced original parent delegation/allocation/Grant", err)
	}
	grant := configuredAgentGrant(ctx, t, a, original.permission)
	if !grant.OnceConsumed {
		t.Fatal("withdrawal restored original once permission")
	}
	use := remoteSessionWithdrawalUse(ctx, t, a, current, original.permission)
	if use.UseID != original.use.UseID || use.IntentHash != original.use.IntentHash || use.RequestDigest != original.use.RequestDigest {
		t.Fatal("withdrawal replaced original approved Use/scope identity")
	}
}

// 一个公开Session查询若未拒是本测试真实失败；仍继续观察各独立原入口。
func remoteSessionCurrentWithdrawalActorProbe(t *testing.T, err error, boundary string) {
	t.Helper()
	var cause *api.Error
	if !errors.As(err, &cause) || cause.Reason == "" || cause.Code != "forbidden" {
		t.Errorf("%s did not reject revoked original actor with typed current credential cause: %v", boundary, err)
		return
	}
	t.Logf("actual actor boundary=%s rejection=%s:%s", boundary, cause.Code, cause.Reason)
}
