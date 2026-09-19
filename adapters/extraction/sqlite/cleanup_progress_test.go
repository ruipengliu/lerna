package sqlite_test

import (
	"context"
	sqliteextraction "lerna/adapters/extraction/sqlite"
	"lerna/extraction"
	"lerna/memory"
	"path/filepath"
	"strings"
	"testing"
)

func TestCleanupProgressReopensAndRejectsStaleOrChangedWorker(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "candidates.db")
	s, err := sqliteextraction.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	b := extraction.CleanupBinding{Namespace: "local", Subject: "alice", Consumer: "memory-cleaner", ConfigSHA256: strings.Repeat("a", 64)}
	if _, err = s.LoadCleanupProgress(ctx, b); err != memory.Missing {
		t.Fatalf("invented progress: %v", err)
	}
	if err = s.AdvanceCleanupProgress(ctx, b, 0, "candidate-b"); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = sqliteextraction.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.LoadCleanupProgress(ctx, b)
	if err != nil || got.Version != 1 || got.After != "candidate-b" {
		t.Fatalf("lost checkpoint: %+v %v", got, err)
	}
	if err = s.AdvanceCleanupProgress(ctx, b, 0, "candidate-a"); err != memory.Conflict {
		t.Fatalf("stale cursor overwrite: %v", err)
	}
	changed := b
	changed.ConfigSHA256 = strings.Repeat("b", 64)
	if _, err = s.LoadCleanupProgress(ctx, changed); err != memory.IdentityConflict {
		t.Fatalf("changed worker reused progress: %v", err)
	}
	if err = s.AdvanceCleanupProgress(ctx, changed, 1, "candidate-c"); err != memory.IdentityConflict {
		t.Fatalf("changed worker advanced progress: %v", err)
	}
	if err = s.AdvanceCleanupProgress(ctx, b, 1, ""); err != nil {
		t.Fatal(err)
	}
	got, err = s.LoadCleanupProgress(ctx, b)
	if err != nil || got.Version != 2 || got.After != "" {
		t.Fatalf("round reset: %+v %v", got, err)
	}
}
