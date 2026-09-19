package extractioncheck

import (
	"context"
	"strings"
	"testing"
	"time"

	executionlocal "lerna/adapters/execution/local"
	extractionexecution "lerna/adapters/extraction/execution"
	"lerna/extraction"
	"lerna/memory"
	"lerna/sdk"
)

func TestExpiredCandidateBodiesEnterOriginalMemoryCleanup(t *testing.T) {
	ctx := context.Background()
	h, err := fresh(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	h.target, err = extractionexecution.WithRetentionDeadline(h.target, h.now().Add(time.Minute).Unix())
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
	if out, e := h.exec.Run(ctx, r.OperationID); e != nil || out.Result != "SUCCESS" {
		t.Fatalf("save task: %+v %v", out, e)
	}
	original, err := h.candidates.Lookup(ctx, "local", r.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := saver.Inspect(ctx, r.OperationID)
	if err != nil || saved.Receipt == nil {
		t.Fatalf("saved receipt: %+v %v", saved, err)
	}
	expiry, err := extraction.NewExpiryWorker(h.candidates, h.clock, "local", "operator", 1)
	if err != nil {
		t.Fatal(err)
	}
	if count, e := expiry.Run(ctx); e != nil || count != 0 {
		t.Fatalf("retired before deadline: %d %v", count, e)
	}
	h.clock.advance(time.Minute)
	if count, e := expiry.Run(ctx); e != nil || count != 1 {
		t.Fatalf("expired retirement: %d %v", count, e)
	}
	if _, err = h.candidates.Lookup(ctx, "local", r.OperationID); err != memory.Missing {
		t.Fatalf("candidate body remains: %v", err)
	}
	state, err := h.candidates.Inspect(ctx, "local", r.OperationID)
	if err != nil || state.State != "retired" || !state.Committed || state.InvocationSHA256 != r.Fingerprint() {
		t.Fatalf("lost original fact: %+v %v", state, err)
	}
	intent, err := h.candidates.LookupSave(ctx, "local", "operator", r.OperationID)
	if err != nil || intent.State != "retired" || intent.Request.Spec != nil {
		t.Fatalf("save intent body remains: %+v %v", intent, err)
	}
	if err = h.candidates.Commit(ctx, original); err != memory.ReplayUnavailable {
		t.Fatalf("expired operation revived: %v", err)
	}
	cleaner := bindProcessCleanup(t, h, store, "", h.candidates)
	worker, err := extraction.NewCleanupWorker(h.candidates, cleaner, extraction.CleanupBinding{Namespace: "local", Subject: "operator", Consumer: "expiry-cleaner", ConfigSHA256: strings.Repeat("b", 64)}, 1)
	if err != nil {
		t.Fatal(err)
	}
	pass, err := worker.Run(ctx)
	if err != nil || len(pass.Attempts) != 1 || pass.Attempts[0].Error != "" || pass.Attempts[0].Result.State != "committed" {
		t.Fatalf("expired Memory cleanup: %+v %v", pass, err)
	}
	if _, err = store.Read(ctx, saved.Receipt.Ref, saved.Receipt.Revision); err != memory.Missing {
		t.Fatalf("expired Memory body remains: %v", err)
	}
	if count, e := expiry.Run(ctx); e != nil || count != 0 {
		t.Fatalf("duplicate expiry: %d %v", count, e)
	}
	replay, err := cleaner.Clean(ctx, r.OperationID)
	if err != nil || replay.State != "committed" || replay.Report == nil || replay.Report.Event.Position != pass.Attempts[0].Result.Report.Event.Position {
		t.Fatalf("cleanup replaced original deletion: %+v %v", replay, err)
	}
}
