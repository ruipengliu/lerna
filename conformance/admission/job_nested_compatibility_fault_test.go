//go:build fault

package admission_test

import (
	"encoding/hex"
	"testing"

	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G3、G4、G7、G10、G11、R7、开始-1、V4
func TestHeldExecutionClaimRefusesNestedUnknownCurrentJobBeforeP4(t *testing.T) {
	target := simulator.NewBillingTarget(3)
	f := newFixtureWithTarget(t, 100, 80, false, target)
	a, start := prepareStart(t, f)
	oldStart := proto.Clone(start).(*v1.StartExecutionCommand)
	original, e := f.h.LedgerWork.QueryJob(f.ctx, f.caller, start.Claim.Ref.Name)
	if e != nil || original.GetState() != "CLAIMED" || original.JobType != "EXECUTE_OPERATION" || original.ClaimEpoch != 1 {
		t.Fatalf("real held execution job: %v %v", original, e)
	}
	opBefore, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || opBefore.GetExecution().GetSend().GetPhase() != "REGISTERED" {
		t.Fatalf("real prepared original execution: %v %v", opBefore, e)
	}
	budgetBefore, e := f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
	if e != nil {
		t.Fatal(e)
	}
	reservationsBefore, e := f.h.Budget.QueryReservations(f.ctx, f.caller, a.TaskId)
	if e != nil || len(reservationsBefore) != 1 || reservationsBefore[0].ConsumedSends != 0 {
		t.Fatalf("original unconsumed reservation: %v %v", reservationsBefore, e)
	}
	admitBefore, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("admit").Identity)
	if e != nil {
		t.Fatal(e)
	}
	claimBefore, e := f.h.LedgerWork.QueryReceipt(f.ctx, f.caller, ledgerHeader("claim").Identity)
	if e != nil {
		t.Fatal(e)
	}
	changed := proto.Clone(original).(*v1.Job)
	changed.Ref.ProtoReflect().SetUnknown(protowire.AppendVarint(protowire.AppendTag(nil, 19001, protowire.VarintType), 1))
	back := proto.Clone(changed).(*v1.Job)
	back.Ref.ProtoReflect().SetUnknown(nil)
	if !proto.Equal(back, original) || !proto.Equal(start, oldStart) {
		t.Fatal("metadata fault changed business identity or original held command")
	}
	b, e := proto.Marshal(changed)
	if e != nil {
		t.Fatal(e)
	}
	var roundtrip v1.Job
	if e = proto.Unmarshal(b, &roundtrip); e != nil || !proto.Equal(&roundtrip, changed) {
		t.Fatalf("nested unknown metadata lost in roundtrip: %v", e)
	}
	if e = f.h.StorageFaultSQL("UPDATE ledger_jobs SET record=X'" + hex.EncodeToString(b) + "' WHERE user_id='u' AND domain_id='d/ledger' AND id='" + original.Ref.Name.LocalId + "'"); e != nil {
		t.Fatal(e)
	}
	saved, e := f.h.LedgerWork.QueryJob(f.ctx, f.caller, original.Ref.Name)
	if e != nil || !proto.Equal(saved, changed) {
		t.Fatalf("actual current saved fault missing: %v %v", saved, e)
	}
	caller := &v1.Caller{UserId: "u", IssuerId: "egress"}
	r, e := f.h.Tasks.StartExecution(f.ctx, caller, start)
	if e != nil || r.GetDecision() != v1.Decision_DECISION_REJECTED || r.GetError().GetCode() != "UNSUPPORTED_FEATURE" {
		t.Fatalf("old clean claim crossed P4 for unsupported current job: %v %v", r, e)
	}
	// 出口重入仍返回原拒绝；独立目标没有请求、效果或账单。
	replay, e := f.h.Egress.Invoke(f.ctx, caller, oldStart)
	if e != nil || !proto.Equal(replay, r) {
		t.Fatalf("egress bypassed original refusal: %v %v", replay, e)
	}
	q, e := f.h.Durable.QueryReceipt(f.ctx, caller, start.Header.Identity)
	if e != nil || q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED || !proto.Equal(q.Receipt, r) {
		t.Fatalf("P4 refusal not durably recorded: %v %v", q, e)
	}
	after, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || !proto.Equal(after, opBefore) {
		t.Fatalf("original execution advanced: %v %v", after, e)
	}
	jobAfter, e := f.h.LedgerWork.QueryJob(f.ctx, f.caller, original.Ref.Name)
	if e != nil || !proto.Equal(jobAfter, changed) {
		t.Fatalf("original held job changed: %v %v", jobAfter, e)
	}
	admissionAfter, e := f.h.Tasks.QueryAdmission(f.ctx, f.caller, a.Ref)
	if e != nil || !proto.Equal(admissionAfter, a) {
		t.Fatalf("original admission changed: %v %v", admissionAfter, e)
	}
	budgetAfter, e := f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
	if e != nil || !proto.Equal(budgetAfter, budgetBefore) {
		t.Fatalf("original budget changed: %v %v", budgetAfter, e)
	}
	reservationsAfter, e := f.h.Budget.QueryReservations(f.ctx, f.caller, a.TaskId)
	if e != nil || len(reservationsAfter) != 1 || !proto.Equal(reservationsAfter[0], reservationsBefore[0]) {
		t.Fatalf("original send allowance changed: %v %v", reservationsAfter, e)
	}
	admitAfter, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("admit").Identity)
	if e != nil || !proto.Equal(admitAfter, admitBefore) {
		t.Fatalf("original admission receipt changed: %v %v", admitAfter, e)
	}
	claimAfter, e := f.h.LedgerWork.QueryReceipt(f.ctx, f.caller, ledgerHeader("claim").Identity)
	if e != nil || !proto.Equal(claimAfter, claimBefore) {
		t.Fatalf("previously accepted ledger claim receipt changed: %v %v", claimAfter, e)
	}
	assertUnstarted(t, f, a, start)
	requests, effects := target.Target.Snapshot()
	if len(requests) != 0 || len(effects) != 0 || len(target.Bills()) != 0 {
		t.Fatalf("unsupported saved job reached external target: requests=%v effects=%v bills=%v", requests, effects, target.Bills())
	}
	t.Logf("original operation=%s attempt=%s send=%s job=%s epoch=%d attempts=%d phase=REGISTERED target_requests=0 effects=0 bills=0 credential_use=ABSENT consumed_sends=0", a.OperationId.LocalId, start.Binding.AttemptId.LocalId, opBefore.Execution.Send.Ref.Name.LocalId, original.Ref.Name.LocalId, original.ClaimEpoch, original.Attempts)
}
