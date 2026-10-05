package admission_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/grants"
)

type queryCredentialGate struct {
	*grants.Service
	after      func()
	credential *v1.Ref
}

func (g *queryCredentialGate) IssueCredential(ctx context.Context, c *v1.Caller, cmd *v1.IssueExitCredentialCommand) (*v1.CommandReceipt, error) {
	r, e := g.Service.IssueCredential(ctx, c, cmd)
	if e == nil && r.GetDecision() == v1.Decision_DECISION_ACCEPTED {
		g.credential = r.ResultRef
		g.after()
	}
	return r, e
}

// 规则：G3、G4、G8、G11、开始-2、开始-3
func TestClosureFirstStartStillChecksCurrentControlAndGrant(t *testing.T) {
	for _, mode := range []string{"control", "revocation"} {
		t.Run(mode, func(t *testing.T) {
			target := simulator.New("queryable")
			target.SetBehavior("drop-after-apply")
			f := newFixtureWithTarget(t, 200, 200, false, target)
			cap, grant := configureReconciliation(t, f)
			a, start := prepareStart(t, f)
			r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
			accepted(t, r, e)
			gate := &queryCredentialGate{Service: f.h.Grants, after: func() {
				if mode == "control" {
					task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, a.TaskId)
					if e != nil {
						t.Fatal(e)
					}
					goal, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("goal").Identity)
					if e != nil {
						t.Fatal(e)
					}
					r, e := f.h.Sessions.SubmitInput(f.ctx, f.caller, &v1.SubmitInputCommand{Header: header("late-pause"), SessionId: goal.Receipt.SessionRef.Name, TaskId: a.TaskId, InputKind: "CONTROL", Control: "PAUSE", ExpectedControlGeneration: task.ControlGeneration})
					accepted(t, r, e)
				} else {
					r, e := f.h.Grants.Revoke(f.ctx, f.caller, &v1.RevokeGrantCommand{Header: header("late-revoke"), GrantId: grant.Name})
					accepted(t, r, e)
				}
			}}
			f.h.Ledger.WithReconciliation(f.h.Tasks, gate, f.h.Egress)
			r, e = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, reconciliationCommand(f, a, cap, grant))
			accepted(t, r, e)
			if e = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); e != nil {
				t.Fatal(e)
			}
			reason := "STALE_GENERATION"
			if mode == "revocation" {
				reason = "GRANT_INVALID"
			}
			p, e := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
			if e != nil || p.State != "PAUSED" || p.PauseReason != reason {
				t.Fatalf("gate was not durable: %v %v", p, e)
			}
			q, e := f.h.Ledger.QueryReconciliationQuery(f.ctx, f.caller, p.QueryRefs[0])
			if e != nil {
				t.Fatal(e)
			}
			op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, q.QueryOperationRef.Name)
			if e != nil || op.Execution.Send.Phase != "REGISTERED" || op.Execution.Attempt.FirstPossibleSendAtUnixMs != 0 {
				t.Fatalf("query passed P5: %v %v", op, e)
			}
			use, e := f.h.Grants.QueryCredentialUse(f.ctx, f.caller, gate.credential)
			if e != nil || use != nil {
				t.Fatalf("failed P4 consumed credential: %v %v", use, e)
			}
			qa, e := f.h.Tasks.QueryAdmission(f.ctx, f.caller, q.AdmissionReceipt.ResultRef)
			if e != nil {
				t.Fatal(e)
			}
			reservation, e := f.h.Budget.QueryReservation(f.ctx, f.caller, qa.BudgetBasis.ReservationRef)
			if e != nil || reservation.ConsumedSends != 0 {
				t.Fatalf("failed P4 consumed send: %v %v", reservation, e)
			}
			if e = f.h.Close(); e != nil {
				t.Fatal(e)
			}
			f.h, e = assembly.Open(f.path, "u", "d")
			if e != nil {
				t.Fatal(e)
			}
			requests, effects := target.Snapshot()
			if len(requests) != 1 || len(effects) != 1 {
				t.Fatalf("failed P4 IO: %v %v", requests, effects)
			}
		})
	}
}

type sealQueryBeforeWorkerExit struct {
	f     *fixture
	t     *testing.T
	grant *v1.Ref
}

func (g *sealQueryBeforeWorkerExit) Invoke(ctx context.Context, c *v1.Caller, cmd *v1.StartExecutionCommand) (*v1.CommandReceipt, error) {
	r, e := g.f.h.Tasks.StartExecution(ctx, c, cmd)
	accepted(g.t, r, e)
	r, e = g.f.h.Grants.Revoke(ctx, g.f.caller, &v1.RevokeGrantCommand{Header: header("seal-query-grant"), GrantId: g.grant.Name})
	accepted(g.t, r, e)
	if e = g.f.h.Grants.ProcessRevocations(ctx); e != nil {
		g.t.Fatal(e)
	}
	return nil, context.Canceled
}

// 规则：G1、G3、G5、G10、G11、R7
func TestSealedQueryRecoveryPausesThenUsesNewAuthorizedQuery(t *testing.T) {
	target := simulator.New("queryable")
	target.SetBehavior("drop-after-apply")
	f := newFixtureWithTarget(t, 200, 200, false, target)
	cap, grant := configureReconciliation(t, f)
	original, start := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	accepted(t, r, e)
	f.h.Ledger.WithWork(&boundedQueryLeases{Service: f.h.LedgerWork})
	f.h.Ledger.WithReconciliation(f.h.Tasks, f.h.Grants, &sealQueryBeforeWorkerExit{f: f, t: t, grant: grant})
	r, e = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, reconciliationCommand(f, original, cap, grant))
	accepted(t, r, e)
	if e = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); !errors.Is(e, context.Canceled) {
		t.Fatalf("worker boundary: %v", e)
	}
	time.Sleep(3200 * time.Millisecond)
	f.h.Ledger.WithReconciliation(f.h.Tasks, f.h.Grants, f.h.Egress)
	if e = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	plan, e := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, original.OperationId)
	if e != nil || plan.State != "PAUSED" || plan.PauseReason != "DISPATCH_SEALED" {
		t.Fatalf("sealed query stranded: %v %v", plan, e)
	}
	old, e := f.h.Ledger.QueryReconciliationQuery(f.ctx, f.caller, plan.QueryRefs[0])
	if e != nil {
		t.Fatal(e)
	}
	sealed, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, old.QueryOperationRef.Name)
	if e != nil || sealed.Execution.Send.Phase != "CLOSED" || sealed.Effect.Outcome != "NOT_APPLIED" || sealed.Effect.LateEffect != "RULED_OUT" {
		t.Fatalf("query closure: %v %v", sealed, e)
	}
	write, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, original.OperationId)
	if e != nil || write.Effect.Outcome != "UNKNOWN" || write.Effect.LateEffect != "MAY_OCCUR" {
		t.Fatalf("query closure changed original effect: %v %v", write, e)
	}
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	f.h, e = assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	plan, e = f.h.Ledger.QueryReconciliation(f.ctx, f.caller, original.OperationId)
	if e != nil || plan.State != "PAUSED" || plan.PauseReason != "DISPATCH_SEALED" {
		t.Fatalf("pause lost on restart: %v %v", plan, e)
	}
	replacement, e := f.h.Grants.QueryGrant(f.ctx, f.caller, grant)
	if e != nil {
		t.Fatal(e)
	}
	replacement.Ref = nil
	replacement.Issuer = nil
	replacement.Status = ""
	replacement.SemanticVersion = 0
	replacement.UsePoolId = "replacement-query"
	r, e = f.h.Grants.Configure(f.ctx, f.caller, &v1.ConfigureGrantCommand{Header: header("replacement-query-grant"), Grant: replacement})
	accepted(t, r, e)
	r, e = f.h.Ledger.ControlReconciliation(f.ctx, f.caller, &v1.ControlReconciliationCommand{Header: ledgerHeader("resume-sealed-query"), OperationId: original.OperationId, ExpectedRevision: plan.Ref.Revision, Action: "RESUME", GrantRef: r.ResultRef})
	accepted(t, r, e)
	if e = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	plan, e = f.h.Ledger.QueryReconciliation(f.ctx, f.caller, original.OperationId)
	if e != nil || plan.State != "COMPLETED" || len(plan.QueryRefs) != 2 {
		t.Fatalf("replacement query: %v %v", plan, e)
	}
	fresh, e := f.h.Ledger.QueryReconciliationQuery(f.ctx, f.caller, plan.QueryRefs[1])
	if e != nil {
		t.Fatal(e)
	}
	next, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, fresh.QueryOperationRef.Name)
	if e != nil || proto.Equal(fresh.Work.AdmissionIdentity, old.Work.AdmissionIdentity) || proto.Equal(next.Execution.Send.Ref.Name, sealed.Execution.Send.Ref.Name) || proto.Equal(fresh.QueryOperationRef.Name, old.QueryOperationRef.Name) {
		t.Fatalf("sealed identity reused: %v %v", fresh, e)
	}
	requests, effects := target.Snapshot()
	if len(requests) != 2 || requests[0].Method != "POST" || requests[1].Method != "GET" || len(effects) != 1 {
		t.Fatalf("unsafe send: %v %v", requests, effects)
	}
}
