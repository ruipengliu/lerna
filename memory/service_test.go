package memory_test

import (
	"context"
	"fmt"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	sqlitememory "lerna/adapters/memory/sqlite"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"lerna/schema"
	"path/filepath"
	"testing"
	"time"
)

type fixedClock struct{}

func (fixedClock) Now() (time.Time, error) { return time.Unix(1900000000, 0), nil }

type localPolicy struct {
	deny, expired bool
	reserved      bool
}

func (p *localPolicy) Check(_ context.Context, b memory.Binding, _ *wire.MemoryRef, s *wire.MemorySpec, action string) error {
	if p.deny || b.Subject != "alice" || b.Location != "device-a" || b.Recipient != "device-a" || s.PolicyRef != "private" || s.Purpose != "assist" {
		return memory.Denied
	}
	return nil
}
func fixture(t *testing.T) (*memory.Service, *sqlitememory.Store, *localPolicy, memory.Binding, *wire.MemorySpec) {
	t.Helper()
	store, e := sqlitememory.Open(filepath.Join(t.TempDir(), "memory.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { store.Close() })
	doc := []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"urn:memory:text","type":"object","properties":{"text":{"type":"string","maxLength":256}},"required":["text"],"additionalProperties":false}`)
	schemas, e := schema.New([]schema.Resource{{Type: "memory-text", ID: "urn:memory:text", Version: "1", Document: doc}})
	if e != nil {
		t.Fatal(e)
	}
	policy := &localPolicy{}
	service, e := memory.New(store, policy, schemas, fixedClock{}, memory.Config{Location: "device-a", Timeout: time.Second})
	if e != nil {
		t.Fatal(e)
	}
	spec := &wire.MemorySpec{Kind: "preference", Content: &wire.DynamicPayload{TypeName: "memory-text", SchemaId: "urn:memory:text", SchemaVersion: "1", SchemaDigest: schema.Digest(doc), Json: []byte(`{"text":"concise"}`)}, Sources: []*wire.MemorySource{{Ref: &wire.ContentSource{Kind: "user", Key: "input-1", Revision: 1}, Method: "explicit"}}, About: "alice", Conditions: "when answering", RecordedAt: 1900000000, Confidence: &wire.MemoryConfidence{Assessment: "explicit", Basis: "user statement", Method: "declaration"}, PolicyRef: "private", Purpose: "assist", RetainUntil: 1900001000}
	return service, store, policy, memory.Binding{Token: "test", Namespace: "local", Subject: "alice", Location: "device-a", Recipient: "device-a"}, spec
}
func TestPutAndCorrectPreserveTypedMemoryAndOriginalOperation(t *testing.T) {
	service, store, _, b, spec := fixture(t)
	ctx := context.Background()
	for _, kind := range []string{"fact", "preference", "inference", "experience"} {
		in := &wire.MemoryWrite{OperationId: "save-" + kind, Ref: &wire.MemoryRef{Namespace: "local", Collection: "personal", Key: kind}, Spec: proto.Clone(spec).(*wire.MemorySpec)}
		in.Spec.Kind = kind
		receipt, e := service.Put(ctx, b, in)
		if e != nil || receipt.Revision != 1 {
			t.Fatalf("%s %+v %v", kind, receipt, e)
		}
		changed := proto.Clone(in).(*wire.MemoryWrite)
		changed.OperationId = "correct-" + kind
		changed.ExpectedRevision = 1
		changed.Spec.Content.Json = []byte(`{"text":"detailed"}`)
		correction, e := service.Correct(ctx, b, changed)
		if e != nil || correction.Revision != 2 {
			t.Fatalf("correct %+v %v", correction, e)
		}
		original, e := service.Put(ctx, b, in)
		if e != nil || original != receipt {
			t.Fatalf("original %+v %v", original, e)
		}
		record, e := store.Read(ctx, receipt.Ref, 2)
		if e != nil {
			t.Fatal(e)
		}
		decoded := new(wire.MemoryRecord)
		if protojson.Unmarshal(record.Document, decoded) != nil || decoded.Spec.Kind != kind || decoded.PreviousRevision != 1 || !proto.Equal(decoded.Spec.Sources[0], spec.Sources[0]) {
			t.Fatalf("record %+v", decoded)
		}
	}
}

func TestMemoryWriteRejectsPolicySchemaAndCurrentRevocationBeforeCommit(t *testing.T) {
	service, store, policy, b, spec := fixture(t)
	ctx := context.Background()
	in := &wire.MemoryWrite{OperationId: "save", Ref: &wire.MemoryRef{Namespace: "local", Collection: "personal", Key: "preference"}, Spec: spec}
	cases := []struct {
		name   string
		change func(*wire.MemoryWrite)
		code   error
	}{
		{"policy", func(r *wire.MemoryWrite) { r.Spec.PolicyRef = "public" }, memory.Denied},
		{"schema", func(r *wire.MemoryWrite) { r.Spec.Content.SchemaDigest = "sha256:unregistered" }, memory.Invalid},
		{"field", func(r *wire.MemoryWrite) { r.Spec.Content.Json = []byte(`{"text":"private","extra":"not allowed"}`) }, memory.Invalid},
		{"source", func(r *wire.MemoryWrite) { r.Spec.Sources = nil }, memory.Invalid},
		{"retention", func(r *wire.MemoryWrite) { r.Spec.RetainUntil = 1899999999 }, memory.Invalid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := proto.Clone(in).(*wire.MemoryWrite)
			c.change(r)
			if _, e := service.Put(ctx, b, r); e != c.code {
				t.Fatalf("%v wanted %v", e, c.code)
			}
			if _, e := store.LookupOperation(ctx, "local", "save"); e != memory.Missing {
				t.Fatalf("rejected write receipt %v", e)
			}
		})
	}
	cloud := b
	cloud.Location = "cloud"
	if _, e := service.Put(ctx, cloud, in); e != memory.Denied {
		t.Fatalf("cloud %v", e)
	}
	if _, e := service.Put(ctx, b, in); e != nil {
		t.Fatal(e)
	}
	policy.deny = true
	if _, e := service.LookupOperation(ctx, b, "save"); e != memory.Denied {
		t.Fatalf("revoked receipt %v", e)
	}
	correction := proto.Clone(in).(*wire.MemoryWrite)
	correction.OperationId = "correct"
	correction.ExpectedRevision = 1
	if _, e := service.Correct(ctx, b, correction); e != memory.Denied {
		t.Fatalf("revoked correction %v", e)
	}
	if _, e := store.Read(ctx, memory.Ref{Namespace: "local", Collection: "personal", Key: "preference"}, 2); e != memory.Missing {
		t.Fatalf("revoked correction persisted %v", e)
	}
}

func TestAdmissionExpiryRejectsNewWritesButOriginalReceiptRemainsQueryable(t *testing.T) {
	service, store, policy, b, spec := fixture(t)
	ctx := context.Background()
	request := &wire.MemoryWrite{OperationId: "accepted", Ref: &wire.MemoryRef{Namespace: "local", Collection: "personal", Key: "preference"}, Spec: spec}
	if _, e := service.Put(ctx, b, request); e != nil {
		t.Fatal(e)
	}
	policy.expired = true
	replay, e := service.Put(ctx, b, request)
	if e != nil || replay.Revision != 1 {
		t.Fatalf("old receipt %+v %v", replay, e)
	}
	request = proto.Clone(request).(*wire.MemoryWrite)
	request.OperationId = "new"
	request.Ref.Key = "other"
	if _, e = service.Put(ctx, b, request); e != memory.AdmissionExpired {
		t.Fatalf("late admission %v", e)
	}
	if _, e = store.LookupOperation(ctx, "local", "new"); e != memory.Missing {
		t.Fatalf("late receipt %v", e)
	}
	status, e := service.InspectOperation(ctx, b, "accepted")
	if e != nil || status.State != "committed" || status.Receipt == nil {
		t.Fatalf("committed %+v %v", status, e)
	}
}

func (p *localPolicy) Admit(_ context.Context, _ memory.Binding, _ string, _ string, _ *wire.MemoryRef, _ *wire.MemorySpec, _ string) (int64, error) {
	if p.expired {
		return 0, memory.AdmissionExpired
	}
	p.reserved = true
	return time.Unix(1900000060, 0).UnixNano(), nil
}
func (p *localPolicy) Inspect(context.Context, memory.Binding, string) (memory.AdmissionState, error) {
	if p.deny {
		return memory.AdmissionState{}, memory.Denied
	}
	return memory.AdmissionState{State: "not_admitted", Reserved: p.reserved}, nil
}

type lostMemoryCommit struct {
	memory.Store
	lost bool
}

func (s *lostMemoryCommit) Commit(ctx context.Context, in memory.Change) (memory.Receipt, error) {
	result, e := s.Store.Commit(ctx, in)
	if e == nil && !s.lost {
		s.lost = true
		return memory.Receipt{}, memory.Unavailable
	}
	return result, e
}
func TestUnknownCommitUsesOriginalReceiptWithoutAnotherRevision(t *testing.T) {
	_, store, policy, b, spec := fixture(t)
	ctx := context.Background()
	lost := &lostMemoryCommit{Store: store}
	service, e := memory.New(lost, policy, acceptRegistered{}, fixedClock{}, memory.Config{Location: "device-a", Timeout: time.Second})
	if e != nil {
		t.Fatal(e)
	}
	request := &wire.MemoryWrite{OperationId: "save-lost", Ref: &wire.MemoryRef{Namespace: "local", Collection: "personal", Key: "preference"}, Spec: spec}
	if _, e = service.Put(ctx, b, request); e != memory.Unavailable {
		t.Fatalf("lost response %v", e)
	}
	status, e := service.InspectOperation(ctx, b, "save-lost")
	if e != nil || status.State != "committed" || status.Receipt == nil || status.Receipt.Revision != 1 {
		t.Fatalf("status %+v %v", status, e)
	}
	result, e := service.Put(ctx, b, request)
	if e != nil || result != *status.Receipt {
		t.Fatalf("retry %+v %v", result, e)
	}
	changes, e := store.ReadChanges(ctx, "local", "personal", 0, 10)
	if e != nil || len(changes) != 1 {
		t.Fatalf("duplicated effect %+v %v", changes, e)
	}
}

type laterClock struct{}

func (laterClock) Now() (time.Time, error) { return time.Unix(1900002000, 0), nil }

type missingBody struct{ memory.Store }

func (missingBody) Read(context.Context, memory.Ref, uint64) (memory.Revision, error) {
	return memory.Revision{}, memory.Missing
}
func TestCommittedStatusSurvivesUnavailableBody(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprint(missing), func(t *testing.T) {
			service, store, policy, b, spec := fixture(t)
			ctx := context.Background()
			if _, e := service.Put(ctx, b, &wire.MemoryWrite{OperationId: "save", Ref: &wire.MemoryRef{Namespace: "local", Collection: "personal", Key: "record"}, Spec: spec}); e != nil {
				t.Fatal(e)
			}
			var backend memory.Store = store
			var clock memory.Clock = laterClock{}
			if missing {
				backend = missingBody{store}
				clock = fixedClock{}
			}
			service, e := memory.New(backend, policy, acceptRegistered{}, clock, memory.Config{Location: "device-a", Timeout: time.Second})
			if e != nil {
				t.Fatal(e)
			}
			state, e := service.InspectOperation(ctx, b, "save")
			if e != nil || state.State != "committed" || state.Receipt == nil || state.ContentAvailability != "unavailable" {
				t.Fatalf("status %v %v", state, e)
			}
			policy.deny = true
			if _, e = service.InspectOperation(ctx, b, "save"); e != memory.Denied {
				t.Fatalf("revoked %v", e)
			}
		})
	}
}
