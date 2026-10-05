//go:build fault

package fault_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/infra/sqlite"
	"google.golang.org/protobuf/proto"
)

// 规则：G3、G4、G7、G10
func TestAdmissionConfigurationCommitBoundaries(t *testing.T) {
	for _, point := range []string{"tasks.planning", "grants.configure", "budget.configure"} {
		for _, mode := range []sqlite.FaultMode{sqlite.CrashBeforeCommit, sqlite.CrashAfterCommit, sqlite.LoseReceipt} {
			t.Run(point+"/"+string(mode), func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "config.db")
				h, e := assembly.Open(path, "u", "d")
				if e != nil {
					t.Fatal(e)
				}
				seedConfigGoal(t, h)
				h.Close()
				if mode == sqlite.LoseReceipt {
					h, e = assembly.Open(path, "u", "d")
					if e != nil {
						t.Fatal(e)
					}
					ctx, e := sqlite.WithFault(context.Background(), point, mode)
					if e != nil {
						t.Fatal(e)
					}
					r, e := applyConfig(t, h, ctx, point)
					if e == nil || r != nil {
						t.Fatalf("lost receipt leaked: %v %v", r, e)
					}
					h.Close()
				} else {
					child := exec.Command(os.Args[0], "-test.run=^TestAdmissionConfigCrashChild$")
					child.Env = append(os.Environ(), "LERNA_CONFIG_DB="+path, "LERNA_CONFIG_POINT="+point, "LERNA_CONFIG_MODE="+string(mode))
					out, e := child.CombinedOutput()
					var exit *exec.ExitError
					if !errors.As(e, &exit) || exit.ExitCode() != sqlite.CrashExitCode {
						t.Fatalf("fault not reached: %v %s", e, out)
					}
				}
				h, e = assembly.Open(path, "u", "d")
				if e != nil {
					t.Fatal(e)
				}
				defer h.Close()
				caller := &v1.Caller{UserId: "u", IssuerId: "host"}
				q, e := h.Durable.QueryReceipt(context.Background(), caller, admissionHeader("config").Identity)
				if e != nil {
					t.Fatal(e)
				}
				if mode == sqlite.CrashBeforeCommit {
					if q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND {
						t.Fatalf("partial config: %v", q)
					}
				} else if q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED {
					t.Fatalf("lost committed config: %v", q)
				}
				r, e := applyConfig(t, h, context.Background(), point)
				requireAccepted(t, r, e)
				if mode != sqlite.CrashBeforeCommit && !proto.Equal(r, q.Receipt) {
					t.Fatalf("original config changed: %v %v", r, q)
				}
				switch point {
				case "tasks.planning":
					v, e := h.Tasks.QueryRequirements(context.Background(), caller, r.ResultRef)
					if e != nil || v == nil || v.Source != "TRUSTED_TEMPLATE" {
						t.Fatalf("missing requirements %v %v", v, e)
					}
				case "grants.configure":
					v, e := h.Grants.QueryGrant(context.Background(), caller, r.ResultRef)
					if e != nil || v == nil || v.Status != "ACTIVE" {
						t.Fatalf("missing grant %v %v", v, e)
					}
				case "budget.configure":
					v, e := h.Budget.QueryBudget(context.Background(), caller, nil)
					if e != nil || v == nil || v.Limit != 100 || v.Reserved != 0 {
						t.Fatalf("missing budget %v %v", v, e)
					}
				}
			})
		}
	}
}

// 规则：G3
func TestAdmissionConfigCrashChild(t *testing.T) {
	path := os.Getenv("LERNA_CONFIG_DB")
	if path == "" {
		t.Skip("subprocess only")
	}
	h, e := assembly.Open(path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	ctx, e := sqlite.WithFault(context.Background(), os.Getenv("LERNA_CONFIG_POINT"), sqlite.FaultMode(os.Getenv("LERNA_CONFIG_MODE")))
	if e != nil {
		t.Fatal(e)
	}
	r, e := applyConfig(t, h, ctx, os.Getenv("LERNA_CONFIG_POINT"))
	t.Fatalf("fault not reached: %v %v", r, e)
}
func seedConfigGoal(t *testing.T, h *assembly.Harness) {
	t.Helper()
	ctx := context.Background()
	caller := &v1.Caller{UserId: "u", IssuerId: "host"}
	_, e := h.Sessions.SubmitGoal(ctx, caller, &v1.SubmitGoalCommand{Identity: admissionHeader("goal").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "create record"})
	if e != nil {
		t.Fatal(e)
	}
	if e = h.Sessions.ProcessPending(ctx, caller); e != nil {
		t.Fatal(e)
	}
}
func applyConfig(t *testing.T, h *assembly.Harness, ctx context.Context, point string) (*v1.CommandReceipt, error) {
	t.Helper()
	caller := &v1.Caller{UserId: "u", IssuerId: "host"}
	q, e := h.Durable.QueryReceipt(context.Background(), caller, admissionHeader("goal").Identity)
	if e != nil {
		t.Fatal(e)
	}
	switch point {
	case "tasks.planning":
		return h.Tasks.AcceptRequirements(ctx, caller, &v1.AcceptRequirementsCommand{Header: admissionHeader("config"), TaskRef: q.Receipt.TaskRef, InputVersion: 1, Source: "TRUSTED_TEMPLATE", Conditions: []*v1.Requirement{{ConditionId: "created", DescriptionRef: q.Receipt.InputRef, Necessary: true, VerificationRule: "TARGET_RECORD", RuleVersion: 1}}})
	case "grants.configure":
		return h.Grants.Configure(ctx, caller, &v1.ConfigureGrantCommand{Header: admissionHeader("config"), Grant: &v1.Grant{Subject: q.Receipt.TaskRef.Name, Permissions: []*v1.PermissionClause{{Action: "CREATE", Resource: "record", UseRight: "INVOKE", ProcessingPurpose: "CURRENT_TASK", ExecutorEndpointId: "local-api"}}, ValidFromUnixMs: 1, ValidUntilUnixMs: 4000000000000, UseMode: "CONTINUOUS", UsePoolId: "root"}})
	case "budget.configure":
		return h.Budget.Configure(ctx, caller, &v1.ConfigureBudgetCommand{Header: admissionHeader("config"), Unit: "USD_MICRO", Limit: 100})
	}
	t.Fatalf("unknown point: %s", point)
	return nil, nil
}
