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
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/infra/sqlite"
	"google.golang.org/protobuf/proto"
)

// 规则：G3、G10、R7
func TestBudgetUsageHandoffCrashKeepsOneChargeAndOriginalReceipt(t *testing.T) {
	for _, point := range []string{"ledger.observation", "budget.usage", "ledger.usage_ack"} {
		for _, mode := range []sqlite.FaultMode{sqlite.CrashBeforeCommit, sqlite.CrashAfterCommit, sqlite.LoseReceipt} {
			t.Run(point+"/"+string(mode), func(t *testing.T) {
				oracle := simulator.NewBillingTarget(25)
				target := httptest.NewServer(oracle)
				defer target.Close()
				path := filepath.Join(t.TempDir(), "budget.db")
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
				requireBudgetFault(t, mode, out, e)
				bills := oracle.Bills()
				if len(bills) != 1 || bills[0].Amount != 25 {
					t.Fatalf("provider %v", bills)
				}
				h, e = assembly.Open(path, "u", "d")
				if e != nil {
					t.Fatal(e)
				}
				defer h.Close()
				ctx := context.Background()
				host := &v1.Caller{UserId: "u", IssuerId: "host"}
				b, e := h.Budget.QueryBudget(ctx, host, nil)
				if e != nil || b.Settled != 25 || b.Reserved != 0 || b.Available != 75 {
					t.Fatalf("recovered fee %v %v", b, e)
				}
				x, e := h.Ledger.QueryExecution(ctx, host, a.OperationId)
				if e != nil {
					t.Fatal(e)
				}
				reports, e := h.Ledger.QueryReports(ctx, host, x.Send.ObservationRef)
				if e != nil {
					t.Fatal(e)
				}
				q, e := h.Durable.QueryReceipt(ctx, &v1.Caller{UserId: "u", IssuerId: "ledger-report"}, reports.Usage.Header.Identity)
				if e != nil || q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED || !proto.Equal(q.Receipt, reports.UsageReceipt) {
					t.Fatalf("original handoff %v %v", q, e)
				}
				first, e := h.Budget.QueryBillingSource(ctx, host, x.Send.Ref)
				if e != nil {
					t.Fatal(e)
				}
				if e = h.Ledger.ProcessReports(ctx, host); e != nil {
					t.Fatal(e)
				}
				again, e := h.Budget.QueryBillingSource(ctx, host, x.Send.Ref)
				if e != nil || !proto.Equal(first, again) || len(oracle.Bills()) != 1 {
					t.Fatalf("duplicate settlement %v %v", again, e)
				}
			})
		}
	}
}
func requireBudgetFault(t *testing.T, mode sqlite.FaultMode, out []byte, e error) {
	t.Helper()
	if mode == sqlite.LoseReceipt {
		if e != nil {
			t.Fatalf("receipt loss child %v %s", e, out)
		}
		return
	}
	var exit *exec.ExitError
	if !errors.As(e, &exit) || exit.ExitCode() != sqlite.CrashExitCode {
		t.Fatalf("fault not reached %v %s", e, out)
	}
}

// 规则：G3、G10、R7
func TestBudgetMutationCrashReturnsOriginalDecisionWithoutRepeatingBalances(t *testing.T) {
	for _, point := range []string{"budget.limit", "budget.import", "budget.release"} {
		for _, mode := range []sqlite.FaultMode{sqlite.CrashBeforeCommit, sqlite.CrashAfterCommit, sqlite.LoseReceipt} {
			t.Run(point+"/"+string(mode), func(t *testing.T) {
				ctx := context.Background()
				host := &v1.Caller{UserId: "u", IssuerId: "host"}
				egress := &v1.Caller{UserId: "u", IssuerId: "egress"}
				oracle := simulator.NewBillingTarget(25)
				oracle.DropReceipt(true)
				server := httptest.NewServer(oracle)
				defer server.Close()
				path := filepath.Join(t.TempDir(), "budget.db")
				h, e := assembly.Open(path, "u", "d")
				if e != nil {
					t.Fatal(e)
				}
				a, c := prepareStartFault(t, h, server.URL)
				switch point {
				case "budget.limit":
					b, e := h.Budget.QueryBudget(ctx, host, nil)
					if e != nil {
						t.Fatal(e)
					}
					writeBudgetMessage(t, path+".budget", &v1.AdjustBudgetLimitCommand{Header: admissionHeader("fault-limit"), ExpectedRef: b.Ref, Limit: 20, Reason: "explicit lower limit"})
				case "budget.import":
					r, e := h.Egress.Invoke(ctx, egress, c)
					requireAccepted(t, r, e)
					body, e := json.Marshal(map[string]any{"billing": oracle.Bills()[0]})
					if e != nil {
						t.Fatal(e)
					}
					evidence, e := h.Content.Stage(ctx, host, &v1.SubmitGoalCommand{Identity: admissionHeader("fault-statement").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: string(body)})
					if e != nil {
						t.Fatal(e)
					}
					x, e := h.Ledger.QueryExecution(ctx, host, a.OperationId)
					if e != nil {
						t.Fatal(e)
					}
					writeBudgetMessage(t, path+".budget", &v1.ImportBillCommand{Header: admissionHeader("fault-import"), SendRef: x.Send.Ref, EvidenceRef: evidence})
				case "budget.release":
					r, e := h.Tasks.StartExecution(ctx, egress, c)
					requireAccepted(t, r, e)
					writeBudgetMessage(t, path+".revoke", &v1.RevokeGrantCommand{Header: admissionHeader("fault-release-revoke"), GrantId: a.GrantRefs[0].Name})
					writeBudgetMessage(t, path+".budget", &v1.ReleaseReservationCommand{Header: admissionHeader("fault-release"), ReservationRef: a.BudgetBasis.ReservationRef})
				}
				h.Close()
				child := exec.Command(os.Args[0], "-test.run=^TestBudgetMutationCrashChild$")
				child.Env = append(os.Environ(), "LERNA_BUDGET_DB="+path, "LERNA_BUDGET_POINT="+point, "LERNA_BUDGET_MODE="+string(mode))
				out, e := child.CombinedOutput()
				requireBudgetFault(t, mode, out, e)
				h, e = assembly.Open(path, "u", "d")
				if e != nil {
					t.Fatal(e)
				}
				defer h.Close()
				cmd, header := readBudgetMessage(t, path+".budget", point)
				q, e := h.Durable.QueryReceipt(ctx, host, header.Identity)
				if e != nil {
					t.Fatal(e)
				}
				if mode == sqlite.CrashBeforeCommit {
					if q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND {
						t.Fatalf("partial original %v", q)
					}
				} else if q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED {
					t.Fatalf("lost original %v", q)
				}
				r, e := executeBudgetCommand(ctx, h, host, cmd)
				requireAccepted(t, r, e)
				if q.Receipt != nil && !proto.Equal(q.Receipt, r) {
					t.Fatal("replayed decision changed")
				}
				again, e := executeBudgetCommand(ctx, h, host, cmd)
				if e != nil || !proto.Equal(again, r) {
					t.Fatalf("repeat %v %v", again, e)
				}
				b, e := h.Budget.QueryBudget(ctx, host, nil)
				if e != nil {
					t.Fatal(e)
				}
				switch point {
				case "budget.limit":
					if b.Limit != 20 || b.Reserved != 30 || b.Deficit != 10 || len(oracle.Bills()) != 0 {
						t.Fatalf("limit %v", b)
					}
				case "budget.import":
					if b.Settled != 25 || b.Reserved != 0 || b.Available != 75 || len(oracle.Bills()) != 1 {
						t.Fatalf("import %v", b)
					}
				case "budget.release":
					if b.Settled != 0 || b.Reserved != 0 || b.Available != 100 || len(oracle.Bills()) != 0 {
						t.Fatalf("release %v", b)
					}
				}
			})
		}
	}
}
func writeBudgetMessage(t *testing.T, path string, m proto.Message) {
	t.Helper()
	b, e := proto.Marshal(m)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
}
func readBudgetMessage(t *testing.T, path, point string) (proto.Message, *v1.CommandHeader) {
	t.Helper()
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	switch point {
	case "budget.limit":
		c := new(v1.AdjustBudgetLimitCommand)
		if e = proto.Unmarshal(b, c); e != nil {
			t.Fatal(e)
		}
		return c, c.Header
	case "budget.import":
		c := new(v1.ImportBillCommand)
		if e = proto.Unmarshal(b, c); e != nil {
			t.Fatal(e)
		}
		return c, c.Header
	case "budget.release":
		c := new(v1.ReleaseReservationCommand)
		if e = proto.Unmarshal(b, c); e != nil {
			t.Fatal(e)
		}
		return c, c.Header
	}
	t.Fatal("unknown command")
	return nil, nil
}
func executeBudgetCommand(ctx context.Context, h *assembly.Harness, caller *v1.Caller, m proto.Message) (*v1.CommandReceipt, error) {
	switch c := m.(type) {
	case *v1.AdjustBudgetLimitCommand:
		return h.Budget.AdjustLimit(ctx, caller, c)
	case *v1.ImportBillCommand:
		return h.Budget.ImportBill(ctx, caller, c)
	case *v1.ReleaseReservationCommand:
		return h.Budget.ReleaseUnused(ctx, caller, c)
	}
	return nil, errors.New("unknown budget command")
}

// 规则：G3、G10、R7
func TestBudgetMutationCrashChild(t *testing.T) {
	path := os.Getenv("LERNA_BUDGET_DB")
	if path == "" {
		t.Skip("child only")
	}
	point := os.Getenv("LERNA_BUDGET_POINT")
	h, e := assembly.Open(path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	host := &v1.Caller{UserId: "u", IssuerId: "host"}
	ctx := context.Background()
	cmd, _ := readBudgetMessage(t, path+".budget", point)
	if point == "budget.release" {
		b, e := os.ReadFile(path + ".revoke")
		if e != nil {
			t.Fatal(e)
		}
		revoke := new(v1.RevokeGrantCommand)
		if e = proto.Unmarshal(b, revoke); e != nil {
			t.Fatal(e)
		}
		r, e := h.Grants.Revoke(ctx, host, revoke)
		requireAccepted(t, r, e)
		if e = h.Grants.ProcessRevocations(ctx); e != nil {
			t.Fatal(e)
		}
		state, e := h.Grants.QueryCurrentRevocation(ctx, host, r.ResultRef.Name)
		if e != nil {
			t.Fatal(e)
		}
		cmd.(*v1.ReleaseReservationCommand).ClosureRef = state.Closures[0].RecipientReceipt.ResultRef
		writeBudgetMessage(t, path+".budget", cmd)
	}
	ctx, e = sqlite.WithFault(ctx, point, sqlite.FaultMode(os.Getenv("LERNA_BUDGET_MODE")))
	if e != nil {
		t.Fatal(e)
	}
	_, e = executeBudgetCommand(ctx, h, host, cmd)
	if e == nil {
		t.Fatal("fault was not returned")
	}
}
