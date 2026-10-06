package admission_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// 规则：G3、G4、G5、G9、准入-9
func TestConfirmedRawPayloadMatchesDisplayedAndSentBytes(t *testing.T) {
	for _, tc := range []struct {
		name            string
		body            []byte
		encoding, value string
	}{
		{"binary", []byte{0, 255, 128, 'x', 0}, "BASE64", "AP+AeAA="},
		{"empty", []byte{}, "UTF-8", ""},
		{"utf8-with-nul", []byte("raw\x00body"), "UTF-8", "raw\x00body"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			received := make(chan []byte, 2)
			target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, e := io.ReadAll(r.Body)
				if e != nil {
					t.Error(e)
				}
				received <- b
				w.Header().Set("Content-Type", "application/octet-stream")
				_, _ = w.Write(tc.body)
			})
			f := newFixtureWithTarget(t, 100, 80, false, target)
			first, start := prepareStart(t, f)
			r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
			accepted(t, r, e)
			<-received
			x, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, first.OperationId)
			if e != nil {
				t.Fatal(e)
			}
			raw, e := f.h.Ledger.QueryObservation(f.ctx, f.caller, x.Send.ObservationRef)
			if e != nil {
				t.Fatal(e)
			}
			r, e = f.h.Sessions.SubmitGoal(f.ctx, f.caller, &v1.SubmitGoalCommand{Identity: header("raw-confirmed-goal").Identity, ContractVersion: 1, FingerprintVersion: 1, SchemaId: "lerna.v1.SubmitGoal", Goal: "send confirmed observed bytes"})
			if e != nil {
				t.Fatal(e)
			}
			if e = f.h.Sessions.ProcessPending(f.ctx, f.caller); e != nil {
				t.Fatal(e)
			}
			goal, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, r.Identity)
			if e != nil {
				t.Fatal(e)
			}
			f.task = goal.Receipt.TaskRef
			r, e = f.h.Tasks.AcceptRequirements(f.ctx, f.caller, &v1.AcceptRequirementsCommand{Header: header("raw-requirements"), TaskRef: f.task, InputVersion: 1, Source: "TRUSTED_TEMPLATE", Conditions: []*v1.Requirement{{ConditionId: "created", DescriptionRef: goal.Receipt.InputRef, Necessary: true, VerificationRule: "TARGET_RECORD", RuleVersion: 1}}})
			accepted(t, r, e)
			r, e = f.h.Budget.Configure(f.ctx, f.caller, &v1.ConfigureBudgetCommand{Header: header("raw-budget"), TaskId: f.task.Name, Unit: "USD_MICRO", Limit: 80})
			accepted(t, r, e)
			g, e := f.h.Grants.QueryGrant(f.ctx, f.caller, f.grant)
			if e != nil {
				t.Fatal(e)
			}
			g.Ref = nil
			g.Issuer = nil
			g.Status = ""
			g.UsePoolId = ""
			g.Subject = f.task.Name
			g.UseMode = "SINGLE"
			g.MaxAdmissions = 1
			g.ConfirmationRequired = true
			g.Permissions[0].ParameterMode = "EXACT"
			g.Permissions[0].ParametersRef = raw.BodyRef
			r, e = f.h.Grants.RequestGrantConfirmation(f.ctx, f.caller, &v1.RequestGrantConfirmationCommand{Header: header("raw-grant-request"), Grant: g})
			accepted(t, r, e)
			grantMatter, e := f.h.Sessions.ReadConfirmation(f.ctx, f.caller, r.ResultRef)
			if e != nil {
				t.Fatal(e)
			}
			type parameters struct{ Encoding, Value, MediaType string }
			var grantDescription struct{ Parameters []parameters }
			if e = json.Unmarshal([]byte(grantMatter.Description), &grantDescription); e != nil {
				t.Fatal(e)
			}
			expected := parameters{tc.encoding, tc.value, "application/octet-stream"}
			if len(grantDescription.Parameters) != 1 || grantDescription.Parameters[0] != expected {
				t.Fatalf("grant displays different bytes: %s", grantMatter.Description)
			}
			r, e = f.h.Sessions.RespondConfirmation(f.ctx, f.caller, &v1.RespondConfirmationCommand{Header: header("raw-grant-approve"), ConfirmationRef: grantMatter.Ref, BindingDigest: grantMatter.BindingDigest, Decision: "APPROVE"})
			accepted(t, r, e)
			r, e = f.h.Grants.IssueGrant(f.ctx, f.caller, &v1.IssueGrantCommand{Header: header("raw-grant-issue"), ConfirmationRef: r.ResultRef})
			accepted(t, r, e)
			f.grant = r.ResultRef
			f.parameters = raw.BodyRef
			f.suffix = "-confirmed-raw"
			proposal := f.propose(t, nil)
			r, e = f.h.Tasks.RequestAdmissionConfirmation(f.ctx, f.caller, &v1.RequestAdmissionConfirmationCommand{Header: header("raw-action-request"), TaskId: f.task.Name, ProposalRef: proposal, GrantRef: f.grant})
			accepted(t, r, e)
			actionMatter, e := f.h.Sessions.ReadConfirmation(f.ctx, f.caller, r.ResultRef)
			if e != nil {
				t.Fatal(e)
			}
			var actionDescription struct{ Parameters parameters }
			if e = json.Unmarshal([]byte(actionMatter.Description), &actionDescription); e != nil {
				t.Fatal(e)
			}
			if actionDescription.Parameters != expected {
				t.Fatalf("action displays different bytes: %s", actionMatter.Description)
			}
			r, e = f.h.Sessions.RespondConfirmation(f.ctx, f.caller, &v1.RespondConfirmationCommand{Header: header("raw-action-approve"), ConfirmationRef: actionMatter.Ref, BindingDigest: actionMatter.BindingDigest, Decision: "APPROVE"})
			accepted(t, r, e)
			r, e = f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("raw-admit"), TaskId: f.task.Name, ProposalRef: proposal, GrantRef: f.grant, ConfirmationRef: r.ResultRef})
			accepted(t, r, e)
			a, e := f.h.Tasks.QueryAdmission(f.ctx, f.caller, r.ResultRef)
			if e != nil {
				t.Fatal(e)
			}
			if e = f.h.Tasks.ProcessHandoffs(f.ctx, f.caller); e != nil {
				t.Fatal(e)
			}
			claim, e := f.h.LedgerWork.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: ledgerHeader("raw-claim").Identity, ContractVersion: 1, Action: "CLAIM", AllowedTypes: []string{"EXECUTE_OPERATION"}, Limit: 100, LeaseMs: 30000, ProcessInstance: "raw-worker"})
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
			r, e = f.h.Ledger.Prepare(f.ctx, f.caller, &v1.PrepareExecutionCommand{Header: ledgerHeader("raw-prepare"), OperationId: a.OperationId, ProcessInstance: "raw-worker", Claim: executionClaim})
			accepted(t, r, e)
			x, e = f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
			if e != nil {
				t.Fatal(e)
			}
			binding := &v1.ExitCredentialBinding{UserId: "u", TaskId: a.TaskId, SubjectId: a.TaskId, OperationId: a.OperationId, AttemptId: x.Attempt.Ref.Name, SendSeq: x.Send.SendSeq, AdmissionRef: a.Ref, GrantUseRef: a.GrantUseRef, CallerIssuerId: "egress", Audience: "egress", ExecutorEndpointId: a.ExecutorEndpointId, ExecutorInstance: x.Send.ProcessInstance, DescriptorDigest: x.CallDescriptor.Digest, UseRight: a.CapabilitySnapshot.UseRight, ProcessingPurpose: a.CapabilitySnapshot.ProcessingPurpose, RequirementsVersion: a.RequirementsVersion, InputVersion: a.InputVersion, ControlGeneration: a.ControlGeneration, BudgetReservationRef: a.BudgetBasis.ReservationRef}
			r, e = f.h.Grants.IssueCredential(f.ctx, f.caller, &v1.IssueExitCredentialCommand{Header: header("raw-credential"), AdmissionRef: a.Ref, Binding: binding, ExpiresAtUnixMs: time.Now().Add(time.Minute).UnixMilli()})
			accepted(t, r, e)
			sh := header("start:" + x.Send.Ref.Name.LocalId)
			sh.Identity.IssuerId = "egress"
			r, e = f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, &v1.StartExecutionCommand{Header: sh, AdmissionRef: a.Ref, CredentialRef: r.ResultRef, Binding: binding, Claim: executionClaim, CallDescriptor: x.CallDescriptor})
			accepted(t, r, e)
			if actual := <-received; !bytes.Equal(actual, tc.body) {
				t.Fatalf("sent %x, displayed original %x", actual, tc.body)
			}
			for _, original := range []*v1.Confirmation{grantMatter, actionMatter} {
				historical, e := f.h.Sessions.ReadConfirmation(f.ctx, f.caller, original.Ref)
				if e != nil || !proto.Equal(historical, original) {
					t.Fatalf("consumed original display changed %v %v", historical, e)
				}
			}
			if f.calls.Load() != 2 {
				t.Fatalf("unexpected actual sends %d", f.calls.Load())
			}
		})
	}
}
