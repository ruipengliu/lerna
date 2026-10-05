package admission_test

import (
	"testing"
	"time"

	"github.com/ruipengliu/lerna/cmd/assembly"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// 规则：G3、G4、G8、准入-7、开始-3
func TestSingleGrantConsumesOnlyAtAdmissionAndCannotBeReused(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	g, e := f.h.Grants.QueryGrant(f.ctx, f.caller, f.grant)
	if e != nil {
		t.Fatal(e)
	}
	g = proto.Clone(g).(*v1.Grant)
	g.Ref = nil
	g.Issuer = nil
	g.Status = ""
	g.UseMode = "SINGLE"
	g.MaxAdmissions = 1
	g.Permissions[0].ParameterMode = "EXACT"
	g.Permissions[0].ParametersRef = f.parameters
	r, e := f.h.Grants.Configure(f.ctx, f.caller, &v1.ConfigureGrantCommand{Header: header("single-grant"), Grant: g})
	accepted(t, r, e)
	f.grant = r.ResultRef
	r, e = f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("admit"), TaskId: f.task.Name, ProposalRef: f.propose(t, nil), GrantRef: f.grant})
	accepted(t, r, e)
	a, e := f.h.Tasks.QueryAdmission(f.ctx, f.caller, r.ResultRef)
	if e != nil {
		t.Fatal(e)
	}
	b := &v1.ExitCredentialBinding{UserId: "u", TaskId: a.TaskId, OperationId: a.OperationId, AttemptId: &v1.GlobalName{UserId: "u", AuthorityDomainId: a.LedgerDomainId, ObjectKind: "attempt", LocalId: "single-attempt"}, SendSeq: 1, AdmissionRef: a.Ref, GrantUseRef: a.GrantUseRef, SubjectId: a.TaskId, CallerIssuerId: "egress", Audience: "egress", ExecutorEndpointId: a.ExecutorEndpointId, ExecutorInstance: "instance", DescriptorDigest: "single-descriptor", UseRight: "INVOKE", ProcessingPurpose: "CURRENT_TASK", RequirementsVersion: a.RequirementsVersion, InputVersion: a.InputVersion, ControlGeneration: a.ControlGeneration, BudgetReservationRef: a.BudgetBasis.ReservationRef}
	r, e = f.h.Grants.IssueCredential(f.ctx, f.caller, &v1.IssueExitCredentialCommand{Header: header("credential"), AdmissionRef: a.Ref, Binding: b, ExpiresAtUnixMs: time.Now().Add(time.Minute).UnixMilli()})
	accepted(t, r, e)
	f.suffix = "-second"
	r, e = f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("admit-second"), TaskId: f.task.Name, ProposalRef: f.propose(t, nil), GrantRef: f.grant})
	if e != nil || r.GetError().GetCode() != "GRANT_EXHAUSTED" {
		t.Fatalf("single used twice: %v %v", r, e)
	}
	uses, e := f.h.Grants.QueryUses(f.ctx, f.caller, f.task.Name)
	if e != nil || len(uses) != 1 {
		t.Fatalf("uses: %v %v", uses, e)
	}
	budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil || budget.Reserved != 30 {
		t.Fatalf("reservation leak: %v %v", budget, e)
	}
	r, e = f.h.Grants.Revoke(f.ctx, f.caller, &v1.RevokeGrantCommand{Header: header("revoke-exhausted"), GrantId: f.grant.Name})
	accepted(t, r, e)
	current, e := f.h.Grants.QueryCurrentGrant(f.ctx, f.caller, f.grant.Name)
	if e != nil || current.Status != "REVOKED" || current.RevocationCompletion != "COMPLETE" {
		t.Fatalf("exhausted grant not revoked: %v %v", current, e)
	}
	uses, e = f.h.Grants.QueryUses(f.ctx, f.caller, f.task.Name)
	if e != nil || len(uses) != 1 {
		t.Fatalf("revocation refunded use: %v %v", uses, e)
	}
	if f.calls.Load() != 0 {
		t.Fatal("authority changes called target")
	}
}

// 规则：G4、G7、G12
func TestGrantScopeRejectsForeignParameterReference(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	g, e := f.h.Grants.QueryGrant(f.ctx, f.caller, f.grant)
	if e != nil {
		t.Fatal(e)
	}
	g.Ref = nil
	g.Issuer = nil
	g.Status = ""
	g.UseMode = "SINGLE"
	g.MaxAdmissions = 1
	p := proto.Clone(f.parameters).(*v1.Ref)
	p.Name.UserId = "another-user"
	g.Permissions[0].ParameterMode = "EXACT"
	g.Permissions[0].ParametersRef = p
	r, e := f.h.Grants.Configure(f.ctx, f.caller, &v1.ConfigureGrantCommand{Header: header("foreign-parameters"), Grant: g})
	if e != nil || r.GetDecision() != v1.Decision_DECISION_REJECTED {
		t.Fatalf("foreign scope accepted: %v %v", r, e)
	}
	q, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("foreign-parameters").Identity)
	if e != nil || !proto.Equal(q.Receipt, r) || r.ResultRef != nil {
		t.Fatalf("invalid scope record: %v %v", q, e)
	}
	if f.calls.Load() != 0 {
		t.Fatal("grant command called target")
	}
}

// 规则：G3、G4、G8、开始-3
func TestRevocationStopsNewUseAndPreservesHistoricalGrant(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	original, e := f.h.Grants.QueryGrant(f.ctx, f.caller, f.grant)
	if e != nil {
		t.Fatal(e)
	}
	r, e := f.h.Grants.Revoke(f.ctx, f.caller, &v1.RevokeGrantCommand{Header: header("revoke"), GrantId: f.grant.Name})
	accepted(t, r, e)
	current, e := f.h.Grants.QueryCurrentGrant(f.ctx, f.caller, f.grant.Name)
	if e != nil || current.Status != "REVOKED" || current.RevocationCompletion != "COMPLETE" || current.RevocationEpoch != 1 || current.Ref.Revision <= original.Ref.Revision {
		t.Fatalf("current revoke: %v %v", current, e)
	}
	historical, e := f.h.Grants.QueryGrant(f.ctx, f.caller, f.grant)
	if e != nil || !proto.Equal(historical, original) {
		t.Fatalf("grant history changed: %v %v", historical, e)
	}
	progress, e := f.h.Grants.QueryRevocation(f.ctx, f.caller, r.ResultRef)
	if e != nil || progress.Status != "COMPLETE" || len(progress.Closures) != 0 {
		t.Fatalf("closure progress: %v %v", progress, e)
	}
	r, e = f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("admit-revoked"), TaskId: f.task.Name, ProposalRef: f.propose(t, nil), GrantRef: f.grant})
	if e != nil || r.Decision != v1.Decision_DECISION_REJECTED {
		t.Fatalf("revoked admission: %v %v", r, e)
	}
	uses, e := f.h.Grants.QueryUses(f.ctx, f.caller, f.task.Name)
	if e != nil || len(uses) != 0 {
		t.Fatalf("revoked use leaked: %v %v", uses, e)
	}
	budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil || budget.Reserved != 0 {
		t.Fatalf("revoked reserve leaked: %v %v", budget, e)
	}
	if f.calls.Load() != 0 {
		t.Fatal("revoked authority called target")
	}
}

// 规则：G7、G8
func TestDelegationAndMultipleSourcesAreExplicitlyUnsupported(t *testing.T) {
	for _, kind := range []string{"parent", "sources", "delegation"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, 100, 80, false)
			g, e := f.h.Grants.QueryGrant(f.ctx, f.caller, f.grant)
			if e != nil {
				t.Fatal(e)
			}
			g.Ref = nil
			g.Issuer = nil
			g.Status = ""
			switch kind {
			case "parent":
				g.ParentGrantRef = f.grant
			case "sources":
				g.SourceGrantRefs = []*v1.Ref{f.grant}
			case "delegation":
				g.DelegationMode = "ALLOW"
			}
			r, e := f.h.Grants.Configure(f.ctx, f.caller, &v1.ConfigureGrantCommand{Header: header("unsupported"), Grant: g})
			if e != nil || r.GetError().GetCode() != "UNSUPPORTED_FEATURE" || r.ResultRef != nil {
				t.Fatalf("expanded grant: %v %v", r, e)
			}
			if f.calls.Load() != 0 {
				t.Fatal("unsupported authority called target")
			}
		})
	}
}

// 规则：G3、G4、G8、G11、开始-3
func TestRevocationRetainsConsumedExitResponsibilityAcrossRestart(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	_, c := prepareStart(t, f)
	r, e := f.h.Tasks.StartExecution(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
	accepted(t, r, e)
	r, e = f.h.Grants.Revoke(f.ctx, f.caller, &v1.RevokeGrantCommand{Header: header("revoke"), GrantId: f.grant.Name})
	accepted(t, r, e)
	pending, e := f.h.Grants.QueryRevocation(f.ctx, f.caller, r.ResultRef)
	if e != nil || pending.Status != "PENDING" || len(pending.Closures) != 1 {
		t.Fatalf("missing responsibility: %v %v", pending, e)
	}
	closureCaller := &v1.Caller{UserId: "u", IssuerId: "grants-revocation"}
	if e = f.h.Grants.ValidateGrantClosure(f.ctx, closureCaller, pending.Closures[0].Command); e != nil {
		t.Fatal(e)
	}
	tampered := proto.Clone(pending.Closures[0].Command).(*v1.CloseGrantExitCommand)
	tampered.Binding.ExecutorInstance = "other"
	if e = f.h.Grants.ValidateGrantClosure(f.ctx, closureCaller, tampered); e == nil {
		t.Fatal("accepted substituted exit closure")
	}
	f.h.Grants.WithRevocationExits(nil)
	if e = f.h.Grants.ProcessRevocations(f.ctx); e == nil {
		t.Fatal("unavailable exit reported closure")
	}
	f.h.Close()
	f.h, e = assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	current, e := f.h.Grants.QueryCurrentRevocation(f.ctx, f.caller, pending.Ref.Name)
	if e != nil || current.Status != "COMPLETE" || len(current.Closures) != 1 || !proto.Equal(current.Closures[0].Command, pending.Closures[0].Command) || current.Closures[0].RecipientReceipt == nil {
		t.Fatalf("original closure not recovered: %v %v", current, e)
	}
	historical, e := f.h.Grants.QueryRevocation(f.ctx, f.caller, pending.Ref)
	if e != nil || !proto.Equal(historical, pending) {
		t.Fatalf("rewritten pending history: %v %v", historical, e)
	}
	grant, e := f.h.Grants.QueryCurrentGrant(f.ctx, f.caller, f.grant.Name)
	if e != nil || grant.RevocationCompletion != "COMPLETE" {
		t.Fatalf("grant pending: %v %v", grant, e)
	}
	use, e := f.h.Grants.QueryCredentialUse(f.ctx, f.caller, c.CredentialRef)
	if e != nil || use == nil {
		t.Fatalf("lost consumption: %v %v", use, e)
	}
	if f.calls.Load() != 0 {
		t.Fatal("revocation recovery sent")
	}
}

// 规则：G4、G7、G8
func TestExpiredGrantCanStillBeRevoked(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	g, e := f.h.Grants.QueryGrant(f.ctx, f.caller, f.grant)
	if e != nil {
		t.Fatal(e)
	}
	g.Ref = nil
	g.Issuer = nil
	g.Status = ""
	g.ValidFromUnixMs = time.Now().Add(-2 * time.Minute).UnixMilli()
	g.ValidUntilUnixMs = time.Now().Add(-time.Minute).UnixMilli()
	r, e := f.h.Grants.Configure(f.ctx, f.caller, &v1.ConfigureGrantCommand{Header: header("expired-grant"), Grant: g})
	accepted(t, r, e)
	ref := r.ResultRef
	r, e = f.h.Grants.Revoke(f.ctx, f.caller, &v1.RevokeGrantCommand{Header: header("expired-revoke"), GrantId: ref.Name})
	accepted(t, r, e)
	current, e := f.h.Grants.QueryCurrentGrant(f.ctx, f.caller, ref.Name)
	if e != nil || current.Status != "REVOKED" || current.RevocationCompletion != "COMPLETE" {
		t.Fatalf("expired grant not revoked: %v %v", current, e)
	}
	if f.calls.Load() != 0 {
		t.Fatal("expired revocation called target")
	}
}

// 规则：G4、G7、G8
func TestGrantStatusComputesRemainingUsesWithoutRewritingGrant(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	g, e := f.h.Grants.QueryGrant(f.ctx, f.caller, f.grant)
	if e != nil {
		t.Fatal(e)
	}
	g.Ref = nil
	g.Issuer = nil
	g.Status = ""
	g.MaxAdmissions = 1
	r, e := f.h.Grants.Configure(f.ctx, f.caller, &v1.ConfigureGrantCommand{Header: header("limited-status"), Grant: g})
	accepted(t, r, e)
	f.grant = r.ResultRef
	before, e := f.h.Grants.QueryGrantStatus(f.ctx, f.caller, f.grant.Name)
	if e != nil || before.RemainingAdmissions == nil || *before.RemainingAdmissions != 1 || before.DisplayStatus != "ACTIVE" {
		t.Fatalf("available status: %v %v", before, e)
	}
	r, e = f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("admit"), TaskId: f.task.Name, ProposalRef: f.propose(t, nil), GrantRef: f.grant})
	accepted(t, r, e)
	after, e := f.h.Grants.QueryGrantStatus(f.ctx, f.caller, f.grant.Name)
	if e != nil || after.RemainingAdmissions == nil || *after.RemainingAdmissions != 0 || after.OccupiedAdmissions != 1 || after.DisplayStatus != "EXHAUSTED" || !proto.Equal(before.Grant, after.Grant) {
		t.Fatalf("exhausted status: %v %v", after, e)
	}
	if f.calls.Load() != 0 {
		t.Fatal("status query called target")
	}
}
