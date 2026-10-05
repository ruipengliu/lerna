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

// 规则：G3、G4、G8
func TestCredentialCommitBoundaries(t *testing.T) {
	for _, mode := range []sqlite.FaultMode{sqlite.CrashBeforeCommit, sqlite.CrashAfterCommit, sqlite.LoseReceipt} {
		t.Run(string(mode), func(t *testing.T) {
			var calls atomic.Int64
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(200) }))
			defer target.Close()
			path := filepath.Join(t.TempDir(), "credential.db")
			h, e := assembly.Open(path, "u", "d")
			if e != nil {
				t.Fatal(e)
			}
			caller := &v1.Caller{UserId: "u", IssuerId: "host"}
			c := prepareAdmission(t, h, target.URL)
			r, e := h.Tasks.Admit(context.Background(), caller, c)
			requireAccepted(t, r, e)
			h.Close()
			if mode == sqlite.LoseReceipt {
				h, e = assembly.Open(path, "u", "d")
				if e != nil {
					t.Fatal(e)
				}
				ctx, e := sqlite.WithFault(context.Background(), "grants.credential", mode)
				if e != nil {
					t.Fatal(e)
				}
				r, e = issueFaultCredential(t, h, ctx)
				if e == nil || r != nil {
					t.Fatalf("lost receipt: %v %v", r, e)
				}
				h.Close()
			} else {
				child := exec.Command(os.Args[0], "-test.run=^TestCredentialCrashChild$")
				child.Env = append(os.Environ(), "LERNA_CREDENTIAL_DB="+path, "LERNA_CREDENTIAL_MODE="+string(mode))
				out, e := child.CombinedOutput()
				var exit *exec.ExitError
				if !errors.As(e, &exit) || exit.ExitCode() != sqlite.CrashExitCode {
					t.Fatalf("fault not hit: %v %s", e, out)
				}
			}
			h, e = assembly.Open(path, "u", "d")
			if e != nil {
				t.Fatal(e)
			}
			defer h.Close()
			q, e := h.Durable.QueryReceipt(context.Background(), caller, admissionHeader("credential").Identity)
			if e != nil {
				t.Fatal(e)
			}
			if mode == sqlite.CrashBeforeCommit {
				if q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND {
					t.Fatalf("partial credential: %v", q)
				}
			} else if q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED {
				t.Fatalf("lost credential: %v", q)
			}
			r, e = issueFaultCredential(t, h, context.Background())
			requireAccepted(t, r, e)
			if mode != sqlite.CrashBeforeCommit && !proto.Equal(r, q.Receipt) {
				t.Fatal("changed original credential receipt")
			}
			cred, e := h.Grants.QueryCredential(context.Background(), caller, r.ResultRef)
			if e != nil || cred == nil || cred.State != "ISSUED" {
				t.Fatalf("missing credential: %v %v", cred, e)
			}
			if calls.Load() != 0 {
				t.Fatal("issuance made physical call")
			}
		})
	}
}

// 规则：G3
func TestCredentialCrashChild(t *testing.T) {
	path := os.Getenv("LERNA_CREDENTIAL_DB")
	if path == "" {
		t.Skip("subprocess only")
	}
	h, e := assembly.Open(path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	ctx, e := sqlite.WithFault(context.Background(), "grants.credential", sqlite.FaultMode(os.Getenv("LERNA_CREDENTIAL_MODE")))
	if e != nil {
		t.Fatal(e)
	}
	r, e := issueFaultCredential(t, h, ctx)
	t.Fatalf("fault not reached: %v %v", r, e)
}
func issueFaultCredential(t *testing.T, h *assembly.Harness, ctx context.Context) (*v1.CommandReceipt, error) {
	t.Helper()
	caller := &v1.Caller{UserId: "u", IssuerId: "host"}
	q, e := h.Durable.QueryReceipt(context.Background(), caller, admissionHeader("admit").Identity)
	if e != nil {
		t.Fatal(e)
	}
	a, e := h.Tasks.QueryAdmission(context.Background(), caller, q.Receipt.ResultRef)
	if e != nil {
		t.Fatal(e)
	}
	g, e := h.Grants.QueryGrant(context.Background(), caller, a.GrantRefs[0])
	if e != nil {
		t.Fatal(e)
	}
	b := &v1.ExitCredentialBinding{UserId: "u", TaskId: a.TaskId, OperationId: a.OperationId, AttemptId: &v1.GlobalName{UserId: "u", AuthorityDomainId: a.LedgerDomainId, ObjectKind: "attempt", LocalId: "original-attempt"}, SendSeq: 1, AdmissionRef: a.Ref, GrantUseRef: a.GrantUseRef, SubjectId: a.TaskId, CallerIssuerId: "egress", Audience: "egress", ExecutorEndpointId: a.ExecutorEndpointId, ExecutorInstance: "original-instance", DescriptorDigest: "fixed", UseRight: "INVOKE", ProcessingPurpose: "CURRENT_TASK", RequirementsVersion: a.RequirementsVersion, InputVersion: a.InputVersion, ControlGeneration: a.ControlGeneration, BudgetReservationRef: a.BudgetBasis.ReservationRef}
	return h.Grants.IssueCredential(ctx, caller, &v1.IssueExitCredentialCommand{Header: admissionHeader("credential"), AdmissionRef: a.Ref, Binding: b, ExpiresAtUnixMs: g.ValidUntilUnixMs})
}
