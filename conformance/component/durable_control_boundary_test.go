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

type closedControlSource struct {
	decision.ControlAuthority
	source *fixture.Store
}

func (a closedControlSource) VerifyControl(ctx context.Context, principal v.SubjectBinding, payload v.DecisionCancelPayload, input *v.DecisionDecidePayload) (*v.Revision, error) {
	if err := a.source.Close(); err != nil {
		return nil, err
	}
	return a.ControlAuthority.VerifyControl(ctx, principal, payload, input)
}

func TestDurableControlRealSourceFailureIsDependencyUnavailable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.NewWorld(t, ctx)
	scene := w.Scenario()
	request := sceneControl(t, ctx, w, scene, "2", "source-closed-before-proof-read")
	source := w.Source()
	s, err := decision.New(decision.Config{Owner: v.OwnerRef{TenantID: scene.DecisionRef.TenantID, OwnerID: scene.DecisionRef.OwnerID}, Store: w.Store(), Authority: source, ControlAuthority: closedControlSource{ControlAuthority: source, source: source}, Source: source, Publisher: source, Component: scene.Request.Payload.ComponentRef, Worker: "control-io", Lease: time.Second, PoolControl: true})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := v.Encode(request)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Cancel(ctx, raw, &scene.Subject)
	var contractErr *v.ContractError
	if !errors.As(err, &contractErr) || contractErr.Code != "dependency_unavailable" {
		t.Fatal("actual closed proof owner was reported as an authorization denial", err)
	}
	w.Reopen(ctx)
	view, err := w.Service().Get(ctx, scene.GetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := view.AsResultUnavailable(); !ok {
		t.Fatal("failed proof dependency wrote a Decision tombstone")
	}
	receipt := controlReceipt(t, ctx, w.Service(), request, scene.Subject)
	if _, ok := receipt.AsApplied(); !ok {
		t.Fatal("same original key could not apply after dependency recovery")
	}
}

func TestDurableControlExpiredProofReplayRequiresCurrentAccess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.NewWorld(t, ctx)
	scene := w.Scenario()
	request := sceneControl(t, ctx, w, scene, "2", "short-proof-cancel")
	until := time.Now().UTC().Add(time.Second).Truncate(time.Microsecond)
	request.AcceptBefore = v.Time(until.Format("2006-01-02T15:04:05.000000Z"))
	basis, err := w.Source().IssueControl(ctx, fixture.ControlClaim{Subject: scene.Subject, DecisionRef: scene.DecisionRef, TaskRef: scene.Request.Payload.TaskRef, InputDigest: request.Payload.DecisionInputDigest, ControlRevision: "2", ValidUntil: request.AcceptBefore})
	if err != nil {
		t.Fatal(err)
	}
	request.Payload.ControlBasis = basis
	before := controlReceipt(t, ctx, w.Service(), request, scene.Subject)
	if _, ok := before.AsApplied(); !ok {
		t.Fatal("short valid proof was not first adopted")
	}
	timer := time.NewTimer(time.Until(until.Add(time.Millisecond)))
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	w.Reopen(ctx)
	replay := controlReceipt(t, ctx, w.Service(), request, scene.Subject)
	first, err := v.Encode(before)
	if err != nil {
		t.Fatal(err)
	}
	second, err := v.Encode(replay)
	if err != nil || string(first) != string(second) {
		t.Fatal("expired proof or accept_before changed original-key replay", err)
	}
	fresh := request
	fresh.CommandID = "fresh-expired-proof"
	fresh.AcceptBefore = scene.Request.AcceptBefore
	raw, err := v.Encode(fresh)
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.Service().Cancel(ctx, raw, &scene.Subject)
	var contractErr *v.ContractError
	if !errors.As(err, &contractErr) || contractErr.Code != "forbidden" {
		t.Fatal("expired proof authorized a fresh cancel key", err)
	}
	ref := scene.DecisionRef
	expired := time.Now().UTC().Add(-time.Second)
	if err := w.Source().SeedControlAccess(ctx, decision.ControlAccess{Subject: scene.Subject, DecisionOwner: v.OwnerRef{TenantID: ref.TenantID, OwnerID: ref.OwnerID}, DecisionRef: &ref, Purposes: []string{}, ValidUntil: expired}); err != nil {
		t.Fatal(err)
	}
	raw, err = v.Encode(request)
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.Service().Cancel(ctx, raw, &scene.Subject)
	if !errors.As(err, &contractErr) || contractErr.Code != "forbidden" {
		t.Fatal("revoked current access disclosed original receipt", err)
	}
}
