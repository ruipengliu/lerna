package sqlite_test

import (
	"context"
	sqliteextraction "lerna/adapters/extraction/sqlite"
	"lerna/extraction"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCandidateCommitReopensUnderOriginalOperation(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "candidates.db")
	s, err := sqliteextraction.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	fragment := "paragraph:1"
	in := extraction.CandidateRecord{Namespace: "local", Subject: "alice", OperationID: "original-extraction", Candidate: extraction.Candidate{About: "alice", Kind: "preference", Attribute: "format", Value: "concise", Conditions: "answer", Sources: []*wire.MemorySource{{Ref: &wire.ContentSource{Kind: "note", Key: "one", Revision: 1}, Method: "authenticated-note", Fragment: &fragment}}, Confidence: &wire.MemoryConfidence{Assessment: "explicit", Basis: "本人明确陈述", Method: "local-rules-v1"}}, Restrictions: extraction.Restrictions{Storage: []string{"device-a"}, Processing: []string{"device-a"}, Recipients: []string{"device-a"}, Purposes: []string{"assist"}, RetainUntil: 1900001000}}
	if err = s.Commit(ctx, in); err != nil {
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
	got, err := s.Lookup(ctx, "local", "original-extraction")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("lost original candidate: %+v", got)
	}
	if err = s.Commit(ctx, in); err != nil {
		t.Fatal(err)
	}
	changed := in
	changed.Candidate.Value = "detailed"
	if err = s.Commit(ctx, changed); err != memory.IdentityConflict {
		t.Fatalf("changed intent: %v", err)
	}
	got, err = s.Lookup(ctx, "local", "original-extraction")
	if err != nil || got.Candidate.Value != "concise" {
		t.Fatalf("original replaced: %+v %v", got, err)
	}
	if _, err = s.Lookup(ctx, "other", "original-extraction"); err != memory.Missing {
		t.Fatalf("namespace: %v", err)
	}
	if err = s.Retire(ctx, "local", "alice", "original-extraction"); err != nil {
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
	if _, err = s.Lookup(ctx, "local", "original-extraction"); err != memory.Missing {
		t.Fatalf("retired body: %v", err)
	}
	state, err := s.Inspect(ctx, "local", "original-extraction")
	if err != nil || state.State != "retired" || state.Subject != "alice" || !state.Committed {
		t.Fatalf("original fact: %+v %v", state, err)
	}
	if err = s.Commit(ctx, in); err != memory.ReplayUnavailable {
		t.Fatalf("resurrected candidate: %v", err)
	}
	if err = s.Retire(ctx, "local", "alice", "original-extraction"); err != nil {
		t.Fatal(err)
	}
	if err = s.Retire(ctx, "local", "alice", "late-extraction"); err != nil {
		t.Fatal(err)
	}
	in.OperationID = "late-extraction"
	if err = s.Commit(ctx, in); err != memory.ReplayUnavailable {
		t.Fatalf("late commit: %v", err)
	}
	state, err = s.Inspect(ctx, "local", "late-extraction")
	if err != nil || state.State != "retired" || state.Committed {
		t.Fatalf("uncommitted fence claimed effect: %+v %v", state, err)
	}

}
