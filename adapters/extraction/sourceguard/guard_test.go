package sourceguard_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	localextractionsource "lerna/adapters/extraction/localsource"
	extractionsourceguard "lerna/adapters/extraction/sourceguard"
	sqliteextraction "lerna/adapters/extraction/sqlite"
	"lerna/extraction"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type clock struct{}

func (clock) Now() (time.Time, error) { return time.Unix(1900000000, 0), nil }

func TestRestoredProviderCannotReleaseInvalidatedSourceToMemoryOrExtraction(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "note.txt")
	body := []byte("回答时，我偏好中文。")
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	ref := &wire.ContentSource{Kind: "note", Key: "one", Revision: 1}
	bounds := extraction.Restrictions{Storage: []string{"local"}, Processing: []string{"local"}, Recipients: []string{"local"}, Purposes: []string{"task"}, RetainUntil: 1900001000}
	entry := localextractionsource.Entry{Ref: ref, Path: path, SHA256: fmt.Sprintf("%x", sha256.Sum256(body)), Speaker: "alice", Method: "note", Fragment: "paragraph:1", Restrictions: bounds}
	provider, err := localextractionsource.New([]localextractionsource.Entry{entry}, clock{})
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqliteextraction.Open(filepath.Join(root, "candidates.db"))
	if err != nil {
		t.Fatal(err)
	}
	guard, err := extractionsourceguard.New(provider, store, "local")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = guard.Read(ctx, []*wire.ContentSource{ref}, "local", "task"); err != nil {
		t.Fatal(err)
	}
	if err = guard.Check(ctx, ref, "store", "task", "local", 1900000500); err != nil {
		t.Fatal(err)
	}
	if _, err = store.InvalidateSource(ctx, extraction.SourceInvalidation{Namespace: "local", Kind: "note", Key: "one", ThroughRevision: 1}); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = sqliteextraction.Open(filepath.Join(root, "candidates.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	provider, err = localextractionsource.New([]localextractionsource.Entry{entry}, clock{})
	if err != nil {
		t.Fatal(err)
	}
	guard, err = extractionsourceguard.New(provider, store, "local")
	if err != nil {
		t.Fatal(err)
	}
	if materials, _, e := guard.Read(ctx, []*wire.ContentSource{ref}, "local", "task"); e != memory.Denied || len(materials) != 0 {
		t.Fatalf("restored extraction release: %v", e)
	}
	if err = guard.Validate(ctx, []*wire.ContentSource{ref}, bounds); err != memory.Denied {
		t.Fatalf("restored retained use: %v", err)
	}
	for _, action := range []string{"store", "process", "discover", "disclose"} {
		if err = guard.Check(ctx, ref, action, "task", "local", 1900000500); err != memory.Denied {
			t.Fatalf("Memory %s restored: %v", action, err)
		}
	}
}
