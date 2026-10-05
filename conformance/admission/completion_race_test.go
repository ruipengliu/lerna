package admission_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G2、G4、开始-2、完成-6
func TestCompletionEndpointSealWinsAfterP4BeforeP5(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	scopeRequirement(t, f)
	a, c := prepareStart(t, f)
	actor := &v1.Caller{UserId: "u", IssuerId: "egress"}
	start, e := f.h.Tasks.StartExecution(f.ctx, actor, c)
	accepted(t, start, e)
	before, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || before.StartReceiptObtained {
		t.Fatalf("test must cover P4 before ledger sees start: %v %v", before, e)
	}
	p := completeProposal(t, f, []*v1.CompletionEvidence{{ConditionId: "created", OperationId: a.OperationId}})
	r, e := f.h.Tasks.BeginCompletion(f.ctx, f.caller, &v1.BeginCompletionCommand{Header: header("p4-begin"), TaskId: f.task.Name, ProposalRef: p})
	accepted(t, r, e)
	if e = f.h.Tasks.ProcessCompletionClosures(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || op.Lifecycle != "SETTLED" || op.Dispatch != "SEALED" || op.Effect.Outcome != "NOT_APPLIED" || op.Effect.LateEffect != "RULED_OUT" {
		t.Fatalf("not sealed %v %v", op, e)
	}
	seal, e := f.h.Ledger.QueryCompletionSeal(f.ctx, f.caller, op.ClosureEvidenceRefs[0])
	if e != nil || !seal.NoSendProven || seal.PhysicalSendWasPossible {
		t.Fatalf("no-send proof %v %v", seal, e)
	}
	original, e := f.h.Tasks.QueryStartReceipt(f.ctx, actor, c.Header.Identity)
	if e != nil || !proto.Equal(original.Receipt, start) {
		t.Fatalf("start fact lost %v %v", original, e)
	}
	r, e = f.h.Egress.Invoke(f.ctx, actor, c)
	if e == nil && r.Decision == v1.Decision_DECISION_ACCEPTED {
		t.Fatalf("sealed start replay accepted physical dispatch: %v", r)
	}
	if f.calls.Load() != 0 {
		t.Fatal("sealed P4 replay sent")
	}
	reservations, e := f.h.Budget.QueryReservations(f.ctx, f.caller, f.task.Name)
	if e != nil || len(reservations) != 1 || reservations[0].ConsumedSends != 1 {
		t.Fatalf("closure erased consumed send %v %v", reservations, e)
	}
}

// 规则：G1、G2、G4、R7、完成-6
func TestCompletionClosureAcrossHarnessesWaitsForActualUse(t *testing.T) {
	target := simulator.New("idempotent")
	entered := make(chan struct{})
	release := make(chan struct{})
	f := newFixtureWithTarget(t, 100, 80, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-release; target.ServeHTTP(w, r) }))
	scopeRequirement(t, f)
	a, c := prepareStart(t, f)
	h2, e := assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	defer h2.Close()
	invoke := make(chan error, 1)
	go func() {
		r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
		if e == nil && r.Error != nil {
			e = &receiptError{code: r.Error.Code}
		}
		invoke <- e
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatal("IO did not begin")
	}
	p := completeProposal(t, f, []*v1.CompletionEvidence{{ConditionId: "created", OperationId: a.OperationId}})
	r, e := h2.Tasks.BeginCompletion(f.ctx, f.caller, &v1.BeginCompletionCommand{Header: header("competing-begin"), TaskId: f.task.Name, ProposalRef: p})
	accepted(t, r, e)
	sealed := make(chan error, 1)
	go func() { sealed <- h2.Tasks.ProcessCompletionClosures(f.ctx, f.caller) }()
	select {
	case e := <-sealed:
		close(release)
		t.Fatalf("closure crossed active physical use: %v", e)
	case <-time.After(75 * time.Millisecond):
	}
	close(release)
	if e = <-invoke; e != nil {
		t.Fatal(e)
	}
	if e = <-sealed; e != nil {
		t.Fatal(e)
	}
	if e = h2.Tasks.ProcessCompletions(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	result, e := h2.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	if e != nil || result == nil || result.Outcome != "SUCCEEDED" {
		t.Fatalf("terminal result %v %v", result, e)
	}
	op, e := h2.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	seal, e := h2.Ledger.QueryCompletionSeal(f.ctx, f.caller, op.ClosureEvidenceRefs[0])
	if e != nil || seal.NoSendProven || !seal.PhysicalSendWasPossible {
		t.Fatalf("P5 was erased %v %v", seal, e)
	}
	requests, effects := target.Snapshot()
	if len(requests) != 1 || len(effects) != 1 {
		t.Fatal("closure repeated target effect")
	}
}

type receiptError struct{ code string }

func (e *receiptError) Error() string { return e.code }

// 规则：G2、G10、G11、R7、完成-7
func TestLateSupersededClosureChangesLiveFactsButNeverFixedResult(t *testing.T) {
	target := simulator.New("idempotent")
	f := newFixtureWithTarget(t, 100, 80, false, target)
	scopeRequirement(t, f)
	a, c := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
	accepted(t, r, e)
	evidence := []*v1.CompletionEvidence{{ConditionId: "created", OperationId: a.OperationId}}
	p := completeProposal(t, f, evidence)
	r, e = f.h.Tasks.BeginCompletion(f.ctx, f.caller, &v1.BeginCompletionCommand{Header: header("old-seal-begin"), TaskId: f.task.Name, ProposalRef: p})
	accepted(t, r, e)
	oldRef := r.ResultRef
	mutateCompletionBasis(t, f, "NONBASIS")
	f.suffix = "new-round"
	p = completeProposal(t, f, evidence)
	r, e = f.h.Tasks.BeginCompletion(f.ctx, f.caller, &v1.BeginCompletionCommand{Header: header("new-seal-begin"), TaskId: f.task.Name, ProposalRef: p})
	accepted(t, r, e)
	newRef := r.ResultRef
	claim, e := f.h.Durable.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: header("two-round-seals").Identity, ContractVersion: 1, Action: "CLAIM", Module: "tasks", AllowedTypes: []string{"DELIVER_COMPLETION_CLOSURE"}, Limit: 10, LeaseMs: 30000, ProcessInstance: "closure-worker"})
	accepted(t, claim, e)
	var oldJob, newJob *v1.Job
	for _, j := range claim.Jobs {
		intent, e := f.h.Tasks.QueryCompletionIntent(f.ctx, f.caller, j.SpecificationRef)
		if e != nil {
			t.Fatal(e)
		}
		if proto.Equal(intent.Command.VerificationRef.Name, oldRef.Name) {
			oldJob = j
		}
		if proto.Equal(intent.Command.VerificationRef.Name, newRef.Name) {
			newJob = j
		}
	}
	if oldJob == nil || newJob == nil {
		t.Fatal("both durable closure responsibilities required")
	}
	if e = f.h.Tasks.ProcessCompletionClosureClaim(f.ctx, newJob); e != nil {
		t.Fatal(e)
	}
	r, e = f.h.Tasks.RecheckCompletion(f.ctx, f.caller, &v1.RecheckCompletionCommand{Header: header("new-round-finish"), VerificationRef: newRef})
	accepted(t, r, e)
	fixed, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	if e != nil || fixed == nil || fixed.UsageSnapshot == nil || fixed.UsageSnapshot.Reserved != 30 {
		t.Fatalf("fixed snapshot %v %v", fixed, e)
	}
	if e = f.h.Tasks.ProcessCompletionClosureClaim(f.ctx, oldJob); e != nil {
		t.Fatal(e)
	}
	current, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || current.Ref.Revision <= fixed.OperationRefs[0].Revision {
		t.Fatalf("late fact missing %v %v", current, e)
	}
	historical, e := f.h.Ledger.QueryOperationVersion(f.ctx, f.caller, fixed.OperationRefs[0])
	if e != nil || !proto.Equal(historical.Ref, fixed.OperationRefs[0]) {
		t.Fatalf("historical result basis lost %v %v", historical, e)
	}
	again, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	if e != nil || !proto.Equal(again, fixed) {
		t.Fatal("late closure changed Result")
	}
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	f.h, e = assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	again, e = f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	if e != nil || !proto.Equal(again, fixed) {
		t.Fatal("restart changed fixed Result")
	}
	requests, effects := target.Snapshot()
	if len(requests) != 1 || len(effects) != 1 {
		t.Fatal("late closure repeated target")
	}
}
