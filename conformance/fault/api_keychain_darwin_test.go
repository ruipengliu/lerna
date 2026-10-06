//go:build fault && darwin && cgo

package fault_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/infra/keys"
	"github.com/ruipengliu/lerna/infra/sqlite"
	"google.golang.org/protobuf/proto"
)

func prepareAPIStartFault(t *testing.T, path, target string) (*assembly.Harness, *keys.SyntheticKeychain, *v1.Admission, *v1.StartExecutionCommand) {
	t.Helper()
	h, e := assembly.Open(path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	c := prepareAdmissionAdapter(t, h, target, "api-reference-v1", 2)
	return prepareAPIStartFromAdmissionFault(t, h, path, c)
}

func prepareAPIStartFromAdmissionFault(t *testing.T, h *assembly.Harness, path string, c *v1.AdmitCommand) (*assembly.Harness, *keys.SyntheticKeychain, *v1.Admission, *v1.StartExecutionCommand) {
	t.Helper()
	ctx, caller := context.Background(), &v1.Caller{UserId: "u", IssuerId: "host"}
	r, e := h.Tasks.Admit(ctx, caller, c)
	requireAccepted(t, r, e)
	a, e := h.Tasks.QueryAdmission(ctx, caller, r.ResultRef)
	if e != nil {
		t.Fatal(e)
	}
	if e = h.Tasks.ProcessHandoffs(ctx, caller); e != nil {
		t.Fatal(e)
	}
	_, start := prepareExistingAdmissionStartFault(t, h, a)
	keychain, e := keys.NewSyntheticKeychain()
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := keychain.Close(); e != nil {
			t.Error(e)
		}
	})
	if e = keychain.Put(start.CallDescriptor.ApiDescriptor.Binding, []byte("synthetic-api-fault-secret-f17e4038")); e != nil {
		t.Fatal(e)
	}
	if e = h.Close(); e != nil {
		t.Fatal(e)
	}
	h, e = assembly.OpenWithOptions(path, "u", "d", assembly.Options{APIKeychainPath: keychain.Path()})
	if e != nil {
		t.Fatal(e)
	}
	return h, keychain, a, start
}

// 规则：G1、G3、G4、G5、G10、G11、R7、开始-4、开始-5
func TestAPICrashRecoveryPreservesWaitAndSingleRequest(t *testing.T) {
	for _, point := range []string{"tasks.start", "ledger.dispatch", "content.observation", "body.accept", "content.publish", "ledger.observation", "content.observation_ack", "budget.usage", "trace.accept", "ledger.usage_ack", "ledger.trace_ack", "ledger.interpret", "trace.source_ack", "trace.index"} {
		for _, mode := range []sqlite.FaultMode{sqlite.CrashBeforeCommit, sqlite.CrashAfterCommit, sqlite.LoseReceipt} {
			t.Run(point+"/"+string(mode), func(t *testing.T) {
				var calls atomic.Int64
				target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					w.Header().Set("Lerna-Rate-Category", "CONCURRENCY")
					w.Header().Set("Retry-After", "30")
					w.WriteHeader(429)
					_ = json.NewEncoder(w).Encode(map[string]any{"protocol": "lerna-reference-api-v1", "external_key": r.Header.Get("Idempotency-Key"), "attempt_id": r.Header.Get("Lerna-Attempt"), "account": r.Header.Get("Lerna-Account"), "origin": "http://" + r.Host, "applied": false, "terminal": true})
				}))
				defer target.Close()
				path := filepath.Join(t.TempDir(), "state.db")
				h, keychain, a, start := prepareAPIStartFault(t, path, target.URL)
				writeStart(t, path, start)
				if e := h.Close(); e != nil {
					t.Fatal(e)
				}
				child := exec.Command(os.Args[0], "-test.run=^TestAPICrashChild$")
				child.Env = append(os.Environ(), "LERNA_API_FAULT_DB="+path, "LERNA_API_FAULT_POINT="+point, "LERNA_API_FAULT_MODE="+string(mode), "LERNA_API_FAULT_KEYCHAIN="+keychain.Path())
				out, e := child.CombinedOutput()
				if mode == sqlite.LoseReceipt {
					if e != nil {
						t.Fatalf("child%v %s", e, out)
					}
				} else {
					var exit *exec.ExitError
					if !errors.As(e, &exit) || exit.ExitCode() != sqlite.CrashExitCode {
						t.Fatalf("no crash%v %s", e, out)
					}
				}
				h, e = assembly.OpenWithOptions(path, "u", "d", assembly.Options{APIKeychainPath: keychain.Path()})
				if e != nil {
					t.Fatal(e)
				}
				defer h.Close()
				ctx, actor := context.Background(), &v1.Caller{UserId: "u", IssuerId: "egress"}
				r, e := h.Egress.Invoke(ctx, actor, start)
				requireAccepted(t, r, e)
				op, e := h.Ledger.QueryOperation(ctx, actor, a.OperationId)
				if e != nil || op.Effect.Outcome != "UNKNOWN" || op.Effect.LateEffect != "MAY_OCCUR" {
					t.Fatalf("lost uncertainty%v %v", op, e)
				}
				expectedCalls := int64(1)
				lostRaw := point == "content.observation" && mode == sqlite.CrashBeforeCommit
				if point == "ledger.dispatch" && mode != sqlite.CrashBeforeCommit {
					expectedCalls = 0
					lostRaw = true
				}
				if calls.Load() != expectedCalls {
					t.Fatalf("calls%d expected%d", calls.Load(), expectedCalls)
				}
				if lostRaw {
					if op.ApiWait != nil || len(op.Effect.EvidenceRefs) != 0 {
						t.Fatal("invented missing observation")
					}
				} else {
					raw, e := h.Ledger.QueryObservation(ctx, actor, op.Execution.Send.ObservationRef)
					if e != nil || op.ApiWait == nil || op.ApiWait.Category != "CONCURRENCY" || op.ApiWait.ReadyAtUnixMs != raw.FinishedAtUnixMs+30000 || !proto.Equal(op.ApiWait.ObservationRef, raw.Ref) || len(op.Effect.EvidenceRefs) != 1 {
						t.Fatalf("lost original wait%v %v", op.ApiWait, e)
					}
					before := proto.Clone(op)
					r, e = h.Egress.Invoke(ctx, actor, start)
					requireAccepted(t, r, e)
					replay, e := h.Ledger.QueryOperation(ctx, actor, a.OperationId)
					if e != nil || !proto.Equal(replay, before) || calls.Load() != expectedCalls {
						t.Fatal("wait replay changed records or target")
					}
				}
				budget, e := h.Budget.QueryBudget(ctx, actor, a.TaskId)
				if e != nil || budget.Reserved != 30 {
					t.Fatalf("lost original fee%v %v", budget, e)
				}
				wantWaits := 1
				if lostRaw {
					wantWaits = 0
				}
				assertAPIRecoveredSources(t, h, op, wantWaits, keychain.Path(), target.URL, "synthetic-api-fault-secret-f17e4038")
			})
		}
	}
}

// 规则：G1、G3、G4、G5、R7
func TestAPICrashChild(t *testing.T) {
	path := os.Getenv("LERNA_API_FAULT_DB")
	if path == "" {
		t.Skip("subprocess only")
	}
	h, e := assembly.OpenWithOptions(path, "u", "d", assembly.Options{APIKeychainPath: os.Getenv("LERNA_API_FAULT_KEYCHAIN")})
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	ctx, e := sqlite.WithFault(context.Background(), os.Getenv("LERNA_API_FAULT_POINT"), sqlite.FaultMode(os.Getenv("LERNA_API_FAULT_MODE")))
	if e != nil {
		t.Fatal(e)
	}
	r, e := h.Egress.Invoke(ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, readStart(t, path))
	if e == nil {
		e = h.Trace.Recover(ctx, &v1.Caller{UserId: "u", IssuerId: "host"})
	}
	if e == nil {
		t.Fatal("fault not reached")
	}
	if point := os.Getenv("LERNA_API_FAULT_POINT"); (point == "tasks.start" || point == "ledger.dispatch") && r != nil {
		t.Fatal("undecided receipt leaked")
	}
}

func assertAPIRecoveredSources(t *testing.T, h *assembly.Harness, op *v1.Operation, wantWaits int, private ...string) {
	t.Helper()
	ctx, actor := context.Background(), &v1.Caller{UserId: "u", IssuerId: "host"}
	if err := h.Trace.Recover(ctx, actor); err != nil {
		t.Fatal(err)
	}
	sources, err := h.Trace.QuerySources(ctx, actor)
	if err != nil {
		t.Fatal(err)
	}
	waits := 0
	for _, source := range sources {
		if source.Receipt == nil {
			t.Fatal("source receipt absent after recovery")
		}
		event := source.Command.Event
		accepted, err := h.Trace.QueryEvent(ctx, actor, event.Ref)
		if err != nil || !proto.Equal(event, accepted) {
			t.Fatal("receiver changed original API source", err)
		}
		encoded, err := proto.Marshal(source)
		if err != nil {
			t.Fatal(err)
		}
		for _, marker := range private {
			if bytes.Contains(encoded, []byte(marker)) {
				t.Fatal("private API value in trace source")
			}
		}
		if event.EventType == "API_WAIT_RECORDED" && proto.Equal(event.OperationId, op.Ref.Name) {
			waits++
			stored, err := h.Ledger.QuerySend(ctx, actor, event.SourceRecordRef)
			if err != nil || stored == nil || stored.ApiWait == nil {
				t.Fatal("wait source version missing", err)
			}
			if !proto.Equal(event.SendRef, stored.ApiWait.SendRef) || !proto.Equal(event.ObservationRef, stored.ApiWait.ObservationRef) || !proto.Equal(event.AttemptId, stored.AttemptId) || event.ReasonCode != "API_429_"+stored.ApiWait.Category {
				t.Fatal("wait trace rebound original source")
			}
		}
	}
	if waits != wantWaits {
		t.Fatal("wait source omitted or invented")
	}
	view, err := h.Trace.Query(ctx, actor, &v1.TraceQuery{OperationId: op.Ref.Name})
	if err != nil || !view.Complete {
		t.Fatal("API index recovery incomplete", err)
	}
}

// 规则：G1、G4、G5、G7、G8、开始-4、开始-5
func TestAPICurrentContentFailureCannotOpenP5(t *testing.T) {
	var calls atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(200) }))
	defer target.Close()
	h, _, a, start := prepareAPIStartFault(t, filepath.Join(t.TempDir(), "state.db"), target.URL)
	defer h.Close()
	ctx, actor := context.Background(), &v1.Caller{UserId: "u", IssuerId: "egress"}
	r, e := h.Tasks.StartExecution(ctx, actor, start)
	requireAccepted(t, r, e)
	r, e = h.Egress.Invoke(sqlite.WithContentReadUnavailable(ctx), actor, start)
	if e == nil || r != nil || calls.Load() != 0 {
		t.Fatal("unavailable content dispatched")
	}
	op, e := h.Ledger.QueryOperation(ctx, actor, a.OperationId)
	if e != nil || op.Execution.Send.Phase != "REGISTERED" || len(op.Effect.EvidenceRefs) != 0 {
		t.Fatalf("P5 opened%v %v", op, e)
	}
}
