//go:build integration

package component_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	decision "github.com/ruipengliu/lerna/components/decision_engine"
	fixture "github.com/ruipengliu/lerna/conformance/internal/decisionfixture"
	v "github.com/ruipengliu/lerna/contract/v1_1"
	"github.com/ruipengliu/lerna/runtime"
)

// This publisher loses the first durable publication reply. The independent
// owner still holds that exact artifact, so replacement must recover its key.
type lostPublicationReply struct {
	decision.Publisher
	original v.ContentRef
	lost     bool
}

func (p *lostPublicationReply) Publish(ctx context.Context, key string, body []byte, sources []v.ContentRef, permit decision.Permission) (v.ContentRef, error) {
	ref, err := p.Publisher.Publish(ctx, key, body, sources, permit)
	if err == nil && !p.lost {
		p.lost = true
		p.original = ref
		return v.ContentRef{}, errors.New("fixture publication reply lost")
	}
	return ref, err
}
func accountingService(t *testing.T, w *fixture.World, p decision.Publisher, worker string, lease time.Duration) *decision.Service {
	t.Helper()
	scene := w.Scenario()
	s, err := decision.New(decision.Config{Owner: v.OwnerRef{TenantID: scene.DecisionRef.TenantID, OwnerID: scene.DecisionRef.OwnerID}, Store: w.Store(), Authority: w.Source(), Source: w.Source(), Publisher: p, Component: scene.Request.Payload.ComponentRef, Worker: worker, Lease: lease, PoolControl: true})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func acceptAccounting(t *testing.T, ctx context.Context, s *decision.Service, scene fixture.Scenario) v.CommandReceipt {
	t.Helper()
	raw, err := v.Encode(scene.Request)
	if err != nil {
		t.Fatal(err)
	}
	reply, err := s.Decide(ctx, raw, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	received, ok := reply.AsReceived()
	if !ok {
		t.Fatal("not received")
	}
	if _, ok := received.Receipt.AsAccepted(); !ok {
		t.Fatal("not accepted")
	}
	return received.Receipt
}
func TestDurableDecisionPreparedRecoversLostPublicationWithoutSecondFee(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.NewWorld(t, ctx)
	scene := w.Scenario()
	scene.Request.Payload.Limits.MaxRuleSteps = "1"
	scene.Request.Payload.Limits.MaxCost.IntegerValue = "1"
	publisher := &lostPublicationReply{Publisher: w.Source()}
	s := accountingService(t, w, publisher, "initial", 3*time.Second)
	receipt := acceptAccounting(t, ctx, s, scene)
	step, err := s.Step(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !publisher.lost || step.Processed != 1 {
		t.Fatal("durable reply-loss boundary not reached")
	}
	view, err := s.Get(ctx, scene.GetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	found, ok := view.AsFound()
	if !ok {
		t.Fatal("missing waiting decision")
	}
	waiting, ok := found.Decision.AsWaiting()
	if !ok {
		t.Fatal("lost reply did not retain responsibility")
	}
	if waiting.Usage.RuleStarts != "1" || waiting.Usage.RuleSteps != "1" || waiting.Usage.Cost.IntegerValue != "1" || !waiting.Usage.MeasurementsComplete {
		t.Fatalf("prepared usage %+v", waiting.Usage)
	}
	w.Reopen(ctx)
	s = accountingService(t, w, w.Source(), "replacement", 3*time.Second)
	timer := time.NewTimer(time.Until(step.NextWake))
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	step, err = s.Step(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if step.Processed != 1 {
		t.Fatal("replacement did not resume original work")
	}
	view, err = s.Get(ctx, scene.GetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	found, ok = view.AsFound()
	if !ok {
		t.Fatal("missing completed decision")
	}
	completed, ok := found.Decision.AsCompleted()
	if !ok {
		t.Fatal("prepared work did not complete within its original one-start budget")
	}
	if completed.Usage != waiting.Usage {
		t.Fatal("republication changed durable start, observations, or fee")
	}
	candidate, ok := completed.Proposal.Advance.AsCandidateResult()
	if !ok || len(candidate.ArtifactRefs) != 1 || candidate.ArtifactRefs[0] != publisher.original {
		t.Fatal("recovery replaced original durable artifact identity")
	}
	bytes, err := w.ReadArtifact(ctx, publisher.original)
	if err != nil || string(bytes) != "fixture result: alpha\n" {
		t.Fatal("original independent artifact bytes unavailable", err)
	}
	command, err := s.GetCommand(ctx, scene.CommandGetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	fixed, ok := command.AsFound()
	if !ok {
		t.Fatal("lost original receipt")
	}
	a, _ := v.Encode(receipt)
	b, _ := v.Encode(fixed.Receipt)
	if string(a) != string(b) {
		t.Fatal("recovery changed acceptance")
	}
}
func TestDurableDecisionStartIsOnePermissionAndUnknownWorkConsumesOriginalLimit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.NewWorld(t, ctx)
	scene := w.Scenario()
	scene.Request.Payload.Limits.MaxRuleSteps = "1"
	scene.Request.Payload.Limits.MaxCost.IntegerValue = "1"
	s := accountingService(t, w, w.Source(), "initial", time.Second)
	acceptAccounting(t, ctx, s, scene)
	claim, err := s.Claim(ctx)
	if err != nil || claim == nil {
		t.Fatal("claim", err)
	}
	started, err := s.Start(ctx, claim.Claim)
	if err != nil || !started.Compute {
		t.Fatal("start", err)
	}
	if _, err = s.Start(ctx, claim.Claim); !errors.Is(err, runtime.ErrClaim) {
		t.Fatal("same epoch acquired a second computation permission", err)
	}
	view, err := s.Get(ctx, scene.GetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	found, _ := view.AsFound()
	running, ok := found.Decision.AsRunning()
	if !ok {
		t.Fatal("missing running")
	}
	if running.Usage.RuleStarts != "1" || running.Usage.RuleSteps != "0" || running.Usage.Cost.IntegerValue != "1" || running.Usage.MeasurementsComplete {
		t.Fatalf("Start invented actual computation %+v", running.Usage)
	}
	w.Reopen(ctx)
	s = accountingService(t, w, w.Source(), "replacement", 3*time.Second)
	timer := time.NewTimer(time.Until(claim.Claim.LeaseUntil))
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	step, err := s.Step(ctx)
	if err != nil || step.Processed != 1 {
		t.Fatal("replacement", err)
	}
	view, err = s.Get(ctx, scene.GetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	found, _ = view.AsFound()
	failed, ok := found.Decision.AsFailed()
	if !ok || failed.Failure != "rule_limit_exceeded" {
		t.Fatal("replacement ignored original one-start bound")
	}
	if failed.Usage != running.Usage {
		t.Fatal("unknown prior invocation was recomputed or charged again")
	}
}

func TestDurableDecisionConcurrentStartAndCumulativeUnknownUsage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.NewWorld(t, ctx)
	scene := w.Scenario()
	scene.Request.Payload.Limits.MaxRuleSteps = "2"
	scene.Request.Payload.Limits.MaxCost.IntegerValue = "2"
	s := accountingService(t, w, w.Source(), "initial", time.Second)
	acceptAccounting(t, ctx, s, scene)
	claimed, err := s.Claim(ctx)
	if err != nil || claimed == nil {
		t.Fatal("claim", err)
	}
	gate := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Go(func() { <-gate; _, err := s.Start(ctx, claimed.Claim); results <- err })
	}
	close(gate)
	workers.Wait()
	close(results)
	allowed, denied := 0, 0
	for err := range results {
		if err == nil {
			allowed++
		} else if errors.Is(err, runtime.ErrClaim) {
			denied++
		} else {
			t.Fatal(err)
		}
	}
	if allowed != 1 || denied != 1 {
		t.Fatalf("concurrent durable permissions: allowed=%d denied=%d", allowed, denied)
	}
	w.Reopen(ctx)
	s = accountingService(t, w, w.Source(), "replacement", 3*time.Second)
	if err = (runtime.WallTimer{}).Wait(ctx, time.Until(claimed.Claim.LeaseUntil)+time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if step, err := s.Step(ctx); err != nil || step.Processed != 1 {
		t.Fatal("replacement", err)
	}
	view, err := s.Get(ctx, scene.GetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	found, _ := view.AsFound()
	completed, ok := found.Decision.AsCompleted()
	if !ok {
		t.Fatal("normal replacement did not complete within original two-start limit")
	}
	if completed.Usage.RuleStarts != "2" || completed.Usage.Cost.IntegerValue != "2" || completed.Usage.RuleSteps != "1" || completed.Usage.MeasurementsComplete || completed.Usage.InputBytes == "0" || completed.Usage.OutputBytes == "0" {
		t.Fatalf("unknown prior invocation was erased %+v", completed.Usage)
	}
}

func TestDurableDecisionOriginalLimitsGateStartAndWholePreparedOutput(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		steps, cost, input, output v.Revision
		failure                    v.DecisionFailure
		starts, observed           v.Revision
	}{{"zero_steps", "0", "1", "1048576", "1048576", "rule_limit_exceeded", "0", "0"}, {"zero_cost", "1", "0", "1048576", "1048576", "budget_exhausted", "0", "0"}, {"bounded_input", "1", "1", "1", "1048576", "input_over_limit", "1", "0"}, {"whole_output", "1", "1", "1048576", "64", "output_over_limit", "1", "1"}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			w := fixture.NewWorld(t, ctx)
			scene := w.Scenario()
			scene.Request.Payload.Limits.MaxRuleSteps = tc.steps
			scene.Request.Payload.Limits.MaxCost.IntegerValue = tc.cost
			scene.Request.Payload.Limits.MaxInputBytes = tc.input
			scene.Request.Payload.Limits.MaxOutputBytes = tc.output
			p := &publicationForbidden{Publisher: w.Source(), t: t}
			s := accountingService(t, w, p, "bounded", time.Second)
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
			if !ok || failed.Failure != tc.failure {
				t.Fatalf("expected %s", tc.failure)
			}
			if failed.Usage.RuleStarts != tc.starts || failed.Usage.RuleSteps != tc.observed || failed.Usage.Cost.IntegerValue != tc.starts || !failed.Usage.MeasurementsComplete {
				t.Fatalf("usage %+v", failed.Usage)
			}
		})
	}
}

type publicationForbidden struct {
	decision.Publisher
	t *testing.T
}

func (p *publicationForbidden) Publish(context.Context, string, []byte, []v.ContentRef, decision.Permission) (v.ContentRef, error) {
	p.t.Error("over-limit work reached external publication")
	return v.ContentRef{}, errors.New("forbidden publication")
}
