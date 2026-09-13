package sqliteextraction_test

import (
	"context"
	"lerna/adapters/sqliteextraction"
	"lerna/memory"
	"path/filepath"
	"strings"
	"testing"
)

func TestOriginalInvocationSurvivesCandidateRetirement(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "candidate.db")
	s, err := sqliteextraction.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	r := processRecord(t)
	r.InvocationSHA256 = strings.Repeat("a", 64)
	if err = s.Commit(ctx, r); err != nil {
		t.Fatal(err)
	}
	altered := r
	altered.InvocationSHA256 = strings.Repeat("b", 64)
	if err = s.Commit(ctx, altered); err != memory.IdentityConflict {
		t.Fatalf("replaced invocation: %v", err)
	}
	if err = s.Retire(ctx, r.Namespace, r.Subject, r.OperationID); err != nil {
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
	state, err := s.Inspect(ctx, r.Namespace, r.OperationID)
	if err != nil || state.InvocationSHA256 != strings.Repeat("a", 64) || !state.Committed || state.State != "retired" {
		t.Fatalf("lost invocation: %+v %v", state, err)
	}
}
