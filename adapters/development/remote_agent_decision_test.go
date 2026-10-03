package development

import (
	"context"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/collaboration"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

func TestConfiguredRemoteAgentDispatchesOriginalChildDecision(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	a, b, profile := configuredAgentPair(t)
	goal, parent := configuredAgentOriginalParent(ctx, t, a)
	id := api.NewID("delegation")
	input := task.DelegateInput{DelegationID: id, ParentTaskRef: a.app.Scope.Ref(parent.TaskID, parent.Revision), ParentGoalRevision: parent.GoalRevision, GoalRef: goal, InputRefs: []api.ContentRef{}, AgentBindingRef: profile.Values.AgentBindingRef, PermissionRefs: []api.ObjectRef{}, Budget: []api.Amount{{Unit: "USD", Value: "2"}}, Deadline: api.Time(time.Now().Add(3 * time.Minute)), PolicyRef: b.app.TaskPolicy.PolicyRef, ReceiverID: b.app.Scope.OwnerID}
	command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.app.Scope.OwnerID, CommandID: api.NewID("command"), Method: "collaboration.delegate", TargetID: id, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(input)}
	receipt, err := a.app.Dispatcher.Command(ctx, a.app.UserAuth, api.Raw(command))
	if err != nil || receipt.Stage != "applied" {
		t.Fatalf("public original delegation: %+v %v", receipt, err)
	}
	if !configuredAgentStep(ctx, t, a, task.JobDelegation) || !configuredAgentStep(ctx, t, b, collaboration.JobRemoteCreate) {
		t.Fatal("original creation work missing")
	}
	if !configuredAgentStep(ctx, t, b, task.JobAdvance) {
		t.Fatal("original child advancement missing")
	}
	d, err := a.app.Task.DelegationRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, id)
	if err != nil {
		t.Fatal(err)
	}
	state, err := a.app.RemoteAgent.State(ctx, a.app.Scope, d)
	if err != nil || state.Task == nil {
		t.Fatalf("original child current state: %+v %v", state, err)
	}
	var original task.TaskSnapshotView
	status, err := b.app.Store.Within(ctx, b.app.Scope, []string{"task"}, func(tx runtime.Tx) error {
		view, found, err := b.app.Task.LatestTaskSnapshotTx(ctx, tx, b.app.ServiceAuth, state.Task.TaskID)
		if err != nil {
			return err
		}
		if !found {
			return api.E("not_found", "actual_child_snapshot_required")
		}
		original = view
		return nil
	})
	if status != runtime.Committed || err != nil {
		t.Fatalf("original child Decision identity: %s %v", status, err)
	}
	if !configuredAgentStep(ctx, t, b, task.JobDispatchDecision) {
		t.Fatal("original child decision dispatch missing")
	}
	accepted, err := b.app.Brain.Get(ctx, b.app.Store, b.app.Scope, b.app.ServiceAuth, original.Intent.DecisionID)
	if err != nil || accepted.Decision.DecisionID != original.Intent.DecisionID || accepted.TaskRef.ObjectID != state.Task.TaskID {
		t.Fatalf("original child dispatch did not actually admit Brain Decision: %s %+v %v", original.Intent.DecisionID, accepted, err)
	}
}
