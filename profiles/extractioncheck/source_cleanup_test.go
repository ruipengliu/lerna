package extractioncheck

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lerna/adapters/extractioncleanup"
	"lerna/answers"
	"lerna/extraction"
	"lerna/memory"
)

type sourceCleanupProbe struct{ Token, Input string }

func TestSourceCleanupRecoversUndeliveredFenceAfterProcessExit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	root := t.TempDir()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSourceCleanupCrashProbe$")
	cmd.Env = append(os.Environ(), "LERNA_SOURCE_CLEANUP_ROOT="+root)
	out, err := cmd.CombinedOutput()
	var exited *exec.ExitError
	if !errors.As(err, &exited) || exited.ExitCode() != 96 {
		t.Fatalf("source probe: %v %s", err, out)
	}
	raw, err := os.ReadFile(filepath.Join(root, "source-cleanup.json"))
	if err != nil {
		t.Fatal(err)
	}
	var probe sourceCleanupProbe
	if err = json.Unmarshal(raw, &probe); err != nil {
		t.Fatal(err)
	}
	h, err := open(ctx, root, probe.Token)
	if err != nil {
		t.Fatal(err)
	}
	defer h.close()
	ref, err := answers.ParseReference(probe.Input)
	if err != nil {
		t.Fatal(err)
	}
	files, err := h.blobs.List(ctx, 128)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, file := range files {
		found = found || file.Key == ref.Key
	}
	if !found {
		t.Fatal("probe did not leave actual input blob")
	}
	sink, err := extractioncleanup.NewContent(h.content)
	if err != nil {
		t.Fatal(err)
	}
	b := extraction.CleanupBinding{Namespace: "local", Subject: "operator", Consumer: "source-content", ConfigSHA256: strings.Repeat("d", 64)}
	worker, err := extraction.NewSourceCleanupWorker(h.candidates, sink, b, 1)
	if err != nil {
		t.Fatal(err)
	}
	pass, err := worker.Run(ctx)
	if err != nil || len(pass.Attempts) != 1 || !pass.Attempts[0].Complete || pass.Attempts[0].Error != "" || pass.Progress.Version != 1 || pass.Progress.After == "" {
		t.Fatalf("recovered source delivery: %+v %v", pass, err)
	}
	files, err = h.blobs.List(ctx, 128)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.Key == ref.Key {
			t.Fatal("source delivery did not remove input blob")
		}
	}
	worker, err = extraction.NewSourceCleanupWorker(h.candidates, sink, b, 1)
	if err != nil {
		t.Fatal(err)
	}
	empty, err := worker.Run(ctx)
	if err != nil || len(empty.Attempts) != 0 || empty.Progress.After != "" || empty.Progress.Version != 2 {
		t.Fatalf("recovered cursor: %+v %v", empty, err)
	}
	again, err := worker.Run(ctx)
	if err != nil || len(again.Attempts) != 1 || !again.Attempts[0].Complete {
		t.Fatalf("idempotent next pass: %+v %v", again, err)
	}
}

func TestSourceCleanupCrashProbe(t *testing.T) {
	root := os.Getenv("LERNA_SOURCE_CLEANUP_ROOT")
	if root == "" {
		t.Skip("subprocess only")
	}
	ctx := context.Background()
	h, err := open(ctx, root, "")
	if err != nil {
		t.Fatal(err)
	}
	r, _, err := h.request(ctx, []byte(`{"sources":[{"kind":"note","key":"one","revision":1}]}`+strings.Repeat(" ", 80)))
	if err != nil {
		t.Fatal(err)
	}
	if n, e := h.candidates.InvalidateSource(ctx, extraction.SourceInvalidation{Namespace: "local", Kind: "note", Key: "one", ThroughRevision: 1}); e != nil || n != 0 {
		t.Fatalf("source-only fence: %d %v", n, e)
	}
	raw, err := json.Marshal(sourceCleanupProbe{h.token, r.InputRef})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "source-cleanup.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	os.Exit(96)
}

// Fail only delivery to the real Content sink; discovery and checkpoints still
// use SQLite, and successful delivery performs actual invalidation and erasure.
type unavailableSourceSink struct {
	extraction.SourceCleanupSink
	key     string
	enabled bool
}

func (s *unavailableSourceSink) ApplySource(ctx context.Context, in extraction.SourceInvalidation) (bool, error) {
	if s.enabled && in.Key == s.key {
		return false, errors.New("private provider detail")
	}
	return s.SourceCleanupSink.ApplySource(ctx, in)
}
func TestSourceCleanupContinuesPastFailureAndRetriesNextRound(t *testing.T) {
	ctx := context.Background()
	h, err := fresh(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	r, _, err := h.request(ctx, []byte(`{"sources":[{"kind":"note","key":"one","revision":1}]}`+strings.Repeat(" ", 80)))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"one", "two"} {
		if _, err = h.candidates.InvalidateSource(ctx, extraction.SourceInvalidation{Namespace: "local", Kind: "note", Key: key, ThroughRevision: 1}); err != nil {
			t.Fatal(err)
		}
	}
	items, err := h.candidates.ListSourceFences(ctx, "local", "", 2)
	if err != nil || len(items) != 2 {
		t.Fatalf("discovery: %+v %v", items, err)
	}
	content, err := extractioncleanup.NewContent(h.content)
	if err != nil {
		t.Fatal(err)
	}
	sink := &unavailableSourceSink{SourceCleanupSink: content, key: items[0].Invalidation.Key, enabled: true}
	b := extraction.CleanupBinding{Namespace: "local", Subject: "operator", Consumer: "source-fair", ConfigSHA256: strings.Repeat("e", 64)}
	worker, err := extraction.NewSourceCleanupWorker(h.candidates, sink, b, 1)
	if err != nil {
		t.Fatal(err)
	}
	first, err := worker.Run(ctx)
	if err != nil || len(first.Attempts) != 1 || first.Attempts[0].Complete || first.Attempts[0].Error != string(memory.Unavailable) || first.Progress.After != items[0].Cursor {
		t.Fatalf("failed delivery: %+v %v", first, err)
	}
	second, err := worker.Run(ctx)
	if err != nil || len(second.Attempts) != 1 || !second.Attempts[0].Complete || second.Attempts[0].Source.Key != items[1].Invalidation.Key {
		t.Fatalf("later source starved: %+v %v", second, err)
	}
	empty, err := worker.Run(ctx)
	if err != nil || len(empty.Attempts) != 0 || empty.Progress.After != "" {
		t.Fatalf("round reset: %+v %v", empty, err)
	}
	sink.enabled = false
	worker, err = extraction.NewSourceCleanupWorker(h.candidates, sink, b, 1)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := worker.Run(ctx)
	if err != nil || len(recovered.Attempts) != 1 || !recovered.Attempts[0].Complete || recovered.Attempts[0].Source.Key != sink.key {
		t.Fatalf("retry: %+v %v", recovered, err)
	}
	ref, err := answers.ParseReference(r.InputRef)
	if err != nil {
		t.Fatal(err)
	}
	files, err := h.blobs.List(ctx, 128)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.Key == ref.Key {
			t.Fatal("source input body survived cleanup")
		}
	}
}
