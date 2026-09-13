package sqliteextraction_test

import (
	"context"
	"database/sql"
	"lerna/adapters/sqliteextraction"
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyRetirementDoesNotInventInvocationProof(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// Schema and rows from the pre-invocation store, independently constructed.
	_, err = db.Exec(`CREATE TABLE retired_candidates(namespace TEXT NOT NULL,operation TEXT NOT NULL,subject TEXT NOT NULL,committed INTEGER NOT NULL CHECK(committed IN (0,1)),PRIMARY KEY(namespace,operation));INSERT INTO retired_candidates VALUES('local','old','alice',1),('local','fenced','alice',0);`)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		s, err := sqliteextraction.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, op := range []string{"old", "fenced"} {
			got, e := s.Inspect(context.Background(), "local", op)
			if e != nil || got.InvocationSHA256 != "" || got.Subject != "alice" || got.State != "retired" || got.Committed != (op == "old") {
				s.Close()
				t.Fatalf("invented binding or lost fact: %+v %v", got, e)
			}
		}
		if err = s.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
