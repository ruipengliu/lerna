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
	"github.com/ruipengliu/lerna/defaults/scripted"
	"github.com/ruipengliu/lerna/infra/sqlite"
	"google.golang.org/protobuf/proto"
)

// 规则：R7、G3、G4、G6、G10、R1
func TestAdmissionHandoffCrashesRecoverOriginalIdentity(t *testing.T) {
	for _, point := range []string{"tasks.admit", "ledger.accept", "tasks.handoff_receipt"} {
		for _, mode := range []sqlite.FaultMode{sqlite.CrashBeforeCommit, sqlite.CrashAfterCommit} {
			t.Run(point+"/"+string(mode), func(t *testing.T) {
				var calls atomic.Int64
				target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(200) }))
				defer target.Close()
				path := filepath.Join(t.TempDir(), "admission.db")
				h, e := assembly.Open(path, "u", "d")
				if e != nil {
					t.Fatal(e)
				}
				c := prepareAdmission(t, h, target.URL)
				h.Close()
				b, e := proto.Marshal(c)
				if e != nil {
					t.Fatal(e)
				}
				if e = os.WriteFile(path+".command", b, 0600); e != nil {
					t.Fatal(e)
				}
				child := exec.Command(os.Args[0], "-test.run=^TestAdmissionHandoffCrashChild$")
				child.Env = append(os.Environ(), "LERNA_ADMISSION_DB="+path, "LERNA_ADMISSION_POINT="+point, "LERNA_ADMISSION_MODE="+string(mode))
				out, e := child.CombinedOutput()
				var exit *exec.ExitError
				if !errors.As(e, &exit) || exit.ExitCode() != sqlite.CrashExitCode {
					t.Fatalf("fault did not terminate: %v %s", e, out)
				}
				h, e = assembly.Open(path, "u", "d")
				if e != nil {
					t.Fatal(e)
				}
				defer h.Close()
				caller := &v1.Caller{UserId: "u", IssuerId: "host"}
				ctx := context.Background()
				q, e := h.Durable.QueryReceipt(ctx, caller, c.Header.Identity)
				if e != nil {
					t.Fatal(e)
				}
				if point == "tasks.admit" && mode == sqlite.CrashBeforeCommit {
					if q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND {
						t.Fatalf("uncommitted P1 visible: %v", q)
					}
					r, e := h.Tasks.Admit(ctx, caller, c)
					requireAccepted(t, r, e)
					if e = h.Tasks.ProcessHandoffs(ctx, caller); e != nil {
						t.Fatal(e)
					}
				} else if q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED {
					t.Fatalf("lost source responsibility: %v", q)
				}
				assertOriginalHandoff(t, h, c)
				if calls.Load() != 0 {
					t.Fatalf("admission/handoff invoked target %d times", calls.Load())
				}
			})
		}
	}
}

// 规则：R7、G3
func TestAdmissionHandoffCrashChild(t *testing.T) {
	path := os.Getenv("LERNA_ADMISSION_DB")
	if path == "" {
		t.Skip("subprocess only")
	}
	h, e := assembly.Open(path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(path + ".command")
	if e != nil {
		t.Fatal(e)
	}
	c := new(v1.AdmitCommand)
	if e = proto.Unmarshal(b, c); e != nil {
		t.Fatal(e)
	}
	ctx, e := sqlite.WithFault(context.Background(), os.Getenv("LERNA_ADMISSION_POINT"), sqlite.FaultMode(os.Getenv("LERNA_ADMISSION_MODE")))
	if e != nil {
		t.Fatal(e)
	}
	caller := &v1.Caller{UserId: "u", IssuerId: "host"}
	r, e := h.Tasks.Admit(ctx, caller, c)
	requireAccepted(t, r, e)
	claim, e := h.Durable.ExecuteJob(ctx, caller, &v1.JobCommand{Identity: admissionHeader("claim").Identity, ContractVersion: 1, Action: "CLAIM", Module: "tasks", AllowedTypes: []string{"DELIVER_HANDOFF"}, Limit: 1, LeaseMs: 100, ProcessInstance: "crashing-worker"})
	requireAccepted(t, claim, e)
	if len(claim.Jobs) != 1 {
		t.Fatal(claim)
	}
	if e = h.Tasks.ProcessHandoffClaim(ctx, claim.Jobs[0]); e != nil {
		t.Fatal(e)
	}
	t.Fatal("fault not reached")
}

// 规则：R7、G3、G4、G10
func TestAdmissionHandoffLostReceiptsQueryBeforeRedelivery(t *testing.T) {
	for _, point := range []string{"tasks.admit", "ledger.accept", "tasks.handoff_receipt"} {
		t.Run(point, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "lost.db")
			h, e := assembly.Open(path, "u", "d")
			if e != nil {
				t.Fatal(e)
			}
			defer h.Close()
			c := prepareAdmission(t, h, "https://never-called.invalid")
			transport := &countedRecipient{recipient: h.Ledger}
			h.Tasks.WithHandoffs(h.Durable, transport)
			caller := &v1.Caller{UserId: "u", IssuerId: "host"}
			ctx, e := sqlite.WithFault(context.Background(), point, sqlite.LoseReceipt)
			if e != nil {
				t.Fatal(e)
			}
			r, e := h.Tasks.Admit(ctx, caller, c)
			if point == "tasks.admit" {
				if e == nil || r != nil {
					t.Fatalf("lost commit leaked receipt: %v %v", r, e)
				}
			} else {
				requireAccepted(t, r, e)
				claim, e := h.Durable.ExecuteJob(context.Background(), caller, &v1.JobCommand{Identity: admissionHeader("claim").Identity, ContractVersion: 1, Action: "CLAIM", Module: "tasks", AllowedTypes: []string{"DELIVER_HANDOFF"}, Limit: 1, LeaseMs: 30000, ProcessInstance: "worker"})
				requireAccepted(t, claim, e)
				if len(claim.Jobs) != 1 {
					t.Fatal(claim)
				}
				if e = h.Tasks.ProcessHandoffClaim(ctx, claim.Jobs[0]); e == nil {
					t.Fatal("lost receipt not injected")
				}
				if point == "ledger.accept" {
					if e = h.Tasks.ProcessHandoffClaim(context.Background(), claim.Jobs[0]); e != nil {
						t.Fatal(e)
					}
				}
			}
			r, e = h.Tasks.Admit(context.Background(), caller, c)
			requireAccepted(t, r, e)
			if e = h.Tasks.ProcessHandoffs(context.Background(), caller); e != nil {
				t.Fatal(e)
			}
			assertOriginalHandoff(t, h, c)
			if point == "ledger.accept" && (transport.accepts != 1 || transport.queries != 2) {
				t.Fatalf("retry did not query original: accepts=%d queries=%d", transport.accepts, transport.queries)
			}
		})
	}
}
func admissionHeader(id string) *v1.CommandHeader {
	return &v1.CommandHeader{Identity: &v1.CommandIdentity{UserId: "u", IssuerId: "host", TargetDomainId: "d", CommandId: id}, ContractVersion: 1, FingerprintVersion: 1, SchemaId: "lerna.v1.AdmissionCommands"}
}
func prepareAdmission(t *testing.T, h *assembly.Harness, target string) *v1.AdmitCommand {
	return prepareAdmissionAdapter(t, h, target, "simulator-idempotent")
}
func prepareAdmissionAdapter(t *testing.T, h *assembly.Harness, target, adapter string, maxSends ...uint32) *v1.AdmitCommand {
	t.Helper()
	ctx := context.Background()
	caller := &v1.Caller{UserId: "u", IssuerId: "host"}
	r, e := h.Sessions.SubmitGoal(ctx, caller, &v1.SubmitGoalCommand{Identity: admissionHeader("goal").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "create record"})
	if e != nil {
		t.Fatal(e)
	}
	if e = h.Sessions.ProcessPending(ctx, caller); e != nil {
		t.Fatal(e)
	}
	q, e := h.Durable.QueryReceipt(ctx, caller, r.Identity)
	if e != nil {
		t.Fatal(e)
	}
	task, parameters := q.Receipt.TaskRef, q.Receipt.InputRef
	r, e = h.Tasks.AcceptRequirements(ctx, caller, &v1.AcceptRequirementsCommand{Header: admissionHeader("requirements"), TaskRef: task, InputVersion: 1, Source: "TRUSTED_TEMPLATE", Conditions: []*v1.Requirement{{ConditionId: "created", DescriptionRef: parameters, Necessary: true, VerificationRule: "TARGET_RECORD", RuleVersion: 1}}})
	requireAccepted(t, r, e)
	ceiling := int64(30)
	sends := uint32(1)
	if len(maxSends) > 0 {
		sends = maxSends[0]
	}
	r, e = h.Tasks.ConfigureCapability(ctx, caller, &v1.ConfigureCapabilityCommand{Header: admissionHeader("cap"), Capability: &v1.Capability{Action: "CREATE", Resource: target, UseRight: "INVOKE", ProcessingPurpose: "CURRENT_TASK", ExecutorEndpointId: "local-api", AdapterRef: &v1.Ref{Name: &v1.GlobalName{UserId: "u", AuthorityDomainId: "adapter", ObjectKind: "adapter", LocalId: adapter}, Revision: 1, SchemaId: "lerna.v1.Adapter"}, Unit: "USD_MICRO", FeeCeiling: &ceiling, RateBasisRef: parameters, MaxSends: sends}})
	requireAccepted(t, r, e)
	capability := r.ResultRef
	for _, c := range []*v1.ConfigureBudgetCommand{{Header: admissionHeader("user-budget"), Unit: "USD_MICRO", Limit: 100}, {Header: admissionHeader("task-budget"), TaskId: task.Name, Unit: "USD_MICRO", Limit: 80}} {
		r, e = h.Budget.Configure(ctx, caller, c)
		requireAccepted(t, r, e)
	}
	r, e = h.Grants.Configure(ctx, caller, &v1.ConfigureGrantCommand{Header: admissionHeader("grant"), Grant: &v1.Grant{Subject: task.Name, Permissions: []*v1.PermissionClause{{Action: "CREATE", Resource: target, UseRight: "INVOKE", ProcessingPurpose: "CURRENT_TASK", ExecutorEndpointId: "local-api"}}, ValidFromUnixMs: 1, ValidUntilUnixMs: 4000000000000, UseMode: "CONTINUOUS", UsePoolId: "root"}})
	requireAccepted(t, r, e)
	grant := r.ResultRef
	snap, e := h.Tasks.RequestProposal(ctx, caller, &v1.RequestProposalCommand{Header: admissionHeader("request"), TaskId: task.Name})
	if e != nil {
		t.Fatal(e)
	}
	p, e := (&scripted.Reasoner{Step: &v1.ActionStep{StepId: "one", ParametersRef: parameters, CapabilityRef: capability}}).Propose(ctx, snap)
	if e != nil {
		t.Fatal(e)
	}
	r, e = h.Tasks.ReceiveProposal(ctx, caller, &v1.ReceiveProposalCommand{Header: admissionHeader("proposal"), Proposal: p})
	requireAccepted(t, r, e)
	return &v1.AdmitCommand{Header: admissionHeader("admit"), TaskId: task.Name, ProposalRef: r.ResultRef, GrantRef: grant}
}
func requireAccepted(t *testing.T, r *v1.CommandReceipt, e error) {
	t.Helper()
	if e != nil || r == nil || r.Decision != v1.Decision_DECISION_ACCEPTED {
		t.Fatalf("not accepted: %v %v", r, e)
	}
}
func assertOriginalHandoff(t *testing.T, h *assembly.Harness, c *v1.AdmitCommand) {
	t.Helper()
	ctx := context.Background()
	caller := &v1.Caller{UserId: "u", IssuerId: "host"}
	r, e := h.Tasks.Admit(ctx, caller, c)
	requireAccepted(t, r, e)
	p, e := h.Tasks.QueryPlanning(ctx, caller, c.TaskId)
	if e != nil || len(p.AdmissionRefs) != 1 || !p.ProposalConsumed {
		t.Fatalf("duplicate admission: %v %v", p, e)
	}
	a, e := h.Tasks.QueryAdmission(ctx, caller, r.ResultRef)
	if e != nil {
		t.Fatal(e)
	}
	outbox, e := h.Tasks.QueryHandoff(ctx, caller, a.Ref)
	if e != nil || outbox.State != "ACKNOWLEDGED" || outbox.RecipientReceipt == nil {
		t.Fatalf("handoff pending: %v %v", outbox, e)
	}
	q, e := h.Ledger.QueryReceipt(ctx, &v1.Caller{UserId: "u", IssuerId: "tasks-handoff"}, a.HandoffIdentity)
	if e != nil || !proto.Equal(q.Receipt, outbox.RecipientReceipt) {
		t.Fatalf("receipt changed: %v %v", q, e)
	}
	op, e := h.Ledger.QueryOperation(ctx, caller, a.OperationId)
	if e != nil || op == nil || len(op.AttemptRefs) != 0 || op.Effect.Outcome != "NOT_APPLIED" {
		t.Fatalf("operation changed: %v %v", op, e)
	}
	jobs, e := h.Ledger.QueryJobs(ctx, caller, a.OperationId)
	if e != nil || len(jobs) != 1 {
		t.Fatalf("duplicate receiver job: %v %v", jobs, e)
	}
	source, e := h.Durable.QueryJob(ctx, caller, outbox.JobRef.Name)
	if e != nil || source.State != "COMPLETED" {
		t.Fatalf("source incomplete: %v %v", source, e)
	}
	uses, e := h.Grants.QueryUses(ctx, caller, c.TaskId)
	if e != nil || len(uses) != 1 {
		t.Fatalf("duplicate use: %v %v", uses, e)
	}
	reservations, e := h.Budget.QueryReservations(ctx, caller, c.TaskId)
	if e != nil || len(reservations) != 1 || reservations[0].Ceiling != 30 {
		t.Fatalf("duplicate reservation: %v %v", reservations, e)
	}
	budget, e := h.Budget.QueryBudget(ctx, caller, nil)
	if e != nil || budget.Reserved != 30 || budget.Settled != 0 {
		t.Fatalf("budget changed: %v %v", budget, e)
	}
	before := proto.Clone(outbox.RecipientReceipt)
	again, e := h.Ledger.Accept(ctx, &v1.Caller{UserId: "u", IssuerId: "tasks-handoff"}, &v1.AcceptOperationCommand{Header: &v1.CommandHeader{Identity: a.HandoffIdentity, ContractVersion: 1, FingerprintVersion: 1, SchemaId: "lerna.v1.AdmissionCommands"}, Admission: a})
	if e != nil || !proto.Equal(before, again) {
		t.Fatalf("original redelivery changed receipt: %v %v", again, e)
	}
}

type receiptRecipient interface {
	QueryReceipt(context.Context, *v1.Caller, *v1.CommandIdentity) (*v1.ReceiptQuery, error)
	Accept(context.Context, *v1.Caller, *v1.AcceptOperationCommand) (*v1.CommandReceipt, error)
}
type countedRecipient struct {
	recipient        receiptRecipient
	accepts, queries int
}

func (r *countedRecipient) QueryReceipt(ctx context.Context, c *v1.Caller, id *v1.CommandIdentity) (*v1.ReceiptQuery, error) {
	r.queries++
	return r.recipient.QueryReceipt(ctx, c, id)
}
func (r *countedRecipient) Accept(ctx context.Context, c *v1.Caller, a *v1.AcceptOperationCommand) (*v1.CommandReceipt, error) {
	r.accepts++
	return r.recipient.Accept(ctx, c, a)
}
