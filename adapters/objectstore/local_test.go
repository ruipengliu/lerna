package objectstore_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/ruipengliu/lerna/adapters/objectstore"
	"github.com/ruipengliu/lerna/api"
)

func TestImmutableLocalBytesSurviveReopenAndRejectChangedVersion(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := objectstore.OpenLocal(dir, 1024)
	if err != nil {
		t.Fatal(err)
	}
	ref := api.ContentRef{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner"), ContentID: api.NewID("content"), Version: 1, Hash: api.Hash([]byte("准确字节")), MediaType: "text/plain", ByteLength: 12}
	loc, err := s.Write(ctx, ref, bytes.NewReader([]byte("准确字节")))
	if err != nil {
		t.Fatal(err)
	}
	s, err = objectstore.OpenLocal(dir, 1024)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Read(ctx, loc, ref, 1024)
	if err != nil || string(got) != "准确字节" {
		t.Fatalf("read = %q, %v", got, err)
	}
	changed := ref
	changed.Hash = api.Hash([]byte("另一字节"))
	if _, err = s.Write(ctx, changed, bytes.NewReader([]byte("另一字节"))); !api.IsCode(err, "idempotency_conflict") {
		t.Fatalf("changed immutable identity = %v", err)
	}
	if s.Durability() != "local_fsync" {
		t.Fatalf("local durability misrepresented: %s", s.Durability())
	}
}
