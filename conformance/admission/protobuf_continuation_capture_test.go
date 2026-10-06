package admission_test

import (
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/protobuf"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// 规则：H1、G2、G3、G11、完成-4、准入-5
func TestCaptureRejectedContinuationPublicObjectsRoundTrip(t *testing.T) {
	var samples []protobuf.Sample
	captureRejectedContinuation(t, &samples)
	named := roundTripClosingSamples(t, samples)
	for _, name := range []string{"continuation-rejected-verification", "continuation-request", "continuation-snapshot", "continuation-owner-job", "continuation-original-admission", "continuation-original-operation", "continuation-begin-command", "continuation-replayed-planning"} {
		if named[name] == nil {
			t.Errorf("missing actual rejected continuation sample %s", name)
		}
	}
	if t.Failed() {
		return
	}
	v := named["continuation-rejected-verification"].(*v1.Verification)
	req := named["continuation-request"].(*v1.ProposalRequest)
	snapshot := named["continuation-snapshot"].(*v1.ContextSnapshot)
	job := named["continuation-owner-job"].(*v1.Job)
	admission := named["continuation-original-admission"].(*v1.Admission)
	operation := named["continuation-original-operation"].(*v1.Operation)
	if v.Status != "REJECTED" || len(v.Gaps) == 0 || v.ContinuationRequestRef == nil || !proto.Equal(v.ContinuationRequestRef, req.Ref) || !proto.Equal(req.SnapshotRef, snapshot.Ref) || !proto.Equal(snapshot.RequestRef, req.Ref) || req.State != "PENDING" || req.Purpose != "PLAN" || !proto.Equal(req.JobRef.Name, job.Ref.Name) || job.JobType != "PROPOSE" || job.State != "READY" || !proto.Equal(job.SpecificationRef, req.Ref) {
		t.Fatal("rejected Verification lost unique actual continuation chain")
	}
	if len(v.AdmissionRefs) != 1 || !proto.Equal(v.AdmissionRefs[0], admission.Ref) || !proto.Equal(operation.Ref.Name, admission.OperationId) || operation.Dispatch != "SEALED" || operation.Lifecycle != "SETTLED" || operation.Effect.Outcome != "NOT_APPLIED" || len(snapshot.ProgressFacts) != 1 || !proto.Equal(snapshot.ProgressFacts[0].AdmissionRef, admission.Ref) {
		t.Fatal("continuation replaced original rejected action")
	}
	t.Logf("captured %d actual rejected-continuation objects; Verification19 and unique Request/Snapshot/Job round-tripped", len(samples))
}

// 规则：G2、G3、G10、G11、完成-4、准入-5
func captureRejectedContinuation(t *testing.T, samples *[]protobuf.Sample) {
	t.Helper()
	add := func(name string, m proto.Message, e error) {
		t.Helper()
		captureObject(t, samples, "continuation-"+name, m, e)
	}
	target := simulator.NewBillingTarget(25)
	target.Target.SetBehavior("reject")
	f := newFixtureWithTarget(t, 100, 80, false, target)
	scopeRequirement(t, f)
	a, start := prepareStart(t, f)
	add("original-admission", a, nil)
	receipt, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	accepted(t, receipt, e)
	proposal := completeProposal(t, f, []*v1.CompletionEvidence{{ConditionId: "created", OperationId: a.OperationId}})
	before, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, a.TaskId)
	add("planning-before", before, e)
	begin := &v1.BeginCompletionCommand{Header: header("capture-rejected-continuation"), TaskId: a.TaskId, ProposalRef: proposal}
	begun, e := f.h.Tasks.BeginCompletion(f.ctx, f.caller, begin)
	accepted(t, begun, e)
	add("begin-command", begin, nil)
	add("begin-receipt", begun, nil)
	if e = f.h.Tasks.ProcessCompletions(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	next, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, a.TaskId)
	add("planning-after", next, e)
	if next.Snapshot == nil || proto.Equal(next.Snapshot.RequestRef, before.Snapshot.RequestRef) || next.Snapshot.PlanningGeneration != before.Snapshot.PlanningGeneration+1 || next.VerificationFreeze != 0 || next.Proposal != nil {
		t.Fatal("real rejection did not generate one new request", next)
	}
	v, e := f.h.Tasks.QueryVerification(f.ctx, f.caller, next.VerificationRef)
	add("rejected-verification", v, e)
	if v.Status != "REJECTED" || v.ContinuationRequestRef == nil || !proto.Equal(v.ContinuationRequestRef, next.Snapshot.RequestRef) || len(v.Gaps) == 0 {
		t.Fatal("Verification19 not populated by actual rejection", v)
	}
	req, e := f.h.Tasks.QueryProposalRequest(f.ctx, f.caller, v.ContinuationRequestRef)
	add("request", req, e)
	snapshot, e := f.h.Tasks.QuerySnapshot(f.ctx, f.caller, req.SnapshotRef)
	add("snapshot", snapshot, e)
	job, e := f.h.Durable.QueryJob(f.ctx, f.caller, req.JobRef.Name)
	add("owner-job", job, e)
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	add("original-operation", op, e)
	source, e := f.h.Budget.QueryBillingSource(f.ctx, f.caller, op.Execution.Send.Ref)
	add("original-billing", source, e)
	if source.Amount == nil || *source.Amount != 25 || source.Status != "SETTLED" {
		t.Fatal("rejected original fee absent", source)
	}
	budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
	add("budget", budget, e)
	if budget.Settled != 25 || budget.Reserved != 0 {
		t.Fatal("rejected request hid original fee", budget)
	}
	assertUnique := func() {
		t.Helper()
		jobs, e := f.h.Durable.Pending(f.ctx, f.caller)
		if e != nil {
			t.Fatal(e)
		}
		count := 0
		for _, j := range jobs {
			if j.JobType == "PROPOSE" {
				count++
				if !proto.Equal(j.Ref, job.Ref) || !proto.Equal(j.SpecificationRef, req.Ref) {
					t.Fatal("unexpected replacement PROPOSE", j)
				}
			}
		}
		if count != 1 {
			t.Fatalf("actual pending PROPOSE jobs=%d want1", count)
		}
		r, e := f.h.Tasks.QueryProposalRequest(f.ctx, f.caller, req.Ref)
		if e != nil || !proto.Equal(r, req) {
			t.Fatal("original continuation request changed", e)
		}
		current, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, a.TaskId)
		if e != nil || !proto.Equal(current.Snapshot, snapshot) || !proto.Equal(current.VerificationRef, v.Ref) {
			t.Fatal("replay resampled continuation", e)
		}
		result, e := f.h.Tasks.QueryResult(f.ctx, f.caller, a.TaskId)
		if e != nil || result != nil {
			t.Fatal("rejected round created Result", e)
		}
	}
	assertUnique()
	for range 2 {
		if e = f.h.Tasks.ProcessCompletions(f.ctx, f.caller); e != nil {
			t.Fatal(e)
		}
	}
	replay, e := f.h.Tasks.BeginCompletion(f.ctx, f.caller, begin)
	if e != nil || !proto.Equal(replay, begun) {
		t.Fatal("original Begin receipt changed", e)
	}
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	h, e := assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	f.h = h
	assertUnique()
	current, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, a.TaskId)
	add("replayed-planning", current, e)
	original, e := f.h.Tasks.QueryVerification(f.ctx, f.caller, v.Ref)
	if e != nil || !proto.Equal(original, v) {
		t.Fatal("recovery rewrote rejected Verification", e)
	}
	assertTaskOwnerSource(t, f, "VERIFICATION_CHANGED", v.Ref, v.ContinuationRequestRef)
	assertTaskOwnerSource(t, f, "PROPOSAL_REQUESTED", snapshot.Ref, req.Ref)
	requests, effects := target.Target.Snapshot()
	if len(requests) != 1 || len(effects) != 0 || len(target.Bills()) != 1 {
		t.Fatal("continuation repeated original action")
	}
	t.Logf("rejected continuation actual calls=%d effects=%d bills=%d pending_PROPOSE=1 current_settled=%d no_Result=true", len(requests), len(effects), len(target.Bills()), budget.Settled)
}
