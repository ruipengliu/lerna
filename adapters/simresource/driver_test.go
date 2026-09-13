package simresource

import (
	"context"
	"database/sql"
	"lerna/execution"
	"path/filepath"
	"testing"
	"time"
)

type fixedClock struct{}

func (fixedClock) Now() (time.Time, error) { return time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC), nil }
func TestNegativeProofUsesOneSnapshot(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "target.db")
	reader, e := Open(path, "add", "main", fixedClock{})
	if e != nil {
		t.Fatal(e)
	}
	defer reader.Close()
	writer, e := Open(path, "add", "main", fixedClock{})
	if e != nil {
		t.Fatal(e)
	}
	defer writer.Close()
	call := execution.Call{Request: execution.Request{OperationID: "original", ResourceVersion: 1, ControlVersion: 1}, Input: []byte(`{"delta":3}`), Control: &execution.ControlQualification{Resource: reader.ref(), Authority: authority, Version: 1}}
	tx, e := reader.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback()
	var count int
	// Pin the same snapshot as an inspector's first absent-job lookup.
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM jobs`).Scan(&count); e != nil || count != 0 {
		t.Fatal(count, e)
	}
	if _, e = writer.StartAsync(ctx, call); e != nil {
		t.Fatal(e)
	}
	if e = writer.Complete(ctx, call.Request.OperationID); e != nil {
		t.Fatal(e)
	}
	if _, e = writer.ApplyResourceControl(ctx, execution.ResourceCommand{OperationID: "takeover", Resource: reader.ref(), Authority: authority, Version: 2, Intent: "TAKEOVER", ExpectedResourceVersion: 2}); e != nil {
		t.Fatal(e)
	}
	fact, e := reader.inspectSnapshot(ctx, tx, call, execution.Association{})
	if e != nil || fact.Observation.Effect != "UNKNOWN" {
		t.Fatal("mixed old absence with new fence", fact, e)
	}
	if e = tx.Rollback(); e != nil {
		t.Fatal(e)
	}
	fact, e = reader.InspectAsync(ctx, call, execution.Association{})
	if e != nil || fact.Observation.Effect != "CONFIRMED" {
		t.Fatal("lost committed effect", fact, e)
	}
	wrong := fact.Association
	wrong.Handle = "incorrect-handle"
	if _, e = reader.InspectAsync(ctx, call, wrong); e == nil {
		t.Fatal("wrong handle reported absence")
	}
}
