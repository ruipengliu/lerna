package artifacts_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"lerna/artifacts"
	wire "lerna/gen/harness/v1"
	"testing"
	"time"
)

func TestSourceInvalidationCleansDerivedContentAndPreventsReintroduction(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	// An unrelated namespace must not invalidate this host's records.
	in := &wire.ContentRequest{Method: "PUT", Spec: &wire.ContentSpec{Kind: "artifact", Resource: "root", Purpose: "research", Sources: []*wire.ContentSource{{Kind: "input", Key: "source", Revision: 1}}, AcquiredAt: h.now.Unix(), MediaType: "text/plain", RetainUntil: h.now.Add(time.Minute).Unix()}, Data: []byte("a body stored outside the inline record")}
	sum := sha256.Sum256(in.Data)
	in.Spec.Size = uint64(len(in.Data))
	in.Spec.Sha256 = hex.EncodeToString(sum[:])
	var err error
	in.OperationId, err = h.auth.NewOperation(ctx, h.token)
	if err != nil {
		t.Fatal(err)
	}
	out, err := h.content.Call(ctx, h.binding, in)
	if err != nil {
		t.Fatal(err)
	}
	wrong := artifacts.SourceInvalidation{Namespace: "other", Kind: "input", Key: "source", ThroughRevision: 1}
	if _, err = h.content.InvalidateSource(ctx, wrong); err == nil {
		t.Fatal("foreign namespace accepted")
	}
	change := wrong
	change.Namespace = "local"
	status, err := h.content.InvalidateSource(ctx, change)
	if err != nil || status.Cleaning != 1 || status.Cleaned != 0 {
		t.Fatalf("invalidation: %+v %v", status, err)
	}
	h.reopen(t)
	if _, err = h.content.Call(ctx, h.binding, &wire.ContentRequest{Method: "READ", Ref: out.Record.Ref, Purpose: "research", Limit: 8}); err == nil {
		t.Fatal("invalidated body disclosed")
	}
	in.OperationId, err = h.auth.NewOperation(ctx, h.token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.content.Call(ctx, h.binding, in); err == nil {
		t.Fatal("invalidated source reintroduced")
	}
	if err = h.content.Clean(ctx); err != nil {
		t.Fatal(err)
	}
	status, err = h.content.InvalidateSource(ctx, change)
	if err != nil || status.Cleaning != 0 || status.Cleaned != 1 {
		t.Fatalf("cleanup replay: %+v %v", status, err)
	}
	files, err := h.blobs.List(ctx, 64)
	if err != nil || len(files) != 0 {
		t.Fatalf("derived file remains: %+v %v", files, err)
	}
}

type multipleSources struct{}

func (multipleSources) Check(_ context.Context, s *wire.ContentSource, _, purpose, location string, _ int64) error {
	if s.Kind != "input" || (s.Key != "source" && s.Key != "other") || s.Revision != 1 || purpose != "research" || location != "device" {
		return artifacts.Error("PERMISSION_DENIED")
	}
	return nil
}
func TestMixedSourceArtifactInvalidatesWithoutDeletingIndependentContent(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	service, err := artifacts.New(h.auth, h.blobs, multipleSources{}, artifacts.Config{Inline: 16, MaxObject: 1 << 20, MaxTotal: 2 << 20, MaxRecords: 32, MaxChunk: 65536, MaxFiles: 64, CleanupBatch: 16, Timeout: time.Second, Retention: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	save := func(keys ...string) *wire.ContentRef {
		t.Helper()
		op, e := h.auth.NewOperation(ctx, h.token)
		if e != nil {
			t.Fatal(e)
		}
		refs := []*wire.ContentSource{}
		for _, key := range keys {
			refs = append(refs, &wire.ContentSource{Kind: "input", Key: key, Revision: 1})
		}
		out, e := service.Call(ctx, h.binding, &wire.ContentRequest{Method: "PUT", OperationId: op, Data: []byte("hello"), Spec: &wire.ContentSpec{Kind: "artifact", Resource: "root", Purpose: "research", Sources: refs, AcquiredAt: h.now.Unix(), MediaType: "text/plain", Size: 5, Sha256: "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824", RetainUntil: h.now.Add(time.Minute).Unix()}})
		if e != nil {
			t.Fatal(e)
		}
		return out.Record.Ref
	}
	mixed := save("source", "other")
	independent := save("other")
	change := artifacts.SourceInvalidation{Namespace: "local", Kind: "input", Key: "source", ThroughRevision: 1}
	status, err := service.InvalidateSource(ctx, change)
	if err != nil || status.Cleaning != 1 {
		t.Fatalf("mixed invalidation: %+v %v", status, err)
	}
	if _, err = service.Call(ctx, h.binding, &wire.ContentRequest{Method: "READ", Ref: mixed, Purpose: "research", Limit: 5}); err == nil {
		t.Fatal("mixed summary still readable")
	}
	if err = service.Clean(ctx); err != nil {
		t.Fatal(err)
	}
	out, err := service.Call(ctx, h.binding, &wire.ContentRequest{Method: "READ", Ref: independent, Purpose: "research", Limit: 5})
	if err != nil || string(out.Data) != "hello" {
		t.Fatalf("independent source was removed: %+v %v", out, err)
	}
}
