package admission_test

import (
	"bytes"
	"testing"

	"github.com/ruipengliu/lerna/adapters/interaction"
	"github.com/ruipengliu/lerna/infra/hosting"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// 规则：G3、G4、G8、G11、开始-3
func TestAcceptedRevocationClosesRealStartedExitBeforePhysicalSend(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	a, c := prepareStart(t, f)
	caller := &v1.Caller{UserId: "u", IssuerId: "egress"}
	r, e := f.h.Tasks.StartExecution(f.ctx, caller, c)
	accepted(t, r, e)
	r, e = f.h.Grants.Revoke(f.ctx, f.caller, &v1.RevokeGrantCommand{Header: header("revoke"), GrantId: f.grant.Name})
	accepted(t, r, e)
	pending, e := f.h.Grants.QueryRevocation(f.ctx, f.caller, r.ResultRef)
	if e != nil || pending.Status != "PENDING" || len(pending.Closures) != 1 {
		t.Fatalf("missing responsibility: %v %v", pending, e)
	}
	cli := interaction.CLI{Grants: f.h.Grants, Sessions: f.h.Sessions, Ledger: f.h.Ledger, Caller: f.caller, Domain: "d"}
	cli.Progress = newManualProgress(t, hosting.ManualDependencies{Sessions: f.h.Sessions, Observations: f.h.Content, Reports: f.h.Ledger, Revocations: f.h.Grants, Reconciliations: f.h.Ledger, ExecutionFollowups: f.h.Ledger})
	if e = cli.Run(f.ctx, []string{"recover"}, new(bytes.Buffer)); e != nil {
		t.Fatal(e)
	}
	rev, e := f.h.Grants.QueryCurrentRevocation(f.ctx, f.caller, pending.Ref.Name)
	if e != nil || rev.Status != "COMPLETE" || rev.Closures[0].RecipientReceipt == nil {
		t.Fatalf("closure incomplete: %v %v", rev, e)
	}
	proof, e := f.h.Ledger.QueryGrantExitClosure(f.ctx, f.caller, rev.Closures[0].RecipientReceipt.ResultRef)
	if e != nil || proof.PhysicalSendWasPossible || !proto.Equal(proof.CredentialRef, c.CredentialRef) {
		t.Fatalf("closure proof: %v %v", proof, e)
	}
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || op.Dispatch != "SEALED" || op.Lifecycle != "SETTLED" || op.Effect.Outcome != "NOT_APPLIED" || op.Effect.LateEffect != "RULED_OUT" {
		t.Fatalf("unclosed operation: %v %v", op, e)
	}
	old, e := f.h.Grants.QueryRevocation(f.ctx, f.caller, pending.Ref)
	if e != nil || !proto.Equal(old, pending) {
		t.Fatalf("rewritten history: %v %v", old, e)
	}
	current, e := f.h.Grants.QueryCurrentGrant(f.ctx, f.caller, f.grant.Name)
	if e != nil || current.RevocationCompletion != "COMPLETE" {
		t.Fatalf("grant incomplete: %v %v", current, e)
	}
	r, e = f.h.Egress.Invoke(f.ctx, caller, c)
	if e == nil && r.GetDecision() == v1.Decision_DECISION_ACCEPTED {
		t.Fatalf("closed exit advanced: %v", r)
	}
	reservations, e := f.h.Budget.QueryReservations(f.ctx, f.caller, a.TaskId)
	if e != nil || len(reservations) != 1 || reservations[0].ConsumedSends != 1 {
		t.Fatalf("lost budget responsibility: %v %v", reservations, e)
	}
	budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil || budget.Reserved != 30 {
		t.Fatalf("invented fee settlement: %v %v", budget, e)
	}
	uses, e := f.h.Grants.QueryUses(f.ctx, f.caller, a.TaskId)
	if e != nil || len(uses) != 1 {
		t.Fatalf("refunded authority: %v %v", uses, e)
	}
	if f.calls.Load() != 0 {
		t.Fatal("closed exit sent")
	}
}

// 规则：G3、G4、G8、开始-3
func TestAcceptedRevocationBeforeStartRejectsOriginalCredential(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	a, c := prepareStart(t, f)
	r, e := f.h.Grants.Revoke(f.ctx, f.caller, &v1.RevokeGrantCommand{Header: header("revoke"), GrantId: f.grant.Name})
	accepted(t, r, e)
	rev, e := f.h.Grants.QueryRevocation(f.ctx, f.caller, r.ResultRef)
	if e != nil || rev.Status != "COMPLETE" || len(rev.Closures) != 0 {
		t.Fatalf("unstarted closure: %v %v", rev, e)
	}
	r, e = f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
	if e != nil || r.GetError().GetCode() != "GRANT_INVALID" {
		t.Fatalf("revoked start: %v %v", r, e)
	}
	assertUnstarted(t, f, a, c)
}

// 规则：G3、G4、G5、G8、G11
func TestAcceptedRevocationAfterDispatchPreservesUncertaintyAndNeverResends(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	a, c := prepareStart(t, f)
	caller := &v1.Caller{UserId: "u", IssuerId: "egress"}
	sent, e := f.h.Egress.Invoke(f.ctx, caller, c)
	accepted(t, sent, e)
	before, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || before.Effect.Outcome != "UNKNOWN" || before.Effect.LateEffect != "MAY_OCCUR" || f.calls.Load() != 1 {
		t.Fatalf("original effect: %v %v", before, e)
	}
	r, e := f.h.Grants.Revoke(f.ctx, f.caller, &v1.RevokeGrantCommand{Header: header("revoke"), GrantId: f.grant.Name})
	accepted(t, r, e)
	ref := r.ResultRef
	if e = f.h.Grants.ProcessRevocations(f.ctx); e != nil {
		t.Fatal(e)
	}
	rev, e := f.h.Grants.QueryCurrentRevocation(f.ctx, f.caller, ref.Name)
	if e != nil || rev.Status != "COMPLETE" {
		t.Fatalf("revocation: %v %v", rev, e)
	}
	proof, e := f.h.Ledger.QueryGrantExitClosure(f.ctx, f.caller, rev.Closures[0].RecipientReceipt.ResultRef)
	if e != nil || !proof.PhysicalSendWasPossible {
		t.Fatalf("lost possible send: %v %v", proof, e)
	}
	after, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || after.Dispatch != "SEALED" || !proto.Equal(after.Effect, before.Effect) || after.Lifecycle == "SETTLED" {
		t.Fatalf("invented certainty: %v %v", after, e)
	}
	replay, e := f.h.Egress.Invoke(f.ctx, caller, c)
	if e != nil || !proto.Equal(replay, sent) || f.calls.Load() != 1 {
		t.Fatalf("resend: %v %v calls %d", replay, e, f.calls.Load())
	}
}
