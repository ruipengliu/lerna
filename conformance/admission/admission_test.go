package admission_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/ruipengliu/lerna/cmd/assembly"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/defaults/scripted"
)

// 规则：G2、G6、R1、准入-1、准入-2、准入-3
func TestTrustedRequirementsAndProposalDoNotExecute(t *testing.T) {
	h, err := assembly.Open(filepath.Join(t.TempDir(), "state.db"), "u", "d")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	ctx := context.Background()
	caller := &v1.Caller{UserId: "u", IssuerId: "host"}
	c := &v1.SubmitGoalCommand{Identity: &v1.CommandIdentity{UserId: "u", IssuerId: "host", TargetDomainId: "d", CommandId: "goal"}, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "create a record"}
	r, err := h.Sessions.SubmitGoal(ctx, caller, c)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Sessions.ProcessPending(ctx, caller); err != nil {
		t.Fatal(err)
	}
	q, err := h.Durable.QueryReceipt(ctx, caller, r.Identity)
	if err != nil {
		t.Fatal(err)
	}
	task := q.Receipt.TaskRef
	accepted, err := h.Tasks.AcceptRequirements(ctx, caller, &v1.AcceptRequirementsCommand{Header: header("requirements"), TaskRef: task, InputVersion: 1, Conditions: []*v1.Requirement{{ConditionId: "created", DescriptionRef: q.Receipt.InputRef, Necessary: true, VerificationRule: "TARGET_RECORD", RuleVersion: 1}}, Source: "TRUSTED_TEMPLATE"})
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Decision != v1.Decision_DECISION_ACCEPTED {
		t.Fatalf("requirements: %v", accepted)
	}
	snap, err := h.Tasks.RequestProposal(ctx, caller, &v1.RequestProposalCommand{Header: header("request"), TaskId: task.Name})
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := (&scripted.Reasoner{Step: &v1.ActionStep{StepId: "one"}}).Propose(ctx, snap)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := h.Tasks.ReceiveProposal(ctx, caller, &v1.ReceiveProposalCommand{Header: header("proposal"), Proposal: proposal})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Decision != v1.Decision_DECISION_ACCEPTED {
		t.Fatal(receipt)
	}
	state, err := h.Tasks.QueryPlanning(ctx, caller, task.Name)
	if err != nil {
		t.Fatal(err)
	}
	if state.Requirements.Source != "TRUSTED_TEMPLATE" || state.Snapshot.InputVersion != 1 || len(state.AdmissionRefs) != 0 {
		t.Fatalf("unexpected state: %v", state)
	}
}
func header(id string) *v1.CommandHeader {
	return &v1.CommandHeader{Identity: &v1.CommandIdentity{UserId: "u", IssuerId: "host", TargetDomainId: "d", CommandId: id}, ContractVersion: 1, FingerprintVersion: 1, SchemaId: "lerna.v1.AdmissionCommands"}
}

// 规则：G4、G6、G7、G10、R1、准入-7、准入-8、准入-10
func TestAdmissionReservesExactAuthorityWithoutExecuting(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	proposal := f.propose(t, nil)
	r, err := f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("admit"), TaskId: f.task.Name, ProposalRef: proposal, GrantRef: f.grant})
	if err != nil {
		t.Fatal(err)
	}
	if r.Decision != v1.Decision_DECISION_ACCEPTED {
		t.Fatalf("admission: %v", r)
	}
	admission, err := f.h.Tasks.QueryAdmission(f.ctx, f.caller, r.ResultRef)
	if err != nil {
		t.Fatal(err)
	}
	if admission.RequirementsVersion != 1 || admission.InputVersion != 1 || admission.ControlGeneration != 2 || admission.BudgetBasis.Ceiling != 30 || admission.OperationId == nil || admission.LedgerDomainId != "d/ledger" {
		t.Fatalf("basis: %v", admission)
	}
	grantUse, err := f.h.Grants.QueryUse(f.ctx, f.caller, admission.OperationId)
	if err != nil || grantUse == nil {
		t.Fatalf("use: %v %v", grantUse, err)
	}
	reservation, err := f.h.Budget.QueryReservation(f.ctx, f.caller, admission.BudgetBasis.ReservationRef)
	if err != nil || reservation.Ceiling != 30 {
		t.Fatalf("reservation: %v %v", reservation, err)
	}
	user, err := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if err != nil || user.Reserved != 30 || user.Settled != 0 {
		t.Fatalf("user budget: %v %v", user, err)
	}
	task, err := f.h.Budget.QueryBudget(f.ctx, f.caller, f.task.Name)
	if err != nil || task.Reserved != 30 {
		t.Fatalf("task budget: %v %v", task, err)
	}
	if f.calls.Load() != 0 {
		t.Fatal("proposal or admission called target")
	}
	replay, err := f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("admit"), TaskId: f.task.Name, ProposalRef: proposal, GrantRef: f.grant})
	if err != nil || !proto.Equal(replay, r) {
		t.Fatalf("replay differs: %v %v", replay, err)
	}
}

type fixture struct {
	path                                string
	suffix                              string
	h                                   *assembly.Harness
	ctx                                 context.Context
	caller                              *v1.Caller
	task, parameters, capability, grant *v1.Ref
	calls                               atomic.Int64
}

func newFixture(t *testing.T, userLimit, taskLimit int64, confirmation bool) *fixture {
	return newFixtureWithTarget(t, userLimit, taskLimit, confirmation, nil)
}
func newFixtureWithTarget(t *testing.T, userLimit, taskLimit int64, confirmation bool, target http.Handler) *fixture {
	return newFixtureWithTargetAndFiles(t, userLimit, taskLimit, confirmation, target, nil)
}
func newFixtureWithTargetAndFiles(t *testing.T, userLimit, taskLimit int64, confirmation bool, target http.Handler, roots map[string]string) *fixture {
	t.Helper()
	f := &fixture{ctx: context.Background(), caller: &v1.Caller{UserId: "u", IssuerId: "host"}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		if target != nil {
			target.ServeHTTP(w, r)
		} else {
			w.WriteHeader(200)
		}
	}))
	t.Cleanup(server.Close)
	var err error
	f.path = filepath.Join(t.TempDir(), "state.db")
	f.h, err = assembly.OpenWithFiles(f.path, "u", "d", roots)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.h.Close() })
	c := &v1.SubmitGoalCommand{Identity: header("goal").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "create a record"}
	r, err := f.h.Sessions.SubmitGoal(f.ctx, f.caller, c)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.h.Sessions.ProcessPending(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	q, err := f.h.Durable.QueryReceipt(f.ctx, f.caller, r.Identity)
	if err != nil {
		t.Fatal(err)
	}
	f.task = q.Receipt.TaskRef
	f.parameters = q.Receipt.InputRef
	r, err = f.h.Tasks.AcceptRequirements(f.ctx, f.caller, &v1.AcceptRequirementsCommand{Header: header("requirements"), TaskRef: f.task, InputVersion: 1, Conditions: []*v1.Requirement{{ConditionId: "created", DescriptionRef: f.parameters, Necessary: true, VerificationRule: "TARGET_RECORD", RuleVersion: 1}}, Source: "TRUSTED_TEMPLATE"})
	accepted(t, r, err)
	ceiling := int64(30)
	r, err = f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("capability"), Capability: &v1.Capability{Action: "CREATE", Resource: server.URL, ExecutorEndpointId: "local-api", AdapterRef: &v1.Ref{Name: &v1.GlobalName{UserId: "u", AuthorityDomainId: "adapter", ObjectKind: "adapter", LocalId: "simulator-idempotent"}, Revision: 1, SchemaId: "lerna.v1.Adapter"}, UseRight: "INVOKE", ProcessingPurpose: "CURRENT_TASK", Unit: "USD_MICRO", FeeCeiling: &ceiling, RateBasisRef: f.parameters, MaxSends: 1}})
	accepted(t, r, err)
	f.capability = r.ResultRef
	for _, c := range []*v1.ConfigureBudgetCommand{{Header: header("user-budget"), Unit: "USD_MICRO", Limit: userLimit}, {Header: header("task-budget"), TaskId: f.task.Name, Unit: "USD_MICRO", Limit: taskLimit}} {
		r, err = f.h.Budget.Configure(f.ctx, f.caller, c)
		accepted(t, r, err)
	}
	r, err = f.h.Grants.Configure(f.ctx, f.caller, &v1.ConfigureGrantCommand{Header: header("grant"), Grant: &v1.Grant{Subject: f.task.Name, Permissions: []*v1.PermissionClause{{Action: "CREATE", Resource: server.URL, UseRight: "INVOKE", ProcessingPurpose: "CURRENT_TASK", ExecutorEndpointId: "local-api"}}, ValidFromUnixMs: time.Now().Add(-time.Minute).UnixMilli(), ValidUntilUnixMs: time.Now().Add(time.Hour).UnixMilli(), UseMode: "CONTINUOUS", UsePoolId: "root", ConfirmationRequired: confirmation}})
	accepted(t, r, err)
	f.grant = r.ResultRef
	return f
}
func (f *fixture) propose(t *testing.T, change func(*v1.Proposal)) *v1.Ref {
	t.Helper()
	snap, err := f.h.Tasks.RequestProposal(f.ctx, f.caller, &v1.RequestProposalCommand{Header: header("request" + f.suffix), TaskId: f.task.Name})
	if err != nil {
		t.Fatal(err)
	}
	p, err := (&scripted.Reasoner{Step: &v1.ActionStep{StepId: "one", CapabilityRef: f.capability, ParametersRef: f.parameters}}).Propose(f.ctx, snap)
	if err != nil {
		t.Fatal(err)
	}
	if change != nil {
		change(p)
	}
	r, err := f.h.Tasks.ReceiveProposal(f.ctx, f.caller, &v1.ReceiveProposalCommand{Header: header("proposal" + f.suffix), Proposal: p})
	accepted(t, r, err)
	return r.ResultRef
}
func accepted(t *testing.T, r *v1.CommandReceipt, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	if r.Decision != v1.Decision_DECISION_ACCEPTED {
		t.Fatalf("rejected: %v", r)
	}
}

// 规则：G4、G7、G10、准入-7、准入-8、准入-9、准入-10
func TestLateRejectionRollsBackParticipantsAndPreservesReceipt(t *testing.T) {
	for _, tc := range []struct {
		name         string
		confirmation bool
		change       func(*v1.Proposal)
		code         string
	}{
		{name: "confirmation", confirmation: true, code: "CONFIRMATION_INVALID"},
		{name: "last-content", change: func(p *v1.Proposal) {
			p.Step.ContentRefs = []*v1.Ref{{Name: &v1.GlobalName{UserId: "u", AuthorityDomainId: "d/content", ObjectKind: "content", LocalId: "missing"}, Revision: 1, SchemaId: "lerna.v1.Content"}}
		}, code: "CONTENT_UNUSABLE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, 100, 80, tc.confirmation)
			proposal := f.propose(t, tc.change)
			c := &v1.AdmitCommand{Header: header("admit"), TaskId: f.task.Name, ProposalRef: proposal, GrantRef: f.grant}
			r, e := f.h.Tasks.Admit(f.ctx, f.caller, c)
			if e != nil || r.Error.GetCode() != tc.code {
				t.Fatalf("rejection: %v %v", r, e)
			}
			p, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
			if e != nil || p.ProposalConsumed || len(p.AdmissionRefs) != 0 {
				t.Fatalf("partial planning: %v %v", p, e)
			}
			for _, task := range []*v1.GlobalName{nil, f.task.Name} {
				b, e := f.h.Budget.QueryBudget(f.ctx, f.caller, task)
				if e != nil || b.Reserved != 0 || b.Settled != 0 {
					t.Fatalf("partial budget: %v %v", b, e)
				}
			}
			uses, e := f.h.Grants.QueryUses(f.ctx, f.caller, f.task.Name)
			if e != nil || len(uses) != 0 {
				t.Fatalf("partial uses: %v %v", uses, e)
			}
			reservations, e := f.h.Budget.QueryReservations(f.ctx, f.caller, f.task.Name)
			if e != nil || len(reservations) != 0 {
				t.Fatalf("partial reservations: %v %v", reservations, e)
			}
			jobs, e := f.h.Durable.Pending(f.ctx, f.caller)
			if e != nil || len(jobs) != 0 {
				t.Fatalf("partial jobs: %v %v", jobs, e)
			}
			q, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, c.Header.Identity)
			if e != nil || !proto.Equal(q.Receipt, r) {
				t.Fatalf("original rejection: %v %v", q, e)
			}
			again, e := f.h.Tasks.Admit(f.ctx, f.caller, c)
			if e != nil || !proto.Equal(again, r) || f.calls.Load() != 0 {
				t.Fatalf("replay: %v %v calls=%d", again, e, f.calls.Load())
			}
		})
	}
}

// 规则：R7、G3、G6、R1
func TestHandoffHasSeparateOriginalReceiptAndLedgerJob(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	proposal := f.propose(t, nil)
	r, e := f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("admit"), TaskId: f.task.Name, ProposalRef: proposal, GrantRef: f.grant})
	accepted(t, r, e)
	a, e := f.h.Tasks.QueryAdmission(f.ctx, f.caller, r.ResultRef)
	if e != nil {
		t.Fatal(e)
	}
	outbox, e := f.h.Tasks.QueryHandoff(f.ctx, f.caller, a.Ref)
	if e != nil || outbox.State != "PENDING" || outbox.RecipientReceipt != nil {
		t.Fatalf("source intent: %v %v", outbox, e)
	}
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || op != nil {
		t.Fatalf("P1 leaked P2: %v %v", op, e)
	}
	if e = f.h.Tasks.ProcessHandoffs(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	outbox, e = f.h.Tasks.QueryHandoff(f.ctx, f.caller, a.Ref)
	if e != nil || outbox.State != "ACKNOWLEDGED" || outbox.RecipientReceipt == nil {
		t.Fatalf("source receipt: %v %v", outbox, e)
	}
	receiver := &v1.Caller{UserId: "u", IssuerId: "tasks-handoff"}
	q, e := f.h.Ledger.QueryReceipt(f.ctx, receiver, a.HandoffIdentity)
	if e != nil || !proto.Equal(q.Receipt, outbox.RecipientReceipt) {
		t.Fatalf("recipient original: %v %v", q, e)
	}
	op, e = f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || op.Lifecycle != "ACCEPTED" || op.Dispatch != "OPEN" || len(op.AttemptRefs) != 0 || op.Effect.Outcome != "NOT_APPLIED" || op.Effect.LateEffect != "RULED_OUT" {
		t.Fatalf("operation: %v %v", op, e)
	}
	jobs, e := f.h.Ledger.QueryJobs(f.ctx, f.caller, a.OperationId)
	if e != nil || len(jobs) != 1 || jobs[0].Ref.Name.AuthorityDomainId != "d/ledger" {
		t.Fatalf("ledger job: %v %v", jobs, e)
	}
	if e = f.h.Tasks.ProcessHandoffs(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	q2, e := f.h.Ledger.QueryReceipt(f.ctx, receiver, a.HandoffIdentity)
	if e != nil || !proto.Equal(q, q2) || f.calls.Load() != 0 {
		t.Fatalf("duplicate delivery: %v %v calls=%d", q2, e, f.calls.Load())
	}
}

// 规则：G2、G4、G6、G8、G9、准入-1、准入-2、准入-3、准入-6、准入-8
func TestAdmissionRejectsStaleOrUnsupportedBasis(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		change     func(*v1.Proposal)
	}{
		{"requirements", "STALE_REQUIREMENT", func(p *v1.Proposal) { p.RequirementsVersion++ }},
		{"input", "STALE_INPUT", func(p *v1.Proposal) { p.InputVersion++ }},
		{"control", "STALE_GENERATION", func(p *v1.Proposal) { p.ControlGeneration++ }},
		{"planning", "STALE_PROPOSAL", func(p *v1.Proposal) { p.PlanningGeneration++ }},
		{"dependency", "UNSATISFIED_DEPENDENCY", func(p *v1.Proposal) { p.Step.Dependencies = []*v1.Ref{p.ContextSnapshotRef} }},
		{"concrete-parameters", "UNSATISFIED_DEPENDENCY", func(p *v1.Proposal) { p.Step.ParametersRef = nil }},
		{"memory", "UNSUPPORTED_FEATURE", func(p *v1.Proposal) {
			p.Step.MemoryDependencies = []*v1.MemoryDependency{{MemoryRef: p.ContextSnapshotRef, Usage: "CURRENT"}}
		}},
		{"reasoner-preparation", "UNSUPPORTED_FEATURE", func(p *v1.Proposal) { p.Step.WorkCategory = "PREPARATION" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, 100, 80, false)
			ref := f.propose(t, tc.change)
			r, e := f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("admit"), TaskId: f.task.Name, ProposalRef: ref, GrantRef: f.grant})
			if e != nil || r.Error.GetCode() != tc.code {
				t.Fatalf("got %v %v", r, e)
			}
			assertNoAdmission(t, f)
		})
	}
}

// 规则：G10、准入-8
func TestUnknownNegativeOrUnprovenCeilingIsNotZero(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*v1.Capability)
	}{
		{"unknown", func(c *v1.Capability) { c.FeeCeiling = nil }},
		{"negative", func(c *v1.Capability) { v := int64(-1); c.FeeCeiling = &v }},
		{"missing-rate", func(c *v1.Capability) { c.RateBasisRef = nil }},
		{"zero-without-proof", func(c *v1.Capability) { v := int64(0); c.FeeCeiling = &v }},
		{"unit", func(c *v1.Capability) { c.Unit = "TOKENS" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, 100, 80, false)
			cap, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
			if e != nil {
				t.Fatal(e)
			}
			cap.Ref = nil
			cap.ApprovedBy = nil
			tc.change(cap)
			r, e := f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("cap2"), Capability: cap})
			accepted(t, r, e)
			f.capability = r.ResultRef
			ref := f.propose(t, nil)
			r, e = f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("admit"), TaskId: f.task.Name, ProposalRef: ref, GrantRef: f.grant})
			if e != nil || r.Error.GetCode() != "COST_CEILING_UNKNOWN" {
				t.Fatalf("got %v %v", r, e)
			}
			assertNoAdmission(t, f)
		})
	}
}

// 规则：G7、准入-7
func TestGrantMatchesWholeClauseAndCurrentValidity(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		change     func(*v1.Grant)
	}{
		{"action", "GRANT_SCOPE_MISMATCH", func(g *v1.Grant) { g.Permissions[0].Action = "READ" }},
		{"resource", "GRANT_SCOPE_MISMATCH", func(g *v1.Grant) { g.Permissions[0].Resource = "another" }},
		{"endpoint", "GRANT_SCOPE_MISMATCH", func(g *v1.Grant) { g.Permissions[0].ExecutorEndpointId = "another" }},
		{"expired", "GRANT_INVALID", func(g *v1.Grant) { g.ValidFromUnixMs = 1; g.ValidUntilUnixMs = 2 }},
		{"not-yet-valid", "GRANT_INVALID", func(g *v1.Grant) {
			g.ValidFromUnixMs = time.Now().Add(time.Hour).UnixMilli()
			g.ValidUntilUnixMs = time.Now().Add(2 * time.Hour).UnixMilli()
		}},
		{"clauses-not-cartesian", "GRANT_SCOPE_MISMATCH", func(g *v1.Grant) {
			p := proto.Clone(g.Permissions[0]).(*v1.PermissionClause)
			g.Permissions[0].Action = "READ"
			p.Resource = "another"
			g.Permissions = append(g.Permissions, p)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, 100, 80, false)
			g, e := f.h.Grants.QueryGrant(f.ctx, f.caller, f.grant)
			if e != nil {
				t.Fatal(e)
			}
			g.Ref = nil
			g.Issuer = nil
			g.Status = ""
			tc.change(g)
			r, e := f.h.Grants.Configure(f.ctx, f.caller, &v1.ConfigureGrantCommand{Header: header("g2"), Grant: g})
			accepted(t, r, e)
			f.grant = r.ResultRef
			ref := f.propose(t, nil)
			r, e = f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("admit"), TaskId: f.task.Name, ProposalRef: ref, GrantRef: f.grant})
			if e != nil || r.Error.GetCode() != tc.code {
				t.Fatalf("got %v %v", r, e)
			}
			assertNoAdmission(t, f)
		})
	}
}
func assertNoAdmission(t *testing.T, f *fixture) {
	t.Helper()
	p, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
	if e != nil || p.ProposalConsumed || len(p.AdmissionRefs) > 0 {
		t.Fatalf("partial admission %v %v", p, e)
	}
	uses, e := f.h.Grants.QueryUses(f.ctx, f.caller, f.task.Name)
	if e != nil || len(uses) > 0 {
		t.Fatalf("partial uses %v %v", uses, e)
	}
	rs, e := f.h.Budget.QueryReservations(f.ctx, f.caller, f.task.Name)
	if e != nil || len(rs) > 0 {
		t.Fatalf("partial reserves %v %v", rs, e)
	}
	for _, id := range []*v1.GlobalName{nil, f.task.Name} {
		b, e := f.h.Budget.QueryBudget(f.ctx, f.caller, id)
		if e != nil || b.Reserved != 0 {
			t.Fatalf("partial budget %v %v", b, e)
		}
	}
	if f.calls.Load() != 0 {
		t.Fatal("target called")
	}
}

// 规则：G10、准入-8
func TestTwoTasksCompeteForLastUserCapacityAcrossConnections(t *testing.T) {
	f := newFixture(t, 30, 80, false)
	second := &fixture{ctx: f.ctx, caller: f.caller, capability: f.capability, path: f.path, suffix: "2", h: f.h}
	r, e := f.h.Sessions.SubmitGoal(f.ctx, f.caller, &v1.SubmitGoalCommand{Identity: header("goal2").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "another record"})
	if e != nil {
		t.Fatal(e)
	}
	if e = f.h.Sessions.ProcessPending(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	q, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, r.Identity)
	if e != nil {
		t.Fatal(e)
	}
	second.task = q.Receipt.TaskRef
	second.parameters = q.Receipt.InputRef
	r, e = f.h.Tasks.AcceptRequirements(f.ctx, f.caller, &v1.AcceptRequirementsCommand{Header: header("requirements2"), TaskRef: second.task, InputVersion: 1, Source: "TRUSTED_TEMPLATE", Conditions: []*v1.Requirement{{ConditionId: "created", DescriptionRef: second.parameters, Necessary: true, VerificationRule: "TARGET_RECORD", RuleVersion: 1}}})
	accepted(t, r, e)
	r, e = f.h.Budget.Configure(f.ctx, f.caller, &v1.ConfigureBudgetCommand{Header: header("budget2"), TaskId: second.task.Name, Unit: "USD_MICRO", Limit: 80})
	accepted(t, r, e)
	grant, e := f.h.Grants.QueryGrant(f.ctx, f.caller, f.grant)
	if e != nil {
		t.Fatal(e)
	}
	grant.Ref = nil
	grant.Issuer = nil
	grant.Status = ""
	grant.Subject = second.task.Name
	r, e = f.h.Grants.Configure(f.ctx, f.caller, &v1.ConfigureGrantCommand{Header: header("grant2"), Grant: grant})
	accepted(t, r, e)
	second.grant = r.ResultRef
	proposal1, proposal2 := f.propose(t, nil), second.propose(t, nil)
	second.h, e = assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	defer second.h.Close()
	start := make(chan struct{})
	results := make(chan *v1.CommandReceipt, 2)
	failures := make(chan error, 2)
	for _, request := range []struct {
		f   *fixture
		ref *v1.Ref
	}{{f, proposal1}, {second, proposal2}} {
		go func(f *fixture, ref *v1.Ref) {
			<-start
			r, e := f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("admit" + f.suffix), TaskId: f.task.Name, ProposalRef: ref, GrantRef: f.grant})
			results <- r
			failures <- e
		}(request.f, request.ref)
	}
	close(start)
	count := 0
	for range 2 {
		r, e := <-results, <-failures
		if e != nil {
			t.Fatal(e)
		}
		if r.Decision == v1.Decision_DECISION_ACCEPTED {
			count++
		} else if r.Error.GetCode() != "BUDGET_EXCEEDED" {
			t.Fatalf("unexpected rejection: %v", r)
		}
	}
	if count != 1 {
		t.Fatalf("accepted %d competing actions", count)
	}
	user, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil || user.Reserved != 30 || user.Settled != 0 {
		t.Fatalf("user budget: %v %v", user, e)
	}
	var total int64
	uses := 0
	reservations := 0
	for _, id := range []*v1.GlobalName{f.task.Name, second.task.Name} {
		b, e := f.h.Budget.QueryBudget(f.ctx, f.caller, id)
		if e != nil {
			t.Fatal(e)
		}
		total += b.Reserved
		u, e := f.h.Grants.QueryUses(f.ctx, f.caller, id)
		if e != nil {
			t.Fatal(e)
		}
		uses += len(u)
		rs, e := f.h.Budget.QueryReservations(f.ctx, f.caller, id)
		if e != nil {
			t.Fatal(e)
		}
		reservations += len(rs)
	}
	if total != 30 || uses != 1 || reservations != 1 || f.calls.Load() != 0 {
		t.Fatalf("total=%d uses=%d reservations=%d calls=%d", total, uses, reservations, f.calls.Load())
	}
}

// 规则：G4、G6、准入-1
func TestHistoricalProposalAndRequirementsRemainDereferenceable(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	proposal := f.propose(t, nil)
	before, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	r, e := f.h.Tasks.AcceptRequirements(f.ctx, f.caller, &v1.AcceptRequirementsCommand{Header: header("requirements-new"), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, InputVersion: 1, Source: "TRUSTED_TEMPLATE", Conditions: []*v1.Requirement{{ConditionId: "created-new", DescriptionRef: f.parameters, Necessary: true, VerificationRule: "TARGET_RECORD", RuleVersion: 1}}})
	accepted(t, r, e)
	_, e = f.h.Tasks.RequestProposal(f.ctx, f.caller, &v1.RequestProposalCommand{Header: header("request-new"), TaskId: f.task.Name})
	if e != nil {
		t.Fatal(e)
	}
	oldProposal, e := f.h.Tasks.QueryProposal(f.ctx, f.caller, proposal)
	if e != nil || !proto.Equal(oldProposal, before.Proposal) {
		t.Fatalf("historical proposal lost %v %v", oldProposal, e)
	}
	oldRequirements, e := f.h.Tasks.QueryRequirements(f.ctx, f.caller, before.Requirements.Ref)
	if e != nil || !proto.Equal(oldRequirements, before.Requirements) {
		t.Fatalf("historical requirements lost %v %v", oldRequirements, e)
	}
	oldSnapshot, e := f.h.Tasks.QuerySnapshot(f.ctx, f.caller, before.Snapshot.Ref)
	if e != nil || !proto.Equal(oldSnapshot, before.Snapshot) {
		t.Fatalf("historical snapshot lost %v %v", oldSnapshot, e)
	}
}

// 规则：G3、G6、准入-1
func TestConsumedProposalCannotCreateSecondOperationAndConflictKeepsOriginal(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	ref := f.propose(t, nil)
	c := &v1.AdmitCommand{Header: header("admit"), TaskId: f.task.Name, ProposalRef: ref, GrantRef: f.grant}
	original, e := f.h.Tasks.Admit(f.ctx, f.caller, c)
	accepted(t, original, e)
	other := proto.Clone(c).(*v1.AdmitCommand)
	other.Header = header("second-admit")
	r, e := f.h.Tasks.Admit(f.ctx, f.caller, other)
	if e != nil || r.Error.GetCode() != "STALE_PROPOSAL" {
		t.Fatalf("consumed proposal admitted: %v %v", r, e)
	}
	changed := proto.Clone(c).(*v1.AdmitCommand)
	changed.ConfirmationRef = f.parameters
	r, e = f.h.Tasks.Admit(f.ctx, f.caller, changed)
	if e == nil || e.Error() != "IDEMPOTENCY_CONFLICT" || r != nil {
		t.Fatalf("changed semantics: %v %v", r, e)
	}
	q, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, c.Header.Identity)
	if e != nil || !proto.Equal(q.Receipt, original) {
		t.Fatalf("original overwritten: %v %v", q, e)
	}
	uses, e := f.h.Grants.QueryUses(f.ctx, f.caller, f.task.Name)
	if e != nil || len(uses) != 1 {
		t.Fatalf("duplicate uses: %v %v", uses, e)
	}
	rs, e := f.h.Budget.QueryReservations(f.ctx, f.caller, f.task.Name)
	if e != nil || len(rs) != 1 {
		t.Fatalf("duplicate reservations: %v %v", rs, e)
	}
}

// 规则：G7、G12、准入-1
func TestUnknownAuthorityFieldsAndWrongCallerCannotConfigureOrAdmit(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	ref := f.propose(t, nil)
	c := &v1.AdmitCommand{Header: header("admit"), TaskId: f.task.Name, ProposalRef: ref, GrantRef: f.grant}
	c.GrantRef = proto.Clone(c.GrantRef).(*v1.Ref)
	c.GrantRef.ProtoReflect().SetUnknown([]byte{0x98, 0x06, 0x01})
	r, e := f.h.Tasks.Admit(f.ctx, f.caller, c)
	if e == nil || e.Error() != "UNSUPPORTED_FEATURE" || r != nil {
		t.Fatalf("unknown authority passed: %v %v", r, e)
	}
	cap, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
	if e != nil {
		t.Fatal(e)
	}
	cap.Ref = nil
	cap.ApprovedBy = nil
	header := header("untrusted-config")
	header.Identity.IssuerId = "reasoner"
	r, e = f.h.Tasks.ConfigureCapability(f.ctx, &v1.Caller{UserId: "u", IssuerId: "reasoner"}, &v1.ConfigureCapabilityCommand{Header: header, Capability: cap})
	if e != nil || r.Error.GetCode() != "PERMISSION_DENIED" {
		t.Fatalf("reasoner configured authority %v %v", r, e)
	}
	assertNoAdmission(t, f)
}

// 规则：R7、G3、G11
func TestGenericJobCompletionCannotEraseUnacknowledgedHandoff(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	ref := f.propose(t, nil)
	r, e := f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("admit"), TaskId: f.task.Name, ProposalRef: ref, GrantRef: f.grant})
	accepted(t, r, e)
	claim, e := f.h.Durable.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: header("claim").Identity, ContractVersion: 1, Action: "CLAIM", Module: "tasks", AllowedTypes: []string{"DELIVER_HANDOFF"}, Limit: 1, LeaseMs: 30000, ProcessInstance: "worker"})
	accepted(t, claim, e)
	if len(claim.Jobs) != 1 {
		t.Fatal(claim)
	}
	j := claim.Jobs[0]
	r, e = f.h.Durable.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: header("finish-without-receipt").Identity, ContractVersion: 1, Action: "PROGRESS", JobRef: j.Ref, ClaimEpoch: j.ClaimEpoch, ProcessInstance: j.ProcessInstance, NextState: "COMPLETED"})
	if e != nil || r.Error.GetCode() != "UNSUPPORTED_FEATURE" {
		t.Fatalf("orphaned responsibility %v %v", r, e)
	}
	current, e := f.h.Durable.QueryJob(f.ctx, f.caller, j.Ref.Name)
	if e != nil || current.State != "CLAIMED" {
		t.Fatalf("job changed: %v %v", current, e)
	}
	if e = f.h.Tasks.ProcessHandoffClaim(f.ctx, j); e != nil {
		t.Fatal(e)
	}
}

// 规则：G3、R7
func TestRecipientRejectionIsAnImmutableOriginalDecision(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	ref := f.propose(t, nil)
	r, e := f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("admit"), TaskId: f.task.Name, ProposalRef: ref, GrantRef: f.grant})
	accepted(t, r, e)
	a, e := f.h.Tasks.QueryAdmission(f.ctx, f.caller, r.ResultRef)
	if e != nil {
		t.Fatal(e)
	}
	bad := proto.Clone(a).(*v1.Admission)
	bad.ExecutorEndpointId = "wrong-endpoint"
	caller := &v1.Caller{UserId: "u", IssuerId: "tasks-handoff"}
	c := &v1.AcceptOperationCommand{Header: &v1.CommandHeader{Identity: a.HandoffIdentity, ContractVersion: 1, FingerprintVersion: 1, SchemaId: "lerna.v1.AdmissionCommands"}, Admission: bad}
	rejection, e := f.h.Ledger.Accept(f.ctx, caller, c)
	if e != nil || rejection.Error.GetCode() != "INVALID_HANDOFF" {
		t.Fatalf("missing rejected receipt: %v %v", rejection, e)
	}
	q, e := f.h.Ledger.QueryReceipt(f.ctx, caller, a.HandoffIdentity)
	if e != nil || !proto.Equal(q.Receipt, rejection) {
		t.Fatalf("query rejection: %v %v", q, e)
	}
	c.Admission = a
	r, e = f.h.Ledger.Accept(f.ctx, caller, c)
	if e == nil || e.Error() != "IDEMPOTENCY_CONFLICT" || r != nil {
		t.Fatalf("rejection replaced: %v %v", r, e)
	}
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || op != nil {
		t.Fatalf("rejected handoff left operation: %v %v", op, e)
	}
}

// 规则：G3、G11
func TestGenericCloseCannotAbandonSubmittedGoalResponsibility(t *testing.T) {
	h, e := assembly.Open(filepath.Join(t.TempDir(), "goal.db"), "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	ctx := context.Background()
	caller := &v1.Caller{UserId: "u", IssuerId: "host"}
	submitted, e := h.Sessions.SubmitGoal(ctx, caller, &v1.SubmitGoalCommand{Identity: header("goal").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "create record"})
	if e != nil {
		t.Fatal(e)
	}
	r, e := h.Durable.ExecuteJob(ctx, caller, &v1.JobCommand{Identity: header("abandon").Identity, ContractVersion: 1, Action: "CONTROL", Module: "sessions", JobRef: submitted.JobRef, NextState: "CLOSED"})
	if e != nil || r.Error.GetCode() != "UNSUPPORTED_FEATURE" {
		t.Fatalf("abandoned submitted responsibility: %v %v", r, e)
	}
	job, e := h.Durable.QueryJob(ctx, caller, submitted.JobRef.Name)
	if e != nil || job.State != "READY" {
		t.Fatalf("job changed: %v %v", job, e)
	}
	if e = h.Sessions.ProcessPending(ctx, caller); e != nil {
		t.Fatal(e)
	}
	q, e := h.Durable.QueryReceipt(ctx, caller, submitted.Identity)
	if e != nil || q.Receipt.Decision != v1.Decision_DECISION_ACCEPTED {
		t.Fatalf("responsibility not recoverable: %v %v", q, e)
	}
}

// 规则：G4、G6、准入-1、准入-2
func TestReasonerCannotRebindOldSnapshotToNewRequirements(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	snap, e := f.h.Tasks.RequestProposal(f.ctx, f.caller, &v1.RequestProposalCommand{Header: header("request"), TaskId: f.task.Name})
	if e != nil {
		t.Fatal(e)
	}
	current, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	r, e := f.h.Tasks.AcceptRequirements(f.ctx, f.caller, &v1.AcceptRequirementsCommand{Header: header("new-requirements"), TaskRef: &v1.Ref{Name: current.TaskId, Revision: current.Revision, SchemaId: "lerna.v1.Task"}, InputVersion: 1, Source: "TRUSTED_TEMPLATE", Conditions: []*v1.Requirement{{ConditionId: "different", DescriptionRef: f.parameters, Necessary: true, VerificationRule: "USER_EVALUATION", RuleVersion: 1}}})
	accepted(t, r, e)
	current, e = f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	p, e := (&scripted.Reasoner{Step: &v1.ActionStep{StepId: "one", ParametersRef: f.parameters, CapabilityRef: f.capability}}).Propose(f.ctx, snap)
	if e != nil {
		t.Fatal(e)
	}
	p.RequirementsVersion = current.RequirementsVersion
	p.ControlGeneration = current.ControlGeneration
	r, e = f.h.Tasks.ReceiveProposal(f.ctx, f.caller, &v1.ReceiveProposalCommand{Header: header("proposal"), Proposal: p})
	accepted(t, r, e)
	r, e = f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("admit"), TaskId: f.task.Name, ProposalRef: r.ResultRef, GrantRef: f.grant})
	if e != nil || r.Error.GetCode() != "STALE_REQUIREMENT" {
		t.Fatalf("reasoner rebound old snapshot: %v %v", r, e)
	}
	assertNoAdmission(t, f)
}
