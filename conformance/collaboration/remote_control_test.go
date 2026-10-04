package collaboration_test

import (
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/collaboration"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

func startRemoteChild(t *testing.T, parent, child *agentFixture, profile collaboration.RemoteAgentProfile) (api.Task, task.Delegation, task.Allocation, api.Task) {
	t.Helper()
	current := parent.readyParent(t)
	id := api.NewID("delegation")
	in := task.DelegateInput{DelegationID: id, ParentTaskRef: parent.scope.Ref(current.TaskID, current.Revision), ParentGoalRevision: current.GoalRevision, GoalRef: parent.publish(t, "finite delegated artifact; parent must independently verify"), InputRefs: []api.ContentRef{}, AgentBindingRef: profile.Values.AgentBindingRef, PermissionRefs: []api.ObjectRef{}, Budget: []api.Amount{{Unit: "USD", Value: "3"}}, Deadline: api.Time(time.Now().Add(10 * time.Minute)), PolicyRef: child.taskPolicy.PolicyRef, ReceiverID: child.scope.OwnerID}
	if receipt, err := parent.dispatch.Command(parent.ctx, parent.auth, api.Raw(parent.command("collaboration.delegate", id, in))); err != nil || receipt.Stage != "applied" {
		t.Fatalf("original parent delegation command %+v %v", receipt, err)
	}
	var d task.Delegation
	var a task.Allocation
	_, err := parent.store.Within(parent.ctx, parent.scope, []string{"task", "content", "memory", "collaboration", "governance"}, func(tx runtime.Tx) error {
		var err error
		d, a, err = parent.task.CheckDelegationTx(parent.ctx, tx, parent.auth, id)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = parent.remote.Create(parent.ctx, parent.scope, d, a); err != nil {
		t.Fatal(err)
	}
	stepRemoteCreate(t, child)
	fact, err := parent.remote.Read(parent.ctx, parent.scope, d)
	if err != nil || fact.ChildTaskRef == nil {
		t.Fatalf("actual original child %+v %v", fact, err)
	}
	actual, err := child.task.Read(child.ctx, child.store, child.scope, child.auth, fact.ChildTaskRef.ObjectID)
	if err != nil {
		t.Fatal(err)
	}
	return current, d, a, actual
}
func controlAgentTask(t *testing.T, f *agentFixture, id, kind string) api.Task {
	t.Helper()
	actual, err := f.task.Read(f.ctx, f.store, f.scope, f.auth, id)
	if err != nil {
		t.Fatal(err)
	}
	c := f.command("task."+kind, id, task.ControlInput{TaskID: id, Reason: "explicit original user control"})
	c.ExpectedRevision = &actual.Revision
	receipt, err := f.dispatch.Command(f.ctx, f.auth, api.Raw(c))
	if err != nil || receipt.Stage != "applied" {
		t.Fatalf("actual %s %+v %v", kind, receipt, err)
	}
	actual, err = f.task.Read(f.ctx, f.store, f.scope, f.auth, id)
	if err != nil {
		t.Fatal(err)
	}
	return actual
}
func checkRemoteChild(t *testing.T, child *agentFixture, id string) error {
	t.Helper()
	_, err := child.store.Within(child.ctx, child.scope, []string{"task", "content", "memory", "collaboration", "governance"}, func(tx runtime.Tx) error {
		actual, err := child.task.ReadTaskTx(child.ctx, tx, child.auth, id)
		if err != nil {
			return err
		}
		return child.remote.CheckTaskCurrentTx(child.ctx, tx, actual, true)
	})
	return err
}
func TestRemoteTLSParentPausePreservesChildPauseAndCancelClosesBudget(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver+"_parent_sqlite_child", func(t *testing.T) { runRemoteParentPauseAndCancel(t, driver) })
	}
}
func runRemoteParentPauseAndCancel(t *testing.T, driver string) {
	t.Helper()
	parent, child, profile := pairedAgentsWithParentDriver(t, driver)
	root, d, a, actual := startRemoteChild(t, parent, child, profile)
	var err error
	child.ctx, err = child.remote.PrepareChildContext(child.ctx, actual.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkRemoteChild(t, child, actual.TaskID); err != nil {
		t.Fatalf("current parent falsely closed %v", err)
	}
	controlAgentTask(t, parent, root.TaskID, "pause")
	if err := parent.remote.Control(parent.ctx, parent.scope, d, "pause"); err != nil {
		t.Fatal(err)
	}
	if err := checkRemoteChild(t, child, actual.TaskID); !api.IsCode(err, "invalid_state") {
		t.Fatalf("parent pause did not gate child %v", err)
	}
	controlAgentTask(t, child, actual.TaskID, "pause")
	controlAgentTask(t, parent, root.TaskID, "resume")
	if err := parent.remote.Control(parent.ctx, parent.scope, d, "resume"); !api.IsCode(err, "dependency_unavailable") {
		t.Fatalf("resume needs original current proof %v", err)
	}
	works, status, err := child.store.Claim(child.ctx, child.scope, api.NewID("boot"), []string{collaboration.JobRemoteControl}, 1, 30*time.Second)
	if err != nil || status != runtime.Committed || len(works) != 1 {
		t.Fatalf("original control responsibility %v %v", status, err)
	}
	h, _ := child.registry.Job(collaboration.JobRemoteControl)
	if err = h(child.ctx, child.store, child.scope, works[0]); err != nil {
		t.Fatal(err)
	}
	if err = parent.remote.Control(parent.ctx, parent.scope, d, "resume"); err != nil {
		t.Fatal(err)
	}
	paused, err := child.task.Read(child.ctx, child.store, child.scope, child.auth, actual.TaskID)
	if err != nil || paused.Control != "paused" {
		t.Fatalf("parent resume undid child's own pause %+v %v", paused, err)
	}
	controlAgentTask(t, parent, root.TaskID, "cancel")
	if err = parent.remote.CloseAllocation(parent.ctx, parent.scope, a); err != nil {
		t.Fatal(err)
	}
	if err = parent.remote.Control(parent.ctx, parent.scope, d, "cancel"); err != nil {
		t.Fatal(err)
	}
	cancelled, err := child.task.Read(child.ctx, child.store, child.scope, child.auth, actual.TaskID)
	if err != nil || cancelled.Status != "cancelled" {
		t.Fatalf("original child not cancelled %+v %v", cancelled, err)
	}
	inc, err := child.task.IncomingRead(child.ctx, child.store, child.scope, parent.peer, d.AllocationRef)
	if err != nil || inc.Gate != "closed" {
		t.Fatalf("consumption gate remained open %+v %v", inc, err)
	}
	if err = checkRemoteChild(t, child, actual.TaskID); !api.IsCode(err, "invalid_state") {
		t.Fatalf("cancelled child allowed new work %v", err)
	}
}
