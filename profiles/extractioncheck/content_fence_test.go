package extractioncheck

import (
	"context"
	"strings"
	"testing"

	"lerna/answers"
	"lerna/artifacts"
	"lerna/extraction"
	wire "lerna/gen/harness/v1"
)

func TestSourceFenceStopsTaskInputDisclosureBeforeContentCleanup(t *testing.T) {
	ctx := context.Background()
	h, err := fresh(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	r, _, err := h.request(ctx, []byte(`{"sources":[{"kind":"note","key":"one","revision":1}]}`+strings.Repeat(" ", 80)))
	if err != nil {
		t.Fatal(err)
	}
	ref, err := answers.ParseReference(r.InputRef)
	if err != nil {
		t.Fatal(err)
	}
	b := artifacts.Binding{Token: h.token, Namespace: "local", Location: "local", Recipient: "local"}
	meta, err := h.content.Call(ctx, b, &wire.ContentRequest{Method: "GET", Ref: ref, Purpose: "task"})
	if err != nil {
		t.Fatal(err)
	}
	if _, e := h.blobs.Read(ctx, ref.Key, 0, uint32(meta.Record.Spec.Size), meta.Record.Spec.Size, meta.Record.Spec.Sha256); e != nil {
		t.Fatalf("input must be stored as actual blob: %v", e)
	}
	read := &wire.ContentRequest{Method: "READ", Ref: ref, Purpose: "task", Limit: uint32(meta.Record.Spec.Size)}
	if out, e := h.content.Call(ctx, b, read); e != nil || len(out.Data) == 0 {
		t.Fatalf("input fixture: %v", e)
	}
	if n, e := h.candidates.InvalidateSource(ctx, extraction.SourceInvalidation{Namespace: "local", Kind: "note", Key: "one", ThroughRevision: 1}); e != nil || n != 0 {
		t.Fatalf("metadata-only source invalidation: %d %v", n, e)
	}
	// Content's own invalidation/cleanup has not run and its source policy still
	// permits revision 1. The shared durable fence must already stop release.
	for _, request := range []*wire.ContentRequest{{Method: "GET", Ref: ref, Purpose: "task"}, read} {
		out, e := h.content.Call(ctx, b, request)
		if e == nil || out != nil {
			t.Fatalf("%s released invalidated task metadata before cleanup: %v", request.Method, e)
		}
	}
	change := artifacts.SourceInvalidation{Namespace: "local", Kind: "note", Key: "one", ThroughRevision: 1}
	if _, err = h.content.InvalidateSource(ctx, change); err != nil {
		t.Fatal(err)
	}
	if err = h.content.Clean(ctx); err != nil {
		t.Fatal(err)
	}
	status, err := h.content.InvalidateSource(ctx, change)
	if err != nil || status.Cleaning != 0 {
		t.Fatalf("content cleanup: %+v %v", status, err)
	}
	files, err := h.blobs.List(ctx, 128)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range files {
		if key.Key == ref.Key {
			t.Fatal("input blob remains after cleanup")
		}
	}
}
