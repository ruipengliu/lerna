package sqlitecredentials_test

import (
	"context"
	"lerna/adapters/sqlitecredentials"
	"lerna/credentials"
	"path/filepath"
	"testing"
)

func TestCiphertextPersistenceAndRevisionConflict(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.db")
	ctx := context.Background()
	s, e := sqlitecredentials.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	r := credentials.Record{Ref: "opaque", Binding: credentials.Binding{Namespace: "local", Subject: "alice", Driver: "driver", Service: "service", Account: "account", Purpose: "task", Location: "local"}, Format: 1, Revision: 1, ExpiresUnix: 1900000000, KeyVersion: "version", Nonce: make([]byte, 12), Ciphertext: make([]byte, 32)}
	if e = s.Swap(ctx, 0, r); e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = sqlitecredentials.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	got, e := s.Get(ctx, "opaque")
	if e != nil || got.Revision != 1 || len(got.Ciphertext) != 32 {
		t.Fatal("ciphertext did not survive reopening", e)
	}
	if e = s.Swap(ctx, 0, r); e != credentials.Conflict {
		t.Fatal("duplicate accepted", e)
	}
	r.Revision = 2
	if e = s.Swap(ctx, 1, r); e != nil {
		t.Fatal(e)
	}
	if e = s.Swap(ctx, 1, r); e != credentials.Conflict {
		t.Fatal("stale revision accepted", e)
	}
}
