//go:build integration

package component_test

import (
	"context"
	"errors"
	"testing"
	"time"

	decision "github.com/ruipengliu/lerna/components/decision_engine"
	fixture "github.com/ruipengliu/lerna/conformance/internal/decisionfixture"
	v "github.com/ruipengliu/lerna/contract/v1_1"
	"github.com/ruipengliu/lerna/runtime"
)

func TestDurableDecisionReadDistinguishesAbsenceAndActualStoreUnavailable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.NewWorld(t, ctx)
	scene := w.Scenario()
	s := w.Service()
	view, err := s.Get(ctx, scene.GetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := view.AsResultUnavailable(); !ok {
		t.Fatal("unwritten authorized identity was not result_unavailable")
	}
	acceptAccounting(t, ctx, s, scene)
	if err = w.Store().Close(); err != nil {
		t.Fatal(err)
	}
	view, err = s.Get(ctx, scene.GetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	unavailable, ok := view.AsUnavailable()
	if !ok || unavailable.Reason != "dependency_unavailable" {
		t.Fatal("closed database was misrepresented as absent")
	}
	w.Reopen(ctx)
	s = w.Service()
	if _, err = s.Step(ctx); err != nil {
		t.Fatal(err)
	}
	view, err = s.Get(ctx, scene.GetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	found, ok := view.AsFound()
	if !ok {
		t.Fatal("normal reopened read unavailable")
	}
	if _, ok = found.Decision.AsCompleted(); !ok {
		t.Fatal("read failure changed original work")
	}
}

// Both publications have actually been persisted and independently read. The
// read reply is withheld until the Claim expires, so Finish must fence it.
type leaseLostAfterPublication struct {
	decision.Publisher
	reached bool
}

func (p *leaseLostAfterPublication) ReadPublished(ctx context.Context, ref v.ContentRef, permit decision.Permission) ([]byte, error) {
	body, err := p.Publisher.ReadPublished(ctx, ref, permit)
	if err != nil {
		return nil, err
	}
	if _, err = v.Decode[v.Proposal](body); err == nil {
		p.reached = true
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return body, nil
}
func TestDurableDecisionFinishFencesExpiredClaimAfterBothPublications(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.NewWorld(t, ctx)
	scene := w.Scenario()
	scene.Request.Payload.Limits.MaxRuleSteps = "1"
	scene.Request.Payload.Limits.MaxCost.IntegerValue = "1"
	p := &leaseLostAfterPublication{Publisher: w.Source()}
	s := accountingService(t, w, p, "stale", 500*time.Millisecond)
	receipt := acceptAccounting(t, ctx, s, scene)
	work, err := s.Claim(ctx)
	if err != nil || work == nil {
		t.Fatal("claim", err)
	}
	err = s.RunClaim(ctx, work.Claim)
	if !p.reached || !errors.Is(err, runtime.ErrClaim) {
		t.Fatal("late publication reply crossed Finish qualification", err)
	}
	view, err := s.Get(ctx, scene.GetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	found, _ := view.AsFound()
	running, ok := found.Decision.AsRunning()
	if !ok {
		t.Fatal("stale worker published completed Proposal")
	}
	if running.Usage.RuleStarts != "1" || running.Usage.RuleSteps != "1" || running.Usage.Cost.IntegerValue != "1" || !running.Usage.MeasurementsComplete {
		t.Fatalf("prepared physical observations lost %+v", running.Usage)
	}
	w.Reopen(ctx)
	s = accountingService(t, w, w.Source(), "current", time.Second)
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
		t.Fatal("replacement recomputed or changed original fee")
	}
	candidate, _ := completed.Proposal.Advance.AsCandidateResult()
	if len(candidate.ArtifactRefs) != 1 {
		t.Fatal("original artifact absent")
	}
	body, err := w.ReadArtifact(ctx, candidate.ArtifactRefs[0])
	if err != nil || string(body) != "fixture result: alpha\n" {
		t.Fatal("artifact readback", err)
	}
	fixed, err := s.GetCommand(ctx, scene.CommandGetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	command, _ := fixed.AsFound()
	a, _ := v.Encode(receipt)
	b, _ := v.Encode(command.Receipt)
	if string(a) != string(b) {
		t.Fatal("late Finish changed original accepted receipt")
	}
}
