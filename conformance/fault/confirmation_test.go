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

type confirmationFaultCommand interface {
	proto.Message
	GetHeader() *v1.CommandHeader
}

var confirmationFaultPoints = map[string]string{"operation-request": "tasks.confirmation", "grant-request": "grants.confirmation", "approve": "sessions.confirmation", "withdraw": "sessions.confirmation", "issue": "grants.configure", "revoke": "grants.revoke", "admit": "tasks.admit"}

// 规则：G3、G4、G6、G7、准入-9
func TestConfirmationLifecycleCommitBoundaries(t *testing.T) {
	for kind, point := range confirmationFaultPoints {
		for _, mode := range []sqlite.FaultMode{sqlite.CrashBeforeCommit, sqlite.CrashAfterCommit, sqlite.LoseReceipt} {
			t.Run(kind+"/"+string(mode), func(t *testing.T) {
				var calls atomic.Int64
				target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(200) }))
				defer target.Close()
				path := filepath.Join(t.TempDir(), "confirmation.db")
				h, e := assembly.Open(path, "u", "d")
				if e != nil {
					t.Fatal(e)
				}
				c := prepareConfirmationFault(t, h, target.URL, kind)
				h.Close()
				b, e := proto.Marshal(c)
				if e != nil {
					t.Fatal(e)
				}
				if e = os.WriteFile(path+".command", b, 0600); e != nil {
					t.Fatal(e)
				}
				if mode == sqlite.LoseReceipt {
					h, e = assembly.Open(path, "u", "d")
					if e != nil {
						t.Fatal(e)
					}
					ctx, e := sqlite.WithFault(context.Background(), point, mode)
					if e != nil {
						t.Fatal(e)
					}
					r, e := runConfirmationFault(h, ctx, c)
					if e == nil || r != nil {
						t.Fatalf("lost receipt: %v %v", r, e)
					}
					h.Close()
				} else {
					child := exec.Command(os.Args[0], "-test.run=^TestConfirmationLifecycleCrashChild$")
					child.Env = append(os.Environ(), "LERNA_CONFIRMATION_DB="+path, "LERNA_CONFIRMATION_KIND="+kind, "LERNA_CONFIRMATION_MODE="+string(mode))
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
				ctx := context.Background()
				caller := &v1.Caller{UserId: "u", IssuerId: "host"}
				q, e := h.Durable.QueryReceipt(ctx, caller, c.GetHeader().Identity)
				if e != nil {
					t.Fatal(e)
				}
				if mode == sqlite.CrashBeforeCommit {
					if q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND {
						t.Fatalf("partial decision: %v", q)
					}
				} else if q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED {
					t.Fatalf("lost decision: %v", q)
				}
				if ac, ok := c.(*v1.AdmitCommand); ok && mode == sqlite.CrashBeforeCommit {
					confirmation, e := h.Sessions.QueryCurrentConfirmation(ctx, caller, ac.ConfirmationRef.Name)
					if e != nil || confirmation.State != "APPROVED" {
						t.Fatalf("partial consumption: %v %v", confirmation, e)
					}
					uses, e := h.Grants.QueryUses(ctx, caller, ac.TaskId)
					if e != nil || len(uses) != 0 {
						t.Fatalf("partial use: %v %v", uses, e)
					}
					budget, e := h.Budget.QueryBudget(ctx, caller, nil)
					if e != nil || budget.Reserved != 0 {
						t.Fatalf("partial budget: %v %v", budget, e)
					}
				}
				r, e := runConfirmationFault(h, ctx, c)
				requireAccepted(t, r, e)
				if mode != sqlite.CrashBeforeCommit && !proto.Equal(r, q.Receipt) {
					t.Fatal("original receipt changed")
				}
				switch kind {
				case "operation-request", "grant-request", "approve", "withdraw":
					confirmation, e := h.Sessions.QueryConfirmation(ctx, caller, r.ResultRef)
					if e != nil || confirmation == nil {
						t.Fatalf("missing confirmation: %v %v", confirmation, e)
					}
					expected := "PENDING"
					if kind == "approve" {
						expected = "APPROVED"
					}
					if kind == "withdraw" {
						expected = "WITHDRAWN"
					}
					if confirmation.State != expected {
						t.Fatalf("confirmation state: %v", confirmation)
					}
					session, e := h.Sessions.QuerySession(ctx, caller, confirmation.SessionId)
					expectedEvents := 1
					if kind == "approve" {
						expectedEvents = 2
					}
					if kind == "withdraw" {
						expectedEvents = 3
					}
					if e != nil || len(session.Inputs) != expectedEvents {
						t.Fatalf("confirmation events: %v %v", session, e)
					}
					for _, input := range session.Inputs[1:] {
						delivery, e := h.Sessions.QueryInput(ctx, caller, &v1.Ref{Name: input.InputId, Revision: 1, SchemaId: "lerna.v1.SessionInput"})
						if e != nil || delivery == nil || !proto.Equal(delivery.Input, input) {
							t.Fatalf("missing exact event: %v %v", delivery, e)
						}
					}
				case "issue":
					grant, e := h.Grants.QueryGrant(ctx, caller, r.ResultRef)
					if e != nil || grant == nil || grant.Status != "ACTIVE" {
						t.Fatalf("missing grant: %v %v", grant, e)
					}
					confirmation, e := h.Sessions.QueryCurrentConfirmation(ctx, caller, c.(*v1.IssueGrantCommand).ConfirmationRef.Name)
					if e != nil || confirmation.State != "CONSUMED" || confirmation.GetConsumedGrantIssuanceRef() == nil {
						t.Fatalf("missing issuance consumption: %v %v", confirmation, e)
					}
				case "revoke":
					grant, e := h.Grants.QueryCurrentGrant(ctx, caller, c.(*v1.RevokeGrantCommand).GrantId)
					if e != nil || grant.Status != "REVOKED" || grant.RevocationCompletion != "COMPLETE" {
						t.Fatalf("missing revocation: %v %v", grant, e)
					}
				case "admit":
					ac := c.(*v1.AdmitCommand)
					confirmation, e := h.Sessions.QueryCurrentConfirmation(ctx, caller, ac.ConfirmationRef.Name)
					if e != nil || confirmation.State != "CONSUMED" || !proto.Equal(confirmation.GetConsumedAdmissionRef(), r.ResultRef) {
						t.Fatalf("missing consumption: %v %v", confirmation, e)
					}
					uses, e := h.Grants.QueryUses(ctx, caller, ac.TaskId)
					if e != nil || len(uses) != 1 {
						t.Fatalf("duplicate use: %v %v", uses, e)
					}
				}
				if calls.Load() != 0 {
					t.Fatal("authority command called target")
				}
			})
		}
	}
}

// 规则：G3
func TestConfirmationLifecycleCrashChild(t *testing.T) {
	path := os.Getenv("LERNA_CONFIRMATION_DB")
	if path == "" {
		t.Skip("subprocess only")
	}
	kind := os.Getenv("LERNA_CONFIRMATION_KIND")
	h, e := assembly.Open(path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(path + ".command")
	if e != nil {
		t.Fatal(e)
	}
	var c confirmationFaultCommand
	switch kind {
	case "operation-request":
		c = &v1.RequestAdmissionConfirmationCommand{}
	case "grant-request":
		c = &v1.RequestGrantConfirmationCommand{}
	case "approve":
		c = &v1.RespondConfirmationCommand{}
	case "withdraw":
		c = &v1.WithdrawConfirmationCommand{}
	case "issue":
		c = &v1.IssueGrantCommand{}
	case "revoke":
		c = &v1.RevokeGrantCommand{}
	case "admit":
		c = &v1.AdmitCommand{}
	}
	if e = proto.Unmarshal(b, c); e != nil {
		t.Fatal(e)
	}
	ctx, e := sqlite.WithFault(context.Background(), confirmationFaultPoints[kind], sqlite.FaultMode(os.Getenv("LERNA_CONFIRMATION_MODE")))
	if e != nil {
		t.Fatal(e)
	}
	r, e := runConfirmationFault(h, ctx, c)
	t.Fatalf("fault not reached: %v %v", r, e)
}
func runConfirmationFault(h *assembly.Harness, ctx context.Context, c confirmationFaultCommand) (*v1.CommandReceipt, error) {
	caller := &v1.Caller{UserId: "u", IssuerId: "host"}
	switch c := c.(type) {
	case *v1.RequestAdmissionConfirmationCommand:
		return h.Tasks.RequestAdmissionConfirmation(ctx, caller, c)
	case *v1.RequestGrantConfirmationCommand:
		return h.Grants.RequestGrantConfirmation(ctx, caller, c)
	case *v1.RespondConfirmationCommand:
		return h.Sessions.RespondConfirmation(ctx, caller, c)
	case *v1.WithdrawConfirmationCommand:
		return h.Sessions.WithdrawConfirmation(ctx, caller, c)
	case *v1.IssueGrantCommand:
		return h.Grants.IssueGrant(ctx, caller, c)
	case *v1.RevokeGrantCommand:
		return h.Grants.Revoke(ctx, caller, c)
	case *v1.AdmitCommand:
		return h.Tasks.Admit(ctx, caller, c)
	}
	panic("unknown command")
}
func prepareConfirmationFault(t *testing.T, h *assembly.Harness, target, kind string) confirmationFaultCommand {
	t.Helper()
	a := prepareAdmission(t, h, target)
	ctx := context.Background()
	caller := &v1.Caller{UserId: "u", IssuerId: "host"}
	if kind == "revoke" {
		return &v1.RevokeGrantCommand{Header: admissionHeader("revoke"), GrantId: a.GrantRef.Name}
	}
	goal, e := h.Durable.QueryReceipt(ctx, caller, admissionHeader("goal").Identity)
	if e != nil {
		t.Fatal(e)
	}
	op := &v1.RequestAdmissionConfirmationCommand{SessionId: goal.Receipt.SessionRef.Name, Header: admissionHeader("confirmation-request"), TaskId: a.TaskId, ProposalRef: a.ProposalRef, GrantRef: a.GrantRef}
	if kind == "operation-request" {
		return op
	}
	var request confirmationFaultCommand = op
	if kind == "grant-request" || kind == "issue" {
		g, e := h.Grants.QueryGrant(ctx, caller, a.GrantRef)
		if e != nil {
			t.Fatal(e)
		}
		g.Ref = nil
		g.Issuer = nil
		g.Status = ""
		g.UsePoolId = ""
		request = &v1.RequestGrantConfirmationCommand{SessionId: goal.Receipt.SessionRef.Name, Header: admissionHeader("grant-confirmation-request"), Grant: g}
		if kind == "grant-request" {
			return request
		}
	}
	r, e := runConfirmationFault(h, ctx, request)
	requireAccepted(t, r, e)
	confirmation, e := h.Sessions.QueryConfirmation(ctx, caller, r.ResultRef)
	if e != nil {
		t.Fatal(e)
	}
	response := &v1.RespondConfirmationCommand{Header: admissionHeader("approve"), ConfirmationRef: confirmation.Ref, BindingDigest: confirmation.BindingDigest, Decision: "APPROVE"}
	if kind == "approve" {
		return response
	}
	r, e = runConfirmationFault(h, ctx, response)
	requireAccepted(t, r, e)
	if kind == "withdraw" {
		return &v1.WithdrawConfirmationCommand{Header: admissionHeader("withdraw"), ConfirmationRef: r.ResultRef}
	}
	if kind == "issue" {
		return &v1.IssueGrantCommand{Header: admissionHeader("issue"), ConfirmationRef: r.ResultRef}
	}
	a.ConfirmationRef = r.ResultRef
	return a
}
