package memory_test

import (
	"context"
	"google.golang.org/protobuf/proto"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"testing"
	"time"
)

type readPermits struct{}

func (readPermits) Reserve(_ context.Context, b memory.Binding, i memory.ReadIntent, material string) (string, error) {
	if material != "single" || b.Subject != "alice" || i.Collection != "personal" || i.Purpose != "assist" {
		return "", memory.Denied
	}
	return i.ID, nil
}
func (readPermits) Validate(ctx context.Context, b memory.Binding, i memory.ReadIntent, material, permit string) error {
	p, e := (readPermits{}).Reserve(ctx, b, i, material)
	if e != nil || p != permit {
		return memory.Denied
	}
	return nil
}
func TestQueryAndGetBindOriginalSelectionBeforeDisclosure(t *testing.T) {
	service, _, _, b, spec := fixture(t)
	ctx := context.Background()
	reader, e := memory.NewReader(service, readPermits{})
	if e != nil {
		t.Fatal(e)
	}
	write := &wire.MemoryWrite{OperationId: "save", Ref: &wire.MemoryRef{Namespace: "local", Collection: "personal", Key: "format"}, Spec: spec}
	if _, e = service.Put(ctx, b, write); e != nil {
		t.Fatal(e)
	}
	query := &wire.MemoryQuery{ReadId: "read-1", Collection: "personal", Purpose: "assist", Text: "concise", Kind: "preference", About: "alice", MaxResults: 4, MaxBytes: 8000}
	first, e := reader.Query(ctx, b, query, "single")
	if e != nil || len(first.Records) != 1 || first.Records[0].Revision != 1 || first.Coverage != "complete" {
		t.Fatalf("first %+v %v", first, e)
	}
	write = proto.Clone(write).(*wire.MemoryWrite)
	write.OperationId = "correct"
	write.ExpectedRevision = 1
	write.Spec.Content.Json = []byte(`{"text":"detailed"}`)
	if _, e = service.Correct(ctx, b, write); e != nil {
		t.Fatal(e)
	}
	retry, e := reader.Query(ctx, b, query, "single")
	if e != nil || len(retry.Records) != 1 || retry.Records[0].Revision != 1 {
		t.Fatalf("retry %+v %v", retry, e)
	}
	exact, e := reader.Get(ctx, b, &wire.MemoryGet{ReadId: "exact-1", Ref: write.Ref, Revision: 1, Purpose: "assist"}, "single")
	if e != nil || len(exact.Records) != 1 || exact.Records[0].Revision != 1 {
		t.Fatalf("exact %+v %v", exact, e)
	}
	query.ReadId = "fresh-1"
	fresh, e := reader.Query(ctx, b, query, "single")
	if e != nil || len(fresh.Records) != 0 || fresh.Coverage != "complete" {
		t.Fatalf("fresh %+v %v", fresh, e)
	}
	query.Text = "detailed"
	if _, e = reader.Query(ctx, b, query, "single"); e != memory.IdentityConflict {
		t.Fatalf("expanded read %v", e)
	}
}

type visibility struct{}

func (visibility) Check(_ context.Context, _ memory.Binding, r *wire.MemoryRef, _ *wire.MemorySpec, action string) error {
	if r.Key == "hidden" {
		return memory.Denied
	}
	if r.Key == "offline" && action == "read" {
		return memory.Unavailable
	}
	return nil
}
func TestQueryCoverageSeparatesHiddenUnavailableAndBudget(t *testing.T) {
	service, store, _, b, spec := fixture(t)
	ctx := context.Background()
	for _, key := range []string{"hidden", "offline", "visible"} {
		if _, e := service.Put(ctx, b, &wire.MemoryWrite{OperationId: "save-" + key, Ref: &wire.MemoryRef{Namespace: "local", Collection: "personal", Key: key}, Spec: spec}); e != nil {
			t.Fatal(e)
		}
	}
	// A distinct policy instance changes visibility without altering storage.
	readerService, e := memory.New(store, visibility{}, acceptRegistered{}, fixedClock{}, memory.Config{Location: "device-a", Timeout: time.Second})
	if e != nil {
		t.Fatal(e)
	}
	reader, e := memory.NewReader(readerService, readPermits{})
	if e != nil {
		t.Fatal(e)
	}
	q := &wire.MemoryQuery{ReadId: "coverage", Collection: "personal", Purpose: "assist", MaxResults: 4, MaxBytes: 8000}
	result, e := reader.Query(ctx, b, q, "single")
	if e != nil || len(result.Records) != 1 || result.Records[0].Ref.Key != "visible" || result.Coverage != "partial_unavailable" {
		t.Fatalf("partial %+v %v", result, e)
	}
	q.ReadId = "budget"
	q.MaxBytes = 1
	result, e = reader.Query(ctx, b, q, "single")
	if e != nil || len(result.Records) != 0 || result.Coverage != "partial_and_budget_exhausted" {
		t.Fatalf("combined %+v %v", result, e)
	}
}

type acceptRegistered struct{}

func (acceptRegistered) Validate(*wire.DynamicPayload) error { return nil }

func (visibility) Admit(context.Context, memory.Binding, string, string, *wire.MemoryRef, *wire.MemorySpec, string) (int64, error) {
	return 0, memory.Denied
}
func (visibility) Inspect(context.Context, memory.Binding, string) (memory.AdmissionState, error) {
	return memory.AdmissionState{}, memory.Denied
}

type changingVisibility struct {
	visibility
	revoked bool
}

func (v *changingVisibility) Check(ctx context.Context, b memory.Binding, ref *wire.MemoryRef, spec *wire.MemorySpec, action string) error {
	if v.revoked {
		return memory.Denied
	}
	return v.visibility.Check(ctx, b, ref, spec, action)
}
func TestCoverageReplayRechecksExcludedSources(t *testing.T) {
	for _, key := range []string{"offline", "visible"} {
		t.Run(key, func(t *testing.T) {
			service, store, _, b, spec := fixture(t)
			ctx := context.Background()
			if _, e := service.Put(ctx, b, &wire.MemoryWrite{OperationId: "save", Ref: &wire.MemoryRef{Namespace: "local", Collection: "personal", Key: key}, Spec: spec}); e != nil {
				t.Fatal(e)
			}
			policy := &changingVisibility{}
			service, e := memory.New(store, policy, acceptRegistered{}, fixedClock{}, memory.Config{Location: "device-a", Timeout: time.Second})
			if e != nil {
				t.Fatal(e)
			}
			reader, e := memory.NewReader(service, readPermits{})
			if e != nil {
				t.Fatal(e)
			}
			q := &wire.MemoryQuery{ReadId: "coverage-replay", Collection: "personal", Purpose: "assist", MaxResults: 1, MaxBytes: 1}
			first, e := reader.Query(ctx, b, q, "single")
			if e != nil || len(first.Records) != 0 || first.Coverage == "complete" {
				t.Fatalf("first %v %v", first, e)
			}
			policy.revoked = true
			if result, e := reader.Query(ctx, b, q, "single"); e != memory.Denied || result != nil {
				t.Fatalf("revoked coverage %v %v", result, e)
			}
		})
	}
}
