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

type recoverySourceFunc func(context.Context, memory.RecoveryChallenge) (memory.RecoveryProof, error)

func (f recoverySourceFunc) ProveRecovery(ctx context.Context, c memory.RecoveryChallenge) (memory.RecoveryProof, error) {
	return f(ctx, c)
}

func restoreFixture(t *testing.T) (*sqlitememory.Store, *sqlitememory.Restore, memory.RecoveryBinding, memory.Ref) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "current.db")
	current, err := sqlitememory.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ref := memory.Ref{Namespace: "local", Collection: "personal", Key: "retained"}
	_, err = current.Commit(context.Background(), memory.Change{OperationID: "original", Subject: "operator", SemanticSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Record: memory.Revision{Ref: ref, Revision: 1, Document: []byte(`{"value":"retained"}`)}})
	if err != nil {
		t.Fatal(err)
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
	t.Cleanup(func() { current.Close() })
	restored, err := sqlitememory.OpenRestored(backup)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { restored.Close() })
	binding := recoveryBinding(t, current, 1)
	return current, restored, binding, ref
}

func recoveryBinding(t *testing.T, current *sqlitememory.Store, minimum uint64) memory.RecoveryBinding {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	scope := memory.RecoveryScope{Namespace: "local", Collection: "personal"}
	issuer, err := recoveryproof.NewIssuer(current, recoveryproof.IssuerConfig{Authority: "primary", Epoch: 1, Scope: scope, Key: private})
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := recoveryproof.NewVerifier(recoveryproof.VerifierConfig{Authority: "primary", Epoch: 1, Scope: scope, Key: public, MinPosition: minimum})
	if err != nil {
		t.Fatal(err)
	}
	return memory.RecoveryBinding{Scope: scope, Source: issuer, Verifier: verifier}
}

func TestRestoreReadRequiresLiveProofAfterSanitization(t *testing.T) {
	for _, mode := range []string{"unavailable", "replayed", "forged"} {
		t.Run(mode, func(t *testing.T) {
			_, restored, binding, ref := restoreFixture(t)
			ctx := context.Background()
			source := binding.Source
			var captured memory.RecoveryProof
			binding.Source = recoverySourceFunc(func(ctx context.Context, c memory.RecoveryChallenge) (memory.RecoveryProof, error) {
				p, e := source.ProveRecovery(ctx, c)
				captured = p
				return p, e
			})
			if _, err := restored.Reconcile(ctx, binding); err != nil {
				t.Fatal(err)
			}
			binding.Source = recoverySourceFunc(func(ctx context.Context, c memory.RecoveryChallenge) (memory.RecoveryProof, error) {
				switch mode {
				case "unavailable":
					return memory.RecoveryProof{}, memory.Unavailable
				case "replayed":
					return captured, nil
				default:
					p, e := source.ProveRecovery(ctx, c)
					if e != nil {
						return p, e
					}
					p.Signature[0] ^= 1
					return p, nil
				}
			})
			got, err := restored.ReadVerified(ctx, binding, memory.VersionRef{Ref: ref, Revision: 1})
			if err == nil || len(got.Document) != 0 {
				t.Fatalf("%s source released sanitized data: %v", mode, err)
			}
			saved, err := restored.InspectRecovery(ctx, binding.Scope)
			if err != nil || saved.State != "sanitized" || saved.AppliedPosition != 1 {
				t.Fatalf("failure changed committed progress: %+v %v", saved, err)
			}
		})
	}
}

func TestRestoreReadRechecksDeletionBeforeRelease(t *testing.T) {
	current, restored, binding, ref := restoreFixture(t)
	source := binding.Source
	calls := 0
	binding.Source = recoverySourceFunc(func(ctx context.Context, c memory.RecoveryChallenge) (memory.RecoveryProof, error) {
		calls++
		if calls == 2 {
			_, err := current.Delete(ctx, memory.Deletion{OperationID: "delete-during-read", Subject: "operator", SemanticSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Ref: ref, Expected: 1})
			if err != nil {
				return memory.RecoveryProof{}, err
			}
		}
		return source.ProveRecovery(ctx, c)
	})
	got, err := restored.ReadVerified(context.Background(), binding, memory.VersionRef{Ref: ref, Revision: 1})
	if calls != 2 || err != memory.Missing || len(got.Document) != 0 {
		t.Fatalf("deletion during read released old body: calls=%d err=%v", calls, err)
	}
	p, err := restored.Reconcile(context.Background(), binding)
	if err != nil || p.State != "sanitized" || p.AppliedPosition != 2 || p.Retained != 0 {
		t.Fatalf("subsequent deletion cleanup: %+v %v", p, err)
	}
}

func TestRestoreRejectsDifferentOriginalOperation(t *testing.T) {
	_, restored, binding, ref := restoreFixture(t)
	other, err := sqlitememory.Open(filepath.Join(t.TempDir(), "other.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	_, err = other.Commit(context.Background(), memory.Change{OperationID: "different-original", Subject: "operator", SemanticSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Record: memory.Revision{Ref: ref, Revision: 1, Document: []byte(`{"value":"retained"}`)}})
	if err != nil {
		t.Fatal(err)
	}
	binding = recoveryBinding(t, other, 1)
	if _, err = restored.Reconcile(context.Background(), binding); err != memory.IdentityConflict {
		t.Fatalf("same body with different original operation accepted: %v", err)
	}
	if _, err = restored.InspectRecovery(context.Background(), binding.Scope); err != memory.Missing {
		t.Fatalf("unverified history recorded as progress: %v", err)
	}
}

func TestRestoreReportsSourceDataMissingFromBackup(t *testing.T) {
	current, restored, binding, ref := restoreFixture(t)
	_, err := current.Commit(context.Background(), memory.Change{OperationID: "correction", Subject: "operator", Expected: 1, SemanticSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Record: memory.Revision{Ref: ref, Revision: 2, Document: []byte(`{"value":"new"}`)}})
	if err != nil {
		t.Fatal(err)
	}
	p, err := restored.Reconcile(context.Background(), binding)
	if err != nil || p.State != "sanitized" || p.Retained != 1 || p.Missing != 1 || p.AppliedPosition != 2 {
		t.Fatalf("incomplete backup reported complete: %+v %v", p, err)
	}
	if got, e := restored.ReadVerified(context.Background(), binding, memory.VersionRef{Ref: ref, Revision: 2}); e != memory.Missing || len(got.Document) != 0 {
		t.Fatalf("missing body fabricated: %v", e)
	}
	view, err := restored.ReadView(binding)
	if err != nil {
		t.Fatal(err)
	}
	if rows, e := view.Scan(context.Background(), binding.Scope.Namespace, binding.Scope.Collection); e != memory.Unavailable || len(rows) != 0 {
		t.Fatalf("incomplete backup claimed a current collection query: %v", e)
	}
	if old, e := view.Read(context.Background(), ref, 1); e != nil || string(old.Document) != `{"value":"retained"}` {
		t.Fatalf("incomplete collection lost a proven retained revision: %v", e)
	}

}
