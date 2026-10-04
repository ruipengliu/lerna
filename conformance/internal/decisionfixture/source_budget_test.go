//go:build integration

package decisionfixture_test

import (
	"context"
	"errors"
	"testing"
	"time"

	decision "github.com/ruipengliu/lerna/components/decision_engine"
	fixture "github.com/ruipengliu/lerna/conformance/internal/decisionfixture"
	v "github.com/ruipengliu/lerna/contract/v1_1"
)

func TestFixtureSourceEnforcesRemainingInputBeforeReturningBody(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	world := fixture.NewSourceWorld(t, ctx)
	scene := world.Scenario()
	source := world.Source()
	permission, err := source.Authorize(ctx, scene.Subject, scene.DecisionRef, "start", &scene.Request.Payload)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := source.ReadSnapshot(ctx, scene.Request.Payload.SnapshotRef, permission, v.MaxBodyBytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Raw) == 0 {
		t.Fatal("normal source omitted original Snapshot bytes")
	}
	if small, err := source.ReadSnapshot(ctx, scene.Request.Payload.SnapshotRef, permission, int64(len(snapshot.Raw)-1)); !errors.Is(err, decision.ErrInputLimit) || len(small.Raw) != 0 {
		t.Fatalf("Snapshot byte limit: returned %d bytes, error %v", len(small.Raw), err)
	}
	lock, err := source.ReadFixtureLock(ctx, scene.Request.Payload.ComponentRef.InstallLockRef, permission, v.MaxBodyBytes)
	if err != nil {
		t.Fatal(err)
	}
	if lock.RuleVersion != "fixture-rule/2" || lock.ChargeBasis != "durable_rule_start" || lock.RuleStartCharge != (v.Amount{Unit: "fixture", IntegerValue: "1"}) {
		t.Fatal("durable manifest omitted the exact rule-start billing basis")
	}
	total := int64(len(lock.Raw) + len(lock.ManifestRaw))
	if total == 0 {
		t.Fatal("normal fixture lock omitted actual source bytes")
	}
	if _, err := source.ReadFixtureLock(ctx, scene.Request.Payload.ComponentRef.InstallLockRef, permission, total); err != nil {
		t.Fatalf("exact lock plus manifest budget: %v", err)
	}
	if small, err := source.ReadFixtureLock(ctx, scene.Request.Payload.ComponentRef.InstallLockRef, permission, total-1); !errors.Is(err, decision.ErrInputLimit) || int64(len(small.Raw)+len(small.ManifestRaw)) > total-1 {
		t.Fatalf("combined lock/manifest bound: %v", err)
	}
	if small, err := source.ReadMaterial(ctx, scene.MaterialRef, "rule.input", permission, 5); !errors.Is(err, decision.ErrInputLimit) || len(small) != 0 {
		t.Fatalf("known six-byte material exceeded five-byte budget: %v", err)
	}
	bytes, err := source.ReadMaterial(ctx, scene.MaterialRef, "rule.input", permission, 6)
	if err != nil {
		t.Fatal(err)
	}
	if string(bytes) != "alpha\n" {
		t.Fatalf("exact material budget: %q", bytes)
	}
	forged := permission
	forged.Subject.SubjectID = "wrong-current-principal"
	if _, err := source.ReadSnapshot(ctx, scene.Request.Payload.SnapshotRef, forged, 0); !errors.Is(err, decision.ErrForbidden) {
		t.Fatalf("current authorization precedes byte limit: %v", err)
	}
}
