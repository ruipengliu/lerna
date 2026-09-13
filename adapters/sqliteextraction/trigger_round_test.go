package sqliteextraction_test

import (
	"context"
	"lerna/adapters/sqliteextraction"
	"lerna/extraction"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"lerna/tasks"
	"path/filepath"
	"testing"
)

func TestTriggerRoundsKeepOriginalSubmissionAndPersistBudget(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "candidate.db")
	s, err := sqliteextraction.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	spec := extraction.TriggerSpec{ID: "scan", Condition: "source.changed", Sources: []extraction.SourceScope{{Kind: "note", Key: "one"}}, MaxRounds: 2, MaxSteps: 2, ExpiresUnix: 1900001000}
	if err = s.RegisterTrigger(ctx, extraction.TriggerRecord{Namespace: "local", Subject: "alice", Location: "local", Purpose: "task", State: "active", Spec: spec}); err != nil {
		t.Fatal(err)
	}
	r := extraction.TriggerRound{Event: &wire.ContentSource{Kind: "note", Key: "one", Revision: 1}, Submission: tasks.Submission{Namespace: "local", OperationID: "original-submit", Goal: "Extract authorized source", InputRefs: []string{"controlled:input-1"}, Constraints: tasks.Constraints{MaxSteps: 2, DeadlineUnix: 1900000500}}}
	if err = s.ReserveTriggerRound(ctx, "local", "alice", "scan", 1900000000, r); err != nil {
		t.Fatal(err)
	}
	if err = s.ReserveTriggerRound(ctx, "local", "alice", "scan", 1900000000, r); err != nil {
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
	got, err := s.GetTriggerRound(ctx, "local", "alice", "scan", r.Event)
	if err != nil || got.Submission.OperationID != "original-submit" || got.Submission.InputRefs[0] != "controlled:input-1" {
		t.Fatalf("lost original task: %+v %v", got, err)
	}
	changed := r
	changed.Submission.OperationID = "replacement"
	if err = s.ReserveTriggerRound(ctx, "local", "alice", "scan", 1900000000, changed); err != memory.IdentityConflict {
		t.Fatalf("replacement task: %v", err)
	}
	r.Event = &wire.ContentSource{Kind: "note", Key: "one", Revision: 2}
	r.Submission.OperationID = "second-submit"
	if err = s.ReserveTriggerRound(ctx, "local", "alice", "scan", 1900000000, r); err != nil {
		t.Fatal(err)
	}
	r.Event = &wire.ContentSource{Kind: "note", Key: "one", Revision: 3}
	r.Submission.OperationID = "third-submit"
	if err = s.ReserveTriggerRound(ctx, "local", "alice", "scan", 1900000000, r); err != memory.Capacity {
		t.Fatalf("round budget reset: %v", err)
	}
	if err = s.CancelTrigger(ctx, "local", "alice", "scan"); err != nil {
		t.Fatal(err)
	}
	if err = s.ReserveTriggerRound(ctx, "local", "alice", "scan", 1900000000, r); err != memory.ReplayUnavailable {
		t.Fatalf("cancelled scan admitted work: %v", err)
	}
}

func TestConcurrentTriggerEventsCannotOverdrawFinalRound(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "candidate.db")
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
	if err = a.RegisterTrigger(ctx, extraction.TriggerRecord{Namespace: "local", Subject: "alice", Location: "local", Purpose: "task", State: "active", Spec: extraction.TriggerSpec{ID: "scan", Condition: "source.changed", Sources: []extraction.SourceScope{{Kind: "note", Key: "one"}}, MaxRounds: 1, MaxSteps: 1, ExpiresUnix: 1900001000}}); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for i, store := range []*sqliteextraction.Store{a, b} {
		round := extraction.TriggerRound{Event: &wire.ContentSource{Kind: "note", Key: "one", Revision: uint64(i + 1)}, Submission: tasks.Submission{Namespace: "local", OperationID: []string{"first", "second"}[i], Goal: "Extract authorized source", InputRefs: []string{"controlled:input"}, Constraints: tasks.Constraints{MaxSteps: 1, DeadlineUnix: 1900000500}}}
		go func() {
			<-start
			results <- store.ReserveTriggerRound(ctx, "local", "alice", "scan", 1900000000, round)
		}()
	}
	close(start)
	accepted, exhausted := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			accepted++
		} else if err == memory.Capacity {
			exhausted++
		} else {
			t.Fatal(err)
		}
	}
	if accepted != 1 || exhausted != 1 {
		t.Fatalf("final round overspent: accepted=%d exhausted=%d", accepted, exhausted)
	}
}
