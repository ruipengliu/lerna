package sqlite_test

import (
	"context"
	sqlitememory "lerna/adapters/memory/sqlite"
	"lerna/memory"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeletionStatusRequiresConfirmedConsumerProgress(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	s, err := sqlitememory.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	ref := memory.Ref{Namespace: "local", Collection: "personal", Key: "source"}
	hash := strings.Repeat("a", 64)
	if _, err = s.Commit(ctx, memory.Change{OperationID: "put", Subject: "alice", SemanticSHA256: hash, Record: memory.Revision{Ref: ref, Revision: 1, Document: []byte(`{"text":"private"}`)}}); err != nil {
		t.Fatal(err)
	}
	receipt, err := s.Delete(ctx, memory.Deletion{OperationID: "delete", Subject: "alice", SemanticSHA256: hash, Ref: ref, Expected: 1})
	if err != nil {
		t.Fatal(err)
	}
	binding := memory.ConsumerBinding{Namespace: "local", Collection: "personal", Consumer: "contexts", ConfigSHA256: hash}
	reporter, err := memory.NewDeletionReporter(s, s, []memory.CleanupTarget{{Name: "contexts", Dimension: memory.DerivedCleanup, Binding: &binding}})
	if err != nil {
		t.Fatal(err)
	}
	report, err := reporter.Status(ctx, memory.SourceEvent{Ref: ref, Revision: receipt.Revision, Position: receipt.Position, Kind: memory.SourceDeleted})
	if err != nil || report.Authority != "committed" || report.Derived[0].State != "pending" || report.Local[0].State != "not_covered" || report.Replicas[0].State != "not_covered" {
		t.Fatalf("unconfirmed coverage: %+v %v", report, err)
	}
	// A status read must not register a consumer or claim cleanup work happened.
	if _, err = s.InspectConsumer(ctx, binding); err != memory.Missing {
		t.Fatalf("status registered consumer: %v", err)
	}
	if _, err = s.BindConsumer(ctx, binding); err != nil {
		t.Fatal(err)
	}
	if err = s.AckEvent(ctx, binding, 0, 1); err != nil {
		t.Fatal(err)
	}
	report, err = reporter.Status(ctx, memory.SourceEvent{Ref: ref, Revision: 2, Position: 2, Kind: memory.SourceDeleted})
	if err != nil || report.Derived[0].State != "pending" || report.Derived[0].Position != 1 {
		t.Fatalf("creation confirmation reported deletion applied: %+v %v", report, err)
	}
	if err = s.AckEvent(ctx, binding, 1, 2); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = sqlitememory.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	reporter, err = memory.NewDeletionReporter(s, s, []memory.CleanupTarget{{Name: "contexts", Dimension: memory.DerivedCleanup, Binding: &binding}})
	if err != nil {
		t.Fatal(err)
	}
	report, err = reporter.Status(ctx, memory.SourceEvent{Ref: ref, Revision: 2, Position: 2, Kind: memory.SourceDeleted})
	if err != nil || report.Derived[0].State != "applied" || report.Derived[0].Position != 2 || report.Replicas[0].State != "not_covered" {
		t.Fatalf("durable scope: %+v %v", report, err)
	}
	if _, err = reporter.Status(ctx, memory.SourceEvent{Ref: ref, Revision: 3, Position: 3, Kind: memory.SourceDeleted}); err != memory.Conflict {
		t.Fatalf("uncommitted deletion reported: %v", err)
	}
	// A changed consumer configuration cannot borrow the old completion cursor.
	altered := binding
	altered.ConfigSHA256 = strings.Repeat("b", 64)
	changed, err := memory.NewDeletionReporter(s, s, []memory.CleanupTarget{{Name: "contexts", Dimension: memory.DerivedCleanup, Binding: &altered}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = changed.Status(ctx, memory.SourceEvent{Ref: ref, Revision: 2, Position: 2, Kind: memory.SourceDeleted}); err != memory.IdentityConflict {
		t.Fatalf("new sink borrowed old completion: %v", err)
	}
	// Same position with a different source is not deletion evidence.
	other := ref
	other.Key = "other"
	if _, err = reporter.Status(ctx, memory.SourceEvent{Ref: other, Revision: 2, Position: 2, Kind: memory.SourceDeleted}); err != memory.Conflict {
		t.Fatalf("mismatched deletion source: %v", err)
	}

}
