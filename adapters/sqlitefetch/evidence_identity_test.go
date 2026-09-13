package sqlitefetch_test

import (
	"context"
	"lerna/adapters/sqlitefetch"
	"lerna/fetch"
	"path/filepath"
	"testing"
)

func TestOriginalEvidenceOperationSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "fetch.db")
	store, err := sqlitefetch.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	in := crashIntent()
	in.EvidenceOperation = "original-content-operation"
	fresh, err := store.Begin(ctx, in)
	if err != nil || !fresh {
		t.Fatalf("begin: %v", err)
	}
	store.Close()
	store, err = sqlitefetch.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got, err := store.Inspect(ctx, in.Task.Namespace, in.OperationID)
	if err != nil || got != in {
		t.Fatalf("original evidence identity lost: %v", err)
	}
	changed := in
	changed.EvidenceOperation = "replacement-content-operation"
	if _, err = store.Begin(ctx, changed); err != fetch.IdentityConflict {
		t.Fatalf("evidence identity replaced: %v", err)
	}
}
