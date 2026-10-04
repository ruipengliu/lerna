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

func TestFixturePublicationPlanNamesExactOutputWithoutPublishing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	world := fixture.NewSourceWorld(t, ctx)
	scene := world.Scenario()
	permit, err := world.Source().Authorize(ctx, scene.Subject, scene.DecisionRef, "start", &scene.Request.Payload)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("planned fixture output\n")
	sources := []v.ContentRef{scene.MaterialRef}
	planned, err := world.Source().PlanPublication(ctx, "prepared-key", body, sources, permit)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := world.Source().ReadPublished(ctx, planned, permit); !errors.Is(err, decision.ErrForbidden) {
		t.Fatalf("planning created readable publication: %v", err)
	}
	world.Reopen(ctx)
	repeated, err := world.Source().PlanPublication(ctx, "prepared-key", body, sources, permit)
	if err != nil {
		t.Fatal(err)
	}
	if repeated != planned {
		t.Fatal("reopening changed exact publication plan")
	}
	published, err := world.Source().Publish(ctx, "prepared-key", body, sources, permit)
	if err != nil {
		t.Fatal(err)
	}
	if published != planned {
		t.Fatal("actual publication differs from exact plan")
	}
	read, err := world.Source().ReadPublished(ctx, published, permit)
	if err != nil {
		t.Fatal(err)
	}
	if string(read) != "planned fixture output\n" {
		t.Fatalf("planned output readback %q", read)
	}
	if _, err := world.Source().PlanPublication(ctx, "prepared-key", []byte("changed output\n"), sources, permit); !errors.Is(err, decision.ErrPublicationConflict) {
		t.Fatalf("planning against different original publication: %v", err)
	}
}
