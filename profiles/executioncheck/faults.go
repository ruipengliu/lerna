package executioncheck

import (
	"context"
	"fmt"
	"google.golang.org/protobuf/proto"
	"lerna/adapters/executionlocal"
	"lerna/authorization"
	"lerna/execution"
	wire "lerna/gen/harness/v1"
	"lerna/internal/executionwire"
	"lerna/sdk"
	"lerna/tasks"
	"sync/atomic"
	"time"
)

type faultStore struct {
	authorization.Store
	fail, lose atomic.Bool
}

func (s *faultStore) Commit(ctx context.Context, version uint64, state authorization.State) error {
	if s.fail.Swap(false) {
		return &authorization.Error{Code: authorization.Unavailable}
	}
	e := s.Store.Commit(ctx, version, state)
	if e == nil && s.lose.Swap(false) {
		return &authorization.Error{Code: authorization.OutcomeUnknown}
	}
	return e
}

type guardHook struct {
	execution.Core
	hook func()
}

func (g guardHook) GuardExecution(tx authorization.RuntimeTransaction, q tasks.Qualification, op string, consume bool) error {
	if e := g.Core.GuardExecution(tx, q, op, consume); e != nil {
		return e
	}
	if consume {
		g.hook()
	}
	return nil
}

type transportFunc func(context.Context, []byte) ([]byte, error)

func (f transportFunc) Exchange(ctx context.Context, b []byte) ([]byte, error) { return f(ctx, b) }

var faultNames = []string{"start-commit-failed", "start-commit-unknown", "sdk-lost-admission", "wire-unknown-field", "sdk-wrong-receipt", "bounded-uncooperative-driver", "unknown-report-then-update", "inspection-budget"}

func faultCheck(ctx context.Context, name string) error {
	h, e := fresh(ctx)
	if e != nil {
		return e
	}
	defer h.destroy()
	r, m, e := h.request(ctx)
	if e != nil {
		return e
	}
	if name == "wire-unknown-field" {
		in := &wire.CapabilityRequest{MessageId: "test", Namespace: "local", Body: &wire.CapabilityRequest_Invoke{Invoke: &wire.InvokeCapability{Invocation: executionwire.Encode(r), GrantMaterial: m}}}
		in.ProtoReflect().SetUnknown([]byte{0x98, 0x06, 0x01})
		data, _ := proto.Marshal(in)
		response, e := executionlocal.Bind(h.exec, "local").Exchange(ctx, data)
		if e != nil {
			return e
		}
		out := new(wire.CapabilityResponse)
		if e = proto.Unmarshal(response, out); e != nil {
			return e
		}
		target, e := h.target.Snapshot(ctx)
		return require(e == nil && out.GetFailure().GetCode() == "UNSUPPORTED" && target.Changes == 0, "unknown field executed")
	}
	if name == "sdk-lost-admission" || name == "sdk-wrong-receipt" {
		binding := executionlocal.Bind(h.exec, "local")
		client := sdk.NewCapabilityClient(transportFunc(func(c context.Context, data []byte) ([]byte, error) {
			b, e := binding.Exchange(c, data)
			if e != nil {
				return nil, e
			}
			if name == "sdk-lost-admission" {
				return nil, &authorization.Error{Code: authorization.OutcomeUnknown}
			}
			out := new(wire.CapabilityResponse)
			if e = proto.Unmarshal(b, out); e != nil {
				return nil, e
			}
			out.GetReceipt().OperationId = "another-operation"
			return proto.Marshal(out)
		}), "local")
		_, e = client.Invoke(ctx, r, m)
		if e == nil {
			return fmt.Errorf("bad response accepted")
		}
		original, e := h.client.GetInvocation(ctx, r.OperationID)
		if e != nil {
			return e
		}
		receipt, e := h.client.Invoke(ctx, r, "")
		return require(e == nil && receipt.Revision == 1 && !original.Started, "receipt lost operation")
	}
	if name == "start-commit-failed" || name == "start-commit-unknown" {
		if e = h.replace(h.target, guardHook{h.work, func() {
			if name == "start-commit-failed" {
				h.store.fail.Store(true)
			} else {
				h.store.lose.Store(true)
			}
		}}); e != nil {
			return e
		}
	}
	if name == "inspection-budget" || name == "unknown-report-then-update" {
		if e = h.replace(wrappedDriver{Driver: h.target, inspect: func(context.Context, execution.Call) (execution.Observation, error) {
			return execution.Observation{Phase: "UNKNOWN", Result: "UNKNOWN", Effect: "UNKNOWN"}, nil
		}}, h.work); e != nil {
			return e
		}
	}
	var release chan struct{}
	if name == "bounded-uncooperative-driver" {
		release = make(chan struct{})
		if e = h.replace(wrappedDriver{Driver: h.target, start: func(_ context.Context, call execution.Call) error {
			<-release
			return h.target.Start(context.Background(), call)
		}}, h.work); e != nil {
			return e
		}
	}
	if _, e = h.client.Invoke(ctx, r, m); e != nil {
		return e
	}
	out, e := h.exec.Run(ctx, r.OperationID)
	if name == "start-commit-failed" || name == "start-commit-unknown" {
		if e == nil {
			return fmt.Errorf("commit fault ignored")
		}
		target, err := h.target.Snapshot(ctx)
		if err != nil {
			return err
		}
		record, err := h.client.GetInvocation(ctx, r.OperationID)
		if err != nil {
			return err
		}
		current, err := h.core.Load(ctx, r.Qualification.Ref)
		if err != nil {
			return err
		}
		if name == "start-commit-failed" {
			return require(target.Changes == 0 && !record.Started && current.Work[0].ExecutionOperation == "", "partial startup commit")
		}
		return require(target.Changes == 0 && record.Started && current.Work[0].ExecutionOperation == r.OperationID, "unknown startup was resent")
	}
	if e != nil {
		return e
	}
	switch name {
	case "bounded-uncooperative-driver":
		if out.Effect != "UNKNOWN" {
			close(release)
			return fmt.Errorf("invented stop")
		}
		next, err := h.exec.Run(ctx, r.OperationID)
		if err != nil {
			close(release)
			return err
		}
		if next.Effect != "UNKNOWN" {
			close(release)
			return fmt.Errorf("overlapping work")
		}
		close(release)
		deadline := time.Now().Add(3 * time.Second)
		for {
			out, e = h.client.GetInvocation(ctx, r.OperationID)
			if e != nil {
				return e
			}
			if out.Effect == "CONFIRMED" {
				break
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("late effect lost")
			}
			time.Sleep(10 * time.Millisecond)
		}
		if e = h.exec.Drain(ctx, 16); e != nil {
			return e
		}
		target, e := h.target.Snapshot(ctx)
		return require(e == nil && target.Changes == 1, "duplicate late start")
	case "inspection-budget":
		for range config().MaxChecks {
			if _, e = h.client.Reconcile(ctx, r.OperationID); e != nil {
				return e
			}
		}
		_, e = h.client.Reconcile(ctx, r.OperationID)
		record, err := h.client.GetInvocation(ctx, r.OperationID)
		return require(authorization.Is(e, authorization.Unavailable) && err == nil && record.Checks == uint32(config().MaxChecks), "unbounded inspections")
	case "unknown-report-then-update":
		if e = h.exec.Drain(ctx, 16); e != nil {
			return e
		}
		if e = h.update(ctx, r); e != nil {
			return e
		}
		if e = h.replace(h.target, h.work); e != nil {
			return e
		}
		if _, e = h.client.Reconcile(ctx, r.OperationID); e != nil {
			return e
		}
		if e = h.exec.Drain(ctx, 16); e != nil {
			return e
		}
		current, e := h.core.Get(ctx, h.token, r.Qualification.Ref)
		return require(e == nil && current.State != "COMPLETED", "old report authorized updated goal")
	}
	return fmt.Errorf("unknown fault check")
}
