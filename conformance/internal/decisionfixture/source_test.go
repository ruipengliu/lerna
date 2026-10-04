//go:build integration

package decisionfixture_test

import (
	"context"
	"testing"
	"time"

	fixture "github.com/ruipengliu/lerna/conformance/internal/decisionfixture"
	v "github.com/ruipengliu/lerna/contract/v1_1"
)

// The fixture seam, rather than private SQL, proves that reopening reads the
// original source bytes and that publication identity survives retries.
func TestFixtureSourceAndPublicationSurviveReopen(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	scope := fixture.NewSourceWorld(t, ctx)
	scenario := scope.Scenario()
	permission, err := scope.Source().Authorize(ctx, scenario.Subject, scenario.DecisionRef, "start", &scenario.Request.Payload)
	if err != nil {
		t.Fatal(err)
	}
	original, err := scope.Source().ReadMaterial(ctx, scenario.MaterialRef, "material", permission)
	if err != nil {
		t.Fatal(err)
	}
	if string(original) != "alpha\n" {
		t.Fatalf("fixed source bytes %q", original)
	}
	ref, err := scope.Source().Publish(ctx, "original-publication", []byte("fixture result: alpha\n"), []v.ContentRef{scenario.MaterialRef}, permission)
	if err != nil {
		t.Fatal(err)
	}
	scope.Reopen(ctx)
	repeated, err := scope.Source().Publish(ctx, "original-publication", []byte("fixture result: alpha\n"), []v.ContentRef{scenario.MaterialRef}, permission)
	if err != nil {
		t.Fatal(err)
	}
	if repeated != ref {
		t.Fatal("retry replaced original publication identity")
	}
	result, err := scope.Source().ReadPublished(ctx, ref, permission)
	if err != nil {
		t.Fatal(err)
	}
	if string(result) != "fixture result: alpha\n" {
		t.Fatalf("published bytes %q", result)
	}
}
