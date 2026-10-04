//go:build integration

package component_test

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"slices"
	"strconv"
	"testing"
	"time"

	decision "github.com/ruipengliu/lerna/components/decision_engine"
	fixture "github.com/ruipengliu/lerna/conformance/internal/decisionfixture"
	v "github.com/ruipengliu/lerna/contract/v1_1"
	"github.com/ruipengliu/lerna/runtime"
)

type observedProposalPublication struct {
	key     string
	ref     v.ContentRef
	body    []byte
	sources []v.ContentRef
}

// This observer delegates every identity and body to the real independent
// publisher. Its finite reply-loss option is a mechanical boundary fault;
// business assertions read the actual Source facts, never call counts.
type proposalPublisherObserver struct {
	decision.Publisher
	planned        []observedProposalPublication
	loseFirstReply bool
	lost           bool
}

func (p *proposalPublisherObserver) PlanPublication(ctx context.Context, key string, body []byte, sources []v.ContentRef, permission decision.Permission) (v.ContentRef, error) {
	ref, err := p.Publisher.PlanPublication(ctx, key, body, sources, permission)
	if err == nil {
		p.planned = append(p.planned, observedProposalPublication{key, ref, slices.Clone(body), slices.Clone(sources)})
	}
	return ref, err
}

func (p *proposalPublisherObserver) Publish(ctx context.Context, key string, body []byte, sources []v.ContentRef, permission decision.Permission) (v.ContentRef, error) {
	ref, err := p.Publisher.Publish(ctx, key, body, sources, permission)
	if err == nil && p.loseFirstReply && !p.lost {
		p.lost = true
		return v.ContentRef{}, errors.New("fixture publication reply lost after independent commit")
	}
	return ref, err
}

func TestDurableProposalOriginalLimitsMatrix(t *testing.T) {
	type limitCase struct {
		name, rule                 string
		steps, cost, input, output v.Revision
		actions                    v.Revision
		failure                    v.DecisionFailure
		starts, confirmed          v.Revision
	}
	cases := []limitCase{
		{"zero_steps", "delta_only", "0", "1", "1048576", "1048576", "4", "rule_limit_exceeded", "0", "0"},
		{"zero_cost", "delta_only", "1", "0", "1048576", "1048576", "4", "budget_exhausted", "0", "0"},
		{"bounded_input", "input_request", "1", "1", "1", "1048576", "4", "input_over_limit", "1", "0"},
		{"whole_artifact_and_proposal", "delta_candidate_result", "1", "1", "1048576", "64", "4", "output_over_limit", "1", "1"},
		{"whole_no_artifact_proposal", "delta_only", "1", "1", "1048576", "64", "4", "output_over_limit", "1", "1"},
	}
	for actions := 0; actions <= 4; actions++ {
		failure := v.DecisionFailure("proposal_invalid")
		if actions == 4 {
			failure = ""
		}
		cases = append(cases, limitCase{"max_actions_" + strconv.Itoa(actions), "actions_four", "1", "1", "1048576", "1048576", v.Revision(strconv.Itoa(actions)), failure, "1", "1"})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			world := fixture.NewWorld(t, ctx)
			scene, _ := proposalScenario(t, ctx, world, "fixture-rule/3", tc.rule)
			limits := &scene.Request.Payload.Limits
			limits.MaxRuleSteps, limits.MaxCost.IntegerValue = tc.steps, tc.cost
			limits.MaxInputBytes, limits.MaxOutputBytes, limits.MaxActions = tc.input, tc.output, tc.actions
			observer := &proposalPublisherObserver{Publisher: world.Source()}
			service := proposalServiceWithPublisher(t, world, scene, observer)
			receipt := acceptAccounting(t, ctx, service, scene)
			if _, err := service.Step(ctx); err != nil {
				t.Fatal(err)
			}
			world.Reopen(ctx)
			service = proposalService(t, world, scene)
			view, err := service.Get(ctx, scene.GetJSON, &scene.Subject)
			if err != nil {
				t.Fatal(err)
			}
			found, ok := view.AsFound()
			if !ok {
				t.Fatal("original bounded Decision unavailable")
			}
			var usage v.DecisionUsage
			if tc.failure == "" {
				completed, ok := found.Decision.AsCompleted()
				if !ok || completed.Input.Limits != *limits {
					t.Fatal("legal four-action limit did not complete with its original allowance")
				}
				usage = completed.Usage
				permission, err := world.Source().Authorize(ctx, scene.Subject, scene.DecisionRef, "get", nil)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := world.Source().ReadPublished(ctx, completed.ProposalRef, permission); err != nil {
					t.Fatal("legal bounded Proposal is not independently readable", err)
				}
			} else {
				failed, ok := found.Decision.AsFailed()
				if !ok || failed.Failure != tc.failure || failed.Input.Limits != *limits {
					t.Fatalf("expected original bounded failure %s", tc.failure)
				}
				usage = failed.Usage
				permission, err := world.Source().Authorize(ctx, scene.Subject, scene.DecisionRef, "get", nil)
				if err != nil {
					t.Fatal(err)
				}
				for _, output := range observer.planned {
					if _, err := world.Source().ReadPublished(ctx, output.ref, permission); !errors.Is(err, decision.ErrForbidden) {
						t.Fatal("over-limit output became independently readable", err)
					}
				}
			}
			if usage.RuleStarts != tc.starts || usage.RuleSteps != tc.confirmed || usage.Cost.IntegerValue != tc.starts || usage.ModelRequests != "0" || !usage.MeasurementsComplete {
				t.Fatal("original limit reset or fabricated measurement", usage)
			}
			command, err := service.GetCommand(ctx, scene.CommandGetJSON, &scene.Subject)
			if err != nil {
				t.Fatal(err)
			}
			fixed, ok := command.AsFound()
			if !ok || !reflect.DeepEqual(fixed.Receipt, receipt) {
				t.Fatal("limit changed original accepted receipt")
			}
			if step, err := service.Step(ctx); err != nil || step.Processed != 0 {
				t.Fatal("bounded terminal retained runnable repair", err)
			}
		})
	}
}

func TestDurableProposalPreparedV2RecoversActualReplyLoss(t *testing.T) {
	for _, rule := range []string{"delta_only", "delta_candidate_result"} {
		t.Run(rule, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			world := fixture.NewWorld(t, ctx)
			scene, _ := proposalScenario(t, ctx, world, "fixture-rule/3", rule)
			scene.Request.Payload.Limits.MaxRuleSteps = "1"
			scene.Request.Payload.Limits.MaxCost.IntegerValue = "1"
			observer := &proposalPublisherObserver{Publisher: world.Source(), loseFirstReply: true}
			service := proposalServiceWithPublisher(t, world, scene, observer)
			receipt := acceptAccounting(t, ctx, service, scene)
			step, err := service.Step(ctx)
			if err != nil {
				t.Fatal(err)
			}
			view, err := service.Get(ctx, scene.GetJSON, &scene.Subject)
			if err != nil {
				t.Fatal(err)
			}
			found, ok := view.AsFound()
			if !ok {
				t.Fatal("reply-loss original Decision unavailable")
			}
			waiting, ok := found.Decision.AsWaiting()
			if !ok || waiting.Reason != "dependency_unavailable" || waiting.Usage.RuleStarts != "1" || waiting.Usage.Cost.IntegerValue != "1" || waiting.Usage.RuleSteps != "1" || !waiting.Usage.MeasurementsComplete {
				t.Fatal("durable handoff/usage missing before publication retry")
			}
			permission, err := world.Source().Authorize(ctx, scene.Subject, scene.DecisionRef, "get", nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(observer.planned) == 0 {
				t.Fatal("actual publisher produced no immutable identity")
			}
			first := observer.planned[0]
			body, err := world.Source().ReadPublished(ctx, first.ref, permission)
			if err != nil || !bytes.Equal(body, first.body) {
				t.Fatal("lost reply did not follow actual independent publication", err)
			}
			world.Reopen(ctx)
			service = proposalService(t, world, scene)
			if err := (runtime.WallTimer{}).Wait(ctx, time.Until(step.NextWake)+time.Millisecond); err != nil {
				t.Fatal(err)
			}
			if step, err := service.Step(ctx); err != nil || step.Processed != 1 {
				t.Fatal("replacement did not resume actual prepared output", err)
			}
			view, err = service.Get(ctx, scene.GetJSON, &scene.Subject)
			if err != nil {
				t.Fatal(err)
			}
			found, ok = view.AsFound()
			if !ok {
				t.Fatal("recovered Decision unavailable")
			}
			completed, ok := found.Decision.AsCompleted()
			if !ok || completed.Usage != waiting.Usage || completed.InputDigest != waiting.InputDigest {
				t.Fatal("recovery recalculated, reset observations or charged a second start")
			}
			if rule == "delta_only" && len(completed.ArtifactRefs) != 0 || rule == "delta_candidate_result" && len(completed.ArtifactRefs) != 1 {
				t.Fatal("recovery invented or lost an actual artifact")
			}
			permission, err = world.Source().Authorize(ctx, scene.Subject, scene.DecisionRef, "get", nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, output := range observer.planned {
				body, err := world.Source().ReadPublished(ctx, output.ref, permission)
				if err != nil || !bytes.Equal(body, output.body) {
					t.Fatal("recovery lost the real publisher's exact original identity/body", err)
				}
			}
			command, err := service.GetCommand(ctx, scene.CommandGetJSON, &scene.Subject)
			if err != nil {
				t.Fatal(err)
			}
			fixed, ok := command.AsFound()
			if !ok || !reflect.DeepEqual(fixed.Receipt, receipt) {
				t.Fatal("prepared recovery changed accepted receipt")
			}
		})
	}
}

func TestDurableProposalRuleVersionRetainsUnknownCaseMeaning(t *testing.T) {
	for _, tc := range []struct{ version, rule string }{{"fixture-rule/2", "actions_four"}, {"fixture-rule/3", "undefined_case"}} {
		t.Run(tc.version, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			world := fixture.NewWorld(t, ctx)
			scene, _ := proposalScenario(t, ctx, world, tc.version, tc.rule)
			service := proposalService(t, world, scene)
			receipt := acceptAccounting(t, ctx, service, scene)
			if _, err := service.Step(ctx); err != nil {
				t.Fatal(err)
			}
			world.Reopen(ctx)
			service = proposalService(t, world, scene)
			view, err := service.Get(ctx, scene.GetJSON, &scene.Subject)
			if err != nil {
				t.Fatal(err)
			}
			found, ok := view.AsFound()
			if !ok {
				t.Fatal("unknown fixed case unavailable after reopen")
			}
			failed, ok := found.Decision.AsFailed()
			if !ok || failed.Failure != "proposal_invalid" || failed.Usage.RuleStarts != "1" || failed.Usage.Cost.IntegerValue != "1" || failed.Usage.OutputBytes != "0" {
				t.Fatal("unsupported case fell back or changed its old fixed meaning")
			}
			command, err := service.GetCommand(ctx, scene.CommandGetJSON, &scene.Subject)
			if err != nil {
				t.Fatal(err)
			}
			fixed, ok := command.AsFound()
			if !ok || !reflect.DeepEqual(fixed.Receipt, receipt) {
				t.Fatal("unsupported case changed original acceptance")
			}
		})
	}
}

func TestDurableProposalCandidateSourceEvidenceUsesAllowedPurpose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	world := fixture.NewWorld(t, ctx)
	scene, snapshot := proposalScenario(t, ctx, world, "fixture-rule/3", "candidate_source_evidence")
	service := proposalService(t, world, scene)
	acceptAccounting(t, ctx, service, scene)
	if _, err := service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	world.Reopen(ctx)
	service = proposalService(t, world, scene)
	view, err := service.Get(ctx, scene.GetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	found, ok := view.AsFound()
	if !ok {
		t.Fatal("candidate source evidence unavailable")
	}
	completed, ok := found.Decision.AsCompleted()
	if !ok {
		t.Fatal("legal evidence purpose did not complete")
	}
	candidate, ok := completed.Proposal.Advance.AsCandidateResult()
	if !ok || len(candidate.Evidence) != 1 || candidate.Evidence[0].RequirementRef != snapshot.RequirementRefs[0] || len(candidate.Evidence[0].EvidenceRefs) != 1 || candidate.Evidence[0].EvidenceRefs[0] != snapshot.MaterialRefs[0] || len(completed.ArtifactRefs) != 1 {
		t.Fatal("source evidence lost its current condition/material or real artifact")
	}
	permission, err := world.Source().Authorize(ctx, scene.Subject, scene.DecisionRef, "get", nil)
	if err != nil {
		t.Fatal(err)
	}
	if body, err := world.Source().ReadMaterial(ctx, candidate.Evidence[0].EvidenceRefs[0], "rule.evidence", permission, v.MaxBodyBytes); err != nil || string(body) != "alpha\n" {
		t.Fatal("evidence is not actually readable under its allowed purpose", err)
	}
	if _, err := world.Source().ReadPublished(ctx, completed.ProposalRef, permission); err != nil {
		t.Fatal("source-evidence Proposal is not independently readable", err)
	}
}
