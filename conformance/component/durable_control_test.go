//go:build integration

package component_test

import (
	"context"
	decision "github.com/ruipengliu/lerna/components/decision_engine"
	fixture "github.com/ruipengliu/lerna/conformance/internal/decisionfixture"
	v "github.com/ruipengliu/lerna/contract/v1_1"
	"testing"
	"time"
)

func TestDurableCancelBeforeDecideSurvivesBothOwnersReopen(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	world := fixture.NewWorld(t, ctx)
	scene := world.Scenario()
	request := scene.Request
	request.Target.ID = "first-cancelled-decision"
	request.Payload.DecisionID = request.Target.ID
	digest, err := v.DecisionInputDigest(request, scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	until, err := time.Parse("2006-01-02T15:04:05.000000Z", string(request.AcceptBefore))
	if err != nil {
		t.Fatal(err)
	}
	access := decision.ControlAccess{Subject: scene.Subject, DecisionOwner: v.OwnerRef{TenantID: request.Target.TenantID, OwnerID: request.Target.OwnerID}, DecisionRef: &request.Target, Purposes: []string{"cancel", "get"}, ValidUntil: until}
	if err = world.Source().SeedControlAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	basis, err := world.Source().IssueControl(ctx, fixture.ControlClaim{Subject: scene.Subject, DecisionRef: request.Target, TaskRef: request.Payload.TaskRef, InputDigest: v.SchemaDigest(digest), ControlRevision: "2", ValidUntil: request.AcceptBefore})
	if err != nil {
		t.Fatal(err)
	}
	control := v.DecisionCancelRequest{ContractVersion: v.Version, Profile: "decision_engine", CommandID: "cancel-before-decide", Target: request.Target, Method: "decision_engine.cancel", AcceptBefore: request.AcceptBefore, Payload: v.DecisionCancelPayload{DecisionRef: request.Target, TaskRef: request.Payload.TaskRef, DecisionInputDigest: v.SchemaDigest(digest), ControlBasis: basis, Reason: "stop before Snapshot admission"}}
	raw, err := v.Encode(control)
	if err != nil {
		t.Fatal(err)
	}
	service := world.Service()
	result, err := service.Cancel(ctx, raw, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	received, ok := result.AsReceived()
	if !ok {
		t.Fatal("cancel receipt not durably received")
	}
	applied, ok := received.Receipt.AsApplied()
	if !ok {
		t.Fatal("valid cancel before decide not applied")
	}
	if applied.ObjectRef.ID != request.Target.ID {
		t.Fatal("cancel receipt changed original identity")
	}
	world.Reopen(ctx)
	service = world.Service()
	getJSON, err := v.Encode(v.DecisionGetRequest{ContractVersion: v.Version, Profile: "decision_engine", CommandID: "read-closed", Target: request.Target, Method: "decision_engine.get", AcceptBefore: request.AcceptBefore, Payload: v.DecisionGetPayload{DecisionRef: request.Target}})
	if err != nil {
		t.Fatal(err)
	}
	view, err := service.Get(ctx, getJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	found, ok := view.AsFound()
	if !ok {
		t.Fatal("reopened control-only Decision unavailable")
	}
	closed, ok := found.Decision.AsCancelled()
	if !ok {
		t.Fatal("reopened tombstone was not cancelled")
	}
	if closed.Input != nil || closed.InputDigest != v.SchemaDigest(digest) || closed.TaskRef != request.Payload.TaskRef || closed.Usage.RuleStarts != "0" {
		t.Fatal("pre-admission close invented input or execution")
	}
	late := world.AdditionalScenario(ctx, request.Target.OwnerID, request.Target.ID, until)
	late.Request.CommandID = "late-decide"
	lateJSON, err := v.Encode(late.Request)
	if err != nil {
		t.Fatal(err)
	}
	decisionResult, err := service.Decide(ctx, lateJSON, &late.Subject)
	if err != nil {
		t.Fatal(err)
	}
	lateReceived, ok := decisionResult.AsReceived()
	if !ok {
		t.Fatal("late decide receipt unavailable")
	}
	refused, ok := lateReceived.Receipt.AsRejected()
	if !ok || refused.Reason != "decision_cancelled" {
		t.Fatal("late identical input resurrected cancelled Decision")
	}
	step, err := service.Step(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if step.Processed != 0 {
		t.Fatal("cancel-before-decide created runnable work")
	}
	pool, err := service.ObservePool(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if pool.Queued["ordinary"] != 0 || pool.Active["ordinary"] != 0 {
		t.Fatal("closed binding retained new execution responsibility")
	}
	world.Reopen(ctx)
	result, err = world.Service().Cancel(ctx, raw, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	replay, ok := result.AsReceived()
	if !ok {
		t.Fatal("fixed cancel replay unavailable")
	}
	want, err := v.Encode(received.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	got, err := v.Encode(replay.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != string(got) {
		t.Fatal("reopen changed original applied receipt")
	}
}
