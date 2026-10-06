package admission_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"
	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// 规则：G3、G4、G11、R7、开始-1、开始-3
func TestCredentialAuthorityUnavailableKeepsOriginalCommandRecoverable(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	r, err := f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("admit"), TaskId: f.task.Name, ProposalRef: f.propose(t, nil), GrantRef: f.grant})
	accepted(t, r, err)
	a, err := f.h.Tasks.QueryAdmission(f.ctx, f.caller, r.ResultRef)
	if err != nil {
		t.Fatal(err)
	}
	binding := &v1.ExitCredentialBinding{UserId: "u", TaskId: a.TaskId, OperationId: a.OperationId, AttemptId: &v1.GlobalName{UserId: "u", AuthorityDomainId: "d/ledger", ObjectKind: "attempt", LocalId: "original-attempt"}, SendSeq: 1, AdmissionRef: a.Ref, GrantUseRef: a.GrantUseRef, SubjectId: a.TaskId, CallerIssuerId: "egress", Audience: "egress", ExecutorEndpointId: a.ExecutorEndpointId, ExecutorInstance: "original-process", DescriptorDigest: "original-descriptor", UseRight: "INVOKE", ProcessingPurpose: "CURRENT_TASK", RequirementsVersion: a.RequirementsVersion, InputVersion: a.InputVersion, ControlGeneration: a.ControlGeneration, BudgetReservationRef: a.BudgetBasis.ReservationRef}
	c := &v1.IssueExitCredentialCommand{Header: header("credential-after-authority-recovery"), AdmissionRef: a.Ref, Binding: binding, ExpiresAtUnixMs: time.Now().Add(time.Minute).UnixMilli()}
	before := queryCommandRecoveryFacts(t, f)
	// 公开装配接口使原准入负责方不可用；没有虚构准入或替代权限。
	f.h.Grants.WithAdmissions(nil)
	r, err = f.h.Grants.IssueCredential(f.ctx, f.caller, c)
	assertUnresolvedOriginalCommand(t, f, f.caller, c.Header.Identity, r, err, "AUTHORITY_UNREACHABLE")
	assertSameCommandRecoveryFacts(t, before, queryCommandRecoveryFacts(t, f))

	f.h.Grants.WithAdmissions(f.h.Tasks)
	r, err = f.h.Grants.IssueCredential(f.ctx, f.caller, c)
	accepted(t, r, err)
	credential, err := f.h.Grants.QueryCredential(f.ctx, f.caller, r.ResultRef)
	if err != nil || credential == nil || credential.State != "ISSUED" || !proto.Equal(credential.Binding, binding) {
		t.Fatalf("original credential not issued after recovery: %v %v", credential, err)
	}
	after := queryCommandRecoveryFacts(t, f)
	issued := 0
	for _, source := range after.sources {
		if source.Command.Event.EventType == "CREDENTIAL_ISSUED" && proto.Equal(source.Command.Event.SourceRecordRef, credential.Ref) {
			issued++
		}
	}
	if issued != 1 || len(after.uses) != len(before.uses) || len(after.reservations) != len(before.reservations) || f.calls.Load() != 0 {
		t.Fatalf("issuance changed original authority/fees or made I/O: issued=%d uses=%d reservations=%d calls=%d", issued, len(after.uses), len(after.reservations), f.calls.Load())
	}
	if !proto.Equal(before.planning, after.planning) {
		t.Fatal("credential issuance changed original planning")
	}
	assertRecoveryMessagesEqual(t, before.uses, after.uses)
	assertRecoveryMessagesEqual(t, before.reservations, after.reservations)
	assertRecoveryMessagesEqual(t, before.budgets, after.budgets)
	q, err := f.h.Durable.QueryReceipt(f.ctx, f.caller, c.Header.Identity)
	if err != nil || q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED || !proto.Equal(q.Receipt, r) || !proto.Equal(r.Identity, c.Header.Identity) {
		t.Fatalf("original credential receipt not durable: %v %v", q, err)
	}
	again, err := f.h.Grants.IssueCredential(f.ctx, f.caller, c)
	if err != nil || !proto.Equal(r, again) {
		t.Fatalf("same original identity changed accepted credential: %v %v", again, err)
	}
	assertSameCommandRecoveryFacts(t, after, queryCommandRecoveryFacts(t, f))
	if err = f.h.Close(); err != nil {
		t.Fatal(err)
	}
	f.h, err = assembly.Open(f.path, "u", "d")
	if err != nil {
		t.Fatal(err)
	}
	again, err = f.h.Grants.IssueCredential(f.ctx, f.caller, c)
	if err != nil || !proto.Equal(r, again) || f.calls.Load() != 0 {
		t.Fatalf("restart lost original credential receipt or sent I/O: %v %v calls=%d", again, err, f.calls.Load())
	}
	restored, err := f.h.Grants.QueryCredential(f.ctx, f.caller, r.ResultRef)
	if err != nil || !proto.Equal(credential, restored) {
		t.Fatalf("restart changed original credential: %v %v", restored, err)
	}
}

func assertUnresolvedOriginalCommand(t *testing.T, f *fixture, caller *v1.Caller, identity *v1.CommandIdentity, r *v1.CommandReceipt, err error, code string) {
	t.Helper()
	var failure *command.Failure
	if r != nil || !errors.As(err, &failure) || failure.Detail.Code != code || failure.Detail.Category != v1.ErrorCategory_ERROR_CATEGORY_TRANSIENT || failure.Detail.CommandAcceptance != v1.CommandAcceptance_COMMAND_ACCEPTANCE_UNKNOWN || failure.Detail.RecoveryAction != "QUERY_OR_RETRY_ORIGINAL" {
		t.Fatalf("unavailable responsible dependency became a durable decision or lost recovery semantics: receipt=%v error=%v", r, err)
	}
	q, err := f.h.Durable.QueryReceipt(f.ctx, caller, identity)
	if err != nil || q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND || q.Receipt != nil {
		t.Fatalf("temporary failure persisted original decision: %v %v", q, err)
	}
}

type commandRecoveryFacts struct {
	planning     *v1.PlanningState
	uses         []*v1.GrantUse
	reservations []*v1.Reservation
	budgets      []*v1.Budget
	sources      []*v1.TraceSourceRecord
}

func queryCommandRecoveryFacts(t *testing.T, f *fixture) commandRecoveryFacts {
	t.Helper()
	var out commandRecoveryFacts
	var err error
	out.planning, err = f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
	if err != nil {
		t.Fatal(err)
	}
	out.uses, err = f.h.Grants.QueryUses(f.ctx, f.caller, f.task.Name)
	if err != nil {
		t.Fatal(err)
	}
	out.reservations, err = f.h.Budget.QueryReservations(f.ctx, f.caller, f.task.Name)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range []*v1.GlobalName{nil, f.task.Name} {
		b, err := f.h.Budget.QueryBudget(f.ctx, f.caller, task)
		if err != nil {
			t.Fatal(err)
		}
		out.budgets = append(out.budgets, b)
	}
	out.sources, err = f.h.Trace.QuerySources(f.ctx, f.caller)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func assertSameCommandRecoveryFacts(t *testing.T, want, got commandRecoveryFacts) {
	t.Helper()
	if !proto.Equal(want.planning, got.planning) {
		t.Fatal("temporary failure or replay changed original planning")
	}
	assertRecoveryMessagesEqual(t, want.uses, got.uses)
	assertRecoveryMessagesEqual(t, want.reservations, got.reservations)
	assertRecoveryMessagesEqual(t, want.budgets, got.budgets)
	assertRecoveryMessagesEqual(t, want.sources, got.sources)
}

func assertRecoveryMessagesEqual[T proto.Message](t *testing.T, want, got []T) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("temporary failure or replay changed original fact count: before=%d after=%d", len(want), len(got))
	}
	for i := range want {
		if !proto.Equal(want[i], got[i]) {
			t.Fatalf("temporary failure or replay changed original fact %d: before=%v after=%v", i, want[i], got[i])
		}
	}
}

// 规则：G1、G3、G4、G5、G11、R7、准入-7、准入-8
func TestClosureDependencyUnavailableKeepsOriginalAdmissionRecoverable(t *testing.T) {
	target := simulator.New("queryable")
	target.SetBehavior("drop-after-apply")
	f := newFixtureWithTarget(t, 200, 200, false, target)
	capability, grant := configureReconciliation(t, f)
	a, start := prepareStart(t, f)
	r, err := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	accepted(t, r, err)
	original, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if err != nil || original.Effect.Outcome != "UNKNOWN" {
		t.Fatalf("missing real original unknown: %v %v", original, err)
	}
	target.SetBehavior("")
	r, err = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, reconciliationCommand(f, a, capability, grant))
	accepted(t, r, err)
	f.h.Tasks.WithClosureSource(nil)
	// 公开恢复先保存真实原核对意图；负责方不可用时没有准入命令决定。
	err = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller)
	var failure *command.Failure
	if !errors.As(err, &failure) || failure.Detail.Code != "DEPENDENCY_UNAVAILABLE" {
		t.Fatalf("unavailable closure source was bypassed: %v", err)
	}
	plan, err := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if err != nil || plan.ActiveQueryRef == nil || len(plan.QueryRefs) != 1 {
		t.Fatalf("original query intent not saved: %v %v", plan, err)
	}
	query, err := f.h.Ledger.QueryReconciliationQuery(f.ctx, f.caller, plan.ActiveQueryRef)
	if err != nil || query.Work == nil || query.AdmissionReceipt != nil {
		t.Fatalf("query was admitted without its responsible source: %v %v", query, err)
	}
	actor := &v1.Caller{UserId: "u", IssuerId: "ledger-reconciliation"}
	h := header(query.Work.AdmissionIdentity.CommandId)
	h.Identity = proto.Clone(query.Work.AdmissionIdentity).(*v1.CommandIdentity)
	c := &v1.AdmitClosureCommand{Header: h, WorkRef: query.Work.Ref}
	before := queryCommandRecoveryFacts(t, f)
	r, err = f.h.Tasks.AdmitClosure(f.ctx, actor, c)
	assertUnresolvedOriginalCommand(t, f, actor, c.Header.Identity, r, err, "DEPENDENCY_UNAVAILABLE")
	assertSameCommandRecoveryFacts(t, before, queryCommandRecoveryFacts(t, f))
	requests, effects := target.Snapshot()
	if len(requests) != 1 || requests[0].Method != "POST" || len(effects) != 1 {
		t.Fatalf("unavailable admission created a query or repeated original effect: requests=%v effects=%v", requests, effects)
	}

	f.h.Tasks.WithClosureSource(f.h.Ledger)
	r, err = f.h.Tasks.AdmitClosure(f.ctx, actor, c)
	accepted(t, r, err)
	admission, err := f.h.Tasks.QueryAdmission(f.ctx, f.caller, r.ResultRef)
	if err != nil || admission.WorkCategory != "CLOSURE" || !proto.Equal(admission.Origin, query.Work.Ref) || !proto.Equal(admission.QuerySubject, query.Work.QuerySubject) || proto.Equal(admission.OperationId, original.Ref.Name) {
		t.Fatalf("recovery changed original closure source or relabelled original action: %v %v", admission, err)
	}
	after := queryCommandRecoveryFacts(t, f)
	if len(after.planning.AdmissionRefs) != 2 || len(after.uses) != len(before.uses)+1 || len(after.reservations) != len(before.reservations)+1 {
		t.Fatalf("original closure was not admitted exactly once: planning=%v uses=%d reservations=%d", after.planning, len(after.uses), len(after.reservations))
	}
	for i := range before.budgets {
		if after.budgets[i].Reserved != before.budgets[i].Reserved+5 || after.budgets[i].Settled != before.budgets[i].Settled {
			t.Fatalf("query fee reservation changed original settlement: before=%v after=%v", before.budgets[i], after.budgets[i])
		}
	}
	assertRecoveryMessagesRetained(t, before.uses, after.uses)
	assertRecoveryMessagesRetained(t, before.reservations, after.reservations)
	receipt, err := f.h.Durable.QueryReceipt(f.ctx, actor, c.Header.Identity)
	if err != nil || receipt.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED || !proto.Equal(receipt.Receipt, r) || !proto.Equal(r.Identity, c.Header.Identity) {
		t.Fatalf("original closure receipt not durable: %v %v", receipt, err)
	}
	again, err := f.h.Tasks.AdmitClosure(f.ctx, actor, c)
	if err != nil || !proto.Equal(r, again) {
		t.Fatalf("same original closure identity changed receipt: %v %v", again, err)
	}
	assertSameCommandRecoveryFacts(t, after, queryCommandRecoveryFacts(t, f))
	current, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, original.Ref.Name)
	if err != nil || !proto.Equal(original, current) {
		t.Fatalf("admission recovery changed original unknown execution: %v %v", current, err)
	}
	work, err := f.h.Ledger.QueryClosureWork(f.ctx, f.caller, query.Work.Ref)
	if err != nil || !proto.Equal(query.Work, work) {
		t.Fatalf("admission recovery rewrote original closure work: %v %v", work, err)
	}
	requests, effects = target.Snapshot()
	if len(requests) != 1 || requests[0].Method != "POST" || len(effects) != 1 {
		t.Fatalf("admission or replay dispatched physical work: requests=%v effects=%v", requests, effects)
	}
}

// 规则：G3、G11、G12
func TestCanonicalCommandFailuresPreserveRecoveryAndAcceptanceSemantics(t *testing.T) {
	for _, tc := range []struct {
		code       string
		category   v1.ErrorCategory
		acceptance v1.CommandAcceptance
		recovery   string
	}{
		{"AUTHORITY_UNREACHABLE", v1.ErrorCategory_ERROR_CATEGORY_TRANSIENT, v1.CommandAcceptance_COMMAND_ACCEPTANCE_UNKNOWN, "QUERY_OR_RETRY_ORIGINAL"},
		{"DEPENDENCY_UNAVAILABLE", v1.ErrorCategory_ERROR_CATEGORY_TRANSIENT, v1.CommandAcceptance_COMMAND_ACCEPTANCE_UNKNOWN, "QUERY_OR_RETRY_ORIGINAL"},
		{"RATE_LIMITED", v1.ErrorCategory_ERROR_CATEGORY_TRANSIENT, v1.CommandAcceptance_COMMAND_ACCEPTANCE_UNKNOWN, "QUERY_OR_RETRY_ORIGINAL"},
		{"DEADLINE_EXCEEDED", v1.ErrorCategory_ERROR_CATEGORY_INDETERMINATE, v1.CommandAcceptance_COMMAND_ACCEPTANCE_UNKNOWN, "QUERY_OR_RETRY_ORIGINAL"},
		{"TRANSPORT_LOST", v1.ErrorCategory_ERROR_CATEGORY_INDETERMINATE, v1.CommandAcceptance_COMMAND_ACCEPTANCE_UNKNOWN, "QUERY_OR_RETRY_ORIGINAL"},
		{"INVALID_INPUT", v1.ErrorCategory_ERROR_CATEGORY_PERMANENT, v1.CommandAcceptance_COMMAND_ACCEPTANCE_NOT_SUBMITTED, "CORRECT_REQUEST"},
		{"BUDGET_EXCEEDED", v1.ErrorCategory_ERROR_CATEGORY_PERMANENT, v1.CommandAcceptance_COMMAND_ACCEPTANCE_NOT_SUBMITTED, "CORRECT_REQUEST"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			var failure *command.Failure
			err := command.Fail(tc.code)
			if !errors.As(err, &failure) || failure.Detail.Code != tc.code || failure.Detail.Category != tc.category || failure.Detail.CommandAcceptance != tc.acceptance || failure.Detail.RecoveryAction != tc.recovery {
				t.Fatalf("canonical error lost contractual recovery semantics: %v", err)
			}
		})
	}
}

func assertRecoveryMessagesRetained[T proto.Message](t *testing.T, original, current []T) {
	t.Helper()
	for _, want := range original {
		found := false
		for _, got := range current {
			if proto.Equal(want, got) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("recovered command rewrote an original responsibility: %v", want)
		}
	}
}
