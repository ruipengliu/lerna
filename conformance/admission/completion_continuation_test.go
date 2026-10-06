package admission_test

import (
	"bytes"
	"slices"
	"testing"

	"context"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/ledger"
	"github.com/ruipengliu/lerna/defaults/scripted"
	"google.golang.org/protobuf/proto"
)

// 规则：G2、G3、G11、完成-4、准入-5
func TestCompletionRejectionDurablyRequestsOneContinuation(t *testing.T) {
	target := simulator.New("idempotent")
	target.SetBehavior("reject")
	f := newFixtureWithTarget(t, 100, 80, false, target)
	scopeRequirement(t, f)
	a, c := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
	accepted(t, r, e)
	p := completeProposal(t, f, []*v1.CompletionEvidence{{ConditionId: "created", OperationId: a.OperationId}})
	before, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	r, e = f.h.Tasks.BeginCompletion(f.ctx, f.caller, &v1.BeginCompletionCommand{Header: header("continue-begin"), TaskId: f.task.Name, ProposalRef: p})
	accepted(t, r, e)
	if e = f.h.Tasks.ProcessCompletions(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	next, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
	if e != nil || next.Snapshot == nil || proto.Equal(next.Snapshot.RequestRef, before.Snapshot.RequestRef) || next.Snapshot.PlanningGeneration != before.Snapshot.PlanningGeneration+1 || next.VerificationFreeze != 0 || next.Proposal != nil {
		t.Fatalf("rejection did not create a new durable request: %v %v", next, e)
	}
	v, e := f.h.Tasks.QueryVerification(f.ctx, f.caller, next.VerificationRef)
	if e != nil || v.Status != "REJECTED" || len(v.Gaps) == 0 || len(v.AdmissionRefs) != 1 || !proto.Equal(v.ContinuationRequestRef, next.Snapshot.RequestRef) {
		t.Fatalf("rejection facts %v %v", v, e)
	}
	req, e := f.h.Tasks.QueryProposalRequest(f.ctx, f.caller, next.Snapshot.RequestRef)
	if e != nil || req == nil || req.State != "PENDING" || req.Purpose != "PLAN" || !proto.Equal(req.SnapshotRef, next.Snapshot.Ref) {
		t.Fatalf("continuation responsibility %v %v", req, e)
	}
	job, e := f.h.Durable.QueryJob(f.ctx, f.caller, req.JobRef.Name)
	if e != nil || job == nil || job.State != "READY" || job.JobType != "PROPOSE" || !proto.Equal(job.SpecificationRef, req.Ref) {
		t.Fatalf("continuation work %v %v", job, e)
	}
	for range 2 {
		if e = f.h.Tasks.ProcessCompletions(f.ctx, f.caller); e != nil {
			t.Fatal(e)
		}
	}
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	f.h, e = assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	again, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
	if e != nil || !proto.Equal(again.Snapshot, next.Snapshot) || !proto.Equal(again.VerificationRef, next.VerificationRef) {
		t.Fatalf("restart repeated continuation %v %v", again, e)
	}
	assertTaskOwnerSource(t, f, "VERIFICATION_CHANGED", v.Ref, v.ContinuationRequestRef)
	assertTaskOwnerSource(t, f, "PROPOSAL_REQUESTED", next.Snapshot.Ref, req.Ref)
	requests, effects := target.Snapshot()
	if len(requests) != 1 || len(effects) != 0 {
		t.Fatalf("continuation resent old action: calls=%d effects=%d", len(requests), len(effects))
	}
}

// 规则：G1、G2、G4、准入-5、开始-2
func TestCompletionRejectionWaitsForP4ReceiptNotYetSeenByLedger(t *testing.T) {
	for _, mode := range []string{"available", "unavailable", "incompatible"} {
		t.Run(mode, func(t *testing.T) {
			target := simulator.New("idempotent")
			target.SetBehavior("reject")
			f := newFixtureWithTarget(t, 200, 200, false, target)
			scopeRequirement(t, f)
			a, c := prepareStart(t, f)
			actor := &v1.Caller{UserId: "u", IssuerId: "egress"}
			r, e := f.h.Egress.Invoke(f.ctx, actor, c)
			accepted(t, r, e)
			f.parameters, e = f.h.Content.Stage(f.ctx, f.caller, &v1.SubmitGoalCommand{Identity: header("other-action-body").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "an independent appendix"})
			if e != nil {
				t.Fatal(e)
			}
			f.suffix = "old-p4"
			b, second := prepareStart(t, f)
			r, e = f.h.Tasks.StartExecution(f.ctx, actor, second)
			accepted(t, r, e)
			op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, b.OperationId)
			if e != nil || op.StartReceiptObtained || op.Effect.Outcome != "NOT_APPLIED" {
				t.Fatalf("not the P4/P5 gap: %v %v", op, e)
			}
			p := completeProposal(t, f, []*v1.CompletionEvidence{{ConditionId: "created", OperationId: a.OperationId}})
			r, e = f.h.Tasks.BeginCompletion(f.ctx, f.caller, &v1.BeginCompletionCommand{Header: header("p4-reject-begin"), TaskId: f.task.Name, ProposalRef: p})
			accepted(t, r, e)
			if mode != "available" {
				f.h.Ledger.WithStarts(&failedStartFacts{StartFacts: f.h.Tasks, mode: mode})
			}
			r, e = f.h.Tasks.RecheckCompletion(f.ctx, f.caller, &v1.RecheckCompletionCommand{Header: header("p4-reject"), VerificationRef: r.ResultRef})
			accepted(t, r, e)
			state, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
			if e != nil {
				t.Fatal(e)
			}
			v, e := f.h.Tasks.QueryVerification(f.ctx, f.caller, state.VerificationRef)
			if e != nil || v.Status != "REJECTED" || v.ContinuationRequestRef != nil || state.VerificationFreeze != 0 {
				t.Fatalf("P4 action allowed substitute request %v %v", v, e)
			}
			if mode != "available" {
				task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
				if e != nil || !slices.Contains(task.GetWaitingOn(), "COMPLETION:BLOCKER_FACTS_UNAVAILABLE") {
					t.Fatalf("unreadable original facts lost explicit wait %v %v", task, e)
				}
			}
			f.h.Ledger.WithStarts(f.h.Tasks)
			if e = f.h.Tasks.ProcessCompletions(f.ctx, f.caller); e != nil {
				t.Fatal(e)
			}
			state, e = f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
			if e != nil {
				t.Fatal(e)
			}
			v, e = f.h.Tasks.QueryVerification(f.ctx, f.caller, state.VerificationRef)
			if e != nil || v.ContinuationRequestRef == nil || !proto.Equal(v.ContinuationRequestRef, state.Snapshot.RequestRef) {
				t.Fatalf("sealed blocker stranded continuation %v %v", v, e)
			}
			op, e = f.h.Ledger.QueryOperation(f.ctx, f.caller, b.OperationId)
			if e != nil || op.Dispatch != "SEALED" || op.Lifecycle != "SETTLED" {
				t.Fatalf("old operation reopened %v %v", op, e)
			}
			r, e = f.h.Egress.Invoke(f.ctx, actor, second)
			if e == nil && r.GetDecision() == v1.Decision_DECISION_ACCEPTED {
				t.Fatalf("old P4 sent after closure %v", r)
			}
			requests, effects := target.Snapshot()
			if len(requests) != 1 || len(effects) != 0 {
				t.Fatalf("old action sent: calls=%d effects=%d", len(requests), len(effects))
			}
		})
	}
}

// 规则：G2、G3、G4、G10、G11、准入-5、准入-9、完成-4、完成-7
func TestRejectedContinuationUsesFreshAuthorityAndCompletesWithNewAction(t *testing.T) {
	target := simulator.New("idempotent")
	target.SetBehavior("reject")
	f := newFixtureWithTarget(t, 200, 200, true, target)
	scopeRequirement(t, f)
	first := f.propose(t, nil)
	oldApproval := approveContinuationAction(t, f, first, "old")
	r, e := f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("old-confirmed-admit"), TaskId: f.task.Name, ProposalRef: first, GrantRef: f.grant, ConfirmationRef: oldApproval})
	accepted(t, r, e)
	a, e := f.h.Tasks.QueryAdmission(f.ctx, f.caller, r.ResultRef)
	if e != nil {
		t.Fatal(e)
	}
	oldStart := prepareExistingAdmissionStart(t, f, a, 30000)
	r, e = f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, oldStart)
	accepted(t, r, e)
	p := completeProposal(t, f, []*v1.CompletionEvidence{{ConditionId: "created", OperationId: a.OperationId}})
	r, e = f.h.Tasks.BeginCompletion(f.ctx, f.caller, &v1.BeginCompletionCommand{Header: header("old-completion"), TaskId: f.task.Name, ProposalRef: p})
	accepted(t, r, e)
	if e = f.h.Tasks.ProcessCompletions(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	state, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	old, e := f.h.Tasks.QueryVerification(f.ctx, f.caller, state.VerificationRef)
	if e != nil || old.Status != "REJECTED" || !proto.Equal(old.ContinuationRequestRef, state.Snapshot.RequestRef) {
		t.Fatalf("continuation %v %v", old, e)
	}
	if len(state.Snapshot.ProgressFacts) != 1 || !proto.Equal(state.Snapshot.ProgressFacts[0].AdmissionRef, a.Ref) || state.Snapshot.ProgressFacts[0].EffectOutcome != "NOT_APPLIED" {
		t.Fatalf("old actions omitted: %v", state.Snapshot)
	}
	fresh, e := f.h.Grants.QueryGrant(f.ctx, f.caller, f.grant)
	if e != nil {
		t.Fatal(e)
	}
	fresh.Ref, fresh.Issuer, fresh.Status, fresh.UsePoolId = nil, nil, "", ""
	fresh.SemanticVersion = 0
	r, e = f.h.Grants.Configure(f.ctx, f.caller, &v1.ConfigureGrantCommand{Header: header("remedial-grant"), Grant: fresh})
	accepted(t, r, e)
	f.grant = r.ResultRef
	proposal, e := (&scripted.Reasoner{Step: &v1.ActionStep{StepId: "remedy", CapabilityRef: f.capability, ParametersRef: f.parameters}}).Propose(f.ctx, state.Snapshot)
	if e != nil {
		t.Fatal(e)
	}
	r, e = f.h.Tasks.ReceiveProposal(f.ctx, f.caller, &v1.ReceiveProposalCommand{Header: header("remedial-proposal"), Proposal: proposal})
	accepted(t, r, e)
	newProposal := r.ResultRef
	r, e = f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("recycle-old-approval"), TaskId: f.task.Name, ProposalRef: newProposal, GrantRef: f.grant, ConfirmationRef: oldApproval})
	if e != nil || r.GetError().GetCode() != "CONFIRMATION_INVALID" {
		t.Fatalf("consumed approval reused: %v %v", r, e)
	}
	approval := approveContinuationAction(t, f, newProposal, "remedial")
	r, e = f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("remedial-admit"), TaskId: f.task.Name, ProposalRef: newProposal, GrantRef: f.grant, ConfirmationRef: approval})
	accepted(t, r, e)
	b, e := f.h.Tasks.QueryAdmission(f.ctx, f.caller, r.ResultRef)
	if e != nil || proto.Equal(a.OperationId, b.OperationId) || proto.Equal(a.GrantUseRef, b.GrantUseRef) || proto.Equal(a.BudgetBasis.ReservationRef, b.BudgetBasis.ReservationRef) || b.ControlGeneration != old.ControlGeneration {
		t.Fatalf("old authority reused %v %v", b, e)
	}
	f.suffix = "remedial"
	newStart := prepareExistingAdmissionStart(t, f, b, 30000)
	if proto.Equal(oldStart.CredentialRef, newStart.CredentialRef) || proto.Equal(oldStart.Binding, newStart.Binding) {
		t.Fatal("old P4 authority reused")
	}
	target.SetBehavior("normal")
	r, e = f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, newStart)
	accepted(t, r, e)
	p = completeProposal(t, f, []*v1.CompletionEvidence{{ConditionId: "created", OperationId: b.OperationId}})
	r, e = f.h.Tasks.BeginCompletion(f.ctx, f.caller, &v1.BeginCompletionCommand{Header: header("remedial-completion"), TaskId: f.task.Name, ProposalRef: p})
	accepted(t, r, e)
	newRound := r.ResultRef
	r, e = f.h.Tasks.RecheckCompletion(f.ctx, f.caller, &v1.RecheckCompletionCommand{Header: header("late-rejected-round"), VerificationRef: old.Ref})
	if e != nil || r.GetError().GetCode() != "STALE_VERIFICATION" {
		t.Fatalf("old round accepted %v %v", r, e)
	}
	state, e = f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
	if e != nil || !proto.Equal(state.VerificationRef, newRound) || state.VerificationFreeze != old.Round+1 {
		t.Fatalf("new freeze released %v %v", state, e)
	}
	if e = f.h.Tasks.ProcessCompletions(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	result, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	if e != nil || result == nil || result.Outcome != "SUCCEEDED" || len(result.OperationRefs) != 2 || result.Conditions[0].Conclusion != "SATISFIED" {
		t.Fatalf("remedial result %v %v", result, e)
	}
	fixed, e := proto.Marshal(result)
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
	_, _ = f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, oldStart)
	original, oe := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if oe != nil || original.Dispatch != "SEALED" || original.Lifecycle != "SETTLED" {
		t.Fatalf("original action reopened %v %v", original, oe)
	}
	again, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	later, e := proto.Marshal(again)
	if e != nil || !bytes.Equal(fixed, later) {
		t.Fatal("Result changed after restart")
	}
	requests, effects := target.Snapshot()
	if len(requests) != 2 || len(effects) != 1 {
		t.Fatalf("target calls=%d effects=%d", len(requests), len(effects))
	}
}

func approveContinuationAction(t *testing.T, f *fixture, proposal *v1.Ref, name string) *v1.Ref {
	t.Helper()
	goal, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("goal").Identity)
	if e != nil {
		t.Fatal(e)
	}
	r, e := f.h.Tasks.RequestAdmissionConfirmation(f.ctx, f.caller, &v1.RequestAdmissionConfirmationCommand{Header: header(name + "-confirmation"), TaskId: f.task.Name, ProposalRef: proposal, GrantRef: f.grant, SessionId: goal.Receipt.SessionRef.Name})
	accepted(t, r, e)
	pending, e := f.h.Sessions.QueryConfirmation(f.ctx, f.caller, r.ResultRef)
	if e != nil {
		t.Fatal(e)
	}
	r, e = f.h.Sessions.RespondConfirmation(f.ctx, f.caller, &v1.RespondConfirmationCommand{Header: header(name + "-approval"), ConfirmationRef: pending.Ref, BindingDigest: pending.BindingDigest, Decision: "APPROVE"})
	accepted(t, r, e)
	return r.ResultRef
}

// failedStartFacts 只注入原跨域读失败或不兼容回报，全部业务事实仍来自共享生产装配。
type failedStartFacts struct {
	ledger.StartFacts
	mode string
}

func (f *failedStartFacts) QueryStartReceipt(ctx context.Context, c *v1.Caller, id *v1.CommandIdentity) (*v1.ReceiptQuery, error) {
	q, e := f.StartFacts.QueryStartReceipt(ctx, c, id)
	if e != nil {
		return nil, e
	}
	if f.mode == "unavailable" {
		return &v1.ReceiptQuery{ResponsibleDomainId: q.ResponsibleDomainId, State: v1.ReceiptQueryState_RECEIPT_QUERY_STATE_UNAVAILABLE}, nil
	}
	q = proto.Clone(q).(*v1.ReceiptQuery)
	if q.Receipt != nil {
		q.Receipt.FingerprintVersion = 2
	}
	return q, nil
}
