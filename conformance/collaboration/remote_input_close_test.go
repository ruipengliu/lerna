package collaboration_test

import (
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/collaboration"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

func TestRemoteInputCloseAfterActualReadCannotConsumeOriginalRequest(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver+"_parent_sqlite_child", func(t *testing.T) { runRemoteInputCloseAfterActualRead(t, driver) })
	}
}
func runRemoteInputCloseAfterActualRead(t *testing.T, driver string) {
	t.Helper()
	parent, child, profile := pairedAgentsWithParentDriver(t, driver)
	installAgentFlow(parent)
	installAgentFlow(child)
	_, d, a, actual := startRemoteChild(t, parent, child, profile)
	stepAgentJob(t, parent, task.JobDelegation)
	var err error
	child.ctx, err = child.remote.PrepareChildContext(child.ctx, actual.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	goal := actual.GoalRevision
	req := api.InputRequest{RequestID: api.NewID("request"), TargetRef: child.scope.Ref(actual.TaskID, actual.Revision), GoalRevision: &goal, Purpose: "supply_context", QuestionRef: child.publish(t, "original exact remote request"), AnswerSchemaRef: child.answerSchemaRef, PreviewRefs: []api.ContentRef{}, ExpiresAt: api.Time(time.Now().Add(10 * time.Minute)), State: "pending"}
	var request api.ObjectRef
	_, err = child.store.Within(child.ctx, child.scope, []string{"task", "content", "memory", "collaboration", "governance"}, func(tx runtime.Tx) error {
		var err error
		request, err = child.task.CreateInputTx(child.ctx, tx, child.serviceAuth, actual.TaskID, req)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	answer := parent.publish(t, "answer bytes were actually read before close")
	id := api.NewID("transfer")
	c := parent.command("collaboration.input.send", id, collaboration.RemoteInputSend{TransferID: id, DelegationID: d.DelegationID, ParentGoalRevision: d.ParentGoalRevision, ChildGoalRevision: goal, Kind: "answer_request", ContentRef: answer, RequestRef: &request})
	receipt, err := parent.dispatch.Command(parent.ctx, parent.auth, api.Raw(c))
	if err != nil || receipt.Stage != "accepted" {
		t.Fatalf("original input %+v %v", receipt, err)
	}
	stepAgentJob(t, parent, collaboration.JobRemoteInputSend)
	stepAgentJob(t, child, collaboration.JobRemoteInputReceive)
	read := make(chan struct{})
	resume := make(chan struct{})
	child.afterRead = func(ref api.ContentRef) {
		if api.Equal(ref, answer) {
			close(read)
			<-resume
		}
	}
	works, status, err := child.store.Claim(child.ctx, child.scope, api.NewID("boot"), []string{task.JobInput}, 1, 30*time.Second)
	if err != nil || status != runtime.Committed || len(works) != 1 {
		t.Fatalf("original input Job %v %v", status, err)
	}
	h, _ := child.registry.Job(task.JobInput)
	done := make(chan error, 1)
	go func() { done <- h(child.ctx, child.store, child.scope, works[0]) }()
	select {
	case <-read:
	case <-time.After(15 * time.Second):
		close(resume)
		t.Fatal("actual source read did not reach window")
	}
	closeErr := parent.remote.CloseAllocation(parent.ctx, parent.scope, a)
	close(resume)
	select {
	case err = <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("actual input worker did not exit")
	}
	if closeErr != nil || err != nil {
		t.Fatalf("original close/job %v %v", closeErr, err)
	}
	if original := originalRemoteInputReceipt(t, parent, receipt); original.Stage != "rejected" || original.Error == nil {
		t.Fatalf("closed input lost original rejection: %+v", original)
	}
	view, err := child.task.InputRequestRead(child.ctx, child.store, child.scope, child.auth, request.ObjectID, 0)
	if err != nil || view.Request.State != "pending" || view.Request.ConsumedBy != "" {
		t.Fatalf("late actual answer consumed after original allocation close: state=%s consumed=%s err=%v", view.Request.State, view.Request.ConsumedBy, err)
	}
}
