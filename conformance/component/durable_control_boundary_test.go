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

func TestDurableControlProofBindsPrincipalIssuerAndOriginalIdentity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.NewWorld(t, ctx)
	scene := w.Scenario()
	base := sceneControl(t, ctx, w, scene, "2", "valid-proof-counterpart")
	until, err := time.Parse("2006-01-02T15:04:05.000000Z", string(base.AcceptBefore))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"subject", "delegation", "decision", "task", "digest", "issuer", "proof_owner", "proof_hash", "proof_length", "revision", "proof_expiry"} {
		t.Run(name, func(t *testing.T) {
			encoded, err := v.Encode(base)
			if err != nil {
				t.Fatal(err)
			}
			altered, err := v.Decode[v.DecisionCancelRequest](encoded)
			if err != nil {
				t.Fatal(err)
			}
			principal := scene.Subject
			principal.DelegationChain = append([]v.DelegatedSubject{}, principal.DelegationChain...)
			altered.CommandID = v.ID("invalid-proof-" + name)
			switch name {
			case "subject":
				principal.SubjectID = "different-trusted-principal"
			case "delegation":
				principal.DelegationChain = append(principal.DelegationChain, v.DelegatedSubject{TenantID: principal.TenantID, SubjectID: "other-delegator"})
			case "decision":
				altered.Target.ID = "other-proof-target"
				altered.Payload.DecisionRef = altered.Target
			case "task":
				altered.Payload.TaskRef.ID = "other-task"
			case "digest":
				altered.Payload.DecisionInputDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			case "issuer":
				altered.Payload.ControlBasis.IssuerOwner.OwnerID = "other-issuer"
			case "proof_owner":
				altered.Payload.ControlBasis.ProofRef.Owner.OwnerID = "other-proof-owner"
			case "proof_hash":
				altered.Payload.ControlBasis.ProofRef.Hash = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			case "proof_length":
				altered.Payload.ControlBasis.ProofRef.ByteLength = "0"
			case "revision":
				altered.Payload.ControlBasis.ControlRevision = "3"
			case "proof_expiry":
				altered.Payload.ControlBasis.ValidUntil = v.Time(until.Add(-time.Second).Format("2006-01-02T15:04:05.000000Z"))
			}
			ref := altered.Target
			if err := w.Source().SeedControlAccess(ctx, decision.ControlAccess{Subject: principal, DecisionOwner: v.OwnerRef{TenantID: ref.TenantID, OwnerID: ref.OwnerID}, DecisionRef: &ref, Purposes: []string{"cancel", "get"}, ValidUntil: until}); err != nil {
				t.Fatal(err)
			}
			// Current trusted principal/scope is independently valid. Only the issued
			// immutable proof fails to bind this exact new request.
			if _, err := w.Source().AuthorizeControl(ctx, principal, ref, "cancel"); err != nil {
				t.Fatal(err)
			}
			raw, err := v.Encode(altered)
			if err != nil {
				t.Fatal(err)
			}
			_, err = w.Service().Cancel(ctx, raw, &principal)
			var failure *v.ContractError
			if !errors.As(err, &failure) || (failure.Code != "forbidden" && failure.Code != "dependency_unavailable") {
				t.Fatal("altered proof authorized a new stop", err)
			}
			get, err := v.DecodeGet(scene.GetJSON)
			if err != nil {
				t.Fatal(err)
			}
			get.Target = ref
			get.Payload.DecisionRef = ref
			getRaw, err := v.Encode(get)
			if err != nil {
				t.Fatal(err)
			}
			view, err := w.Service().Get(ctx, getRaw, &principal)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := view.AsResultUnavailable(); !ok {
				t.Fatal("invalid proof wrote a closed binding")
			}
		})
	}
	receipt := controlReceipt(t, ctx, w.Service(), base, scene.Subject)
	if _, ok := receipt.AsApplied(); !ok {
		t.Fatal("valid exact proof counterpart could not stop original identity")
	}
}

func TestDurableControlReadsRealSnapshotFloorAndForbidsExpectedRevision(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.NewWorld(t, ctx)
	scene := w.Scenario()
	s := w.Service()
	acceptAccounting(t, ctx, s, scene)
	equal := sceneControl(t, ctx, w, scene, "1", "equal-snapshot-control")
	receipt := controlReceipt(t, ctx, s, equal, scene.Subject)
	rejected, ok := receipt.AsRejected()
	if !ok || rejected.Reason != "revision_changed" {
		t.Fatal("actual Snapshot control floor was bypassed")
	}
	before := controlView(t, ctx, s, scene)
	if _, ok := before.Decision.AsAccepted(); !ok || before.CurrentControl != nil {
		t.Fatal("rejected Snapshot-equal control closed original work")
	}
	high := sceneControl(t, ctx, w, scene, "2", "higher-snapshot-control")
	raw, err := v.Encode(high)
	if err != nil {
		t.Fatal(err)
	}
	invalid := []byte(strings.Replace(string(raw), "\"payload\":", "\"expected_revision\":\"1\",\"payload\":", 1))
	_, err = s.Cancel(ctx, invalid, &scene.Subject)
	var failure *v.ContractError
	if !errors.As(err, &failure) || failure.Code != "schema_invalid" {
		t.Fatal("cancel accepted forbidden generic expected_revision", err)
	}
	receipt = controlReceipt(t, ctx, s, high, scene.Subject)
	if _, ok := receipt.AsApplied(); !ok {
		t.Fatal("higher issuer control did not stop original Snapshot responsibility")
	}
	w.Reopen(ctx)
	view := controlView(t, ctx, w.Service(), scene)
	if _, ok := view.Decision.AsCancelled(); !ok || view.CurrentControl == nil {
		t.Fatal("reopened Snapshot-floor stop lost its fixed facts")
	}
}
