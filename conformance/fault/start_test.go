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
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/ruipengliu/lerna/cmd/assembly"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/infra/sqlite"
)

// 规则：G3、G4、开始-4
func TestStartContentUnavailableRollsBackAndRetriesOriginal(t *testing.T) {
	var calls atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(200) }))
	defer target.Close()
	h, e := assembly.Open(filepath.Join(t.TempDir(), "state.db"), "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	a, c := prepareStartFault(t, h, target.URL)
	caller := &v1.Caller{UserId: "u", IssuerId: "egress"}
	ctx := context.Background()
	r, e := h.Tasks.StartExecution(sqlite.WithContentReadUnavailable(ctx), caller, c)
	if e == nil || r != nil {
		t.Fatalf("dependency failure became accepted: %v %v", r, e)
	}
	q, e := h.Durable.QueryReceipt(ctx, caller, c.Header.Identity)
	if e != nil || q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND {
		t.Fatalf("infrastructure failure became permanent decision %v %v", q, e)
	}
	use, e := h.Grants.QueryCredentialUse(ctx, caller, c.CredentialRef)
	if e != nil || use != nil {
		t.Fatalf("credential leaked %v %v", use, e)
	}
	reservations, e := h.Budget.QueryReservations(ctx, caller, a.TaskId)
	if e != nil || reservations[0].ConsumedSends != 0 || calls.Load() != 0 {
		t.Fatalf("send leaked %v %v calls%d", reservations, e, calls.Load())
	}
	r, e = h.Tasks.StartExecution(ctx, caller, c)
	requireAccepted(t, r, e)
}
func prepareStartFault(t *testing.T, h *assembly.Harness, target string) (*v1.Admission, *v1.StartExecutionCommand) {
	t.Helper()
	a := admitExecution(t, h, target)
	ctx := context.Background()
	caller := &v1.Caller{UserId: "u", IssuerId: "host"}
	claim, e := h.LedgerWork.ExecuteJob(ctx, caller, &v1.JobCommand{Identity: executionHeader("claim").Identity, ContractVersion: 1, Action: "CLAIM", AllowedTypes: []string{"EXECUTE_OPERATION"}, Limit: 1, LeaseMs: 30000, ProcessInstance: "worker"})
	requireAccepted(t, claim, e)
	r, e := h.Ledger.Prepare(ctx, caller, &v1.PrepareExecutionCommand{Header: executionHeader("prepare"), OperationId: a.OperationId, ProcessInstance: "worker", Claim: claim.Jobs[0]})
	requireAccepted(t, r, e)
	x, e := h.Ledger.QueryExecution(ctx, caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	b := &v1.ExitCredentialBinding{UserId: "u", TaskId: a.TaskId, SubjectId: a.TaskId, OperationId: a.OperationId, AttemptId: x.Attempt.Ref.Name, SendSeq: x.Send.SendSeq, AdmissionRef: a.Ref, GrantUseRef: a.GrantUseRef, CallerIssuerId: "egress", Audience: "egress", ExecutorEndpointId: a.ExecutorEndpointId, ExecutorInstance: x.Send.ProcessInstance, DescriptorDigest: x.CallDescriptor.Digest, UseRight: a.CapabilitySnapshot.UseRight, ProcessingPurpose: a.CapabilitySnapshot.ProcessingPurpose, RequirementsVersion: a.RequirementsVersion, InputVersion: a.InputVersion, ControlGeneration: a.ControlGeneration, BudgetReservationRef: a.BudgetBasis.ReservationRef}
	r, e = h.Grants.IssueCredential(ctx, caller, &v1.IssueExitCredentialCommand{Header: admissionHeader("credential"), AdmissionRef: a.Ref, Binding: b, ExpiresAtUnixMs: time.Now().Add(time.Minute).UnixMilli()})
	requireAccepted(t, r, e)
	header := admissionHeader("start:" + x.Send.Ref.Name.LocalId)
	header.Identity.IssuerId = "egress"
	return a, &v1.StartExecutionCommand{Header: header, AdmissionRef: a.Ref, CredentialRef: r.ResultRef, Binding: b, Claim: claim.Jobs[0], CallDescriptor: x.CallDescriptor}
}

// 规则：G3、G4、开始-6
func TestUnqualifiedLiveConnectionCannotStart(t *testing.T) {
	var calls atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(200) }))
	defer target.Close()
	h, e := assembly.Open(filepath.Join(t.TempDir(), "state.db"), "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	a, c := prepareStartFault(t, h, target.URL)
	ctx := context.Background()
	caller := &v1.Caller{UserId: "u", IssuerId: "egress"}
	if e = h.StorageFaultSQL("PRAGMA synchronous=OFF"); e != nil {
		t.Fatal(e)
	}
	r, e := h.Tasks.StartExecution(ctx, caller, c)
	if e == nil || r != nil {
		t.Fatalf("unqualified connection started %v %v", r, e)
	}
	if e = h.StorageFaultSQL("PRAGMA synchronous=FULL"); e != nil {
		t.Fatal(e)
	}
	use, e := h.Grants.QueryCredentialUse(ctx, caller, c.CredentialRef)
	if e != nil || use != nil {
		t.Fatalf("credential leaked %v %v", use, e)
	}
	reservations, e := h.Budget.QueryReservations(ctx, caller, a.TaskId)
	if e != nil || reservations[0].ConsumedSends != 0 || calls.Load() != 0 {
		t.Fatalf("send allowance leaked %v %v", reservations, e)
	}
	r, e = h.Tasks.StartExecution(ctx, caller, c)
	requireAccepted(t, r, e)
}

// 规则：G3、G4、开始-5
func TestStartCrashAndLostReceiptKeepOneConsumption(t *testing.T) {
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
			b, e := proto.Marshal(c)
			if e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(path+".start", b, 0600); e != nil {
				t.Fatal(e)
			}
			h.Close()
			child := exec.Command(os.Args[0], "-test.run=^TestStartCrashChild$")
			child.Env = append(os.Environ(), "LERNA_START_DB="+path, "LERNA_START_MODE="+string(mode))
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
			ctx := context.Background()
			caller := &v1.Caller{UserId: "u", IssuerId: "egress"}
			q, e := h.Durable.QueryReceipt(ctx, caller, c.Header.Identity)
			if e != nil {
				t.Fatal(e)
			}
			use, e := h.Grants.QueryCredentialUse(ctx, caller, c.CredentialRef)
			if e != nil {
				t.Fatal(e)
			}
			reservations, e := h.Budget.QueryReservations(ctx, caller, a.TaskId)
			if e != nil {
				t.Fatal(e)
			}
			if mode == sqlite.CrashBeforeCommit {
				if q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND || use != nil || reservations[0].ConsumedSends != 0 {
					t.Fatalf("partial P4 %v %v %v", q, use, reservations)
				}
			} else if q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED || use == nil || reservations[0].ConsumedSends != 1 {
				t.Fatalf("lost P4 %v %v %v", q, use, reservations)
			}
			x, e := h.Ledger.QueryExecution(ctx, caller, a.OperationId)
			if e != nil || x.Send.Phase != "REGISTERED" || calls.Load() != 0 {
				t.Fatalf("P4 dispatched %v %v", x, e)
			}
		})
	}
}

// 规则：G3、开始-5
func TestStartCrashChild(t *testing.T) {
	path := os.Getenv("LERNA_START_DB")
	if path == "" {
		t.Skip("subprocess only")
	}
	h, e := assembly.Open(path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	b, e := os.ReadFile(path + ".start")
	if e != nil {
		t.Fatal(e)
	}
	c := new(v1.StartExecutionCommand)
	if e = proto.Unmarshal(b, c); e != nil {
		t.Fatal(e)
	}
	ctx, e := sqlite.WithFault(context.Background(), "tasks.start", sqlite.FaultMode(os.Getenv("LERNA_START_MODE")))
	if e != nil {
		t.Fatal(e)
	}
	caller := &v1.Caller{UserId: "u", IssuerId: "egress"}
	r, e := h.Tasks.StartExecution(ctx, caller, c)
	if os.Getenv("LERNA_START_MODE") == string(sqlite.LoseReceipt) {
		if e == nil || r != nil {
			t.Fatalf("leaked lost receipt %v %v", r, e)
		}
		r, e = h.Tasks.StartExecution(context.Background(), caller, c)
		requireAccepted(t, r, e)
		return
	}
	t.Fatal("fault not reached")
}
