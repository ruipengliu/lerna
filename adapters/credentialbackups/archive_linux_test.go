package credentialbackups_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"lerna/adapters/credentialbackups"
	"lerna/credentials"
	"os"
	"path/filepath"
	"testing"
)

func TestDeletedSnapshotCannotBeRecreatedByLateWriter(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if e := os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	first, e := credentialbackups.Open(dir, "archive")
	if e != nil {
		t.Fatal(e)
	}
	second, e := credentialbackups.Open(dir, "archive")
	if e != nil {
		t.Fatal(e)
	}
	defer second.Close()
	idBytes := sha256.Sum256([]byte("fixture-operation"))
	id := hex.EncodeToString(idBytes[:])
	raw := []byte("public ciphertext fixture")
	digest, e := first.Put(ctx, id, raw)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = second.Put(ctx, id, []byte("different snapshot")); e != credentials.Conflict {
		t.Fatal("immutable snapshot overwritten", e)
	}
	if e = first.Delete(ctx, id, digest); e != nil {
		t.Fatal(e)
	}
	first.Close()
	reopened, e := credentialbackups.Open(dir, "archive")
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	if e = reopened.Check(ctx, id, digest); e != credentials.Missing {
		t.Fatal("disposed data present", e)
	}
	if _, e = second.Put(ctx, id, raw); e != credentials.Conflict {
		t.Fatal("late write resurrected snapshot", e)
	}
	if e = reopened.Delete(ctx, id, digest); e != nil {
		t.Fatal("delete replay failed", e)
	}
	if _, e = os.Stat(filepath.Join(dir, id+".json")); !os.IsNotExist(e) {
		t.Fatal("snapshot file still exists", e)
	}
}

func TestPartialDispositionResumesFromDurableTombstone(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if e := os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	archive, e := credentialbackups.Open(dir, "archive")
	if e != nil {
		t.Fatal(e)
	}
	defer archive.Close()
	sum := sha256.Sum256([]byte("interrupted-delete"))
	id := hex.EncodeToString(sum[:])
	digest, e := archive.Put(ctx, id, []byte("ciphertext fixture"))
	if e != nil {
		t.Fatal(e)
	}
	// Inject the on-disk boundary after the tombstone is durable but before the
	// ciphertext has been unlinked. This is a storage fault fixture, not a claim
	// that a subprocess was killed inside Delete.
	f, e := os.OpenFile(filepath.Join(dir, id+".deleted"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.WriteString(digest); e != nil {
		t.Fatal(e)
	}
	if e = f.Sync(); e != nil {
		t.Fatal(e)
	}
	f.Close()
	if e = archive.Check(ctx, id, digest); e != credentials.Unavailable {
		t.Fatal("partial deletion reported absent", e)
	}
	if e = archive.Delete(ctx, id, digest); e != nil {
		t.Fatal(e)
	}
	if e = archive.Check(ctx, id, digest); e != credentials.Missing {
		t.Fatal("partial deletion not resumed", e)
	}
}

func TestArchiveIdentityDoesNotFollowCopiedDirectory(t *testing.T) {
	firstDir, secondDir := t.TempDir(), t.TempDir()
	for _, dir := range []string{firstDir, secondDir} {
		if e := os.Chmod(dir, 0700); e != nil {
			t.Fatal(e)
		}
	}
	first, e := credentialbackups.Open(firstDir, "same-label")
	if e != nil {
		t.Fatal(e)
	}
	id := first.ID()
	first.Close()
	reopened, e := credentialbackups.Open(firstDir, "same-label")
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	other, e := credentialbackups.Open(secondDir, "same-label")
	if e != nil {
		t.Fatal(e)
	}
	defer other.Close()
	if reopened.ID() != id || other.ID() == id {
		t.Fatal("archive root identity was not preserved or isolated")
	}
}
