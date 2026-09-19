package recoveryproof_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"lerna/adapters/memory/recoveryproof"
	sqlitememory "lerna/adapters/memory/sqlite"
	"lerna/memory"
)

func TestCurrentAuthorityProofRejectsBackupReplayAndRollback(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "current.db")
	store, err := sqlitememory.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if store != nil {
			store.Close()
		}
	}()
	ref := memory.Ref{Namespace: "local", Collection: "personal", Key: "preference"}
	_, err = store.Commit(ctx, memory.Change{OperationID: "put", Subject: "operator", SemanticSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("put"))), Record: memory.Revision{Ref: ref, Revision: 1, Document: []byte(`{"preference":"synthetic"}`)}})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(root, "old.db")
	if err = os.WriteFile(backupPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	store, err = sqlitememory.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	// The verifier's pin and the live authority's signing key are held outside
	// the restored Memory database. A proof cannot select its own trust anchor.
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	scope := memory.RecoveryScope{Namespace: "local", Collection: "personal"}
	issuer, err := recoveryproof.NewIssuer(store, recoveryproof.IssuerConfig{Authority: "local-authority", Epoch: 1, Scope: scope, Key: private})
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := recoveryproof.NewVerifier(recoveryproof.VerifierConfig{Authority: "local-authority", Epoch: 1, Scope: scope, Key: public})
	if err != nil {
		t.Fatal(err)
	}
	first := memory.RecoveryChallenge{Scope: scope, Nonce: [32]byte{1}}
	old, err := issuer.ProveRecovery(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	if err = verifier.VerifyRecovery(first, old); err != nil {
		t.Fatal(err)
	}
	_, err = store.Delete(ctx, memory.Deletion{OperationID: "delete", Subject: "operator", SemanticSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("delete"))), Ref: ref, Expected: 1})
	if err != nil {
		t.Fatal(err)
	}
	next := memory.RecoveryChallenge{Scope: scope, Nonce: [32]byte{2}}
	if err = verifier.VerifyRecovery(next, old); err == nil {
		t.Fatal("backup proof replayed into a fresh recovery")
	}
	refreshed := old
	refreshed.Challenge = next
	if err = verifier.VerifyRecovery(next, refreshed); err == nil {
		t.Fatal("backup changed signed freshness material")
	}
	proof, err := issuer.ProveRecovery(ctx, next)
	if err != nil {
		t.Fatal(err)
	}
	if err = verifier.VerifyRecovery(next, proof); err != nil {
		t.Fatal(err)
	}
	if proof.Snapshot.Position != 2 || len(proof.Snapshot.Records) != 0 || len(proof.Snapshot.Deleted) != 1 {
		t.Fatal("issuer signed an old pre-deletion state")
	}
	floor, err := recoveryproof.NewVerifier(recoveryproof.VerifierConfig{Authority: "local-authority", Epoch: 1, Scope: scope, Key: public, MinPosition: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err = floor.VerifyRecovery(first, old); err == nil {
		t.Fatal("trusted external deletion floor rolled back")
	}
	staleStore, err := sqlitememory.Open(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	defer staleStore.Close()
	staleIssuer, err := recoveryproof.NewIssuer(staleStore, recoveryproof.IssuerConfig{Authority: "local-authority", Epoch: 1, Scope: scope, Key: private})
	if err != nil {
		t.Fatal(err)
	}
	staleProof, err := staleIssuer.ProveRecovery(ctx, next)
	if err != nil {
		t.Fatal(err)
	}
	if staleProof.Snapshot.Position != 1 || len(staleProof.Snapshot.Records) != 1 {
		t.Fatal("fixture is not the pre-deletion database")
	}
	if err = floor.VerifyRecovery(next, staleProof); err == nil {
		t.Fatal("fresh signature over rolled-back storage bypassed external floor")
	}
	tampered := proof
	tampered.Snapshot = old.Snapshot
	if err = verifier.VerifyRecovery(next, tampered); err == nil {
		t.Fatal("signed state could be substituted")
	}
	otherPublic, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	other, err := recoveryproof.NewVerifier(recoveryproof.VerifierConfig{Authority: "local-authority", Epoch: 1, Scope: scope, Key: otherPublic})
	if err != nil {
		t.Fatal(err)
	}
	if err = other.VerifyRecovery(next, proof); err == nil {
		t.Fatal("proof chose an unpinned signing key")
	}
	wrongScope := next
	wrongScope.Scope.Collection = "another"
	if _, err = issuer.ProveRecovery(ctx, wrongScope); err != memory.Denied {
		t.Fatal("issuer expanded configured collection scope")
	}
	otherEpoch, err := recoveryproof.NewIssuer(store, recoveryproof.IssuerConfig{Authority: "local-authority", Epoch: 2, Scope: scope, Key: private})
	if err != nil {
		t.Fatal(err)
	}
	epochProof, err := otherEpoch.ProveRecovery(ctx, next)
	if err != nil {
		t.Fatal(err)
	}
	if err = verifier.VerifyRecovery(next, epochProof); err == nil {
		t.Fatal("proof selected a different authority epoch")
	}
	if _, err = recoveryproof.NewIssuer(store, recoveryproof.IssuerConfig{Authority: "invalid-\xff", Epoch: 1, Scope: scope, Key: private}); err != memory.Invalid {
		t.Fatal("signing identity allowed ambiguous UTF-8 replacement")
	}
	emptyScope := memory.RecoveryScope{Namespace: "local", Collection: "empty"}
	emptyIssuer, err := recoveryproof.NewIssuer(store, recoveryproof.IssuerConfig{Authority: "local-authority", Epoch: 1, Scope: emptyScope, Key: private})
	if err != nil {
		t.Fatal(err)
	}
	emptyVerifier, err := recoveryproof.NewVerifier(recoveryproof.VerifierConfig{Authority: "local-authority", Epoch: 1, Scope: emptyScope, Key: public})
	if err != nil {
		t.Fatal(err)
	}
	emptyChallenge := memory.RecoveryChallenge{Scope: emptyScope, Nonce: [32]byte{3}}
	emptyProof, err := emptyIssuer.ProveRecovery(ctx, emptyChallenge)
	if err != nil {
		t.Fatal(err)
	}
	// Equivalent empty repeated fields may be materialized by another transport.
	emptyProof.Snapshot.Records = []memory.RecoveryRecord{}
	emptyProof.Snapshot.Deleted = []memory.VersionRef{}
	emptyProof.Snapshot.Operations = []memory.Receipt{}
	emptyProof.Snapshot.Erased = []memory.SourceEvent{}
	emptyProof.Snapshot.SourceFences = []memory.SourceErasure{}
	if err = emptyVerifier.VerifyRecovery(emptyChallenge, emptyProof); err != nil {
		t.Fatalf("empty collection encoding changed signature: %v", err)
	}
}

func TestAuthorityProofAuthenticatesExactErasureAndSourceFences(t *testing.T) {
	ctx := context.Background()
	store, err := sqlitememory.Open(filepath.Join(t.TempDir(), "current.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ref := memory.Ref{Namespace: "local", Collection: "personal", Key: "derived"}
	_, err = store.Commit(ctx, memory.Change{OperationID: "put", Subject: "operator", SemanticSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("put"))), Record: memory.Revision{Ref: ref, Revision: 1, Document: []byte(`{"spec":{"sources":[{"ref":{"kind":"note","key":"source","revision":"1"}}]}}`)}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.EraseSource(ctx, memory.SourceErasure{Namespace: "local", Kind: "note", Key: "source", ThroughRevision: 1}); err != nil {
		t.Fatal(err)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	scope := memory.RecoveryScope{Namespace: "local", Collection: "personal"}
	issuer, err := recoveryproof.NewIssuer(store, recoveryproof.IssuerConfig{Authority: "authority", Epoch: 1, Scope: scope, Key: private})
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := recoveryproof.NewVerifier(recoveryproof.VerifierConfig{Authority: "authority", Epoch: 1, Scope: scope, Key: public})
	if err != nil {
		t.Fatal(err)
	}
	challenge := memory.RecoveryChallenge{Scope: scope, Nonce: [32]byte{4}}
	proof, err := issuer.ProveRecovery(ctx, challenge)
	if err != nil {
		t.Fatal(err)
	}
	if len(proof.Snapshot.Erased) != 1 || len(proof.Snapshot.SourceFences) != 1 {
		t.Fatal("proof omitted erasure state")
	}
	if err = verifier.VerifyRecovery(challenge, proof); err != nil {
		t.Fatal(err)
	}
	altered := proof
	altered.Snapshot.SourceFences = append([]memory.SourceErasure(nil), proof.Snapshot.SourceFences...)
	altered.Snapshot.SourceFences[0].ThroughRevision = 2
	if !memory.ValidRecoverySnapshot(altered.Snapshot) {
		t.Fatal("signature mutation should remain structurally valid")
	}
	if err = verifier.VerifyRecovery(challenge, altered); err == nil {
		t.Fatal("source cutoff changed without new authority signature")
	}
}
