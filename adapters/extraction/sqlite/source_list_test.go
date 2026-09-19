package sqlite_test

import (
	"context"
	sqliteextraction "lerna/adapters/extraction/sqlite"
	"lerna/extraction"
	"path/filepath"
	"testing"
)

func TestSourceFenceDiscoveryKeepsCutoffsAndPaginationAcrossReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "candidates.db")
	s, err := sqliteextraction.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range []extraction.SourceInvalidation{{Namespace: "local", Kind: "note", Key: "one", ThroughRevision: 1}, {Namespace: "local", Kind: "note", Key: "two", ThroughRevision: 2}, {Namespace: "other", Kind: "note", Key: "one", ThroughRevision: 3}} {
		if n, e := s.InvalidateSource(ctx, in); e != nil || n != 0 {
			t.Fatalf("source-only fence: %d %v", n, e)
		}
	}
	first, err := s.ListSourceFences(ctx, "local", "", 1)
	if err != nil || len(first) != 1 {
		t.Fatalf("first bounded page: %+v %v", first, err)
	}
	updated := first[0].Invalidation
	updated.ThroughRevision = 9
	if _, err = s.InvalidateSource(ctx, updated); err != nil {
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
	next, err := s.ListSourceFences(ctx, "local", first[0].Cursor, 1)
	if err != nil || len(next) != 1 || next[0].Invalidation.Key == first[0].Invalidation.Key {
		t.Fatalf("cursor lost another source: %+v %v", next, err)
	}
	end, err := s.ListSourceFences(ctx, "local", next[0].Cursor, 1)
	if err != nil || len(end) != 0 {
		t.Fatalf("namespace/page end: %+v %v", end, err)
	}
	revisited, err := s.ListSourceFences(ctx, "local", "", 16)
	if err != nil || len(revisited) != 2 {
		t.Fatalf("next pass: %+v %v", revisited, err)
	}
	if revisited[0].Cursor != first[0].Cursor || revisited[0].Invalidation.ThroughRevision != 9 {
		t.Fatal("updated cutoff behind cursor was not revisited")
	}
}
