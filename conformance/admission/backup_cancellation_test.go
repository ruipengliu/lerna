package admission_test

import (
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G3、G4、G10、G11、R7、V4
func TestStoppedBackupRestoresOriginalCancellationBeforeAdmissionDelivery(t *testing.T) {
	for _, sealed := range []bool{false, true} {
		name := "saved-control"
		if sealed {
			name = "saved-tombstone"
		}
		t.Run(name, func(t *testing.T) {
			target := simulator.NewBillingTarget(3)
			f := newFixtureWithTarget(t, 100, 80, false, target)
			proposal := f.propose(t, nil)
			command := &v1.AdmitCommand{Header: header("backup-cancel-admit"), TaskId: f.task.Name, ProposalRef: proposal, GrantRef: f.grant}
			receipt, err := f.h.Tasks.Admit(f.ctx, f.caller, command)
			accepted(t, receipt, err)
			admission, err := f.h.Tasks.QueryAdmission(f.ctx, f.caller, receipt.ResultRef)
			if err != nil {
				t.Fatal(err)
			}
			control := cancelTask(t, f, "backup-original-cancel")
			if sealed {
				if err = f.h.Tasks.ProcessCancellations(f.ctx, f.caller); err != nil {
					t.Fatal(err)
				}
			}
			scope, err := f.h.Tasks.QueryCancellation(f.ctx, f.caller, f.task.Name)
			if err != nil {
				t.Fatal(err)
			}
			intent, err := f.h.Tasks.QueryCancellationIntent(f.ctx, f.caller, scope.ClosureIntentRefs[0])
			if err != nil {
				t.Fatal(err)
			}
			var seal *v1.CancellationSeal
			if sealed {
				seal, err = f.h.Ledger.QueryCancellationSeal(f.ctx, f.caller, intent.RecipientReceipt.ResultRef)
				if err != nil || !seal.NoSendProven || seal.OperationRef != nil {
					t.Fatalf("original tombstone %v %v", seal, err)
				}
			} else if intent.RecipientReceipt != nil {
				t.Fatal("pending cancellation already acknowledged")
			}
			before, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, admission.OperationId)
			if err != nil || before != nil {
				t.Fatalf("original handoff already delivered %v %v", before, err)
			}
			directory := t.TempDir()
			if _, err = f.h.CloseAndBackup(f.ctx, directory); err != nil {
				t.Fatal(err)
			}
			stopped := stoppedFileSet(t, f.path)
			restored, err := assembly.RestoreBackup(f.ctx, directory, t.TempDir(), "u", "d")
			if err != nil {
				t.Fatal(err)
			}
			f.h = restored
			gotScope, err := f.h.Tasks.QueryCancellation(f.ctx, f.caller, f.task.Name)
			if err != nil || !proto.Equal(gotScope, scope) {
				t.Fatalf("original scope changed %v %v", gotScope, err)
			}
			gotIntent, err := f.h.Tasks.QueryCancellationIntent(f.ctx, f.caller, intent.Ref)
			if err != nil || gotIntent.RecipientReceipt == nil || !proto.Equal(gotIntent.Command, intent.Command) || !proto.Equal(gotIntent.Ref, intent.Ref) {
				t.Fatalf("original closure responsibility changed %v %v", gotIntent, err)
			}
			gotSeal, err := f.h.Ledger.QueryCancellationSeal(f.ctx, f.caller, gotIntent.RecipientReceipt.ResultRef)
			if err != nil || !gotSeal.NoSendProven || !proto.Equal(gotSeal.OperationId, admission.OperationId) {
				t.Fatalf("closure not proven %v %v", gotSeal, err)
			}
			if sealed && (!proto.Equal(gotSeal, seal) || !proto.Equal(gotIntent.RecipientReceipt, intent.RecipientReceipt)) {
				t.Fatal("original seal or endpoint receipt rewritten")
			}
			op, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, admission.OperationId)
			if err != nil || op == nil || op.Dispatch != "SEALED" || op.Lifecycle != "SETTLED" || op.Effect.Outcome != "NOT_APPLIED" || op.Effect.LateEffect != "RULED_OUT" || op.Execution != nil {
				t.Fatalf("restored original operation escaped closure %v %v", op, err)
			}
			gotAdmission, err := f.h.Tasks.QueryAdmission(f.ctx, f.caller, admission.Ref)
			if err != nil || !proto.Equal(gotAdmission, admission) {
				t.Fatalf("original admission rewritten %v %v", gotAdmission, err)
			}
			gotControl, err := f.h.Durable.QueryReceipt(f.ctx, f.caller, control.Identity)
			if err != nil || !proto.Equal(gotControl.Receipt, control) {
				t.Fatalf("original control receipt changed %v %v", gotControl, err)
			}
			replay, err := f.h.Tasks.Admit(f.ctx, f.caller, command)
			if err != nil || !proto.Equal(replay, receipt) {
				t.Fatalf("original admission replay changed %v %v", replay, err)
			}
			result, err := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
			if err != nil || result != nil {
				t.Fatalf("cancel invented result %v %v", result, err)
			}
			requests, effects := target.Target.Snapshot()
			if len(requests) != 0 || len(effects) != 0 || len(target.Bills()) != 0 {
				t.Fatal("restore sent original canceled operation")
			}
			assertStoppedFileSet(t, f.path, stopped)
		})
	}
}
