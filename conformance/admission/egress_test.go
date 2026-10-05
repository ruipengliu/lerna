package admission_test

import (
	"testing"
	"time"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// 规则：G3、G4、G5、G11
func TestPreparePersistsOneAttemptWithoutSending(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	p := f.propose(t, nil)
	r, e := f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("admit"), TaskId: f.task.Name, ProposalRef: p, GrantRef: f.grant})
	accepted(t, r, e)
	a, e := f.h.Tasks.QueryAdmission(f.ctx, f.caller, r.ResultRef)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.h.Tasks.ProcessHandoffs(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	h := header("prepare")
	h.Identity.TargetDomainId = "d/ledger"
	claim, e := f.h.LedgerWork.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: ledgerHeader("claim").Identity, ContractVersion: 1, Action: "CLAIM", AllowedTypes: []string{"EXECUTE_OPERATION"}, Limit: 1, LeaseMs: 30000, ProcessInstance: "process-a"})
	accepted(t, claim, e)
	c := &v1.PrepareExecutionCommand{Claim: claim.Jobs[0], Header: h, OperationId: a.OperationId, ProcessInstance: "process-a"}
	r, e = f.h.Ledger.Prepare(f.ctx, f.caller, c)
	accepted(t, r, e)
	x, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil || x == nil || x.Attempt.Phase != "REGISTERED" || x.Attempt.ExternalKey == "" || x.Send.SendSeq != 1 || x.Send.Phase != "REGISTERED" || x.CallDescriptor.Target != a.CapabilitySnapshot.Resource {
		t.Fatalf("execution: %v %v", x, e)
	}
	again, e := f.h.Ledger.Prepare(f.ctx, f.caller, c)
	if e != nil || !proto.Equal(r, again) {
		t.Fatalf("replay %v %v", again, e)
	}
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || len(op.AttemptRefs) != 1 || op.Effect.Outcome != "NOT_APPLIED" || f.calls.Load() != 0 {
		t.Fatalf("operation %v %v calls=%d", op, e, f.calls.Load())
	}
}

// 规则：G3、G11
func TestExecutionRequiresCurrentLedgerClaim(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	p := f.propose(t, nil)
	r, e := f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("admit"), TaskId: f.task.Name, ProposalRef: p, GrantRef: f.grant})
	accepted(t, r, e)
	a, e := f.h.Tasks.QueryAdmission(f.ctx, f.caller, r.ResultRef)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.h.Tasks.ProcessHandoffs(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	h := header("unclaimed")
	h.Identity.TargetDomainId = "d/ledger"
	r, e = f.h.Ledger.Prepare(f.ctx, f.caller, &v1.PrepareExecutionCommand{Header: h, OperationId: a.OperationId, ProcessInstance: "unclaimed"})
	if e != nil || r.GetError().GetCode() != "STALE_CLAIM" {
		t.Fatalf("unclaimed execution: %v %v", r, e)
	}
	x, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil || x != nil || f.calls.Load() != 0 {
		t.Fatalf("leaked %v %v", x, e)
	}
}

func ledgerHeader(id string) *v1.CommandHeader {
	h := header(id)
	h.Identity.TargetDomainId = "d/ledger"
	return h
}

// 规则：G3、G11
func TestLedgerTakeoverFencesOldWorkerAndPreservesAttempt(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	p := f.propose(t, nil)
	r, e := f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("admit"), TaskId: f.task.Name, ProposalRef: p, GrantRef: f.grant})
	accepted(t, r, e)
	a, e := f.h.Tasks.QueryAdmission(f.ctx, f.caller, r.ResultRef)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.h.Tasks.ProcessHandoffs(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	old, e := f.h.LedgerWork.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: ledgerHeader("old-claim").Identity, ContractVersion: 1, Action: "CLAIM", AllowedTypes: []string{"EXECUTE_OPERATION"}, Limit: 1, LeaseMs: 100, ProcessInstance: "old-worker"})
	accepted(t, old, e)
	r, e = f.h.Ledger.Prepare(f.ctx, f.caller, &v1.PrepareExecutionCommand{Header: ledgerHeader("old-prepare"), OperationId: a.OperationId, ProcessInstance: "old-worker", Claim: old.Jobs[0]})
	accepted(t, r, e)
	original := r.ResultRef
	originalExecution, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	originalSend := originalExecution.Send.Ref
	time.Sleep(120 * time.Millisecond)
	fresh, e := f.h.LedgerWork.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: ledgerHeader("fresh-claim").Identity, ContractVersion: 1, Action: "CLAIM", AllowedTypes: []string{"EXECUTE_OPERATION"}, Limit: 1, LeaseMs: 30000, ProcessInstance: "fresh-worker"})
	accepted(t, fresh, e)
	r, e = f.h.Ledger.Prepare(f.ctx, f.caller, &v1.PrepareExecutionCommand{Header: ledgerHeader("stale-prepare"), OperationId: a.OperationId, ProcessInstance: "old-worker", Claim: old.Jobs[0]})
	if e != nil || r.GetError().GetCode() != "STALE_CLAIM" {
		t.Fatalf("old worker %v %v", r, e)
	}
	r, e = f.h.Ledger.Prepare(f.ctx, f.caller, &v1.PrepareExecutionCommand{Header: ledgerHeader("fresh-prepare"), OperationId: a.OperationId, ProcessInstance: "fresh-worker", Claim: fresh.Jobs[0]})
	accepted(t, r, e)
	historical, e := f.h.Ledger.QuerySend(f.ctx, f.caller, originalSend)
	if e != nil || historical.ProcessInstance != "old-worker" || historical.ClaimEpoch != 1 {
		t.Fatalf("rewritten original send: %v %v", historical, e)
	}
	x, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil || !proto.Equal(original, x.Attempt.Ref) || x.Send.ProcessInstance != "fresh-worker" || x.Send.ClaimEpoch != 2 || f.calls.Load() != 0 {
		t.Fatalf("takeover %v %v", x, e)
	}
}

// 规则：G3、G11、R3
func TestGenericJobCommandCannotAbandonExecutionResponsibility(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	p := f.propose(t, nil)
	r, e := f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("admit"), TaskId: f.task.Name, ProposalRef: p, GrantRef: f.grant})
	accepted(t, r, e)
	if e = f.h.Tasks.ProcessHandoffs(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	claim, e := f.h.LedgerWork.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: ledgerHeader("claim").Identity, ContractVersion: 1, Action: "CLAIM", AllowedTypes: []string{"EXECUTE_OPERATION"}, Limit: 1, LeaseMs: 30000, ProcessInstance: "worker"})
	accepted(t, claim, e)
	for _, action := range []string{"PROGRESS", "CONTROL"} {
		next := "COMPLETED"
		if action == "CONTROL" {
			next = "CLOSED"
		}
		job := claim.Jobs[0]
		r, e = f.h.LedgerWork.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: ledgerHeader(action).Identity, ContractVersion: 1, Action: action, Module: "ledger", JobRef: job.Ref, ClaimEpoch: job.ClaimEpoch, ProcessInstance: job.ProcessInstance, NextState: next})
		if e != nil || r.GetError().GetCode() != "UNSUPPORTED_FEATURE" {
			t.Fatalf("bypass %s: %v %v", action, r, e)
		}
	}
	pending, e := f.h.LedgerWork.Pending(f.ctx, f.caller)
	if e != nil || len(pending) != 1 || f.calls.Load() != 0 {
		t.Fatalf("lost responsibility %v %v", pending, e)
	}
}

// 规则：G1、G3、G4、G5
func TestOnlyNewDurableDispatchCanPerformPhysicalIO(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	a, c := prepareStart(t, f)
	caller := &v1.Caller{UserId: "u", IssuerId: "egress"}
	r, e := f.h.Egress.Invoke(f.ctx, caller, c)
	accepted(t, r, e)
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || op.Execution.Send.Phase != "OBSERVED" || op.Effect.Outcome != "UNKNOWN" || op.Effect.LateEffect != "MAY_OCCUR" || f.calls.Load() != 1 {
		t.Fatalf("dispatch %v %v calls=%d", op, e, f.calls.Load())
	}
	oldAttempt, e := f.h.Ledger.QueryAttempt(f.ctx, f.caller, &v1.Ref{Name: c.Binding.AttemptId, Revision: 1, SchemaId: "lerna.v1.ExecutionAttempt"})
	if e != nil || oldAttempt.Phase != "REGISTERED" {
		t.Fatalf("rewritten attempt %v %v", oldAttempt, e)
	}
	again, e := f.h.Egress.Invoke(f.ctx, caller, c)
	if e != nil || !proto.Equal(again, r) || f.calls.Load() != 1 {
		t.Fatalf("duplicate physicalIO %v %v calls=%d", again, e, f.calls.Load())
	}
}
