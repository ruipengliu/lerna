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

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/defaults/scripted"
	"github.com/ruipengliu/lerna/infra/sqlite"
	"google.golang.org/protobuf/proto"
)

// 规则：G2、G3、G11、R7、完成-4、完成-6、完成-7
func TestCompletionCommitBoundariesRecoverOneResultAndOriginalSeal(t *testing.T) {
	for _, point := range []string{"tasks.verification", "ledger.completion_seal", "tasks.completion_receipt", "tasks.completion"} {
		for _, mode := range []sqlite.FaultMode{sqlite.CrashBeforeCommit, sqlite.CrashAfterCommit, sqlite.LoseReceipt} {
			t.Run(point+"/"+string(mode), func(t *testing.T) {
				target := simulator.New("idempotent")
				server := httptest.NewServer(target)
				defer server.Close()
				path := filepath.Join(t.TempDir(), "completion.db")
				h, e := assembly.Open(path, "u", "d")
				if e != nil {
					t.Fatal(e)
				}
				a, begin := prepareCompletionFault(t, h, server.URL)
				body, e := proto.Marshal(begin)
				if e != nil {
					t.Fatal(e)
				}
				if e = os.WriteFile(path+".completion", body, 0600); e != nil {
					t.Fatal(e)
				}
				h.Close()
				child := exec.Command(os.Args[0], "-test.run=^TestCompletionCommitChild$")
				child.Env = append(os.Environ(), "LERNA_COMPLETION_DB="+path, "LERNA_COMPLETION_POINT="+point, "LERNA_COMPLETION_MODE="+string(mode))
				out, e := child.CombinedOutput()
				if mode == sqlite.LoseReceipt {
					if e != nil {
						t.Fatalf("child %v %s", e, out)
					}
				} else {
					var exit *exec.ExitError
					if !errors.As(e, &exit) || exit.ExitCode() != sqlite.CrashExitCode {
						t.Fatalf("fault not reached %v %s", e, out)
					}
				}
				h, e = assembly.Open(path, "u", "d")
				if e != nil {
					t.Fatal(e)
				}
				defer h.Close()
				ctx := context.Background()
				caller := &v1.Caller{UserId: "u", IssuerId: "host"}
				original, e := h.Durable.QueryReceipt(ctx, caller, begin.Header.Identity)
				if e != nil {
					t.Fatal(e)
				}
				if point == "tasks.verification" && mode == sqlite.CrashBeforeCommit {
					if original.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND {
						t.Fatal("partial begin survived")
					}
				} else if original.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED {
					t.Fatal("committed begin lost")
				}
				r, e := h.Tasks.BeginCompletion(ctx, caller, begin)
				requireAccepted(t, r, e)
				if original.Receipt != nil && !proto.Equal(original.Receipt, r) {
					t.Fatal("begin original receipt changed")
				}
				if e = h.Tasks.RecoverCompletions(ctx, caller); e != nil {
					t.Fatal(e)
				}
				result, e := h.Tasks.QueryResult(ctx, caller, a.TaskId)
				if e != nil || result == nil || result.Outcome != "SUCCEEDED" {
					t.Fatalf("result %v %v", result, e)
				}
				task, e := h.Tasks.QueryTask(ctx, caller, a.TaskId)
				if e != nil || task.Lifecycle != v1.TaskLifecycle_TASK_LIFECYCLE_CLOSED || !proto.Equal(task.ResultRef, result.Ref) {
					t.Fatalf("partial result/task %v %v", task, e)
				}
				planning, e := h.Tasks.QueryPlanning(ctx, caller, a.TaskId)
				if e != nil || planning.VerificationFreeze != 0 {
					t.Fatalf("orphan freeze %v %v", planning, e)
				}
				round, e := h.Tasks.QueryVerification(ctx, caller, result.VerificationRef)
				if e != nil || round.Status != "PASSED" || len(round.ClosureIntentRefs) != 1 {
					t.Fatalf("partial round %v %v", round, e)
				}
				intent, e := h.Tasks.QueryCompletionIntent(ctx, caller, round.ClosureIntentRefs[0])
				if e != nil || intent.RecipientReceipt == nil {
					t.Fatalf("lost source ack %v %v", intent, e)
				}
				op, e := h.Ledger.QueryOperation(ctx, caller, a.OperationId)
				if e != nil || len(op.ClosureEvidenceRefs) != 1 || !proto.Equal(op.ClosureEvidenceRefs[0], intent.RecipientReceipt.ResultRef) {
					t.Fatalf("duplicate seal %v %v", op, e)
				}
				if point == "tasks.completion" {
					q, e := h.Durable.QueryReceipt(ctx, caller, admissionHeader("completion-finish").Identity)
					if e != nil {
						t.Fatal(e)
					}
					if mode == sqlite.CrashBeforeCommit {
						if q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND {
							t.Fatal("uncommitted finish receipt survived")
						}
					} else if q.Receipt == nil || !proto.Equal(q.Receipt.ResultRef, result.Ref) {
						t.Fatalf("fixed result changed %v", q)
					}
				}
				if e = h.Tasks.ProcessCompletions(ctx, caller); e != nil {
					t.Fatal(e)
				}
				again, e := h.Tasks.QueryResult(ctx, caller, a.TaskId)
				if e != nil || !proto.Equal(again, result) {
					t.Fatal("recovery changed Result")
				}
				requests, effects := target.Snapshot()
				if len(requests) != 1 || len(effects) != 1 {
					t.Fatalf("target receives=%d effects=%d", len(requests), len(effects))
				}
			})
		}
	}
}

// 规则：G3、R7
func TestCompletionCommitChild(t *testing.T) {
	path := os.Getenv("LERNA_COMPLETION_DB")
	if path == "" {
		t.Skip("subprocess only")
	}
	h, e := assembly.Open(path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	body, e := os.ReadFile(path + ".completion")
	if e != nil {
		t.Fatal(e)
	}
	begin := new(v1.BeginCompletionCommand)
	if e = proto.Unmarshal(body, begin); e != nil {
		t.Fatal(e)
	}
	point := os.Getenv("LERNA_COMPLETION_POINT")
	ctx, e := sqlite.WithFault(context.Background(), point, sqlite.FaultMode(os.Getenv("LERNA_COMPLETION_MODE")))
	if e != nil {
		t.Fatal(e)
	}
	caller := &v1.Caller{UserId: "u", IssuerId: "host"}
	r, e := h.Tasks.BeginCompletion(ctx, caller, begin)
	if point == "tasks.verification" {
		if e == nil {
			t.Fatal("fault not reached")
		}
		return
	}
	requireAccepted(t, r, e)
	claim, e := h.Durable.ExecuteJob(ctx, caller, &v1.JobCommand{Identity: admissionHeader("completion-claim").Identity, ContractVersion: 1, Action: "CLAIM", Module: "tasks", AllowedTypes: []string{"DELIVER_COMPLETION_CLOSURE"}, Limit: 1, LeaseMs: 100, ProcessInstance: "crashing-completion-worker"})
	requireAccepted(t, claim, e)
	if len(claim.Jobs) != 1 {
		t.Fatal("missing closure job")
	}
	e = h.Tasks.ProcessCompletionClosureClaim(ctx, claim.Jobs[0])
	if point != "tasks.completion" {
		if e == nil {
			t.Fatal("fault not reached")
		}
		return
	}
	if e != nil {
		t.Fatal(e)
	}
	_, e = h.Tasks.RecheckCompletion(ctx, caller, &v1.RecheckCompletionCommand{Header: admissionHeader("completion-finish"), VerificationRef: r.ResultRef})
	if e == nil {
		t.Fatal("fault not reached")
	}
}

func prepareCompletionFault(t *testing.T, h *assembly.Harness, target string) (*v1.Admission, *v1.BeginCompletionCommand) {
	t.Helper()
	ctx := context.Background()
	caller := &v1.Caller{UserId: "u", IssuerId: "host"}
	admit := prepareAdmission(t, h, target)
	proposal, e := h.Tasks.QueryProposal(ctx, caller, admit.ProposalRef)
	if e != nil {
		t.Fatal(e)
	}
	task, e := h.Tasks.QueryTask(ctx, caller, admit.TaskId)
	if e != nil {
		t.Fatal(e)
	}
	r, e := h.Tasks.AcceptRequirements(ctx, caller, &v1.AcceptRequirementsCommand{Header: admissionHeader("completion-scope"), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, InputVersion: task.InputVersion, Source: "TRUSTED_TEMPLATE", Conditions: []*v1.Requirement{{ConditionId: "created", DescriptionRef: proposal.Step.ParametersRef, Necessary: true, VerificationRule: "TARGET_RECORD", RuleVersion: 1, TargetRecord: &v1.TargetRecordAssertion{CapabilityRef: proposal.Step.CapabilityRef, ParametersRef: proposal.Step.ParametersRef}}}})
	requireAccepted(t, r, e)
	snap, e := h.Tasks.RequestProposal(ctx, caller, &v1.RequestProposalCommand{Header: admissionHeader("completion-action-request"), TaskId: task.TaskId})
	if e != nil {
		t.Fatal(e)
	}
	p, e := (&scripted.Reasoner{Step: proposal.Step}).Propose(ctx, snap)
	if e != nil {
		t.Fatal(e)
	}
	r, e = h.Tasks.ReceiveProposal(ctx, caller, &v1.ReceiveProposalCommand{Header: admissionHeader("completion-action-proposal"), Proposal: p})
	requireAccepted(t, r, e)
	admit.ProposalRef = r.ResultRef
	r, e = h.Tasks.Admit(ctx, caller, admit)
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
	snap, e = h.Tasks.RequestProposal(ctx, caller, &v1.RequestProposalCommand{Header: admissionHeader("completion-request"), TaskId: a.TaskId})
	if e != nil {
		t.Fatal(e)
	}
	p, e = (&scripted.Reasoner{CompletionEvidence: []*v1.CompletionEvidence{{ConditionId: "created", OperationId: a.OperationId}}}).Propose(ctx, snap)
	if e != nil {
		t.Fatal(e)
	}
	r, e = h.Tasks.ReceiveProposal(ctx, caller, &v1.ReceiveProposalCommand{Header: admissionHeader("completion-proposal"), Proposal: p})
	requireAccepted(t, r, e)
	return a, &v1.BeginCompletionCommand{Header: admissionHeader("completion-begin"), TaskId: a.TaskId, ProposalRef: r.ResultRef}
}

// 规则：G2、G3、G11、完成-4、完成-7
func TestRejectedContinuationCommitRecoversOneActualRequest(t *testing.T) {
	for _, mode := range []sqlite.FaultMode{sqlite.CrashBeforeCommit, sqlite.CrashAfterCommit, sqlite.LoseReceipt} {
		t.Run(string(mode), func(t *testing.T) {
			target := simulator.New("idempotent")
			target.SetBehavior("reject")
			server := httptest.NewServer(target)
			defer server.Close()
			path := filepath.Join(t.TempDir(), "continuation.db")
			h, e := assembly.Open(path, "u", "d")
			if e != nil {
				t.Fatal(e)
			}
			a, begin := prepareCompletionFault(t, h, server.URL)
			writeBudgetMessage(t, path+".completion", begin)
			if e = h.Close(); e != nil {
				t.Fatal(e)
			}
			child := exec.Command(os.Args[0], "-test.run=^TestCompletionCommitChild$")
			child.Env = append(os.Environ(), "LERNA_COMPLETION_DB="+path, "LERNA_COMPLETION_POINT=tasks.completion", "LERNA_COMPLETION_MODE="+string(mode))
			out, e := child.CombinedOutput()
			requireBudgetFault(t, mode, out, e)
			h, e = assembly.Open(path, "u", "d")
			if e != nil {
				t.Fatal(e)
			}
			defer h.Close()
			ctx := context.Background()
			caller := &v1.Caller{UserId: "u", IssuerId: "host"}
			state, e := h.Tasks.QueryPlanning(ctx, caller, a.TaskId)
			if e != nil {
				t.Fatal(e)
			}
			v, e := h.Tasks.QueryVerification(ctx, caller, state.VerificationRef)
			if e != nil || v.Status != "REJECTED" || v.ContinuationRequestRef == nil || !proto.Equal(v.ContinuationRequestRef, state.Snapshot.RequestRef) || state.VerificationFreeze != 0 || len(v.AdmissionRefs) != 1 || len(v.Gaps) == 0 {
				t.Fatalf("partial rejection or continuation %v %v", v, e)
			}
			req, e := h.Tasks.QueryProposalRequest(ctx, caller, v.ContinuationRequestRef)
			if e != nil || req.State != "PENDING" || !proto.Equal(req.SnapshotRef, state.Snapshot.Ref) {
				t.Fatalf("actual continuation missing %v %v", req, e)
			}
			job, e := h.Durable.QueryJob(ctx, caller, req.JobRef.Name)
			if e != nil || job.State != "READY" || job.JobType != "PROPOSE" || !proto.Equal(job.SpecificationRef, req.Ref) {
				t.Fatalf("durable request responsibility lost %v %v", job, e)
			}
			for range 2 {
				if e = h.Tasks.ProcessCompletions(ctx, caller); e != nil {
					t.Fatal(e)
				}
			}
			if e = h.Close(); e != nil {
				t.Fatal(e)
			}
			h, e = assembly.Open(path, "u", "d")
			if e != nil {
				t.Fatal(e)
			}
			defer h.Close()
			again, e := h.Tasks.QueryPlanning(ctx, caller, a.TaskId)
			if e != nil || !proto.Equal(again.Snapshot, state.Snapshot) || !proto.Equal(again.VerificationRef, state.VerificationRef) {
				t.Fatal("restart generated another continuation")
			}
			result, e := h.Tasks.QueryResult(ctx, caller, a.TaskId)
			if e != nil || result != nil {
				t.Fatal("rejection closed task")
			}
			requests, effects := target.Snapshot()
			if len(requests) != 1 || len(effects) != 0 {
				t.Fatal("continuation blindly resent old action")
			}
		})
	}
}
