package sqlitefetch_test

import (
	"context"
	"errors"
	"lerna/adapters/sqlitefetch"
	"lerna/fetch"
	"lerna/tasks"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func crashIntent() fetch.AttemptIntent {
	return fetch.AttemptIntent{Task: tasks.Ref{Namespace: "local", TaskID: "original-task"}, Subject: "alice", OperationID: "original-fetch", Fingerprint: strings.Repeat("b", 64), MaxRequests: 2, TaskLimit: 2}
}
func TestFetchReservationSurvivesProcessExitBeforeResult(t *testing.T) {
	for _, phase := range []string{"reserved", "completed"} {
		t.Run(phase, func(t *testing.T) { checkFetchCrash(t, phase) })
	}
}
func checkFetchCrash(t *testing.T, phase string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "fetch.db")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.CommandContext(ctx, executable, "-test.run=^TestFetchReservationCrashProbe$")
	child.Env = append(os.Environ(), "HARNESS_FETCH_BUDGET_CRASH="+path, "HARNESS_FETCH_BUDGET_RESULT="+phase)
	output, err := child.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 75 {
		t.Fatalf("child: %v %s", err, output)
	}
	store, err := sqlitefetch.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	intent := crashIntent()
	fresh, err := store.Begin(ctx, intent)
	if err != nil || fresh {
		t.Fatalf("crash authorized redispatch: %v %v", fresh, err)
	}
	changed := intent
	changed.OperationID = "replacement"
	if _, err = store.Begin(ctx, changed); err != fetch.LimitExceeded {
		t.Fatalf("unknown budget refunded: %v", err)
	}
	budget, err := store.Budget(ctx, intent.Task)
	if err != nil || budget.Charged != 2 || budget.Limit != 2 {
		t.Fatalf("lost budget: %+v %v", budget, err)
	}
	result, known, err := store.Outcome(ctx, "local", "original-fetch")
	if err != nil || known != (phase == "completed") {
		t.Fatalf("crash result state: %+v %v %v", result, known, err)
	}
	if known && result != (fetch.Outcome{Status: "timed_out", Requests: 1}) {
		t.Fatalf("changed result after crash: %+v", result)
	}
}
func TestFetchReservationCrashProbe(t *testing.T) {
	path := os.Getenv("HARNESS_FETCH_BUDGET_CRASH")
	if path == "" {
		t.Skip("subprocess probe")
	}
	store, err := sqlitefetch.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := store.Begin(context.Background(), crashIntent())
	if err != nil || !fresh {
		t.Fatalf("reserve: %v %v", fresh, err)
	}
	if os.Getenv("HARNESS_FETCH_BUDGET_RESULT") == "completed" {
		if err = store.Complete(context.Background(), crashIntent(), fetch.Outcome{Status: "timed_out", Requests: 1}); err != nil {
			t.Fatal(err)
		}
	}
	os.Exit(75)
}
