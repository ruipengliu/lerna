//go:build fault

package fault_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/infra/sqlite"
)

// 规则：G1、G4、G5、R7
func TestObservationCrashRecoveryKeepsSinglePhysicalSend(t *testing.T) {
	for _, point := range []string{"content.observation", "ledger.observation", "content.observation_ack", "budget.usage", "trace.accept", "ledger.usage_ack", "ledger.trace_ack", "ledger.interpret"} {
		for _, mode := range []sqlite.FaultMode{sqlite.CrashBeforeCommit, sqlite.CrashAfterCommit, sqlite.LoseReceipt} {
			t.Run(point+"/"+string(mode), func(t *testing.T) {
				var calls atomic.Int64
				oracle := simulator.New("idempotent")
				target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); oracle.ServeHTTP(w, r) }))
				defer target.Close()
				path := filepath.Join(t.TempDir(), "state.db")
				h, e := assembly.Open(path, "u", "d")
				if e != nil {
					t.Fatal(e)
				}
				a, c := prepareStartFault(t, h, target.URL)
				writeStart(t, path, c)
				h.Close()
				child := exec.Command(os.Args[0], "-test.run=^TestObservationCrashChild$")
				child.Env = append(os.Environ(), "LERNA_OBSERVATION_DB="+path, "LERNA_OBSERVATION_POINT="+point, "LERNA_OBSERVATION_MODE="+string(mode))
				out, e := child.CombinedOutput()
				if mode == sqlite.LoseReceipt {
					if e != nil {
						t.Fatalf("child %v %s", e, out)
					}
				} else {
					var x *exec.ExitError
					if !errors.As(e, &x) || x.ExitCode() != sqlite.CrashExitCode {
						t.Fatalf("crash %v %s", e, out)
					}
				}
				_, effects := oracle.Snapshot()
				if len(effects) != 1 {
					t.Fatalf("actual effects %v", effects)
				}
				if calls.Load() != 1 {
					t.Fatalf("actual receives %d", calls.Load())
				}
				h, e = assembly.Open(path, "u", "d")
				if e != nil {
					t.Fatal(e)
				}
				defer h.Close()
				ctx := context.Background()
				caller := &v1.Caller{UserId: "u", IssuerId: "egress"}
				r, e := h.Egress.Invoke(ctx, caller, c)
				requireAccepted(t, r, e)
				op, e := h.Ledger.QueryOperation(ctx, caller, a.OperationId)
				if e != nil {
					t.Fatal(e)
				}
				wantEvidence := 1
				wantOutcome := "APPLIED"
				if point == "content.observation" && mode == sqlite.CrashBeforeCommit {
					wantEvidence = 0
					wantOutcome = "UNKNOWN"
				}
				if len(op.Effect.EvidenceRefs) != wantEvidence || op.Effect.Outcome != wantOutcome || calls.Load() != 1 {
					t.Fatalf("recovery %v calls %d", op, calls.Load())
				}
			})
		}
	}
}

// 规则：G4、R7
func TestObservationCrashChild(t *testing.T) {
	path := os.Getenv("LERNA_OBSERVATION_DB")
	if path == "" {
		t.Skip("child only")
	}
	h, e := assembly.Open(path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	c := readStart(t, path)
	ctx, e := sqlite.WithFault(context.Background(), os.Getenv("LERNA_OBSERVATION_POINT"), sqlite.FaultMode(os.Getenv("LERNA_OBSERVATION_MODE")))
	if e != nil {
		t.Fatal(e)
	}
	_, e = h.Egress.Invoke(ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
	if e == nil {
		t.Fatal("injected receipt loss not returned")
	}
}
