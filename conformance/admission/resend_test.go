package admission_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/interaction"
	"github.com/ruipengliu/lerna/cmd/assembly"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/ruipengliu/lerna/contracts/command"
	"github.com/ruipengliu/lerna/infra/egressio"

	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G3、G10、G11、开始-5
func TestSafeResendKeepsAttemptAndReservesEachPhysicalSend(t *testing.T) {
	target := simulator.New("idempotent")
	target.SetBehavior("drop-after-apply")
	f := newFixtureWithTarget(t, 100, 80, false, target)
	f.h.Ledger.WithWork(declaredExecutionWork{ExecutionWork: f.h.LedgerWork})
	cap, err := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
	if err != nil {
		t.Fatal(err)
	}
	cap.Ref, cap.ApprovedBy, cap.MaxSends = nil, nil, 2
	r, err := f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("resend-capability"), Capability: cap})
	accepted(t, r, err)
	f.capability = r.ResultRef
	a, first := prepareStart(t, f)
	actor := &v1.Caller{UserId: "u", IssuerId: "egress"}
	r, err = f.h.Egress.Invoke(f.ctx, actor, first)
	accepted(t, r, err)
	before, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if err != nil || before.Effect.Outcome != "UNKNOWN" {
		t.Fatalf("original: %v %v", before, err)
	}
	target.SetBehavior("")
	claim, err := f.h.LedgerWork.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: ledgerHeader("resend-claim").Identity, ContractVersion: 1, Action: "CLAIM", AllowedTypes: []string{"EXECUTE_OPERATION"}, Limit: 1, LeaseMs: 30000, ProcessInstance: "new-worker"})
	accepted(t, claim, err)
	if len(claim.Jobs) != 1 {
		t.Fatalf("claim: %v", claim)
	}
	request := &v1.PrepareResendCommand{Header: ledgerHeader("resend"), OperationId: a.OperationId, PreviousSendRef: before.Execution.Send.Ref, Claim: claim.Jobs[0]}
	r, err = f.h.Ledger.PrepareResend(f.ctx, f.caller, request)
	accepted(t, r, err)
	decision := proto.Clone(r).(*v1.CommandReceipt)
	x, err := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if err != nil || x.Send.SendSeq != 2 || !proto.Equal(x.Attempt.Ref.Name, before.Execution.Attempt.Ref.Name) || !proto.Equal(x.CallDescriptor, before.Execution.CallDescriptor) {
		t.Fatalf("new send: %v %v", x, err)
	}
	second := resendStart(t, f, a, first, x, claim.Jobs[0])
	r, err = f.h.Egress.Invoke(f.ctx, actor, second)
	accepted(t, r, err)
	again, err := f.h.Ledger.PrepareResend(f.ctx, f.caller, request)
	if err != nil || !proto.Equal(again, decision) {
		t.Fatalf("replay: %v %v", again, err)
	}
	r, err = f.h.Egress.Invoke(f.ctx, actor, second)
	accepted(t, r, err)
	requests, effects := target.Snapshot()
	if len(requests) != 2 || len(effects) != 1 || requests[0].ExternalKey != requests[1].ExternalKey || requests[0].Send == requests[1].Send {
		t.Fatalf("requests=%v effects=%v", requests, effects)
	}
	op, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if err != nil || op.Effect.Outcome != "APPLIED" || op.Lifecycle != "SETTLED" || len(op.Execution.PreviousSends) != 1 {
		t.Fatalf("settled: %v %v", op, err)
	}
	reservations, err := f.h.Budget.QueryReservations(f.ctx, f.caller, a.TaskId)
	if err != nil || len(reservations) != 2 {
		t.Fatalf("reservations: %v %v", reservations, err)
	}
	budget, err := f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
	if err != nil || budget.Reserved != 60 {
		t.Fatalf("unknown fees held: %v %v", budget, err)
	}
	for _, send := range []*v1.PhysicalSend{before.Execution.Send, op.Execution.Send} {
		source, err := f.h.Budget.QueryBillingSource(f.ctx, f.caller, send.Ref)
		if err != nil || source == nil || source.Amount != nil {
			t.Fatalf("source %v %v", source, err)
		}
	}
	assertResendTrace(t, f, request, decision)
}

func resendStart(t *testing.T, f *fixture, a *v1.Admission, first *v1.StartExecutionCommand, x *v1.Execution, claim *v1.Job) *v1.StartExecutionCommand {
	t.Helper()
	b := proto.Clone(first.Binding).(*v1.ExitCredentialBinding)
	b.SendSeq = x.Send.SendSeq
	b.ExecutorInstance = x.Send.ProcessInstance
	r, err := f.h.Grants.IssueCredential(f.ctx, f.caller, &v1.IssueExitCredentialCommand{Header: header("resend-credential"), AdmissionRef: a.Ref, Binding: b, ExpiresAtUnixMs: time.Now().Add(time.Minute).UnixMilli()})
	accepted(t, r, err)
	h := header("start:" + x.Send.Ref.Name.LocalId)
	h.Identity.IssuerId = "egress"
	return &v1.StartExecutionCommand{Header: h, AdmissionRef: a.Ref, CredentialRef: r.ResultRef, Binding: b, CallDescriptor: x.CallDescriptor, Claim: claim}
}

// 规则：G1、G3、G10、G11、R7
func TestResendPreparationDoesNotRejectOriginalWorkersLateRaw(t *testing.T) {
	target := simulator.New("idempotent")
	entered, release := make(chan struct{}), make(chan struct{})
	target.SetWriteResponseGate(entered, release)
	f := newFixtureWithTarget(t, 100, 80, false, target)
	configureResendLimit(t, f, 2)
	a, first := prepareStartWithLease(t, f, 1500)
	done := make(chan error, 1)
	go func() {
		_, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, first)
		done <- e
	}()
	<-entered
	time.Sleep(time.Until(time.UnixMilli(first.Claim.LeaseUntilUnixMs)) + 20*time.Millisecond)
	before, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil {
		close(release)
		<-done
		t.Fatal(e)
	}
	claim, e := f.h.LedgerWork.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: ledgerHeader("takeover-resend").Identity, ContractVersion: 1, Action: "CLAIM", AllowedTypes: []string{"EXECUTE_OPERATION"}, Limit: 1, LeaseMs: 30000, ProcessInstance: "new-worker"})
	if e != nil || len(claim.GetJobs()) != 1 {
		close(release)
		<-done
		t.Fatalf("claim %v %v", claim, e)
	}
	r, e := f.h.Ledger.PrepareResend(f.ctx, f.caller, &v1.PrepareResendCommand{Header: ledgerHeader("late-resend"), OperationId: a.OperationId, PreviousSendRef: before.Send.Ref, Claim: claim.Jobs[0]})
	if e != nil || r.GetDecision() != v1.Decision_DECISION_ACCEPTED {
		close(release)
		<-done
		t.Fatalf("prepare %v %v", r, e)
	}
	current, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil {
		close(release)
		<-done
		t.Fatal(e)
	}
	close(release)
	if e = <-done; e != nil {
		t.Fatalf("original response rejected after preparing new send: %v", e)
	}
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || !proto.Equal(op.Execution.Send.Ref, current.Send.Ref) || op.Execution.Send.Phase != "REGISTERED" || op.Execution.PreviousSends[0].Phase != "OBSERVED" || op.Effect.Outcome != "APPLIED" {
		t.Fatalf("late provenance: %v %v", op, e)
	}
	old, e := f.h.Ledger.QuerySend(f.ctx, f.caller, before.Send.Ref)
	if e != nil || old.Phase != "DISPATCH_POSSIBLE" {
		t.Fatalf("immutable old send %v %v", old, e)
	}
	requests, effects := target.Snapshot()
	if len(requests) != 1 || len(effects) != 1 {
		t.Fatalf("unexpected sends/effects %v %v", requests, effects)
	}
	assertLateSendTrace(t, f, op, before.Send.Ref)
}

func configureResendLimit(t *testing.T, f *fixture, limit uint32) {
	t.Helper()
	cap, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
	if e != nil {
		t.Fatal(e)
	}
	cap.Ref, cap.ApprovedBy, cap.MaxSends = nil, nil, limit
	r, e := f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("resend-cap"), Capability: cap})
	accepted(t, r, e)
	f.capability = r.ResultRef
}

// 规则：G1、G10、G11、开始-5
func TestResendLateFirstBillSettlesOnlyOriginalSend(t *testing.T) {
	target := simulator.NewBillingTarget(17)
	target.DropReceipt(true)
	f := newFixtureWithTarget(t, 100, 80, false, target)
	configureResendLimit(t, f, 2)
	a, first := prepareStart(t, f)
	actor := &v1.Caller{UserId: "u", IssuerId: "egress"}
	r, e := f.h.Egress.Invoke(f.ctx, actor, first)
	accepted(t, r, e)
	original, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	second := prepareNextResend(t, f, a, first)
	target.DropReceipt(false)
	r, e = f.h.Egress.Invoke(f.ctx, actor, second)
	accepted(t, r, e)
	b, e := f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
	if e != nil || b.Reserved != 30 || b.Settled != 17 {
		t.Fatalf("first hold lost: %v %v", b, e)
	}
	bills := target.Bills()
	if len(bills) != 2 {
		t.Fatalf("bills %v", bills)
	}
	body, e := json.Marshal(map[string]any{"billing": bills[0]})
	if e != nil {
		t.Fatal(e)
	}
	evidence, e := f.h.Content.Stage(f.ctx, f.caller, &v1.SubmitGoalCommand{Identity: header("old-bill").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: string(body)})
	if e != nil {
		t.Fatal(e)
	}
	r, e = f.h.Budget.ImportBill(f.ctx, f.caller, &v1.ImportBillCommand{Header: header("late-old-bill"), SendRef: original.Send.Ref, EvidenceRef: evidence})
	accepted(t, r, e)
	b, e = f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
	if e != nil || b.Reserved != 0 || b.Settled != 34 {
		t.Fatalf("independent late settlement: %v %v", b, e)
	}
	requests, effects := target.Target.Snapshot()
	if len(requests) != 2 || len(effects) != 1 {
		t.Fatalf("requests/effects %v %v", requests, effects)
	}
}

func prepareNextResend(t *testing.T, f *fixture, a *v1.Admission, first *v1.StartExecutionCommand) *v1.StartExecutionCommand {
	t.Helper()
	before, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	claim, e := f.h.LedgerWork.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: ledgerHeader("next-claim").Identity, ContractVersion: 1, Action: "CLAIM", AllowedTypes: []string{"EXECUTE_OPERATION"}, Limit: 1, LeaseMs: 30000, ProcessInstance: "next-worker"})
	accepted(t, claim, e)
	if len(claim.Jobs) != 1 {
		t.Fatalf("claim %v", claim)
	}
	r, e := f.h.Ledger.PrepareResend(f.ctx, f.caller, &v1.PrepareResendCommand{Header: ledgerHeader("next-resend"), OperationId: a.OperationId, PreviousSendRef: before.Send.Ref, Claim: claim.Jobs[0]})
	accepted(t, r, e)
	x, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	return resendStart(t, f, a, first, x, claim.Jobs[0])
}

// 规则：G1、G4、G10、G11、开始-5
func TestClosingPreparedResendPreservesOriginalUnknownAndFee(t *testing.T) {
	target := simulator.New("idempotent")
	target.SetBehavior("drop-after-apply")
	f := newFixtureWithTarget(t, 100, 80, false, target)
	configureResendLimit(t, f, 2)
	a, first := prepareStart(t, f)
	actor := &v1.Caller{UserId: "u", IssuerId: "egress"}
	r, e := f.h.Egress.Invoke(f.ctx, actor, first)
	accepted(t, r, e)
	original, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	second := prepareNextResend(t, f, a, first)
	r, e = f.h.Tasks.StartExecution(f.ctx, actor, second)
	accepted(t, r, e)
	r, e = f.h.Grants.Revoke(f.ctx, f.caller, &v1.RevokeGrantCommand{Header: header("revoke-resend"), GrantId: f.grant.Name})
	accepted(t, r, e)
	if e = f.h.Grants.ProcessRevocations(f.ctx); e != nil {
		t.Fatal(e)
	}
	if e = f.h.Budget.ProcessClosures(f.ctx); e != nil {
		t.Fatal(e)
	}
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || op.Dispatch != "SEALED" || op.Execution.Send.Phase != "CLOSED" || op.Effect.Outcome != "UNKNOWN" || op.Effect.LateEffect != "MAY_OCCUR" {
		t.Fatalf("old unknown overwritten: %v %v", op, e)
	}
	b, e := f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
	if e != nil || b.Reserved != 30 || b.Settled != 0 {
		t.Fatalf("only unsent fee releases: %v %v", b, e)
	}
	old, e := f.h.Budget.QueryBillingSource(f.ctx, f.caller, original.Send.Ref)
	if e != nil || old.Status != "PENDING" || old.Amount != nil {
		t.Fatalf("old fee %v %v", old, e)
	}
	closed, e := f.h.Budget.QueryBillingSource(f.ctx, f.caller, op.Execution.Send.Ref)
	if e != nil || closed.Status != "UNUSED_CLOSED" || closed.Amount != nil {
		t.Fatalf("closed fee %v %v", closed, e)
	}
	_, e = f.h.Egress.Invoke(f.ctx, actor, second)
	requests, effects := target.Snapshot()
	if len(requests) != 1 || len(effects) != 1 {
		t.Fatalf("closed resend reached target: %v %v (%v)", requests, effects, e)
	}
}

// 规则：G1、G2、G10、G11、完成-6
func TestCompletionSealOfResendCannotClaimOriginalNeverSent(t *testing.T) {
	target := simulator.New("idempotent")
	target.SetBehavior("drop-after-apply")
	f := newFixtureWithTarget(t, 100, 80, false, target)
	configureResendLimit(t, f, 2)
	a, first := prepareStart(t, f)
	actor := &v1.Caller{UserId: "u", IssuerId: "egress"}
	r, e := f.h.Egress.Invoke(f.ctx, actor, first)
	accepted(t, r, e)
	second := prepareNextResend(t, f, a, first)
	r, e = f.h.Tasks.StartExecution(f.ctx, actor, second)
	accepted(t, r, e)
	p := completeProposal(t, f, nil)
	r, e = f.h.Tasks.BeginCompletion(f.ctx, f.caller, &v1.BeginCompletionCommand{Header: header("resend-completion"), TaskId: f.task.Name, ProposalRef: p})
	accepted(t, r, e)
	if e = f.h.Tasks.ProcessCompletionClosures(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || op.Effect.Outcome != "UNKNOWN" || op.Effect.LateEffect != "MAY_OCCUR" || op.Dispatch != "SEALED" || op.Execution.Send.Phase != "CLOSED" {
		t.Fatalf("false no-send closure: %v %v", op, e)
	}
	seal, e := f.h.Ledger.QueryCompletionSeal(f.ctx, f.caller, op.ClosureEvidenceRefs[0])
	if e != nil || seal.NoSendProven || !seal.PhysicalSendWasPossible {
		t.Fatalf("false whole operation proof: %v %v", seal, e)
	}
	if e = f.h.Budget.ProcessClosures(f.ctx); e != nil {
		t.Fatal(e)
	}
	b, e := f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
	if e != nil || b.Reserved != 30 {
		t.Fatalf("wrong fee released: %v %v", b, e)
	}
	_, e = f.h.Egress.Invoke(f.ctx, actor, second)
	requests, effects := target.Snapshot()
	if len(requests) != 1 || len(effects) != 1 {
		t.Fatalf("sealed resend reached target: %v %v (%v)", requests, effects, e)
	}
}

// 规则：G1、G4、G11、开始-1
func TestResendRequiresOriginalAndCurrentCapabilityGuarantees(t *testing.T) {
	target := simulator.New("idempotent")
	target.SetBehavior("drop-after-apply")
	f := newFixtureWithTarget(t, 100, 80, false, target)
	configureResendLimit(t, f, 2)
	a, first := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, first)
	accepted(t, r, e)
	original, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
	if e != nil {
		t.Fatal(e)
	}
	replacement := proto.Clone(original).(*v1.Capability)
	replacement.Ref, replacement.ApprovedBy = nil, nil
	replacement.AdapterRef.Name.LocalId = "simulator-opaque"
	r, e = f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("narrow-guarantee"), Replaces: original.Ref, Capability: replacement})
	accepted(t, r, e)
	saved, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, original.Ref)
	if e != nil || !proto.Equal(saved, original) {
		t.Fatalf("old snapshot overwritten: %v %v", saved, e)
	}
	before, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	claim, e := f.h.LedgerWork.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: ledgerHeader("narrowed-claim").Identity, ContractVersion: 1, Action: "CLAIM", AllowedTypes: []string{"EXECUTE_OPERATION"}, Limit: 1, LeaseMs: 30000, ProcessInstance: "new-worker"})
	accepted(t, claim, e)
	r, e = f.h.Ledger.PrepareResend(f.ctx, f.caller, &v1.PrepareResendCommand{Header: ledgerHeader("narrowed-resend"), OperationId: a.OperationId, PreviousSendRef: before.Send.Ref, Claim: claim.Jobs[0]})
	if e != nil || r.GetDecision() != v1.Decision_DECISION_REJECTED {
		t.Fatalf("narrowed guarantee resent: %v %v", r, e)
	}
	after, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || !proto.Equal(before, after.Execution) || after.Effect.Outcome != "UNKNOWN" {
		t.Fatalf("unknown changed: %v %v", after, e)
	}
	requests, effects := target.Snapshot()
	if len(requests) != 1 || len(effects) != 1 {
		t.Fatalf("extra request %v %v", requests, effects)
	}
}

// 规则：G1、G11、开始-1
func TestFiniteKeyRetentionWithoutArrivalProofKeepsUnknown(t *testing.T) {
	target := simulator.New("idempotent")
	target.SetBehavior("drop-after-apply")
	f := newFixtureWithTarget(t, 100, 80, false, target)
	cap, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
	if e != nil {
		t.Fatal(e)
	}
	cap.Ref, cap.ApprovedBy = nil, nil
	cap.MaxSends = 2
	cap.AdapterRef.Name.LocalId = "simulator-idempotent-evicting"
	cap.IdempotencyRetentionMs = 60000
	r, e := f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("finite-cap"), Capability: cap})
	accepted(t, r, e)
	f.capability = r.ResultRef
	a, first := prepareStart(t, f)
	r, e = f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, first)
	accepted(t, r, e)
	x, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	if x.Attempt.GetKeyValidUntilUnixMs() <= time.Now().UnixMilli() {
		t.Fatal("test must have positive remaining TTL")
	}
	claim, e := f.h.LedgerWork.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: ledgerHeader("finite-claim").Identity, ContractVersion: 1, Action: "CLAIM", AllowedTypes: []string{"EXECUTE_OPERATION"}, Limit: 1, LeaseMs: 30000, ProcessInstance: "worker2"})
	accepted(t, claim, e)
	r, e = f.h.Ledger.PrepareResend(f.ctx, f.caller, &v1.PrepareResendCommand{Header: ledgerHeader("finite-resend"), OperationId: a.OperationId, PreviousSendRef: x.Send.Ref, Claim: claim.Jobs[0]})
	if e != nil || r.GetError().GetCode() != "RESEND_ARRIVAL_UNPROVEN" {
		t.Fatalf("unproven TTL sent: %v %v", r, e)
	}
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || op.Effect.Outcome != "UNKNOWN" || !proto.Equal(op.Execution, x) {
		t.Fatalf("finite unknown %v %v", op, e)
	}
	requests, effects := target.Snapshot()
	if len(requests) != 1 || len(effects) != 1 {
		t.Fatalf("requests/effects %v %v", requests, effects)
	}
}

// 规则：G1、G3、G11、开始-1
func TestExpiringResendIsRejectedAtTargetAfterDelayedArrival(t *testing.T) {
	target := simulator.New("idempotent-expiring")
	target.SetBehavior("drop-after-apply")
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int64
	var expiryHeaders []string
	var headersMu sync.Mutex
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headersMu.Lock()
		expiryHeaders = append(expiryHeaders, r.Header.Get("Lerna-Key-Valid-Until"))
		headersMu.Unlock()
		if calls.Add(1) == 2 {
			close(entered)
			<-release
		}
		target.ServeHTTP(w, r)
	})
	f := newFixtureWithTarget(t, 100, 80, false, handler)
	cap, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
	if e != nil {
		t.Fatal(e)
	}
	cap.Ref, cap.ApprovedBy = nil, nil
	cap.MaxSends = 2
	cap.AdapterRef.Name.LocalId = "simulator-idempotent-expiring"
	cap.IdempotencyRetentionMs = 3000
	r, e := f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("expiry-cap"), Capability: cap})
	accepted(t, r, e)
	f.capability = r.ResultRef
	a, first := prepareStart(t, f)
	actor := &v1.Caller{UserId: "u", IssuerId: "egress"}
	r, e = f.h.Egress.Invoke(f.ctx, actor, first)
	accepted(t, r, e)
	old, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	target.SetBehavior("")
	second := prepareNextResend(t, f, a, first)
	done := make(chan error, 1)
	go func() { _, err := f.h.Egress.Invoke(f.ctx, actor, second); done <- err }()
	select {
	case <-entered:
	case err := <-done:
		close(release)
		t.Fatalf("no second IO: %v", err)
	case <-time.After(3 * time.Second):
		close(release)
		<-done
		t.Fatal("no IO")
	}
	time.Sleep(time.Until(time.UnixMilli(old.Attempt.GetKeyValidUntilUnixMs())) + 30*time.Millisecond)
	close(release)
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || op.Effect.Outcome != "UNKNOWN" || op.Effect.LateEffect != "MAY_OCCUR" || op.Execution.Attempt.GetKeyValidUntilUnixMs() != old.Attempt.GetKeyValidUntilUnixMs() || op.Execution.Attempt.FirstPossibleSendAtUnixMs != old.Attempt.FirstPossibleSendAtUnixMs {
		t.Fatalf("expiry changed original risk/window: %v %v", op, e)
	}
	raw, e := f.h.Ledger.QueryObservation(f.ctx, f.caller, op.Execution.Send.ObservationRef)
	if e != nil || raw.StatusCode != 410 {
		t.Fatalf("target did not reject expiry: %v %v", raw, e)
	}
	requests, effects := target.Snapshot()
	if len(requests) != 2 || len(effects) != 1 {
		t.Fatalf("late arrival created duplicate: %v %v", requests, effects)
	}
	headersMu.Lock()
	defer headersMu.Unlock()
	if len(expiryHeaders) != 2 || expiryHeaders[0] == "" || expiryHeaders[0] != expiryHeaders[1] {
		t.Fatalf("wire expiry changed %v", expiryHeaders)
	}
}

// 规则：G1、G2、G3、G11、R7
func TestLateUnknownFirstResponseCannotEraseProvenResendEffect(t *testing.T) {
	target := simulator.New("idempotent")
	target.SetBehavior("empty-success")
	f := newFixtureWithTarget(t, 100, 80, false, target)
	configureResendLimit(t, f, 2)
	a, first := prepareStart(t, f)
	old := performWithoutObservation(t, f, a, first)
	x, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	r, e := f.h.Ledger.PrepareResend(f.ctx, f.caller, &v1.PrepareResendCommand{Header: ledgerHeader("late-unknown-resend"), OperationId: a.OperationId, PreviousSendRef: x.Send.Ref, Claim: first.Claim})
	accepted(t, r, e)
	x, e = f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	second := resendStart(t, f, a, first, x, first.Claim)
	target.SetBehavior("")
	r, e = f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, second)
	accepted(t, r, e)
	before, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || before.Effect.Outcome != "APPLIED" {
		t.Fatalf("resend proof %v %v", before, e)
	}
	savePhysicalObservation(t, f, old)
	after, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || after.Effect.Outcome != "APPLIED" || after.Effect.LateEffect != "RULED_OUT" || after.Lifecycle != "SETTLED" || len(after.Effect.EvidenceRefs) != 2 {
		t.Fatalf("late weak observation erased stronger proof: %v %v", after, e)
	}
	requests, effects := target.Snapshot()
	if len(requests) != 2 || len(effects) != 1 {
		t.Fatalf("requests/effects %v %v", requests, effects)
	}
}

func performWithoutObservation(t *testing.T, f *fixture, a *v1.Admission, c *v1.StartExecutionCommand) *v1.PhysicalIOResult {
	t.Helper()
	actor := &v1.Caller{UserId: "u", IssuerId: "egress"}
	start, e := f.h.Tasks.StartExecution(f.ctx, actor, c)
	accepted(t, start, e)
	x, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	dh := ledgerHeader("dispatch:" + x.Send.Ref.Name.LocalId)
	dh.Identity.IssuerId = "egress"
	r, fresh, e := f.h.Ledger.RecordDispatch(f.ctx, actor, &v1.DispatchCommand{Header: dh, OperationId: a.OperationId, StartReceipt: start, Claim: c.Claim})
	accepted(t, r, e)
	if !fresh {
		t.Fatal("no physical send right")
	}
	x, e = f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	body, e := f.h.Content.Read(f.ctx, actor, x.CallDescriptor.ParametersRef)
	if e != nil {
		t.Fatal(e)
	}
	result, e := (egressio.HTTP{}).Perform(f.ctx, &v1.PhysicalIORequest{TaskId: a.TaskId, OperationId: a.OperationId, ExecutorEndpointId: a.ExecutorEndpointId, Attempt: x.Attempt, Send: x.Send, CallDescriptor: x.CallDescriptor, Body: command.ContentBytes(body)})
	if e != nil {
		t.Fatal(e)
	}
	return result
}

func savePhysicalObservation(t *testing.T, f *fixture, result *v1.PhysicalIOResult) {
	t.Helper()
	actor := &v1.Caller{UserId: "u", IssuerId: "egress-io"}
	h := header("observe:" + result.Observation.Ref.Name.LocalId)
	h.Identity.IssuerId = actor.IssuerId
	h.Identity.TargetDomainId = "d/content"
	r, e := f.h.Content.RegisterObservation(f.ctx, actor, &v1.RegisterObservationCommand{Header: h, Observation: result.Observation, Body: result.Body})
	accepted(t, r, e)
	if e = f.h.Content.ProcessObservations(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	if e = f.h.Ledger.ProcessReports(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	if e = f.h.Ledger.ProcessInterpretations(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
}

// 规则：G1、G4、G11、开始-2
func TestSafeResendUsesCurrentControlInsteadOfAdmissionGeneration(t *testing.T) {
	target := simulator.New("idempotent")
	target.SetBehavior("drop-after-apply")
	f := newFixtureWithTarget(t, 100, 80, false, target)
	configureResendLimit(t, f, 2)
	a, first := prepareStart(t, f)
	actor := &v1.Caller{UserId: "u", IssuerId: "egress"}
	r, e := f.h.Egress.Invoke(f.ctx, actor, first)
	accepted(t, r, e)
	resendTaskControl(t, f, "PAUSE", "pause")
	resendTaskControl(t, f, "RESUME", "resume")
	task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, a.TaskId)
	if e != nil || task.ControlGeneration == a.ControlGeneration {
		t.Fatalf("generation unchanged: %v %v", task, e)
	}
	second := prepareNextResend(t, f, a, first)
	target.SetBehavior("")
	r, e = f.h.Egress.Invoke(f.ctx, actor, second)
	accepted(t, r, e)
	requests, effects := target.Snapshot()
	if len(requests) != 2 || len(effects) != 1 {
		t.Fatalf("safe active resend blocked: %v %v", requests, effects)
	}
}

func resendTaskControl(t *testing.T, f *fixture, control, id string) {
	t.Helper()
	task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	goal, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("goal").Identity)
	if e != nil {
		t.Fatal(e)
	}
	r, e := f.h.Sessions.SubmitInput(f.ctx, f.caller, &v1.SubmitInputCommand{Header: header(id), SessionId: goal.Receipt.SessionRef.Name, TaskId: f.task.Name, InputKind: "CONTROL", Control: control, ExpectedControlGeneration: task.ControlGeneration})
	accepted(t, r, e)
}

// 规则：G1、G3、G11、开始-5
func TestOpaqueTimeoutRemainsUnknownInCLIAfterRestartAndResendRefusal(t *testing.T) {
	target := simulator.New("opaque")
	target.SetBehavior("drop-after-apply")
	f := newFixtureWithTarget(t, 100, 80, false, target)
	cap, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
	if e != nil {
		t.Fatal(e)
	}
	cap.Ref, cap.ApprovedBy = nil, nil
	cap.MaxSends = 2
	cap.AdapterRef.Name.LocalId = "simulator-opaque"
	r, e := f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("opaque-cap"), Capability: cap})
	accepted(t, r, e)
	f.capability = r.ResultRef
	a, first := prepareStart(t, f)
	r, e = f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, first)
	accepted(t, r, e)
	original, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	f.h, e = assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	claim, e := f.h.LedgerWork.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: ledgerHeader("opaque-claim").Identity, ContractVersion: 1, Action: "CLAIM", AllowedTypes: []string{"EXECUTE_OPERATION"}, Limit: 1, LeaseMs: 30000, ProcessInstance: "new-process"})
	accepted(t, claim, e)
	request := &v1.PrepareResendCommand{Header: ledgerHeader("opaque-resend"), OperationId: a.OperationId, PreviousSendRef: original.Execution.Send.Ref, Claim: claim.Jobs[0]}
	path := filepath.Join(t.TempDir(), "resend.json")
	data, e := protojson.Marshal(request)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, data, 0600); e != nil {
		t.Fatal(e)
	}
	cli := interaction.CLI{Sessions: f.h.Sessions, Tasks: f.h.Tasks, Durable: f.h.Durable, Ledger: f.h.Ledger, Egress: f.h.Egress, Content: f.h.Content, Observations: f.h.Content, Caller: f.caller, Domain: "d"}
	var out bytes.Buffer
	if e = cli.Run(f.ctx, []string{"prepare-resend", path}, &out); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(out.String(), "RESEND_UNSAFE") {
		t.Fatalf("refusal not visible: %s", out.String())
	}
	out.Reset()
	if e = cli.Run(f.ctx, []string{"recover"}, &out); e != nil {
		t.Fatal(e)
	}
	out.Reset()
	if e = cli.Run(f.ctx, []string{"operation", a.OperationId.LocalId}, &out); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(out.String(), "UNKNOWN") || !strings.Contains(out.String(), "MAY_OCCUR") {
		t.Fatalf("unknown not visible: %s", out.String())
	}
	after, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || !proto.Equal(after, original) {
		t.Fatalf("opaque responsibility changed: %v %v", after, e)
	}
	requests, effects := target.Snapshot()
	if len(requests) != 1 || len(effects) != 1 {
		t.Fatalf("opaque auto-retried %v %v", requests, effects)
	}
}

// 规则：G1、G3、G11
func TestOriginalInvocationReplayKeepsItsReceiptAfterResend(t *testing.T) {
	target := simulator.New("idempotent")
	target.SetBehavior("drop-after-apply")
	f := newFixtureWithTarget(t, 100, 80, false, target)
	configureResendLimit(t, f, 2)
	a, first := prepareStart(t, f)
	actor := &v1.Caller{UserId: "u", IssuerId: "egress"}
	original, e := f.h.Egress.Invoke(f.ctx, actor, first)
	accepted(t, original, e)
	second := prepareNextResend(t, f, a, first)
	target.SetBehavior("")
	r, e := f.h.Egress.Invoke(f.ctx, actor, second)
	accepted(t, r, e)
	replay, e := f.h.Egress.Invoke(f.ctx, actor, first)
	if e != nil || !proto.Equal(replay, original) {
		t.Fatalf("original dispatch receipt changed: %v %v", replay, e)
	}
	requests, effects := target.Snapshot()
	if len(requests) != 2 || len(effects) != 1 {
		t.Fatalf("replay sent %v %v", requests, effects)
	}
}

// 规则：G1、G4、G7、G10、G11、开始-1、开始-2、开始-3、开始-5
func TestResendCannotBypassCurrentControlAuthorityBudgetOrBindings(t *testing.T) {
	for _, kind := range []string{"PAUSE", "CANCEL", "BUDGET", "REVOKE", "PAYLOAD", "TARGET", "ENDPOINT", "USER", "CAPABILITY_AFTER_P4"} {
		t.Run(kind, func(t *testing.T) {
			target := simulator.New("idempotent")
			target.SetBehavior("drop-after-apply")
			f := newFixtureWithTarget(t, 100, 80, false, target)
			configureResendLimit(t, f, 2)
			a, first := prepareStart(t, f)
			actor := &v1.Caller{UserId: "u", IssuerId: "egress"}
			r, e := f.h.Egress.Invoke(f.ctx, actor, first)
			accepted(t, r, e)
			second := prepareNextResend(t, f, a, first)
			held := int64(30)
			switch kind {
			case "PAUSE", "CANCEL":
				resendTaskControl(t, f, kind, "control-before-resend")
			case "BUDGET":
				b, err := f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
				if err != nil {
					t.Fatal(err)
				}
				r, e = f.h.Budget.AdjustLimit(f.ctx, f.caller, &v1.AdjustBudgetLimitCommand{Header: header("limit-resend"), ExpectedRef: b.Ref, Limit: 40, Reason: "current limit"})
				accepted(t, r, e)
			case "REVOKE":
				r, e = f.h.Grants.Revoke(f.ctx, f.caller, &v1.RevokeGrantCommand{Header: header("revoke-before-resend"), GrantId: f.grant.Name})
				accepted(t, r, e)
			case "PAYLOAD":
				second.CallDescriptor = proto.Clone(second.CallDescriptor).(*v1.CallDescriptor)
				second.CallDescriptor.ParametersRef = proto.Clone(f.parameters).(*v1.Ref)
				second.CallDescriptor.ParametersRef.Revision++
			case "TARGET":
				second.CallDescriptor = proto.Clone(second.CallDescriptor).(*v1.CallDescriptor)
				second.CallDescriptor.Target += "/elsewhere"
			case "ENDPOINT":
				second.Binding.ExecutorEndpointId = "different-endpoint"
			case "USER":
				actor = &v1.Caller{UserId: "other", IssuerId: "egress"}
			case "CAPABILITY_AFTER_P4":
				r, e = f.h.Tasks.StartExecution(f.ctx, actor, second)
				accepted(t, r, e)
				held = 60
				cap, err := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
				if err != nil {
					t.Fatal(err)
				}
				original := cap.Ref
				cap.Ref, cap.ApprovedBy = nil, nil
				cap.AdapterRef.Revision = 2
				r, e = f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("changed-after-start"), Replaces: original, Capability: cap})
				accepted(t, r, e)
			}
			r, e = f.h.Egress.Invoke(f.ctx, actor, second)
			if e == nil && r.GetDecision() == v1.Decision_DECISION_ACCEPTED {
				t.Fatalf("gate bypassed: %v", r)
			}
			op, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if err != nil || op.Effect.Outcome != "UNKNOWN" || op.Execution.Send.Phase != "REGISTERED" {
				t.Fatalf("unknown or dispatch changed %v %v", op, err)
			}
			b, err := f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
			if err != nil || b.Reserved != held {
				t.Fatalf("fee gate changed reservation %v %v", b, err)
			}
			requests, effects := target.Snapshot()
			if len(requests) != 1 || len(effects) != 1 {
				t.Fatalf("gate reached target: %v %v", requests, effects)
			}
		})
	}
}

// 规则：G1、G11、开始-5
func TestResendOfAcceptedPendingRequestAppliesOnlyOnce(t *testing.T) {
	target := simulator.New("idempotent")
	target.SetBehavior("accept-and-delay")
	f := newFixtureWithTarget(t, 100, 80, false, target)
	configureResendLimit(t, f, 2)
	a, first := prepareStart(t, f)
	actor := &v1.Caller{UserId: "u", IssuerId: "egress"}
	r, e := f.h.Egress.Invoke(f.ctx, actor, first)
	accepted(t, r, e)
	second := prepareNextResend(t, f, a, first)
	r, e = f.h.Egress.Invoke(f.ctx, actor, second)
	accepted(t, r, e)
	target.ReleasePending()
	requests, effects := target.Snapshot()
	if len(requests) != 2 || len(effects) != 1 {
		t.Fatalf("same pending attempt applied twice: requests=%v effects=%v", requests, effects)
	}
}

// 规则：G1、G3、G11、开始-5
func TestQueryableIdempotentResendRequiresQueryFirst(t *testing.T) {
	for _, queryResult := range []string{"weak", "drop", "applied"} {
		t.Run(queryResult, func(t *testing.T) {
			target := simulator.New("idempotent-queryable")
			target.SetBehavior("drop-after-apply")
			f := newFixtureWithTarget(t, 200, 200, false, target)
			queryCap, queryGrant := configureReconciliation(t, f)
			cap, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
			if e != nil {
				t.Fatal(e)
			}
			cap.Ref, cap.ApprovedBy, cap.MaxSends = nil, nil, 2
			cap.AdapterRef.Name.LocalId = "simulator-idempotent-queryable"
			r, e := f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("both-capabilities"), Capability: cap})
			accepted(t, r, e)
			f.capability = r.ResultRef
			a, first := prepareStart(t, f)
			actor := &v1.Caller{UserId: "u", IssuerId: "egress"}
			r, e = f.h.Egress.Invoke(f.ctx, actor, first)
			accepted(t, r, e)
			before, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
			if e != nil {
				t.Fatal(e)
			}
			claim, e := f.h.LedgerWork.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: ledgerHeader("query-first-claim").Identity, ContractVersion: 1, Action: "CLAIM", AllowedTypes: []string{"EXECUTE_OPERATION"}, Limit: 1, LeaseMs: 30000, ProcessInstance: "retry-worker"})
			accepted(t, claim, e)
			if len(claim.Jobs) != 1 {
				t.Fatalf("claim %v", claim)
			}
			c := &v1.PrepareResendCommand{Header: ledgerHeader("before-query-resend"), OperationId: a.OperationId, PreviousSendRef: before.Send.Ref, Claim: claim.Jobs[0]}
			r, e = f.h.Ledger.PrepareResend(f.ctx, f.caller, c)
			if e != nil || r.GetError().GetCode() != "RESEND_QUERY_FIRST" {
				t.Fatalf("query-first bypass: %v %v", r, e)
			}
			assertModelSourceEvent(t, f, "DECISION_REJECTED", r.DecisionRef, "RESEND_QUERY_FIRST")
			target.SetQueryBehavior(queryResult)
			r, e = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, reconciliationCommand(f, a, queryCap, queryGrant))
			accepted(t, r, e)
			if e = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); e != nil {
				t.Fatal(e)
			}
			c.Header = ledgerHeader("after-query-resend")
			r, e = f.h.Ledger.PrepareResend(f.ctx, f.caller, c)
			if queryResult == "applied" {
				if e == nil && r.GetDecision() == v1.Decision_DECISION_ACCEPTED {
					t.Fatalf("settled write resent: %v", r)
				}
			} else {
				accepted(t, r, e)
				x, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
				if e != nil {
					t.Fatal(e)
				}
				assertResendTrace(t, f, c, r)
				target.SetBehavior("")
				r, e = f.h.Egress.Invoke(f.ctx, actor, resendStart(t, f, a, first, x, claim.Jobs[0]))
				accepted(t, r, e)
			}
			requests, effects := target.Snapshot()
			want := 3
			if queryResult == "applied" {
				want = 2
			}
			if len(requests) != want || requests[1].Method != "GET" || len(effects) != 1 {
				t.Fatalf("requests=%v effects=%v", requests, effects)
			}
			plan, e := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
			if e != nil || len(plan.GetQueryRefs()) != 1 {
				t.Fatalf("query identity: %v %v", plan, e)
			}
			relation, e := f.h.Ledger.QueryReconciliationQuery(f.ctx, f.caller, plan.QueryRefs[0])
			if e != nil {
				t.Fatal(e)
			}
			query, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, relation.QueryOperationRef.Name)
			if e != nil || query.QuerySubject == nil || !proto.Equal(query.QuerySubject.OperationId, a.OperationId) || proto.Equal(query.Ref.Name, a.OperationId) {
				t.Fatalf("query relabeled original: %v %v", query, e)
			}
			if requests[0].Operation != a.OperationId.LocalId || requests[0].Attempt != before.Attempt.Ref.Name.LocalId || requests[0].Send != "1" || requests[1].Operation != query.Ref.Name.LocalId || requests[1].Attempt != query.Execution.Attempt.Ref.Name.LocalId || requests[1].Send != "1" {
				t.Fatalf("actual query/original identity: %v", requests)
			}
			current, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
			if e != nil {
				t.Fatal(e)
			}
			executions := []*v1.Execution{before, query.Execution}
			if queryResult != "applied" {
				if requests[2].Operation != a.OperationId.LocalId || requests[2].Attempt != before.Attempt.Ref.Name.LocalId || requests[2].Send != "2" || requests[2].ExternalKey != requests[0].ExternalKey {
					t.Fatalf("actual resend identity: %v", requests)
				}
				executions = append(executions, current)
			}
			seenSources := map[string]bool{}
			for _, execution := range executions {
				source, err := f.h.Budget.QueryBillingSource(f.ctx, f.caller, execution.Send.Ref)
				if err != nil || source == nil || !proto.Equal(source.SendRef.Name, execution.Send.Ref.Name) || !proto.Equal(source.OperationId, execution.Attempt.OperationId) || source.Amount != nil || seenSources[source.Ref.Name.LocalId] {
					t.Fatalf("query/resend fee source merged or fabricated: %v %v", source, err)
				}
				seenSources[source.Ref.Name.LocalId] = true
			}
			if queryResult == "drop" {
				plan, e := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
				if e != nil {
					t.Fatal(e)
				}
				q, e := f.h.Ledger.QueryReconciliationQuery(f.ctx, f.caller, plan.QueryRefs[0])
				if e != nil {
					t.Fatal(e)
				}
				read, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, q.QueryOperationRef.Name)
				if e != nil || read.Effect.Outcome != "UNKNOWN" || read.Effect.LateEffect != "MAY_OCCUR" {
					t.Fatalf("lost query responsibility erased: %v %v", read, e)
				}
			}
		})
	}
}

// 规则：G1、G3、G11、R7
func TestLateUnknownOriginalDoesNotRevokePreparedResendClaim(t *testing.T) {
	target := simulator.New("idempotent")
	target.SetBehavior("empty-success")
	f := newFixtureWithTarget(t, 100, 80, false, target)
	configureResendLimit(t, f, 2)
	a, first := prepareStartWithLease(t, f, 1500)
	raw := performWithoutObservation(t, f, a, first)
	time.Sleep(time.Until(time.UnixMilli(first.Claim.LeaseUntilUnixMs)) + 20*time.Millisecond)
	second := prepareNextResend(t, f, a, first)
	savePhysicalObservation(t, f, raw)
	target.SetBehavior("")
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, second)
	accepted(t, r, e)
	requests, effects := target.Snapshot()
	if len(requests) != 2 || len(effects) != 1 {
		t.Fatalf("requests=%v effects=%v", requests, effects)
	}
}

// 规则：G1、G3、G10、G11、开始-5
func TestResendExpiryIsCheckedAtPreparationStartAndDispatch(t *testing.T) {
	for _, phase := range []string{"prepare", "start", "dispatch"} {
		t.Run(phase, func(t *testing.T) {
			target := simulator.New("idempotent-expiring")
			target.SetBehavior("drop-after-apply")
			f := newFixtureWithTarget(t, 100, 80, false, target)
			cap, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
			if e != nil {
				t.Fatal(e)
			}
			cap.Ref, cap.ApprovedBy, cap.MaxSends = nil, nil, 2
			cap.AdapterRef.Name.LocalId = "simulator-idempotent-expiring"
			cap.IdempotencyRetentionMs = 2000
			r, e := f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("expiry-gate-cap"), Capability: cap})
			accepted(t, r, e)
			f.capability = r.ResultRef
			a, first := prepareStart(t, f)
			actor := &v1.Caller{UserId: "u", IssuerId: "egress"}
			r, e = f.h.Egress.Invoke(f.ctx, actor, first)
			accepted(t, r, e)
			before, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
			if e != nil {
				t.Fatal(e)
			}
			var second *v1.StartExecutionCommand
			if phase != "prepare" {
				second = prepareNextResend(t, f, a, first)
			}
			if phase == "dispatch" {
				r, e = f.h.Tasks.StartExecution(f.ctx, actor, second)
				accepted(t, r, e)
			}
			time.Sleep(time.Until(time.UnixMilli(before.Attempt.GetKeyValidUntilUnixMs())) + 20*time.Millisecond)
			if phase == "prepare" {
				claim, err := f.h.LedgerWork.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: ledgerHeader("expired-claim").Identity, ContractVersion: 1, Action: "CLAIM", AllowedTypes: []string{"EXECUTE_OPERATION"}, Limit: 1, LeaseMs: 30000, ProcessInstance: "expired-worker"})
				accepted(t, claim, err)
				r, e = f.h.Ledger.PrepareResend(f.ctx, f.caller, &v1.PrepareResendCommand{Header: ledgerHeader("expired-resend"), OperationId: a.OperationId, PreviousSendRef: before.Send.Ref, Claim: claim.Jobs[0]})
			} else {
				r, e = f.h.Egress.Invoke(f.ctx, actor, second)
			}
			if e != nil || r.GetError().GetCode() != "RESEND_KEY_EXPIRED" {
				t.Fatalf("expiry bypassed at %s: %v %v", phase, r, e)
			}
			requests, effects := target.Snapshot()
			if len(requests) != 1 || len(effects) != 1 {
				t.Fatalf("expired IO: %v %v", requests, effects)
			}
			op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if e != nil || op.Effect.Outcome != "UNKNOWN" || op.Effect.LateEffect != "MAY_OCCUR" {
				t.Fatalf("unknown erased: %v %v", op, e)
			}
		})
	}
}

// 规则：G1、G4、准入-6
func TestCapabilityReplacementCannotAdmitAnObsoleteReconciliationRead(t *testing.T) {
	target := simulator.New("queryable")
	target.SetBehavior("drop-after-apply")
	f := newFixtureWithTarget(t, 200, 200, false, target)
	queryCap, grant := configureReconciliation(t, f)
	a, first := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, first)
	accepted(t, r, e)
	r, e = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, reconciliationCommand(f, a, queryCap, grant))
	accepted(t, r, e)
	cap, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, queryCap)
	if e != nil {
		t.Fatal(e)
	}
	cap.Ref, cap.ApprovedBy = nil, nil
	cap.MaxSends = 0
	r, e = f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("replace-read"), Replaces: queryCap, Capability: cap})
	accepted(t, r, e)
	_ = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller)
	requests, effects := target.Snapshot()
	if len(requests) != 1 || len(effects) != 1 {
		t.Fatalf("obsolete read admitted: %v %v", requests, effects)
	}
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || op.Effect.Outcome != "UNKNOWN" {
		t.Fatalf("original unknown changed %v %v", op, e)
	}
}

// 规则：G3、G4、G6、准入-6
func TestCapabilityReplacementFencesNewModelAuthority(t *testing.T) {
	for _, phase := range []string{"prepare", "admit", "confirmation"} {
		t.Run(phase, func(t *testing.T) {
			provider := simulator.NewModelProvider()
			f := modelFixtureTarget(t, provider)
			run := modelRunCommand(t, f, 30000)
			var call *v1.ModelCall
			var e error
			if phase != "prepare" {
				call, e = f.h.Tasks.PrepareModelCall(f.ctx, f.caller, run.Preparation)
				if e != nil {
					t.Fatal(e)
				}
			}
			cap, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
			if e != nil {
				t.Fatal(e)
			}
			cap.Ref, cap.ApprovedBy = nil, nil
			cap.MaxSends = 0
			r, e := f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("obsolete-model-cap"), Replaces: f.capability, Capability: cap})
			accepted(t, r, e)
			switch phase {
			case "prepare":
				_, e = f.h.Tasks.PrepareModelCall(f.ctx, f.caller, run.Preparation)
				if e == nil || e.Error() != "CAPABILITY_INVALID" {
					t.Fatalf("obsolete preparation: %v", e)
				}
			case "admit":
				r, e = f.h.Tasks.AdmitModelCall(f.ctx, f.caller, &v1.AdmitModelCallCommand{Header: header("obsolete-model-admission"), RequestRef: call.RequestRef, Claim: run.Preparation.Claim, DescriptorDigest: call.DescriptorDigest, GrantRef: f.grant})
				if e != nil || r.GetError().GetCode() != "CAPABILITY_INVALID" {
					t.Fatalf("obsolete admission: %v %v", r, e)
				}
			case "confirmation":
				goal, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("goal").Identity)
				if e != nil {
					t.Fatal(e)
				}
				r, e = f.h.Tasks.RequestAdmissionConfirmation(f.ctx, f.caller, &v1.RequestAdmissionConfirmationCommand{Header: header("obsolete-model-confirmation"), TaskId: f.task.Name, ProposalRef: call.Ref, GrantRef: f.grant, SessionId: goal.Receipt.SessionRef.Name})
				if e != nil || r.GetError().GetCode() != "CAPABILITY_INVALID" {
					t.Fatalf("obsolete confirmation: %v %v", r, e)
				}
			}
			requests, charges := provider.Calls(), provider.Bills()
			if requests != 0 || len(charges) != 0 {
				t.Fatalf("obsolete model sent: %v %v", requests, charges)
			}
		})
	}
}

// 规则：G3、G6、G9、准入-10
func TestModelInputRetainsExactCapabilityVersionsFromSnapshot(t *testing.T) {
	f := modelFixture(t)
	run := modelRunCommand(t, f, 30000)
	planning, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	var original *v1.Capability
	for _, ref := range planning.Snapshot.CapabilityRefs {
		if !proto.Equal(ref, f.capability) {
			original, e = f.h.Tasks.QueryCapability(f.ctx, f.caller, ref)
			if e != nil {
				t.Fatal(e)
			}
			break
		}
	}
	if original == nil {
		t.Fatal("fixture needs another snapshot capability")
	}
	changed := proto.Clone(original).(*v1.Capability)
	changed.Ref, changed.ApprovedBy = nil, nil
	changed.Resource = "http://127.0.0.1:1/after-snapshot"
	r, e := f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("replace-snapshot-cap"), Replaces: original.Ref, Capability: changed})
	accepted(t, r, e)
	call, e := f.h.Tasks.PrepareModelCall(f.ctx, f.caller, run.Preparation)
	if e != nil {
		t.Fatal(e)
	}
	content, e := f.h.Content.Read(f.ctx, f.caller, call.InputRef)
	if e != nil {
		t.Fatal(e)
	}
	var body struct {
		Facts []json.RawMessage `json:"facts"`
	}
	if e = json.Unmarshal(command.ContentBytes(content), &body); e != nil {
		t.Fatal(e)
	}
	found := false
	for _, fact := range body.Facts {
		cap := new(v1.Capability)
		if protojson.Unmarshal(fact, cap) == nil && proto.Equal(cap.GetRef().GetName(), original.Ref.Name) {
			found = true
			if !proto.Equal(cap, original) {
				t.Fatalf("snapshot version changed: got=%v want=%v", cap, original)
			}
		}
	}
	if !found {
		t.Fatal("original capability fact omitted")
	}
}

// 规则：G1、G4、准入-6
func TestCapabilityReplacementRejectsNewReconciliationPlan(t *testing.T) {
	target := simulator.New("queryable")
	target.SetBehavior("drop-after-apply")
	f := newFixtureWithTarget(t, 200, 200, false, target)
	queryCap, grant := configureReconciliation(t, f)
	a, first := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, first)
	accepted(t, r, e)
	cap, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, queryCap)
	if e != nil {
		t.Fatal(e)
	}
	cap.Ref, cap.ApprovedBy = nil, nil
	cap.MaxSends = 0
	r, e = f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("replace-before-plan"), Replaces: queryCap, Capability: cap})
	accepted(t, r, e)
	r, e = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, reconciliationCommand(f, a, queryCap, grant))
	if e != nil || r.GetError().GetCode() != "CAPABILITY_INVALID" {
		t.Fatalf("obsolete plan admitted: %v %v", r, e)
	}
	requests, effects := target.Snapshot()
	if len(requests) != 1 || len(effects) != 1 {
		t.Fatalf("unexpected query %v %v", requests, effects)
	}
}
