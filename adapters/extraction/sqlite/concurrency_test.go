package sqlite_test

import (
	"context"
	"fmt"
	sqliteextraction "lerna/adapters/extraction/sqlite"
	"lerna/memory"
	"path/filepath"
	"testing"
	"time"
)

func TestRetirementFencesCompetingCandidateCommit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "candidates.db")
	first, err := sqliteextraction.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := sqliteextraction.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	for i := 0; i < 12; i++ {
		record := processRecord(t)
		record.OperationID = fmt.Sprintf("competing-%d", i)
		start := make(chan struct{})
		commitResult := make(chan error, 1)
		retireResult := make(chan error, 1)
		go func() { <-start; commitResult <- first.Commit(ctx, record) }()
		go func() {
			<-start
			retireResult <- second.Retire(ctx, record.Namespace, record.Subject, record.OperationID)
		}()
		close(start)
		committed := <-commitResult
		retired := <-retireResult
		if retired != nil {
			t.Fatalf("retirement failed: %v", retired)
		}
		if committed != nil && committed != memory.ReplayUnavailable {
			t.Fatalf("unexpected competing commit: %v", committed)
		}
		state, e := second.Inspect(ctx, record.Namespace, record.OperationID)
		if e != nil || state.State != "retired" || state.Committed != (committed == nil) {
			t.Fatalf("wrong original effect: %+v %v, commit=%v", state, e, committed)
		}
		if _, e = first.Lookup(ctx, record.Namespace, record.OperationID); e != memory.Missing {
			t.Fatalf("retired body released: %v", e)
		}
		if e = first.Commit(ctx, record); e != memory.ReplayUnavailable {
			t.Fatalf("late body revived: %v", e)
		}
	}
}
