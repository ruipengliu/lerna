package admission_test

import (
	"testing"

	"github.com/ruipengliu/lerna/conformance/protobuf"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

func captureObject(t *testing.T, samples *[]protobuf.Sample, name string, message proto.Message, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	if message == nil || !message.ProtoReflect().IsValid() {
		t.Fatalf("%s absent", name)
	}
	*samples = append(*samples, protobuf.Sample{Name: name, Message: proto.Clone(message)})
}

func captureConfirmations(t *testing.T, samples *[]protobuf.Sample) {
	t.Helper()
	f := newFixture(t, 100, 80, true)
	p := f.propose(t, nil)
	request := &v1.RequestAdmissionConfirmationCommand{Header: header("capture-confirm"), TaskId: f.task.Name, ProposalRef: p, GrantRef: f.grant}
	receipt, err := f.h.Tasks.RequestAdmissionConfirmation(f.ctx, f.caller, request)
	accepted(t, receipt, err)
	captureObject(t, samples, "request-confirmation-command", request, nil)
	pending, err := f.h.Sessions.QueryConfirmation(f.ctx, f.caller, receipt.ResultRef)
	captureObject(t, samples, "confirmation-pending", pending, err)
	approve := &v1.RespondConfirmationCommand{Header: header("capture-approve"), ConfirmationRef: pending.Ref, BindingDigest: pending.BindingDigest, Decision: "APPROVE"}
	receipt, err = f.h.Sessions.RespondConfirmation(f.ctx, f.caller, approve)
	accepted(t, receipt, err)
	captureObject(t, samples, "approve-confirmation-command", approve, nil)
	approved, err := f.h.Sessions.QueryConfirmation(f.ctx, f.caller, receipt.ResultRef)
	captureObject(t, samples, "confirmation-approved", approved, err)
	admit := &v1.AdmitCommand{Header: header("capture-admit"), TaskId: f.task.Name, ProposalRef: p, GrantRef: f.grant, ConfirmationRef: approved.Ref}
	receipt, err = f.h.Tasks.Admit(f.ctx, f.caller, admit)
	accepted(t, receipt, err)
	captureObject(t, samples, "confirmed-admit-command", admit, nil)
	consumed, err := f.h.Sessions.QueryCurrentConfirmation(f.ctx, f.caller, pending.Ref.Name)
	captureObject(t, samples, "confirmation-consumed", consumed, err)
	if consumed.State != "CONSUMED" || consumed.GetConsumedAdmissionRef() == nil {
		t.Fatal(consumed)
	}
	if f.calls.Load() != 0 {
		t.Fatal("confirmation caused external call")
	}

	// 同一公共事项走撤回分支，撤回后的准入必须留下拒绝回执。
	f.suffix = "withdraw"
	p = f.propose(t, nil)
	request = &v1.RequestAdmissionConfirmationCommand{Header: header("capture-confirm-withdraw"), TaskId: f.task.Name, ProposalRef: p, GrantRef: f.grant}
	receipt, err = f.h.Tasks.RequestAdmissionConfirmation(f.ctx, f.caller, request)
	accepted(t, receipt, err)
	pending, err = f.h.Sessions.QueryConfirmation(f.ctx, f.caller, receipt.ResultRef)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err = f.h.Sessions.RespondConfirmation(f.ctx, f.caller, &v1.RespondConfirmationCommand{Header: header("capture-approve-withdraw"), ConfirmationRef: pending.Ref, BindingDigest: pending.BindingDigest, Decision: "APPROVE"})
	accepted(t, receipt, err)
	approvedRef := receipt.ResultRef
	withdraw := &v1.WithdrawConfirmationCommand{Header: header("capture-withdraw"), ConfirmationRef: approvedRef}
	receipt, err = f.h.Sessions.WithdrawConfirmation(f.ctx, f.caller, withdraw)
	accepted(t, receipt, err)
	captureObject(t, samples, "withdraw-confirmation-command", withdraw, nil)
	withdrawn, err := f.h.Sessions.QueryCurrentConfirmation(f.ctx, f.caller, pending.Ref.Name)
	captureObject(t, samples, "confirmation-withdrawn", withdrawn, err)
	if withdrawn.State != "WITHDRAWN" {
		t.Fatal(withdrawn)
	}
	receipt, err = f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("capture-withdrawn-admit"), TaskId: f.task.Name, ProposalRef: p, GrantRef: f.grant, ConfirmationRef: approvedRef})
	captureObject(t, samples, "withdrawn-admission-rejection", receipt, err)
	if receipt.GetError().GetCode() != "CONFIRMATION_INVALID" {
		t.Fatal(receipt)
	}
	uses, err := f.h.Grants.QueryUses(f.ctx, f.caller, f.task.Name)
	if err != nil || len(uses) != 1 || f.calls.Load() != 0 {
		t.Fatalf("uses=%v err=%v calls=%d", uses, err, f.calls.Load())
	}

	grant, err := f.h.Grants.QueryGrant(f.ctx, f.caller, f.grant)
	if err != nil {
		t.Fatal(err)
	}
	grant.Ref = nil
	grant.Issuer = nil
	grant.Status = ""
	grant.UsePoolId = ""
	grant.UseMode = "SINGLE"
	grant.MaxAdmissions = 1
	grant.Permissions[0].ParameterMode = "EXACT"
	grant.Permissions[0].ParametersRef = f.parameters
	grantRequest := &v1.RequestGrantConfirmationCommand{Header: header("capture-grant-request"), Grant: grant}
	receipt, err = f.h.Grants.RequestGrantConfirmation(f.ctx, f.caller, grantRequest)
	accepted(t, receipt, err)
	captureObject(t, samples, "grant-confirmation-command", grantRequest, nil)
	pending, err = f.h.Sessions.QueryConfirmation(f.ctx, f.caller, receipt.ResultRef)
	captureObject(t, samples, "grant-confirmation-pending", pending, err)
	receipt, err = f.h.Sessions.RespondConfirmation(f.ctx, f.caller, &v1.RespondConfirmationCommand{Header: header("capture-grant-approve"), ConfirmationRef: pending.Ref, BindingDigest: pending.BindingDigest, Decision: "APPROVE"})
	accepted(t, receipt, err)
	issue := &v1.IssueGrantCommand{Header: header("capture-grant-issue"), ConfirmationRef: receipt.ResultRef}
	receipt, err = f.h.Grants.IssueGrant(f.ctx, f.caller, issue)
	accepted(t, receipt, err)
	captureObject(t, samples, "issue-grant-command", issue, nil)
	consumed, err = f.h.Sessions.QueryCurrentConfirmation(f.ctx, f.caller, pending.Ref.Name)
	captureObject(t, samples, "grant-confirmation-consumed", consumed, err)
	if consumed.GetConsumedGrantIssuanceRef() == nil || consumed.GetConsumedAdmissionRef() != nil {
		t.Fatal(consumed)
	}
	issuance, err := f.h.Grants.QueryGrantIssuance(f.ctx, f.caller, consumed.GetConsumedGrantIssuanceRef())
	captureObject(t, samples, "grant-issuance", issuance, err)
	single, err := f.h.Grants.QueryGrant(f.ctx, f.caller, issuance.GrantRef)
	captureObject(t, samples, "single-exact-grant", single, err)
}

func captureRevocationRelease(t *testing.T, samples *[]protobuf.Sample) {
	t.Helper()
	f := newFixture(t, 100, 80, false)
	a, start := prepareStart(t, f)
	receipt, err := f.h.Tasks.StartExecution(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	accepted(t, receipt, err)
	started, err := f.h.Tasks.QueryStart(f.ctx, f.caller, receipt.ResultRef)
	captureObject(t, samples, "start-record", started, err)
	revoke := &v1.RevokeGrantCommand{Header: header("capture-revoke"), GrantId: f.grant.Name}
	receipt, err = f.h.Grants.Revoke(f.ctx, f.caller, revoke)
	accepted(t, receipt, err)
	captureObject(t, samples, "revoke-grant-command", revoke, nil)
	pending, err := f.h.Grants.QueryRevocation(f.ctx, f.caller, receipt.ResultRef)
	captureObject(t, samples, "revocation-pending", pending, err)
	if pending.Status != "PENDING" || len(pending.Closures) != 1 {
		t.Fatal(pending)
	}
	if err = f.h.Grants.ProcessRevocations(f.ctx); err != nil {
		t.Fatal(err)
	}
	complete, err := f.h.Grants.QueryCurrentRevocation(f.ctx, f.caller, pending.Ref.Name)
	captureObject(t, samples, "revocation-complete", complete, err)
	if complete.Status != "COMPLETE" {
		t.Fatal(complete)
	}
	status, err := f.h.Grants.QueryGrantStatus(f.ctx, f.caller, f.grant.Name)
	captureObject(t, samples, "revoked-grant-status", status, err)
	op, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	captureObject(t, samples, "sealed-operation", op, err)
	proof := op.ClosureEvidenceRefs[0]
	closure, err := f.h.Ledger.QueryGrantExitClosure(f.ctx, f.caller, proof)
	captureObject(t, samples, "grant-exit-closure", closure, err)
	release := &v1.ReleaseReservationCommand{Header: header("capture-release"), ReservationRef: a.BudgetBasis.ReservationRef, ClosureRef: proof}
	receipt, err = f.h.Budget.ReleaseUnused(f.ctx, f.caller, release)
	accepted(t, receipt, err)
	captureObject(t, samples, "release-reservation-command", release, nil)
	released, err := f.h.Budget.QueryReservationRelease(f.ctx, f.caller, a.BudgetBasis.ReservationRef)
	captureObject(t, samples, "reservation-release", released, err)
	budget, err := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	captureObject(t, samples, "released-budget", budget, err)
	if budget.Reserved != 0 || budget.Settled != 0 || f.calls.Load() != 0 {
		t.Fatalf("release %v calls=%d", budget, f.calls.Load())
	}
}
