//go:build integration

package decisionfixture_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	fixture "github.com/ruipengliu/lerna/conformance/internal/decisionfixture"
	v "github.com/ruipengliu/lerna/contract/v1_1"
)

func TestFixtureLegacyManifestReadKeepsAbsentBillingBasis(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	world := fixture.NewSourceWorld(t, ctx)
	scene := world.Scenario()
	source := world.Source()
	permit, err := source.Authorize(ctx, scene.Subject, scene.DecisionRef, "get", nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := source.ReadSnapshot(ctx, scene.Request.Payload.SnapshotRef, permit, v.MaxBodyBytes)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Ref.ID = "legacy-snapshot"
	snapshot.ComponentRef.InstallLockRef.ID = "legacy-lock"
	hash := sha256.Sum256([]byte("fixture-rule/1"))
	snapshot.ComponentRef.ArtifactDigest = v.SchemaDigest("sha256:" + hex.EncodeToString(hash[:]))
	snapshot.UseRefs = []v.UseRef{{TenantID: scene.DecisionRef.TenantID, OwnerID: snapshot.TaskRef.OwnerID, Kind: "fixture_use", ID: "legacy-use", Revision: "1"}}
	permit.ComponentRef = snapshot.ComponentRef
	permit.UseRefs = snapshot.UseRefs
	permit.ChargeBasis = ""
	permit.RuleStartCharge = v.Amount{}
	permit.RuleVersion = ""
	ref := scene.DecisionRef
	ref.ID = "legacy-decision"
	_, err = source.Seed(ctx, fixture.Bundle{DecisionRef: ref, Permission: permit, Snapshot: snapshot, Materials: []fixture.Material{{Ref: scene.MaterialRef, Bytes: []byte("alpha\n")}}, Purposes: []string{"get", "material", "fixture.lock", "publish"}, RuleVersion: "fixture-rule/1"})
	if err != nil {
		t.Fatal(err)
	}
	permit, err = source.Authorize(ctx, scene.Subject, ref, "get", nil)
	if err != nil {
		t.Fatal(err)
	}
	original, err := source.Publish(ctx, "legacy-key", []byte("legacy original output\n"), []v.ContentRef{scene.MaterialRef}, permit)
	if err != nil {
		t.Fatal(err)
	}
	world.Reopen(ctx)
	source = world.Source()
	current, err := source.Authorize(ctx, scene.Subject, ref, "get", nil)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := source.ReadFixtureLock(ctx, snapshot.ComponentRef.InstallLockRef, current, v.MaxBodyBytes)
	if err != nil {
		t.Fatal(err)
	}
	if lock.RuleVersion != "fixture-rule/1" || lock.ChargeBasis != "" || lock.RuleStartCharge != (v.Amount{}) || current.RuleVersion != "" {
		t.Fatal("legacy read supplied a new durable-start fee")
	}
	planned, err := source.PlanPublication(ctx, "legacy-key", []byte("legacy original output\n"), []v.ContentRef{scene.MaterialRef}, current)
	if err != nil {
		t.Fatal(err)
	}
	if planned != original {
		t.Fatal("legacy recovery replaced original publication identity")
	}
	bytes, err := source.ReadPublished(ctx, original, current)
	if err != nil {
		t.Fatal(err)
	}
	if string(bytes) != "legacy original output\n" {
		t.Fatalf("legacy source readback %q", bytes)
	}
}
