//go:build fault

package admission_test

import (
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G2、G3、G10、G11、R7、完成-6
func TestCompletionClosurePreservesMissingExecutionResponsibility(t *testing.T) {
	testClosurePreservesMissingExecutionResponsibility(t, "completion")
}

// 规则：G1、G3、G4、G10、G11、R7、开始-2
func TestCancellationClosurePreservesMissingExecutionResponsibility(t *testing.T) {
	testClosurePreservesMissingExecutionResponsibility(t, "cancellation")
}

// 规则：G1、G2、G3、G10、G11、R7、完成-6
func TestTaskClosurePreservesMissingExecutionResponsibility(t *testing.T) {
	testClosurePreservesMissingExecutionResponsibility(t, "task-close")
}

type sendClosureProof interface {
	GetNoSendProven() bool
	GetPhysicalSendWasPossible() bool
	GetOperationRef() *v1.Ref
	GetClosedSendRefs() []*v1.Ref
}

func testClosurePreservesMissingExecutionResponsibility(t *testing.T, kind string) {
	t.Helper()
	for _, missing := range []string{"execution", "current-send"} {
		t.Run(missing, func(t *testing.T) {
			target := simulator.NewBillingTarget(25)
			f := newFixtureWithTarget(t, 100, 80, false, target)
			scopeRequirement(t, f)
			a, start := prepareStart(t, f)
			_ = performWithoutObservation(t, f, a, start)
			original, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if e != nil || original.Effect.Outcome != "UNKNOWN" || original.Effect.LateEffect != "MAY_OCCUR" || len(original.AttemptRefs) == 0 {
				t.Fatalf("original possible send: %v %v", original, e)
			}
			var close func() (*v1.CommandReceipt, error)
			var queryProof func(*v1.Ref) (sendClosureProof, error)
			var queryIntent func() (proto.Message, error)
			var identity *v1.CommandIdentity
			var intent proto.Message
			var jobRef *v1.Ref
			switch kind {
			case "completion":
				p := completeProposal(t, f, []*v1.CompletionEvidence{{ConditionId: "created", OperationId: a.OperationId}})
				r, e := f.h.Tasks.BeginCompletion(f.ctx, f.caller, &v1.BeginCompletionCommand{Header: header("missing-history-begin"), TaskId: f.task.Name, ProposalRef: p})
				accepted(t, r, e)
				round, e := f.h.Tasks.QueryVerification(f.ctx, f.caller, r.ResultRef)
				if e != nil || len(round.ClosureIntentRefs) != 1 {
					t.Fatalf("original completion scope: %v %v", round, e)
				}
				i, e := f.h.Tasks.QueryCompletionIntent(f.ctx, f.caller, round.ClosureIntentRefs[0])
				if e != nil {
					t.Fatal(e)
				}
				identity, intent, jobRef = i.Command.Header.Identity, i, i.JobRef
				actor := &v1.Caller{UserId: "u", IssuerId: "tasks-completion"}
				close = func() (*v1.CommandReceipt, error) { return f.h.Egress.CloseForCompletion(f.ctx, actor, i.Command) }
				queryProof = func(ref *v1.Ref) (sendClosureProof, error) {
					return f.h.Ledger.QueryCompletionSeal(f.ctx, f.caller, ref)
				}
				queryIntent = func() (proto.Message, error) { return f.h.Tasks.QueryCompletionIntent(f.ctx, f.caller, i.Ref) }
			case "cancellation":
				cancelTask(t, f, "missing-history-cancel")
				scope, e := f.h.Tasks.QueryCancellation(f.ctx, f.caller, f.task.Name)
				if e != nil || len(scope.ClosureIntentRefs) != 1 {
					t.Fatalf("original cancellation scope: %v %v", scope, e)
				}
				i, e := f.h.Tasks.QueryCancellationIntent(f.ctx, f.caller, scope.ClosureIntentRefs[0])
				if e != nil {
					t.Fatal(e)
				}
				identity, intent, jobRef = i.Command.Header.Identity, i, i.JobRef
				actor := &v1.Caller{UserId: "u", IssuerId: "tasks-cancellation"}
				close = func() (*v1.CommandReceipt, error) { return f.h.Egress.CloseForCancellation(f.ctx, actor, i.Command) }
				queryProof = func(ref *v1.Ref) (sendClosureProof, error) {
					return f.h.Ledger.QueryCancellationSeal(f.ctx, f.caller, ref)
				}
				queryIntent = func() (proto.Message, error) { return f.h.Tasks.QueryCancellationIntent(f.ctx, f.caller, i.Ref) }
			case "task-close":
				r, e := beginNonSuccessClose(t, f, "missing-history-close", "FAILED", "USER_STOPPED")
				accepted(t, r, e)
				closing, e := f.h.Tasks.QueryTaskClosing(f.ctx, f.caller, r.ResultRef)
				if e != nil || len(closing.ClosureIntentRefs) != 1 {
					t.Fatalf("original task closure scope: %v %v", closing, e)
				}
				i, e := f.h.Tasks.QueryTaskClosureIntent(f.ctx, f.caller, closing.ClosureIntentRefs[0])
				if e != nil {
					t.Fatal(e)
				}
				identity, intent, jobRef = i.Command.Header.Identity, i, i.JobRef
				actor := &v1.Caller{UserId: "u", IssuerId: "tasks-closing"}
				close = func() (*v1.CommandReceipt, error) { return f.h.Egress.CloseForTaskClose(f.ctx, actor, i.Command) }
				queryProof = func(ref *v1.Ref) (sendClosureProof, error) {
					return f.h.Ledger.QueryTaskClosureSeal(f.ctx, f.caller, ref)
				}
				queryIntent = func() (proto.Message, error) { return f.h.Tasks.QueryTaskClosureIntent(f.ctx, f.caller, i.Ref) }
			default:
				t.Fatal("unknown test workflow")
			}
			job, e := f.h.Durable.QueryJob(f.ctx, f.caller, jobRef.Name)
			if e != nil {
				t.Fatal(e)
			}
			executeJob, e := f.h.LedgerWork.QueryJob(f.ctx, f.caller, start.Claim.Ref.Name)
			if e != nil {
				t.Fatal(e)
			}
			budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
			if e != nil {
				t.Fatal(e)
			}
			source, e := f.h.Budget.QueryBillingSource(f.ctx, f.caller, original.Execution.Send.Ref)
			if e != nil || source == nil {
				t.Fatal("original source missing", e)
			}
			changed := proto.Clone(original).(*v1.Operation)
			if missing == "execution" {
				changed.Execution = nil
			} else {
				changed.Execution.Send = nil
			}
			faultClosureExecutionRecord(t, f, changed)
			r, closeErr := close()
			if missing == "current-send" {
				if r != nil || closeErr == nil || !strings.Contains(closeErr.Error(), "incomplete execution history") {
					t.Fatalf("partial history must fail before a closure decision: %v %v", r, closeErr)
				}
				after, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
				if e != nil || !proto.Equal(after, changed) {
					t.Fatal("failed closure changed original facts", e)
				}
				actor := &v1.Caller{UserId: "u", IssuerId: identity.IssuerId}
				q, e := f.h.LedgerWork.QueryReceipt(f.ctx, actor, identity)
				if e != nil || q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND {
					t.Fatalf("partial history fixed a recipient decision: %v %v", q, e)
				}
				pending, e := queryIntent()
				if e != nil || !proto.Equal(pending, intent) {
					t.Fatal("failed closure changed original source intent", e)
				}
				pendingJob, e := f.h.Durable.QueryJob(f.ctx, f.caller, jobRef.Name)
				if e != nil || !proto.Equal(pendingJob, job) {
					t.Fatal("failed closure changed original source work", e)
				}
				pendingExecuteJob, e := f.h.LedgerWork.QueryJob(f.ctx, f.caller, start.Claim.Ref.Name)
				if e != nil || !proto.Equal(pendingExecuteJob, executeJob) {
					t.Fatal("failed closure changed original execution work", e)
				}
				pendingBudget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
				if e != nil || !proto.Equal(pendingBudget, budget) {
					t.Fatal("failed closure released original fee", e)
				}
				faultClosureExecutionRecord(t, f, original)
				r, closeErr = close()
			}
			accepted(t, r, closeErr)
			if !proto.Equal(r.Identity, identity) {
				t.Fatal("recovery replaced original closure identity")
			}
			replayed, replayErr := close()
			if replayErr != nil || !proto.Equal(replayed, r) {
				t.Fatalf("replay replaced original closure receipt: %v %v", replayed, replayErr)
			}
			proof, e := queryProof(r.ResultRef)
			if e != nil || proof.GetNoSendProven() || !proof.GetPhysicalSendWasPossible() || proof.GetOperationRef() == nil || len(proof.GetClosedSendRefs()) != 0 {
				t.Fatalf("missing or possible history became no-send proof: %v %v", proof, e)
			}
			after, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if e != nil || after.Dispatch != "SEALED" || !proto.Equal(after.Effect, original.Effect) || after.Lifecycle == "SETTLED" {
				t.Fatalf("closure erased original unknown: %v %v", after, e)
			}
			if !proto.Equal(proof.GetOperationRef(), after.Ref) {
				t.Fatal("seal lost exact original operation revision")
			}
			afterExecuteJob, e := f.h.LedgerWork.QueryJob(f.ctx, f.caller, start.Claim.Ref.Name)
			if e != nil || afterExecuteJob.State != "WAITING" || afterExecuteJob.ProcessInstance != "" || afterExecuteJob.LeaseUntilUnixMs != 0 {
				t.Fatalf("closure lost unresolved execution work: %v %v", afterExecuteJob, e)
			}
			if kind == "task-close" {
				seal, e := f.h.Ledger.QueryTaskClosureSeal(f.ctx, f.caller, r.ResultRef)
				if e != nil || seal.ExecutionFollowupRef == nil {
					t.Fatal("closure lost original execution followup", e)
				}
				followup, e := f.h.Ledger.QueryExecutionFollowup(f.ctx, f.caller, seal.ExecutionFollowupRef)
				if e != nil || !proto.Equal(followup.OperationRef, after.Ref) || !proto.Equal(followup.AdmissionRef, a.Ref) || followup.ExecutorEndpointId != a.ExecutorEndpointId {
					t.Fatalf("followup lost original scope: %v %v", followup, e)
				}
				followupJob, e := f.h.LedgerWork.QueryJob(f.ctx, f.caller, followup.JobRef.Name)
				if e != nil || followupJob.State != "WAITING" || followupJob.WaitingReason == "" {
					t.Fatalf("closure discarded unresolved followup: %v %v", followupJob, e)
				}
			}
			afterIntent, e := queryIntent()
			if e != nil || !proto.Equal(afterIntent, intent) {
				t.Fatal("recipient closure changed source intent", e)
			}
			afterJob, e := f.h.Durable.QueryJob(f.ctx, f.caller, jobRef.Name)
			if e != nil || !proto.Equal(afterJob, job) {
				t.Fatal("recipient closure completed original source work", e)
			}
			afterBudget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
			if e != nil || !proto.Equal(afterBudget, budget) {
				t.Fatal("closure released original unknown fee", e)
			}
			afterSource, e := f.h.Budget.QueryBillingSource(f.ctx, f.caller, original.Execution.Send.Ref)
			if e != nil || !proto.Equal(afterSource, source) {
				t.Fatal("closure erased original billing source", e)
			}
			send, e := f.h.Ledger.QuerySend(f.ctx, f.caller, original.Execution.Send.Ref)
			if e != nil || !proto.Equal(send, original.Execution.Send) {
				t.Fatal("closure rewrote immutable send evidence", e)
			}
			result, e := f.h.Tasks.QueryResult(f.ctx, f.caller, a.TaskId)
			if e != nil || result != nil {
				t.Fatal("endpoint closure fabricated final Result", e)
			}
			requests, effects := target.Target.Snapshot()
			if len(requests) != 1 || len(effects) != 1 || len(target.Bills()) != 1 {
				t.Fatal("missing history created another external request/effect/bill")
			}
		})
	}
}

// 故障仅移除真实原动作的执行材料，当前及同修订保存版本一致；断言全部走原负责方公共查询。
func faultClosureExecutionRecord(t *testing.T, f *fixture, operation *v1.Operation) {
	t.Helper()
	body, e := proto.Marshal(operation)
	if e != nil {
		t.Fatal(e)
	}
	wire := hex.EncodeToString(body)
	id := operation.Ref.Name.LocalId
	if e = f.h.StorageFaultSQL("UPDATE operations SET record=X'" + wire + "' WHERE user_id='u' AND domain_id='d/ledger' AND id='" + id + "'"); e != nil {
		t.Fatal(e)
	}
	if e = f.h.StorageFaultSQL("UPDATE ledger_versions SET record=X'" + wire + "' WHERE user_id='u' AND domain_id='d/ledger' AND object_kind='operation' AND id='" + id + "' AND revision=" + fmt.Sprint(operation.Ref.Revision)); e != nil {
		t.Fatal(e)
	}
}
