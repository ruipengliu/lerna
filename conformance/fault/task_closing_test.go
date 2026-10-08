//go:build fault

package fault_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"
	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/infra/egressio"
	"github.com/ruipengliu/lerna/infra/sqlite"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G3、G4、G10、G11、R7、完成-4、完成-6
func TestTaskClosingCommitBoundariesPreserveOriginalResponsibilities(t *testing.T) {
	testTaskClosingCommitBoundaries(t, "FAILED")
}

// 规则：G1、G3、G4、G10、G11、R7、完成-4、完成-6
func TestCancelledClosingCommitBoundariesPreserveOriginalResponsibilities(t *testing.T) {
	testTaskClosingCommitBoundaries(t, "CANCELLED")
}

func testTaskClosingCommitBoundaries(t *testing.T, closingOutcome string) {
	t.Helper()
	for _, window := range []string{"before-p4", "after-p4", "after-p5"} {
		for _, point := range []string{"tasks.closing", "ledger.task_closure_seal", "tasks.task_closure_receipt", "tasks.close_final", "ledger.followup_completion", "budget.followup_completion"} {
			if window != "after-p5" && strings.Contains(point, "followup") {
				continue
			}
			for _, mode := range []sqlite.FaultMode{sqlite.CrashBeforeCommit, sqlite.CrashAfterCommit, sqlite.LoseReceipt} {
				t.Run(window+"/"+point+"/"+string(mode), func(t *testing.T) {
					target := simulator.NewBillingTarget(25)
					server := httptest.NewServer(target)
					defer server.Close()
					path := filepath.Join(t.TempDir(), "closing.db")
					h, e := assembly.Open(path, "u", "d")
					if e != nil {
						t.Fatal(e)
					}
					a, close, late, bill := prepareTaskClosingFault(t, h, target, server.URL, window, closingOutcome)
					writeBudgetMessage(t, path+".close", close)
					if late != nil {
						writeBudgetMessage(t, path+".late", late)
						writeBudgetMessage(t, path+".bill", bill)
					}
					if e = h.Close(); e != nil {
						t.Fatal(e)
					}
					child := exec.Command(os.Args[0], "-test.run=^TestTaskClosingCommitChild$")
					child.Env = append(os.Environ(), "LERNA_TASK_CLOSE_DB="+path, "LERNA_TASK_CLOSE_POINT="+point, "LERNA_TASK_CLOSE_MODE="+string(mode))
					out, e := child.CombinedOutput()
					requireBudgetFault(t, mode, out, e)
					h, e = assembly.Open(path, "u", "d")
					if e != nil {
						t.Fatal(e)
					}
					defer h.Close()
					ctx := context.Background()
					caller := &v1.Caller{UserId: "u", IssuerId: "host"}
					q, e := h.Durable.QueryReceipt(ctx, caller, close.Header.Identity)
					if e != nil {
						t.Fatal(e)
					}
					if point == "tasks.closing" && mode == sqlite.CrashBeforeCommit {
						if q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND {
							t.Fatal("uncommitted close receipt survived")
						}
					} else if q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED {
						t.Fatal("committed close receipt lost")
					}
					r, e := h.Tasks.BeginTaskClose(ctx, caller, close)
					requireAccepted(t, r, e)
					if q.Receipt != nil && !proto.Equal(q.Receipt, r) {
						t.Fatal("original close receipt changed")
					}
					if e = h.Tasks.ProcessTaskClosings(ctx, caller); e != nil {
						t.Fatal(e)
					}
					unknown, jobCount, calls, cost, outcome := 0, 1, 0, int64(0), "NOT_APPLIED"
					if window == "after-p5" {
						unknown, jobCount, calls, cost, outcome = 1, 2, 1, 25, "APPLIED"
					}
					view, e := h.Tasks.QueryTaskClosingView(ctx, caller, a.TaskId)
					if e != nil || view.Result == nil || view.Result.Outcome != closingOutcome || view.Result.VerificationRef != nil || len(view.Result.UnknownOperationRefs) != unknown || len(view.Result.ExecutionFollowupRefs) != unknown || len(view.ExecutionFollowups) != unknown || len(view.SettlementFollowups) != 1 || len(view.PendingClosureRefs) != 0 {
						t.Fatalf("lost fixed close or responsibility %v %v", view, e)
					}
					fixed, e := proto.Marshal(view.Result)
					if e != nil {
						t.Fatal(e)
					}
					if previous, e := os.ReadFile(path + ".fixed"); e == nil && !bytes.Equal(previous, fixed) {
						t.Fatal("committed Result rewritten by restart")
					}
					intent, e := h.Tasks.QueryTaskClosureIntent(ctx, caller, view.Closing.ClosureIntentRefs[0])
					if e != nil || intent.RecipientReceipt == nil {
						t.Fatal("source acknowledgement lost")
					}
					seal, e := h.Ledger.QueryTaskClosureSeal(ctx, caller, intent.RecipientReceipt.ResultRef)
					if e != nil || seal == nil || (unknown == 1 && (!proto.Equal(seal.ExecutionFollowupRef, view.ExecutionFollowups[0].Ref) || !proto.Equal(seal.OperationRef, view.ExecutionFollowups[0].OperationRef))) {
						t.Fatalf("ledger followup split from original seal %v %v", seal, e)
					}
					if late != nil {
						saveTaskClosingLateFacts(t, h, ctx, late, bill)
					}
					if e = h.Budget.ProcessClosures(ctx); e != nil {
						t.Fatal(e)
					}
					if e = h.Ledger.ProcessExecutionFollowups(ctx, caller); e != nil {
						t.Fatal(e)
					}
					if e = h.Budget.ProcessSettlementFollowups(ctx, caller); e != nil {
						t.Fatal(e)
					}
					view, e = h.Tasks.QueryTaskClosingView(ctx, caller, a.TaskId)
					if e != nil || view.Operations[0].Dispatch != "SEALED" || view.Operations[0].Effect.Outcome != outcome || view.Operations[0].ExecutorEndpointId != a.ExecutorEndpointId || len(view.FollowupJobs) != jobCount {
						t.Fatalf("late original facts lost %v %v", view, e)
					}
					for _, job := range view.FollowupJobs {
						if job.State != "COMPLETED" {
							t.Fatalf("terminal original responsibility stranded %v", job)
						}
					}
					current, e := h.Budget.QueryBudget(ctx, caller, a.TaskId)
					if e != nil || current.Settled != cost || current.Reserved != 0 {
						t.Fatalf("original billing repeated or lost %v %v", current, e)
					}
					later, e := proto.Marshal(view.Result)
					if e != nil || !bytes.Equal(fixed, later) {
						t.Fatal("late facts rewrote Result")
					}
					if e = h.Close(); e != nil {
						t.Fatal(e)
					}
					h, e = assembly.Open(path, "u", "d")
					if e != nil {
						t.Fatal(e)
					}
					defer h.Close()
					again, e := h.Tasks.QueryResult(ctx, caller, a.TaskId)
					if e != nil || !proto.Equal(again, view.Result) {
						t.Fatal("second restart changed Result")
					}
					assertTaskClosingFaultSources(t, h, ctx, caller, view)
					if closingOutcome == "CANCELLED" {
						assertClosingCancellation(t, h, ctx, caller, view.Closing, window == "after-p5")
					}
					requests, effects := target.Target.Snapshot()
					if len(requests) != calls || len(effects) != calls || len(target.Bills()) != calls {
						t.Fatalf("recovery performed another original call: %d/%d/%d", len(requests), len(effects), len(target.Bills()))
					}
				})
			}
		}
	}
}

// 规则：G3、G6、G11、R7、完成-6
func assertTaskClosingFaultSources(t *testing.T, h *assembly.Harness, ctx context.Context, caller *v1.Caller, view *v1.TaskClosingView) {
	t.Helper()
	sources, e := h.Trace.QuerySources(ctx, caller)
	if e != nil {
		t.Fatal(e)
	}
	want := map[string][]*v1.Ref{"TASK_CLOSE_REQUESTED": {view.Closing.Ref}, "RESULT_FIXED": {view.Result.Ref}, "TASK_CLOSURE_ACKNOWLEDGED": view.Closing.ClosureIntentRefs}
	for _, f := range view.ExecutionFollowups {
		want["EXECUTION_FOLLOWUP_RETAINED"] = append(want["EXECUTION_FOLLOWUP_RETAINED"], f.Ref)
	}
	for _, f := range view.SettlementFollowups {
		want["SETTLEMENT_FOLLOWUP_RETAINED"] = append(want["SETTLEMENT_FOLLOWUP_RETAINED"], f.Ref)
	}
	for _, j := range view.FollowupJobs {
		kind := "SETTLEMENT_FOLLOWUP_COMPLETED"
		if j.Module == "ledger" {
			kind = "EXECUTION_FOLLOWUP_COMPLETED"
		}
		want[kind] = append(want[kind], j.Ref)
	}
	for kind, refs := range want {
		for _, ref := range refs {
			count := 0
			for _, source := range sources {
				ev := source.Command.Event
				if ev.EventType == kind && proto.Equal(ev.SourceRecordRef, ref) {
					count++
					if !proto.Equal(ev.TaskId, view.Closing.TaskId) {
						t.Fatal("closing source relabelled original task")
					}
				}
			}
			if count != 1 {
				t.Fatalf("original source %s saved %d times for %v", kind, count, ref)
			}
		}
	}
	if e = h.Trace.Recover(ctx, caller); e != nil {
		t.Fatal(e)
	}
	for _, source := range sources {
		event, e := h.Trace.QueryEvent(ctx, caller, source.Command.Event.Ref)
		if e != nil || !proto.Equal(event, source.Command.Event) {
			t.Fatalf("receiver changed original committed source %v %v", event, e)
		}
	}
}

// 规则：G3、G4、G10、R7
func TestTaskClosingCommitChild(t *testing.T) {
	path := os.Getenv("LERNA_TASK_CLOSE_DB")
	if path == "" {
		t.Skip("subprocess only")
	}
	h, e := assembly.Open(path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	close := new(v1.BeginTaskCloseCommand)
	readTaskClosingMessage(t, path+".close", close)
	point := os.Getenv("LERNA_TASK_CLOSE_POINT")
	ctx, e := sqlite.WithFault(context.Background(), point, sqlite.FaultMode(os.Getenv("LERNA_TASK_CLOSE_MODE")))
	if e != nil {
		t.Fatal(e)
	}
	caller := &v1.Caller{UserId: "u", IssuerId: "host"}
	r, e := h.Tasks.BeginTaskClose(ctx, caller, close)
	if point == "tasks.closing" {
		if e == nil {
			t.Fatal("fault not reached")
		}
		return
	}
	requireAccepted(t, r, e)
	claim, e := h.Durable.ExecuteJob(ctx, caller, &v1.JobCommand{Identity: admissionHeader("closing-fault-claim").Identity, ContractVersion: 1, Action: "CLAIM", Module: "tasks", AllowedTypes: []string{"DELIVER_TASK_CLOSURE"}, Limit: 1, LeaseMs: 100, ProcessInstance: "crashing-close-worker"})
	requireAccepted(t, claim, e)
	if len(claim.Jobs) != 1 {
		t.Fatal("missing original closure job")
	}
	e = h.Tasks.ProcessClosureClaim(ctx, claim.Jobs[0])
	if point == "ledger.task_closure_seal" || point == "tasks.task_closure_receipt" {
		if e == nil {
			t.Fatal("fault not reached")
		}
		return
	}
	if e != nil {
		t.Fatal(e)
	}
	e = h.Tasks.ProcessTaskClosings(ctx, caller)
	if point == "tasks.close_final" {
		if e == nil {
			t.Fatal("fault not reached")
		}
		return
	}
	if e != nil {
		t.Fatal(e)
	}
	result, e := h.Tasks.QueryResult(ctx, caller, close.TaskRef.Name)
	if e != nil || result == nil {
		t.Fatalf("close not saved %v", e)
	}
	writeBudgetMessage(t, path+".fixed", result)
	late := new(v1.PhysicalIOResult)
	bill := new(v1.ImportBillCommand)
	readTaskClosingMessage(t, path+".late", late)
	readTaskClosingMessage(t, path+".bill", bill)
	saveTaskClosingLateFacts(t, h, context.Background(), late, bill)
	if point == "ledger.followup_completion" {
		e = h.Ledger.ProcessExecutionFollowups(ctx, caller)
	} else {
		if e = h.Ledger.ProcessExecutionFollowups(context.Background(), caller); e != nil {
			t.Fatal(e)
		}
		e = h.Budget.ProcessSettlementFollowups(ctx, caller)
	}
	if e == nil {
		t.Fatal("fault not reached")
	}
}

func prepareTaskClosingFault(t *testing.T, h *assembly.Harness, target *simulator.BillingTarget, url, window, closingOutcome string) (*v1.Admission, *v1.BeginTaskCloseCommand, *v1.PhysicalIOResult, *v1.ImportBillCommand) {
	t.Helper()
	ctx := context.Background()
	caller := &v1.Caller{UserId: "u", IssuerId: "host"}
	a, c := prepareStartFault(t, h, url)
	var late *v1.PhysicalIOResult
	var bill *v1.ImportBillCommand
	if window != "before-p4" {
		actor := &v1.Caller{UserId: "u", IssuerId: "egress"}
		r, e := h.Tasks.StartExecution(ctx, actor, c)
		requireAccepted(t, r, e)
		x, e := h.Ledger.QueryExecution(ctx, caller, a.OperationId)
		if e != nil {
			t.Fatal(e)
		}
		if window == "after-p5" {
			dh := executionHeader("dispatch:" + x.Send.Ref.Name.LocalId)
			dh.Identity.IssuerId = actor.IssuerId
			r, fresh, e := h.Ledger.RecordDispatch(ctx, actor, &v1.DispatchCommand{Header: dh, OperationId: a.OperationId, StartReceipt: r, Claim: c.Claim})
			requireAccepted(t, r, e)
			if !fresh {
				t.Fatal("missing original physical send right")
			}
			x, e = h.Ledger.QueryExecution(ctx, caller, a.OperationId)
			if e != nil {
				t.Fatal(e)
			}
			content, e := h.Content.Read(ctx, actor, x.CallDescriptor.ParametersRef)
			if e != nil {
				t.Fatal(e)
			}
			late, e = (egressio.HTTP{}).Perform(ctx, &v1.PhysicalIORequest{TaskId: a.TaskId, OperationId: a.OperationId, ExecutorEndpointId: a.ExecutorEndpointId, Attempt: x.Attempt, Send: x.Send, CallDescriptor: x.CallDescriptor, Body: command.ContentBytes(content)})
			if e != nil {
				t.Fatal(e)
			}
			body, e := json.Marshal(map[string]any{"billing": target.Bills()[0]})
			if e != nil {
				t.Fatal(e)
			}
			evidence, e := h.Content.Stage(ctx, caller, &v1.SubmitGoalCommand{Identity: admissionHeader("closing-fault-statement").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: string(body)})
			if e != nil {
				t.Fatal(e)
			}
			bill = &v1.ImportBillCommand{Header: admissionHeader("closing-fault-import"), SendRef: x.Send.Ref, EvidenceRef: evidence}
		}
	}
	var cancelRef *v1.Ref
	if closingOutcome == "CANCELLED" {
		cancelRef = prepareClosingCancellation(t, h, ctx, caller, a.TaskId)
	}
	task, e := h.Tasks.QueryTask(ctx, caller, a.TaskId)
	if e != nil {
		t.Fatal(e)
	}
	return a, &v1.BeginTaskCloseCommand{Header: admissionHeader("closing-fault-begin"), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, ExpectedControlGeneration: task.ControlGeneration, Outcome: closingOutcome, CloseReason: "UNABLE_TO_COMPLETE", CancellationRef: cancelRef}, late, bill
}

func readTaskClosingMessage(t *testing.T, path string, message proto.Message) {
	t.Helper()
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	if e = proto.Unmarshal(b, message); e != nil {
		t.Fatal(e)
	}
}

func saveTaskClosingLateFacts(t *testing.T, h *assembly.Harness, ctx context.Context, late *v1.PhysicalIOResult, bill *v1.ImportBillCommand) {
	t.Helper()
	caller := &v1.Caller{UserId: "u", IssuerId: "host"}
	r, e := h.Budget.ImportBill(ctx, caller, bill)
	requireAccepted(t, r, e)
	actor := &v1.Caller{UserId: "u", IssuerId: "egress-io"}
	header := admissionHeader("observe:" + late.Observation.Ref.Name.LocalId)
	header.Identity.IssuerId, header.Identity.TargetDomainId = actor.IssuerId, "d/content"
	r, e = h.Content.RegisterObservation(ctx, actor, &v1.RegisterObservationCommand{Header: header, Observation: late.Observation, Body: late.Body})
	requireAccepted(t, r, e)
	if e = h.Content.ProcessObservations(ctx, caller); e != nil {
		t.Fatal(e)
	}
	if e = h.Ledger.ProcessReports(ctx, caller); e != nil {
		t.Fatal(e)
	}
	if e = h.Ledger.ProcessInterpretations(ctx, caller); e != nil {
		t.Fatal(e)
	}
}

// prepareClosingCancellation 保存真实用户停止及源 ACK，不把新关闭作为取消的替代依据。
func prepareClosingCancellation(t *testing.T, h *assembly.Harness, ctx context.Context, caller *v1.Caller, taskID *v1.GlobalName) *v1.Ref {
	t.Helper()
	task, e := h.Tasks.QueryTask(ctx, caller, taskID)
	if e != nil {
		t.Fatal(e)
	}
	goal, e := h.Durable.QueryReceipt(ctx, caller, admissionHeader("goal").Identity)
	if e != nil || goal.GetReceipt().GetSessionRef() == nil {
		t.Fatal("missing actual original session", e)
	}
	c := &v1.SubmitInputCommand{Header: admissionHeader("closing-original-cancel"), SessionId: goal.Receipt.SessionRef.Name, TaskId: taskID, InputKind: "CONTROL", Control: "CANCEL", ExpectedControlGeneration: task.ControlGeneration}
	r, e := h.Sessions.SubmitInput(ctx, caller, c)
	requireAccepted(t, r, e)
	if e = h.Tasks.ProcessCancellations(ctx, caller); e != nil {
		t.Fatal(e)
	}
	scope, e := h.Tasks.QueryCancellation(ctx, caller, taskID)
	if e != nil || scope == nil || !proto.Equal(scope.ControlIdentity, c.Header.Identity) {
		t.Fatal("original cancellation responsibility missing", e)
	}
	return scope.Ref
}

func assertClosingCancellation(t *testing.T, h *assembly.Harness, ctx context.Context, caller *v1.Caller, closing *v1.TaskClosing, possibleSend bool) {
	t.Helper()
	scope, e := h.Tasks.QueryCancellation(ctx, caller, closing.TaskId)
	if e != nil || scope == nil || !proto.Equal(scope.Ref, closing.CancellationRef) || len(scope.AdmissionRefs) != 1 || len(scope.ClosureIntentRefs) != 1 || !proto.Equal(scope.AdmissionRefs[0], closing.AdmissionRefs[0]) {
		t.Fatalf("original cancellation scope changed %v %v", scope, e)
	}
	intent, e := h.Tasks.QueryCancellationIntent(ctx, caller, scope.ClosureIntentRefs[0])
	if e != nil || intent == nil {
		t.Fatal("original cancellation intent missing", e)
	}
	r := intent.RecipientReceipt
	if r == nil || r.Phase != v1.ReceiptPhase_RECEIPT_PHASE_DECIDED || r.Decision != v1.Decision_DECISION_ACCEPTED || !proto.Equal(r.Identity, intent.Command.Header.Identity) || r.Fingerprint != command.SemanticFingerprint("cancellation-seal", intent.Command) {
		t.Fatalf("original cancellation ACK missing or changed %v", r)
	}
	seal, e := h.Ledger.QueryCancellationSeal(ctx, caller, r.ResultRef)
	if e != nil || seal == nil || !proto.Equal(seal.CancellationRef, scope.Ref) || !proto.Equal(seal.IntentRef, intent.Ref) || !proto.Equal(seal.AdmissionRef, scope.AdmissionRefs[0]) || !proto.Equal(seal.OperationId, intent.Command.OperationId) || seal.ExecutorEndpointId != intent.Command.ExecutorEndpointId || seal.NoSendProven != !possibleSend || seal.PhysicalSendWasPossible != possibleSend {
		t.Fatalf("original cancellation endpoint proof changed %v %v", seal, e)
	}
}
