//go:build fault

package admission_test

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G2、G3、G4、G10、G11、R7、V4
func TestStartupChecksOriginalTaskClosingContractsBeforeAnyOwnerRecovery(t *testing.T) {
	for _, test := range []struct{ table, metadata string }{
		{"task_closings", "unknown"}, {"task_closings", "nested"},
		{"task_closure_intents", "header-version"}, {"task_closure_intents", "nested"},
		{"task_closure_seals", "unknown"}, {"task_sealed_operations", "unknown"},
		{"execution_followups", "unknown"}, {"settlement_followups", "nested"},
		{"execution_followup_jobs", "job-version"},
	} {
		t.Run(test.table+"/"+test.metadata, func(t *testing.T) {
			target := simulator.NewBillingTarget(3)
			f := newFixtureWithTarget(t, 100, 80, false, target)
			a, start := prepareStart(t, f)
			performWithoutObservation(t, f, a, start)
			closeReceipt, err := beginNonSuccessClose(t, f, "compatibility-original-close", "FAILED", "USER_STOPPED")
			accepted(t, closeReceipt, err)
			if err = processNonSuccessClosings(f, "FAILED"); err != nil {
				t.Fatal(err)
			}
			view, err := f.h.Tasks.QueryTaskClosingView(f.ctx, f.caller, f.task.Name)
			if err != nil || view.Result.GetOutcome() != "FAILED" || len(view.ClosureIntents) != 1 || len(view.ClosureSeals) != 1 || len(view.ExecutionFollowups) != 1 || len(view.SettlementFollowups) != 1 {
				t.Fatalf("actual original closing responsibility: %v %v", view, err)
			}
			fixed, err := proto.Marshal(view.Result)
			if err != nil {
				t.Fatal(err)
			}
			opBefore, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if err != nil || opBefore.Effect.Outcome != "UNKNOWN" {
				t.Fatalf("actual original UNKNOWN: %v %v", opBefore, err)
			}
			budgetBefore, err := f.h.Budget.QueryBudget(f.ctx, f.caller, f.task.Name)
			if err != nil {
				t.Fatal(err)
			}
			var original proto.Message
			var ref *v1.Ref
			switch test.table {
			case "task_closings":
				original, ref = view.Closing, view.Closing.Ref
			case "task_closure_intents":
				original, ref = view.ClosureIntents[0], view.ClosureIntents[0].Ref
			case "task_closure_seals", "task_sealed_operations":
				original, ref = view.ClosureSeals[0], view.ClosureSeals[0].Ref
			case "execution_followups":
				original, ref = view.ExecutionFollowups[0], view.ExecutionFollowups[0].Ref
			case "settlement_followups":
				original, ref = view.SettlementFollowups[0], view.SettlementFollowups[0].Ref
			case "execution_followup_jobs":
				for _, job := range view.FollowupJobs {
					if job.JobType == "RECONCILE_UNRESOLVED_OPERATION" {
						original, ref = job, job.Ref
					}
				}
			}
			if original == nil || ref == nil {
				t.Fatal("public producer did not save the required original record")
			}
			changed := proto.Clone(original)
			metadata := changed
			if test.metadata == "nested" {
				switch record := changed.(type) {
				case *v1.TaskClosing:
					metadata = record.Ref
				case *v1.TaskClosureIntent:
					metadata = record.Command.Header
				case *v1.SettlementFollowup:
					metadata = record.ReservationRefs[0]
				}
			}
			expectedError := "UNSUPPORTED_FEATURE"
			switch test.metadata {
			case "header-version":
				changed.(*v1.TaskClosureIntent).Command.Header.ContractVersion = 2
				expectedError = "UNSUPPORTED_CONTRACT"
			case "job-version":
				changed.(*v1.Job).ContractVersion = 2
				expectedError = "UNSUPPORTED_CONTRACT"
			default:
				unknown := protowire.AppendTag(nil, 19000, protowire.VarintType)
				metadata.ProtoReflect().SetUnknown(protowire.AppendVarint(unknown, 1))
			}
			back := proto.Clone(changed)
			switch test.metadata {
			case "header-version":
				back.(*v1.TaskClosureIntent).Command.Header.ContractVersion = 1
			case "job-version":
				back.(*v1.Job).ContractVersion = 1
			case "nested":
				switch record := back.(type) {
				case *v1.TaskClosing:
					record.Ref.ProtoReflect().SetUnknown(nil)
				case *v1.TaskClosureIntent:
					record.Command.Header.ProtoReflect().SetUnknown(nil)
				case *v1.SettlementFollowup:
					record.ReservationRefs[0].ProtoReflect().SetUnknown(nil)
				}
			default:
				back.ProtoReflect().SetUnknown(nil)
			}
			if !proto.Equal(back, original) {
				t.Fatal("contract metadata fault changed business facts")
			}
			wire, err := proto.Marshal(changed)
			if err != nil {
				t.Fatal(err)
			}
			domain, id := ref.Name.AuthorityDomainId, ref.Name.LocalId
			if test.table == "task_sealed_operations" {
				domain, id = a.OperationId.AuthorityDomainId, a.OperationId.LocalId
			}
			// 只改真实原记录的兼容元数据，不插入业务责任或效果。
			if err = f.h.StorageFaultSQL("UPDATE " + test.table + " SET record=X'" + hex.EncodeToString(wire) + "' WHERE user_id='u' AND domain_id='" + domain + "' AND id='" + id + "'"); err != nil {
				t.Fatal(err)
			}
			goal := &v1.SubmitGoalCommand{Identity: header("task-closing-other-owner-pending").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "preserve the original queued responsibility"}
			pending, err := f.h.Sessions.SubmitGoal(f.ctx, f.caller, goal)
			if err != nil {
				t.Fatal(err)
			}
			jobBefore, err := f.h.Durable.QueryJob(f.ctx, f.caller, pending.JobRef.Name)
			if err != nil || jobBefore.State != "READY" || jobBefore.ClaimEpoch != 0 {
				t.Fatalf("actual original queued job: %v %v", jobBefore, err)
			}
			candidate, openErr := assembly.Open(f.path, "u", "d")
			if candidate != nil {
				if err = candidate.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if openErr == nil || openErr.Error() != expectedError {
				t.Errorf("startup interpreted unsupported original %s: %v", test.metadata, openErr)
			}
			afterReceipt, err := f.h.Durable.QueryReceipt(f.ctx, f.caller, goal.Identity)
			if err != nil || !proto.Equal(afterReceipt.GetReceipt(), pending) {
				t.Errorf("startup advanced another original owner: %v %v", afterReceipt, err)
			}
			jobAfter, err := f.h.Durable.QueryJob(f.ctx, f.caller, pending.JobRef.Name)
			if err != nil || !proto.Equal(jobAfter, jobBefore) || jobAfter.State != "READY" || jobAfter.ClaimEpoch != 0 {
				t.Errorf("startup claimed another original job: %v %v", jobAfter, err)
			}
			opAfter, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if err != nil || !proto.Equal(opAfter, opBefore) {
				t.Errorf("startup changed original operation: %v %v", opAfter, err)
			}
			budgetAfter, err := f.h.Budget.QueryBudget(f.ctx, f.caller, f.task.Name)
			if err != nil || !proto.Equal(budgetAfter, budgetBefore) {
				t.Errorf("startup changed original budget: %v %v", budgetAfter, err)
			}
			result, err := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
			later, marshalErr := proto.Marshal(result)
			if err != nil || marshalErr != nil || !bytes.Equal(fixed, later) {
				t.Errorf("startup rewrote fixed Result: %v %v", err, marshalErr)
			}
			closeAfter, err := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("compatibility-original-close").Identity)
			if err != nil || !proto.Equal(closeAfter.GetReceipt(), closeReceipt) {
				t.Errorf("startup changed original closing receipt: %v %v", closeAfter, err)
			}
			requests, effects := target.Target.Snapshot()
			if len(requests) != 1 || requests[0].Method != "POST" || len(effects) != 1 || len(target.Bills()) != 1 {
				t.Fatal("compatibility changed independent actual target history")
			}
		})
	}
}
