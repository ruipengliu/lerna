package extractioncheck

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"lerna/adapters/executionlocal"
	"lerna/adapters/extractionexecution"
	"lerna/execution"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"lerna/sdk"
)

func TestInferenceRetentionCapSurvivesAutomaticSave(t *testing.T) {
	ctx := context.Background()
	h, err := fresh(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	installObservedChoices(t, h)
	until := h.now().Add(5 * time.Minute).Unix()
	h.target, err = extractionexecution.WithRetentionDeadline(h.target, until)
	if err != nil {
		t.Fatal(err)
	}
	// Reconfiguration cannot extend an already selected deadline.
	h.target, err = extractionexecution.WithRetentionDeadline(h.target, h.now().Add(2*time.Hour).Unix())
	if err != nil {
		t.Fatal(err)
	}
	store, saver := bindProcessSave(t, h, "")
	h.client = sdk.NewCapabilityClient(executionlocal.Bind(h.exec, "local"), "local")
	r, grant, err := h.request(ctx, []byte(`{"sources":[{"kind":"choice","key":"one","revision":1},{"kind":"choice","key":"two","revision":1},{"kind":"choice","key":"three","revision":1}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.client.Invoke(ctx, r, grant); err != nil {
		t.Fatal(err)
	}
	out, err := h.exec.Run(ctx, r.OperationID)
	if err != nil || out.Result != "SUCCESS" {
		t.Fatalf("execute: %+v %v", out, err)
	}
	candidate, err := h.candidates.Lookup(ctx, "local", r.OperationID)
	if err != nil || candidate.Restrictions.RetainUntil != until {
		t.Fatalf("candidate deadline: %+v %v", candidate.Restrictions, err)
	}
	saved, err := saver.Inspect(ctx, r.OperationID)
	if err != nil || saved.State != "committed" || saved.Receipt == nil {
		t.Fatalf("save: %+v %v", saved, err)
	}
	row, err := store.Read(ctx, saved.Receipt.Ref, saved.Receipt.Revision)
	if err != nil {
		t.Fatal(err)
	}
	record := new(wire.MemoryRecord)
	if err = protojson.Unmarshal(row.Document, record); err != nil {
		t.Fatal(err)
	}
	if record.GetSpec().GetRetainUntil() != until {
		t.Fatal("automatic save widened candidate retention")
	}
	h.clock.advance(5 * time.Minute)
	observed, err := h.target.Inspect(ctx, execution.Call{Request: r})
	if err != nil || observed.Effect != "CONFIRMED" || len(observed.Output) != 0 {
		t.Fatalf("expired candidate disclosure/fact: %+v %v", observed, err)
	}
}

func TestExpiredRetentionConfigurationCannotCreateCandidateOrSaveIntent(t *testing.T) {
	ctx := context.Background()
	h, err := fresh(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	h.target, err = extractionexecution.WithRetentionDeadline(h.target, h.now().Unix())
	if err != nil {
		t.Fatal(err)
	}
	store, saver := bindProcessSave(t, h, "")
	h.client = sdk.NewCapabilityClient(executionlocal.Bind(h.exec, "local"), "local")
	r, grant, err := h.request(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.client.Invoke(ctx, r, grant); err != nil {
		t.Fatal(err)
	}
	out, err := h.exec.Run(ctx, r.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if out.Result == "SUCCESS" || out.Reference != "" {
		t.Fatalf("expired extraction succeeded: %+v", out)
	}
	if _, err = h.candidates.Inspect(ctx, "local", r.OperationID); !errors.Is(err, memory.Missing) {
		t.Fatalf("expired configuration retained candidate: %v", err)
	}
	if _, err = saver.Inspect(ctx, r.OperationID); !errors.Is(err, memory.Missing) {
		t.Fatalf("expired configuration created save intent: %v", err)
	}
	changes, err := store.ReadChanges(ctx, "local", "personal", 0, 16)
	if err != nil || len(changes) != 0 {
		t.Fatalf("expired configuration saved Memory: %v", err)
	}
}
