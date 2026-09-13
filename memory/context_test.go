package memory_test

import (
	"context"
	"google.golang.org/protobuf/proto"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"testing"
)

func TestContextValidityTracksOnlyItsAuthorizedCurrentRevision(t *testing.T) {
	service, _, policy, b, spec := fixture(t)
	ctx := context.Background()
	ref := &wire.MemoryRef{Namespace: "local", Collection: "personal", Key: "style"}
	write := &wire.MemoryWrite{OperationId: "create", Ref: ref, Spec: spec}
	if _, e := service.Put(ctx, b, write); e != nil {
		t.Fatal(e)
	}
	if e := service.ValidateCurrent(ctx, b, ref, 1, "assist"); e != nil {
		t.Fatal(e)
	}
	other := proto.Clone(write).(*wire.MemoryWrite)
	other.OperationId = "other"
	other.Ref.Key = "unrelated"
	if _, e := service.Put(ctx, b, other); e != nil {
		t.Fatal(e)
	}
	if e := service.ValidateCurrent(ctx, b, ref, 1, "assist"); e != nil {
		t.Fatalf("unrelated change %v", e)
	}
	correction := proto.Clone(write).(*wire.MemoryWrite)
	correction.OperationId = "correct"
	correction.ExpectedRevision = 1
	correction.Spec.Content.Json = []byte(`{"text":"detailed"}`)
	if _, e := service.Correct(ctx, b, correction); e != nil {
		t.Fatal(e)
	}
	if e := service.ValidateCurrent(ctx, b, ref, 1, "assist"); e != memory.ContextInvalidated {
		t.Fatalf("stale context %v", e)
	}
	if e := service.ValidateCurrent(ctx, b, ref, 2, "assist"); e != nil {
		t.Fatal(e)
	}
	policy.deny = true
	if e := service.ValidateCurrent(ctx, b, ref, 1, "assist"); e != memory.Denied {
		t.Fatalf("hidden stale metadata %v", e)
	}
	if e := service.ValidateCurrent(ctx, b, ref, 2, "assist"); e != memory.Denied {
		t.Fatalf("revoked current %v", e)
	}
}
