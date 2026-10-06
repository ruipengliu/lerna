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
	port "github.com/ruipengliu/lerna/contracts/reasoner"
	defaultreasoner "github.com/ruipengliu/lerna/defaults/reasoner"
	"github.com/ruipengliu/lerna/defaults/scripted"
	"github.com/ruipengliu/lerna/infra/sqlite"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G3、G5、G8、G9、G10、G11、V4
func TestReasonerBodyAndOutcomeCrashRecovery(t *testing.T) {
	for _, point := range []string{"content.derivation", "content.derivation_input", "content.derivation_seal", "content.derivation_commit", "tasks.proposal_outcome"} {
		for _, mode := range []sqlite.FaultMode{sqlite.CrashBeforeCommit, sqlite.CrashAfterCommit, sqlite.LoseReceipt} {
			t.Run(point+"/"+string(mode), func(t *testing.T) {
				provider := simulator.NewModelProvider()
				provider.Output = `{"kind":"QUESTION","question":{"question":"Choose a destination","changesBasis":true},"gaps":["destination missing"]}`
				server := httptest.NewServer(provider)
				defer server.Close()
				path := filepath.Join(t.TempDir(), "reasoner.db")
				h, e := assembly.Open(path, "u", "d")
				if e != nil {
					t.Fatal(e)
				}
				ctx := context.Background()
				caller := &v1.Caller{UserId: "u", IssuerId: "host"}
				run := prepareModelFault(t, h, server.URL)
				run.Preparation.Settings.ViewPolicy = port.ViewPolicy
				run.Preparation.Settings.MaxInputBytes = 262144
				run.Preparation.Settings.MaxInputTokens = 65536
				result, e := h.Tasks.RunModelCall(ctx, caller, run)
				if e != nil || result.Status != "COMPLETED" {
					t.Fatalf("model %v %v", result, e)
				}
				encoded, e := proto.Marshal(run)
				if e != nil {
					t.Fatal(e)
				}
				if e = os.WriteFile(path+".run", encoded, 0600); e != nil {
					t.Fatal(e)
				}
				h.Close()
				child := exec.Command(os.Args[0], "-test.run=^TestReasonerCrashChild$")
				child.Env = append(os.Environ(), "LERNA_REASONER_DB="+path, "LERNA_REASONER_POINT="+point, "LERNA_REASONER_MODE="+string(mode))
				output, e := child.CombinedOutput()
				if mode == sqlite.LoseReceipt {
					if e != nil {
						t.Fatalf("child %v %s", e, output)
					}
				} else {
					var exit *exec.ExitError
					if !errors.As(e, &exit) || exit.ExitCode() != sqlite.CrashExitCode {
						t.Fatalf("fault missed %v %s", e, output)
					}
				}
				h, e = assembly.Open(path, "u", "d")
				if e != nil {
					t.Fatal(e)
				}
				defer h.Close()
				outcome, e := h.Tasks.RunReasoner(ctx, caller, run, &defaultreasoner.Reasoner{Settings: run.Preparation.Settings})
				if e != nil || outcome.ProposalRef == nil || outcome.Proposal.BodyContentRef == nil {
					t.Fatalf("recovery %v %v", outcome, e)
				}
				body, e := h.Tasks.ReadProposal(ctx, caller, outcome.ProposalRef)
				if e != nil || body.GetQuestion().GetQuestion() != "Choose a destination" {
					t.Fatalf("body %v %v", body, e)
				}
				replay, e := h.Tasks.RunReasoner(ctx, caller, run, &defaultreasoner.Reasoner{Settings: run.Preparation.Settings})
				if e != nil || !proto.Equal(outcome, replay) || provider.Calls() != 1 || len(provider.Bills()) != 1 {
					t.Fatalf("replay %v %v calls=%d", replay, e, provider.Calls())
				}
				request, e := h.Tasks.QueryProposalRequest(ctx, caller, run.Preparation.RequestRef)
				if e != nil || len(request.OutcomeRefs) != 1 {
					t.Fatalf("duplicate outcomes %v %v", request, e)
				}
				assertReasonerFaultSource(t, h, request.TaskId, "PROPOSAL_RECEIVED", outcome.ProposalRef, outcome.Proposal.BodyContentRef)
				assertReasonerFaultSource(t, h, request.TaskId, "PROPOSAL_OUTCOME_ACCEPTED", outcome.Ref, outcome.Proposal.BodyContentRef)
			})
		}
	}
}

// 规则：G3、V4
func TestReasonerCrashChild(t *testing.T) {
	path := os.Getenv("LERNA_REASONER_DB")
	if path == "" {
		t.Skip("child only")
	}
	h, e := assembly.Open(path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	encoded, e := os.ReadFile(path + ".run")
	if e != nil {
		t.Fatal(e)
	}
	run := new(v1.RunModelCallCommand)
	if e = proto.Unmarshal(encoded, run); e != nil {
		t.Fatal(e)
	}
	ctx, e := sqlite.WithFault(context.Background(), os.Getenv("LERNA_REASONER_POINT"), sqlite.FaultMode(os.Getenv("LERNA_REASONER_MODE")))
	if e != nil {
		t.Fatal(e)
	}
	_, e = h.Tasks.RunReasoner(ctx, &v1.Caller{UserId: "u", IssuerId: "host"}, run, &defaultreasoner.Reasoner{Settings: run.Preparation.Settings})
	if e == nil {
		t.Fatal("fault did not fire")
	}
}

// 规则：G2、G3、G4、G6、完成-5、V4
func TestSubjectiveCompletionConsumptionCrashRecovery(t *testing.T) {
	for _, mode := range []sqlite.FaultMode{sqlite.CrashBeforeCommit, sqlite.CrashAfterCommit, sqlite.LoseReceipt} {
		t.Run(string(mode), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "subjective.db")
			h, e := assembly.Open(path, "u", "d")
			if e != nil {
				t.Fatal(e)
			}
			ctx := context.Background()
			caller := &v1.Caller{UserId: "u", IssuerId: "host"}
			old := prepareAdmission(t, h, "https://target.invalid")
			task, e := h.Tasks.QueryTask(ctx, caller, old.TaskId)
			if e != nil {
				t.Fatal(e)
			}
			goal, e := h.Durable.QueryReceipt(ctx, caller, admissionHeader("goal").Identity)
			if e != nil {
				t.Fatal(e)
			}
			requirements, e := h.Tasks.AcceptRequirements(ctx, caller, &v1.AcceptRequirementsCommand{Header: admissionHeader("subjective-requirements"), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, InputVersion: task.InputVersion, Source: "TRUSTED_TEMPLATE", Conditions: []*v1.Requirement{{ConditionId: "review", DescriptionRef: goal.Receipt.InputRef, Necessary: true, VerificationRule: "USER_EVALUATION", RuleVersion: 1}}})
			requireAccepted(t, requirements, e)
			requested, e := h.Tasks.RequestConditionConfirmation(ctx, caller, &v1.RequestConditionConfirmationCommand{Header: admissionHeader("subjective-confirmation"), TaskId: task.TaskId, RequirementsRef: requirements.ResultRef, ConditionId: "review", EvidenceRefs: []*v1.Ref{goal.Receipt.InputRef}, SessionId: goal.Receipt.SessionRef.Name})
			requireAccepted(t, requested, e)
			pending, e := h.Sessions.QueryConfirmation(ctx, caller, requested.ResultRef)
			if e != nil {
				t.Fatal(e)
			}
			approved, e := h.Sessions.RespondConfirmation(ctx, caller, &v1.RespondConfirmationCommand{Header: admissionHeader("subjective-approve"), ConfirmationRef: pending.Ref, BindingDigest: pending.BindingDigest, Decision: "APPROVE"})
			requireAccepted(t, approved, e)
			snapshot, e := h.Tasks.RequestProposal(ctx, caller, &v1.RequestProposalCommand{Header: admissionHeader("subjective-request"), TaskId: task.TaskId})
			if e != nil {
				t.Fatal(e)
			}
			proposal, e := (&scripted.Reasoner{CompletionEvidence: []*v1.CompletionEvidence{{ConditionId: "review", ConfirmationRef: approved.ResultRef}}}).Propose(ctx, snapshot)
			if e != nil {
				t.Fatal(e)
			}
			received, e := h.Tasks.ReceiveProposal(ctx, caller, &v1.ReceiveProposalCommand{Header: admissionHeader("subjective-proposal"), Proposal: proposal})
			requireAccepted(t, received, e)
			begin := &v1.BeginCompletionCommand{Header: admissionHeader("subjective-begin"), TaskId: task.TaskId, ProposalRef: received.ResultRef}
			encoded, e := proto.Marshal(begin)
			if e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(path+".begin", encoded, 0600); e != nil {
				t.Fatal(e)
			}
			h.Close()
			child := exec.Command(os.Args[0], "-test.run=^TestSubjectiveCompletionCrashChild$")
			child.Env = append(os.Environ(), "LERNA_SUBJECTIVE_DB="+path, "LERNA_SUBJECTIVE_MODE="+string(mode))
			output, e := child.CombinedOutput()
			if mode == sqlite.LoseReceipt {
				if e != nil {
					t.Fatalf("child %v %s", e, output)
				}
			} else {
				var exit *exec.ExitError
				if !errors.As(e, &exit) || exit.ExitCode() != sqlite.CrashExitCode {
					t.Fatalf("fault missed %v %s", e, output)
				}
			}
			h, e = assembly.Open(path, "u", "d")
			if e != nil {
				t.Fatal(e)
			}
			defer h.Close()
			begun, e := h.Tasks.BeginCompletion(ctx, caller, begin)
			requireAccepted(t, begun, e)
			again, e := h.Tasks.BeginCompletion(ctx, caller, begin)
			requireAccepted(t, again, e)
			if !proto.Equal(begun, again) {
				t.Fatal("duplicate verification")
			}
			current, e := h.Sessions.QueryCurrentConfirmation(ctx, caller, approved.ResultRef.Name)
			if e != nil || current.State != "CONSUMED" || !proto.Equal(current.GetConsumedVerificationRef(), begun.ResultRef) {
				t.Fatalf("non-atomic consumption %v %v", current, e)
			}
			verification, e := h.Tasks.QueryVerification(ctx, caller, begun.ResultRef)
			if e != nil || len(verification.Candidates) != 1 || !proto.Equal(verification.Candidates[0].ConfirmationRef, current.Ref) {
				t.Fatalf("wrong verification binding %v %v", verification, e)
			}
			if e = h.Tasks.ProcessCompletions(ctx, caller); e != nil {
				t.Fatal(e)
			}
			result, e := h.Tasks.QueryResult(ctx, caller, task.TaskId)
			if e != nil || result == nil || result.Outcome != "SUCCEEDED" {
				t.Fatalf("completion recovery %v %v", result, e)
			}
			assertReasonerFaultSource(t, h, task.TaskId, "CONFIRMATION_CONSUMED", current.Ref, nil, begun.ResultRef)
			assertReasonerFaultSource(t, h, task.TaskId, "VERIFICATION_CHANGED", begun.ResultRef, nil, current.Ref)
		})
	}
}

// 规则：G3、V4
func TestSubjectiveCompletionCrashChild(t *testing.T) {
	path := os.Getenv("LERNA_SUBJECTIVE_DB")
	if path == "" {
		t.Skip("child only")
	}
	h, e := assembly.Open(path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	encoded, e := os.ReadFile(path + ".begin")
	if e != nil {
		t.Fatal(e)
	}
	begin := new(v1.BeginCompletionCommand)
	if e = proto.Unmarshal(encoded, begin); e != nil {
		t.Fatal(e)
	}
	ctx, e := sqlite.WithFault(context.Background(), "tasks.verification", sqlite.FaultMode(os.Getenv("LERNA_SUBJECTIVE_MODE")))
	if e != nil {
		t.Fatal(e)
	}
	_, e = h.Tasks.BeginCompletion(ctx, &v1.Caller{UserId: "u", IssuerId: "host"}, begin)
	if e == nil {
		t.Fatal("fault did not fire")
	}
}

// 规则：G3、G5、G9、G10、V4
func TestReasonerModelReadReceiptLossRetainsRecoverableRequest(t *testing.T) {
	for _, phase := range []string{"input", "output"} {
		t.Run(phase, func(t *testing.T) {
			provider := simulator.NewModelProvider()
			provider.Output = `{"kind":"QUESTION","question":{"question":"Choose a destination"}}`
			server := httptest.NewServer(provider)
			defer server.Close()
			path := filepath.Join(t.TempDir(), "reasoner-read.db")
			h, e := assembly.Open(path, "u", "d")
			if e != nil {
				t.Fatal(e)
			}
			ctx := context.Background()
			caller := &v1.Caller{UserId: "u", IssuerId: "host"}
			run := prepareModelFault(t, h, server.URL)
			run.Preparation.Settings.ViewPolicy = port.ViewPolicy
			run.Preparation.Settings.MaxInputBytes = 262144
			run.Preparation.Settings.MaxInputTokens = 65536
			if phase == "output" {
				if _, e = h.Tasks.PrepareModelCall(ctx, caller, run.Preparation); e != nil {
					t.Fatal(e)
				}
			}
			encoded, e := proto.Marshal(run)
			if e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(path+".run", encoded, 0600); e != nil {
				t.Fatal(e)
			}
			h.Close()
			child := exec.Command(os.Args[0], "-test.run=^TestReasonerCrashChild$")
			child.Env = append(os.Environ(), "LERNA_REASONER_DB="+path, "LERNA_REASONER_POINT=content.derivation_input", "LERNA_REASONER_MODE="+string(sqlite.LoseReceipt))
			output, e := child.CombinedOutput()
			if e != nil {
				t.Fatalf("lost read receipt became terminal: %v %s", e, output)
			}
			h, e = assembly.Open(path, "u", "d")
			if e != nil {
				t.Fatal(e)
			}
			defer h.Close()
			request, e := h.Tasks.QueryProposalRequest(ctx, caller, run.Preparation.RequestRef)
			if e != nil || request.OutcomeReceipt != nil || len(request.OutcomeRefs) != 0 {
				t.Fatalf("transient fault persisted outcome %v %v", request, e)
			}
			outcome, e := h.Tasks.RunReasoner(ctx, caller, run, &defaultreasoner.Reasoner{Settings: run.Preparation.Settings})
			if e != nil || outcome.ProposalRef == nil || provider.Calls() != 1 || len(provider.Bills()) != 1 {
				t.Fatalf("same-request recovery %v %v calls=%d", outcome, e, provider.Calls())
			}
			assertReasonerFaultSource(t, h, request.TaskId, "PROPOSAL_OUTCOME_ACCEPTED", outcome.Ref, outcome.Proposal.BodyContentRef)
		})
	}
}

func assertReasonerFaultSource(t *testing.T, h *assembly.Harness, task *v1.GlobalName, kind string, ref, body *v1.Ref, related ...*v1.Ref) {
	t.Helper()
	ctx := context.Background()
	caller := &v1.Caller{UserId: "u", IssuerId: "host"}
	sources, e := h.Trace.QuerySources(ctx, caller)
	if e != nil {
		t.Fatal(e)
	}
	var original *v1.TraceEvent
	for _, source := range sources {
		event := source.Command.Event
		if event.EventType != kind || !proto.Equal(event.SourceRecordRef, ref) {
			continue
		}
		if original != nil || !proto.Equal(event.BodyRef, body) {
			t.Fatalf("duplicate or rebound source %s", kind)
		}
		original = event
		for _, want := range related {
			found := false
			for _, actual := range event.RelatedRefs {
				found = found || proto.Equal(want, actual)
			}
			if !found {
				t.Fatalf("source lost original related ref %v", want)
			}
		}
	}
	if original == nil {
		t.Fatalf("lost atomic source %s", kind)
	}
	if e = h.Trace.Recover(ctx, caller); e != nil {
		t.Fatal(e)
	}
	event, e := h.Trace.QueryEvent(ctx, caller, original.Ref)
	if e != nil || !proto.Equal(event, original) {
		t.Fatalf("recovered source changed %v %v", event, e)
	}
	view, e := h.Trace.Query(ctx, caller, &v1.TraceQuery{TaskId: task})
	if e != nil || !view.Complete {
		t.Fatalf("recovered trace incomplete %v %v", view, e)
	}
	count := 0
	for _, event := range view.Events {
		if proto.Equal(event, original) {
			count++
		}
	}
	if count != 1 {
		t.Fatal("indexed original source missing or duplicated")
	}
}
