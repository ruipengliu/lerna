//go:build fault

package fault_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/infra/sqlite"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G3、G4、G10、G11、R7、开始-2
func TestCancellationCommitBoundariesKeepOriginalWindowAndResponsibility(t *testing.T) {
	for _, window := range []string{"before-p4", "between-p4-p5", "after-p5"} {
		for _, point := range []string{"sessions.input", "ledger.cancellation_seal", "tasks.cancellation_receipt"} {
			for _, mode := range []sqlite.FaultMode{sqlite.CrashBeforeCommit, sqlite.CrashAfterCommit, sqlite.LoseReceipt} {
				t.Run(window+"/"+point+"/"+string(mode), func(t *testing.T) {
					target := simulator.NewBillingTarget(25)
					target.DropReceipt(true)
					server := httptest.NewServer(target)
					defer server.Close()
					path := filepath.Join(t.TempDir(), "cancel.db")
					h, e := assembly.Open(path, "u", "d")
					if e != nil {
						t.Fatal(e)
					}
					ctx := context.Background()
					caller := &v1.Caller{UserId: "u", IssuerId: "host"}
					a, start := prepareStartFault(t, h, server.URL)
					if window == "between-p4-p5" {
						r, e := h.Tasks.StartExecution(ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
						requireAccepted(t, r, e)
					}
					if window == "after-p5" {
						r, e := h.Egress.Invoke(ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
						requireAccepted(t, r, e)
					}
					task, e := h.Tasks.QueryTask(ctx, caller, a.TaskId)
					if e != nil {
						t.Fatal(e)
					}
					goal, e := h.Durable.QueryReceipt(ctx, caller, admissionHeader("goal").Identity)
					if e != nil {
						t.Fatal(e)
					}
					cancel := &v1.SubmitInputCommand{Header: admissionHeader("cancel-boundary"), SessionId: goal.Receipt.SessionRef.Name, TaskId: a.TaskId, InputKind: "CONTROL", Control: "CANCEL", ExpectedControlGeneration: task.ControlGeneration}
					body, e := proto.Marshal(cancel)
					if e != nil {
						t.Fatal(e)
					}
					if e = os.WriteFile(path+".cancel", body, 0600); e != nil {
						t.Fatal(e)
					}
					if e = h.Close(); e != nil {
						t.Fatal(e)
					}
					child := exec.Command(os.Args[0], "-test.run=^TestCancellationCommitChild$")
					child.Env = append(os.Environ(), "LERNA_CANCEL_DB="+path, "LERNA_CANCEL_POINT="+point, "LERNA_CANCEL_MODE="+string(mode))
					out, e := child.CombinedOutput()
					if mode == sqlite.LoseReceipt {
						if e != nil {
							t.Fatalf("child %v %s", e, out)
						}
					} else {
						var exit *exec.ExitError
						if !errors.As(e, &exit) || exit.ExitCode() != sqlite.CrashExitCode {
							t.Fatalf("fault not reached %v %s", e, out)
						}
					}
					h, e = assembly.Open(path, "u", "d")
					if e != nil {
						t.Fatal(e)
					}
					defer h.Close()
					q, e := h.Durable.QueryReceipt(ctx, caller, cancel.Header.Identity)
					if e != nil {
						t.Fatal(e)
					}
					scope, e := h.Tasks.QueryCancellation(ctx, caller, a.TaskId)
					if e != nil {
						t.Fatal(e)
					}
					if point == "sessions.input" && mode == sqlite.CrashBeforeCommit {
						if q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND || scope != nil {
							t.Fatal("partial cancel survived rollback")
						}
						sources, e := h.Trace.QuerySources(ctx, caller)
						if e != nil {
							t.Fatal(e)
						}
						for _, source := range sources {
							if strings.HasPrefix(source.Command.Event.EventType, "CANCELLATION_") {
								t.Fatal("cancel source escaped original rollback")
							}
						}
					} else if q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED || scope == nil {
						t.Fatal("saved cancel responsibility lost")
					}
					r, e := h.Sessions.SubmitInput(ctx, caller, cancel)
					requireAccepted(t, r, e)
					if q.Receipt != nil && !proto.Equal(q.Receipt, r) {
						t.Fatal("original control receipt changed")
					}
					if e = h.Tasks.RecoverCancellations(ctx, caller); e != nil {
						t.Fatal(e)
					}
					scope, e = h.Tasks.QueryCancellation(ctx, caller, a.TaskId)
					if e != nil || len(scope.AdmissionRefs) != 1 || !proto.Equal(scope.AdmissionRefs[0], a.Ref) || len(scope.ClosureIntentRefs) != 1 {
						t.Fatalf("incomplete scope %v %v", scope, e)
					}
					intent, e := h.Tasks.QueryCancellationIntent(ctx, caller, scope.ClosureIntentRefs[0])
					if e != nil || intent.RecipientReceipt == nil {
						t.Fatalf("source ack lost %v %v", intent, e)
					}
					seal, e := h.Ledger.QueryCancellationSeal(ctx, caller, intent.RecipientReceipt.ResultRef)
					if e != nil || seal.NoSendProven != (window != "after-p5") || seal.PhysicalSendWasPossible != (window == "after-p5") {
						t.Fatalf("window changed %v %v", seal, e)
					}
					op, e := h.Ledger.QueryOperation(ctx, caller, a.OperationId)
					if e != nil || op.Dispatch != "SEALED" || len(op.ClosureEvidenceRefs) != 1 || !proto.Equal(op.ClosureEvidenceRefs[0], seal.Ref) {
						t.Fatalf("duplicate or missing seal %v %v", op, e)
					}
					assertCancellationFaultSources(t, h, caller, cancel, scope, intent, seal, op, path, server.URL)
					if e = h.Budget.ProcessClosures(ctx); e != nil {
						t.Fatal(e)
					}
					budget, e := h.Budget.QueryBudget(ctx, caller, a.TaskId)
					if e != nil {
						t.Fatal(e)
					}
					expectedCalls := 0
					if window == "after-p5" {
						expectedCalls = 1
						if op.Effect.Outcome != "UNKNOWN" || op.Effect.LateEffect != "MAY_OCCUR" || budget.Reserved != 30 {
							t.Fatalf("post-P5 risk lost %v %v", op, budget)
						}
					} else if op.Effect.Outcome != "NOT_APPLIED" || op.Effect.LateEffect != "RULED_OUT" || budget.Reserved != 0 {
						t.Fatalf("no-send proof unused %v %v", op, budget)
					}
					_, _ = h.Egress.Invoke(ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
					if e = h.Tasks.ProcessCancellations(ctx, caller); e != nil {
						t.Fatal(e)
					}
					requests, effects := target.Target.Snapshot()
					if len(requests) != expectedCalls || len(effects) != expectedCalls || len(target.Bills()) != expectedCalls {
						t.Fatalf("target receives=%d effects=%d bills=%d", len(requests), len(effects), len(target.Bills()))
					}
				})
			}
		}
	}
}

func assertCancellationFaultSources(t *testing.T, h *assembly.Harness, caller *v1.Caller, cancel *v1.SubmitInputCommand, scope *v1.Cancellation, intent *v1.CancellationClosureIntent, seal *v1.CancellationSeal, op *v1.Operation, path, target string) {
	t.Helper()
	ctx := context.Background()
	if e := h.Trace.Recover(ctx, caller); e != nil {
		t.Fatal(e)
	}
	sources, e := h.Trace.QuerySources(ctx, caller)
	if e != nil {
		t.Fatal(e)
	}
	want := map[string]*v1.Ref{"CANCELLATION_SAVED": scope.Ref, "CANCELLATION_CLOSURE_REQUESTED": intent.Ref, "CANCELLATION_CLOSURE_ACKNOWLEDGED": intent.Ref, "CANCELLATION_SEALED": seal.Ref}
	found := map[string]bool{}
	for _, source := range sources {
		ev := source.Command.Event
		if want[ev.EventType] == nil {
			continue
		}
		if found[ev.EventType] || !proto.Equal(ev.SourceRecordRef, want[ev.EventType]) || !proto.Equal(ev.TaskId, scope.TaskId) || source.Receipt == nil || !proto.Equal(source.Receipt.Identity, source.Command.Header.Identity) {
			t.Fatalf("original cancellation source duplicated or lost %v", source)
		}
		found[ev.EventType] = true
		origin := intent.Command.Header.Identity
		if ev.EventType == "CANCELLATION_SAVED" {
			origin = cancel.Header.Identity
		} else if !proto.Equal(ev.OperationId, intent.Command.OperationId) {
			t.Fatal("original cancellation operation lost")
		}
		if !proto.Equal(ev.OriginCommand, origin) {
			t.Fatal("original cancellation command lost")
		}
		if ev.EventType == "CANCELLATION_SEALED" && (!proto.Equal(ev.AttemptId, op.Execution.Attempt.Ref.Name) || !proto.Equal(ev.SendRef, op.Execution.Send.Ref) || ev.EffectOutcome != op.Effect.Outcome || ev.LateEffect != op.Effect.LateEffect) {
			t.Fatal("original window/send facts changed in source")
		}
		accepted, e := h.Trace.QueryEvent(ctx, caller, ev.Ref)
		if e != nil || !proto.Equal(accepted, ev) {
			t.Fatal("receiver changed source after recovery", e)
		}
	}
	if len(found) != len(want) {
		t.Fatalf("missing cancellation sources after crash %v", found)
	}
	view, e := h.Trace.Query(ctx, caller, &v1.TraceQuery{TaskId: scope.TaskId})
	if e != nil || !view.Complete {
		t.Fatal("source coverage after recovery", e)
	}
	encoded, e := protojson.Marshal(view)
	if e != nil {
		t.Fatal(e)
	}
	for _, private := range []string{path, target, "create a record", "local-api"} {
		if strings.Contains(string(encoded), private) {
			t.Fatal("cancellation source leaked body/path/target/endpoint")
		}
	}
}

// 规则：G3、G11、R7
func TestCancellationCommitChild(t *testing.T) {
	path := os.Getenv("LERNA_CANCEL_DB")
	if path == "" {
		t.Skip("subprocess only")
	}
	h, e := assembly.Open(path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	body, e := os.ReadFile(path + ".cancel")
	if e != nil {
		t.Fatal(e)
	}
	cancel := new(v1.SubmitInputCommand)
	if e = proto.Unmarshal(body, cancel); e != nil {
		t.Fatal(e)
	}
	point := os.Getenv("LERNA_CANCEL_POINT")
	ctx, e := sqlite.WithFault(context.Background(), point, sqlite.FaultMode(os.Getenv("LERNA_CANCEL_MODE")))
	if e != nil {
		t.Fatal(e)
	}
	caller := &v1.Caller{UserId: "u", IssuerId: "host"}
	r, e := h.Sessions.SubmitInput(ctx, caller, cancel)
	if point == "sessions.input" {
		if e == nil || r != nil {
			t.Fatal("fault not reached")
		}
		return
	}
	requireAccepted(t, r, e)
	claim, e := h.Durable.ExecuteJob(ctx, caller, &v1.JobCommand{Identity: admissionHeader("cancel-claim").Identity, ContractVersion: 1, Action: "CLAIM", Module: "tasks", AllowedTypes: []string{"DELIVER_CANCELLATION_CLOSURE"}, Limit: 1, LeaseMs: 100, ProcessInstance: "crashing-cancel-worker"})
	requireAccepted(t, claim, e)
	if len(claim.Jobs) != 1 {
		t.Fatal("missing cancellation closure job")
	}
	if e = h.Tasks.ProcessClosureClaim(ctx, claim.Jobs[0]); e == nil {
		t.Fatal("fault not reached")
	}
}
