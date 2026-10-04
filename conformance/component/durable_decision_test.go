//go:build integration

package component_test

import (
	"context"
	"testing"
	"time"

	decision "github.com/ruipengliu/lerna/components/decision_engine"
	fixture "github.com/ruipengliu/lerna/conformance/internal/decisionfixture"
	v "github.com/ruipengliu/lerna/contract/v1_1"
)

func TestDurableRuleProposalSurvivesReopen(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	world := fixture.NewWorld(t, ctx)
	input := world.Scenario()
	service := world.Service()
	raw, err := v.Encode(input.Request)
	if err != nil {
		t.Fatal(err)
	}
	received, err := service.Decide(ctx, raw, &input.Subject)
	if err != nil {
		t.Fatal(err)
	}
	outcome, ok := received.AsReceived()
	if !ok {
		t.Fatal("normal decision was not durably received")
	}
	accepted, ok := outcome.Receipt.AsAccepted()
	if !ok {
		t.Fatal("normal decision was not accepted")
	}
	world.Reopen(ctx)
	service = world.Service()
	result, err := service.Step(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Processed != 1 {
		t.Fatalf("normal rule processed %d", result.Processed)
	}
	view, err := service.Get(ctx, input.GetJSON, &input.Subject)
	if err != nil {
		t.Fatal(err)
	}
	found, ok := view.AsFound()
	if !ok {
		t.Fatal("reopened original decision unavailable")
	}
	completed, ok := found.Decision.AsCompleted()
	if !ok {
		t.Fatal("normal rule did not complete")
	}
	candidate, ok := completed.Proposal.Advance.AsCandidateResult()
	if !ok {
		t.Fatal("normal rule did not propose candidate result")
	}
	if len(candidate.ArtifactRefs) != 1 {
		t.Fatal("normal rule requires one durable artifact")
	}
	bytes, err := world.ReadArtifact(ctx, candidate.ArtifactRefs[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(bytes) != "fixture result: alpha\n" {
		t.Fatalf("independent artifact read got %q", bytes)
	}
	command, err := service.GetCommand(ctx, input.CommandGetJSON, &input.Subject)
	if err != nil {
		t.Fatal(err)
	}
	fixed, ok := command.AsFound()
	if !ok {
		t.Fatal("original command receipt unavailable")
	}
	prior, err := v.Encode(accepted)
	if err != nil {
		t.Fatal(err)
	}
	current, err := v.Encode(fixed.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	if string(prior) != string(current) {
		t.Fatal("completion changed original accepted receipt")
	}
	_ = decision.ErrUnavailable
}
