package sqlitefetch_test

import (
	"context"
	"lerna/adapters/sqlitefetch"
	"lerna/fetch"
	"lerna/tasks"
	"path/filepath"
	"strings"
	"testing"
)

func TestKnownFetchOutcomePersistsWithoutRefundOrReplacement(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "fetch.db")
	store, err := sqlitefetch.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	in := fetch.AttemptIntent{Task: tasks.Ref{Namespace: "local", TaskID: "task"}, Subject: "alice", OperationID: "original", Fingerprint: strings.Repeat("c", 64), MaxRequests: 2, TaskLimit: 2}
	if _, err = store.Begin(ctx, in); err != nil {
		t.Fatal(err)
	}
	if _, known, err := store.Outcome(ctx, "local", "original"); err != nil || known {
		t.Fatalf("pending became known: %v %v", known, err)
	}
	want := fetch.Outcome{Status: "timed_out", Requests: 1}
	if err = store.Complete(ctx, in, want); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = sqlitefetch.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got, known, err := store.Outcome(ctx, "local", "original")
	if err != nil || !known || got != want {
		t.Fatalf("known result lost: %+v %v %v", got, known, err)
	}
	if err = store.Complete(ctx, in, want); err != nil {
		t.Fatalf("result replay: %v", err)
	}
	changed := want
	changed.Requests = 2
	if err = store.Complete(ctx, in, changed); err != fetch.IdentityConflict {
		t.Fatalf("replaced known usage: %v", err)
	}
	in.OperationID = "replacement"
	if err = store.Complete(ctx, in, want); err != fetch.Missing {
		t.Fatalf("result without original attempt: %v", err)
	}
	if _, err = store.Begin(ctx, in); err != fetch.LimitExceeded {
		t.Fatalf("failed fetch refunded budget: %v", err)
	}
	budget, err := store.Budget(ctx, in.Task)
	if err != nil || budget.Charged != 2 {
		t.Fatalf("charged is not actual usage: %+v %v", budget, err)
	}
}
