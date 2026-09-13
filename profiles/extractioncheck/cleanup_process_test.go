package extractioncheck

import (
	"context"
	"encoding/json"
	"errors"
	"lerna/adapters/memoryauth"
	"lerna/adapters/sqlitememory"
	"lerna/extraction"
	"lerna/memory"
	"lerna/schema"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func bindProcessCleanup(t *testing.T, h *harness, store *sqlitememory.Store, mode string, journals ...extraction.CleanupJournal) *extraction.Cleaner {
	t.Helper()
	d := extraction.SavedSchema()
	schemas, err := schema.New([]schema.Resource{d})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := memoryauth.New(h.auth, h.source, []memoryauth.Collection{{Namespace: "local", Name: "personal", Resource: "root", PolicyRef: "local-extracted", Purpose: "task", Storage: []string{"local"}, Processing: []string{"local"}, Recipients: []string{"local"}, Schemas: map[string]string{d.Type: schema.Digest(d.Document)}}})
	if err != nil {
		t.Fatal(err)
	}
	var backend memory.Store = store
	if mode == "before-delete" || mode == "after-delete" {
		backend = cleanupProcessStore{Store: store, mode: mode}
	}
	service, err := memory.New(backend, policy, schemas, h.clock, memory.Config{Location: "local", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	reporter, err := memory.NewDeletionReporter(store, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	service, err = service.WithDeletionReporter(reporter)
	if err != nil {
		t.Fatal(err)
	}
	var journal extraction.CleanupJournal = h.candidates
	if len(journals) > 1 {
		t.Fatal("at most one cleanup journal override")
	}
	if len(journals) == 1 {
		journal = journals[0]
	}
	cleaner, err := extraction.NewCleaner(journal, service, memory.Binding{Token: h.token, Namespace: "local", Subject: "operator", Location: "local", Recipient: "local"}, "task", h.operation)
	if err != nil {
		t.Fatal(err)
	}
	return cleaner
}

func TestCleanupRecoversOriginalDeletionAfterProcessExit(t *testing.T) {
	for _, mode := range []string{"before-delete", "after-delete", "after-confirmation"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			root := t.TempDir()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCleanupCrashProbe$")
			cmd.Env = append(os.Environ(), "LERNA_CLEANUP_ROOT="+root, "LERNA_CLEANUP_MODE="+mode)
			out, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 93 {
				t.Fatalf("child: %v %s", err, out)
			}
			raw, err := os.ReadFile(filepath.Join(root, "cleanup-probe.json"))
			if err != nil {
				t.Fatal(err)
			}
			var config saveProbeConfig
			if err = json.Unmarshal(raw, &config); err != nil {
				t.Fatal(err)
			}
			h, err := open(ctx, root, config.Token)
			if err != nil {
				t.Fatal(err)
			}
			defer h.close()
			store, _ := bindProcessSave(t, h, "")
			cleaner := bindProcessCleanup(t, h, store, "")
			request, err := h.candidates.LookupCleanup(ctx, "local", "operator", config.Request.OperationID)
			if err != nil {
				t.Fatal(err)
			}
			result, err := cleaner.Clean(ctx, config.Request.OperationID)
			if err != nil {
				t.Fatal(err)
			}
			want := "committed"
			count := 2
			if mode == "before-delete" {
				want = "unknown"
				count = 1
			}
			if result.State != want {
				t.Fatalf("deletion: %s want %s", result.State, want)
			}
			if want == "unknown" && result.Report != nil || want == "committed" && result.Report == nil {
				t.Fatal("wrong deletion report")
			}
			ref := memory.Ref{Namespace: request.Ref.Namespace, Collection: request.Ref.Collection, Key: request.Ref.Key}
			_, err = store.Read(ctx, ref, 1)
			if mode == "before-delete" && err != nil || mode != "before-delete" && err != memory.Missing {
				t.Fatalf("actual body state: %v", err)
			}
			replay, err := cleaner.Clean(ctx, config.Request.OperationID)
			if err != nil || replay.State != want {
				t.Fatalf("replay: %s %v", replay.State, err)
			}
			after, err := h.candidates.LookupCleanup(ctx, "local", "operator", config.Request.OperationID)
			if err != nil || after.OperationID != request.OperationID {
				t.Fatalf("replacement deletion: %v", err)
			}
			changes, err := store.ReadChanges(ctx, "local", "personal", 0, 16)
			if err != nil || len(changes) != count {
				t.Fatalf("actual effect count: %d %v", len(changes), err)
			}
			if want == "committed" && (changes[1].OperationID != request.OperationID || result.Report.Event != replay.Report.Event) {
				t.Fatal("original deletion changed")
			}
		})
	}
}

func TestCleanupCrashProbe(t *testing.T) {
	root := os.Getenv("LERNA_CLEANUP_ROOT")
	if root == "" {
		t.Skip("child only")
	}
	mode := os.Getenv("LERNA_CLEANUP_MODE")
	ctx := context.Background()
	h, err := open(ctx, root, "")
	if err != nil {
		t.Fatal(err)
	}
	r, grant, err := h.request(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(saveProbeConfig{Token: h.token, Request: r})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "cleanup-probe.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	store, _ := bindProcessSave(t, h, "")
	if _, err = h.client.Invoke(ctx, r, grant); err != nil {
		t.Fatal(err)
	}
	if _, err = h.exec.Run(ctx, r.OperationID); err != nil {
		t.Fatal(err)
	}
	if err = h.exec.Drain(ctx, 16); err != nil {
		t.Fatal(err)
	}
	if _, err = h.candidates.InvalidateSource(ctx, extraction.SourceInvalidation{Namespace: "local", Kind: "note", Key: "one", ThroughRevision: 1}); err != nil {
		t.Fatal(err)
	}
	cleaner := bindProcessCleanup(t, h, store, mode)
	result, err := cleaner.Clean(ctx, r.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if mode != "after-confirmation" || result.State != "committed" {
		t.Fatal("did not reach cleanup crash boundary")
	}
	os.Exit(93)
}

type cleanupProcessStore struct {
	*sqlitememory.Store
	mode string
}

func (s cleanupProcessStore) Delete(ctx context.Context, in memory.Deletion) (memory.Receipt, error) {
	if s.mode == "before-delete" {
		os.Exit(93)
	}
	r, err := s.Store.Delete(ctx, in)
	if err != nil {
		return r, err
	}
	os.Exit(93)
	return r, nil
}
