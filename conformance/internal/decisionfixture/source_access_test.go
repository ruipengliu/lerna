//go:build integration

package decisionfixture_test

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

func TestFixturePublicationRejectsDifferentOriginalContent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	world := fixture.NewSourceWorld(t, ctx)
	scene := world.Scenario()
	source := world.Source()
	permission, err := source.Authorize(ctx, scene.Subject, scene.DecisionRef, "start", &scene.Request.Payload)
	if err != nil {
		t.Fatal(err)
	}
	original, err := source.Publish(ctx, "fixed-key", []byte("original fixture bytes\n"), []v.ContentRef{scene.MaterialRef}, permission)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = source.Publish(ctx, "fixed-key", []byte("changed fixture bytes\n"), []v.ContentRef{scene.MaterialRef}, permission); !errors.Is(err, decision.ErrPublicationConflict) {
		t.Fatalf("different original content: %v", err)
	}
	preserved, err := source.ReadPublished(ctx, original, permission)
	if err != nil {
		t.Fatal(err)
	}
	if string(preserved) != "original fixture bytes\n" {
		t.Fatalf("conflict changed original publication: %q", preserved)
	}
}

func TestFixtureReadsRequireExactCurrentPrincipalPurposeAndReference(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	world := fixture.NewSourceWorld(t, ctx)
	scene := world.Scenario()
	source := world.Source()
	permission, err := source.Authorize(ctx, scene.Subject, scene.DecisionRef, "start", &scene.Request.Payload)
	if err != nil {
		t.Fatal(err)
	}
	normal, err := source.ReadMaterial(ctx, scene.MaterialRef, "rule.input", permission, v.MaxBodyBytes)
	if err != nil {
		t.Fatal(err)
	}
	if string(normal) != "alpha\n" {
		t.Fatalf("normal current read: %q", normal)
	}
	wrongSubject := scene.Subject
	wrongSubject.SubjectID = "other-principal"
	if _, err = source.Authorize(ctx, wrongSubject, scene.DecisionRef, "start", &scene.Request.Payload); !errors.Is(err, decision.ErrForbidden) {
		t.Fatalf("other principal: %v", err)
	}
	forged := permission
	forged.Subject = wrongSubject
	if _, err = source.ReadMaterial(ctx, scene.MaterialRef, "rule.input", forged, v.MaxBodyBytes); !errors.Is(err, decision.ErrForbidden) {
		t.Fatalf("forged current principal: %v", err)
	}
	if _, err = source.ReadMaterial(ctx, scene.MaterialRef, "ungranted.disclosure", permission, v.MaxBodyBytes); !errors.Is(err, decision.ErrForbidden) {
		t.Fatalf("ungranted purpose: %v", err)
	}
	wrongRef := scene.MaterialRef
	wrongRef.Hash = "sha256:" + strings.Repeat("0", 64)
	if _, err = source.ReadMaterial(ctx, wrongRef, "rule.input", permission, v.MaxBodyBytes); !errors.Is(err, decision.ErrForbidden) {
		t.Fatalf("changed exact hash: %v", err)
	}
	wrongRef = scene.MaterialRef
	wrongRef.Version = "2"
	if _, err = source.ReadMaterial(ctx, wrongRef, "rule.input", permission, v.MaxBodyBytes); !errors.Is(err, decision.ErrForbidden) {
		t.Fatalf("changed exact version: %v", err)
	}
	wrongOwner := scene.DecisionRef
	wrongOwner.OwnerID = "other-owner"
	if _, err = source.Authorize(ctx, scene.Subject, wrongOwner, "command.get", nil); !errors.Is(err, decision.ErrForbidden) {
		t.Fatalf("different owner read scope: %v", err)
	}
	commandIdentity := scene.DecisionRef
	commandIdentity.ID = scene.Request.CommandID
	if _, err = source.Authorize(ctx, scene.Subject, commandIdentity, "command.get", nil); err != nil {
		t.Fatalf("explicit current owner command read: %v", err)
	}
}

func TestFixturePermissionExpiryClosesReadsAndOriginalPublicationRetries(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	world := fixture.NewSourceWorld(t, ctx)
	scene := world.Scenario()
	source := world.Source()
	originalPermission, err := source.Authorize(ctx, scene.Subject, scene.DecisionRef, "start", &scene.Request.Payload)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := source.ReadSnapshot(ctx, scene.Request.Payload.SnapshotRef, originalPermission, v.MaxBodyBytes)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Ref.ID = "expiry-snapshot"
	snapshot.ComponentRef.InstallLockRef.ID = "expiry-lock"
	snapshot.UseRefs = []v.UseRef{{TenantID: scene.DecisionRef.TenantID, OwnerID: snapshot.TaskRef.OwnerID, Kind: "fixture_use", ID: "expiry-use", Revision: "1"}}
	ref := scene.DecisionRef
	ref.ID = "expiry-decision"
	until := time.Now().UTC().Add(2 * time.Second).Truncate(time.Microsecond)
	permit := originalPermission
	permit.ComponentRef = snapshot.ComponentRef
	permit.UseRefs = snapshot.UseRefs
	permit.ValidUntil = until
	_, err = source.Seed(ctx, fixture.Bundle{DecisionRef: ref, Permission: permit, Snapshot: snapshot, Materials: []fixture.Material{{Ref: scene.MaterialRef, Bytes: []byte("alpha\n")}}, Purposes: []string{"start", "material", "rule.input", "fixture.lock", "publish"}, RuleVersion: permit.RuleVersion, ChargeBasis: permit.ChargeBasis, RuleStartCharge: permit.RuleStartCharge})
	if err != nil {
		t.Fatal(err)
	}
	permit, err = source.Authorize(ctx, scene.Subject, ref, "start", nil)
	if err != nil {
		t.Fatal(err)
	}
	normal, err := source.ReadMaterial(ctx, scene.MaterialRef, "rule.input", permit, v.MaxBodyBytes)
	if err != nil {
		t.Fatal(err)
	}
	if string(normal) != "alpha\n" {
		t.Fatalf("finite permission normal read: %q", normal)
	}
	output, err := source.Publish(ctx, "expiry-key", []byte("finite permission output\n"), []v.ContentRef{scene.MaterialRef}, permit)
	if err != nil {
		t.Fatal(err)
	}
	// Wait for the exact persisted permit boundary outside every transaction.
	timer := time.NewTimer(time.Until(until))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case <-timer.C:
	}
	if _, err = source.ReadMaterial(ctx, scene.MaterialRef, "rule.input", permit, v.MaxBodyBytes); !errors.Is(err, decision.ErrForbidden) {
		t.Fatalf("expired current read: %v", err)
	}
	if _, err = source.Publish(ctx, "expiry-key", []byte("finite permission output\n"), []v.ContentRef{scene.MaterialRef}, permit); !errors.Is(err, decision.ErrForbidden) {
		t.Fatalf("expired original publication retry: %v", err)
	}
	if _, err = source.ReadPublished(ctx, output, permit); !errors.Is(err, decision.ErrForbidden) {
		t.Fatalf("expired published read: %v", err)
	}
}
