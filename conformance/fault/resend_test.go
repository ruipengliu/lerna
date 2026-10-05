//go:build fault

package fault_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/infra/sqlite"
	"google.golang.org/protobuf/proto"
)

func prepareResendFault(t *testing.T, h *assembly.Harness, target string) (*v1.Admission, *v1.StartExecutionCommand, *v1.PrepareResendCommand) {
	t.Helper()
	ctx := context.Background()
	caller := &v1.Caller{UserId: "u", IssuerId: "host"}
	c := prepareAdmissionAdapter(t, h, target, "simulator-idempotent", 2)
	r, e := h.Tasks.Admit(ctx, caller, c)
	requireAccepted(t, r, e)
	a, e := h.Tasks.QueryAdmission(ctx, caller, r.ResultRef)
	if e != nil {
		t.Fatal(e)
	}
	if e = h.Tasks.ProcessHandoffs(ctx, caller); e != nil {
		t.Fatal(e)
	}
	_, first := prepareExistingAdmissionStartFault(t, h, a)
	r, e = h.Egress.Invoke(ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, first)
	requireAccepted(t, r, e)
	x, e := h.Ledger.QueryExecution(ctx, caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	claim, e := h.LedgerWork.ExecuteJob(ctx, caller, &v1.JobCommand{Identity: executionHeader("resend-claim").Identity, ContractVersion: 1, Action: "CLAIM", AllowedTypes: []string{"EXECUTE_OPERATION"}, Limit: 1, LeaseMs: 30000, ProcessInstance: "second-worker"})
	requireAccepted(t, claim, e)
	if len(claim.Jobs) != 1 {
		t.Fatalf("claim %v", claim)
	}
	return a, first, &v1.PrepareResendCommand{Header: executionHeader("resend"), OperationId: a.OperationId, PreviousSendRef: x.Send.Ref, Claim: claim.Jobs[0]}
}
func faultResendStart(t *testing.T, h *assembly.Harness, first *v1.StartExecutionCommand, c *v1.PrepareResendCommand) *v1.StartExecutionCommand {
	t.Helper()
	ctx := context.Background()
	caller := &v1.Caller{UserId: "u", IssuerId: "host"}
	x, e := h.Ledger.QueryExecution(ctx, caller, c.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	b := proto.Clone(first.Binding).(*v1.ExitCredentialBinding)
	b.SendSeq = x.Send.SendSeq
	b.ExecutorInstance = x.Send.ProcessInstance
	r, e := h.Grants.IssueCredential(ctx, caller, &v1.IssueExitCredentialCommand{Header: admissionHeader("resend-credential"), AdmissionRef: first.AdmissionRef, Binding: b, ExpiresAtUnixMs: c.Claim.LeaseUntilUnixMs + time.Minute.Milliseconds()})
	requireAccepted(t, r, e)
	hdr := admissionHeader("start:" + x.Send.Ref.Name.LocalId)
	hdr.Identity.IssuerId = "egress"
	return &v1.StartExecutionCommand{Header: hdr, AdmissionRef: first.AdmissionRef, CredentialRef: r.ResultRef, Binding: b, CallDescriptor: x.CallDescriptor, Claim: c.Claim}
}

// 规则：G1、G3、G5、G10、G11、R7
func TestResendCrashBoundariesPreserveIdentityAndIndependentFees(t *testing.T) {
	for _, point := range []string{"ledger.resend", "tasks.start", "ledger.dispatch", "content.observation", "ledger.observation", "ledger.interpret"} {
		for _, mode := range []sqlite.FaultMode{sqlite.CrashBeforeCommit, sqlite.CrashAfterCommit, sqlite.LoseReceipt} {
			t.Run(point+"/"+string(mode), func(t *testing.T) {
				target := simulator.New("idempotent")
				target.SetBehavior("drop-after-apply")
				server := httptest.NewServer(target)
				defer server.Close()
				path := filepath.Join(t.TempDir(), "resend.db")
				h, e := assembly.Open(path, "u", "d")
				if e != nil {
					t.Fatal(e)
				}
				a, first, c := prepareResendFault(t, h, server.URL)
				before, e := h.Ledger.QueryExecution(context.Background(), &v1.Caller{UserId: "u", IssuerId: "host"}, a.OperationId)
				if e != nil {
					t.Fatal(e)
				}
				bytes, e := proto.Marshal(c)
				if e != nil {
					t.Fatal(e)
				}
				if e = os.WriteFile(path+".resend", bytes, 0600); e != nil {
					t.Fatal(e)
				}
				writeStart(t, path, first)
				h.Close()
				target.SetBehavior("")
				child := exec.Command(os.Args[0], "-test.run=^TestResendCrashChild$")
				child.Env = append(os.Environ(), "LERNA_RESEND_DB="+path, "LERNA_RESEND_POINT="+point, "LERNA_RESEND_MODE="+string(mode))
				out, e := child.CombinedOutput()
				if mode == sqlite.LoseReceipt {
					if e != nil {
						t.Fatalf("child %v %s", e, out)
					}
				} else {
					var exit *exec.ExitError
					if !errors.As(e, &exit) || exit.ExitCode() != sqlite.CrashExitCode {
						t.Fatalf("not crashed %v %s", e, out)
					}
				}
				h, e = assembly.Open(path, "u", "d")
				if e != nil {
					t.Fatal(e)
				}
				defer h.Close()
				ctx := context.Background()
				caller := &v1.Caller{UserId: "u", IssuerId: "host"}
				r, e := h.Ledger.PrepareResend(ctx, caller, c)
				requireAccepted(t, r, e)
				x, e := h.Ledger.QueryExecution(ctx, caller, a.OperationId)
				if e != nil {
					t.Fatal(e)
				}
				if x.Send.SendSeq != 2 || len(x.PreviousSends) != 1 || !proto.Equal(x.Attempt.Ref.Name, before.Attempt.Ref.Name) || !proto.Equal(x.CallDescriptor, before.CallDescriptor) {
					t.Fatalf("identity changed %v", x)
				}
				second := faultResendStart(t, h, first, c)
				r, e = h.Egress.Invoke(ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, second)
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
				requests, effects := target.Snapshot()
				want := 2
				if point == "ledger.dispatch" && mode != sqlite.CrashBeforeCommit {
					want = 1
				}
				if len(requests) != want || len(effects) != 1 {
					t.Fatalf("unexpected resend: requests=%v effects=%v", requests, effects)
				}
				reservations, e := h.Budget.QueryReservations(ctx, caller, a.TaskId)
				if e != nil || len(reservations) != 2 {
					t.Fatalf("fees: %v %v", reservations, e)
				}
				source, e := h.Budget.QueryBillingSource(ctx, caller, before.Send.Ref)
				if e != nil || source.Amount != nil || source.Status != "PENDING" {
					t.Fatalf("original fee erased %v %v", source, e)
				}
				op, e := h.Ledger.QueryOperation(ctx, caller, a.OperationId)
				if e != nil {
					t.Fatal(e)
				}
				lost := point == "ledger.dispatch" && mode != sqlite.CrashBeforeCommit || point == "content.observation" && mode == sqlite.CrashBeforeCommit
				if lost && (op.Effect.Outcome != "UNKNOWN" || op.Effect.LateEffect != "MAY_OCCUR") {
					t.Fatalf("lost evidence promoted %v", op)
				}
			})
		}
	}
}

// 规则：G3、G5、G11、R7
func TestResendCrashChild(t *testing.T) {
	path := os.Getenv("LERNA_RESEND_DB")
	if path == "" {
		t.Skip("subprocess only")
	}
	h, e := assembly.Open(path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	bytes, e := os.ReadFile(path + ".resend")
	if e != nil {
		t.Fatal(e)
	}
	c := new(v1.PrepareResendCommand)
	if e = proto.Unmarshal(bytes, c); e != nil {
		t.Fatal(e)
	}
	ctx, e := sqlite.WithFault(context.Background(), os.Getenv("LERNA_RESEND_POINT"), sqlite.FaultMode(os.Getenv("LERNA_RESEND_MODE")))
	if e != nil {
		t.Fatal(e)
	}
	caller := &v1.Caller{UserId: "u", IssuerId: "host"}
	r, e := h.Ledger.PrepareResend(ctx, caller, c)
	if e == nil {
		requireAccepted(t, r, e)
		second := faultResendStart(t, h, readStart(t, path), c)
		r, e = h.Egress.Invoke(ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, second)
	}
	if os.Getenv("LERNA_RESEND_MODE") == string(sqlite.LoseReceipt) {
		if e == nil {
			t.Fatalf("receipt loss not reached %v", r)
		}
		return
	}
	t.Fatal("fault not reached")
}

// 规则：G1、G3、G5、G10、G11
func TestResendActualSyncFailurePreventsSecondIO(t *testing.T) {
	for _, mode := range []string{"vfs", "native"} {
		if mode == "native" && runtime.GOOS != "darwin" {
			continue
		}
		t.Run(mode, func(t *testing.T) {
			target := simulator.New("idempotent")
			target.SetBehavior("drop-after-apply")
			server := httptest.NewServer(target)
			defer server.Close()
			path := filepath.Join(t.TempDir(), "resend-sync.db")
			h, e := assembly.Open(path, "u", "d")
			if e != nil {
				t.Fatal(e)
			}
			a, first, c := prepareResendFault(t, h, server.URL)
			r, e := h.Ledger.PrepareResend(context.Background(), &v1.Caller{UserId: "u", IssuerId: "host"}, c)
			requireAccepted(t, r, e)
			second := faultResendStart(t, h, first, c)
			writeStart(t, path, second)
			h.Close()
			child := exec.Command(os.Args[0], "-test.run=^TestDispatchSyncChild$")
			child.Env = append(os.Environ(), "LERNA_DISPATCH_SYNC_DB="+path, "LERNA_DISPATCH_SYNC_MODE="+mode)
			if out, e := child.CombinedOutput(); e != nil {
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
			if !result.Failed || mode == "native" && result.NativeFailures == 0 {
				t.Fatalf("barrier not exercised %+v", result)
			}
			requests, effects := target.Snapshot()
			if len(requests) != 1 || len(effects) != 1 {
				t.Fatalf("second IO before durable P5 %v %v", requests, effects)
			}
			h, e = assembly.Open(path, "u", "d")
			if e != nil {
				t.Fatal(e)
			}
			defer h.Close()
			op, e := h.Ledger.QueryOperation(context.Background(), &v1.Caller{UserId: "u", IssuerId: "host"}, a.OperationId)
			if e != nil || op.Effect.Outcome != "UNKNOWN" || op.Effect.LateEffect != "MAY_OCCUR" {
				t.Fatalf("original unknown erased %v %v", op, e)
			}
		})
	}
}
