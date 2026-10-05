//go:build integration

package component_test

import (
	"context"
	adapter "github.com/ruipengliu/lerna/adapters/content/decision"
	fixture "github.com/ruipengliu/lerna/conformance/internal/decisionfixture"
	c "github.com/ruipengliu/lerna/contract/v1_2"
	compiler "github.com/ruipengliu/lerna/domain/task/context"
	"slices"
	"strconv"
	"testing"
	"time"
)

func TestContentContextStaticModeMaximumReachable62Materials(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// The independent static policy and complete-read bound are fixed at Install.
	trace := fixture.NewContextClaimDiagnostics(t, ctx)
	w := fixture.NewContextWorldWithInputDiagnostic(t, ctx, func(in *compiler.Input) {
		in.Policy.Capacity = "524288"
		in.Policy.OutputReserve = "131072"
		in.Policy.ReadBudget = "262144" // all processed sources are selected
		in.Request.Payload.Limits.MaxInputBytes = "262144"
		in.Request.Payload.Limits.MaxOutputBytes = "131072"
		in.Materials = in.Materials[:1]
		in.Policy.SelectionVersion = ""
	}, trace)
	w.Input.Revision = "2"
	// This explicit static declaration makes no claim that B chose materials.
	// All61 processed upstream sources are directly selected.
	primary := w.Input.Materials[0].Ref
	for i := 0; i < 60; i++ {
		ref := primary
		ref.ContentID = c.ID("static-source-" + strconv.Itoa(i))
		publishContextSource(t, ctx, w, ref, []byte("alpha\n"), []c.ContentRef{})
		w.Input.Materials = append(w.Input.Materials, compiler.Material{Ref: ref, Selected: true, Role: "additional"})
	}
	trace.Mark("phase.all-upstream-sources-published")
	if err := w.Dispatcher.InstallInput(ctx, "context-input", w.Input, "1"); err != nil {
		t.Fatal(err)
	}
	bundle, err := w.Compile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Materials) != 62 || len(bundle.Processed) != 61 {
		t.Fatal("static reachable62 direct materials projection lost", len(bundle.Materials), len(bundle.Processed))
	}
	lockStart := time.Now()
	trace.Mark("phase.independent-lock-observation-start")
	lock, err := w.Content.ReadForProcessing(ctx, &w.Subject, bundle.Lock.Ref, w.Input.Purpose, 262144)
	trace.Stage(ctx, "phase.independent-lock-observation-end", lockStart, err)
	if err != nil || len(lock.Sources) != 64 {
		t.Fatal("maximum reachable static graph's true closure differs", err, len(lock.Sources))
	}
	for _, ref := range []c.ContentRef{bundle.Mandatory.Ref, bundle.Shell.Ref, bundle.Manifest.Ref} {
		if !slices.Contains(lock.Sources, ref) {
			t.Fatal("intermediate exact ancestor missing")
		}
	}
	for _, material := range w.Input.Materials {
		if !slices.Contains(lock.Sources, material.Ref) {
			t.Fatal("processed exact ancestor missing")
		}
	}
	done := completeContextDecision(t, ctx, w, bundle)
	if len(done.Proposal.ProcessedSourceRefs) != 63 {
		t.Fatal("worker must actually process manifest plus62 direct materials")
	}
	manifest, err := adapter.ToRule(bundle.Manifest.Ref)
	if err != nil || done.Proposal.ProcessedSourceRefs[0] != manifest {
		t.Fatal("fixed manifest worker projection differs", err)
	}
	for i, material := range bundle.Materials {
		exact, err := adapter.ToRule(material)
		if err != nil || done.Proposal.ProcessedSourceRefs[i+1] != exact {
			t.Fatal("full actual worker material projection differs", err)
		}
	}
	assertContextActualByteMeasurements(t, ctx, w, bundle, done)
}
