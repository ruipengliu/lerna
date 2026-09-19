package artifacts_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	contextmemory "lerna/adapters/context/memory"
	memorycleanup "lerna/adapters/memory/cleanup"
	"lerna/artifacts"
	"lerna/contextassembly"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"testing"
	"time"
)

type revisionPolicy struct{}

func (revisionPolicy) Check(_ context.Context, s *wire.ContentSource, _, purpose, location string, _ int64) error {
	if s == nil || s.Revision < 1 || s.Revision > 3 || purpose != "research" || location != "device" {
		return artifacts.Error("PERMISSION_DENIED")
	}
	return nil
}
func exactContentService(t *testing.T, h *harness) *artifacts.Service {
	t.Helper()
	s, err := artifacts.New(h.auth, h.blobs, revisionPolicy{}, artifacts.Config{Inline: 16, MaxObject: 1 << 20, MaxTotal: 2 << 20, MaxRecords: 32, MaxChunk: 65536, MaxFiles: 64, CleanupBatch: 16, Timeout: time.Second, Retention: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func TestExactMemoryErasureCleansOnlyMatchingArtifactRevision(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	service := exactContentService(t, h)
	body := []byte("actual file body beyond the inline threshold")
	sum := sha256.Sum256(body)
	save := func(revisions ...uint64) (*wire.ContentRecord, error) {
		op, e := h.auth.NewOperation(ctx, h.token)
		if e != nil {
			return nil, e
		}
		sources := []*wire.ContentSource{}
		for _, revision := range revisions {
			sources = append(sources, contextmemory.SourceReference(contextassembly.Reference{Namespace: "local", Collection: "personal", Key: "style", Revision: revision}))
		}
		out, e := service.Call(ctx, h.binding, &wire.ContentRequest{Method: "PUT", OperationId: op, Data: body, Spec: &wire.ContentSpec{Kind: "artifact", Resource: "root", Purpose: "research", Sources: sources, AcquiredAt: h.now.Unix(), MediaType: "text/plain", Size: uint64(len(body)), Sha256: hex.EncodeToString(sum[:]), RetainUntil: h.now.Add(time.Minute).Unix()}})
		if e != nil {
			return nil, e
		}
		return out.Record, nil
	}
	records := map[uint64]*wire.ContentRecord{}
	for _, revision := range []uint64{1, 2, 3} {
		r, e := save(revision)
		if e != nil {
			t.Fatal(e)
		}
		records[revision] = r
		if _, e = h.blobs.Read(ctx, r.Ref.Key, 0, uint32(r.Spec.Size), r.Spec.Size, r.Spec.Sha256); e != nil {
			t.Fatalf("real blob fixture: %v", e)
		}
	}
	mixed, err := save(2, 3)
	if err != nil {
		t.Fatal(err)
	}
	sink, err := memorycleanup.NewArtifacts(service)
	if err != nil {
		t.Fatal(err)
	}
	event := memory.SourceEvent{Ref: memory.Ref{Namespace: "local", Collection: "personal", Key: "style"}, Kind: memory.SourceErased, Revision: 2, Position: 4}
	if complete, e := sink.Apply(ctx, event); e != nil || !complete {
		t.Fatalf("exact artifact cleanup: %v %v", complete, e)
	}
	files, err := h.blobs.List(ctx, 64)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.Key == records[2].Ref.Key || file.Key == mixed.Ref.Key {
			t.Fatal("affected blob remains")
		}
	}
	for _, revision := range []uint64{1, 3} {
		out, e := service.Call(ctx, h.binding, &wire.ContentRequest{Method: "READ", Ref: records[revision].Ref, Purpose: "research", Limit: uint32(len(body))})
		if e != nil || string(out.Data) != string(body) {
			t.Fatalf("independent revision %d damaged: %v", revision, e)
		}
	}
	h.reopen(t)
	service = exactContentService(t, h)
	sink, err = memorycleanup.NewArtifacts(service)
	if err != nil {
		t.Fatal(err)
	}
	if complete, e := sink.Apply(ctx, event); e != nil || !complete {
		t.Fatalf("replay: %v %v", complete, e)
	}
	if _, e := save(2); e == nil {
		t.Fatal("erased source reintroduced after reopen")
	}
	if _, e := save(3); e != nil {
		t.Fatalf("independent source blocked: %v", e)
	}
}
