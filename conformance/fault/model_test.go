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
	"github.com/ruipengliu/lerna/infra/sqlite"
	"google.golang.org/protobuf/proto"
)

func prepareModelFault(t *testing.T, h *assembly.Harness, url string) *v1.RunModelCallCommand {
	t.Helper()
	ctx := context.Background()
	caller := &v1.Caller{UserId: "u", IssuerId: "host"}
	old := prepareAdmission(t, h, url)
	p, e := h.Tasks.QueryProposal(ctx, caller, old.ProposalRef)
	if e != nil {
		t.Fatal(e)
	}
	cap, e := h.Tasks.QueryCapability(ctx, caller, p.Step.CapabilityRef)
	if e != nil {
		t.Fatal(e)
	}
	cap.Ref = nil
	cap.ApprovedBy = nil
	cap.Action = "MODEL_INFER"
	cap.AdapterRef.Name.LocalId = "model-reference-v1"
	r, e := h.Tasks.ConfigureCapability(ctx, caller, &v1.ConfigureCapabilityCommand{Header: admissionHeader("model-cap"), Capability: cap})
	requireAccepted(t, r, e)
	capRef := r.ResultRef
	r, e = h.Grants.Configure(ctx, caller, &v1.ConfigureGrantCommand{Header: admissionHeader("model-grant"), Grant: &v1.Grant{Subject: old.TaskId, Permissions: []*v1.PermissionClause{{Action: "MODEL_INFER", Resource: url, UseRight: "INVOKE", ProcessingPurpose: "CURRENT_TASK", ExecutorEndpointId: cap.ExecutorEndpointId}}, ValidFromUnixMs: 1, ValidUntilUnixMs: 4000000000000, UseMode: "CONTINUOUS"}})
	requireAccepted(t, r, e)
	grant := r.ResultRef
	snap, e := h.Tasks.RequestProposal(ctx, caller, &v1.RequestProposalCommand{Header: admissionHeader("model-request"), TaskId: old.TaskId})
	if e != nil {
		t.Fatal(e)
	}
	claim, e := h.Durable.ExecuteJob(ctx, caller, &v1.JobCommand{Identity: admissionHeader("model-claim").Identity, ContractVersion: 1, Action: "CLAIM", AllowedTypes: []string{"PROPOSE"}, Limit: 1, LeaseMs: 60000, ProcessInstance: "model-fault-worker"})
	requireAccepted(t, claim, e)
	if len(claim.Jobs) != 1 {
		t.Fatal(claim)
	}
	return &v1.RunModelCallCommand{Preparation: &v1.PrepareModelCallCommand{Header: admissionHeader("model-prepare"), RequestRef: snap.RequestRef, Claim: claim.Jobs[0], CapabilityRef: capRef, Settings: &v1.ModelSettings{Provider: "reference", Model: "model-v1", EncoderVersion: "reference-model-v1", PolicyVersion: "m1-v1", ParametersJson: []byte(`{}`), ToolSchemasJson: []byte(`[]`), MaxOutputTokens: 100}, InputRefs: snap.ContentRefs}, GrantRef: grant}
}
func modelOutcomeCommand(t *testing.T, h *assembly.Harness, run *v1.RunModelCallCommand, result *v1.ModelCallResult) *v1.SubmitProposalOutcomeCommand {
	t.Helper()
	ctx := context.Background()
	caller := &v1.Caller{UserId: "u", IssuerId: "host"}
	r, e := h.Tasks.QueryProposalRequest(ctx, caller, run.Preparation.RequestRef)
	if e != nil {
		t.Fatal(e)
	}
	snap, e := h.Tasks.QuerySnapshot(ctx, caller, r.SnapshotRef)
	if e != nil {
		t.Fatal(e)
	}
	p := &v1.Proposal{TaskId: r.TaskId, ContextSnapshotRef: snap.Ref, RequestRef: r.Ref, RequirementsVersion: snap.RequirementsVersion, InputVersion: snap.InputVersion, ControlGeneration: snap.ControlGeneration, PlanningGeneration: snap.PlanningGeneration, Kind: "COMPLETE", ReasonerRef: &v1.Ref{Name: &v1.GlobalName{UserId: "u", AuthorityDomainId: "reasoner", ObjectKind: "reasoner", LocalId: "default"}, Revision: 1, SchemaId: "lerna.v1.Reasoner"}}
	return &v1.SubmitProposalOutcomeCommand{Header: admissionHeader("model-outcome"), RequestRef: r.Ref, Claim: run.Preparation.Claim, Proposal: p, ModelCallRef: result.CallRef, OutputRef: result.OutputRef, UsageRef: result.UsageRef}
}

// 规则：G1、G3、G4、G5、G9、G10、G11、R7、V4
func TestModelCallCrashMatrix(t *testing.T) {
	cases := []struct{ point, phase string }{{"tasks.model_prepare", "input"}, {"tasks.model_seal", "input"}, {"tasks.model_admit", "input"}, {"tasks.start", "input"}, {"ledger.dispatch", "input"}, {"tasks.model_result", "input"}, {"tasks.proposal_outcome", "outcome"}, {"tasks.proposal_stop", "stop"}, {"tasks.planning", "request"}}
	for _, point := range []string{"content.derivation", "content.derivation_input", "content.derivation_seal", "content.derivation_commit"} {
		cases = append(cases, struct{ point, phase string }{point, "input"}, struct{ point, phase string }{point, "output"})
	}
	for _, tc := range cases {
		for _, mode := range []sqlite.FaultMode{sqlite.CrashBeforeCommit, sqlite.CrashAfterCommit, sqlite.LoseReceipt} {
			t.Run(tc.phase+"/"+tc.point+"/"+string(mode), func(t *testing.T) {
				provider := simulator.NewModelProvider()
				server := httptest.NewServer(provider)
				defer server.Close()
				path := filepath.Join(t.TempDir(), "model.db")
				h, e := assembly.Open(path, "u", "d")
				if e != nil {
					t.Fatal(e)
				}
				ctx := context.Background()
				caller := &v1.Caller{UserId: "u", IssuerId: "host"}
				run := prepareModelFault(t, h, server.URL)
				if tc.phase == "output" {
					if _, e = h.Tasks.PrepareModelCall(ctx, caller, run.Preparation); e != nil {
						t.Fatal(e)
					}
				}
				if tc.phase == "outcome" {
					result, e := h.Tasks.RunModelCall(ctx, caller, run)
					if e != nil {
						t.Fatal(e)
					}
					b, e := proto.Marshal(modelOutcomeCommand(t, h, run, result))
					if e != nil {
						t.Fatal(e)
					}
					if e = os.WriteFile(path+".outcome", b, 0600); e != nil {
						t.Fatal(e)
					}
				}
				b, e := proto.Marshal(run)
				if e != nil {
					t.Fatal(e)
				}
				if e = os.WriteFile(path+".run", b, 0600); e != nil {
					t.Fatal(e)
				}
				h.Close()
				child := exec.Command(os.Args[0], "-test.run=^TestModelCallCrashChild$")
				child.Env = append(os.Environ(), "LERNA_MODEL_DB="+path, "LERNA_MODEL_POINT="+tc.point, "LERNA_MODEL_PHASE="+tc.phase, "LERNA_MODEL_MODE="+string(mode))
				out, e := child.CombinedOutput()
				if mode == sqlite.LoseReceipt {
					if e != nil {
						t.Fatalf("child: %v %s", e, out)
					}
				} else {
					var exit *exec.ExitError
					if !errors.As(e, &exit) || exit.ExitCode() != sqlite.CrashExitCode {
						t.Fatalf("fault missed: %v %s", e, out)
					}
				}
				before := provider.Calls()
				h, e = assembly.Open(path, "u", "d")
				if e != nil {
					t.Fatal(e)
				}
				defer h.Close()
				switch tc.phase {
				case "outcome":
					b, e := os.ReadFile(path + ".outcome")
					if e != nil {
						t.Fatal(e)
					}
					c := new(v1.SubmitProposalOutcomeCommand)
					if e = proto.Unmarshal(b, c); e != nil {
						t.Fatal(e)
					}
					receipt, e := h.Tasks.SubmitProposalOutcome(ctx, caller, c)
					requireAccepted(t, receipt, e)
					request, e := h.Tasks.QueryProposalRequest(ctx, caller, c.RequestRef)
					if e != nil || request.State != "REPORTED" || len(request.OutcomeRefs) != 1 || !proto.Equal(request.OutcomeReceipt, receipt) {
						t.Fatalf("outcome receipt: %v %v", request, e)
					}
					if provider.Calls() != before || before != 1 {
						t.Fatal("outcome recovery called model")
					}
					return
				case "stop":
					r, e := h.Tasks.StopProposalRequest(ctx, caller, &v1.StopProposalRequestCommand{Header: admissionHeader("model-stop"), RequestRef: run.Preparation.RequestRef})
					requireAccepted(t, r, e)
					request, e := h.Tasks.QueryProposalRequest(ctx, caller, run.Preparation.RequestRef)
					if e != nil || request.State != "STOPPED" || provider.Calls() != 0 {
						t.Fatalf("stop: %v %v", request, e)
					}
					return
				case "request":
					original, e := h.Tasks.QueryProposalRequest(ctx, caller, run.Preparation.RequestRef)
					if e != nil {
						t.Fatal(e)
					}
					snap, e := h.Tasks.RequestProposal(ctx, caller, &v1.RequestProposalCommand{Header: admissionHeader("model-fault-request"), TaskId: original.TaskId})
					if e != nil {
						t.Fatal(e)
					}
					request, e := h.Tasks.QueryProposalRequest(ctx, caller, snap.RequestRef)
					if e != nil || !proto.Equal(request.SnapshotRef, snap.Ref) || provider.Calls() != 0 {
						t.Fatalf("request: %v %v", request, e)
					}
					return
				}
				result, e := h.Tasks.RunModelCall(ctx, caller, run)
				if e != nil {
					t.Fatal(e)
				}
				call, e := h.Tasks.QueryModelCall(ctx, caller, run.Preparation.RequestRef, 0)
				if e != nil {
					t.Fatal(e)
				}
				want := 1
				status := "COMPLETED"
				if tc.point == "ledger.dispatch" && mode != sqlite.CrashBeforeCommit {
					want = 0
					status = "UNKNOWN"
				}
				if result.Status != status || provider.Calls() != want || len(provider.Bills()) != want {
					t.Fatalf("result %v calls=%d bills=%d", result, provider.Calls(), len(provider.Bills()))
				}
				if call.Result == nil || !proto.Equal(call.Result, result) {
					t.Fatalf("result not durable: %v", call)
				}
				a, e := h.Tasks.QueryAdmission(ctx, caller, call.AdmissionRef)
				if e != nil {
					t.Fatal(e)
				}
				state, e := h.Tasks.QueryPlanning(ctx, caller, a.TaskId)
				if e != nil || len(state.AdmissionRefs) != 1 {
					t.Fatalf("duplicate admission: %v %v", state, e)
				}
				budget, e := h.Budget.QueryBudget(ctx, caller, nil)
				if e != nil {
					t.Fatal(e)
				}
				if want == 0 {
					if budget.Reserved != 30 || budget.Settled != 0 {
						t.Fatalf("unknown fee: %v", budget)
					}
				} else if budget.Settled != 7 || budget.Reserved != 0 {
					t.Fatalf("settlement: %v", budget)
				}
				replay, e := h.Tasks.RunModelCall(ctx, caller, run)
				if e != nil || !proto.Equal(replay, result) || provider.Calls() != want {
					t.Fatalf("replay: %v %v", replay, e)
				}
			})
		}
	}
}

// 规则：G3、V4
func TestModelCallCrashChild(t *testing.T) {
	path := os.Getenv("LERNA_MODEL_DB")
	if path == "" {
		t.Skip("child only")
	}
	h, e := assembly.Open(path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	b, e := os.ReadFile(path + ".run")
	if e != nil {
		t.Fatal(e)
	}
	run := new(v1.RunModelCallCommand)
	if e = proto.Unmarshal(b, run); e != nil {
		t.Fatal(e)
	}
	ctx, e := sqlite.WithFault(context.Background(), os.Getenv("LERNA_MODEL_POINT"), sqlite.FaultMode(os.Getenv("LERNA_MODEL_MODE")))
	if e != nil {
		t.Fatal(e)
	}
	caller := &v1.Caller{UserId: "u", IssuerId: "host"}
	switch os.Getenv("LERNA_MODEL_PHASE") {
	case "outcome":
		b, e := os.ReadFile(path + ".outcome")
		if e != nil {
			t.Fatal(e)
		}
		c := new(v1.SubmitProposalOutcomeCommand)
		if e = proto.Unmarshal(b, c); e != nil {
			t.Fatal(e)
		}
		_, e = h.Tasks.SubmitProposalOutcome(ctx, caller, c)
		if e == nil {
			t.Fatal("outcome fault missed")
		}
		return
	case "stop":
		_, e = h.Tasks.StopProposalRequest(ctx, caller, &v1.StopProposalRequestCommand{Header: admissionHeader("model-stop"), RequestRef: run.Preparation.RequestRef})
	case "request":
		r, err := h.Tasks.QueryProposalRequest(ctx, caller, run.Preparation.RequestRef)
		if err != nil {
			t.Fatal(err)
		}
		_, e = h.Tasks.RequestProposal(ctx, caller, &v1.RequestProposalCommand{Header: admissionHeader("model-fault-request"), TaskId: r.TaskId})
	default:
		_, e = h.Tasks.RunModelCall(ctx, caller, run)
	}
	if e == nil {
		t.Fatal("fault did not fire")
	}
}
