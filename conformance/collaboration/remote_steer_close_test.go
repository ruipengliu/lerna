package collaboration_test

import (
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/collaboration"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

func TestRemoteSteerCloseAfterActualPublicationCannotChangeOriginalGoal(t *testing.T) {
	parent, child, profile := pairedAgents(t)
	installAgentFlow(parent)
	installAgentFlow(child)
	_, d, a, actual := startRemoteChild(t, parent, child, profile)
	stepAgentJob(t, parent, task.JobDelegation)
	id := api.NewID("transfer")
	c := parent.command("collaboration.input.send", id, collaboration.RemoteInputSend{TransferID: id, DelegationID: d.DelegationID, ParentGoalRevision: d.ParentGoalRevision, ChildGoalRevision: actual.GoalRevision, Kind: "steer", ContentRef: parent.publish(t, "original late goal amendment")})
	receipt, err := parent.dispatch.Command(parent.ctx, parent.auth, api.Raw(c))
	if err != nil || receipt.Stage != "accepted" {
		t.Fatalf("original steer %+v %v", receipt, err)
	}
	stepAgentJob(t, parent, collaboration.JobRemoteInputSend)
	stepAgentJob(t, child, collaboration.JobRemoteInputReceive)
	published := make(chan struct{})
	resume := make(chan struct{})
	child.afterPublish = func(ref api.ContentRef) { close(published); <-resume }
	works, status, err := child.store.Claim(child.ctx, child.scope, api.NewID("boot"), []string{task.JobSteer}, 1, 30*time.Second)
	if err != nil || status != runtime.Committed || len(works) != 1 {
		t.Fatalf("original steer Job %v %v", status, err)
	}
	h, _ := child.registry.Job(task.JobSteer)
	done := make(chan error, 1)
	go func() { done <- h(child.ctx, child.store, child.scope, works[0]) }()
	select {
	case <-published:
	case <-time.After(15 * time.Second):
		close(resume)
		t.Fatal("actual source publication did not reach window")
	}
	closeErr := parent.remote.CloseAllocation(parent.ctx, parent.scope, a)
	close(resume)
	select {
	case err = <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("actual steer worker did not exit")
	}
	if closeErr != nil || err != nil {
		t.Fatalf("original close/job %v %v", closeErr, err)
	}
	if original := originalRemoteInputReceipt(t, parent, receipt); original.Stage != "rejected" || original.Error == nil {
		t.Fatalf("closed steer lost original rejection: %+v", original)
	}
	current, err := child.task.Read(child.ctx, child.store, child.scope, child.auth, actual.TaskID)
	if err != nil || current.GoalRevision != actual.GoalRevision || !api.Equal(current.GoalRef, actual.GoalRef) {
		t.Fatalf("late actual publication changed original goal after close: old=%d current=%d err=%v", actual.GoalRevision, current.GoalRevision, err)
	}
}
