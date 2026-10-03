package development

import (
	"context"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/collaboration"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

func TestRemoteActionGateRetainsExactPairedResourceScope(t *testing.T) {
	owner, tenant := api.NewID("owner"), api.NewID("tenant")
	capability, resource := component("file.read"), component("managed-files")
	binding := api.ObjectRef{TenantID: tenant, OwnerID: owner, ObjectID: api.NewID("binding"), Revision: 1}
	original := collaboration.RemoteActionScope{CapabilityRef: capability, BindingRef: binding, Resources: []string{"managed-files"}, ResourceRefs: []api.ComponentRef{resource}, Actions: []string{"file.read"}, Recipient: owner, Location: "cloud"}
	admission := collaboration.RemoteParentAdmission{CapabilityRefs: []api.ComponentRef{capability}, ActionScopes: []collaboration.RemoteActionScope{original}}
	descriptor := actionDescriptor{Capability: execution.Capability{Ref: capability}, BindingRef: binding, Resources: []string{"managed-files"}, Actions: []string{"file.read"}, Recipient: owner, Location: "cloud"}
	if !remoteResourcesSubset(original, original) || !remoteActionScope(admission, descriptor) {
		t.Fatal("explicit paired resource refused by parent action gate")
	}
	changed := original
	changed.ResourceRefs = append([]api.ComponentRef{}, original.ResourceRefs...)
	changed.ResourceRefs[0].Digest = api.Hash([]byte("other original resource version"))
	if remoteResourcesSubset(changed, original) {
		t.Fatal("recursive scope substituted a resource version behind the same Grant name")
	}
	changed.ResourceRefs = nil
	if remoteResourcesSubset(changed, original) {
		t.Fatal("recursive action scope omitted its exact resource mapping")
	}
	for _, change := range []func(*actionDescriptor){
		func(d *actionDescriptor) { d.Resources = []string{"other-files"} },
		func(d *actionDescriptor) { d.BindingRef.Revision++ },
		func(d *actionDescriptor) { d.Recipient = api.NewID("owner") },
		func(d *actionDescriptor) { d.Location = "device" },
	} {
		candidate := descriptor
		change(&candidate)
		if remoteActionScope(admission, candidate) {
			t.Fatal("action gate expanded exact original resource/binding/receiver/location")
		}
	}
}

func configuredAgentControl(ctx context.Context, t *testing.T, e *configuredAgentEndpoint, id, action string) api.Task {
	t.Helper()
	actual, err := e.app.Task.Read(ctx, e.app.Store, e.app.Scope, e.app.UserAuth, id)
	if err != nil {
		t.Fatal(err)
	}
	command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: e.app.Scope.OwnerID, CommandID: api.NewID("command"), Method: "task." + action, TargetID: id, ExpectedRevision: &actual.Revision, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(task.ControlInput{TaskID: id, Reason: "explicit original parent control"})}
	receipt, err := e.app.Dispatcher.Command(ctx, e.app.UserAuth, api.Raw(command))
	if err != nil || receipt.Stage != "applied" || receipt.Error != nil {
		t.Fatalf("original %s control: %+v %v", action, receipt, err)
	}
	actual, err = e.app.Task.Read(ctx, e.app.Store, e.app.Scope, e.app.UserAuth, id)
	if err != nil {
		t.Fatal(err)
	}
	return actual
}

func configuredAgentGrant(ctx context.Context, t *testing.T, e *configuredAgentEndpoint, ref api.ObjectRef) governance.GrantRecord {
	t.Helper()
	raw, err := clarificationQuery(ctx, e.app, "grant.read", ref.ObjectID, governance.IDInput{ID: ref.ObjectID})
	var out governance.GrantRecord
	if err != nil || api.Decode(raw, &out) != nil {
		t.Fatalf("original delegation Grant: %v", err)
	}
	return out
}

func configuredAgentPublishOriginalClosureProof(ctx context.Context, t *testing.T, e *configuredAgentEndpoint, ref api.ContentRef) {
	t.Helper()
	for attempts := 0; attempts < 64; attempts++ {
		body, err := e.app.Memory.ReadBytes(ctx, e.app.Scope, e.app.ServiceAuth, ref, "content.read", "cloud")
		if err == nil {
			if uint64(len(body)) != ref.ByteLength || api.Hash(body) != ref.Hash {
				t.Fatal("original Closure proof bytes changed")
			}
			t.Logf("actual original Closure proof=%s bytes=%d hash=%s published by original platform Proof Job", ref.ContentID, len(body), ref.Hash)
			return
		}
		if !api.IsCode(err, "not_found") {
			t.Fatalf("original proof current permission: %v", err)
		}
		if !configuredAgentStep(ctx, t, e, proofJob) {
			t.Fatal("original platform proof publication responsibility missing")
		}
	}
	t.Fatal("bounded original platform proof publication did not finish")
}

// 金额来自明确受信的原计费源夹具，不代表真实供应商账单。本测试验证
// 真实双HTTPS/SQL原账务和完整child签名：父终态不抹账，once不退款。
// 不运行Decision/Operation，避免把模型质量或报告成功当成费用证据。
func TestConfiguredRemoteOriginalGrantSettlesSignedLateClosureAfterParentTerminal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	a, b, profile := configuredAgentPair(t)
	permission := approveRemoteDelegationGrant(ctx, t, a, b.app.Scope.OwnerID)
	profile = configureRemoteFileScope(ctx, t, a, b, profile, permission)
	goal, parent := configuredAgentOriginalParent(ctx, t, a)
	id := api.NewID("delegation")
	in := task.DelegateInput{DelegationID: id, ParentTaskRef: a.app.Scope.Ref(parent.TaskID, parent.Revision), ParentGoalRevision: parent.GoalRevision, GoalRef: goal, InputRefs: []api.ContentRef{}, AgentBindingRef: profile.Values.AgentBindingRef, PermissionRefs: []api.ObjectRef{permission}, Budget: []api.Amount{{Unit: "USD", Value: "2"}}, Deadline: api.Time(time.Now().Add(3 * time.Minute)), PolicyRef: b.app.TaskPolicy.PolicyRef, ReceiverID: b.app.Scope.OwnerID}
	original := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.app.Scope.OwnerID, CommandID: api.NewID("command"), Method: "collaboration.delegate", TargetID: id, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(in)}
	receipt, err := a.app.Dispatcher.Command(ctx, a.app.UserAuth, api.Raw(original))
	if err != nil || receipt.Stage != "applied" || receipt.Error != nil {
		t.Fatalf("original real delegation Use: %+v %v", receipt, err)
	}
	reserved := configuredAgentGrant(ctx, t, a, permission)
	if !reserved.OnceConsumed || !api.Equal(reserved.Reserved, []api.Amount{{Unit: "USD", Value: "2"}}) {
		t.Fatalf("delegate did not consume/reserve original Grant: %+v", reserved)
	}
	if !configuredAgentStep(ctx, t, a, task.JobDelegation) || !configuredAgentStep(ctx, t, b, collaboration.JobRemoteCreate) {
		t.Fatal("original remote creation missing")
	}
	d, err := a.app.Task.DelegationRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, id)
	if err != nil {
		t.Fatal(err)
	}
	state, err := a.app.RemoteAgent.State(ctx, a.app.Scope, d)
	if err != nil || state.Task == nil {
		t.Fatalf("actual original child: %+v %v", state, err)
	}
	child := *state.Task
	t.Logf("original parent=%s child=%s delegation=%s allocation=%s command=%s Grant=%s", parent.TaskID, child.TaskID, id, d.AllocationRef.ObjectID, original.CommandID, permission.ObjectID)
	// 受信宿主显式取得本次父事实；随后public command仍执行完整当前门禁。
	childCtx := b.app.foreignContextFactory(ctx, runtime.Flow{Kind: "command", Scope: b.app.Scope, Auth: b.app.UserAuth})
	childCtx, err = b.app.RemoteAgent.PrepareChildContext(childCtx, child.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	allocationID := api.NewID("allocation")
	allocation := task.AllocateInput{AllocationID: allocationID, ParentTaskRef: b.app.Scope.Ref(child.TaskID, child.Revision), ReceiverID: b.app.Scope.OwnerID, Limits: []api.Amount{{Unit: "USD", Value: "1"}}, Deadline: child.Deadline}
	reserveCommand := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: b.app.Scope.OwnerID, CommandID: api.NewID("command"), Method: "budget.allocate", TargetID: allocationID, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(allocation)}
	allocated, err := b.app.Dispatcher.Command(childCtx, b.app.UserAuth, api.Raw(reserveCommand))
	if err != nil || allocated.Stage != "applied" || allocated.Error != nil {
		t.Fatalf("child original unsettled reservation: %+v %v", allocated, err)
	}
	configuredAgentControl(ctx, t, a, parent.TaskID, "cancel")
	actualAllocation, err := a.app.Task.AllocationRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, d.AllocationRef.ObjectID)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.app.RemoteAgent.CloseAllocation(ctx, a.app.Scope, actualAllocation); err != nil {
		t.Fatal(err)
	}
	if err = a.app.RemoteAgent.Control(ctx, a.app.Scope, d, "cancel"); err != nil {
		t.Fatal(err)
	}
	unknown, err := a.app.RemoteAgent.State(ctx, a.app.Scope, d)
	if err != nil || !unknown.Fact.GoalWorkClosed || !unknown.Fact.EffectsClosed || unknown.Fact.UsageFinal || unknown.AllocationClosure != nil {
		t.Fatalf("unknown fee was closed by parent terminal: %+v %v", unknown, err)
	}
	grant := configuredAgentGrant(ctx, t, a, permission)
	if !grant.OnceConsumed || !api.Equal(grant.Reserved, []api.Amount{{Unit: "USD", Value: "2"}}) {
		t.Fatalf("unknown original Grant liability lost: %+v", grant)
	}
	for revision, amount := range []string{"0.5", "0.75"} {
		// 受信输入只进入原child allocation的累计账；父只能取得签名Closure。
		proof, err := b.app.Publish(ctx, b.app.Scope, b.app.ServiceAuth, api.NewID("content"), "text/plain", []byte("explicit trusted original billing source cumulative USD "+amount), []api.ContentRef{}, []api.ContentRef{})
		if err != nil {
			t.Fatal(err)
		}
		usage := api.UsageSnapshot{SourceRef: b.app.Scope.Ref(allocationID, 1), UsageRevision: uint64(revision + 1), Cumulative: []api.Amount{{Unit: "USD", Value: amount}}, SpendingClosed: true, UsageFinal: true, ProofRefs: []api.ContentRef{proof}}
		usage.UsageDigest, err = task.UsageDigest(usage)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = b.app.Task.ReconcileUsage(ctx, b.app.Store, b.app.Scope, b.app.ServiceAuth, "budget_allocation", usage); err != nil {
			t.Fatal(err)
		}
		closed, err := a.app.RemoteAgent.State(ctx, a.app.Scope, d)
		if err != nil || closed.AllocationClosure == nil || closed.AllocationClosureRef == nil || !closed.Fact.UsageFinal {
			t.Fatalf("actual signed final child Closure: %+v %v", closed, err)
		}
		configuredAgentPublishOriginalClosureProof(ctx, t, b, closed.AllocationClosure.ProofRef)
		// 原Closure与准确证明绑定；伪字节元数据必须在任何出站前拒绝。
		wrong := *closed.AllocationClosure
		wrong.ProofRef.ByteLength++
		requests := a.requests.Load()
		if err = b.app.RemoteAgent.ReportClosure(ctx, b.app.Scope, a.app.Scope.OwnerID, *closed.AllocationClosureRef, wrong); !api.IsCode(err, "idempotency_conflict") || a.requests.Load() != requests {
			t.Fatalf("changed original Closure sent: %v", err)
		}
		for repeat := 0; repeat < 2; repeat++ {
			if err = b.app.RemoteAgent.ReportClosure(ctx, b.app.Scope, a.app.Scope.OwnerID, *closed.AllocationClosureRef, *closed.AllocationClosure); err != nil {
				t.Fatal(err)
			}
		}
		actual, err := a.app.Task.Read(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, parent.TaskID)
		if err != nil || actual.Status != "cancelled" || actual.ResultRef != nil || actual.Budget[0].Reserved != "0" || actual.Budget[0].Spent != amount {
			t.Fatalf("signed original fee reopens/doubles parent: %+v %v", actual, err)
		}
		grant = configuredAgentGrant(ctx, t, a, permission)
		if !grant.OnceConsumed || !api.Equal(grant.Reserved, []api.Amount{{Unit: "USD", Value: "0"}}) || !api.Equal(grant.Spent, []api.Amount{{Unit: "USD", Value: amount}}) {
			t.Fatalf("signed late original fee did not settle Grant once: %+v", grant)
		}
		t.Logf("verified child originalClosure=%s@%d cumulativeUSD=%s parentStatus=%s originalGrantOnce=%v reserved=%s", closed.AllocationClosureRef.ObjectID, closed.AllocationClosureRef.Revision, amount, actual.Status, grant.OnceConsumed, grant.Reserved[0].Value)
		if revision == 1 {
			a.reopenOriginal(t)
			b.reopenOriginal(t)
			if err = b.app.RemoteAgent.ReportClosure(ctx, b.app.Scope, a.app.Scope.OwnerID, *closed.AllocationClosureRef, *closed.AllocationClosure); err != nil {
				t.Fatal(err)
			}
			grant = configuredAgentGrant(ctx, t, a, permission)
			if !grant.OnceConsumed || !api.Equal(grant.Spent, []api.Amount{{Unit: "USD", Value: amount}}) {
				t.Fatalf("joined original DB/replay doubled original Grant: %+v", grant)
			}
			// 主动关闭原证明仍阻断新正文读取，不由账务角色绕过DataPolicy。
			expectedControlRevision := uint64(1)
			closeCommand := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: b.app.Scope.OwnerID, CommandID: api.NewID("command"), Method: "content.close", TargetID: closed.AllocationClosure.ProofRef.ContentID, ExpectedRevision: &expectedControlRevision, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(memory.CloseInput{ContentRef: closed.AllocationClosure.ProofRef, Reason: "original Closure proof withdrawn"})}
			closedReceipt, closeErr := b.app.Dispatcher.Command(ctx, b.app.UserAuth, api.Raw(closeCommand))
			if closeErr != nil || closedReceipt.Stage != "applied" {
				t.Fatalf("explicit original proof close: %+v %v", closedReceipt, closeErr)
			}
			requests = a.requests.Load()
			if err = b.app.RemoteAgent.ReportClosure(ctx, b.app.Scope, a.app.Scope.OwnerID, *closed.AllocationClosureRef, *closed.AllocationClosure); !api.IsCode(err, "forbidden") || a.requests.Load() != requests {
				t.Fatalf("closed proof bypassed current Memory gate: %v", err)
			}
		}
	}
}
