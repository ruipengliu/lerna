package admission_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// 规则：G2、G3、G4、G6、G10、G11、准入-5、完成-4
func TestProductionDriverUsesRejectedVerificationContinuationForRemedy(t *testing.T) {
	x := rejectionDriverFixture(t)
	f := x.f
	d := x.advance(t)
	firstRequest := d.RequestRef
	d = x.advance(t)
	outcome, e := f.h.Tasks.QueryProposalOutcome(f.ctx, f.caller, d.OutcomeRef)
	if e != nil || outcome.GetProposalRef() == nil {
		t.Fatalf("real first model outcome: %v %v", outcome, e)
	}
	approvalFixture := &fixture{h: f.h, ctx: f.ctx, caller: f.caller, task: f.task, grant: x.targetGrant}
	x.policy.Actions[0].ConfirmationRef = approveContinuationAction(t, approvalFixture, outcome.ProposalRef, "driver-first")
	x.configure(t, "driver-first-authority", d.Ref)
	d = x.advance(t)
	firstAdmission, e := f.h.Tasks.QueryAdmission(f.ctx, f.caller, d.AdmissionRef)
	if e != nil || firstAdmission == nil {
		t.Fatalf("real first driver admission: %v %v", firstAdmission, e)
	}
	d = x.advance(t)
	if proto.Equal(d.RequestRef, firstRequest) {
		t.Fatal("real target result did not create next request")
	}
	failedInput, e := f.h.Tasks.QuerySnapshot(f.ctx, f.caller, mustDriverRequest(t, f, d.RequestRef).SnapshotRef)
	if e != nil || len(failedInput.ProgressFacts) == 0 {
		t.Fatalf("actual failed target snapshot: %v %v", failedInput, e)
	}
	d = x.advance(t)
	failedComplete, e := f.h.Tasks.ReadProposal(f.ctx, f.caller, mustDriverOutcome(t, f, d.OutcomeRef).ProposalRef)
	if e != nil || failedComplete.Kind != "COMPLETE" || len(failedComplete.Verdicts) != 1 || failedComplete.Verdicts[0].Conclusion != "UNSATISFIED" || !proto.Equal(failedComplete.Verdicts[0].OperationId, firstAdmission.OperationId) || len(failedComplete.Verdicts[0].EvidenceRefs) == 0 {
		t.Fatalf("not truthful actual negative model proposal: %v %v", failedComplete, e)
	}
	x.advance(t)
	planning, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	v, e := f.h.Tasks.QueryVerification(f.ctx, f.caller, planning.VerificationRef)
	if e != nil || v.Status != "REJECTED" || v.EndedReason != "REQUIRED_CONDITION_NOT_MET" || len(v.Conditions) != 1 || v.Conditions[0].Conclusion != "UNSATISFIED" || planning.VerificationFreeze != 0 || v.ContinuationRequestRef == nil || !proto.Equal(v.ContinuationRequestRef, planning.Snapshot.RequestRef) {
		t.Fatalf("truthful negative evidence did not produce exact rejected continuation: %v planning=%v %v", v, planning, e)
	}
	req := mustDriverRequest(t, f, v.ContinuationRequestRef)
	if !proto.Equal(req.SnapshotRef, planning.Snapshot.Ref) || req.State != "PENDING" || req.Purpose != "PLAN" || len(req.ModelOperationRefs) != 0 || len(req.OutcomeRefs) != 0 {
		t.Fatalf("original completion owner request: %v", req)
	}
	job, e := f.h.Durable.QueryJob(f.ctx, f.caller, req.JobRef.Name)
	if e != nil || job.State != "READY" || job.JobType != "PROPOSE" || !proto.Equal(job.SpecificationRef, req.Ref) {
		t.Fatalf("original completion owner job: %v %v", job, e)
	}
	d = x.advance(t)
	if !proto.Equal(d.RequestRef, req.Ref) || d.OutcomeRef != nil || d.AdmissionRef != nil {
		t.Fatalf("driver substituted the rejected round request: %v", d)
	}
	if x.provider.Calls() != 2 || len(x.provider.Bills()) != 2 {
		t.Fatal("adoption sampled before actual owner request was observed")
	}
	rejectedBytes, e := proto.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	firstOperation, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, firstAdmission.OperationId)
	if e != nil || firstOperation.Dispatch != "SEALED" || firstOperation.Effect.Outcome != "NOT_APPLIED" || firstOperation.Effect.LateEffect != "RULED_OUT" {
		t.Fatalf("actual original negative operation: %v %v", firstOperation, e)
	}
	d = x.advance(t)
	remedy := mustDriverOutcome(t, f, d.OutcomeRef)
	if remedy.ProposalRef == nil {
		t.Fatalf("actual remedy model outcome missing proposal: driver=%v outcome=%v", d, remedy)
	}
	if !proto.Equal(remedy.RequestRef, req.Ref) {
		t.Fatalf("sampled substitute request: %v", remedy)
	}
	proposal, e := f.h.Tasks.ReadProposal(f.ctx, f.caller, remedy.ProposalRef)
	if e != nil || proposal.Kind != "ACTION" || !proto.Equal(proposal.ContextSnapshotRef, req.SnapshotRef) {
		t.Fatalf("real remedy model proposal: %v %v", proposal, e)
	}
	// 消费过的原确认不能授权补建，driver 仍保留这次原模型位置。
	d = x.advance(t)
	if d.State != "WAITING" || d.WaitingReason != "CONFIRMATION_INVALID" || d.AdmissionRef != nil || x.provider.Calls() != 3 || f.calls.Load() != 1 {
		t.Fatalf("original confirmation reused or resampled: %v", d)
	}
	fresh, e := f.h.Grants.QueryGrant(f.ctx, f.caller, x.targetGrant)
	if e != nil {
		t.Fatal(e)
	}
	fresh.Ref, fresh.Issuer, fresh.Status, fresh.UsePoolId = nil, nil, "", ""
	fresh.SemanticVersion = 0
	r, e := f.h.Grants.Configure(f.ctx, f.caller, &v1.ConfigureGrantCommand{Header: header("driver-remedy-grant"), Grant: fresh})
	accepted(t, r, e)
	approvalFixture.grant = r.ResultRef
	x.policy.Actions[0].GrantRef = r.ResultRef
	x.policy.Actions[0].ConfirmationRef = approveContinuationAction(t, approvalFixture, remedy.ProposalRef, "driver-remedy")
	x.configure(t, "driver-remedy-authority", d.Ref)
	d = x.advance(t)
	remedialAdmission, e := f.h.Tasks.QueryAdmission(f.ctx, f.caller, d.AdmissionRef)
	if e != nil || remedialAdmission == nil || proto.Equal(remedialAdmission.OperationId, firstAdmission.OperationId) || proto.Equal(remedialAdmission.GrantUseRef, firstAdmission.GrantUseRef) || proto.Equal(remedialAdmission.BudgetBasis.ReservationRef, firstAdmission.BudgetBasis.ReservationRef) || !proto.Equal(remedialAdmission.GrantRefs[0], approvalFixture.grant) {
		t.Fatalf("remedy reused old authority or operation: %v %v", remedialAdmission, e)
	}
	x.target.Target.SetBehavior("normal")
	d = x.advance(t)
	lastRequest := d.RequestRef
	x.advance(t)
	d = x.advance(t)
	if d.State != "COMPLETED" || proto.Equal(lastRequest, req.Ref) {
		t.Fatalf("real remedy did not complete: %v", d)
	}
	result, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	if e != nil || result == nil || result.Outcome != "SUCCEEDED" || len(result.OperationRefs) != 6 || result.Conditions[0].Conclusion != "SATISFIED" || !proto.Equal(result.Conditions[0].OperationRef.Name, remedialAdmission.OperationId) {
		t.Fatalf("real remedy result: %v %v", result, e)
	}
	currentFirst, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, firstAdmission.OperationId)
	if e != nil || !proto.Equal(currentFirst.Execution, firstOperation.Execution) || !proto.Equal(currentFirst.Effect, firstOperation.Effect) || !proto.Equal(currentFirst.CapabilitySnapshot, firstOperation.CapabilitySnapshot) || currentFirst.Dispatch != firstOperation.Dispatch || currentFirst.Lifecycle != firstOperation.Lifecycle {
		t.Fatalf("remedy rewrote original failed operation: %v %v", currentFirst, e)
	}
	remedialOperation, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, remedialAdmission.OperationId)
	if e != nil || proto.Equal(remedialOperation.Execution.Attempt.Ref.Name, firstOperation.Execution.Attempt.Ref.Name) || remedialOperation.Execution.Attempt.ExternalKey == firstOperation.Execution.Attempt.ExternalKey || proto.Equal(remedialOperation.Execution.Send.Ref, firstOperation.Execution.Send.Ref) {
		t.Fatalf("remedy reused original physical identity: %v %v", remedialOperation, e)
	}
	for _, requestRef := range []*v1.Ref{firstRequest, failedComplete.RequestRef, req.Ref, lastRequest} {
		request := mustDriverRequest(t, f, requestRef)
		assertDriverReceivedSnapshot(t, x, request)
		call, e := f.h.Tasks.QueryModelCall(f.ctx, f.caller, requestRef, 0)
		if e != nil || call == nil || call.InputRef == nil || len(request.ModelOperationRefs) != 1 || len(request.OutcomeRefs) != 1 {
			t.Fatalf("original request/position: %v %v", request, e)
		}
		if extra, e := f.h.Tasks.QueryModelCall(f.ctx, f.caller, requestRef, 1); e != nil || extra != nil {
			t.Fatalf("extra model position: %v %v", extra, e)
		}
	}
	fixed, e := proto.Marshal(result)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	advance := writeReasonerCLIJSON(t, "remedy-advance.json", &v1.AdvanceReasonerTaskRequest{TaskId: f.task.Name, Limit: 16})
	for range 2 {
		state := new(v1.ReasonerDriver)
		if e = protojson.Unmarshal(cancellationDriverCLI(t, f, x.binary, "advance-task", "--json", advance), state); e != nil || !proto.Equal(state, d) {
			t.Fatalf("actual CLI resampled completed remedy: %v %v", state, e)
		}
	}
	f.h, e = assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	again, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	later, e := proto.Marshal(again)
	if e != nil || !bytes.Equal(fixed, later) {
		t.Fatal("completed Result changed on restart")
	}
	old, e := f.h.Tasks.QueryVerification(f.ctx, f.caller, v.Ref)
	if e != nil {
		t.Fatal(e)
	}
	oldBytes, e := proto.Marshal(old)
	if e != nil || !bytes.Equal(rejectedBytes, oldBytes) || !proto.Equal(old.ContinuationRequestRef, req.Ref) {
		t.Fatal("original rejected Verification changed")
	}
	budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	requests, effects := x.target.Target.Snapshot()
	if e != nil || budget.Settled != 34 || budget.Reserved != 0 || x.provider.Calls() != 4 || len(x.provider.Bills()) != 4 || len(requests) != 2 || len(effects) != 1 || len(x.target.Bills()) != 2 {
		t.Fatalf("actual remedy counts or fees: %v %v models=%d target=%d effects=%d", budget, e, x.provider.Calls(), len(requests), len(effects))
	}
	assertTaskOwnerSource(t, f, "VERIFICATION_CHANGED", v.Ref, req.Ref)
	t.Logf("actual remedy: MODEL4/bills4; target requests2/effects1/bills2; settled34/reserved0; immutable rejected continuation=%s", req.Ref.Name.LocalId)
}

func assertDriverReceivedSnapshot(t *testing.T, x *rejectionDriver, request *v1.ProposalRequest) {
	t.Helper()
	saved, e := x.f.h.Tasks.QuerySnapshot(x.f.ctx, x.f.caller, request.SnapshotRef)
	if e != nil {
		t.Fatal(e)
	}
	for _, body := range x.provider.Bodies() {
		var view struct {
			Facts struct {
				Facts []json.RawMessage `json:"facts"`
			} `json:"facts"`
		}
		if e = json.Unmarshal(body, &view); e != nil || len(view.Facts.Facts) == 0 {
			t.Fatalf("actual provider body: %v", e)
		}
		received := new(v1.ContextSnapshot)
		if e = protojson.Unmarshal(view.Facts.Facts[0], received); e != nil {
			t.Fatal(e)
		}
		if proto.Equal(received.RequestRef, request.Ref) {
			if !proto.Equal(received, saved) {
				t.Fatalf("provider did not receive exact immutable owner snapshot: got=%v saved=%v", received, saved)
			}
			for _, progress := range received.ProgressFacts {
				admission, err := x.f.h.Tasks.QueryAdmission(x.f.ctx, x.f.caller, progress.AdmissionRef)
				if err != nil {
					t.Fatal(err)
				}
				if admission.ModelDescriptorDigest != "" && slices.ContainsFunc(received.ContentRefs, func(ref *v1.Ref) bool { return proto.Equal(ref, admission.ParametersRef) }) {
					t.Fatal("next round recursively embedded prior model prompt")
				}
			}
			return
		}
	}
	t.Fatalf("original provider request not received: %v", request.Ref)
}

type rejectionDriver struct {
	f                             *fixture
	provider                      *simulator.ModelProvider
	target                        *simulator.BillingTarget
	targetCapability, targetGrant *v1.Ref
	policy                        *v1.ReasonerDriverPolicy
	binary                        string
}

// 规则：G2、G3、G4、G11、准入-5、开始-2、完成-4
// 本切片用公开完成生产者制造真实 P4 读失败；上例另证实完整默认 MODEL 的真实 UNSAT 链。
func TestProductionDriverWaitsForRejectedCompletionOwnerBeforeAdopting(t *testing.T) {
	x := rejectionDriverFixtureWithConfirmation(t, false)
	f := x.f
	producer := &fixture{path: f.path, h: f.h, ctx: f.ctx, caller: f.caller, task: f.task, parameters: f.parameters, capability: x.targetCapability, grant: x.targetGrant}
	a, start := prepareDriverOwnerAction(t, producer)
	actor := &v1.Caller{UserId: "u", IssuerId: "egress"}
	r, e := f.h.Egress.Invoke(f.ctx, actor, start)
	accepted(t, r, e)
	producer.parameters, e = f.h.Content.Stage(f.ctx, f.caller, &v1.SubmitGoalCommand{Identity: header("driver-owner-other-action").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: `{"destination":"appendix"}`})
	if e != nil {
		t.Fatal(e)
	}
	producer.suffix = "owner-p4"
	b, second := prepareDriverOwnerAction(t, producer)
	r, e = f.h.Tasks.StartExecution(f.ctx, actor, second)
	accepted(t, r, e)
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, b.OperationId)
	if e != nil || op.StartReceiptObtained || op.Execution == nil || op.Effect.Outcome != "NOT_APPLIED" {
		t.Fatalf("not actual original P4 gap: %v %v", op, e)
	}
	p := completeProposal(t, producer, []*v1.CompletionEvidence{{ConditionId: "created", OperationId: a.OperationId}})
	d := x.advance(t)
	oldRequest := d.RequestRef
	acceptedProposal, e := f.h.Tasks.ReadProposal(f.ctx, f.caller, p)
	if e != nil || !proto.Equal(acceptedProposal.RequestRef, oldRequest) || d.OutcomeRef != nil {
		t.Fatalf("driver did not own actual public COMPLETE request before verification: driver=%v proposal=%v %v", d, acceptedProposal, e)
	}
	t.Logf("actual public COMPLETE consumer before Begin: driver request=%s, accepted proposal=%s; MODEL outcome absent (no provider call)", oldRequest.Name.LocalId, p.Name.LocalId)
	r, e = f.h.Tasks.BeginCompletion(f.ctx, f.caller, &v1.BeginCompletionCommand{Header: header("driver-owner-begin"), TaskId: f.task.Name, ProposalRef: p})
	accepted(t, r, e)
	f.h.Ledger.WithStarts(&failedStartFacts{StartFacts: f.h.Tasks, mode: "unavailable"})
	r, e = f.h.Tasks.RecheckCompletion(f.ctx, f.caller, &v1.RecheckCompletionCommand{Header: header("driver-owner-recheck"), VerificationRef: r.ResultRef})
	accepted(t, r, e)
	planning, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	v, e := f.h.Tasks.QueryVerification(f.ctx, f.caller, planning.VerificationRef)
	task, te := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil || te != nil || v.Status != "REJECTED" || v.ContinuationRequestRef != nil || planning.VerificationFreeze != 0 || !slices.Contains(task.WaitingOn, "COMPLETION:BLOCKER_FACTS_UNAVAILABLE") {
		t.Fatalf("actual rejected owner read failure: v=%v task=%v %v %v", v, task, e, te)
	}
	immutable, e := proto.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	for range 2 {
		d = x.advance(t)
		if !proto.Equal(d.RequestRef, oldRequest) || x.provider.Calls() != 0 || len(x.provider.Bills()) != 0 {
			t.Fatalf("unavailable original owner replaced request or sampled: %v", d)
		}
	}
	f.h.Ledger.WithStarts(f.h.Tasks)
	// 原封闭交付可以独立完成；这一步不运行继续请求负责方。
	if e = f.h.Tasks.ProcessCompletionClosures(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	closedP4, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, b.OperationId)
	if e != nil || closedP4.Dispatch != "SEALED" || closedP4.Lifecycle != "SETTLED" || closedP4.Execution.Send.Phase != "CLOSED" || closedP4.Execution.Send.ObservationRef != nil || closedP4.Effect.Outcome != "NOT_APPLIED" || closedP4.Effect.LateEffect != "RULED_OUT" {
		t.Fatalf("actual independent original no-send closure: %v %v", closedP4, e)
	}
	// 故意让真实 driver 消费者先于公开完成恢复运行，不能依据完成自己增加的控制代次另建请求。
	d = x.advance(t)
	planning, e = f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	current, e := f.h.Tasks.QueryVerification(f.ctx, f.caller, planning.VerificationRef)
	if e != nil {
		t.Fatal(e)
	}
	if !proto.Equal(d.RequestRef, oldRequest) && (current.ContinuationRequestRef == nil || !proto.Equal(d.RequestRef, current.ContinuationRequestRef)) {
		t.Fatalf("driver created an unlinked substitute before owner recovery: driver=%v verification=%v", d, current)
	}
	if e = f.h.Tasks.ProcessCompletions(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	planning, e = f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	current, e = f.h.Tasks.QueryVerification(f.ctx, f.caller, planning.VerificationRef)
	if e != nil || current.ContinuationRequestRef == nil || !proto.Equal(current.ContinuationRequestRef, planning.Snapshot.RequestRef) {
		t.Fatalf("original owner continuation missing: %v %v", current, e)
	}
	req := mustDriverRequest(t, f, current.ContinuationRequestRef)
	if !proto.Equal(req.SnapshotRef, planning.Snapshot.Ref) {
		t.Fatalf("owner snapshot replaced: %v", req)
	}
	d = x.advance(t)
	if !proto.Equal(d.RequestRef, req.Ref) {
		t.Fatalf("did not adopt exact owner request: %v", d)
	}
	old, e := f.h.Tasks.QueryVerification(f.ctx, f.caller, v.Ref)
	if e != nil {
		t.Fatal(e)
	}
	oldBytes, e := proto.Marshal(old)
	if e != nil || !bytes.Equal(immutable, oldBytes) || old.ContinuationRequestRef != nil {
		t.Fatal("immutable unavailable owner version changed")
	}
	for range 2 {
		x.advance(t)
	}
	request := mustDriverRequest(t, f, req.Ref)
	if x.provider.Calls() != 1 || len(x.provider.Bills()) != 1 || len(request.ModelOperationRefs) != 1 || len(request.OutcomeRefs) != 1 {
		t.Fatalf("owner consumer sampled more than one position: %v models=%d", request, x.provider.Calls())
	}
	if extra, e := f.h.Tasks.QueryModelCall(f.ctx, f.caller, req.Ref, 1); e != nil || extra != nil {
		t.Fatalf("extra owner model position: %v %v", extra, e)
	}
	op, e = f.h.Ledger.QueryOperation(f.ctx, f.caller, b.OperationId)
	if e != nil || op.Dispatch != "SEALED" || op.Lifecycle != "SETTLED" {
		t.Fatalf("old P4 reopened: %v %v", op, e)
	}
	r, e = f.h.Egress.Invoke(f.ctx, actor, second)
	if e == nil && r.GetDecision() == v1.Decision_DECISION_ACCEPTED {
		t.Fatal("old P4 was sent")
	}
	requests, effects := x.target.Target.Snapshot()
	if len(requests) != 1 || len(effects) != 0 || len(x.target.Bills()) != 1 {
		t.Fatalf("owner recovery repeated original action: requests%d effects%d bills%d", len(requests), len(effects), len(x.target.Bills()))
	}
	t.Log("actual owner read failure/restoration: exact Verification-linked request, MODEL1/bill1, original target1/effect0/bill1, old P4 sealed without send")
}

func rejectionDriverFixture(t *testing.T) *rejectionDriver {
	return rejectionDriverFixtureWithConfirmation(t, true)
}

func prepareDriverOwnerAction(t *testing.T, f *fixture) (*v1.Admission, *v1.StartExecutionCommand) {
	t.Helper()
	capability, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
	if e != nil {
		t.Fatal(e)
	}
	proposal := f.propose(t, func(p *v1.Proposal) { p.Step.SchemaDigest = capability.SchemaDigest })
	r, e := f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("owner-admit" + f.suffix), TaskId: f.task.Name, ProposalRef: proposal, GrantRef: f.grant})
	accepted(t, r, e)
	return prepareAdmittedStart(t, f, r.ResultRef, 30000)
}

func rejectionDriverFixtureWithConfirmation(t *testing.T, confirmation bool) *rejectionDriver {
	t.Helper()
	x := &rejectionDriver{target: simulator.NewBillingTarget(3), provider: simulator.NewModelProvider()}
	x.target.Target.SetBehavior("reject")
	x.f = newFixtureWithTarget(t, 200, 200, confirmation, x.target)
	f := x.f
	parameters, e := f.h.Content.Stage(f.ctx, f.caller, &v1.SubmitGoalCommand{Identity: header("rejection-driver-parameters").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: `{"destination":"archive"}`})
	if e != nil {
		t.Fatal(e)
	}
	f.parameters = parameters
	capability, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
	if e != nil {
		t.Fatal(e)
	}
	capability.Ref, capability.ApprovedBy = nil, nil
	capability.ParameterSchemaJson = []byte(`{"type":"object","properties":{"destination":{"type":"string"}},"required":["destination"],"additionalProperties":false}`)
	r, e := f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("rejection-target-cap"), Capability: capability})
	accepted(t, r, e)
	f.capability = r.ResultRef
	capability, e = f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
	if e != nil {
		t.Fatal(e)
	}
	x.targetCapability, x.targetGrant = f.capability, f.grant
	scopeRequirement(t, f)
	action := &v1.Proposal{Kind: "ACTION", Step: &v1.ActionStep{StepId: "create", CapabilityRef: f.capability, SchemaDigest: capability.SchemaDigest, ParametersRef: parameters}, BasisRefs: []*v1.Ref{parameters}}
	var mu sync.Mutex
	var failedComplete *v1.ContextSnapshot
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		body, e := io.ReadAll(r.Body)
		var view struct {
			Facts struct {
				Facts []json.RawMessage `json:"facts"`
			} `json:"facts"`
		}
		if e != nil || json.Unmarshal(body, &view) != nil || len(view.Facts.Facts) == 0 {
			t.Error("no actual serialized model snapshot")
			http.Error(w, "snapshot", 500)
			return
		}
		snap := new(v1.ContextSnapshot)
		if e = protojson.Unmarshal(view.Facts.Facts[0], snap); e != nil || !proto.Equal(snap.TaskRef.GetName(), f.task.Name) || snap.RequestRef == nil || snap.RequirementsRef == nil {
			t.Error("model snapshot identity", e)
			http.Error(w, "snapshot", 500)
			return
		}
		output := action
		for _, fact := range snap.ProgressFacts {
			if !proto.Equal(fact.CapabilityRef, x.targetCapability) || fact.EvidenceConflict || fact.ExecutionReportPending {
				continue
			}
			if fact.EffectOutcome == "APPLIED" {
				output = &v1.Proposal{Kind: "COMPLETE", Verdicts: []*v1.ConditionVerdict{{ConditionId: "created", Conclusion: "SATISFIED", OperationId: fact.OperationRef.Name, EvidenceRefs: fact.EvidenceRefs}}, BasisRefs: fact.EvidenceRefs}
				break
			}
			if fact.EffectOutcome == "NOT_APPLIED" {
				if failedComplete == nil {
					failedComplete = proto.Clone(snap).(*v1.ContextSnapshot)
				}
				if snap.ControlGeneration > failedComplete.ControlGeneration && snap.PlanningGeneration > failedComplete.PlanningGeneration {
					output = action
					continue
				}
				output = &v1.Proposal{Kind: "COMPLETE", Verdicts: []*v1.ConditionVerdict{{ConditionId: "created", Conclusion: "UNSATISFIED", OperationId: fact.OperationRef.Name, EvidenceRefs: fact.EvidenceRefs, Gaps: []string{"target reliably rejected original create"}}}, BasisRefs: fact.EvidenceRefs}
			}
		}
		encoded, e := protojson.Marshal(output)
		if e != nil {
			t.Error(e)
			http.Error(w, "output", 500)
			return
		}
		x.provider.Output = string(encoded)
		r.Body = io.NopCloser(bytes.NewReader(body))
		x.provider.ServeHTTP(w, r)
	}))
	t.Cleanup(endpoint.Close)
	capability.Ref, capability.ApprovedBy = nil, nil
	capability.Action, capability.Resource = "MODEL_INFER", endpoint.URL
	capability.AdapterRef.Name.LocalId = "model-reference-v1"
	capability.ParameterSchemaJson, capability.SchemaDigest = nil, ""
	r, e = f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("rejection-model-cap"), Capability: capability})
	accepted(t, r, e)
	f.capability = r.ResultRef
	grant, e := f.h.Grants.QueryGrant(f.ctx, f.caller, f.grant)
	if e != nil {
		t.Fatal(e)
	}
	grant.Ref, grant.Issuer, grant.Status = nil, nil, ""
	grant.UsePoolId = "rejection-driver-model"
	grant.ConfirmationRequired = false
	for _, permission := range grant.Permissions {
		permission.Action, permission.Resource = "MODEL_INFER", endpoint.URL
	}
	r, e = f.h.Grants.Configure(f.ctx, f.caller, &v1.ConfigureGrantCommand{Header: header("rejection-model-grant"), Grant: grant})
	accepted(t, r, e)
	f.grant = r.ResultRef
	x.policy = driverPolicy(f)
	x.policy.Actions = []*v1.ReasonerActionAuthority{{CapabilityRef: x.targetCapability, GrantRef: x.targetGrant}}
	x.binary = filepath.Join(t.TempDir(), "lerna")
	if out, e := exec.Command("go", "build", "-o", x.binary, "../../cmd/lerna").CombinedOutput(); e != nil {
		t.Fatalf("build: %v %s", e, out)
	}
	configuration := writeReasonerCLIJSON(t, "rejection-driver.json", &v1.ConfigureReasonerDriverCommand{Header: header("rejection-driver-config"), TaskId: f.task.Name, Policy: x.policy})
	configured := new(v1.CommandReceipt)
	if e = protojson.Unmarshal(cancellationDriverCLI(t, f, x.binary, "configure-reasoner", "--json", configuration), configured); e != nil {
		t.Fatal(e)
	}
	accepted(t, configured, nil)
	return x
}

func (x *rejectionDriver) advance(t *testing.T) *v1.ReasonerDriver {
	t.Helper()
	d, e := x.f.h.Tasks.AdvanceReasonerTask(x.f.ctx, x.f.caller, &v1.AdvanceReasonerTaskRequest{TaskId: x.f.task.Name, Limit: 1})
	if e != nil {
		t.Fatal(e)
	}
	return d
}

func (x *rejectionDriver) configure(t *testing.T, id string, replaces *v1.Ref) {
	t.Helper()
	r, e := x.f.h.Tasks.ConfigureReasonerDriver(x.f.ctx, x.f.caller, &v1.ConfigureReasonerDriverCommand{Header: header(id), TaskId: x.f.task.Name, Replaces: replaces, Policy: x.policy})
	accepted(t, r, e)
}

func mustDriverRequest(t *testing.T, f *fixture, ref *v1.Ref) *v1.ProposalRequest {
	t.Helper()
	r, e := f.h.Tasks.QueryProposalRequest(f.ctx, f.caller, ref)
	if e != nil || r == nil {
		t.Fatalf("original request: %v %v", r, e)
	}
	return r
}

func mustDriverOutcome(t *testing.T, f *fixture, ref *v1.Ref) *v1.ProposalOutcome {
	t.Helper()
	r, e := f.h.Tasks.QueryProposalOutcome(f.ctx, f.caller, ref)
	if e != nil || r == nil {
		t.Fatalf("original outcome: %v %v", r, e)
	}
	return r
}
