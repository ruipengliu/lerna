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

type modifyingAuthority struct{ decision.Authority }

func (a modifyingAuthority) Authorize(ctx context.Context, subject v.SubjectBinding, ref v.DecisionRef, purpose string, input *v.DecisionDecidePayload) (decision.Permission, error) {
	permit, err := a.Authority.Authorize(ctx, subject, ref, purpose, input)
	if err == nil && purpose == "decide" {
		input.Limits.MaxRuleSteps = "0"
		input.Deadline = "2099-01-01T00:00:00.000000Z"
	}
	return permit, err
}
func TestDurableDecisionAuthorityCannotRewriteFixedCommandInput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.NewWorld(t, ctx)
	scene := w.Scenario()
	s, err := decision.New(decision.Config{Owner: v.OwnerRef{TenantID: scene.DecisionRef.TenantID, OwnerID: scene.DecisionRef.OwnerID}, Store: w.Store(), Authority: modifyingAuthority{w.Source()}, Source: w.Source(), Publisher: w.Source(), Component: scene.Request.Payload.ComponentRef, Worker: "immutable", Lease: 3 * time.Second, PoolControl: true})
	if err != nil {
		t.Fatal(err)
	}
	acceptAccounting(t, ctx, s, scene)
	view, err := s.Get(ctx, scene.GetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	found, _ := view.AsFound()
	accepted, ok := found.Decision.AsAccepted()
	if !ok {
		t.Fatal("accepted input unavailable")
	}
	original, _ := v.Encode(scene.Request.Payload)
	actual, _ := v.Encode(accepted.Input)
	if string(original) != string(actual) {
		t.Fatal("authority callback changed the input accepted under the original Command bytes")
	}
	if _, err = s.Step(ctx); err != nil {
		t.Fatal(err)
	}
	view, err = s.Get(ctx, scene.GetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	found, _ = view.AsFound()
	if _, ok = found.Decision.AsCompleted(); !ok {
		t.Fatal("original allowed rule failed after callback mutation")
	}
}
func TestDurableDecisionOriginalReceiptSurvivesComponentConfigurationChange(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.NewWorld(t, ctx)
	scene := w.Scenario()
	receipt := acceptAccounting(t, ctx, w.Service(), scene)
	component := scene.Request.Payload.ComponentRef
	component.ConfigDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	s, err := decision.New(decision.Config{Owner: v.OwnerRef{TenantID: scene.DecisionRef.TenantID, OwnerID: scene.DecisionRef.OwnerID}, Store: w.Store(), Authority: w.Source(), Source: w.Source(), Publisher: w.Source(), Component: component, Worker: "reconfigured", Lease: 3 * time.Second, PoolControl: true})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := v.Encode(scene.Request)
	out, err := s.Decide(ctx, raw, &scene.Subject)
	if err != nil {
		t.Fatal("current authenticated original key lost its fixed receipt after component update", err)
	}
	received, ok := out.AsReceived()
	if !ok {
		t.Fatal("original replay not received")
	}
	a, _ := v.Encode(receipt)
	b, _ := v.Encode(received.Receipt)
	if string(a) != string(b) {
		t.Fatal("reconfiguration changed the original accepted receipt")
	}
	scene.Request.CommandID = "fresh-old-component"
	raw, _ = v.Encode(scene.Request)
	out, err = s.Decide(ctx, raw, &scene.Subject)
	if err != nil {
		t.Fatal("new old binding needs a fixed business refusal", err)
	}
	received, ok = out.AsReceived()
	if !ok {
		t.Fatal("fresh refusal not received")
	}
	rejected, ok := received.Receipt.AsRejected()
	if !ok || rejected.Reason != "unsupported" {
		t.Fatal("fresh old component binding was admitted")
	}
}
