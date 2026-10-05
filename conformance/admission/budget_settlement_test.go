package admission_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"testing"

	"github.com/ruipengliu/lerna/adapters/interaction"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// 规则：G3、G10、准入-8
func TestBudgetLimitRevisionPreservesReservationAndHistoricalBasis(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	a, _ := prepareStart(t, f)
	before, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil {
		t.Fatal(e)
	}
	r, e := f.h.Budget.AdjustLimit(f.ctx, f.caller, &v1.AdjustBudgetLimitCommand{Header: header("lower-limit"), ExpectedRef: before.Ref, Limit: 20, Reason: "user limit"})
	accepted(t, r, e)
	current, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil || current.Reserved != 30 || current.Limit != 20 || current.Deficit != 10 || current.Available != 0 {
		t.Fatalf("current %v %v", current, e)
	}
	original, e := f.h.Budget.QueryBudgetVersion(f.ctx, f.caller, a.BudgetBasis.BudgetChainRefs[0])
	if e != nil || original.Limit != 100 || original.Reserved != 30 {
		t.Fatalf("history %v %v", original, e)
	}
}

// 规则：G3、G10、R7
func TestKnownUsageSettlesOnceAndRetainsOriginalReport(t *testing.T) {
	f := newFixtureWithTarget(t, 100, 80, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"billing": map[string]any{"rule": "reference-billing-v1", "namespace": "lerna-reference", "account": "reference-account", "native_instance": "charge:" + r.Header.Get("Lerna-Send-Id"), "component": "call", "send_id": r.Header.Get("Lerna-Send-Id"), "external_key": r.Header.Get("Idempotency-Key"), "source_version": 1, "unit": "USD_MICRO", "amount": 25, "final": true, "price_version": "reference-price-v1"}})
	}))
	a, c := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
	accepted(t, r, e)
	b, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil || b.Settled != 25 || b.Reserved != 0 || b.Available != 75 {
		t.Fatalf("settlement %v %v", b, e)
	}
	x, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	reports, e := f.h.Ledger.QueryReports(f.ctx, f.caller, x.Send.ObservationRef)
	if e != nil {
		t.Fatal(e)
	}
	u, e := f.h.Budget.QueryUsage(f.ctx, f.caller, reports.UsageReceipt.ResultRef)
	if e != nil || u.Settlement != "PENDING" {
		t.Fatalf("immutable report %v %v", u, e)
	}
	replay := proto.Clone(reports.Usage).(*v1.AcceptUsageCommand)
	replay.Header.Identity.CommandId += ":duplicate"
	r, e = f.h.Budget.AcceptUsage(f.ctx, &v1.Caller{UserId: "u", IssuerId: "ledger-report"}, replay)
	accepted(t, r, e)
	b, e = f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil || b.Settled != 25 || b.Reserved != 0 || f.calls.Load() != 1 {
		t.Fatalf("duplicate %v %v calls=%d", b, e, f.calls.Load())
	}
}

// 规则：G1、G10、G11、R7
func TestChargedReceiptLostRetainsHoldThenAcceptsLateOverCeilingBill(t *testing.T) {
	target := simulator.NewBillingTarget(120)
	target.DropReceipt(true)
	f := newFixtureWithTarget(t, 100, 80, false, target)
	a, c := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
	accepted(t, r, e)
	b, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil || b.Settled != 0 || b.Reserved != 30 {
		t.Fatalf("unknown fee %v %v", b, e)
	}
	bills := target.Bills()
	if len(bills) != 1 || bills[0].Amount != 120 {
		t.Fatalf("provider charges %v", bills)
	}
	body, _ := json.Marshal(map[string]any{"billing": bills[0]})
	evidence, e := f.h.Content.Stage(f.ctx, f.caller, &v1.SubmitGoalCommand{Identity: header("late-statement").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: string(body)})
	if e != nil {
		t.Fatal(e)
	}
	x, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	r, e = f.h.Budget.ImportBill(f.ctx, f.caller, &v1.ImportBillCommand{Header: header("import-bill"), SendRef: x.Send.Ref, EvidenceRef: evidence})
	accepted(t, r, e)
	b, e = f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil || b.Settled != 120 || b.Reserved != 0 || b.Deficit != 20 || b.Available != 0 {
		t.Fatalf("late bill %v %v", b, e)
	}
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || op.Effect.Outcome != "UNKNOWN" || len(target.Bills()) != 1 {
		t.Fatalf("fee changed effect or resent: %v %v", op, e)
	}
	task, e := f.h.Budget.QueryBudget(f.ctx, f.caller, f.task.Name)
	if e != nil || task.Settled != 120 || task.Deficit != 40 {
		t.Fatalf("nested budget %v %v", task, e)
	}
}

// 规则：G3、G10、G11、开始-3
func TestNoSendClosureReleasesUnusedHoldWithoutUsageOrAuthorityRefund(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	a, c := prepareStart(t, f)
	r, e := f.h.Tasks.StartExecution(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
	accepted(t, r, e)
	r, e = f.h.Grants.Revoke(f.ctx, f.caller, &v1.RevokeGrantCommand{Header: header("release-revoke"), GrantId: f.grant.Name})
	accepted(t, r, e)
	if e = f.h.Grants.ProcessRevocations(f.ctx); e != nil {
		t.Fatal(e)
	}
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	proof := op.ClosureEvidenceRefs[0]
	cmd := &v1.ReleaseReservationCommand{Header: header("release-unused"), ReservationRef: a.BudgetBasis.ReservationRef, ClosureRef: proof}
	r, e = f.h.Budget.ReleaseUnused(f.ctx, f.caller, cmd)
	accepted(t, r, e)
	cmd.Header = header("release-again")
	r, e = f.h.Budget.ReleaseUnused(f.ctx, f.caller, cmd)
	accepted(t, r, e)
	b, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil || b.Reserved != 0 || b.Settled != 0 || b.Available != 100 {
		t.Fatalf("release %v %v", b, e)
	}
	reservations, e := f.h.Budget.QueryReservations(f.ctx, f.caller, a.TaskId)
	if e != nil || len(reservations) != 1 || reservations[0].ConsumedSends != 1 || reservations[0].Status != "UNUSED_CLOSED" {
		t.Fatalf("reservation %v %v", reservations, e)
	}
	uses, e := f.h.Grants.QueryUses(f.ctx, f.caller, a.TaskId)
	if e != nil || len(uses) != 1 || f.calls.Load() != 0 {
		t.Fatalf("refunded authority %v %v", uses, e)
	}
	source, e := f.h.Budget.QueryBillingSource(f.ctx, f.caller, op.Execution.Send.Ref)
	if e != nil || source.Status != "UNUSED_CLOSED" || source.EntryRef != nil {
		t.Fatalf("fabricated usage %v %v", source, e)
	}
}

// 规则：G10、准入-8
func TestDistinctPhysicalSendsWithIdenticalParametersAreBothCharged(t *testing.T) {
	target := simulator.NewBillingTarget(25)
	f := newFixtureWithTarget(t, 100, 80, false, target)
	_, c := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
	accepted(t, r, e)
	f.suffix = "second"
	_, c = prepareStart(t, f)
	r, e = f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
	accepted(t, r, e)
	bills := target.Bills()
	b, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil || b.Settled != 50 || b.Reserved != 0 || len(bills) != 2 || bills[0].NativeInstance == bills[1].NativeInstance {
		t.Fatalf("separate charges %v %v %v", b, bills, e)
	}
	requests, _ := target.Target.Snapshot()
	if len(requests) != 2 || requests[0].BodyDigest != requests[1].BodyDigest {
		t.Fatalf("target %v", requests)
	}
}

// 规则：G3、G10、G12、准入-8
func TestConflictingProviderAliasRetainsBothResponsibilitiesAndBlocksNewAdmission(t *testing.T) {
	target := simulator.NewBillingTarget(25)
	target.SetNativeInstance("same-provider-line")
	f := newFixtureWithTarget(t, 150, 150, false, target)
	_, c := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
	accepted(t, r, e)
	f.suffix = "alias-conflict"
	a, c := prepareStart(t, f)
	r, e = f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
	accepted(t, r, e)
	b, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil || b.Settled != 25 || b.Reserved != 30 || !b.BillingBlocked {
		t.Fatalf("conflict balances %v %v", b, e)
	}
	x, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	source, e := f.h.Budget.QueryBillingSource(f.ctx, f.caller, x.Send.Ref)
	if e != nil || source.Status != "CONFLICT" || source.ConflictRef == nil {
		t.Fatalf("missing conflict %v %v", source, e)
	}
	f.suffix = "after-conflict"
	p := f.propose(t, nil)
	r, e = f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("blocked-by-alias"), TaskId: f.task.Name, ProposalRef: p, GrantRef: f.grant})
	if e != nil || r.GetError().GetCode() != "BILLING_CONFLICT" {
		t.Fatalf("new admission %v %v", r, e)
	}
	if len(target.Bills()) != 2 {
		t.Fatal("new physical send after conflict")
	}
}

// 规则：G3、G10、G12
func TestBillingHistoricalFactsAndExactReferencesRemainImmutable(t *testing.T) {
	target := simulator.NewBillingTarget(25)
	f := newFixtureWithTarget(t, 100, 80, false, target)
	a, c := prepareStart(t, f)
	r, e := f.h.Tasks.StartExecution(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
	accepted(t, r, e)
	x, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	initial, e := f.h.Budget.QueryBillingSource(f.ctx, f.caller, x.Send.Ref)
	if e != nil {
		t.Fatal(e)
	}
	r, e = f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
	accepted(t, r, e)
	if initial.Amount != nil {
		t.Fatal("unknown fee represented as known zero")
	}
	old, e := f.h.Budget.QueryBillingSourceVersion(f.ctx, f.caller, initial.Ref)
	if e != nil || !proto.Equal(old, initial) || old.Status != "PENDING" {
		t.Fatalf("source history %v %v", old, e)
	}
	fake := proto.Clone(x.Send.Ref).(*v1.Ref)
	fake.Revision = 999
	if _, e = f.h.Budget.QueryBillingSource(f.ctx, f.caller, fake); e == nil {
		t.Fatal("nonexistent send revision accepted")
	}
	current, e := f.h.Budget.QueryBillingSource(f.ctx, f.caller, x.Send.Ref)
	if e != nil {
		t.Fatal(e)
	}
	if current.Identity.GetNamespace() != "lerna-reference" || current.Identity.GetNativeInstance() != target.Bills()[0].NativeInstance {
		t.Fatalf("missing stable native identity %v", current)
	}
	entry, e := f.h.Budget.QueryBillingEntry(f.ctx, f.caller, current.EntryRef)
	if e != nil || entry.Amount != 25 || entry.Released != 5 || entry.MeasurementRule != "reference-billing-v1" {
		t.Fatalf("entry %v %v", entry, e)
	}
}

// 规则：G3、G10、R7
func TestRestartConsumesDurableUnusedClosureOnce(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	_, c := prepareStart(t, f)
	r, e := f.h.Tasks.StartExecution(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
	accepted(t, r, e)
	r, e = f.h.Grants.Revoke(f.ctx, f.caller, &v1.RevokeGrantCommand{Header: header("recover-revoke"), GrantId: f.grant.Name})
	accepted(t, r, e)
	if e = f.h.Grants.ProcessRevocations(f.ctx); e != nil {
		t.Fatal(e)
	}
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	f.h, e = assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	b, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil || b.Reserved != 0 || b.Available != 100 {
		t.Fatalf("recovery retained proved-unused hold %v %v", b, e)
	}
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	f.h, e = assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	b, e = f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil || b.Reserved != 0 || b.Settled != 0 || f.calls.Load() != 0 {
		t.Fatalf("duplicate release %v %v", b, e)
	}
}

// 规则：G10、G12
func TestCLIShowsBudgetDeficitAndLocalUserCanChangeLimit(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	_, _ = prepareStart(t, f)
	cli := interaction.CLI{Budget: f.h.Budget, Caller: &v1.Caller{UserId: "u", IssuerId: "local-cli"}, Domain: "d"}
	before, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil {
		t.Fatal(e)
	}
	h := header("cli-limit")
	h.Identity.IssuerId = "local-cli"
	cmd := &v1.AdjustBudgetLimitCommand{Header: h, ExpectedRef: before.Ref, Limit: 20, Reason: "local user limit"}
	data, e := protojson.Marshal(cmd)
	if e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	if e = cli.Run(f.ctx, []string{"budget-limit", string(data)}, &out); e != nil {
		t.Fatal(e)
	}
	receipt := new(v1.CommandReceipt)
	if e = protojson.Unmarshal(out.Bytes(), receipt); e != nil {
		t.Fatal(e)
	}
	accepted(t, receipt, nil)
	out.Reset()
	if e = cli.Run(f.ctx, []string{"budget"}, &out); e != nil {
		t.Fatal(e)
	}
	b := new(v1.Budget)
	if e = protojson.Unmarshal(out.Bytes(), b); e != nil {
		t.Fatal(e)
	}
	if b.Limit != 20 || b.Reserved != 30 || b.Deficit != 10 || b.Available != 0 {
		t.Fatalf("CLI %s", out.String())
	}
}

// 规则：G1、G10、G11
func TestCancellationAndPostSendSealCannotReleaseUnknownFee(t *testing.T) {
	target := simulator.NewBillingTarget(25)
	target.DropReceipt(true)
	f := newFixtureWithTarget(t, 100, 80, false, target)
	a, c := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
	accepted(t, r, e)
	goal, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("goal").Identity)
	if e != nil {
		t.Fatal(e)
	}
	task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, a.TaskId)
	if e != nil {
		t.Fatal(e)
	}
	r, e = f.h.Sessions.SubmitInput(f.ctx, f.caller, &v1.SubmitInputCommand{Header: header("cancel-fee"), SessionId: goal.Receipt.SessionRef.Name, TaskId: a.TaskId, InputKind: "CONTROL", Control: "CANCEL", ExpectedControlGeneration: task.ControlGeneration})
	accepted(t, r, e)
	r, e = f.h.Grants.Revoke(f.ctx, f.caller, &v1.RevokeGrantCommand{Header: header("seal-after-send"), GrantId: f.grant.Name})
	accepted(t, r, e)
	if e = f.h.Grants.ProcessRevocations(f.ctx); e != nil {
		t.Fatal(e)
	}
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	r, e = f.h.Budget.ReleaseUnused(f.ctx, f.caller, &v1.ReleaseReservationCommand{Header: header("invalid-release"), ReservationRef: a.BudgetBasis.ReservationRef, ClosureRef: op.ClosureEvidenceRefs[0]})
	if e != nil || r.GetError().GetCode() != "NO_SEND_UNPROVEN" {
		t.Fatalf("post-send release %v %v", r, e)
	}
	if e = f.h.Budget.ProcessClosures(f.ctx); e != nil {
		t.Fatal(e)
	}
	b, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil || b.Reserved != 30 || b.Settled != 0 || len(target.Bills()) != 1 {
		t.Fatalf("lost unknown fee %v %v", b, e)
	}
	bill := target.Bills()[0]
	ref := stageBill(t, f, bill, "cancelled-statement")
	r, e = f.h.Budget.ImportBill(f.ctx, f.caller, &v1.ImportBillCommand{Header: header("cancelled-bill"), SendRef: op.Execution.Send.Ref, EvidenceRef: ref})
	accepted(t, r, e)
	b, e = f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil || b.Settled != 25 || b.Reserved != 0 {
		t.Fatalf("late cancelled charge %v %v", b, e)
	}
}
func stageBill(t *testing.T, f *fixture, bill simulator.Bill, id string) *v1.Ref {
	t.Helper()
	body, e := json.Marshal(map[string]any{"billing": bill})
	if e != nil {
		t.Fatal(e)
	}
	ref, e := f.h.Content.Stage(f.ctx, f.caller, &v1.SubmitGoalCommand{Identity: header(id).Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: string(body)})
	if e != nil {
		t.Fatal(e)
	}
	return ref
}

// 规则：G10、准入-8
func TestActualOverCeilingBlocksNextAdmissionEvenAfterExplicitLimitRaise(t *testing.T) {
	target := simulator.NewBillingTarget(120)
	f := newFixtureWithTarget(t, 100, 100, false, target)
	_, c := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
	accepted(t, r, e)
	f.suffix = "exceeded"
	p := f.propose(t, nil)
	r, e = f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("exceeded-admission"), TaskId: f.task.Name, ProposalRef: p, GrantRef: f.grant})
	if e != nil || r.GetError().GetCode() != "BUDGET_EXCEEDED" {
		t.Fatalf("deficit admission %v %v", r, e)
	}
	for i, id := range []*v1.GlobalName{nil, f.task.Name} {
		b, e := f.h.Budget.QueryBudget(f.ctx, f.caller, id)
		if e != nil {
			t.Fatal(e)
		}
		r, e = f.h.Budget.AdjustLimit(f.ctx, f.caller, &v1.AdjustBudgetLimitCommand{Header: header(fmt.Sprintf("raise-%d", i)), ExpectedRef: b.Ref, Limit: 150, Reason: "user grants capacity"})
		accepted(t, r, e)
	}
	r, e = f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("after-limit-raise"), TaskId: f.task.Name, ProposalRef: p, GrantRef: f.grant})
	if e != nil || r.GetError().GetCode() != "COST_CEILING_VIOLATED" {
		t.Fatalf("limit change concealed broken ceiling %v %v", r, e)
	}
	b, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil || b.Settled != 120 || b.Reserved != 0 || b.Deficit != 0 || !b.CeilingViolation || len(target.Bills()) != 1 {
		t.Fatalf("limit raise %v %v", b, e)
	}
}

// 规则：G3、G10、R7、完成-6
func TestCompletionSealReleasesOnlyItsOriginalUnusedReservation(t *testing.T) {
	for _, started := range []bool{false, true} {
		t.Run(fmt.Sprint(started), func(t *testing.T) {
			f := newFixture(t, 100, 80, false)
			var a *v1.Admission
			if started {
				var c *v1.StartExecutionCommand
				a, c = prepareStart(t, f)
				r, e := f.h.Tasks.StartExecution(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
				accepted(t, r, e)
			} else {
				p := f.propose(t, nil)
				r, e := f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("absent-admit"), TaskId: f.task.Name, ProposalRef: p, GrantRef: f.grant})
				accepted(t, r, e)
				a, e = f.h.Tasks.QueryAdmission(f.ctx, f.caller, r.ResultRef)
				if e != nil {
					t.Fatal(e)
				}
			}
			p := completeProposal(t, f, nil)
			r, e := f.h.Tasks.BeginCompletion(f.ctx, f.caller, &v1.BeginCompletionCommand{Header: header("release-completion"), TaskId: f.task.Name, ProposalRef: p})
			accepted(t, r, e)
			round, e := f.h.Tasks.QueryVerification(f.ctx, f.caller, r.ResultRef)
			if e != nil {
				t.Fatal(e)
			}
			if e = f.h.Tasks.ProcessCompletionClosures(f.ctx, f.caller); e != nil {
				t.Fatal(e)
			}
			intent, e := f.h.Tasks.QueryCompletionIntent(f.ctx, f.caller, round.ClosureIntentRefs[0])
			if e != nil {
				t.Fatal(e)
			}
			r, e = f.h.Budget.ReleaseUnused(f.ctx, f.caller, &v1.ReleaseReservationCommand{Header: header("release-completion-proof"), ReservationRef: a.BudgetBasis.ReservationRef, ClosureRef: intent.RecipientReceipt.ResultRef})
			accepted(t, r, e)
			b, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
			if e != nil || b.Reserved != 0 || b.Settled != 0 || f.calls.Load() != 0 {
				t.Fatalf("completion release %v %v", b, e)
			}
			if e = f.h.Tasks.ProcessHandoffs(f.ctx, f.caller); e != nil {
				t.Fatal(e)
			}
			op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if e != nil || op.Dispatch != "SEALED" {
				t.Fatalf("released late P2 escaped %v %v", op, e)
			}
		})
	}
}

// 规则：G2、G10、G11、完成-7
func TestClosedTaskAcceptsLateBillWithoutChangingFixedResult(t *testing.T) {
	target := simulator.NewBillingTarget(120)
	target.WithholdBill(true)
	f := newFixtureWithTarget(t, 100, 80, false, target)
	scopeRequirement(t, f)
	a, c := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
	accepted(t, r, e)
	p := completeProposal(t, f, []*v1.CompletionEvidence{{ConditionId: "created", OperationId: a.OperationId}})
	r, e = f.h.Tasks.BeginCompletion(f.ctx, f.caller, &v1.BeginCompletionCommand{Header: header("closed-fee-begin"), TaskId: f.task.Name, ProposalRef: p})
	accepted(t, r, e)
	if e = f.h.Tasks.ProcessCompletions(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	result, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	if e != nil || result == nil || result.Outcome != "SUCCEEDED" || result.UsageSnapshot.Reserved != 30 {
		t.Fatalf("fee-pending result %v %v", result, e)
	}
	if e = f.h.Budget.ProcessClosures(f.ctx); e != nil {
		t.Fatal(e)
	}
	b, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil || b.Reserved != 30 {
		t.Fatalf("closed task lost hold %v %v", b, e)
	}
	x, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	ref := stageBill(t, f, target.Bills()[0], "closed-statement")
	r, e = f.h.Budget.ImportBill(f.ctx, f.caller, &v1.ImportBillCommand{Header: header("closed-import"), SendRef: x.Send.Ref, EvidenceRef: ref})
	accepted(t, r, e)
	again, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	if e != nil || !proto.Equal(result, again) {
		t.Fatalf("late bill rewrote result %v %v", again, e)
	}
	b, e = f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil || b.Settled != 120 || b.Reserved != 0 || b.Deficit != 20 || len(target.Bills()) != 1 {
		t.Fatalf("late closed cost %v %v", b, e)
	}
}

// 规则：G3、G10、G12
func TestImportedBillCannotRebindOrCorrectSettledSource(t *testing.T) {
	target := simulator.NewBillingTarget(25)
	f := newFixtureWithTarget(t, 100, 80, false, target)
	a, c := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
	accepted(t, r, e)
	x, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	original := target.Bills()[0]
	originalRef := stageBill(t, f, original, "duplicate-statement")
	r, e = f.h.Budget.ImportBill(f.ctx, f.caller, &v1.ImportBillCommand{Header: header("duplicate-import"), SendRef: x.Send.Ref, EvidenceRef: originalRef})
	accepted(t, r, e)
	first := proto.Clone(r).(*v1.CommandReceipt)
	r, e = f.h.Budget.ImportBill(f.ctx, f.caller, &v1.ImportBillCommand{Header: header("duplicate-import"), SendRef: x.Send.Ref, EvidenceRef: originalRef})
	if e != nil || !proto.Equal(first, r) {
		t.Fatalf("receipt changed %v %v", r, e)
	}
	for _, tc := range []struct {
		name, code string
		change     func(*simulator.Bill)
	}{
		{"cross-send", "BILLING_BINDING_MISMATCH", func(b *simulator.Bill) { b.SendID = "another-send" }},
		{"cross-key", "BILLING_BINDING_MISMATCH", func(b *simulator.Bill) { b.ExternalKey = "another-attempt" }},
		{"negative", "INVALID_BILL", func(b *simulator.Bill) { b.Amount = -1 }},
		{"different-account", "INVALID_BILL", func(b *simulator.Bill) { b.Account = "different-account" }},
		{"correction", "UNSUPPORTED_BILLING_CORRECTION", func(b *simulator.Bill) { b.Amount = 20; b.SourceVersion = 2 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bill := original
			tc.change(&bill)
			ref := stageBill(t, f, bill, tc.name+"-statement")
			r, e := f.h.Budget.ImportBill(f.ctx, f.caller, &v1.ImportBillCommand{Header: header(tc.name), SendRef: x.Send.Ref, EvidenceRef: ref})
			if e != nil || r.GetError().GetCode() != tc.code {
				t.Fatalf("invalid bill %v %v", r, e)
			}
			b, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
			if e != nil || b.Settled != 25 || b.Reserved != 0 {
				t.Fatalf("partial balances %v %v", b, e)
			}
		})
	}
	for _, operation := range []string{"REFUND", "CREDIT", "CORRECT"} {
		r, e = f.h.Budget.ImportBill(f.ctx, f.caller, &v1.ImportBillCommand{Header: header(operation), SendRef: x.Send.Ref, EvidenceRef: originalRef, Operation: operation})
		if e != nil || r.GetError().GetCode() != "UNSUPPORTED_BILLING_OPERATION" {
			t.Fatalf("M2 operation %v %v", r, e)
		}
	}
}

// 规则：G1、G10
func TestMalformedOrNonfinalBillingEvidenceKeepsUnknownHold(t *testing.T) {
	for _, kind := range []string{"nonfinal", "missing-amount", "fraction", "duplicate-amount", "wrong-account"} {
		t.Run(kind, func(t *testing.T) {
			target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				bill := map[string]any{"rule": "reference-billing-v1", "namespace": "lerna-reference", "account": "reference-account", "native_instance": "native-charge", "component": "call", "send_id": r.Header.Get("Lerna-Send-Id"), "external_key": r.Header.Get("Idempotency-Key"), "source_version": 1, "unit": "USD_MICRO", "amount": 25, "final": true, "price_version": "reference-price-v1"}
				switch kind {
				case "nonfinal":
					bill["final"] = false
				case "missing-amount":
					delete(bill, "amount")
				case "fraction":
					bill["amount"] = 25.5
				case "wrong-account":
					bill["account"] = "unrelated"
				}
				data, _ := json.Marshal(map[string]any{"billing": bill})
				if kind == "duplicate-amount" {
					data = bytes.Replace(data, []byte(`"amount":25`), []byte(`"amount":25,"amount":0`), 1)
				}
				_, _ = w.Write(data)
			})
			f := newFixtureWithTarget(t, 100, 80, false, target)
			_, c := prepareStart(t, f)
			r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
			accepted(t, r, e)
			b, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
			if e != nil || b.Settled != 0 || b.Reserved != 30 || f.calls.Load() != 1 {
				t.Fatalf("invented known fee %v %v", b, e)
			}
		})
	}
}

// 规则：G3、G10
func TestBillUsesExactIntegerAndAcceptsMaximumRepresentableAmount(t *testing.T) {
	target := simulator.NewBillingTarget(math.MaxInt64)
	f := newFixtureWithTarget(t, 100, 80, false, target)
	_, c := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
	accepted(t, r, e)
	b, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil || b.Settled != 9223372036854775807 || b.Deficit != 9223372036854775707 || b.Reserved != 0 {
		t.Fatalf("integer precision %v %v", b, e)
	}
}

// 规则：G3、G10、准入-8
func TestContradictorySameVersionBillIsRecordedAsConflictWithoutChargingDifference(t *testing.T) {
	target := simulator.NewBillingTarget(25)
	f := newFixtureWithTarget(t, 100, 80, false, target)
	a, c := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
	accepted(t, r, e)
	x, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	bill := target.Bills()[0]
	bill.Amount = 26
	ref := stageBill(t, f, bill, "contradictory-version")
	r, e = f.h.Budget.ImportBill(f.ctx, f.caller, &v1.ImportBillCommand{Header: header("contradictory-version"), SendRef: x.Send.Ref, EvidenceRef: ref})
	accepted(t, r, e)
	conflict, e := f.h.Budget.QueryBillingConflict(f.ctx, f.caller, r.ResultRef)
	if e != nil || conflict == nil || conflict.Reason != "BILLING_VERSION_CONFLICT" {
		t.Fatalf("version contradiction %v %v", conflict, e)
	}
	b, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil || b.Settled != 25 || b.Reserved != 0 || !b.BillingBlocked {
		t.Fatalf("contradictory amount accepted %v %v", b, e)
	}
}

// 规则：G10、准入-8
func TestBrokenFeeCeilingBlocksNewAdmissionEvenWithRemainingCapacity(t *testing.T) {
	target := simulator.NewBillingTarget(40)
	f := newFixtureWithTarget(t, 100, 80, false, target)
	_, c := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
	accepted(t, r, e)
	f.suffix = "broken-ceiling"
	p := f.propose(t, nil)
	r, e = f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("broken-ceiling-next"), TaskId: f.task.Name, ProposalRef: p, GrantRef: f.grant})
	if e != nil || r.GetError().GetCode() != "COST_CEILING_VIOLATED" {
		t.Fatalf("broken provider ceiling admitted again %v %v", r, e)
	}
	b, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil || b.Settled != 40 || b.Reserved != 0 || b.Available != 60 || !b.CeilingViolation {
		t.Fatalf("missing ceiling failure %v %v", b, e)
	}
}
