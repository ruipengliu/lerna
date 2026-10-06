package admission_test

import (
	"context"
	"testing"

	"github.com/ruipengliu/lerna/conformance/simulator"
	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// unavailableTrace 是公开 TraceReceiver 端口的持续依赖故障；不替换业务所有者。
type unavailableTrace struct{ queries int }

func (s *unavailableTrace) QueryReceipt(context.Context, *v1.Caller, *v1.CommandIdentity) (*v1.ReceiptQuery, error) {
	s.queries++
	return nil, command.Fail("DEPENDENCY_UNAVAILABLE")
}
func (*unavailableTrace) Accept(context.Context, *v1.Caller, *v1.AcceptTraceCommand) (*v1.CommandReceipt, error) {
	return nil, command.Fail("DEPENDENCY_UNAVAILABLE")
}

// 规则：G1、G2、G3、G10、G11、R3、完成-4、完成-5、完成-7
func TestCompletionUsesOriginalEvidenceWhileTraceRemainsUnavailable(t *testing.T) {
	for _, unknown := range []bool{true, false} {
		name := "succeeded"
		if unknown {
			name = "unknown"
		}
		t.Run(name, func(t *testing.T) {
			target := simulator.NewBillingTarget(25)
			target.DropReceipt(unknown)
			f := newFixtureWithTarget(t, 100, 80, false, target)
			sink := new(unavailableTrace)
			f.h.Ledger.WithReports(f.h.Budget, f.h.Durable, sink)
			assertAbsent := func() {
				t.Helper()
				m, e := f.h.Trace.QueryMetrics(f.ctx, f.caller)
				if e != nil || m.Produced == 0 || m.Accepted != 0 || m.Indexed != 0 || m.PendingAcknowledgements != m.Produced || m.ReceiverBacklog != m.Produced || m.CoverageComplete || m.Diagnostics.Source.Availability != "DISABLED" {
					t.Fatalf("trace absence: %v %v", m, e)
				}
				view, e := f.h.Trace.Query(f.ctx, f.caller, &v1.TraceQuery{TaskId: f.task.Name})
				if e != nil || len(view.Events) != 0 || view.Complete || view.Backlog == 0 {
					t.Fatalf("uncollected view: %v %v", view, e)
				}
				sources, e := f.h.Trace.QuerySources(f.ctx, f.caller)
				if e != nil || len(sources) == 0 {
					t.Fatalf("mandatory sources: %v %v", sources, e)
				}
				for _, s := range sources {
					if s.Receipt != nil {
						t.Fatal("unexpected collector ACK")
					}
				}
			}
			scopeRequirement(t, f)
			a, start := prepareStart(t, f)
			assertAbsent()
			r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
			if e == nil || r.GetDecision() != v1.Decision_DECISION_ACCEPTED {
				t.Fatalf("persistent trace outage: %v %v", r, e)
			}
			dispatch := proto.Clone(r).(*v1.CommandReceipt)
			if e = f.h.Ledger.ProcessInterpretations(f.ctx, f.caller); e != nil {
				t.Fatal(e)
			}
			original, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
			if e != nil {
				t.Fatal(e)
			}
			originalSend, e := f.h.Ledger.QuerySend(f.ctx, f.caller, original.Send.Ref)
			if e != nil {
				t.Fatal(e)
			}
			budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
			if e != nil {
				t.Fatal(e)
			}
			if unknown && (budget.Reserved != 30 || budget.Settled != 0) || !unknown && (budget.Reserved != 0 || budget.Settled != 25) {
				t.Fatalf("original fee: %v", budget)
			}
			assertAbsent()
			p := completeProposal(t, f, []*v1.CompletionEvidence{{ConditionId: "created", OperationId: a.OperationId}})
			begin := &v1.BeginCompletionCommand{Header: header("trace-outage-completion"), TaskId: f.task.Name, ProposalRef: p}
			receipt, e := f.h.Tasks.BeginCompletion(f.ctx, f.caller, begin)
			accepted(t, receipt, e)
			if e = f.h.Tasks.ProcessCompletions(f.ctx, f.caller); e != nil {
				t.Fatal(e)
			}
			result, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
			if e != nil || unknown && result != nil || !unknown && (result == nil || result.Outcome != "SUCCEEDED") {
				t.Fatalf("completion evidence: %v %v", result, e)
			}
			op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if e != nil || unknown && op.Effect.Outcome != "UNKNOWN" || !unknown && op.Effect.Outcome != "APPLIED" {
				t.Fatalf("original effect: %v %v", op, e)
			}
			if !proto.Equal(original.Attempt, op.Execution.Attempt) || !proto.Equal(original.CallDescriptor, op.Execution.CallDescriptor) || !proto.Equal(original.Send.Ref.Name, op.Execution.Send.Ref.Name) {
				t.Fatal("completion replaced original execution")
			}
			sealed := proto.Clone(op.Execution).(*v1.Execution)
			for range 2 {
				if e = f.h.Ledger.ProcessReports(f.ctx, f.caller); e == nil {
					t.Fatal("trace sink recovered unexpectedly")
				}
				r, e = f.h.Tasks.BeginCompletion(f.ctx, f.caller, begin)
				accepted(t, r, e)
				if !proto.Equal(r, receipt) {
					t.Fatal("completion receipt changed")
				}
				if e = f.h.Tasks.ProcessCompletions(f.ctx, f.caller); e != nil {
					t.Fatal(e)
				}
				again, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
				if e != nil || !proto.Equal(result, again) {
					t.Fatalf("result changed: %v %v", again, e)
				}
				q, e := f.h.Egress.QueryReceipt(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, dispatch.Identity)
				if e != nil || !proto.Equal(dispatch, q.Receipt) {
					t.Fatalf("dispatch receipt changed: %v %v", q, e)
				}
				send, e := f.h.Ledger.QuerySend(f.ctx, f.caller, original.Send.Ref)
				if e != nil || !proto.Equal(send, originalSend) {
					t.Fatalf("historical send changed: %v %v", send, e)
				}
				x, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
				if e != nil || !proto.Equal(sealed, x) {
					t.Fatalf("execution changed: %v %v", x, e)
				}
				b, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
				if e != nil || !proto.Equal(budget, b) {
					t.Fatalf("fee changed: %v %v", b, e)
				}
				assertAbsent()
			}
			calls, effects := target.Target.Snapshot()
			if len(calls) != 1 || len(effects) != 1 || len(target.Bills()) != 1 || sink.queries < 3 {
				t.Fatalf("actual target or outage: calls=%d effects=%d bills=%d failures=%d", len(calls), len(effects), len(target.Bills()), sink.queries)
			}
		})
	}
}
