//go:build integration

package decisionfixture_test

import (
	"context"
	decision "github.com/ruipengliu/lerna/components/decision_engine"
	fixture "github.com/ruipengliu/lerna/conformance/internal/decisionfixture"
	v "github.com/ruipengliu/lerna/contract/v1_1"
	"testing"
	"time"
)

func TestDurableControlWithoutSnapshotSurvivesSourceReopen(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	world := fixture.NewSourceWorld(t, ctx)
	scene := world.Scenario()
	ref := scene.DecisionRef
	ref.ID = "cancel-before-any-snapshot"
	until := time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond)
	access := decision.ControlAccess{Subject: scene.Subject, DecisionOwner: v.OwnerRef{TenantID: ref.TenantID, OwnerID: ref.OwnerID}, DecisionRef: &ref, Purposes: []string{"cancel", "get"}, ValidUntil: until}
	if err := world.Source().SeedControlAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	claim := fixture.ControlClaim{Subject: scene.Subject, DecisionRef: ref, TaskRef: scene.Request.Payload.TaskRef, InputDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ControlRevision: "9007199254740993", ValidUntil: v.Time(until.Format("2006-01-02T15:04:05.000000Z"))}
	basis, err := world.Source().IssueControl(ctx, claim)
	if err != nil {
		t.Fatal(err)
	}
	world.Reopen(ctx)
	if _, err := world.Source().AuthorizeControl(ctx, scene.Subject, ref, "cancel"); err != nil {
		t.Fatal(err)
	}
	payload := v.DecisionCancelPayload{DecisionRef: ref, TaskRef: claim.TaskRef, DecisionInputDigest: claim.InputDigest, ControlBasis: basis, Reason: "stop before admission"}
	floor, err := world.Source().VerifyControl(ctx, scene.Subject, payload, nil)
	if err != nil {
		t.Fatal(err)
	}
	if floor != nil {
		t.Fatal("control without an input invented a Snapshot floor")
	}
	if _, err = world.Source().Authorize(ctx, scene.Subject, ref, "decide", &scene.Request.Payload); err == nil {
		t.Fatal("control scope granted execution access")
	}
}
