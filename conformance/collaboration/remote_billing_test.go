package collaboration_test

import (
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
)

// 金额来自原受信源的公开累计账入口，绝非模型成功或第二项父支出。
func TestRemoteTLSOriginalLateUsageSurvivesParentTerminal(t *testing.T) {
	parent, child, profile := pairedAgents(t)
	root, d, original, actual := startRemoteChild(t, parent, child, profile)
	var err error
	child.ctx, err = child.remote.PrepareChildContext(child.ctx, actual.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	actual, err = child.task.Read(child.ctx, child.store, child.scope, child.auth, actual.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	allocationID := api.NewID("allocation")
	in := task.AllocateInput{AllocationID: allocationID, ParentTaskRef: child.scope.Ref(actual.TaskID, actual.Revision), ReceiverID: child.scope.OwnerID, Limits: []api.Amount{{Unit: "USD", Value: "1"}}, Deadline: api.Time(time.Now().Add(5 * time.Minute))}
	receipt, err := child.dispatch.Command(child.ctx, child.auth, api.Raw(child.command("budget.allocate", allocationID, in)))
	if err != nil || receipt.Stage != "applied" {
		t.Fatalf("actual original reservation %+v %v", receipt, err)
	}
	controlAgentTask(t, parent, root.TaskID, "cancel")
	if err = parent.remote.CloseAllocation(parent.ctx, parent.scope, original); err != nil {
		t.Fatal(err)
	}
	if err = parent.remote.Control(parent.ctx, parent.scope, d, "cancel"); err != nil {
		t.Fatal(err)
	}
	open, err := parent.remote.State(parent.ctx, parent.scope, d)
	if err != nil || !open.Fact.GoalWorkClosed || !open.Fact.EffectsClosed || open.Fact.UsageFinal || open.AllocationClosure != nil {
		t.Fatalf("fee unknown was closed %+v %v", open, err)
	}
	parentFact, err := parent.task.Read(parent.ctx, parent.store, parent.scope, parent.auth, root.TaskID)
	if err != nil || parentFact.Status != "cancelled" || parentFact.Budget[0].Reserved != "3" {
		t.Fatalf("unknown reserve released %+v %v", parentFact, err)
	}
	proof := child.publish(t, "original trusted source cumulative USD 0.5; spending closed")
	u := api.UsageSnapshot{SourceRef: child.scope.Ref(allocationID, 1), UsageRevision: 1, Cumulative: []api.Amount{{Unit: "USD", Value: "0.5"}}, SpendingClosed: true, UsageFinal: true, ProofRefs: []api.ContentRef{proof}}
	u.UsageDigest, err = task.UsageDigest(u)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = child.task.ReconcileUsage(child.ctx, child.store, child.scope, child.serviceAuth, "budget_allocation", u); err != nil {
		t.Fatal(err)
	}
	closed, err := parent.remote.State(parent.ctx, parent.scope, d)
	if err != nil || closed.AllocationClosure == nil || closed.AllocationClosureRef == nil || !closed.Fact.UsageFinal {
		t.Fatalf("actual original closure %+v %v", closed, err)
	}
	if err = child.remote.ReportClosure(child.ctx, child.scope, parent.scope.OwnerID, *closed.AllocationClosureRef, *closed.AllocationClosure); err != nil {
		t.Fatal(err)
	}
	parentFact, err = parent.task.Read(parent.ctx, parent.store, parent.scope, parent.auth, root.TaskID)
	if err != nil || parentFact.Status != "cancelled" || parentFact.ResultRef != nil || parentFact.Budget[0].Reserved != "0" || parentFact.Budget[0].Spent != "0.5" {
		t.Fatalf("late original bill missing %+v %v", parentFact, err)
	}
	if err = child.remote.ReportClosure(child.ctx, child.scope, parent.scope.OwnerID, *closed.AllocationClosureRef, *closed.AllocationClosure); err != nil {
		t.Fatal(err)
	}
	u.UsageRevision = 2
	u.Cumulative[0].Value = "0.75"
	u.ProofRefs = []api.ContentRef{child.publish(t, "original trusted source correction cumulative USD 0.75")}
	u.UsageDigest, err = task.UsageDigest(u)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = child.task.ReconcileUsage(child.ctx, child.store, child.scope, child.serviceAuth, "budget_allocation", u); err != nil {
		t.Fatal(err)
	}
	corrected, err := parent.remote.State(parent.ctx, parent.scope, d)
	if err != nil || corrected.AllocationClosure == nil || corrected.AllocationClosureRef == nil {
		t.Fatalf("original correction %+v %v", corrected, err)
	}
	if err = child.remote.ReportClosure(child.ctx, child.scope, parent.scope.OwnerID, *corrected.AllocationClosureRef, *corrected.AllocationClosure); err != nil {
		t.Fatal(err)
	}
	parentFact, err = parent.task.Read(parent.ctx, parent.store, parent.scope, parent.auth, root.TaskID)
	if err != nil || parentFact.Status != "cancelled" || parentFact.Budget[0].Reserved != "0" || parentFact.Budget[0].Spent != "0.75" {
		t.Fatalf("correction reopens or double bills %+v %v", parentFact, err)
	}
}
