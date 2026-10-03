package task_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

type dispatchPayload struct {
	Intent task.OperationIntent `json:"intent"`
	Window api.ControlSnapshot  `json:"window"`
}
type operationReceiver struct {
	*sessionProbe
	dispatch  *runtime.Dispatcher
	auth      runtime.Auth
	loseReply bool
}

func (p *operationReceiver) Dispatch(ctx context.Context, _ runtime.Scope, intent task.OperationIntent, window api.ControlSnapshot) error {
	c := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: p.dispatch.OwnerID, CommandID: intent.CommandID, Method: "receiver.invoke", TargetID: intent.OperationID, ExpiresAt: intent.Deadline, Payload: api.Raw(dispatchPayload{intent, window})}
	r, err := p.dispatch.Command(ctx, p.auth, api.Raw(c))
	if err != nil {
		return err
	}
	if r.Error != nil {
		return r.Error
	}
	if p.loseReply {
		p.loseReply = false
		return api.E("dependency_unavailable", "invoke_reply_lost")
	}
	return nil
}
func (*operationReceiver) Read(context.Context, runtime.Scope, api.ObjectRef) (api.Operation, error) {
	return api.Operation{}, api.E("dependency_unavailable", "no_effect_in_admission_fixture")
}
func (*operationReceiver) Usage(context.Context, runtime.Scope, api.ObjectRef) (api.UsageSnapshot, error) {
	return api.UsageSnapshot{}, api.E("dependency_unavailable", "no_usage_in_admission_fixture")
}
func (*operationReceiver) Control(context.Context, runtime.Scope, string, api.ControlSnapshot) error {
	return api.E("unsupported", "control_not_in_admission_fixture")
}

func TestOperationLostInvokeReplyRetransmitsExactOriginalControlWindow(t *testing.T) {
	proof := &localProofFixture{}
	receiver := &operationReceiver{loseReply: true}
	h := newHarness(t, task.Ports{ActionAuthorization: proof, ControlProof: proof, Execution: receiver})
	proof.install(t, h.scope)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "original-invoke.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	r := runtime.NewRegistry()
	if err = r.Register(runtime.Method{Contract: api.Contract[dispatchPayload, api.ObjectRef]("receiver.invoke", "execution", "command", false, false), Participants: []string{"receiver"}, Apply: func(ctx context.Context, tx runtime.Tx, _ runtime.Auth, c api.Command) (runtime.Outcome, error) {
		var in dispatchPayload
		if err := api.Decode(c.Payload, &in); err != nil {
			return runtime.Outcome{}, err
		}
		if err := tx.Create(ctx, "receiver.invocations", c.TargetID, "", in); err != nil {
			return runtime.Outcome{}, err
		}
		return runtime.Applied(tx.Scope().Ref(c.TargetID, 1)), nil
	}}); err != nil {
		t.Fatal(err)
	}
	receiver.dispatch = &runtime.Dispatcher{Store: store, OwnerID: h.scope.OwnerID, Registry: r}
	receiver.auth = h.trusted()
	current := h.submit(t)
	prepared := h.prepared(current, "0")
	if _, err = h.service.PrepareDecision(context.Background(), h.store, h.scope, h.trusted(), prepared); err != nil {
		t.Fatal(err)
	}
	a := preparedAction(h, "0", "fixed_original_window")
	a.SafeRequirementCheck = true
	out, err := h.service.ConsumeProposal(context.Background(), h.store, h.scope, h.trusted(), task.Proposal{DecisionID: prepared.DecisionID, Kind: "act", ReasonRef: current.GoalRef, Actions: []task.PreparedAction{a}}, nil)
	if err != nil || out.Outcome != "adopted" {
		t.Fatalf("safe admission %+v %v", out, err)
	}
	drainKind(t, h, task.JobDispatchOperation)
	remote := runtime.Scope{TenantID: h.scope.TenantID, OwnerID: h.scope.OwnerID, DatabaseID: store.ID()}
	var before dispatchPayload
	if _, err = store.Read(context.Background(), remote, "receiver.invocations", a.OperationID, 1, &before); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1050 * time.Millisecond)
	drainKind(t, h, task.JobDispatchOperation)
	var after dispatchPayload
	if _, err = store.Read(context.Background(), remote, "receiver.invocations", a.OperationID, 1, &after); err != nil || !api.Equal(before, after) {
		t.Fatalf("original invoke changed after reply loss %+v %v", after, err)
	}
	commands, err := receiver.dispatch.Lookup(context.Background(), receiver.auth, a.CommandID)
	if err != nil || commands.Stage != "applied" {
		t.Fatalf("original invoke receipt lost %+v %v", commands, err)
	}
}
