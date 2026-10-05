package admission_test

import (
	"strings"
	"testing"

	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G2、G6、完成-3、完成-5、完成-6
func TestCompletionCannotInventEvidenceOrIgnoreUnsettledEffects(t *testing.T) {
	for _, name := range []string{"missing-scope", "different-parameters", "missing-candidate", "foreign-candidate", "old-requirements", "unknown", "nonterminal", "conflicting", "user-evaluation"} {
		t.Run(name, func(t *testing.T) {
			target := simulator.New("idempotent")
			switch name {
			case "unknown":
				target.SetBehavior("drop-after-apply")
			case "nonterminal":
				target.SetBehavior("applied-not-terminal")
			case "conflicting":
				target.SetBehavior("contradictory")
			}
			f := newFixtureWithTarget(t, 100, 80, false, target)
			if name != "missing-scope" {
				scopeRequirement(t, f)
			}
			if name == "different-parameters" {
				var e error
				f.parameters, e = f.h.Content.Stage(f.ctx, f.caller, &v1.SubmitGoalCommand{Identity: header("different-body").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "a different request"})
				if e != nil {
					t.Fatal(e)
				}
			}
			if name == "user-evaluation" {
				task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
				if e != nil {
					t.Fatal(e)
				}
				r, e := f.h.Tasks.AcceptRequirements(f.ctx, f.caller, &v1.AcceptRequirementsCommand{Header: header("evaluation-requirement"), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, InputVersion: task.InputVersion, Source: "TRUSTED_TEMPLATE", Conditions: []*v1.Requirement{{ConditionId: "created", DescriptionRef: f.parameters, Necessary: true, VerificationRule: "USER_EVALUATION", RuleVersion: 1}}})
				accepted(t, r, e)
			}
			a, c := prepareStart(t, f)
			r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
			accepted(t, r, e)
			if name == "old-requirements" {
				f.suffix = "replacement"
				scopeRequirement(t, f)
			}
			candidate := proto.Clone(a.OperationId).(*v1.GlobalName)
			if name == "foreign-candidate" {
				candidate.UserId = "other-user"
			}
			evidence := []*v1.CompletionEvidence{{ConditionId: "created", OperationId: candidate}}
			if name == "missing-candidate" {
				evidence = nil
			}
			p := completeProposal(t, f, evidence)
			r, e = f.h.Tasks.BeginCompletion(f.ctx, f.caller, &v1.BeginCompletionCommand{Header: header("negative-begin"), TaskId: f.task.Name, ProposalRef: p})
			accepted(t, r, e)
			if e = f.h.Tasks.ProcessCompletions(f.ctx, f.caller); e != nil {
				t.Fatal(e)
			}
			result, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
			if e != nil || result != nil {
				t.Fatalf("invented completion %v %v", result, e)
			}
			state, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
			if e != nil {
				t.Fatal(e)
			}
			v, e := f.h.Tasks.QueryVerification(f.ctx, f.caller, state.VerificationRef)
			if e != nil || v.Status != "VERIFYING" || len(v.Gaps) == 0 || state.VerificationFreeze != v.Round {
				t.Fatalf("missing lawful wait %v %v", v, e)
			}
			if name == "missing-scope" && !strings.Contains(strings.Join(v.Gaps, " "), "TARGET_SCOPE_MISSING") {
				t.Fatalf("missing specific scope gap %v", v.Gaps)
			}
			requests, _ := target.Snapshot()
			if len(requests) != 1 {
				t.Fatal("verification sent another request")
			}
		})
	}
}

// 规则：G2、G6、完成-5
func TestRequirementScopeMustResolveTrustedCapabilityAndExactContent(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	fake := proto.Clone(f.capability).(*v1.Ref)
	fake.Revision++
	r, e := f.h.Tasks.AcceptRequirements(f.ctx, f.caller, &v1.AcceptRequirementsCommand{Header: header("invalid-scope"), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, InputVersion: task.InputVersion, Source: "TRUSTED_TEMPLATE", Conditions: []*v1.Requirement{{ConditionId: "created", DescriptionRef: f.parameters, Necessary: true, VerificationRule: "TARGET_RECORD", RuleVersion: 1, TargetRecord: &v1.TargetRecordAssertion{CapabilityRef: fake, ParametersRef: f.parameters}}}})
	if e != nil || r.Decision != v1.Decision_DECISION_REJECTED || r.Error.GetCode() != "INVALID_REQUIREMENTS" {
		t.Fatalf("untrusted assertion accepted %v %v", r, e)
	}
}
