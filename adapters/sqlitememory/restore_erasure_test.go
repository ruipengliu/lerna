package sqlitememory_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"lerna/adapters/recoveryproof"
	"lerna/adapters/sqlitememory"
	"lerna/memory"
	"os"
	"path/filepath"
	"testing"
)

func TestRestoreExactErasureKeepsIndependentRevisionWithoutHeadFallback(t *testing.T) {
	for _, latestErased := range []bool{false, true} {
		for _, backupAfter := range []bool{false, true} {
			name := "historical"
			if latestErased {
				name = "current"
			}
			if backupAfter {
				name += "-already-erased"
			}
			t.Run(name, func(t *testing.T) {
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
				ref := memory.Ref{Namespace: "local", Collection: "personal", Key: "corrected"}
				keys := []string{"restricted", "independent"}
				erased, kept := uint64(1), uint64(2)
				if latestErased {
					keys = []string{"independent", "restricted"}
					erased, kept = 2, 1
				}
				for i, key := range keys {
					if _, err = current.Commit(ctx, sourceRevision(ref, uint64(i+1), key)); err != nil {
						t.Fatal(err)
					}
				}
				in := memory.SourceErasure{Namespace: "local", Kind: "note", Key: "restricted", ThroughRevision: 1}
				if backupAfter {
					if _, err = current.EraseSource(ctx, in); err != nil {
						t.Fatal(err)
					}
				}
				if err = current.Close(); err != nil {
					t.Fatal(err)
				}
				current = nil
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
				if _, err = current.EraseSource(ctx, in); err != nil {
					t.Fatal(err)
				}
				public, private, err := ed25519.GenerateKey(rand.Reader)
				if err != nil {
					t.Fatal(err)
				}
				scope := memory.RecoveryScope{Namespace: "local", Collection: "personal"}
				issuer, err := recoveryproof.NewIssuer(current, recoveryproof.IssuerConfig{Authority: "current", Epoch: 1, Scope: scope, Key: private})
				if err != nil {
					t.Fatal(err)
				}
				verifier, err := recoveryproof.NewVerifier(recoveryproof.VerifierConfig{Authority: "current", Epoch: 1, Scope: scope, Key: public})
				if err != nil {
					t.Fatal(err)
				}
				binding := memory.RecoveryBinding{Scope: scope, Source: issuer, Verifier: verifier}
				restored, err := sqlitememory.OpenRestored(backup)
				if err != nil {
					t.Fatal(err)
				}
				progress, err := restored.Reconcile(ctx, binding)
				if err != nil || progress.State != "sanitized" || progress.Retained != 1 || progress.Missing != 0 || progress.AppliedPosition != 3 {
					t.Fatalf("exact recovery: %+v %v", progress, err)
				}
				if _, e := restored.ReadVerified(ctx, binding, memory.VersionRef{Ref: ref, Revision: erased}); e != memory.Missing {
					t.Fatalf("erased body restored: %v", e)
				}
				if _, e := restored.ReadVerified(ctx, binding, memory.VersionRef{Ref: ref, Revision: kept}); e != nil {
					t.Fatalf("independent body lost: %v", e)
				}
				view, err := restored.ReadView(binding)
				if err != nil {
					t.Fatal(err)
				}
				rows, err := view.Scan(ctx, "local", "personal")
				if err != nil || latestErased && len(rows) != 0 || !latestErased && (len(rows) != 1 || rows[0].Revision != 2) {
					t.Fatalf("restored head fallback: %+v %v", rows, err)
				}
				if _, e := view.Commit(ctx, sourceRevision(ref, 3, "independent")); e != memory.Quarantined {
					t.Fatalf("restore granted mutation: %v", e)
				}
				if err = restored.Close(); err != nil {
					t.Fatal(err)
				}
				restored, err = sqlitememory.OpenRestored(backup)
				if err != nil {
					t.Fatal(err)
				}
				defer restored.Close()
				if p, e := restored.Reconcile(ctx, binding); e != nil || p.State != "sanitized" || p.AppliedPosition != 3 {
					t.Fatalf("reopened recovery: %+v %v", p, e)
				}
			})
		}
	}
}

func TestRestoreRejectsSourceCutoffRollbackAtUnchangedCollectionPosition(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "current.db")
	current, err := sqlitememory.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ref := memory.Ref{Namespace: "local", Collection: "personal", Key: "stable"}
	if _, err = current.Commit(ctx, sourceRevision(ref, 1, "independent")); err != nil {
		t.Fatal(err)
	}
	fence := memory.SourceErasure{Namespace: "local", Kind: "note", Key: "unused-source", ThroughRevision: 1}
	if n, e := current.EraseSource(ctx, fence); e != nil || n != 0 {
		t.Fatalf("source-only fence: %d %v", n, e)
	}
	if err = current.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	stalePath := filepath.Join(root, "stale.db")
	if err = os.WriteFile(stalePath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	current, err = sqlitememory.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	fence.ThroughRevision = 2
	if _, err = current.EraseSource(ctx, fence); err != nil {
		t.Fatal(err)
	}
	if err = current.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(root, "backup.db")
	if err = os.WriteFile(backupPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	current, err = sqlitememory.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer current.Close()
	stale, err := sqlitememory.Open(stalePath)
	if err != nil {
		t.Fatal(err)
	}
	defer stale.Close()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	scope := memory.RecoveryScope{Namespace: "local", Collection: "personal"}
	config := recoveryproof.IssuerConfig{Authority: "current", Epoch: 1, Scope: scope, Key: private}
	issuer, err := recoveryproof.NewIssuer(current, config)
	if err != nil {
		t.Fatal(err)
	}
	oldIssuer, err := recoveryproof.NewIssuer(stale, config)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := recoveryproof.NewVerifier(recoveryproof.VerifierConfig{Authority: "current", Epoch: 1, Scope: scope, Key: public})
	if err != nil {
		t.Fatal(err)
	}
	restored, err := sqlitememory.OpenRestored(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	binding := memory.RecoveryBinding{Scope: scope, Source: issuer, Verifier: verifier}
	if p, e := restored.Reconcile(ctx, binding); e != nil || p.State != "sanitized" || p.AppliedPosition != 1 {
		t.Fatalf("current fence recovery: %+v %v", p, e)
	}
	binding.Source = oldIssuer
	if _, e := restored.Reconcile(ctx, binding); e != memory.IdentityConflict {
		t.Fatalf("validly signed older cutoff accepted: %v", e)
	}
	if _, e := restored.ReadVerified(ctx, binding, memory.VersionRef{Ref: ref, Revision: 1}); e != memory.IdentityConflict {
		t.Fatalf("stale cutoff permitted restored read: %v", e)
	}
}
