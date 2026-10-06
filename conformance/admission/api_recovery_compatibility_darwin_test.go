//go:build darwin && cgo

package admission_test

import (
	"errors"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G3、G4、G7、G10、G11、R7、V4
func TestStartupReadsOriginalAPIHistoryWithoutNativeCredentialResolution(t *testing.T) {
	for _, kind := range []string{"no-execution", "registered-missing-item"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, 100, 80, false)
			descriptor := configureAPI(t, f)
			keychain := withAPIKeychain(t, f, descriptor, []byte("synthetic-api-recovery-read"))
			var admission *v1.Admission
			var start *v1.StartExecutionCommand
			if kind == "registered-missing-item" {
				admission, start = prepareStart(t, f)
			} else {
				proposal := f.propose(t, nil)
				r, e := f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("api-recovery-unprepared"), TaskId: f.task.Name, ProposalRef: proposal, GrantRef: f.grant})
				accepted(t, r, e)
				admission, e = f.h.Tasks.QueryAdmission(f.ctx, f.caller, r.ResultRef)
				if e != nil {
					t.Fatal(e)
				}
				if e = f.h.Tasks.ProcessHandoffs(f.ctx, f.caller); e != nil {
					t.Fatal(e)
				}
			}
			original, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, admission.OperationId)
			if e != nil || original == nil || (kind == "no-execution") != (original.Execution == nil) {
				t.Fatalf("original actual API frontier: %v %v", original, e)
			}
			budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
			if e != nil {
				t.Fatal(e)
			}
			if e = keychain.Remove(descriptor.Binding); e != nil {
				t.Fatal(e)
			}
			if e = f.h.Close(); e != nil {
				t.Fatal(e)
			}
			next, e := assembly.OpenWithOptions(f.path, "u", "d", assembly.Options{APIKeychainPath: keychain.Path()})
			if e != nil {
				t.Fatal("supported original API history refused before any credential use", e)
			}
			f.h = next
			retained, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, admission.OperationId)
			if e != nil || !proto.Equal(retained, original) || f.calls.Load() != 0 {
				t.Fatalf("qualification changed original history or sent: %v %v", retained, e)
			}
			after, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
			if e != nil || !proto.Equal(after, budget) {
				t.Fatalf("qualification changed original budget: %v %v", after, e)
			}
			if start != nil {
				_, e = f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
				var failure *command.Failure
				if !errors.As(e, &failure) || failure.Detail.Code != "CREDENTIAL_UNAVAILABLE" {
					t.Fatalf("missing original native item was not distinguished from compatibility: %v", e)
				}
				retained, e = f.h.Ledger.QueryOperation(f.ctx, f.caller, admission.OperationId)
				if e != nil || !proto.Equal(retained, original) || f.calls.Load() != 0 || retained.Execution.Send.Phase != "REGISTERED" {
					t.Fatalf("missing item crossed P5: %v %v", retained, e)
				}
			}
			t.Logf("actual_original=%s startup_requests=0 native_item=MISSING supported_history=RETAINED no_P5=true", kind)
		})
	}
}

// originalAPIReceipt 用原提交者和负责域查询原回执，不触发业务重放。
func originalAPIReceipt(t *testing.T, f *fixture, id *v1.CommandIdentity) *v1.ReceiptQuery {
	t.Helper()
	caller := proto.Clone(f.caller).(*v1.Caller)
	caller.IssuerId = id.IssuerId
	var q *v1.ReceiptQuery
	var e error
	if id.TargetDomainId == "d/ledger" {
		q, e = f.h.LedgerWork.QueryReceipt(f.ctx, caller, id)
	} else {
		q, e = f.h.Durable.QueryReceipt(f.ctx, caller, id)
	}
	if e != nil || q.GetReceipt() == nil {
		t.Fatalf("original API receipt %s: %v %v", id.CommandId, q, e)
	}
	return q
}
