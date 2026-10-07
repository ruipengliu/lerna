//go:build fault

package fault_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/durable"
	"github.com/ruipengliu/lerna/core/ledger"
	"github.com/ruipengliu/lerna/infra/sqlite"
	"google.golang.org/protobuf/proto"
)

func prepareQueryFault(t *testing.T, h *assembly.Harness, target string) *v1.RequestReconciliationCommand {
	t.Helper()
	ctx := context.Background()
	caller := &v1.Caller{UserId: "u", IssuerId: "host"}
	c := prepareAdmissionAdapter(t, h, target, "simulator-queryable")
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
	r, e = h.Egress.Invoke(ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	requireAccepted(t, r, e)
	cap := proto.Clone(a.CapabilitySnapshot).(*v1.Capability)
	cap.Ref = nil
	cap.ApprovedBy = nil
	cap.Action = "QUERY"
	cap.UseRight = "READ"
	fee := int64(5)
	cap.FeeCeiling = &fee
	r, e = h.Tasks.ConfigureCapability(ctx, caller, &v1.ConfigureCapabilityCommand{Header: admissionHeader("query-cap"), Capability: cap})
	requireAccepted(t, r, e)
	capRef := r.ResultRef
	r, e = h.Grants.Configure(ctx, caller, &v1.ConfigureGrantCommand{Header: admissionHeader("query-grant"), Grant: &v1.Grant{Subject: a.TaskId, Permissions: []*v1.PermissionClause{{Action: "QUERY", Resource: target, UseRight: "READ", ProcessingPurpose: "CURRENT_TASK", ExecutorEndpointId: "local-api"}, {Action: "QUERY", Resource: target, UseRight: "SAVE", ProcessingPurpose: "CURRENT_TASK", ExecutorEndpointId: "local-api"}}, ValidFromUnixMs: 1, ValidUntilUnixMs: 4000000000000, UseMode: "CONTINUOUS", UsePoolId: "query-root"}})
	requireAccepted(t, r, e)
	return &v1.RequestReconciliationCommand{Header: executionHeader("request-query"), OperationId: a.OperationId, QueryCapabilityRef: capRef, ParametersRef: a.ParametersRef, GrantRef: r.ResultRef, PolicyVersion: 1, Limits: &v1.ReconciliationLimits{MaxChecks: 3, DeadlineUnixMs: time.Now().Add(time.Hour).UnixMilli(), MaxFee: 20, InitialDelayMs: 10, MaxDelayMs: 100}}
}

// 规则：G1、G3、G4、G5、G11、R7、V4
func TestReconciliationCrashBoundariesPreserveQueryIdentity(t *testing.T) {
	for _, point := range []string{"ledger.reconcile_pause_sealed", "ledger.reconcile_confirmation", "tasks.closure_confirmation", "ledger.reconcile_request", "ledger.reconcile_prepare", "tasks.closure_admit", "ledger.reconcile_admission_ack", "ledger.reconcile_control", "tasks.operation_progress", "ledger.progress_ack", "ledger.reconcile_pause", "ledger.accept", "tasks.handoff_receipt", "tasks.start", "ledger.dispatch", "content.observation", "ledger.observation", "ledger.interpret"} {
		for _, mode := range []sqlite.FaultMode{sqlite.CrashBeforeCommit, sqlite.CrashAfterCommit, sqlite.LoseReceipt} {
			t.Run(point+"/"+string(mode), func(t *testing.T) {
				target := simulator.New("queryable")
				target.SetBehavior("drop-after-apply")
				server := httptest.NewServer(target)
				defer server.Close()
				path := filepath.Join(t.TempDir(), "state.db")
				h, e := assembly.Open(path, "u", "d")
				if e != nil {
					t.Fatal(e)
				}
				c := prepareQueryFault(t, h, server.URL)
				if point == "tasks.closure_confirmation" {
					actor := &v1.Caller{UserId: "u", IssuerId: "host"}
					g, e := h.Grants.QueryGrant(context.Background(), actor, c.GrantRef)
					if e != nil {
						t.Fatal(e)
					}
					g.Ref = nil
					g.Issuer = nil
					g.Status = ""
					g.SemanticVersion = 0
					g.ConfirmationRequired = true
					r, e := h.Grants.Configure(context.Background(), actor, &v1.ConfigureGrantCommand{Header: admissionHeader("fault-confirmation-grant"), Grant: g})
					requireAccepted(t, r, e)
					c.GrantRef = r.ResultRef
				}
				b, e := proto.Marshal(c)
				if e != nil {
					t.Fatal(e)
				}
				if e = os.WriteFile(path+".query", b, 0600); e != nil {
					t.Fatal(e)
				}
				h.Close()
				child := exec.Command(os.Args[0], "-test.run=^TestReconciliationCrashChild$")
				child.Env = append(os.Environ(), "LERNA_QUERY_DB="+path, "LERNA_QUERY_POINT="+point, "LERNA_QUERY_MODE="+string(mode))
				out, e := child.CombinedOutput()
				if mode == sqlite.LoseReceipt {
					if e != nil {
						t.Fatalf("child: %v %s", e, out)
					}
				} else {
					var exit *exec.ExitError
					if !errors.As(e, &exit) || exit.ExitCode() != sqlite.CrashExitCode {
						t.Fatalf("not crashed: %v %s", e, out)
					}
				}
				time.Sleep(3200 * time.Millisecond)
				h, e = assembly.Open(path, "u", "d")
				if e != nil {
					t.Fatal(e)
				}
				defer h.Close()
				caller := &v1.Caller{UserId: "u", IssuerId: "host"}
				r, e := h.Ledger.RequestReconciliation(context.Background(), caller, c)
				requireAccepted(t, r, e)
				if e = h.Ledger.ProcessReconciliations(context.Background(), caller); e != nil {
					t.Fatal(e)
				}
				p, e := h.Ledger.QueryReconciliation(context.Background(), caller, c.OperationId)
				if e != nil {
					t.Fatal(e)
				}
				if point == "tasks.closure_confirmation" {
					if p.State != "PAUSED" || p.PauseReason != "CONFIRMATION_REQUIRED" {
						t.Fatalf("missing confirmation wait: %v", p)
					}
					q, e := h.Ledger.QueryReconciliationQuery(context.Background(), caller, p.ActiveQueryRef)
					if e != nil {
						t.Fatal(e)
					}
					requested, e := h.Tasks.RequestClosureConfirmation(context.Background(), caller, &v1.RequestClosureConfirmationCommand{Header: admissionHeader("fault-closure-confirmation"), WorkRef: q.Work.Ref})
					requireAccepted(t, requested, e)
					pending, e := h.Sessions.QueryConfirmation(context.Background(), caller, requested.ResultRef)
					if e != nil {
						t.Fatal(e)
					}
					approved, e := h.Sessions.RespondConfirmation(context.Background(), caller, &v1.RespondConfirmationCommand{Header: admissionHeader("fault-approve-closure"), ConfirmationRef: pending.Ref, BindingDigest: pending.BindingDigest, Decision: "APPROVE"})
					requireAccepted(t, approved, e)
					resumed, e := h.Ledger.ControlReconciliation(context.Background(), caller, &v1.ControlReconciliationCommand{Header: executionHeader("resume-confirmed-query"), OperationId: c.OperationId, ExpectedRevision: p.Ref.Revision, Action: "RESUME", ConfirmationRef: approved.ResultRef})
					requireAccepted(t, resumed, e)
					if e = h.Ledger.ProcessReconciliations(context.Background(), caller); e != nil {
						t.Fatal(e)
					}
					p, e = h.Ledger.QueryReconciliation(context.Background(), caller, c.OperationId)
					if e != nil {
						t.Fatal(e)
					}
				}
				if point == "ledger.reconcile_control" && mode != sqlite.CrashBeforeCommit {
					if p.State != "PAUSED" || p.PauseReason != "USER_PAUSED" || p.CheckCount != 0 {
						t.Fatalf("lost durable pause: %v", p)
					}
					r, e = h.Ledger.ControlReconciliation(context.Background(), caller, &v1.ControlReconciliationCommand{Header: executionHeader("resume-query"), OperationId: c.OperationId, ExpectedRevision: p.Ref.Revision, Action: "RESUME"})
					requireAccepted(t, r, e)
					if e = h.Ledger.ProcessReconciliations(context.Background(), caller); e != nil {
						t.Fatal(e)
					}
					p, e = h.Ledger.QueryReconciliation(context.Background(), caller, c.OperationId)
					if e != nil {
						t.Fatal(e)
					}
				}
				lostRaw := point == "content.observation" && mode == sqlite.CrashBeforeCommit
				sealed := point == "ledger.reconcile_pause_sealed"
				unknown := sealed || lostRaw || point == "ledger.reconcile_pause" || point == "ledger.dispatch" && mode != sqlite.CrashBeforeCommit
				if unknown {
					reason := "QUERY_RESULT_UNKNOWN"
					if sealed {
						reason = "DISPATCH_SEALED"
					}
					if p.State != "PAUSED" || p.PauseReason != reason {
						t.Fatalf("missing own responsibility: %v", p)
					}
				} else if p.State != "COMPLETED" {
					t.Fatalf("recovery incomplete: %v", p)
				}
				if p.CheckCount != 1 || len(p.QueryRefs) != 1 {
					t.Fatalf("duplicate query: %v", p)
				}
				requests, effects := target.Snapshot()
				want := 2
				if unknown && !lostRaw {
					want = 1
				}
				if len(requests) != want || len(effects) != 1 {
					t.Fatalf("resends: %v %v", requests, effects)
				}
				if e = h.Trace.Recover(context.Background(), caller); e != nil {
					t.Fatal(e)
				}
				view, e := h.Trace.Query(context.Background(), caller, &v1.TraceQuery{OperationId: c.OperationId})
				if e != nil || !view.Complete {
					t.Fatalf("reconciliation trace incomplete %v %v", view, e)
				}
				planFound := false
				for _, event := range view.Events {
					if event.EventType == "RECONCILIATION_"+p.State && proto.Equal(event.SourceRecordRef, p.Ref) {
						planFound = true
					}
				}
				if !planFound {
					t.Fatal("source transaction lost original reconciliation state")
				}
				for _, event := range view.Events {
					if event.Producer != "ledger" {
						continue
					}
					stored, e := h.Trace.QueryEvent(context.Background(), caller, event.Ref)
					if e != nil || !proto.Equal(stored, event) {
						t.Fatalf("recovered association changed %v %v", stored, e)
					}
				}
			})
		}
	}
}

// 规则：G3、G5、G11
func TestReconciliationCrashChild(t *testing.T) {
	path := os.Getenv("LERNA_QUERY_DB")
	if path == "" {
		t.Skip("subprocess only")
	}
	h, e := assembly.Open(path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	b, e := os.ReadFile(path + ".query")
	if e != nil {
		t.Fatal(e)
	}
	c := new(v1.RequestReconciliationCommand)
	if e = proto.Unmarshal(b, c); e != nil {
		t.Fatal(e)
	}
	point := os.Getenv("LERNA_QUERY_POINT")
	if point == "ledger.reconcile_pause_sealed" {
		point = "ledger.reconcile_pause"
	}
	ctx, e := sqlite.WithFault(context.Background(), point, sqlite.FaultMode(os.Getenv("LERNA_QUERY_MODE")))
	if e != nil {
		t.Fatal(e)
	}
	h.Ledger.WithWork(&shortQueryLeases{Service: h.LedgerWork})
	caller := &v1.Caller{UserId: "u", IssuerId: "host"}
	r, e := h.Ledger.RequestReconciliation(ctx, caller, c)
	if e == nil {
		requireAccepted(t, r, e)
		switch os.Getenv("LERNA_QUERY_POINT") {
		case "tasks.closure_confirmation":
			if e = h.Ledger.ProcessReconciliations(ctx, caller); e != nil {
				t.Fatal(e)
			}
			plan, err := h.Ledger.QueryReconciliation(ctx, caller, c.OperationId)
			if err != nil {
				t.Fatal(err)
			}
			query, err := h.Ledger.QueryReconciliationQuery(ctx, caller, plan.ActiveQueryRef)
			if err != nil {
				t.Fatal(err)
			}
			_, err = h.Tasks.RequestClosureConfirmation(ctx, caller, &v1.RequestClosureConfirmationCommand{Header: admissionHeader("fault-closure-confirmation"), WorkRef: query.Work.Ref})
			e = err
		case "ledger.reconcile_control":
			p, err := h.Ledger.QueryReconciliation(ctx, caller, c.OperationId)
			if err != nil {
				t.Fatal(err)
			}
			_, e = h.Ledger.ControlReconciliation(ctx, caller, &v1.ControlReconciliationCommand{Header: executionHeader("pause-query"), OperationId: c.OperationId, ExpectedRevision: p.Ref.Revision, Action: "PAUSE"})
		case "ledger.reconcile_pause_sealed":
			h.Ledger.WithReconciliation(h.Tasks, h.Grants, &sealedQueryFaultEgress{h: h, t: t, grant: c.GrantRef})
			if err := h.Ledger.ProcessReconciliations(context.Background(), caller); !errors.Is(err, context.Canceled) {
				t.Fatalf("worker did not stop: %v", err)
			}
			h.Ledger.WithReconciliation(h.Tasks, h.Grants, h.Egress)
			time.Sleep(3200 * time.Millisecond)
			e = h.Ledger.RecoverReconciliations(ctx, caller)
		case "ledger.reconcile_pause":
			interrupted, err := sqlite.WithFault(context.Background(), "ledger.dispatch", sqlite.LoseReceipt)
			if err != nil {
				t.Fatal(err)
			}
			if err = h.Ledger.ProcessReconciliations(interrupted, caller); err == nil {
				t.Fatal("P5 receipt not lost")
			}
			time.Sleep(3200 * time.Millisecond)
			e = h.Ledger.RecoverReconciliations(ctx, caller)
		default:
			e = h.Ledger.ProcessReconciliations(ctx, caller)
			if e == nil {
				e = h.Ledger.ProcessOperationProgress(ctx, caller)
			}
		}
	}
	if os.Getenv("LERNA_QUERY_MODE") == string(sqlite.LoseReceipt) {
		if e == nil {
			t.Fatal("fault not hit")
		}
		return
	}
	t.Fatal("fault not hit")
}

// shortQueryLeases 请求较短但真实经过单调领取检查的租约，减少崩溃接替测试的空等。
type shortQueryLeases struct{ *durable.Service }

var _ ledger.ExecutionWork = (*shortQueryLeases)(nil)

func (w *shortQueryLeases) ExecuteJob(ctx context.Context, c *v1.Caller, j *v1.JobCommand) (*v1.CommandReceipt, error) {
	if j.Action == "CLAIM" {
		j = proto.Clone(j).(*v1.JobCommand)
		j.LeaseMs = 3000
	}
	return w.Service.ExecuteJob(ctx, c, j)
}

type sealedQueryFaultEgress struct {
	h     *assembly.Harness
	t     *testing.T
	grant *v1.Ref
}

func (g *sealedQueryFaultEgress) Invoke(ctx context.Context, c *v1.Caller, cmd *v1.StartExecutionCommand) (*v1.CommandReceipt, error) {
	r, e := g.h.Tasks.StartExecution(ctx, c, cmd)
	requireAccepted(g.t, r, e)
	actor := &v1.Caller{UserId: "u", IssuerId: "host"}
	r, e = g.h.Grants.Revoke(ctx, actor, &v1.RevokeGrantCommand{Header: admissionHeader("seal-query-grant"), GrantId: g.grant.Name})
	requireAccepted(g.t, r, e)
	if e = g.h.Grants.ProcessRevocations(ctx); e != nil {
		g.t.Fatal(e)
	}
	return nil, context.Canceled
}
