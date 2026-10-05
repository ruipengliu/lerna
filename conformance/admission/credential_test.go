package admission_test

import (
	"context"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/contracts/command"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// 规则：G3、G4、G8、开始-1、开始-3
func TestOpaqueCredentialBindsOriginalAdmissionAndReplays(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	r, e := f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("admit"), TaskId: f.task.Name, ProposalRef: f.propose(t, nil), GrantRef: f.grant})
	accepted(t, r, e)
	a, e := f.h.Tasks.QueryAdmission(f.ctx, f.caller, r.ResultRef)
	if e != nil {
		t.Fatal(e)
	}
	b := &v1.ExitCredentialBinding{UserId: "u", TaskId: a.TaskId, OperationId: a.OperationId, AttemptId: &v1.GlobalName{UserId: "u", AuthorityDomainId: "d/ledger", ObjectKind: "attempt", LocalId: "attempt"}, SendSeq: 1, AdmissionRef: a.Ref, GrantUseRef: a.GrantUseRef, SubjectId: a.TaskId, CallerIssuerId: "egress", Audience: "egress", ExecutorEndpointId: a.ExecutorEndpointId, ExecutorInstance: "process-one", DescriptorDigest: "fixed-descriptor-digest", UseRight: "INVOKE", ProcessingPurpose: "CURRENT_TASK", RequirementsVersion: a.RequirementsVersion, InputVersion: a.InputVersion, ControlGeneration: a.ControlGeneration, BudgetReservationRef: a.BudgetBasis.ReservationRef}
	c := &v1.IssueExitCredentialCommand{Header: header("credential"), AdmissionRef: a.Ref, Binding: b, ExpiresAtUnixMs: time.Now().Add(time.Minute).UnixMilli()}
	r, e = f.h.Grants.IssueCredential(f.ctx, f.caller, c)
	accepted(t, r, e)
	cred, e := f.h.Grants.QueryCredential(f.ctx, f.caller, r.ResultRef)
	if e != nil || cred == nil || !proto.Equal(cred.Binding, b) || cred.State != "ISSUED" {
		t.Fatalf("credential: %v %v", cred, e)
	}
	replay, e := f.h.Grants.IssueCredential(f.ctx, f.caller, c)
	if e != nil || !proto.Equal(r, replay) {
		t.Fatalf("replay: %v %v", replay, e)
	}
	startHeader := header("start")
	startHeader.Identity.IssuerId = "egress"
	exitCaller := &v1.Caller{UserId: "u", IssuerId: "egress"}
	start := func() (*v1.CommandReceipt, error) {
		return f.h.Durable.Execute(f.ctx, exitCaller, startHeader, command.SemanticFingerprint("start", r.ResultRef, b), "grants.credential", func(tx context.Context) (*v1.Ref, error) {
			return r.ResultRef, f.h.Grants.ConsumeCredentialInTransaction(tx, exitCaller, r.ResultRef, b, a)
		})
	}
	started, e := start()
	accepted(t, started, e)
	historical, e := f.h.Grants.QueryCredential(f.ctx, f.caller, r.ResultRef)
	if e != nil || !proto.Equal(historical, cred) {
		t.Fatalf("credential history changed: %v %v", historical, e)
	}
	use, e := f.h.Grants.QueryCredentialUse(f.ctx, f.caller, r.ResultRef)
	if e != nil || use == nil || !proto.Equal(use.CredentialRef, r.ResultRef) || use.ConsumedSendIdentity != "attempt/1" {
		t.Fatalf("missing consumption: %v %v", use, e)
	}
	again, e := start()
	if e != nil || !proto.Equal(started, again) {
		t.Fatalf("start replay changed: %v %v", again, e)
	}

	if f.calls.Load() != 0 {
		t.Fatal("issuance made physical call")
	}
}
