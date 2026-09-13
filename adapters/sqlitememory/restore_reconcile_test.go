package sqlitememory_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"

	"lerna/adapters/recoveryproof"
	"lerna/adapters/sqlitememory"
	"lerna/memory"
)

func TestRestoreCleansDeletedBodyAndReadsRetainedData(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "current.db")
	current, err := sqlitememory.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if current != nil {
			current.Close()
		}
	}()
	scope := memory.RecoveryScope{Namespace: "local", Collection: "personal"}
	removed := memory.Ref{Namespace: scope.Namespace, Collection: scope.Collection, Key: "removed"}
	kept := memory.Ref{Namespace: scope.Namespace, Collection: scope.Collection, Key: "kept"}
	other := memory.Ref{Namespace: scope.Namespace, Collection: "other", Key: "unrelated"}
	for _, ref := range []memory.Ref{removed, kept, other} {
		_, err = current.Commit(ctx, memory.Change{OperationID: "put-" + ref.Key, Subject: "operator", SemanticSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Record: memory.Revision{Ref: ref, Revision: 1, Document: []byte(`{"preference":"synthetic"}`)}})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err = current.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(root, "backup.db")
	if err = os.WriteFile(backup, raw, 0600); err != nil {
		t.Fatal(err)
	}
	current, err = sqlitememory.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = current.Delete(ctx, memory.Deletion{OperationID: "delete", Subject: "operator", SemanticSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Ref: removed, Expected: 1})
	if err != nil {
		t.Fatal(err)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	source, err := recoveryproof.NewIssuer(current, recoveryproof.IssuerConfig{Authority: "primary", Epoch: 1, Scope: scope, Key: private})
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := recoveryproof.NewVerifier(recoveryproof.VerifierConfig{Authority: "primary", Epoch: 1, Scope: scope, Key: public, MinPosition: 3})
	if err != nil {
		t.Fatal(err)
	}
	binding := memory.RecoveryBinding{Scope: scope, Source: source, Verifier: verifier}
	restored, err := sqlitememory.OpenRestored(backup)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if restored != nil {
			restored.Close()
		}
	}()
	progress, err := restored.Reconcile(ctx, binding)
	if err != nil {
		t.Fatal(err)
	}
	if progress.State != "sanitized" || progress.OriginPosition != 2 || progress.VerifiedPosition != 2 || progress.AppliedPosition != 3 || progress.Retained != 1 || progress.Missing != 0 {
		t.Fatalf("restore cleanup: %+v", progress)
	}
	if _, err = restored.ReadVerified(ctx, binding, memory.VersionRef{Ref: removed, Revision: 1}); err != memory.Missing {
		t.Fatalf("deleted backup body released: %v", err)
	}
	got, err := restored.ReadVerified(ctx, binding, memory.VersionRef{Ref: kept, Revision: 1})
	if err != nil || string(got.Document) != `{"preference":"synthetic"}` {
		t.Fatalf("valid data was discarded or blocked: %v", err)
	}
	if err = restored.Close(); err != nil {
		t.Fatal(err)
	}
	if ordinary, e := sqlitememory.Open(backup); ordinary != nil || e != memory.Quarantined {
		if ordinary != nil {
			ordinary.Close()
		}
		t.Fatal("data restoration activated an ordinary runtime")
	}
	restored, err = sqlitememory.OpenRestored(backup)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := restored.InspectRecovery(ctx, scope)
	if err != nil || saved != progress {
		t.Fatalf("cleanup state lost on reopen: %+v %v", saved, err)
	}
	if _, err = restored.ReadVerified(ctx, binding, memory.VersionRef{Ref: kept, Revision: 1}); err != nil {
		t.Fatal(err)
	}
	otherScope := memory.RecoveryScope{Namespace: other.Namespace, Collection: other.Collection}
	otherSource, err := recoveryproof.NewIssuer(current, recoveryproof.IssuerConfig{Authority: "primary", Epoch: 1, Scope: otherScope, Key: private})
	if err != nil {
		t.Fatal(err)
	}
	otherVerifier, err := recoveryproof.NewVerifier(recoveryproof.VerifierConfig{Authority: "primary", Epoch: 1, Scope: otherScope, Key: public, MinPosition: 1})
	if err != nil {
		t.Fatal(err)
	}
	otherBinding := memory.RecoveryBinding{Scope: otherScope, Source: otherSource, Verifier: otherVerifier}
	if body, e := restored.ReadVerified(ctx, otherBinding, memory.VersionRef{Ref: other, Revision: 1}); e != nil || string(body.Document) != `{"preference":"synthetic"}` {
		t.Fatalf("cleanup destroyed an unrelated collection: %v", e)
	}
}
