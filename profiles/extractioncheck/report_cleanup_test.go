package extractioncheck

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	sqlitecontext "lerna/adapters/context/sqlite"
	memorycleanup "lerna/adapters/memory/cleanup"
	sqlitememory "lerna/adapters/memory/sqlite"
	"lerna/artifacts"
	"lerna/contextassembly"
	"lerna/extraction"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"testing"
	"time"
)

func cleanPublishedReport(t *testing.T, h *harness, store *sqlitememory.Store, service *memory.Service, snapshots *sqlitecontext.Store, content *artifacts.Service, key contextassembly.Key, output *wire.ContentRef, spec *wire.ContentSpec) {
	t.Helper()
	ctx := context.Background()
	if _, err := snapshots.Read(ctx, key); err != nil {
		t.Fatalf("no retained context before cleanup: %v", err)
	}
	if _, err := h.blobs.Read(ctx, output.Key, 0, uint32(spec.Size), spec.Size, spec.Sha256); err != nil {
		t.Fatalf("no actual report bytes before cleanup: %v", err)
	}
	contexts, err := memorycleanup.NewContexts(snapshots)
	if err != nil {
		t.Fatal(err)
	}
	artifactsSink, err := memorycleanup.NewArtifacts(content)
	if err != nil {
		t.Fatal(err)
	}
	var configs []memory.ConsumerConfig
	var targets []memory.CleanupTarget
	for _, name := range []string{"report-contexts", "report-artifacts"} {
		scope, err := json.Marshal(struct {
			Root, Consumer string
			Version        int
		}{h.root, name, 1})
		if err != nil {
			t.Fatal(err)
		}
		binding := memory.ConsumerBinding{Namespace: "local", Collection: "personal", Consumer: name, ConfigSHA256: fmt.Sprintf("%x", sha256.Sum256(scope))}
		configs = append(configs, memory.ConsumerConfig{Binding: binding, Batch: 16, Timeout: 5 * time.Second})
		targets = append(targets, memory.CleanupTarget{Name: name, Dimension: memory.DerivedCleanup, Binding: &binding})
	}
	reporter, err := memory.NewDeletionReporter(store, store, targets)
	if err != nil {
		t.Fatal(err)
	}
	service, err = service.WithDeletionReporter(reporter)
	if err != nil {
		t.Fatal(err)
	}
	b := memory.Binding{Token: h.token, Namespace: "local", Subject: "operator", Location: "local", Recipient: "local"}
	cleaner, err := extraction.NewCleaner(h.candidates, service, b, "task", h.operation)
	if err != nil {
		t.Fatal(err)
	}
	retired, err := h.candidates.ListRetiredSaves(ctx, "local", "operator", "", 16)
	if err != nil || len(retired) != 1 {
		t.Fatalf("retired report source: %v", err)
	}
	deleted, err := cleaner.Clean(ctx, retired[0])
	if err != nil || deleted.State != "committed" || deleted.Report == nil {
		t.Fatalf("Memory cleanup: %v", err)
	}
	if _, err = store.Read(ctx, deleted.Report.Event.Ref, 1); err != memory.Missing {
		t.Fatalf("deleted Memory body remains available: %v", err)
	}
	if len(deleted.Report.Derived) != 2 {
		t.Fatal("missing declared consumers")
	}
	for _, target := range deleted.Report.Derived {
		if target.State != "pending" {
			t.Fatal("consumer cleanup reported before applying deletion")
		}
	}
	sinks := []memory.SourceSink{contexts, artifactsSink}
	for i, sink := range sinks {
		consumer, err := memory.NewSourceConsumer(store, store, sink, configs[i])
		if err != nil {
			t.Fatal(err)
		}
		pass, err := consumer.Run(ctx)
		if err != nil || pass.Pending || pass.Position != deleted.Report.Event.Position {
			t.Fatalf("consumer %s: %+v %v", configs[i].Binding.Consumer, pass, err)
		}
		// Replacement consumer recovers the durable position, with no new work.
		consumer, err = memory.NewSourceConsumer(store, store, sink, configs[i])
		if err != nil {
			t.Fatal(err)
		}
		replay, err := consumer.Run(ctx)
		if err != nil || replay.Position != pass.Position || replay.Applied != 0 || replay.Pending {
			t.Fatalf("consumer replay: %+v %v", replay, err)
		}
	}
	if _, err = snapshots.Read(ctx, key); err != contextassembly.Invalidated {
		t.Fatalf("context snapshot not retired: %v", err)
	}
	files, err := h.blobs.List(ctx, 128)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.Key == output.Key {
			t.Fatal("cleaned report blob remains on disk")
		}
	}
	after, err := cleaner.Clean(ctx, retired[0])
	if err != nil || after.Report == nil || after.Report.Event != deleted.Report.Event {
		t.Fatalf("cleanup reconciliation changed original deletion: %v", err)
	}
	for _, target := range after.Report.Derived {
		if target.State != "applied" || target.Position < after.Report.Event.Position {
			t.Fatal("missing durable consumer acknowledgment")
		}
	}
	for _, target := range after.Report.Replicas {
		if target.State != "not_covered" {
			t.Fatal("unconnected replicas claimed clean")
		}
	}
}
