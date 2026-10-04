//go:build integration

package component_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	decision "github.com/ruipengliu/lerna/components/decision_engine"
	fixture "github.com/ruipengliu/lerna/conformance/internal/decisionfixture"
	v "github.com/ruipengliu/lerna/contract/v1_1"
	"github.com/ruipengliu/lerna/runtime"
)

// Cancellation happens at an independent publisher boundary, after its real
// planning transaction. It never injects prepared bytes or a Decision record.
type interruptHandoff struct {
	decision.Publisher
	cancel        context.CancelFunc
	beforePublish bool
	reached       bool
	artifact      v.ContentRef
}

func (p *interruptHandoff) PlanPublication(ctx context.Context, key string, body []byte, sources []v.ContentRef, permit decision.Permission) (v.ContentRef, error) {
	ref, err := p.Publisher.PlanPublication(ctx, key, body, sources, permit)
	if err != nil {
		return ref, err
	}
	if strings.HasSuffix(key, "/artifact") {
		p.artifact = ref
	}
	if !p.beforePublish && strings.HasSuffix(key, "/proposal") {
		p.reached = true
		p.cancel()
	}
	return ref, nil
}
func (p *interruptHandoff) Publish(ctx context.Context, key string, body []byte, sources []v.ContentRef, permit decision.Permission) (v.ContentRef, error) {
	if p.beforePublish && !p.reached {
		p.reached = true
		p.cancel()
		return v.ContentRef{}, ctx.Err()
	}
	return p.Publisher.Publish(ctx, key, body, sources, permit)
}
func TestDurableDecisionInterruptedComputedOutputRetainsUnknownAllowance(t *testing.T) {
	for _, tc := range []struct {
		name        string
		steps, cost v.Revision
		failure     v.DecisionFailure
	}{{"remaining_original_allowance", "2", "2", ""}, {"original_steps_spent", "1", "2", "rule_limit_exceeded"}, {"original_cost_spent", "2", "1", "budget_exhausted"}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			w := fixture.NewWorld(t, ctx)
			scene := w.Scenario()
			scene.Request.Payload.Limits.MaxRuleSteps = tc.steps
			scene.Request.Payload.Limits.MaxCost.IntegerValue = tc.cost
			interrupted, stop := context.WithCancel(ctx)
			defer stop()
			p := &interruptHandoff{Publisher: w.Source(), cancel: stop}
			s := accountingService(t, w, p, "initial", time.Second)
			acceptAccounting(t, ctx, s, scene)
			claim, err := s.Claim(ctx)
			if err != nil || claim == nil {
				t.Fatal("claim", err)
			}
			err = s.RunClaim(interrupted, claim.Claim)
			if !p.reached || err == nil {
				t.Fatal("last planning transaction did not interrupt the handoff", err)
			}
			view, err := s.Get(ctx, scene.GetJSON, &scene.Subject)
			if err != nil {
				t.Fatal(err)
			}
			found, _ := view.AsFound()
			running, ok := found.Decision.AsRunning()
			if !ok || running.Usage.RuleStarts != "1" || running.Usage.RuleSteps != "0" || running.Usage.Cost.IntegerValue != "1" || running.Usage.MeasurementsComplete {
				t.Fatal("unconfirmed physical computation was presented as exact")
			}
			permit, err := w.Source().Authorize(ctx, scene.Subject, scene.DecisionRef, "get", nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = w.Source().ReadPublished(ctx, p.artifact, permit); !errors.Is(err, decision.ErrForbidden) {
				t.Fatal("planning published an artifact", err)
			}
			// RunClaim has returned: no old goroutine/writer can resume after reopening.
			w.Reopen(ctx)
			s = accountingService(t, w, w.Source(), "replacement", 3*time.Second)
			if err = (runtime.WallTimer{}).Wait(ctx, time.Until(claim.Claim.LeaseUntil)+time.Millisecond); err != nil {
				t.Fatal(err)
			}
			if step, err := s.Step(ctx); err != nil || step.Processed != 1 {
				t.Fatal("replacement", err)
			}
			view, err = s.Get(ctx, scene.GetJSON, &scene.Subject)
			if err != nil {
				t.Fatal(err)
			}
			found, _ = view.AsFound()
			if tc.failure != "" {
				failed, ok := found.Decision.AsFailed()
				if !ok || failed.Failure != tc.failure || failed.Usage != running.Usage {
					t.Fatal("replacement refreshed the original computation or cost limit")
				}
				permit, err = w.Source().Authorize(ctx, scene.Subject, scene.DecisionRef, "get", nil)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = w.Source().ReadPublished(ctx, p.artifact, permit); !errors.Is(err, decision.ErrForbidden) {
					t.Fatal("exhausted replacement published the old planned output", err)
				}
				return
			}
			completed, ok := found.Decision.AsCompleted()
			if !ok || completed.Usage.RuleStarts != "2" || completed.Usage.Cost.IntegerValue != "2" || completed.Usage.RuleSteps != "1" || completed.Usage.MeasurementsComplete {
				t.Fatal("replacement erased an unconfirmed computation")
			}
			candidate, _ := completed.Proposal.Advance.AsCandidateResult()
			if len(candidate.ArtifactRefs) != 1 || candidate.ArtifactRefs[0] != p.artifact {
				t.Fatal("replacement changed the fixed planned artifact")
			}
			body, err := w.ReadArtifact(ctx, p.artifact)
			if err != nil || string(body) != "fixture result: alpha\n" {
				t.Fatal("independent original bytes", err)
			}
		})
	}
}
func TestDurableDecisionPreparedBeforePublicationResumesWithoutNewStart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.NewWorld(t, ctx)
	scene := w.Scenario()
	scene.Request.Payload.Limits.MaxRuleSteps = "1"
	scene.Request.Payload.Limits.MaxCost.IntegerValue = "1"
	interrupted, stop := context.WithCancel(ctx)
	defer stop()
	p := &interruptHandoff{Publisher: w.Source(), cancel: stop, beforePublish: true}
	s := accountingService(t, w, p, "initial", time.Second)
	acceptAccounting(t, ctx, s, scene)
	claim, err := s.Claim(ctx)
	if err != nil || claim == nil {
		t.Fatal("claim", err)
	}
	err = s.RunClaim(interrupted, claim.Claim)
	if !p.reached || err == nil {
		t.Fatal("first publication boundary not interrupted", err)
	}
	view, err := s.Get(ctx, scene.GetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	found, _ := view.AsFound()
	running, ok := found.Decision.AsRunning()
	if !ok || running.Usage.RuleStarts != "1" || running.Usage.RuleSteps != "1" || !running.Usage.MeasurementsComplete {
		t.Fatal("prepared observations not durable before publication")
	}
	permit, err := w.Source().Authorize(ctx, scene.Subject, scene.DecisionRef, "get", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Source().ReadPublished(ctx, p.artifact, permit); !errors.Is(err, decision.ErrForbidden) {
		t.Fatal("publication boundary had already written", err)
	}
	w.Reopen(ctx)
	s = accountingService(t, w, w.Source(), "replacement", 3*time.Second)
	if err = (runtime.WallTimer{}).Wait(ctx, time.Until(claim.Claim.LeaseUntil)+time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if step, err := s.Step(ctx); err != nil || step.Processed != 1 {
		t.Fatal("replacement", err)
	}
	view, err = s.Get(ctx, scene.GetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	found, _ = view.AsFound()
	completed, ok := found.Decision.AsCompleted()
	if !ok || completed.Usage != running.Usage {
		t.Fatal("prepared recovery needed a second computation or fee")
	}
	candidate, _ := completed.Proposal.Advance.AsCandidateResult()
	if len(candidate.ArtifactRefs) != 1 || candidate.ArtifactRefs[0] != p.artifact {
		t.Fatal("prepared recovery replaced publication identity")
	}
}

type mismatchedPublication struct {
	decision.Publisher
	original v.ContentRef
}

func (p *mismatchedPublication) Publish(ctx context.Context, key string, body []byte, sources []v.ContentRef, permit decision.Permission) (v.ContentRef, error) {
	ref, err := p.Publisher.Publish(ctx, key, body, sources, permit)
	if err != nil {
		return ref, err
	}
	p.original = ref
	ref.Version = "2"
	return ref, nil
}
func TestDurableDecisionCannotCompleteWithDifferentPublisherReference(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.NewWorld(t, ctx)
	scene := w.Scenario()
	p := &mismatchedPublication{Publisher: w.Source()}
	s := accountingService(t, w, p, "mismatch", 3*time.Second)
	acceptAccounting(t, ctx, s, scene)
	if _, err := s.Step(ctx); err != nil {
		t.Fatal(err)
	}
	view, err := s.Get(ctx, scene.GetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	found, _ := view.AsFound()
	failed, ok := found.Decision.AsFailed()
	if !ok || failed.Failure != "proposal_invalid" || failed.Usage.RuleStarts != "1" || failed.Usage.Cost.IntegerValue != "1" {
		t.Fatal("publisher rewrote prepared output or lost its charge")
	}
	body, err := w.ReadArtifact(ctx, p.original)
	if err != nil || string(body) != "fixture result: alpha\n" {
		t.Fatal("original external publication fact lost", err)
	}
}
