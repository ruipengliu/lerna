package task_test

import (
	"context"
	"testing"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

func TestCheckDelegationTxUsesOriginalParentControlAndAllocation(t *testing.T) {
	rule := fixtureRule()
	gate := &evidenceBridge{rules: map[string]api.RuleDefinition{rule.RuleRef.ComponentID: rule}}
	h := newHarness(t, task.Ports{Gate: gate}, rule)
	parent := readyTask(t, h, rule)
	id := api.NewID("delegation")
	in := task.DelegateInput{DelegationID: id, ParentTaskRef: h.scope.Ref(parent.TaskID, parent.Revision), ParentGoalRevision: parent.GoalRevision, GoalRef: h.content("original bounded child"), InputRefs: []api.ContentRef{}, AgentBindingRef: h.scope.Ref(api.NewID("binding"), 1), PermissionRefs: []api.ObjectRef{}, Budget: []api.Amount{{Unit: "USD", Value: "3"}}, Deadline: parent.Deadline, PolicyRef: h.policy.PolicyRef, ReceiverID: h.scope.OwnerID, Internal: true}
	out, err := h.service.Delegate(context.Background(), h.store, h.scope, h.auth, h.command("task.delegate", id, nil, in), in)
	if err != nil {
		t.Fatal(err)
	}
	check := func() (task.Delegation, task.Allocation, error) {
		var d task.Delegation
		var a task.Allocation
		_, err := h.store.Within(context.Background(), h.scope, []string{"task"}, func(tx runtime.Tx) error {
			var err error
			d, a, err = h.service.CheckDelegationTx(context.Background(), tx, h.auth, id)
			return err
		})
		return d, a, err
	}
	d, a, err := check()
	if err != nil || d.CreationKey != id || a.AllocationID != out.AllocationRef.ObjectID || a.ParentTaskRef.ObjectID != parent.TaskID || !api.Equal(a.Limits, in.Budget) {
		t.Fatalf("original current scope %+v %+v %v", d, a, err)
	}
	parent, err = h.service.Read(context.Background(), h.store, h.scope, h.auth, parent.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	r, err := h.dispatch.Command(context.Background(), h.auth, api.Raw(h.command("task.pause", parent.TaskID, &parent.Revision, task.ControlInput{TaskID: parent.TaskID, Reason: "freeze original scope"})))
	if err != nil || r.Stage != "applied" {
		t.Fatalf("pause %+v %v", r, err)
	}
	if _, _, err = check(); !api.IsCode(err, "invalid_state") {
		t.Fatalf("paused parent supplied start proof: %v", err)
	}
}
