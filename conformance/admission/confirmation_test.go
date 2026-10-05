package admission_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/cmd/assembly"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// 规则：G3、G4、G6、准入-9
func TestTrustedOperationConfirmationConsumedWithAdmission(t *testing.T) {
	f := newFixture(t, 100, 80, true)
	p := f.propose(t, nil)
	goal, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("goal").Identity)
	if e != nil {
		t.Fatal(e)
	}
	r, e := f.h.Tasks.RequestAdmissionConfirmation(f.ctx, f.caller, &v1.RequestAdmissionConfirmationCommand{Header: header("request-confirmation"), TaskId: f.task.Name, ProposalRef: p, GrantRef: f.grant, SessionId: goal.Receipt.SessionRef.Name})
	accepted(t, r, e)
	requestRef := r.ResultRef
	pending, e := f.h.Sessions.QueryConfirmation(f.ctx, f.caller, requestRef)
	if e != nil || pending == nil || pending.MatterType != "OPERATION_ADMISSION" || pending.State != "PENDING" || pending.BindingDigest == "" || !strings.Contains(pending.Description, "create a record") {
		t.Fatalf("confirmation: %v %v", pending, e)
	}
	before, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	r, e = f.h.Sessions.RespondConfirmation(f.ctx, f.caller, &v1.RespondConfirmationCommand{Header: header("approve"), ConfirmationRef: requestRef, BindingDigest: pending.BindingDigest, Decision: "APPROVE"})
	accepted(t, r, e)
	approvedRef := r.ResultRef
	after, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil || !proto.Equal(before, after) {
		t.Fatalf("approval changed task versions: %v %v", after, e)
	}
	session, e := f.h.Sessions.QuerySession(f.ctx, f.caller, goal.Receipt.SessionRef.Name)
	if e != nil || len(session.Inputs) != 2 || session.Inputs[1].InputKind != "CONFIRMATION" || !proto.Equal(session.Inputs[1].ConfirmationRef, approvedRef) {
		t.Fatalf("missing response event: %v %v", session, e)
	}
	event := session.Inputs[1]
	delivery, e := f.h.Sessions.QueryInput(f.ctx, f.caller, &v1.Ref{Name: event.InputId, Revision: 1, SchemaId: "lerna.v1.SessionInput"})
	if e != nil || delivery == nil || !proto.Equal(delivery.Input, event) {
		t.Fatalf("confirmation event not queryable: %v %v", delivery, e)
	}
	r, e = f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("admit"), TaskId: f.task.Name, ProposalRef: p, GrantRef: f.grant, ConfirmationRef: approvedRef})
	accepted(t, r, e)
	consumed, e := f.h.Sessions.QueryCurrentConfirmation(f.ctx, f.caller, requestRef.Name)
	if e != nil || consumed.State != "CONSUMED" || !proto.Equal(consumed.GetConsumedAdmissionRef(), r.ResultRef) || consumed.Ref.Revision <= approvedRef.Revision {
		t.Fatalf("consumption: %v %v", consumed, e)
	}
	historical, e := f.h.Sessions.QueryConfirmation(f.ctx, f.caller, approvedRef)
	if e != nil || historical.State != "APPROVED" {
		t.Fatalf("approval history changed: %v %v", historical, e)
	}
	historical, e = f.h.Sessions.QueryConfirmation(f.ctx, f.caller, requestRef)
	if e != nil || !proto.Equal(historical, pending) {
		t.Fatalf("request history changed: %v %v", historical, e)
	}
	uses, e := f.h.Grants.QueryUses(f.ctx, f.caller, f.task.Name)
	if e != nil || len(uses) != 1 {
		t.Fatalf("uses: %v %v", uses, e)
	}
	budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil || budget.Reserved != 30 {
		t.Fatalf("budget: %v %v", budget, e)
	}
	if f.calls.Load() != 0 {
		t.Fatal("confirmation called target")
	}
}

// 规则：G3、G4、G7、G12
func TestGrantIssuanceConfirmationIsTypedAndConsumedWithoutTask(t *testing.T) {
	h, e := assembly.Open(filepath.Join(t.TempDir(), "grant.db"), "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	ctx := context.Background()
	caller := &v1.Caller{UserId: "u", IssuerId: "host"}
	subject := &v1.GlobalName{UserId: "u", AuthorityDomainId: "d", ObjectKind: "task", LocalId: "future-task"}
	spec := &v1.Grant{Subject: subject, Permissions: []*v1.PermissionClause{{Action: "CREATE", Resource: "record-A", UseRight: "INVOKE", ProcessingPurpose: "CURRENT_TASK", ExecutorEndpointId: "local-api", ParameterMode: "ANY"}}, ValidFromUnixMs: time.Now().Add(-time.Minute).UnixMilli(), ValidUntilUnixMs: time.Now().Add(time.Hour).UnixMilli(), UseMode: "CONTINUOUS", MaxAdmissions: 3}
	r, e := h.Grants.RequestGrantConfirmation(ctx, caller, &v1.RequestGrantConfirmationCommand{Header: header("request-grant"), Grant: spec})
	accepted(t, r, e)
	pending, e := h.Sessions.QueryConfirmation(ctx, caller, r.ResultRef)
	if e != nil || pending.MatterType != "GRANT_ISSUANCE" || pending.GetGrantIssuance() == nil || pending.GetOperationAdmission() != nil {
		t.Fatalf("grant matter: %v %v", pending, e)
	}
	r, e = h.Sessions.RespondConfirmation(ctx, caller, &v1.RespondConfirmationCommand{Header: header("approve-grant"), ConfirmationRef: pending.Ref, BindingDigest: pending.BindingDigest, Decision: "APPROVE"})
	accepted(t, r, e)
	approved := r.ResultRef
	r, e = h.Grants.IssueGrant(ctx, caller, &v1.IssueGrantCommand{Header: header("issue-grant"), ConfirmationRef: approved})
	accepted(t, r, e)
	grant, e := h.Grants.QueryGrant(ctx, caller, r.ResultRef)
	if e != nil || grant == nil || grant.Status != "ACTIVE" || grant.MaxAdmissions != 3 || !proto.Equal(grant.Subject, subject) {
		t.Fatalf("issued grant: %v %v", grant, e)
	}
	consumed, e := h.Sessions.QueryCurrentConfirmation(ctx, caller, pending.Ref.Name)
	if e != nil || consumed.State != "CONSUMED" || consumed.GetConsumedGrantIssuanceRef() == nil || consumed.GetConsumedAdmissionRef() != nil {
		t.Fatalf("typed consumption: %v %v", consumed, e)
	}
	issuance, e := h.Grants.QueryGrantIssuance(ctx, caller, consumed.GetConsumedGrantIssuanceRef())
	if e != nil || issuance.State != "ISSUED" || !proto.Equal(issuance.GrantRef, grant.Ref) {
		t.Fatalf("issuance: %v %v", issuance, e)
	}
	r, e = h.Grants.IssueGrant(ctx, caller, &v1.IssueGrantCommand{Header: header("issue-again"), ConfirmationRef: approved})
	if e != nil || r.Decision != v1.Decision_DECISION_REJECTED || r.ResultRef != nil {
		t.Fatalf("reconsumed grant confirmation: %v %v", r, e)
	}
	task, e := h.Tasks.QueryTask(ctx, caller, subject)
	if e != nil || task != nil {
		t.Fatalf("grant created task: %v %v", task, e)
	}
}

// 规则：G3、G4、G6、准入-9
func TestWithdrawnConfirmationCannotConsumeAuthority(t *testing.T) {
	f := newFixture(t, 100, 80, true)
	p := f.propose(t, nil)
	goal, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("goal").Identity)
	if e != nil {
		t.Fatal(e)
	}
	before, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	r, e := f.h.Tasks.RequestAdmissionConfirmation(f.ctx, f.caller, &v1.RequestAdmissionConfirmationCommand{Header: header("request-confirmation"), TaskId: f.task.Name, ProposalRef: p, GrantRef: f.grant, SessionId: goal.Receipt.SessionRef.Name})
	accepted(t, r, e)
	pending, e := f.h.Sessions.QueryConfirmation(f.ctx, f.caller, r.ResultRef)
	if e != nil {
		t.Fatal(e)
	}
	r, e = f.h.Sessions.RespondConfirmation(f.ctx, f.caller, &v1.RespondConfirmationCommand{Header: header("approve"), ConfirmationRef: pending.Ref, BindingDigest: pending.BindingDigest, Decision: "APPROVE"})
	accepted(t, r, e)
	approved := r.ResultRef
	responseReplay, e := f.h.Sessions.RespondConfirmation(f.ctx, f.caller, &v1.RespondConfirmationCommand{Header: header("approve"), ConfirmationRef: pending.Ref, BindingDigest: pending.BindingDigest, Decision: "APPROVE"})
	if e != nil || !proto.Equal(responseReplay, r) {
		t.Fatalf("response replay: %v %v", responseReplay, e)
	}
	r, e = f.h.Sessions.WithdrawConfirmation(f.ctx, f.caller, &v1.WithdrawConfirmationCommand{Header: header("withdraw"), ConfirmationRef: approved})
	accepted(t, r, e)
	withdrawn := r.ResultRef
	withdrawReplay, e := f.h.Sessions.WithdrawConfirmation(f.ctx, f.caller, &v1.WithdrawConfirmationCommand{Header: header("withdraw"), ConfirmationRef: approved})
	if e != nil || !proto.Equal(withdrawReplay, r) {
		t.Fatalf("withdrawal replay: %v %v", withdrawReplay, e)
	}
	session, e := f.h.Sessions.QuerySession(f.ctx, f.caller, goal.Receipt.SessionRef.Name)
	if e != nil || len(session.Inputs) != 3 {
		t.Fatalf("duplicate events: %v %v", session, e)
	}
	for i, expected := range []*v1.Ref{approved, withdrawn} {
		event := session.Inputs[i+1]
		delivery, e := f.h.Sessions.QueryInput(f.ctx, f.caller, &v1.Ref{Name: event.InputId, Revision: 1, SchemaId: "lerna.v1.SessionInput"})
		if e != nil || delivery == nil || !proto.Equal(delivery.Input, event) || !proto.Equal(event.ConfirmationRef, expected) {
			t.Fatalf("event query: %v %v", delivery, e)
		}
	}
	after, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil || !proto.Equal(before, after) {
		t.Fatalf("confirmation changed task: %v %v", after, e)
	}
	current, e := f.h.Sessions.QueryCurrentConfirmation(f.ctx, f.caller, pending.Ref.Name)
	if e != nil || current.State != "WITHDRAWN" {
		t.Fatalf("withdrawal: %v %v", current, e)
	}
	r, e = f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("admit"), TaskId: f.task.Name, ProposalRef: p, GrantRef: f.grant, ConfirmationRef: approved})
	if e != nil || r.GetError().GetCode() != "CONFIRMATION_INVALID" {
		t.Fatalf("withdrawn admission: %v %v", r, e)
	}
	uses, e := f.h.Grants.QueryUses(f.ctx, f.caller, f.task.Name)
	if e != nil || len(uses) != 0 {
		t.Fatalf("partial use: %v %v", uses, e)
	}
	budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil || budget.Reserved != 0 {
		t.Fatalf("partial budget: %v %v", budget, e)
	}
	if f.calls.Load() != 0 {
		t.Fatal("withdrawal called target")
	}
}

// 规则：G3、G4、G7、准入-9
func TestSingleGrantConfirmationShowsExactParameters(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	g, e := f.h.Grants.QueryGrant(f.ctx, f.caller, f.grant)
	if e != nil {
		t.Fatal(e)
	}
	g.Ref = nil
	g.Issuer = nil
	g.Status = ""
	g.UsePoolId = ""
	g.UseMode = "SINGLE"
	g.MaxAdmissions = 1
	g.Permissions[0].ParameterMode = "EXACT"
	g.Permissions[0].ParametersRef = f.parameters
	r, e := f.h.Grants.RequestGrantConfirmation(f.ctx, f.caller, &v1.RequestGrantConfirmationCommand{Header: header("single-description"), Grant: g})
	accepted(t, r, e)
	c, e := f.h.Sessions.QueryConfirmation(f.ctx, f.caller, r.ResultRef)
	if e != nil || !strings.Contains(c.Description, "create a record") {
		t.Fatalf("parameters hidden from user: %v %v", c, e)
	}
	if f.calls.Load() != 0 {
		t.Fatal("confirmation called target")
	}
}

// 规则：G3、G4、G6、准入-9
func TestConfirmationRejectsWrongDigestUntrustedIssuerAndCrossConsumption(t *testing.T) {
	f := newFixture(t, 100, 80, true)
	p := f.propose(t, nil)
	r, e := f.h.Tasks.RequestAdmissionConfirmation(f.ctx, f.caller, &v1.RequestAdmissionConfirmationCommand{Header: header("request-confirmation-security"), TaskId: f.task.Name, ProposalRef: p, GrantRef: f.grant})
	accepted(t, r, e)
	pending, e := f.h.Sessions.QueryConfirmation(f.ctx, f.caller, r.ResultRef)
	if e != nil {
		t.Fatal(e)
	}
	r, e = f.h.Sessions.RespondConfirmation(f.ctx, f.caller, &v1.RespondConfirmationCommand{Header: header("wrong-digest"), ConfirmationRef: pending.Ref, BindingDigest: "different", Decision: "APPROVE"})
	if e != nil || r.GetError().GetCode() != "CONFIRMATION_INVALID" {
		t.Fatalf("digest: %v %v", r, e)
	}
	untrusted := &v1.Caller{UserId: "u", IssuerId: "reasoner"}
	untrustedHeader := header("untrusted")
	untrustedHeader.Identity.IssuerId = "reasoner"
	r, e = f.h.Sessions.RespondConfirmation(f.ctx, untrusted, &v1.RespondConfirmationCommand{Header: untrustedHeader, ConfirmationRef: pending.Ref, BindingDigest: pending.BindingDigest, Decision: "APPROVE"})
	if e != nil || r.GetError().GetCode() != "PERMISSION_DENIED" {
		t.Fatalf("issuer: %v %v", r, e)
	}
	current, e := f.h.Sessions.QueryCurrentConfirmation(f.ctx, f.caller, pending.Ref.Name)
	if e != nil || !proto.Equal(current, pending) {
		t.Fatalf("rejected response changed confirmation: %v %v", current, e)
	}
	r, e = f.h.Sessions.RespondConfirmation(f.ctx, f.caller, &v1.RespondConfirmationCommand{Header: header("approve"), ConfirmationRef: pending.Ref, BindingDigest: pending.BindingDigest, Decision: "APPROVE"})
	accepted(t, r, e)
	approved := r.ResultRef
	r, e = f.h.Grants.IssueGrant(f.ctx, f.caller, &v1.IssueGrantCommand{Header: header("cross-consume"), ConfirmationRef: approved})
	if e != nil || r.GetError().GetCode() != "CONFIRMATION_INVALID" {
		t.Fatalf("cross consumption: %v %v", r, e)
	}
	r, e = f.h.Sessions.RespondConfirmation(f.ctx, f.caller, &v1.RespondConfirmationCommand{Header: header("second-response"), ConfirmationRef: pending.Ref, BindingDigest: pending.BindingDigest, Decision: "REJECT"})
	if e != nil || r.GetError().GetCode() != "CONFIRMATION_INVALID" {
		t.Fatalf("second response: %v %v", r, e)
	}
	current, e = f.h.Sessions.QueryCurrentConfirmation(f.ctx, f.caller, pending.Ref.Name)
	if e != nil || current.State != "APPROVED" || !proto.Equal(current.Ref, approved) {
		t.Fatalf("approval lost: %v %v", current, e)
	}
	uses, e := f.h.Grants.QueryUses(f.ctx, f.caller, f.task.Name)
	if e != nil || len(uses) != 0 {
		t.Fatalf("leaked use: %v %v", uses, e)
	}
	budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil || budget.Reserved != 0 {
		t.Fatalf("leaked budget: %v %v", budget, e)
	}
	if f.calls.Load() != 0 {
		t.Fatal("response called target")
	}
}

// 规则：G3、G4、G6、准入-9、准入-11
func TestLateContentFailureRollsBackConfirmationUseAndBudget(t *testing.T) {
	f := newFixture(t, 100, 80, true)
	p := f.propose(t, func(p *v1.Proposal) {
		p.Step.ContentRefs = []*v1.Ref{{Name: &v1.GlobalName{UserId: "u", AuthorityDomainId: "d/content", ObjectKind: "content", LocalId: "missing"}, Revision: 1, SchemaId: "lerna.v1.Content"}}
	})
	r, e := f.h.Tasks.RequestAdmissionConfirmation(f.ctx, f.caller, &v1.RequestAdmissionConfirmationCommand{Header: header("request-confirmation-late"), TaskId: f.task.Name, ProposalRef: p, GrantRef: f.grant})
	accepted(t, r, e)
	pending, e := f.h.Sessions.QueryConfirmation(f.ctx, f.caller, r.ResultRef)
	if e != nil {
		t.Fatal(e)
	}
	r, e = f.h.Sessions.RespondConfirmation(f.ctx, f.caller, &v1.RespondConfirmationCommand{Header: header("approve"), ConfirmationRef: pending.Ref, BindingDigest: pending.BindingDigest, Decision: "APPROVE"})
	accepted(t, r, e)
	approved := r.ResultRef
	r, e = f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("admit"), TaskId: f.task.Name, ProposalRef: p, GrantRef: f.grant, ConfirmationRef: approved})
	if e != nil || r.Decision != v1.Decision_DECISION_REJECTED {
		t.Fatalf("missing content admitted: %v %v", r, e)
	}
	current, e := f.h.Sessions.QueryCurrentConfirmation(f.ctx, f.caller, pending.Ref.Name)
	if e != nil || current.State != "APPROVED" || !proto.Equal(current.Ref, approved) {
		t.Fatalf("consumed on rejected admission: %v %v", current, e)
	}
	uses, e := f.h.Grants.QueryUses(f.ctx, f.caller, f.task.Name)
	if e != nil || len(uses) != 0 {
		t.Fatalf("use leaked: %v %v", uses, e)
	}
	budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil || budget.Reserved != 0 {
		t.Fatalf("budget leaked: %v %v", budget, e)
	}
	if f.calls.Load() != 0 {
		t.Fatal("failed admission called target")
	}
}

// 规则：G3、G4、G6、准入-9
func TestParameterChangeInvalidatesPendingAndApprovedConfirmation(t *testing.T) {
	for _, approveFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-approval", true: "after-approval"}[approveFirst], func(t *testing.T) {
			f := newFixture(t, 100, 80, true)
			p := f.propose(t, nil)
			r, e := f.h.Tasks.RequestAdmissionConfirmation(f.ctx, f.caller, &v1.RequestAdmissionConfirmationCommand{Header: header("confirmation-parameter-change"), TaskId: f.task.Name, ProposalRef: p, GrantRef: f.grant})
			accepted(t, r, e)
			pending, e := f.h.Sessions.QueryConfirmation(f.ctx, f.caller, r.ResultRef)
			if e != nil {
				t.Fatal(e)
			}
			response := &v1.RespondConfirmationCommand{Header: header("approve"), ConfirmationRef: pending.Ref, BindingDigest: pending.BindingDigest, Decision: "APPROVE"}
			selected := pending.Ref
			if approveFirst {
				r, e = f.h.Sessions.RespondConfirmation(f.ctx, f.caller, response)
				accepted(t, r, e)
				selected = r.ResultRef
			}
			changed, e := f.h.Content.Stage(f.ctx, f.caller, &v1.SubmitGoalCommand{Identity: header("changed-parameters").Identity, ContractVersion: 1, FingerprintVersion: 1, SchemaId: "lerna.v1.SubmitGoal", Goal: "create a different record"})
			if e != nil {
				t.Fatal(e)
			}
			f.parameters = changed
			f.suffix = "-changed"
			newProposal := f.propose(t, nil)
			if approveFirst {
				r, e = f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("admit"), TaskId: f.task.Name, ProposalRef: newProposal, GrantRef: f.grant, ConfirmationRef: selected})
			} else {
				r, e = f.h.Sessions.RespondConfirmation(f.ctx, f.caller, response)
			}
			if e != nil || r.GetError().GetCode() != "CONFIRMATION_INVALID" {
				t.Fatalf("accepted stale matter: %v %v", r, e)
			}
			current, e := f.h.Sessions.QueryCurrentConfirmation(f.ctx, f.caller, pending.Ref.Name)
			if e != nil || !proto.Equal(current.Ref, selected) || current.State == "CONSUMED" {
				t.Fatalf("changed confirmation: %v %v", current, e)
			}
			uses, e := f.h.Grants.QueryUses(f.ctx, f.caller, f.task.Name)
			if e != nil || len(uses) != 0 {
				t.Fatalf("leaked use: %v %v", uses, e)
			}
			budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
			if e != nil || budget.Reserved != 0 {
				t.Fatalf("leaked budget: %v %v", budget, e)
			}
			if f.calls.Load() != 0 {
				t.Fatal("changed matter called target")
			}
		})
	}
}

// 规则：G3、G4、G6、准入-9
func TestConcurrentConfirmationResponsesCommitOnlyOneDecision(t *testing.T) {
	f := newFixture(t, 100, 80, true)
	p := f.propose(t, nil)
	r, e := f.h.Tasks.RequestAdmissionConfirmation(f.ctx, f.caller, &v1.RequestAdmissionConfirmationCommand{Header: header("concurrent-confirmation"), TaskId: f.task.Name, ProposalRef: p, GrantRef: f.grant})
	accepted(t, r, e)
	pending, e := f.h.Sessions.QueryConfirmation(f.ctx, f.caller, r.ResultRef)
	if e != nil {
		t.Fatal(e)
	}
	type result struct {
		receipt  *v1.CommandReceipt
		err      error
		decision string
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for _, decision := range []string{"APPROVE", "REJECT"} {
		go func(decision string) {
			<-start
			r, e := f.h.Sessions.RespondConfirmation(f.ctx, f.caller, &v1.RespondConfirmationCommand{Header: header("respond-" + decision), ConfirmationRef: pending.Ref, BindingDigest: pending.BindingDigest, Decision: decision})
			results <- result{r, e, decision}
		}(decision)
	}
	close(start)
	winner := ""
	count := 0
	for i := 0; i < 2; i++ {
		got := <-results
		if got.err != nil {
			t.Fatal(got.err)
		}
		if got.receipt.Decision == v1.Decision_DECISION_ACCEPTED {
			count++
			winner = got.decision
		} else if got.receipt.GetError().GetCode() != "CONFIRMATION_INVALID" {
			t.Fatal(got.receipt)
		}
	}
	current, e := f.h.Sessions.QueryCurrentConfirmation(f.ctx, f.caller, pending.Ref.Name)
	expected := "REJECTED"
	if winner == "APPROVE" {
		expected = "APPROVED"
	}
	if e != nil || count != 1 || current.State != expected || current.Ref.Revision != 2 {
		t.Fatalf("responses: count %d current %v err %v", count, current, e)
	}
	if f.calls.Load() != 0 {
		t.Fatal("response called target")
	}
}

// 规则：G3、G4、G6、准入-9
func TestWithdrawalAndAdmissionHaveOneAtomicWinner(t *testing.T) {
	f := newFixture(t, 100, 80, true)
	p := f.propose(t, nil)
	r, e := f.h.Tasks.RequestAdmissionConfirmation(f.ctx, f.caller, &v1.RequestAdmissionConfirmationCommand{Header: header("withdraw-race"), TaskId: f.task.Name, ProposalRef: p, GrantRef: f.grant})
	accepted(t, r, e)
	pending, e := f.h.Sessions.QueryConfirmation(f.ctx, f.caller, r.ResultRef)
	if e != nil {
		t.Fatal(e)
	}
	r, e = f.h.Sessions.RespondConfirmation(f.ctx, f.caller, &v1.RespondConfirmationCommand{Header: header("approve"), ConfirmationRef: pending.Ref, BindingDigest: pending.BindingDigest, Decision: "APPROVE"})
	accepted(t, r, e)
	approved := r.ResultRef
	type result struct {
		receipt *v1.CommandReceipt
		err     error
		action  string
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	go func() {
		<-start
		r, e := f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("admit"), TaskId: f.task.Name, ProposalRef: p, GrantRef: f.grant, ConfirmationRef: approved})
		results <- result{r, e, "admit"}
	}()
	go func() {
		<-start
		r, e := f.h.Sessions.WithdrawConfirmation(f.ctx, f.caller, &v1.WithdrawConfirmationCommand{Header: header("withdraw"), ConfirmationRef: approved})
		results <- result{r, e, "withdraw"}
	}()
	close(start)
	winner := ""
	count := 0
	for i := 0; i < 2; i++ {
		got := <-results
		if got.err != nil {
			t.Fatal(got.err)
		}
		if got.receipt.Decision == v1.Decision_DECISION_ACCEPTED {
			count++
			winner = got.action
		} else if got.receipt.GetError().GetCode() != "CONFIRMATION_INVALID" {
			t.Fatal(got.receipt)
		}
	}
	current, e := f.h.Sessions.QueryCurrentConfirmation(f.ctx, f.caller, pending.Ref.Name)
	expected := "WITHDRAWN"
	expectedUses := 0
	expectedReserved := int64(0)
	if winner == "admit" {
		expected = "CONSUMED"
		expectedUses = 1
		expectedReserved = 30
	}
	if e != nil || count != 1 || current.State != expected {
		t.Fatalf("winner %q count %d current %v err %v", winner, count, current, e)
	}
	uses, e := f.h.Grants.QueryUses(f.ctx, f.caller, f.task.Name)
	if e != nil || len(uses) != expectedUses {
		t.Fatalf("uses: %v %v", uses, e)
	}
	budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil || budget.Reserved != expectedReserved {
		t.Fatalf("budget: %v %v", budget, e)
	}
	if f.calls.Load() != 0 {
		t.Fatal("confirmation race called target")
	}
}
