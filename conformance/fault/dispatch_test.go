//go:build fault

package fault_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/fault/storagevfs"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/infra/sqlite"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G3、G5
func TestDispatchCrashWindowNeverBlindlyResends(t *testing.T) {
	for _, mode := range []sqlite.FaultMode{sqlite.CrashBeforeCommit, sqlite.CrashAfterCommit, sqlite.LoseReceipt} {
		t.Run(string(mode), func(t *testing.T) {
			var calls atomic.Int64
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(200) }))
			defer target.Close()
			path := filepath.Join(t.TempDir(), "state.db")
			h, e := assembly.Open(path, "u", "d")
			if e != nil {
				t.Fatal(e)
			}
			a, c := prepareStartFault(t, h, target.URL)
			writeStart(t, path, c)
			h.Close()
			child := exec.Command(os.Args[0], "-test.run=^TestDispatchCrashChild$")
			child.Env = append(os.Environ(), "LERNA_DISPATCH_DB="+path, "LERNA_DISPATCH_MODE="+string(mode))
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
			if calls.Load() != 0 {
				t.Fatalf("I/O before crash boundary: %d", calls.Load())
			}
			h, e = assembly.Open(path, "u", "d")
			if e != nil {
				t.Fatal(e)
			}
			defer h.Close()
			ctx := context.Background()
			caller := &v1.Caller{UserId: "u", IssuerId: "egress"}
			op, e := h.Ledger.QueryOperation(ctx, caller, a.OperationId)
			if e != nil {
				t.Fatal(e)
			}
			if mode == sqlite.CrashBeforeCommit {
				if op.Execution.Send.Phase != "REGISTERED" || op.Effect.Outcome != "NOT_APPLIED" {
					t.Fatalf("uncommitted P5 %v", op)
				}
			} else if op.Execution.Send.Phase != "DISPATCH_POSSIBLE" || op.Effect.Outcome != "UNKNOWN" || op.Effect.LateEffect != "MAY_OCCUR" {
				t.Fatalf("lost unknown %v", op)
			}
			r, e := h.Egress.Invoke(ctx, caller, c)
			requireAccepted(t, r, e)
			want := int64(0)
			if mode == sqlite.CrashBeforeCommit {
				want = 1
			}
			if calls.Load() != want {
				t.Fatalf("unsafe recovery sends=%d want%d", calls.Load(), want)
			}
		})
	}
}

// 规则：G1、G3、G5
func TestDispatchCrashChild(t *testing.T) {
	path := os.Getenv("LERNA_DISPATCH_DB")
	if path == "" {
		t.Skip("subprocess only")
	}
	h, e := assembly.Open(path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	c := readStart(t, path)
	ctx, e := sqlite.WithFault(context.Background(), "ledger.dispatch", sqlite.FaultMode(os.Getenv("LERNA_DISPATCH_MODE")))
	if e != nil {
		t.Fatal(e)
	}
	caller := &v1.Caller{UserId: "u", IssuerId: "egress"}
	r, e := h.Egress.Invoke(ctx, caller, c)
	if os.Getenv("LERNA_DISPATCH_MODE") == string(sqlite.LoseReceipt) {
		if e == nil || r != nil {
			t.Fatalf("leaked P5 receipt %v %v", r, e)
		}
		r, e = h.Egress.Invoke(context.Background(), caller, c)
		requireAccepted(t, r, e)
		return
	}
	t.Fatal("fault not reached")
}

// 规则：G3、G5
func TestDispatchActualSyncFailurePreventsIO(t *testing.T) {
	for _, mode := range []string{"vfs", "native"} {
		if mode == "native" && runtime.GOOS != "darwin" {
			continue
		}
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int64
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(200) }))
			defer target.Close()
			path := filepath.Join(t.TempDir(), "state.db")
			h, e := assembly.Open(path, "u", "d")
			if e != nil {
				t.Fatal(e)
			}
			_, c := prepareStartFault(t, h, target.URL)
			writeStart(t, path, c)
			h.Close()
			child := exec.Command(os.Args[0], "-test.run=^TestDispatchSyncChild$")
			child.Env = append(os.Environ(), "LERNA_DISPATCH_SYNC_DB="+path, "LERNA_DISPATCH_SYNC_MODE="+mode)
			out, e := child.CombinedOutput()
			if e != nil {
				t.Fatalf("child %v %s", e, out)
			}
			b, e := os.ReadFile(path + ".sync-result")
			if e != nil {
				t.Fatal(e)
			}
			var result struct {
				Failed         bool
				NativeFailures uint64
			}
			if e = json.Unmarshal(b, &result); e != nil {
				t.Fatal(e)
			}
			if !result.Failed || calls.Load() != 0 || mode == "native" && result.NativeFailures == 0 {
				t.Fatalf("barrier not respected: %+v calls=%d", result, calls.Load())
			}
		})
	}
}

// 规则：G3、G5
func TestDispatchSyncChild(t *testing.T) {
	path := os.Getenv("LERNA_DISPATCH_SYNC_DB")
	if path == "" {
		t.Skip("subprocess only")
	}
	if e := storagevfs.Register(path + ".trace"); e != nil {
		t.Fatal(e)
	}
	h, e := assembly.Open(path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	c := readStart(t, path)
	ctx := context.Background()
	caller := &v1.Caller{UserId: "u", IssuerId: "egress"}
	r, e := h.Tasks.StartExecution(ctx, caller, c)
	requireAccepted(t, r, e)
	// P4 已提交；重放是只读，下一笔有脏页的提交必须是 P5。
	mode := 2
	if os.Getenv("LERNA_DISPATCH_SYNC_MODE") == "native" {
		mode = 3
	}
	storagevfs.ModeAfter(mode, 0)
	r, e = h.Egress.Invoke(ctx, caller, c)
	_, _, failures := storagevfs.NativeBarriers()
	b, _ := json.Marshal(struct {
		Failed         bool
		NativeFailures uint64
	}{e != nil && r == nil, failures})
	if e = os.WriteFile(path+".sync-result", b, 0600); e != nil {
		t.Fatal(e)
	}
	storagevfs.Mode(0)
}
func writeStart(t *testing.T, path string, c *v1.StartExecutionCommand) {
	t.Helper()
	b, e := proto.Marshal(c)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path+".start", b, 0600); e != nil {
		t.Fatal(e)
	}
}
func readStart(t *testing.T, path string) *v1.StartExecutionCommand {
	t.Helper()
	b, e := os.ReadFile(path + ".start")
	if e != nil {
		t.Fatal(e)
	}
	c := new(v1.StartExecutionCommand)
	if e = proto.Unmarshal(b, c); e != nil {
		t.Fatal(e)
	}
	return c
}
