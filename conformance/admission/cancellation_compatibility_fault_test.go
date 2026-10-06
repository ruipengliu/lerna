//go:build fault

package admission_test

import (
	"encoding/hex"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G3、G4、G11、R7、V4
func TestStartupChecksOriginalCancellationContractsBeforeAnyOwnerRecovery(t *testing.T) {
	for _, table := range []string{"cancellations", "cancellation_intents", "cancellation_seals", "cancellation_sealed_operations"} {
		t.Run(table, func(t *testing.T) {
			target := simulator.NewBillingTarget(3)
			f := newFixtureWithTarget(t, 100, 80, false, target)
			proposed := f.propose(t, nil)
			acceptedAdmission, err := f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("compatibility-cancel-admit"), TaskId: f.task.Name, ProposalRef: proposed, GrantRef: f.grant})
			accepted(t, acceptedAdmission, err)
			admission, err := f.h.Tasks.QueryAdmission(f.ctx, f.caller, acceptedAdmission.ResultRef)
			if err != nil {
				t.Fatal(err)
			}
			cancelTask(t, f, "compatibility-cancel")
			if err = f.h.Tasks.ProcessCancellations(f.ctx, f.caller); err != nil {
				t.Fatal(err)
			}
			scope, err := f.h.Tasks.QueryCancellation(f.ctx, f.caller, f.task.Name)
			if err != nil {
				t.Fatal(err)
			}
			intent, err := f.h.Tasks.QueryCancellationIntent(f.ctx, f.caller, scope.ClosureIntentRefs[0])
			if err != nil {
				t.Fatal(err)
			}
			seal, err := f.h.Ledger.QueryCancellationSeal(f.ctx, f.caller, intent.RecipientReceipt.ResultRef)
			if err != nil || !seal.NoSendProven || seal.OperationRef != nil {
				t.Fatalf("actual pre-P2 seal: %v %v", seal, err)
			}
			var original proto.Message = seal
			column, domain, id := "id", seal.Ref.Name.AuthorityDomainId, seal.Ref.Name.LocalId
			switch table {
			case "cancellations":
				original, column, domain, id = scope, "task_id", scope.TaskId.AuthorityDomainId, scope.TaskId.LocalId
			case "cancellation_intents":
				original, domain, id = intent, intent.Ref.Name.AuthorityDomainId, intent.Ref.Name.LocalId
			case "cancellation_sealed_operations":
				id = seal.OperationId.LocalId
			}
			changed := proto.Clone(original)
			metadata := changed
			if table == "cancellation_intents" {
				metadata = changed.(*v1.CancellationClosureIntent).Command.Header
			}
			unknown := protowire.AppendTag(nil, 19000, protowire.VarintType)
			unknown = protowire.AppendVarint(unknown, 1)
			metadata.ProtoReflect().SetUnknown(unknown)
			back := proto.Clone(changed)
			if table == "cancellation_intents" {
				back.(*v1.CancellationClosureIntent).Command.Header.ProtoReflect().SetUnknown(nil)
			} else {
				back.ProtoReflect().SetUnknown(nil)
			}
			if !proto.Equal(back, original) {
				t.Fatal("metadata fault altered business facts")
			}
			wire, err := proto.Marshal(changed)
			if err != nil {
				t.Fatal(err)
			}
			statement := "UPDATE " + table + " SET record=X'" + hex.EncodeToString(wire) + "' WHERE user_id='u' AND domain_id='" + domain + "' AND " + column + "='" + id + "'"
			if err = f.h.StorageFaultSQL(statement); err != nil {
				t.Fatal(err)
			}
			goal := &v1.SubmitGoalCommand{Identity: header("original-other-owner-pending").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "preserve the queued original task"}
			pending, err := f.h.Sessions.SubmitGoal(f.ctx, f.caller, goal)
			if err != nil {
				t.Fatal(err)
			}
			jobBefore, err := f.h.Durable.QueryJob(f.ctx, f.caller, pending.JobRef.Name)
			if err != nil {
				t.Fatal(err)
			}
			candidate, openErr := assembly.Open(f.path, "u", "d")
			if candidate != nil {
				if err := candidate.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if openErr == nil {
				t.Error("startup accepted unsupported original cancellation contract")
			}
			receiptAfter, err := f.h.Durable.QueryReceipt(f.ctx, f.caller, goal.Identity)
			if err != nil || !proto.Equal(receiptAfter.Receipt, pending) {
				t.Errorf("startup advanced other owner before refusal: %v %v", receiptAfter, err)
			}
			jobAfter, err := f.h.Durable.QueryJob(f.ctx, f.caller, pending.JobRef.Name)
			if err != nil || !proto.Equal(jobAfter, jobBefore) || jobAfter.State != "READY" || jobAfter.ClaimEpoch != 0 {
				t.Errorf("startup claimed another original job: %v %v", jobAfter, err)
			}
			operation, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, admission.OperationId)
			if err != nil || operation != nil {
				t.Errorf("startup delivered original handoff before refusal: %v %v", operation, err)
			}
			requests, effects := target.Target.Snapshot()
			if len(requests) != 0 || len(effects) != 0 || len(target.Bills()) != 0 {
				t.Fatal("compatibility check reached target")
			}
		})
	}
}
