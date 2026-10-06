// Package executor 提供可复用的执行适配器公共契约套件。
package executor

import (
	"context"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// Fixture 的目标计数独立于核心效果解释；故障只由装配注入。
type Fixture struct {
	Harness             *assembly.Harness
	Context             context.Context
	Admission           *v1.Admission
	Start               *v1.StartExecutionCommand
	Actual              func() (calls, effects int)
	ExpectedCalls       int
	LoseDispatchReceipt func(context.Context) (context.Context, error)
}

// FixtureFactory 为每个场景创建独立服务与真实目标。
type FixtureFactory func(*testing.T) Fixture

func accepted(t *testing.T, r *v1.CommandReceipt, e error) {
	t.Helper()
	if e != nil || r.GetDecision() != v1.Decision_DECISION_ACCEPTED {
		t.Fatalf("expected accepted receipt: %v %v", r, e)
	}
}

// 规则：G1、G2、G3、G4、G5、G10、G11
func Run(t *testing.T, factory FixtureFactory) {
	t.Helper()
	for _, scenario := range []string{"one-per-permit", "descriptor-bound", "unknown-dispatch"} {
		t.Run(scenario, func(t *testing.T) {
			f := factory(t)
			if f.Harness == nil || f.Context == nil || f.Admission == nil || f.Start == nil || f.Actual == nil || f.LoseDispatchReceipt == nil {
				t.Fatal("incomplete executor conformance fixture")
			}
			// 同一入口、同一契约断言；oracle 来自独立 API 目标或实际原生文件原语完成事件。
			if calls, effects := f.Actual(); calls != 0 || effects != 0 {
				t.Fatal("prepare performed external I/O")
			}
			actor := &v1.Caller{UserId: f.Admission.TaskId.UserId, IssuerId: "egress"}
			send := proto.Clone(f.Start).(*v1.StartExecutionCommand)
			switch scenario {
			case "descriptor-bound":
				send.CallDescriptor.Target += "-outside"
				r, e := f.Harness.Egress.Invoke(f.Context, actor, send)
				if e == nil && r.GetDecision() == v1.Decision_DECISION_ACCEPTED {
					t.Fatalf("mutated fixed scope accepted: %v", r)
				}
				if calls, effects := f.Actual(); calls != 0 || effects != 0 {
					t.Fatal("denied scope performed I/O")
				}
				return
			case "unknown-dispatch":
				ctx, e := f.LoseDispatchReceipt(f.Context)
				if e != nil {
					t.Fatal(e)
				}
				r, e := f.Harness.Egress.Invoke(ctx, actor, send)
				if e == nil || r != nil {
					t.Fatalf("P5 receipt-loss did not fire: %v %v", r, e)
				}
				if calls, effects := f.Actual(); calls != 0 || effects != 0 {
					t.Fatal("unknown P5 performed I/O")
				}
			}
			r, e := f.Harness.Egress.Invoke(f.Context, actor, send)
			accepted(t, r, e)
			firstCalls, firstEffects := f.Actual()
			if scenario == "one-per-permit" && f.ExpectedCalls > 0 && firstCalls != f.ExpectedCalls {
				t.Fatalf("first invocation calls=%d want=%d", firstCalls, f.ExpectedCalls)
			}
			again, e := f.Harness.Egress.Invoke(f.Context, actor, send)
			accepted(t, again, e)
			if !proto.Equal(r, again) {
				t.Fatal("original dispatch receipt changed")
			}
			calls, effects := f.Actual()
			if calls != firstCalls || effects != firstEffects {
				t.Fatalf("permit replay changed actual target: before=%d/%d after=%d/%d", firstCalls, firstEffects, calls, effects)
			}
			op, e := f.Harness.Ledger.QueryOperation(f.Context, actor, f.Admission.OperationId)
			if e != nil {
				t.Fatal(e)
			}
			if op.Execution.Send.SendSeq != 1 || !proto.Equal(op.Execution.Attempt.Ref.Name, send.Binding.AttemptId) {
				t.Fatal("retry changed original responsibility")
			}
			if scenario == "unknown-dispatch" {
				if calls != 0 || effects != 0 || op.Effect.Outcome != "UNKNOWN" {
					t.Fatalf("unknown attempt blindly resent: %d %d %v", calls, effects, op.Effect)
				}
				return
			}
			if calls == 0 || effects != 1 || op.Effect.Outcome != "APPLIED" || op.Effect.LateEffect != "RULED_OUT" || op.Lifecycle != "SETTLED" {
				t.Fatalf("execution/actual target mismatch: %d %d %v", calls, effects, op)
			}
			raw, e := f.Harness.Ledger.QueryObservation(f.Context, actor, op.Execution.Send.ObservationRef)
			if e != nil || raw.Source != "TRUSTED_IO" || !proto.Equal(raw.SendRef.Name, op.Execution.Send.Ref.Name) || !proto.Equal(raw.OperationId, op.Ref.Name) {
				t.Fatalf("observation attribution: %v %v", raw, e)
			}
			source, e := f.Harness.Budget.QueryBillingSource(f.Context, actor, op.Execution.Send.Ref)
			if e != nil || source == nil || !proto.Equal(source.SendRef.Name, op.Execution.Send.Ref.Name) || !proto.Equal(source.OperationId, op.Ref.Name) {
				t.Fatalf("original billing responsibility: %v %v", source, e)
			}
		})
	}
}
