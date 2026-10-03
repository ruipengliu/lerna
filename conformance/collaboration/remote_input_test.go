package collaboration_test

import (
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/collaboration"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

func stepAgentJob(t *testing.T, f *agentFixture, kind string) {
	t.Helper()
	var works []runtime.Work
	var status runtime.CommitStatus
	var err error
	until := time.Now().Add(5 * time.Second)
	for {
		works, status, err = f.store.Claim(f.ctx, f.scope, api.NewID("boot"), []string{kind}, 1, 30*time.Second)
		if err != nil || status != runtime.Committed || len(works) != 0 || !time.Now().Before(until) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil || status != runtime.Committed || len(works) != 1 {
		t.Fatalf("actual %s responsibility %d %v %v", kind, len(works), status, err)
	}
	h, _ := f.registry.Job(kind)
	if err = h(f.ctx, f.store, f.scope, works[0]); err != nil {
		t.Fatal(err)
	}
}
func originalRemoteInputReceipt(t *testing.T, parent *agentFixture, accepted api.Receipt) api.Receipt {
	t.Helper()
	var ack collaboration.RemoteInputAck
	if err := api.Decode(accepted.Output, &ack); err != nil || ack.ReceiverCommandRef == nil {
		t.Fatalf("original remote input identity missing: %+v %v", ack, err)
	}
	receipt, err := parent.client.Receipt(parent.ctx, ack.ReceiverCommandRef.ObjectID)
	if err != nil {
		t.Fatal(err)
	}
	return receipt
}
func TestRemoteTLSOriginalAnswerAndSteerKeepSourceAndRequest(t *testing.T) {
	parent, child, profile := pairedAgents(t)
	installAgentFlow(parent)
	installAgentFlow(child)
	_, d, _, actual := startRemoteChild(t, parent, child, profile)
	stepAgentJob(t, parent, task.JobDelegation)
	var err error
	child.ctx, err = child.remote.PrepareChildContext(child.ctx, actual.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	goalRevision := actual.GoalRevision
	req := api.InputRequest{RequestID: api.NewID("request"), TargetRef: child.scope.Ref(actual.TaskID, actual.Revision), GoalRevision: &goalRevision, Purpose: "supply_context", QuestionRef: child.publish(t, "supply exact context"), AnswerSchemaRef: child.answerSchemaRef, PreviewRefs: []api.ContentRef{actual.GoalRef}, ExpiresAt: api.Time(time.Now().Add(10 * time.Minute)), State: "pending"}
	var requestRef api.ObjectRef
	_, err = child.store.Within(child.ctx, child.scope, []string{"task", "content", "memory", "collaboration", "governance"}, func(tx runtime.Tx) error {
		var err error
		requestRef, err = child.task.CreateInputTx(child.ctx, tx, child.serviceAuth, actual.TaskID, req)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	answer := parent.publish(t, "exact remote answer")
	id := api.NewID("transfer")
	in := collaboration.RemoteInputSend{TransferID: id, DelegationID: d.DelegationID, ParentGoalRevision: d.ParentGoalRevision, ChildGoalRevision: goalRevision, Kind: "answer_request", ContentRef: answer, RequestRef: &requestRef}
	c := parent.command("collaboration.input.send", id, in)
	receipt, err := parent.dispatch.Command(parent.ctx, parent.auth, api.Raw(c))
	if err != nil || receipt.Stage != "accepted" {
		t.Fatalf("original answer acceptance %+v %v", receipt, err)
	}
	stepAgentJob(t, parent, collaboration.JobRemoteInputSend)
	stepAgentJob(t, child, collaboration.JobRemoteInputReceive)
	if original := originalRemoteInputReceipt(t, parent, receipt); original.Stage != "accepted" {
		t.Fatalf("original answer preparation %+v", original)
	}
	if current, err := parent.remote.State(parent.ctx, parent.scope, d); err != nil || current.Fact.TransfersClosed {
		t.Fatalf("unconsumed original answer falsely sealed: closed=%v err=%v", current.Fact.TransfersClosed, err)
	}
	stepAgentJob(t, child, task.JobInput)
	if original := originalRemoteInputReceipt(t, parent, receipt); original.Stage != "applied" {
		t.Fatalf("original answer consumption %+v", original)
	}
	// 两方原异步责任均收束；不能依赖第一回答的 waiting 恰好尚未到期，
	// 否则慢介质/race 时下一次 Claim 会合法先领取旧回答而非新 steer。
	stepAgentJob(t, child, collaboration.JobRemoteInputReceive)
	stepAgentJob(t, parent, collaboration.JobRemoteInputSend)
	if current, err := parent.remote.State(parent.ctx, parent.scope, d); err != nil || !current.Fact.TransfersClosed {
		t.Fatalf("consumed original answer responsibility remains open: closed=%v err=%v", current.Fact.TransfersClosed, err)
	}
	view, err := child.task.InputRequestRead(child.ctx, child.store, child.scope, child.auth, requestRef.ObjectID, 0)
	if err != nil || view.Request.State != "answered" || view.Request.ConsumedBy == "" {
		t.Fatalf("original request not consumed %+v %v", view, err)
	}
	bytes, err := child.memory.ReadBytes(child.ctx, child.scope, child.auth, answer, "task.goal", "cloud")
	if err != nil || string(bytes) != "exact remote answer" {
		t.Fatalf("original answer bytes %v", err)
	}
	actual, err = child.task.Read(child.ctx, child.store, child.scope, child.auth, actual.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	steerRef := parent.publish(t, "exact additional constraint, without new tool permission")
	id = api.NewID("transfer")
	in = collaboration.RemoteInputSend{TransferID: id, DelegationID: d.DelegationID, ParentGoalRevision: d.ParentGoalRevision, ChildGoalRevision: actual.GoalRevision, Kind: "steer", ContentRef: steerRef}
	c = parent.command("collaboration.input.send", id, in)
	receipt, err = parent.dispatch.Command(parent.ctx, parent.auth, api.Raw(c))
	if err != nil || receipt.Stage != "accepted" {
		t.Fatalf("original steer acceptance %+v %v", receipt, err)
	}
	stepAgentJob(t, parent, collaboration.JobRemoteInputSend)
	stepAgentJob(t, child, collaboration.JobRemoteInputReceive)
	if original := originalRemoteInputReceipt(t, parent, receipt); original.Stage != "accepted" {
		t.Fatalf("original steer preparation %+v", original)
	}
	stepAgentJob(t, child, task.JobSteer)
	if original := originalRemoteInputReceipt(t, parent, receipt); original.Stage != "applied" {
		t.Fatalf("original steer consumption %+v", original)
	}
	changed, err := child.task.Read(child.ctx, child.store, child.scope, child.auth, actual.TaskID)
	if err != nil || changed.GoalRevision != actual.GoalRevision+1 {
		t.Fatalf("steer original goal %+v %v", changed, err)
	}
	body, err := child.memory.ReadBytes(child.ctx, child.scope, child.auth, changed.GoalRef, "task.goal", "cloud")
	if err != nil {
		t.Fatal(err)
	}
	var doc api.GoalDocument
	if err = api.Decode(body, &doc); err != nil || !api.Equal(doc.InitialGoalRef, actual.GoalRef) || len(doc.AmendmentRefs) != 2 || !api.Equal(doc.AmendmentRefs[0], answer) || !api.Equal(doc.AmendmentRefs[1], steerRef) {
		t.Fatalf("original source changed %+v %v", doc, err)
	}
	readCtx, err := child.memory.PrepareForeignContext(child.ctx, child.scope, child.auth, []api.ContentRef{changed.GoalRef}, "task.goal", "cloud")
	if err != nil {
		t.Fatal(err)
	}
	var metadata memory.ContentVersion
	_, err = child.store.Within(readCtx, child.scope, []string{"task", "content", "memory", "collaboration", "governance"}, func(tx runtime.Tx) error {
		var err error
		metadata, err = child.memory.CheckContentTx(readCtx, tx, child.auth, changed.GoalRef, "task.goal", "cloud", false)
		return err
	})
	if err != nil || len(metadata.ProcessedSources) != 3 {
		t.Fatalf("derived goal lost actual provenance %+v %v", metadata, err)
	}
}
