//go:build linux && integration

package local

import (
	"context"
	"errors"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	d "github.com/ruipengliu/lerna/domain/content"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The trusted holder and independent exact bytes are adopted ticket05 seams.
// This control proves a durable closed key survives reopening the holder.
func TestErasedExactVersionCannotBeReinstalledAfterReopen(t *testing.T) {
	root, confirmed := ownedLocalRoot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ref := v.ContentRef{Owner: v.OwnerRef{TenantID: "tenant-cleanup", OwnerID: "content-owner"}, ContentID: "artifact", Version: "1", Hash: "sha256:b6a98d9ce9a2d9149288fa3df42d377c3e42737afdcdaf714e33c0a100b51060", MediaType: "text/plain", ByteLength: "6"}
	_, key, err := d.VersionIdentity(ref)
	if err != nil {
		t.Fatal(err)
	}
	identity := d.ErasureIdentity{Ref: ref, ObjectKey: key, HolderID: "primary", SealID: "cleanup-original"}
	store, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Put(ctx, key, key+".1.tmp", ref.Hash, 6, []byte("alpha\n")); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(filepath.Join(root, key))
	if err != nil || string(actual) != "alpha\n" {
		t.Fatalf("independent normal body: %q %v", actual, err)
	}
	if _, err = store.FenceAndErase(ctx, identity, []string{key + ".1.tmp"}); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	observation, err := reopened.ObserveErasure(ctx, identity, "", 2)
	if err != nil || !observation.Fenced || !observation.Erased || observation.NextCursor != "" {
		t.Fatalf("independent reopened erasure: %+v %v", observation, err)
	}
	if _, err = os.ReadFile(filepath.Join(root, key)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("exact body remains: %v", err)
	}
	if err = reopened.Put(ctx, key, key+".2.tmp", ref.Hash, 6, []byte("alpha\n")); !errors.Is(err, d.ErrBodySealed) {
		t.Fatalf("late original version rebuilt body: %v", err)
	}
	if _, err = reopened.Read(ctx, key, ref.Hash, 6); !errors.Is(err, d.ErrBodySealed) {
		t.Fatalf("closed version delivered bytes: %v", err)
	}
	if err = reopened.Close(); err != nil {
		t.Fatal(err)
	}
	confirmed()
}
