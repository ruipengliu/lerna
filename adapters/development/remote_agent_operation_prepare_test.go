package development

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/collaboration"
	execadapter "github.com/ruipengliu/lerna/adapters/execution"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// 真实原Decision接纳的首次动作经过独立Job；它不能继承该Decision的父证明。
// 这里仅验证首个inspect_file，不把一个动作当作完整子/父报告的通过证据。
func TestConfiguredRemoteFirstOriginalOperationUsesFreshParentScope(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver+"_parent_sqlite_child", func(t *testing.T) { runConfiguredRemoteFirstOriginalOperation(t, false, driver) })
	}
}

func TestConfiguredRemoteOriginalPreparedAttemptCannotStartAfterParentPause(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver+"_parent_sqlite_child", func(t *testing.T) { runConfiguredRemoteFirstOriginalOperation(t, true, driver) })
	}
}

func runConfiguredRemoteFirstOriginalOperation(t *testing.T, pausePrepared bool, driver string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	a, b, profile := configuredAgentPairWithParentDriver(t, driver)
	permission := approveRemoteDelegationGrant(ctx, t, a, b.app.Scope.OwnerID)
	profile = configureRemoteFileScope(ctx, t, a, b, profile, permission)
	goal, parent := configuredAgentOriginalParent(ctx, t, a)
	id := api.NewID("delegation")
	in := task.DelegateInput{DelegationID: id, ParentTaskRef: a.app.Scope.Ref(parent.TaskID, parent.Revision), ParentGoalRevision: parent.GoalRevision, GoalRef: goal, InputRefs: []api.ContentRef{}, AgentBindingRef: profile.Values.AgentBindingRef, PermissionRefs: []api.ObjectRef{permission}, Budget: []api.Amount{{Unit: "USD", Value: "2"}}, Deadline: api.Time(time.Now().Add(3 * time.Minute)), PolicyRef: b.app.TaskPolicy.PolicyRef, ReceiverID: b.app.Scope.OwnerID}
	original := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.app.Scope.OwnerID, CommandID: api.NewID("command"), Method: "collaboration.delegate", TargetID: id, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(in)}
	receipt, err := a.app.Dispatcher.Command(ctx, a.app.UserAuth, api.Raw(original))
	if err != nil || receipt.Stage != "applied" || receipt.Error != nil {
		t.Fatalf("original public delegation: %+v %v", receipt, err)
	}
	if !configuredAgentStep(ctx, t, a, task.JobDelegation) || !configuredAgentStep(ctx, t, b, collaboration.JobRemoteCreate) {
		t.Fatal("original child creation missing")
	}
	d, err := a.app.Task.DelegationRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, id)
	if err != nil {
		t.Fatal(err)
	}
	state, err := a.app.RemoteAgent.State(ctx, a.app.Scope, d)
	if err != nil || state.Task == nil {
		t.Fatalf("original independent child: %+v %v", state, err)
	}
	childID := state.Task.TaskID
	t.Logf("original parent=%s child=%s delegation=%s allocation=%s command=%s", parent.TaskID, childID, id, d.AllocationRef.ObjectID, original.CommandID)
	var intent task.OperationIntent
	// 不领取dispatch_operation：让本次独立入口亲自证明准备前的强门禁拒绝。
	for round := 0; round < 96; round++ {
		facts, err := b.app.Task.ContextFacts(ctx, b.app.Store, b.app.Scope, b.app.ServiceAuth, childID)
		if err != nil {
			t.Fatal(err)
		}
		if len(facts.Operations) > 0 {
			if len(facts.Operations) != 1 {
				t.Fatal("first operation fixture advanced past the original first action")
			}
			intent = facts.Operations[0].Intent
			break
		}
		if !configuredAgentStep(ctx, t, b, task.JobAdvance, task.JobDispatchDecision, brain.JobAdvance, task.JobCoverage, task.JobBilling) {
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(25 * time.Millisecond):
			}
		}
	}
	if intent.OperationID == "" || intent.AdmissionSourceKind != "decision" || intent.CapabilityRef != execadapter.FileReadCapability().Ref {
		t.Fatal("original first inspect action was not actually admitted")
	}
	flow := b.app.foreignContextFactory(ctx, runtime.Flow{Kind: "job", Scope: b.app.Scope, Auth: b.app.ServiceAuth})
	check := func(current context.Context) error {
		status, err := b.app.Store.Within(current, b.app.Scope, []string{"task", "collaboration", "content", "memory", "governance", "platform"}, func(tx runtime.Tx) error {
			return b.app.Task.CheckTaskCurrentTx(current, tx, b.app.ServiceAuth, childID, true)
		})
		if status == runtime.CommitUnknown {
			return runtime.ErrCommitUnknown
		}
		return err
	}
	before := check(flow)
	if !api.IsCode(before, "dependency_unavailable") || before.Error() != "dependency_unavailable: remote_parent_scope_required" {
		t.Fatalf("new entry borrowed a previous Job parent proof: %v", before)
	}
	requests := a.requests.Load() + b.requests.Load()
	changed := intent
	changed.GoalRevision++
	if _, err = b.app.prepareRemoteAgentOperationParent(flow, b.app.Scope, changed); !api.IsCode(err, "idempotency_conflict") {
		t.Fatalf("prepared a substituted original intent: %v", err)
	}
	if a.requests.Load()+b.requests.Load() != requests {
		t.Fatal("substituted intent contacted the original parent authority")
	}
	if err = (executionBridge{b.app}).PrepareDispatch(flow, b.app.Scope, intent); err != nil {
		t.Fatalf("original preparation: %v", err)
	}
	if err = check(flow); err != nil {
		t.Fatalf("current proof did not reach the original shared entry: %v", err)
	}
	if !configuredAgentStep(ctx, t, b, task.JobDispatchOperation) {
		t.Fatal("original first dispatch Job missing")
	}
	if pausePrepared {
		assertRemotePreparedAttemptStoppedByCurrentParent(ctx, t, a, b, parent.TaskID, intent.OperationID)
		return
	}
	if !configuredAgentStep(ctx, t, b, execution.RunJob) {
		t.Fatal("actual executor did not receive original Invoke")
	}
	var actual execution.OperationView
	for round := 0; round < 32; round++ {
		raw, err := b.app.queryAs(ctx, b.app.ServiceAuth, "execution.get", intent.OperationID, execution.OperationIDInput{OperationID: intent.OperationID})
		if err != nil || api.Decode(raw, &actual) != nil {
			t.Fatalf("original execution observation: %v", err)
		}
		if actual.Operation.Attempts.TotalCount != 0 && actual.Operation.ExecutionState == "closed" && actual.Operation.ResultRef != nil {
			break
		}
		if !configuredAgentStep(ctx, t, b, execution.RunJob, execution.ReconcileJob) {
			t.Fatal("first executor work stopped before an observed result")
		}
	}
	if actual.Operation.Attempts.TotalCount != 1 || actual.Operation.ResultRef == nil || actual.Operation.ExecutionState != "closed" || !actual.ActuallyStopped {
		t.Fatalf("first original action lacks actual physical completion: %+v", actual)
	}
	body, err := b.app.ReadContentBytes(ctx, b.app.Scope, b.app.ServiceAuth, *actual.Operation.ResultRef, "task.context", "cloud")
	if err != nil || api.Hash(body) != actual.Operation.ResultRef.Hash || uint64(len(body)) != actual.Operation.ResultRef.ByteLength {
		t.Fatalf("first original output bytes: %v", err)
	}
	var observed execadapter.FileReadResult
	if err = api.Decode(body, &observed); err != nil || observed.Path != "reports/parent.md" || observed.Version != "absent" || observed.DataBase64 != "" || observed.ObservedAt == "" {
		t.Fatalf("first native inspect did not observe the original absent file: %v", err)
	}
	t.Logf("actual first Operation=%s attempts=%d state=%s exact_result=%s bytes=%d hash=%s", intent.OperationID, actual.Operation.Attempts.TotalCount, actual.Operation.ExecutionState, actual.Operation.ResultRef.ContentID, len(body), actual.Operation.ResultRef.Hash)
}

// 这是受信Authority边界的领取丢失故障，不伪造成功许可或修改业务记录。
// 原VerifyStart已完整验真后、实际目标IO前返回领取丢失，保留原prepared责任。
type remotePreparedClaimFault struct {
	executionAuthority
	authorityObserved bool
}

func (f *remotePreparedClaimFault) VerifyStart(ctx context.Context, tx runtime.Tx, request execution.StartRequest, prepared execution.PreparedStart) (execution.StartPermit, error) {
	permit, err := f.executionAuthority.VerifyStart(ctx, tx, request, prepared)
	if err != nil {
		return permit, err
	}
	f.authorityObserved = prepared.AuthorityRevision != 0
	return permit, runtime.ErrClaimLost
}

func assertRemotePreparedAttemptStoppedByCurrentParent(ctx context.Context, t *testing.T, a, b *configuredAgentEndpoint, parentID, operationID string) {
	t.Helper()
	authority := &remotePreparedClaimFault{executionAuthority: executionAuthority{b.app}}
	service, err := execution.New(execution.Config{OwnerID: b.app.Scope.OwnerID, Content: executionContent{b.app}, Authority: authority, AuthorityParticipants: []string{"task", "governance", "content", "memory", "platform", "collaboration"}, Drivers: []execution.Driver{&execadapter.FileDriver{Files: b.app.Files, Content: executionContent{b.app}, Location: "cloud", ReadOnly: true}}, Location: "cloud"})
	if err != nil {
		t.Fatal(err)
	}
	registry := runtime.NewRegistry()
	registry.SetContextFactory(b.app.foreignContextFactory)
	if err = service.Register(registry); err != nil {
		t.Fatal(err)
	}
	works, status, err := b.app.Store.Claim(ctx, b.app.Scope, api.NewID("boot"), []string{execution.RunJob}, 1, time.Minute)
	if err != nil || status != runtime.Committed || len(works) != 1 || works[0].Job.SourceRef.ObjectID != operationID {
		t.Fatalf("original prepared fault claim: %v %v", status, err)
	}
	handler, ok := registry.Job(execution.RunJob)
	if !ok {
		t.Fatal("original prepared fault handler missing")
	}
	if err = handler(ctx, b.app.Store, b.app.Scope, works[0]); !errors.Is(err, runtime.ErrClaimLost) || !authority.authorityObserved {
		t.Fatalf("fault did not preserve an originally authorized prepared Attempt: %v", err)
	}
	read := func() execution.OperationView {
		t.Helper()
		raw, err := b.app.queryAs(ctx, b.app.ServiceAuth, "execution.get", operationID, execution.OperationIDInput{OperationID: operationID})
		var view execution.OperationView
		if err != nil || api.Decode(raw, &view) != nil {
			t.Fatalf("original prepared observation: %v", err)
		}
		return view
	}
	original := read()
	if len(original.Attempts.Items) != 1 || original.Attempts.Items[0].Phase != "prepared" || original.Attempts.Items[0].StartedAt != "" || original.Operation.ResultRef != nil {
		t.Fatal("fault did not retain exactly one original unstarted prepared Attempt")
	}
	if err = runtime.Finish(ctx, b.app.Store, b.app.Scope, []string{"execution"}, works[0], runtime.Waiting(time.Now().UTC()), nil); err != nil {
		t.Fatal(err)
	}
	parent, err := a.app.Task.Read(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, parentID)
	if err != nil {
		t.Fatal(err)
	}
	pause := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.app.Scope.OwnerID, CommandID: api.NewID("command"), Method: "task.pause", TargetID: parentID, ExpectedRevision: &parent.Revision, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(task.ControlInput{TaskID: parentID, Reason: "stop the original child before restoring its prepared Attempt"})}
	receipt, err := a.app.Dispatcher.Command(ctx, a.app.UserAuth, api.Raw(pause))
	if err != nil || receipt.Stage != "applied" || receipt.Error != nil {
		t.Fatalf("original parent pause: %+v %v", receipt, err)
	}
	requests := a.requests.Load() + b.requests.Load()
	b.reopenOriginal(t)
	if a.requests.Load()+b.requests.Load() != requests {
		t.Fatal("prepared recovery constructor contacted another authority")
	}
	works, status, err = b.app.Store.Claim(ctx, b.app.Scope, api.NewID("boot"), []string{execution.RunJob}, 1, time.Minute)
	if err != nil || status != runtime.Committed || len(works) != 1 || works[0].Job.SourceRef.ObjectID != operationID {
		t.Fatalf("original prepared recovery claim: %v %v", status, err)
	}
	handler, ok = b.app.Registry.Job(execution.RunJob)
	if !ok {
		t.Fatal("original prepared recovery handler missing")
	}
	parentRequests := a.requests.Load()
	if err = handler(ctx, b.app.Store, b.app.Scope, works[0]); err != nil {
		t.Fatalf("current parent denial did not close the original unstarted responsibility: %v", err)
	}
	if a.requests.Load() <= parentRequests {
		t.Fatal("restored prepared authority bypassed the current original parent query")
	}
	restored := read()
	if len(restored.Attempts.Items) != 1 || restored.Attempts.Items[0].AttemptID != original.Attempts.Items[0].AttemptID || restored.Attempts.Items[0].StartedAt != "" || restored.Operation.ResultRef != nil || restored.Operation.Effect != "not_started" || !restored.NewAttemptsClosed || !restored.ActuallyStopped || restored.Operation.ExecutionState != "closed" {
		t.Fatalf("prepared restore crossed the physical barrier or changed the original responsibility after parent pause: original_attempt=%s actual_attempts=%+v effect=%s result_ref=%+v new_attempts_closed=%t actually_stopped=%t execution_state=%s", original.Attempts.Items[0].AttemptID, restored.Attempts.Items, restored.Operation.Effect, restored.Operation.ResultRef, restored.NewAttemptsClosed, restored.ActuallyStopped, restored.Operation.ExecutionState)
	}
	// 查原开始屏障实际持久的拒绝原因，不把五秒窗自然到期当作父暂停门禁通过。
	var stopped struct {
		CancelReason string `json:"cancel_reason"`
	}
	if _, err = b.app.Store.Read(ctx, b.app.Scope, execution.Namespace+".operations", operationID, 0, &stopped); err != nil {
		t.Fatal(err)
	}
	if stopped.CancelReason != "forbidden: remote_parent_scope_denied" && stopped.CancelReason != "invalid_state: remote_parent_paused" {
		t.Fatalf("original prepared start did not consume the actual current parent pause: %s", stopped.CancelReason)
	}
	t.Logf("original prepared operation=%s attempt=%s reopened_same_attempt=true current_parent_queried=true no_start=true original_unstarted_closed=true actual_current_pause_cause=%s", operationID, original.Attempts.Items[0].AttemptID, stopped.CancelReason)
}
