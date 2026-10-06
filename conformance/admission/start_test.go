package admission_test

import (
	"testing"
	"time"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// 规则：G3、G4、G5、开始-1、开始-3、开始-5
func TestStartAtomicallyConsumesCredentialAndOneSend(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	a, c := prepareStart(t, f)
	caller := &v1.Caller{UserId: "u", IssuerId: "egress"}
	r, e := f.h.Tasks.StartExecution(f.ctx, caller, c)
	accepted(t, r, e)
	start, e := f.h.Tasks.QueryStart(f.ctx, f.caller, r.ResultRef)
	if e != nil || start == nil || !proto.Equal(start.Binding, c.Binding) {
		t.Fatalf("start %v %v", start, e)
	}
	use, e := f.h.Grants.QueryCredentialUse(f.ctx, f.caller, c.CredentialRef)
	if e != nil || use == nil {
		t.Fatalf("credential use %v %v", use, e)
	}
	reservations, e := f.h.Budget.QueryReservations(f.ctx, f.caller, a.TaskId)
	if e != nil || len(reservations) != 1 || reservations[0].ConsumedSends != 1 {
		t.Fatalf("send allowance %v %v", reservations, e)
	}
	original, e := f.h.Budget.QueryReservation(f.ctx, f.caller, a.BudgetBasis.ReservationRef)
	if e != nil || original.ConsumedSends != 0 {
		t.Fatalf("rewritten admission reservation %v %v", original, e)
	}
	again, e := f.h.Tasks.StartExecution(f.ctx, caller, c)
	if e != nil || !proto.Equal(again, r) {
		t.Fatalf("replay %v %v", again, e)
	}
	reservations, e = f.h.Budget.QueryReservations(f.ctx, f.caller, a.TaskId)
	if e != nil || reservations[0].ConsumedSends != 1 || f.calls.Load() != 0 {
		t.Fatalf("duplicate consumption or IO %v %v calls=%d", reservations, e, f.calls.Load())
	}
}
func prepareStart(t *testing.T, f *fixture) (*v1.Admission, *v1.StartExecutionCommand) {
	return prepareStartWithLease(t, f, 30000)
}
func prepareStartWithLease(t *testing.T, f *fixture, lease int64) (*v1.Admission, *v1.StartExecutionCommand) {
	t.Helper()
	p := f.propose(t, nil)
	r, e := f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("admit" + f.suffix), TaskId: f.task.Name, ProposalRef: p, GrantRef: f.grant})
	accepted(t, r, e)
	a, e := f.h.Tasks.QueryAdmission(f.ctx, f.caller, r.ResultRef)
	if e != nil {
		t.Fatal(e)
	}
	return a, prepareExistingAdmissionStart(t, f, a, lease)
}
func prepareExistingAdmissionStart(t *testing.T, f *fixture, a *v1.Admission, lease int64) *v1.StartExecutionCommand {
	t.Helper()
	if e := f.h.Tasks.ProcessHandoffs(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	claim, e := f.h.LedgerWork.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: ledgerHeader("claim" + f.suffix).Identity, ContractVersion: 1, Action: "CLAIM", AllowedTypes: []string{"EXECUTE_OPERATION"}, Limit: 100, LeaseMs: lease, ProcessInstance: "worker"})
	accepted(t, claim, e)
	var executionClaim *v1.Job
	for _, job := range claim.Jobs {
		if proto.Equal(job.SpecificationRef.Name, a.OperationId) {
			executionClaim = job
			break
		}
	}
	if executionClaim == nil {
		t.Fatal("operation claim missing")
	}
	r, e := f.h.Ledger.Prepare(f.ctx, f.caller, &v1.PrepareExecutionCommand{Header: ledgerHeader("prepare" + f.suffix), OperationId: a.OperationId, ProcessInstance: "worker", Claim: executionClaim})
	accepted(t, r, e)
	x, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	b := &v1.ExitCredentialBinding{UserId: "u", TaskId: a.TaskId, SubjectId: a.TaskId, OperationId: a.OperationId, AttemptId: x.Attempt.Ref.Name, SendSeq: x.Send.SendSeq, AdmissionRef: a.Ref, GrantUseRef: a.GrantUseRef, CallerIssuerId: "egress", Audience: "egress", ExecutorEndpointId: a.ExecutorEndpointId, ExecutorInstance: x.Send.ProcessInstance, DescriptorDigest: x.CallDescriptor.Digest, UseRight: a.CapabilitySnapshot.UseRight, ProcessingPurpose: a.CapabilitySnapshot.ProcessingPurpose, RequirementsVersion: a.RequirementsVersion, InputVersion: a.InputVersion, ControlGeneration: a.ControlGeneration, BudgetReservationRef: a.BudgetBasis.ReservationRef}
	grant, e := f.h.Grants.QueryGrant(f.ctx, f.caller, f.grant)
	if e != nil {
		t.Fatal(e)
	}
	r, e = f.h.Grants.IssueCredential(f.ctx, f.caller, &v1.IssueExitCredentialCommand{Header: header("credential" + f.suffix), AdmissionRef: a.Ref, Binding: b, ExpiresAtUnixMs: min(time.Now().Add(time.Minute).UnixMilli(), grant.ValidUntilUnixMs)})
	accepted(t, r, e)
	h := header("start:" + x.Send.Ref.Name.LocalId)
	h.Identity.IssuerId = "egress"
	return &v1.StartExecutionCommand{Header: h, AdmissionRef: a.Ref, CredentialRef: r.ResultRef, Binding: b, Claim: executionClaim, CallDescriptor: x.CallDescriptor}
}

// 规则：G4、开始-2
func TestChangedRequirementsPreventOriginalStart(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	a, c := prepareStart(t, f)
	task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, a.TaskId)
	if e != nil {
		t.Fatal(e)
	}
	r, e := f.h.Tasks.AcceptRequirements(f.ctx, f.caller, &v1.AcceptRequirementsCommand{Header: header("requirements-new"), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, InputVersion: task.InputVersion, Conditions: []*v1.Requirement{{ConditionId: "changed", DescriptionRef: f.parameters, Necessary: true, VerificationRule: "TARGET_RECORD", RuleVersion: 1}}, Source: "TRUSTED_TEMPLATE"})
	accepted(t, r, e)
	r, e = f.h.Tasks.StartExecution(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
	if e != nil || r.GetError().GetCode() != "STALE_GENERATION" {
		t.Fatalf("stale start %v %v", r, e)
	}
	assertUnstarted(t, f, a, c)
}
func assertUnstarted(t *testing.T, f *fixture, a *v1.Admission, c *v1.StartExecutionCommand) {
	t.Helper()
	use, e := f.h.Grants.QueryCredentialUse(f.ctx, f.caller, c.CredentialRef)
	if e != nil || use != nil {
		t.Fatalf("credential leaked %v %v", use, e)
	}
	reservations, e := f.h.Budget.QueryReservations(f.ctx, f.caller, a.TaskId)
	if e != nil || reservations[0].ConsumedSends != 0 {
		t.Fatalf("send allowance leaked %v %v", reservations, e)
	}
	x, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil || x.Send.Phase != "REGISTERED" || f.calls.Load() != 0 {
		t.Fatalf("execution advanced %v %v", x, e)
	}
}

// 规则：G4、G7、开始-1
func TestStartRejectsChangedCallerOrDescriptor(t *testing.T) {
	for _, mode := range []string{"caller", "target", "digest", "attempt", "credential"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, 100, 80, false)
			a, c := prepareStart(t, f)
			original := proto.Clone(c).(*v1.StartExecutionCommand)
			caller := &v1.Caller{UserId: "u", IssuerId: "egress"}
			switch mode {
			case "caller":
				caller.IssuerId = "adapter"
				c.Header.Identity.IssuerId = "adapter"
			case "target":
				c.CallDescriptor.Target = "http://127.0.0.1:1/wrong"
			case "digest":
				c.Binding.DescriptorDigest = "altered"
			case "attempt":
				c.Binding.AttemptId.LocalId = "other-attempt"
			case "credential":
				c.CredentialRef.Name.LocalId = "missing"
			}
			r, e := f.h.Tasks.StartExecution(f.ctx, caller, c)
			if e != nil || r.Decision != v1.Decision_DECISION_REJECTED {
				t.Fatalf("invalid start %v %v", r, e)
			}
			assertUnstarted(t, f, a, original)
		})
	}
}

// 规则：G4、G8、开始-3
func TestExpiredGrantCannotStartAlreadyAdmittedOperation(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	g, e := f.h.Grants.QueryGrant(f.ctx, f.caller, f.grant)
	if e != nil {
		t.Fatal(e)
	}
	g.Ref = nil
	g.Issuer = nil
	g.Status = ""
	g.SemanticVersion = 0
	g.ValidUntilUnixMs = time.Now().Add(2 * time.Second).UnixMilli()
	r, e := f.h.Grants.Configure(f.ctx, f.caller, &v1.ConfigureGrantCommand{Header: header("short-grant"), Grant: g})
	accepted(t, r, e)
	f.grant = r.ResultRef
	a, c := prepareStart(t, f)
	time.Sleep(time.Until(time.UnixMilli(g.ValidUntilUnixMs)) + 5*time.Millisecond)
	r, e = f.h.Tasks.StartExecution(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
	if e != nil || r.GetError().GetCode() != "GRANT_INVALID" {
		t.Fatalf("expired grant %v %v", r, e)
	}
	assertUnstarted(t, f, a, c)
}

// 规则：G4、G5、开始-2
func TestQuestionAnswerOnlyFencesAnAdmittedStartWhenItChangesBasis(t *testing.T) {
	for _, changesBasis := range []bool{false, true} {
		t.Run(map[bool]string{false: "nonbasis", true: "basis"}[changesBasis], func(t *testing.T) {
			f := newFixture(t, 100, 80, false)
			a, c := prepareStart(t, f)
			task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, a.TaskId)
			if e != nil {
				t.Fatal(e)
			}
			goal, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("goal").Identity)
			if e != nil {
				t.Fatal(e)
			}
			q, e := f.h.Sessions.PublishQuestion(f.ctx, f.caller, &v1.PublishQuestionCommand{Header: header("question"), SessionId: goal.Receipt.SessionRef.Name, TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, ContentRef: f.parameters, ExpiresAtUnixMs: time.Now().Add(time.Minute).UnixMilli(), ChangesBasis: changesBasis})
			accepted(t, q, e)
			r, e := f.h.Sessions.SubmitInput(f.ctx, f.caller, &v1.SubmitInputCommand{Header: header("answer"), SessionId: goal.Receipt.SessionRef.Name, TaskId: a.TaskId, InputKind: "ANSWER", ContentRef: f.parameters, ExpectedInputVersion: task.InputVersion, ExpectedRequirementsVersion: task.RequirementsVersion, RequestRef: q.ResultRef})
			accepted(t, r, e)
			r, e = f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
			if changesBasis {
				if e != nil || r.GetError().GetCode() != "STALE_GENERATION" {
					t.Fatalf("basis start %v %v", r, e)
				}
				assertUnstarted(t, f, a, c)
			} else {
				accepted(t, r, e)
				if f.calls.Load() != 1 {
					t.Fatalf("nonbasis answer blocked admitted send: %d", f.calls.Load())
				}
			}
		})
	}
}
