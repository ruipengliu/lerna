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
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/infra/sqlite"
	"google.golang.org/protobuf/proto"
)

// 规则：G3、G5、G11
func TestPrepareCrashKeepsOriginalAttempt(t *testing.T) {
	for _, mode := range []sqlite.FaultMode{sqlite.CrashBeforeCommit, sqlite.CrashAfterCommit, sqlite.LoseReceipt} {
		t.Run(string(mode), func(t *testing.T) {
			var calls atomic.Int64
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(200) }))
			defer target.Close()
			path := filepath.Join(t.TempDir(), "execution.db")
			h, e := assembly.Open(path, "u", "d")
			if e != nil {
				t.Fatal(e)
			}
			a := admitExecution(t, h, target.URL)
			b, e := proto.Marshal(a)
			if e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(path+".admission", b, 0600); e != nil {
				t.Fatal(e)
			}
			h.Close()
			child := exec.Command(os.Args[0], "-test.run=^TestPrepareCrashChild$")
			child.Env = append(os.Environ(), "LERNA_EXECUTION_DB="+path, "LERNA_EXECUTION_MODE="+string(mode))
			out, e := child.CombinedOutput()
			if mode == sqlite.LoseReceipt {
				if e != nil {
					t.Fatalf("child %v %s", e, out)
				}
			} else {
				var exit *exec.ExitError
				if !errors.As(e, &exit) || exit.ExitCode() != sqlite.CrashExitCode {
					t.Fatalf("no crash %v %s", e, out)
				}
			}
			h, e = assembly.Open(path, "u", "d")
			if e != nil {
				t.Fatal(e)
			}
			defer h.Close()
			q, e := h.Ledger.QueryReceipt(context.Background(), &v1.Caller{UserId: "u", IssuerId: "host"}, executionHeader("prepare").Identity)
			if e != nil {
				t.Fatal(e)
			}
			x, e := h.Ledger.QueryExecution(context.Background(), &v1.Caller{UserId: "u", IssuerId: "host"}, a.OperationId)
			if e != nil {
				t.Fatal(e)
			}
			if mode == sqlite.CrashBeforeCommit {
				if q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND || x != nil {
					t.Fatalf("uncommitted state %v %v", q, x)
				}
			} else if q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED || x == nil || !proto.Equal(q.Receipt.ResultRef, x.Attempt.Ref) || x.Send.Phase != "REGISTERED" {
				t.Fatalf("lost state %v %v", q, x)
			}
			if calls.Load() != 0 {
				t.Fatalf("P3 called target %d", calls.Load())
			}
		})
	}
}

// 规则：G3、G11
func TestPrepareCrashChild(t *testing.T) {
	path := os.Getenv("LERNA_EXECUTION_DB")
	if path == "" {
		t.Skip("subprocess only")
	}
	h, e := assembly.Open(path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	b, e := os.ReadFile(path + ".admission")
	if e != nil {
		t.Fatal(e)
	}
	a := new(v1.Admission)
	if e = proto.Unmarshal(b, a); e != nil {
		t.Fatal(e)
	}
	caller := &v1.Caller{UserId: "u", IssuerId: "host"}
	ctx := context.Background()
	r, e := h.LedgerWork.ExecuteJob(ctx, caller, &v1.JobCommand{Identity: executionHeader("claim").Identity, ContractVersion: 1, Action: "CLAIM", AllowedTypes: []string{"EXECUTE_OPERATION"}, Limit: 1, LeaseMs: 30000, ProcessInstance: "crashing-worker"})
	requireAccepted(t, r, e)
	c := &v1.PrepareExecutionCommand{Header: executionHeader("prepare"), OperationId: a.OperationId, ProcessInstance: "crashing-worker", Claim: r.Jobs[0]}
	ctx, e = sqlite.WithFault(ctx, "ledger.prepare", sqlite.FaultMode(os.Getenv("LERNA_EXECUTION_MODE")))
	if e != nil {
		t.Fatal(e)
	}
	r, e = h.Ledger.Prepare(ctx, caller, c)
	if os.Getenv("LERNA_EXECUTION_MODE") == string(sqlite.LoseReceipt) {
		if e == nil || r != nil {
			t.Fatalf("leaked lost receipt %v %v", r, e)
		}
		r, e = h.Ledger.Prepare(context.Background(), caller, c)
		requireAccepted(t, r, e)
		return
	}
	t.Fatal("fault not reached")
}
func executionHeader(id string) *v1.CommandHeader {
	h := admissionHeader(id)
	h.Identity.TargetDomainId = "d/ledger"
	return h
}
func admitExecution(t *testing.T, h *assembly.Harness, target string) *v1.Admission {
	t.Helper()
	ctx := context.Background()
	caller := &v1.Caller{UserId: "u", IssuerId: "host"}
	c := prepareAdmission(t, h, target)
	r, e := h.Tasks.Admit(ctx, caller, c)
	requireAccepted(t, r, e)
	a, e := h.Tasks.QueryAdmission(ctx, caller, r.ResultRef)
	if e != nil {
		t.Fatal(e)
	}
	if e = h.Tasks.ProcessHandoffs(ctx, caller); e != nil {
		t.Fatal(e)
	}
	return a
}
