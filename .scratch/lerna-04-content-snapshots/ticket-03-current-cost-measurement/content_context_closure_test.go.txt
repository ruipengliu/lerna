//go:build integration

package component_test

import (
	"context"
	"encoding/base64"
	fixture "github.com/ruipengliu/lerna/conformance/internal/decisionfixture"
	c "github.com/ruipengliu/lerna/contract/v1_2"
	compiler "github.com/ruipengliu/lerna/domain/task/context"
	"slices"
	"strconv"
	"testing"
	"time"
)

func publishContextSource(t *testing.T, ctx context.Context, w *fixture.ContextWorld, ref c.ContentRef, body []byte, sources []c.ContentRef) {
	t.Helper()
	if err := w.Dispatcher.PrepareContent(ctx, w.Input, ref); err != nil {
		t.Fatal(err)
	}
	raw, err := c.Encode(c.ContentPutRequest{ContractVersion: c.Version, Profile: "content", CommandID: c.ID("source-" + string(ref.ContentID) + "-" + ref.Version), Target: c.ContentTarget{TenantID: ref.Owner.TenantID, OwnerID: ref.Owner.OwnerID, Kind: "content", ID: ref.ContentID}, Method: "content.put", AcceptBefore: c.Time(w.Input.Request.AcceptBefore), Payload: c.ContentPutPayload{ContentRef: ref, Sources: sources, Purpose: c.Purpose(w.Input.Purpose), RetainUntil: c.Time(w.Input.RetainUntil), BytesBase64: base64.StdEncoding.EncodeToString(body)}})
	if err != nil {
		t.Fatal(err)
	}
	response, err := w.Content.Put(ctx, raw, &w.Subject)
	if err != nil {
		t.Fatal(err)
	}
	received, ok := response.AsReceived()
	if !ok {
		t.Fatal("source transport unavailable")
	}
	if _, ok = received.Receipt.AsAccepted(); !ok {
		t.Fatal("source not accepted", received.Receipt)
	}
	if done, err := w.Content.Step(ctx); err != nil || !done {
		t.Fatal("real source publication failed", done, err)
	}
	read, err := w.Content.ReadForProcessing(ctx, &w.Subject, ref, w.Input.Purpose, int64(len(body)))
	if err != nil || string(read.Bytes) != string(body) {
		t.Fatal("source bytes not published exactly", err)
	}
}

func TestContentContextReachableClosure64And65IncludesIntermediateVersions(t *testing.T) {
	for _, ancestors := range []int{64, 65} {
		t.Run(strconv.Itoa(ancestors), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			trace := fixture.NewContextClaimDiagnostics(t, ctx)
			// Fix the independent finite closure-isolation tuple before Install.
			w := fixture.NewContextWorldWithInputDiagnostic(t, ctx, func(in *compiler.Input) {
				in.Policy.Capacity = "524288"
				in.Policy.OutputReserve = "131072"
				in.Policy.ReadBudget = "262160" // original MaxInput plus omitted B16
				in.Request.Payload.Limits.MaxInputBytes = "262144"
				in.Request.Payload.Limits.MaxOutputBytes = "131072"
			}, trace)
			w.Input.Revision = "2"
			primary := w.Input.Materials[0].Ref
			var first c.ContentRef
			// M, shell and manifest add three actual intermediate ancestors to
			// lock/artifact. A+B plus59 extras are61 unique upstream versions.
			for i := 0; i < ancestors-5; i++ {
				ref := primary
				ref.ContentID = c.ID("closure-source-" + strconv.Itoa(i))
				sources := []c.ContentRef{primary}
				if i == 1 {
					ref.ContentID = first.ContentID
					ref.Version = "2"
				}
				if i > 0 {
					sources = append(sources, first)
				}
				publishContextSource(t, ctx, w, ref, []byte("alpha\n"), sources)
				if i == 0 {
					first = ref
				}
				w.Input.Materials = append(w.Input.Materials, compiler.Material{Ref: ref, Selected: true, Role: "additional"})
			}
			trace.Mark("phase.all-upstream-sources-published")
			if err := w.Dispatcher.InstallInput(ctx, "context-input", w.Input, "1"); err != nil {
				t.Fatal(err)
			}
			if ancestors == 65 {
				compileOverflowObservedThroughReopen(t, ctx, w)
				return
			}
			bundle, err := w.Compile(ctx)
			if err != nil {
				t.Fatal(err)
			}
			lockStart := time.Now()
			trace.Mark("phase.independent-lock-observation-start")
			lock, err := w.Content.ReadForProcessing(ctx, &w.Subject, bundle.Lock.Ref, w.Input.Purpose, 262144)
			trace.Stage(ctx, "phase.independent-lock-observation-end", lockStart, err)
			if err != nil || len(lock.Sources) != 64 || !slices.Contains(lock.Sources, bundle.Mandatory.Ref) || !slices.Contains(lock.Sources, bundle.Shell.Ref) || !slices.Contains(lock.Sources, bundle.Manifest.Ref) {
				t.Fatal("actual full closure lost intermediate versions", err, len(lock.Sources))
			}
			for _, m := range w.Input.Materials {
				if !slices.Contains(lock.Sources, m.Ref) {
					t.Fatal("actual closure omitted exact processed version", m.Ref)
				}
			}
			if len(bundle.Materials) != 61 {
				t.Fatal("this assembly reachable normal quantity must be reported exactly", len(bundle.Materials))
			}
			completeContextDecision(t, ctx, w, bundle)
		})
	}
}
