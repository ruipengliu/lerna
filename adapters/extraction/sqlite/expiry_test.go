package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"

	sqliteextraction "lerna/adapters/extraction/sqlite"
	"lerna/memory"
)

func TestExpiryIsBoundedScopedAndDurableAcrossCompetingStores(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "candidates.db")
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
	for _, item := range []struct {
		op, ns, subject string
		until           int64
	}{
		{"oldest", "local", "alice", 100},
		{"due", "local", "alice", 200},
		{"future", "local", "alice", 201},
		{"other-owner", "local", "bob", 100},
		{"other-namespace", "other", "alice", 100},
	} {
		r := processRecord(t)
		r.OperationID, r.Namespace, r.Subject = item.op, item.ns, item.subject
		r.Candidate.About = item.subject
		r.Restrictions.RetainUntil = item.until
		if err = a.Commit(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	if n, e := a.RetireExpired(ctx, "local", "alice", 200, 1); e != nil || n != 1 {
		t.Fatalf("bounded pass: %d %v", n, e)
	}
	if _, err = a.Lookup(ctx, "local", "oldest"); err != memory.Missing {
		t.Fatalf("oldest not retired: %v", err)
	}
	if _, err = a.Lookup(ctx, "local", "due"); err != nil {
		t.Fatalf("batch exceeded: %v", err)
	}
	type result struct {
		n   int
		err error
	}
	start := make(chan struct{})
	done := make(chan result, 2)
	for _, store := range []*sqliteextraction.Store{a, b} {
		go func(s *sqliteextraction.Store) {
			<-start
			n, e := s.RetireExpired(ctx, "local", "alice", 200, 1)
			done <- result{n, e}
		}(store)
	}
	close(start)
	total := 0
	for range 2 {
		out := <-done
		if out.err != nil {
			t.Fatal(out.err)
		}
		total += out.n
	}
	if total != 1 {
		t.Fatalf("competing expiry double counted: %d", total)
	}
	if err = b.Close(); err != nil {
		t.Fatal(err)
	}
	b, err = sqliteextraction.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if n, e := b.RetireExpired(ctx, "local", "alice", 200, 16); e != nil || n != 0 {
		t.Fatalf("reopen repeated retirement: %d %v", n, e)
	}
	for _, op := range []string{"oldest", "due"} {
		state, e := b.Inspect(ctx, "local", op)
		if e != nil || state.State != "retired" || !state.Committed {
			t.Fatalf("reopened fact: %+v %v", state, e)
		}
	}
	for _, item := range []struct{ ns, op string }{{"local", "future"}, {"local", "other-owner"}, {"other", "other-namespace"}} {
		if _, err = b.Lookup(ctx, item.ns, item.op); err != nil {
			t.Fatalf("retired outside cutoff/scope: %s %v", item.op, err)
		}
	}
}
