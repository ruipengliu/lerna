package reasoner_test

import (
	"context"
	"encoding/json"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/reasoner"
	defaultreasoner "github.com/ruipengliu/lerna/defaults/reasoner"
	"github.com/ruipengliu/lerna/defaults/scripted"
)

type model struct {
	calls  int
	output []byte
}

func (m *model) Call(_ context.Context, _ *reasoner.ModelRequest) (*reasoner.ModelResponse, error) {
	m.calls++
	return &reasoner.ModelResponse{Result: &v1.ModelCallResult{Status: "COMPLETED"}, Body: m.output}, nil
}

// 规则：G5、G6、R1、R6
func TestImplementationsShareVersionBoundQuestionContract(t *testing.T) {
	snapshot := &v1.ContextSnapshot{TaskRef: &v1.Ref{Name: &v1.GlobalName{UserId: "u", LocalId: "task"}}, Ref: &v1.Ref{Revision: 1}, RequestRef: &v1.Ref{Revision: 2}, RequirementsVersion: 3, InputVersion: 4, ControlGeneration: 5, PlanningGeneration: 6, AllowedPurposes: []string{"INTERPRET_INPUT"}, MaxModelPositions: 1, MaxModelSends: 1}
	question := &v1.QuestionProposal{Question: "Which destination?", ConditionIds: []string{"destination"}, ChangesBasis: true}
	for name, factory := range map[string]func() reasoner.Reasoner{
		"scripted": func() reasoner.Reasoner { return &scripted.Reasoner{Question: question} },
		"default": func() reasoner.Reasoner {
			return &defaultreasoner.Reasoner{Settings: &v1.ModelSettings{Model: "model-v1"}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			provider := &model{output: []byte(`{"kind":"QUESTION","question":{"question":"Which destination?","conditionIds":["destination"],"changesBasis":true}}`)}
			p, e := factory().Propose(context.Background(), snapshot, &reasoner.Invocation{Model: provider})
			if e != nil || p.Kind != "QUESTION" || p.Question.Question != question.Question || p.RequirementsVersion != 3 || p.InputVersion != 4 || p.ControlGeneration != 5 || p.PlanningGeneration != 6 {
				t.Fatalf("proposal %v: %v", p, e)
			}
			if provider.calls > 1 {
				t.Fatal("hidden model calls")
			}
		})
	}
}

// 规则：G2、G4、G5、G6、准入-2
func TestDefaultParsesFourKindsAndRejectsAuthorityAndPurposeChanges(t *testing.T) {
	s := &v1.ContextSnapshot{TaskRef: &v1.Ref{Name: &v1.GlobalName{UserId: "u", LocalId: "task"}}, RequirementsVersion: 3, AllowedPurposes: []string{"PLAN"}, MaxModelPositions: 1, MaxModelSends: 1}
	for _, tc := range []struct {
		body  string
		valid bool
	}{
		{`{"kind":"ACTION","step":{"stepId":"one","capabilityRef":{"revision":"1"},"parametersRef":{"revision":"1"}}}`, true},
		{`{"kind":"REQUIREMENTS","requirementsChange":{"originalVersion":"3","conditions":[{"conditionId":"done","necessary":true,"verificationRule":"USER_EVALUATION","ruleVersion":1}],"reason":"Ask user to accept","impact":"completion"}}`, true},
		{`{"kind":"COMPLETE","verdicts":[{"conditionId":"done","conclusion":"UNKNOWN","gaps":["confirmation missing"]}],"gaps":["confirmation missing"]}`, true},
		{`{"kind":"QUESTION","question":{"question":"Destination?"},"taskId":{"localId":"other"}}`, false},
		{`{"kind":"ACTION","step":{"stepId":"one"},"question":{"question":"also"}}`, false},
		{`{"kind":"QUESTION","question":{"question":"Destination?"},"fees":0}`, false},
		{`{"kind":"QUESTION","kind":"COMPLETE"}`, false},
	} {
		m := &model{output: []byte(tc.body)}
		p, e := (&defaultreasoner.Reasoner{}).Propose(context.Background(), s, &reasoner.Invocation{Model: m})
		if (e == nil) != tc.valid {
			t.Fatalf("%s: %v %v", tc.body, p, e)
		}
		if m.calls != 1 {
			t.Fatalf("calls %d", m.calls)
		}
	}
	s.AllowedPurposes = []string{"INTERPRET_INPUT"}
	m := &model{output: []byte(`{"kind":"COMPLETE"}`)}
	if _, e := (&defaultreasoner.Reasoner{}).Propose(context.Background(), s, &reasoner.Invocation{Model: m}); e == nil {
		t.Fatal("completion despite pending input")
	}
}

// 规则：G1、G5、G8、G9
func TestDeterministicViewClipsWholeOptionalMaterialsAndRetainsUnknown(t *testing.T) {
	mandatory := reasoner.ViewMaterial{Ref: &v1.Ref{Name: &v1.GlobalName{LocalId: "goal"}}, Body: []byte("send one invoice"), Required: true}
	a := reasoner.ViewMaterial{Ref: &v1.Ref{Name: &v1.GlobalName{LocalId: "a"}}, Body: []byte("earlier complete interaction")}
	z := reasoner.ViewMaterial{Ref: &v1.Ref{Name: &v1.GlobalName{LocalId: "z"}}, Body: make([]byte, 4000)}
	facts := []byte(`{"effect":"UNKNOWN","executionReportPending":true,"requirementsVersion":3}`)
	one, e := reasoner.BuildView(facts, []reasoner.ViewMaterial{z, mandatory, a}, 2000, 2000)
	if e != nil {
		t.Fatal(e)
	}
	two, e := reasoner.BuildView(facts, []reasoner.ViewMaterial{a, z, mandatory}, 2000, 2000)
	if e != nil || string(one) != string(two) {
		t.Fatal("selection depends on input enumeration")
	}
	var view struct {
		Facts   map[string]any
		Inputs  []struct{ Body []byte }
		Omitted []any
	}
	if e = json.Unmarshal(one, &view); e != nil || view.Facts["effect"] != "UNKNOWN" || len(view.Inputs) != 2 || len(view.Omitted) != 1 {
		t.Fatalf("view %s: %v", one, e)
	}
	mandatory.Body = make([]byte, 4000)
	if _, e = reasoner.BuildView(facts, []reasoner.ViewMaterial{mandatory}, 2000, 2000); e == nil {
		t.Fatal("mandatory body silently clipped")
	}
}

// 规则：G2、G5、G6、R1、R6
func TestAllImplementationsUseSameFourKindConformance(t *testing.T) {
	factories := map[string]func(*v1.Proposal) reasoner.Reasoner{
		"scripted": func(p *v1.Proposal) reasoner.Reasoner { return &scripted.Reasoner{Template: p} },
		"default":  func(_ *v1.Proposal) reasoner.Reasoner { return &defaultreasoner.Reasoner{} },
	}
	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			for _, p := range []*v1.Proposal{
				{Kind: "ACTION", Step: &v1.ActionStep{StepId: "one", CapabilityRef: &v1.Ref{Revision: 1}, ParametersRef: &v1.Ref{Revision: 1}}},
				{Kind: "QUESTION", Question: &v1.QuestionProposal{Question: "Which destination?", ChangesBasis: true}},
				{Kind: "REQUIREMENTS", RequirementsChange: &v1.RequirementsProposal{OriginalVersion: 2, Reason: "clarify output", Impact: "completion", Conditions: []*v1.Requirement{{ConditionId: "done", VerificationRule: "USER_EVALUATION", RuleVersion: 1}}}},
				{Kind: "COMPLETE", Verdicts: []*v1.ConditionVerdict{{ConditionId: "done", Conclusion: "UNKNOWN", Gaps: []string{"confirmation missing"}}}, Gaps: []string{"confirmation missing"}},
			} {
				t.Run(p.Kind, func(t *testing.T) {
					s := &v1.ContextSnapshot{TaskRef: &v1.Ref{Name: &v1.GlobalName{UserId: "u", LocalId: "task"}}, RequestRef: &v1.Ref{Revision: 1}, Ref: &v1.Ref{Revision: 2}, RequirementsVersion: 2, InputVersion: 3, ControlGeneration: 4, PlanningGeneration: 5, AllowedPurposes: []string{"PLAN"}, MaxModelPositions: 1, MaxModelSends: 1}
					encoded, e := protojson.Marshal(p)
					if e != nil {
						t.Fatal(e)
					}
					m := &model{output: encoded}
					impl := factory(p)
					description, e := impl.DescribeReasoner(1)
					if e != nil || description.MaxCallPositions != 1 || description.MaxPhysicalSends != 1 || description.MaxPlanLength != 1 || len(description.SupportedKinds) != 4 {
						t.Fatalf("description %v %v", description, e)
					}
					proposed, e := impl.Propose(context.Background(), s, &reasoner.Invocation{Model: m})
					if e != nil || proposed.Kind != p.Kind || proposed.RequirementsVersion != 2 || proposed.InputVersion != 3 || proposed.ControlGeneration != 4 || proposed.PlanningGeneration != 5 || proposed.Ref != nil || m.calls > 1 {
						t.Fatalf("proposal %v %v", proposed, e)
					}
					if p.Kind == "ACTION" || p.Kind == "COMPLETE" {
						s.AllowedPurposes = []string{"INTERPRET_INPUT"}
						if _, e = impl.Propose(context.Background(), s, &reasoner.Invocation{Model: m}); e == nil {
							t.Fatal("ordinary proposal despite unprocessed input")
						}
					}
				})
			}
		})
	}
}
