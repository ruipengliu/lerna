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
)

type delegatedSubjectAuthority struct{ decision.Authority }

func (a delegatedSubjectAuthority) Authorize(ctx context.Context, subject v.SubjectBinding, ref v.DecisionRef, purpose string, input *v.DecisionDecidePayload) (decision.Permission, error) {
	p, err := a.Authority.Authorize(ctx, subject, ref, purpose, input)
	if err == nil && purpose == "decide" {
		subject.DelegationChain[0].SubjectID = "rewritten-delegate"
		p.Subject = subject
	}
	return p, err
}
func TestDurableDecisionAuthorityCannotRewriteFullDelegatedSubject(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.NewWorld(t, ctx)
	scene := w.Scenario()
	source := w.Source()
	permit, err := source.Authorize(ctx, scene.Subject, scene.DecisionRef, "start", &scene.Request.Payload)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := source.ReadSnapshot(ctx, scene.Request.Payload.SnapshotRef, permit, v.MaxBodyBytes)
	if err != nil {
		t.Fatal(err)
	}
	material, err := source.ReadMaterial(ctx, scene.MaterialRef, "rule.input", permit, v.MaxBodyBytes)
	if err != nil {
		t.Fatal(err)
	}
	scene.Subject.DelegationChain = []v.DelegatedSubject{{TenantID: scene.Subject.TenantID, SubjectID: "fixture-delegator"}}
	permit.Subject = scene.Subject
	scene.DecisionRef.ID = "delegated-decision"
	scene.Request.Target = scene.DecisionRef
	scene.Request.Payload.DecisionID = scene.DecisionRef.ID
	get, err := v.DecodeGet(scene.GetJSON)
	if err != nil {
		t.Fatal(err)
	}
	get.Target = scene.DecisionRef
	get.Payload.DecisionRef = scene.DecisionRef
	scene.GetJSON, err = v.Encode(get)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = source.Seed(ctx, fixture.Bundle{DecisionRef: scene.DecisionRef, Permission: permit, Snapshot: snapshot, Materials: []fixture.Material{{Ref: scene.MaterialRef, Bytes: material}}, Purposes: []string{"decide", "get", "command.get", "start", "material", "rule.input", "fixture.lock", "publish", "proposal.publish", "artifact.publish"}, RuleVersion: permit.RuleVersion, ChargeBasis: permit.ChargeBasis, RuleStartCharge: permit.RuleStartCharge}); err != nil {
		t.Fatal(err)
	}
	s, err := decision.New(decision.Config{Owner: permit.DecisionOwner, Store: w.Store(), Authority: delegatedSubjectAuthority{source}, Source: source, Publisher: source, Component: scene.Request.Payload.ComponentRef, Worker: "delegated", Lease: 3 * time.Second, PoolControl: true})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := v.Encode(scene.Request)
	_, err = s.Decide(ctx, raw, &scene.Subject)
	var refusal *v.ContractError
	if !errors.As(err, &refusal) || refusal.Code != "forbidden" {
		t.Fatal("authorization rewrote the full trusted subject", err)
	}
	if scene.Subject.DelegationChain[0].SubjectID != "fixture-delegator" {
		t.Fatal("authority changed the caller's binding")
	}
	normal := w.Service()
	view, err := normal.Get(ctx, scene.GetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := view.AsResultUnavailable(); !ok {
		t.Fatal("forged subject produced a Decision")
	}
	acceptAccounting(t, ctx, normal, scene)
	if _, err = normal.Step(ctx); err != nil {
		t.Fatal(err)
	}
	view, err = normal.Get(ctx, scene.GetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	found, _ := view.AsFound()
	if _, ok := found.Decision.AsCompleted(); !ok {
		t.Fatal("legitimate delegated principal did not complete")
	}
}

type modifyingAuthority struct{ decision.Authority }

func (a modifyingAuthority) Authorize(ctx context.Context, subject v.SubjectBinding, ref v.DecisionRef, purpose string, input *v.DecisionDecidePayload) (decision.Permission, error) {
	permit, err := a.Authority.Authorize(ctx, subject, ref, purpose, input)
	if err == nil && input != nil {
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
