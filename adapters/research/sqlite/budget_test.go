package sqlite_test

import (
	"context"
	sqlitefetch "lerna/adapters/research/sqlite"
	"lerna/fetch"
	"lerna/tasks"
	"path/filepath"
	"strings"
	"testing"
)

func TestOriginalFetchReservationSurvivesReopenWithoutResettingTaskBudget(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "fetch.db")
	store, err := sqlitefetch.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	intent := fetch.AttemptIntent{Task: tasks.Ref{Namespace: "local", TaskID: "task-1"}, Subject: "alice", OperationID: "fetch-1", Fingerprint: strings.Repeat("a", 64), MaxRequests: 2, TaskLimit: 3}
	fresh, err := store.Begin(ctx, intent)
	if err != nil || !fresh {
		t.Fatalf("first reservation: %v %v", fresh, err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = sqlitefetch.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	fresh, err = store.Begin(ctx, intent)
	if err != nil || fresh {
		t.Fatalf("restart created a new attempt: %v %v", fresh, err)
	}
	got, err := store.Inspect(ctx, "local", "fetch-1")
	if err != nil || got != intent {
		t.Fatalf("original identity lost: %+v %v", got, err)
	}
	next := intent
	next.OperationID = "fetch-2"
	if _, err = store.Begin(ctx, next); err != fetch.LimitExceeded {
		t.Fatalf("task budget reset: %v", err)
	}
	outcome, known, err := store.Outcome(ctx, "local", "fetch-2")
	if err != nil || !known || outcome.Status != "limit_exceeded" || outcome.Requests != 0 || outcome.Reference != "" {
		t.Fatal("budget rejection was not retained as a finite outcome")
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = sqlitefetch.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if fresh, err = store.Begin(ctx, next); err != nil || fresh {
		t.Fatal("rejected operation restarted")
	}
	next.MaxRequests = 1
	if _, err = store.Begin(ctx, next); err != fetch.IdentityConflict {
		t.Fatal("rejected identity was rewritten")
	}
	next.OperationID = "fetch-3"
	if fresh, err = store.Begin(ctx, next); err != nil || !fresh {
		t.Fatalf("remaining budget: %v %v", fresh, err)
	}
	changed := intent
	changed.MaxRequests = 1
	if _, err = store.Begin(ctx, changed); err != fetch.IdentityConflict {
		t.Fatalf("changed old operation: %v", err)
	}
	budget, err := store.Budget(ctx, intent.Task)
	if err != nil || budget.Limit != 3 || budget.Charged != 3 {
		t.Fatalf("durable task budget: %+v %v", budget, err)
	}
}

func TestCompetingFetchReservationsCannotOverdrawTask(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "fetch.db")
	a, err := sqlitefetch.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := sqlitefetch.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	intent := fetch.AttemptIntent{Task: tasks.Ref{Namespace: "local", TaskID: "task-1"}, Subject: "alice", OperationID: "one", Fingerprint: strings.Repeat("a", 64), MaxRequests: 1, TaskLimit: 1}
	start := make(chan struct{})
	results := make(chan error, 2)
	for i, store := range []*sqlitefetch.Store{a, b} {
		go func(i int, s *sqlitefetch.Store) {
			<-start
			in := intent
			if i == 1 {
				in.OperationID = "two"
			}
			_, e := s.Begin(ctx, in)
			results <- e
		}(i, store)
	}
	close(start)
	successes, limited := 0, 0
	for i := 0; i < 2; i++ {
		switch e := <-results; e {
		case nil:
			successes++
		case fetch.LimitExceeded:
			limited++
		default:
			t.Fatal(e)
		}
	}
	budget, err := a.Budget(ctx, intent.Task)
	if err != nil || successes != 1 || limited != 1 || budget.Charged != 1 {
		t.Fatalf("overdraw: successes=%d limited=%d budget=%+v err=%v", successes, limited, budget, err)
	}
}
