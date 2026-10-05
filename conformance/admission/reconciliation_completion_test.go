package admission_test

import (
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G2、G3、G11、完成-4、完成-5、完成-6、完成-7、R7
func TestVerifyingTaskRechecksNewClosureQueryBeforeFixedResult(t *testing.T) {
	for _, mode := range []string{"applied", "strong-negative", "drop"} {
		t.Run(mode, func(t *testing.T) {
			target := simulator.New("queryable")
			target.SetBehavior("drop-after-apply")
			if mode == "strong-negative" {
				target.SetBehavior("accept-and-delay")
			}
			if mode != "applied" {
				target.SetQueryBehavior(mode)
			}
			f := newFixtureWithTarget(t, 200, 200, false, target)
			cap, grant := configureReconciliation(t, f)
			scopeRequirement(t, f)
			a, start := prepareStart(t, f)
			r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
			accepted(t, r, e)
			proposal := completeProposal(t, f, []*v1.CompletionEvidence{{ConditionId: "created", OperationId: a.OperationId}})
			r, e = f.h.Tasks.BeginCompletion(f.ctx, f.caller, &v1.BeginCompletionCommand{Header: header("begin-query-completion"), TaskId: a.TaskId, ProposalRef: proposal})
			accepted(t, r, e)
			first := r.ResultRef
			if e = f.h.Tasks.ProcessCompletions(f.ctx, f.caller); e != nil {
				t.Fatal(e)
			}
			planning, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, a.TaskId)
			if e != nil {
				t.Fatal(e)
			}
			waiting, e := f.h.Tasks.QueryVerification(f.ctx, f.caller, planning.VerificationRef)
			if e != nil || waiting.Status != "VERIFYING" || len(waiting.AdmissionRefs) != 1 || planning.VerificationFreeze == 0 {
				t.Fatalf("unknown action closed: %v %v", waiting, e)
			}
			r, e = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, reconciliationCommand(f, a, cap, grant))
			accepted(t, r, e)
			if e = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); e != nil {
				t.Fatal(e)
			}
			if e = f.h.Ledger.ProcessOperationProgress(f.ctx, f.caller); e != nil {
				t.Fatal(e)
			}
			planning, e = f.h.Tasks.QueryPlanning(f.ctx, f.caller, a.TaskId)
			if e != nil {
				t.Fatal(e)
			}
			round, e := f.h.Tasks.QueryVerification(f.ctx, f.caller, planning.VerificationRef)
			if e != nil {
				t.Fatal(e)
			}
			if len(round.AdmissionRefs) != 2 || len(round.OperationRefs) != 2 || len(round.ClosureIntentRefs) != 2 {
				t.Fatalf("new query omitted from verification: %v", round)
			}
			result, e := f.h.Tasks.QueryResult(f.ctx, f.caller, a.TaskId)
			if e != nil {
				t.Fatal(e)
			}
			switch mode {
			case "applied":
				if round.Status != "PASSED" || result == nil || result.Outcome != "SUCCEEDED" || len(result.OperationRefs) != 2 || len(result.ReservationRefs) != 2 || result.Conditions[0].Conclusion != "SATISFIED" {
					t.Fatalf("query evidence not verified: %v %v", round, result)
				}
				found := false
				for _, ref := range result.Conditions[0].EvidenceRefs {
					if ref.SchemaId == "lerna.v1.ReconciliationFinding" {
						found = true
					}
				}
				if !found {
					t.Fatalf("missing immutable query provenance: %v", result)
				}
			case "strong-negative":
				if round.Status != "REJECTED" || result != nil || round.Conditions[0].Conclusion != "UNSATISFIED" {
					t.Fatalf("negative proof not verified: %v %v", round, result)
				}
			case "drop":
				if round.Status != "VERIFYING" || result != nil || planning.VerificationFreeze == 0 {
					t.Fatalf("unknown read closed task: %v %v", round, result)
				}
			}
			old, e := f.h.Tasks.QueryVerification(f.ctx, f.caller, first)
			if e != nil || old.Status != "VERIFYING" || len(old.AdmissionRefs) != 1 {
				t.Fatalf("historical round mutated: %v %v", old, e)
			}
			notices, e := f.h.Ledger.QueryOperationProgress(f.ctx, f.caller, a.OperationId)
			if e != nil {
				t.Fatal(e)
			}
			for _, n := range notices {
				if n.RecipientReceipt == nil {
					t.Fatalf("completion wakeup stranded: %v", n)
				}
			}
			if e = f.h.Close(); e != nil {
				t.Fatal(e)
			}
			f.h, e = assembly.Open(f.path, "u", "d")
			if e != nil {
				t.Fatal(e)
			}
			after, e := f.h.Tasks.QueryResult(f.ctx, f.caller, a.TaskId)
			if e != nil || !proto.Equal(after, result) {
				t.Fatalf("result changed on recovery: %v %v", after, e)
			}
			requests, effects := target.Snapshot()
			want := 1
			if mode == "strong-negative" {
				want = 0
			}
			if len(requests) != 2 || len(effects) != want {
				t.Fatalf("verification resent target work: %v %v", requests, effects)
			}
		})
	}
}
