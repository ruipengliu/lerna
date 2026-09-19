package sqlite_test

import (
	"context"
	"fmt"
	sqliteextraction "lerna/adapters/extraction/sqlite"
	"lerna/extraction"
	"lerna/memory"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnsupportedAttemptKeepsOriginalIdentityAfterReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "attempt.db")
	s, err := sqliteextraction.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	fact := extraction.CandidateState{Namespace: "local", Subject: "alice", OperationID: "unsupported-attempt", InvocationSHA256: strings.Repeat("a", 64), State: "unsupported", Reason: "insufficient_evidence"}
	if err = s.Decline(ctx, fact); err != nil {
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
	got, err := s.Inspect(ctx, "local", fact.OperationID)
	if err != nil || got != fact {
		t.Fatalf("unsupported fact after reopen: %+v %v", got, err)
	}
	if _, err = s.Lookup(ctx, "local", fact.OperationID); err != memory.Missing {
		t.Fatalf("unsupported attempt contains a candidate: %v", err)
	}
	candidate := processRecord(t)
	candidate.OperationID = fact.OperationID
	candidate.InvocationSHA256 = fact.InvocationSHA256
	if err = s.Commit(ctx, candidate); err != memory.ReplayUnavailable {
		t.Fatalf("unsupported attempt later committed a candidate: %v", err)
	}
	if err = s.Decline(ctx, fact); err != nil {
		t.Fatal(err)
	}
	changed := fact
	changed.Reason = "unsupported"
	if err = s.Decline(ctx, changed); err != memory.IdentityConflict {
		t.Fatalf("changed original reason: %v", err)
	}
	changed = fact
	changed.InvocationSHA256 = strings.Repeat("b", 64)
	if err = s.Decline(ctx, changed); err != memory.IdentityConflict {
		t.Fatalf("changed original invocation: %v", err)
	}
	changed = fact
	changed.OperationID = "other"
	changed.Reason = "source body or model instructions"
	if err = s.Decline(ctx, changed); err != memory.Invalid {
		t.Fatalf("unbounded reason: %v", err)
	}
	if err = s.Retire(ctx, "local", "alice", fact.OperationID); err != nil {
		t.Fatal(err)
	}
	after, err := s.Inspect(ctx, "local", fact.OperationID)
	if err != nil || after != fact {
		t.Fatalf("retirement invented or changed effect: %+v %v", after, err)
	}
}

func TestCandidateAndUnsupportedFactCannotBothCommit(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "race.db")
	a, err := sqliteextraction.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := sqliteextraction.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	for i := 0; i < 8; i++ {
		candidate := processRecord(t)
		candidate.OperationID = fmt.Sprintf("race-%d", i)
		candidate.InvocationSHA256 = strings.Repeat("a", 64)
		fact := extraction.CandidateState{Namespace: "local", Subject: "alice", OperationID: candidate.OperationID, InvocationSHA256: candidate.InvocationSHA256, State: "unsupported", Reason: "unsupported"}
		start := make(chan struct{})
		commits, declines := make(chan error, 1), make(chan error, 1)
		go func() { <-start; commits <- a.Commit(ctx, candidate) }()
		go func() { <-start; declines <- b.Decline(ctx, fact) }()
		close(start)
		commit, decline := <-commits, <-declines
		if !(commit == nil && decline == memory.IdentityConflict || decline == nil && commit == memory.ReplayUnavailable) {
			t.Fatalf("conflicting outcomes: %v / %v", commit, decline)
		}
		got, err := a.Inspect(ctx, "local", candidate.OperationID)
		if err != nil {
			t.Fatal(err)
		}
		if commit == nil && (!got.Committed || got.State != "retained" || got.Reason != "") || decline == nil && got != fact {
			t.Fatalf("contradictory stored fact: %+v", got)
		}
	}
}
