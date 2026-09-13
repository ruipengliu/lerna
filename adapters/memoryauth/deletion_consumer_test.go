package memoryauth_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"lerna/adapters/contextmemory"
	"lerna/adapters/filecontent"
	"lerna/adapters/memoryauth"
	"lerna/adapters/memorycleanup"
	"lerna/adapters/sqlitememory"
	"lerna/artifacts"
	"lerna/authorization"
	"lerna/contextassembly"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
)

// Fail only the persistent acknowledgment, after the real sink has returned.
type failedAck struct{ memory.ConsumerProgress }

func (failedAck) AckEvent(context.Context, memory.ConsumerBinding, uint64, uint64) error {
	return memory.Unavailable
}

type lostAck struct{ memory.ConsumerProgress }

func (p lostAck) AckEvent(ctx context.Context, binding memory.ConsumerBinding, expected, next uint64) error {
	if err := p.ConsumerProgress.AckEvent(ctx, binding, expected, next); err != nil {
		return err
	}
	return memory.Unavailable
}

func checkDeletionConsumer(t *testing.T, service *memory.Service, store *sqlitememory.Store, policy *memoryauth.Authority, auth *authorization.Service, b memory.Binding, spec *wire.MemorySpec) {
	ctx := context.Background()
	next := func() string {
		op, err := auth.NewOperation(ctx, b.Token)
		if err != nil {
			t.Fatal(err)
		}
		return op
	}
	ref := &wire.MemoryRef{Namespace: b.Namespace, Collection: "personal", Key: "consumer-victim"}
	putID := next()
	if _, err := service.Put(ctx, b, &wire.MemoryWrite{OperationId: putID, Ref: ref, Spec: proto.Clone(spec).(*wire.MemorySpec)}); err != nil {
		t.Fatal(err)
	}
	dependency := contextassembly.Reference{Namespace: ref.Namespace, Collection: ref.Collection, Key: ref.Key, Revision: 1}
	resolver, err := contextmemory.NewSources(service, func(view artifacts.SourceAuthority) memory.Checker { return policy.ReadPolicy(view) }, sourcePolicyForMissing{}, b.Location, []contextassembly.Reference{dependency})
	if err != nil {
		t.Fatal(err)
	}
	blobs, err := filecontent.Open(filepath.Join(t.TempDir(), "content"))
	if err != nil {
		t.Fatal(err)
	}
	defer blobs.Close()
	content, err := artifacts.New(auth, blobs, resolver, artifacts.Config{Inline: 16, MaxObject: 1024, MaxTotal: 4096, MaxRecords: 16, MaxChunk: 1024, MaxFiles: 32, CleanupBatch: 16, Timeout: time.Second, Retention: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	binding := artifacts.Binding{Token: b.Token, Namespace: b.Namespace, Location: b.Location, Recipient: b.Recipient}
	put := &wire.ContentRequest{Method: "PUT", OperationId: next(), Data: []byte("hello"), Spec: &wire.ContentSpec{Kind: "artifact", Resource: "root", Purpose: "assist", Sources: []*wire.ContentSource{contextmemory.SourceReference(dependency)}, AcquiredAt: 1900000000, MediaType: "text/plain", Size: 5, Sha256: "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824", RetainUntil: 1900000500}}
	put.Data = []byte("controlled derived memory text")
	hash := sha256.Sum256(put.Data)
	put.Spec.Size = uint64(len(put.Data))
	put.Spec.Sha256 = hex.EncodeToString(hash[:])
	out, err := content.Call(ctx, binding, put)
	if err != nil {
		t.Fatal(err)
	}
	sink, err := memorycleanup.NewArtifacts(content)
	if err != nil {
		t.Fatal(err)
	}
	config := memory.ConsumerConfig{Binding: memory.ConsumerBinding{Namespace: b.Namespace, Collection: ref.Collection, Consumer: "artifact-cleanup", ConfigSHA256: strings.Repeat("a", 64)}, Batch: 32, Timeout: 5 * time.Second}
	consumer, err := memory.NewSourceConsumer(store, store, sink, config)
	if err != nil {
		t.Fatal(err)
	}
	before, err := consumer.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	read := &wire.ContentRequest{Method: "READ", Ref: out.Record.Ref, Purpose: "assist", Limit: uint32(len(put.Data))}
	if got, err := content.Call(ctx, binding, read); err != nil || string(got.GetData()) != "controlled derived memory text" {
		t.Fatalf("create event removed current source: %v", err)
	}
	deleted, err := service.Delete(ctx, b, memory.DeleteRequest{OperationID: next(), Ref: ref, ExpectedRevision: 1, Purpose: "assist"})
	if err != nil {
		t.Fatal(err)
	}
	faulty, err := memory.NewSourceConsumer(store, failedAck{store}, sink, config)
	if err != nil {
		t.Fatal(err)
	}
	result, err := faulty.Run(ctx)
	if err != memory.Unavailable || result.Position != before.Position {
		t.Fatalf("failed confirmation advanced: %+v %v", result, err)
	}
	if position, err := store.BindConsumer(ctx, config.Binding); err != nil || position != before.Position {
		t.Fatalf("durable cursor: %d %v", position, err)
	}
	unlock, err := blobs.Lock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	files, err := blobs.List(ctx, 32)
	unlock()
	if err != nil || len(files) != 0 {
		t.Fatalf("cleanup not performed before acknowledgment: %+v %v", files, err)
	}
	if _, err := content.Call(ctx, binding, read); err == nil {
		t.Fatal("deleted source disclosed")
	}
	result, err = consumer.Run(ctx)
	if err != nil || result.Position != deleted.Position || result.Applied != 1 {
		t.Fatalf("original event recovery: %+v %v", result, err)
	}
	if again, err := consumer.Run(ctx); err != nil || again.Position != deleted.Position || again.Applied != 0 {
		t.Fatalf("repeat: %+v %v", again, err)
	}

	// An acknowledgment can also commit and lose its reply. Reconciliation
	// reads durable progress before deciding whether the event needs replay.
	nextRef := proto.Clone(ref).(*wire.MemoryRef)
	nextRef.Key = "consumer-lost-ack"
	created, err := service.Put(ctx, b, &wire.MemoryWrite{OperationId: next(), Ref: nextRef, Spec: proto.Clone(spec).(*wire.MemorySpec)})
	if err != nil {
		t.Fatal(err)
	}
	lost, err := memory.NewSourceConsumer(store, lostAck{store}, sink, config)
	if err != nil {
		t.Fatal(err)
	}
	unknown, err := lost.Run(ctx)
	if err != memory.Unavailable || unknown.Position != deleted.Position {
		t.Fatalf("unknown acknowledgment reported confirmed: %+v %v", unknown, err)
	}
	if position, err := store.BindConsumer(ctx, config.Binding); err != nil || position != created.Position {
		t.Fatalf("lost acknowledgment durable result: %d %v", position, err)
	}
	restarted, err := memory.NewSourceConsumer(store, store, sink, config)
	if err != nil {
		t.Fatal(err)
	}
	if recovered, err := restarted.Run(ctx); err != nil || recovered.Applied != 0 || recovered.Position != created.Position {
		t.Fatalf("committed acknowledgment replay: %+v %v", recovered, err)
	}
	beforeAdmission, err := auth.InspectMemoryOperation(ctx, b.Token, putID)
	if err != nil || beforeAdmission.Admission.SemanticSHA256 == "" {
		t.Fatalf("original comparison: %+v %v", beforeAdmission, err)
	}
	if err = auth.EraseMemoryComparisons(ctx, "other", []authorization.MemoryComparison{{OperationID: putID, Subject: b.Subject}}); !authorization.Is(err, authorization.Denied) {
		t.Fatalf("cross namespace cleanup: %v", err)
	}
	if err = auth.EraseMemoryComparisons(ctx, b.Namespace, []authorization.MemoryComparison{{OperationID: putID, Subject: "other"}}); !authorization.Is(err, authorization.IdentityConflict) {
		t.Fatalf("wrong original subject: %v", err)
	}
	admissions, err := memorycleanup.NewAdmissions(store, auth)
	if err != nil {
		t.Fatal(err)
	}
	config.Binding.Consumer = "admission-cleanup"
	cleaner, err := memory.NewSourceConsumer(store, store, admissions, config)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := cleaner.Run(ctx); err != nil || result.Position != created.Position {
		t.Fatalf("admission cleanup: %+v %v", result, err)
	}
	afterAdmission, err := auth.InspectMemoryOperation(ctx, b.Token, putID)
	if err != nil || afterAdmission.State != "reserved" || afterAdmission.Admission.Subject != b.Subject || afterAdmission.Admission.SemanticSHA256 != "" {
		t.Fatalf("erased admission: %+v %v", afterAdmission, err)
	}
	if _, err = auth.ReserveMemoryOperation(ctx, b.Token, putID, beforeAdmission.Admission.SemanticSHA256, beforeAdmission.Admission.Action); !authorization.Is(err, authorization.ResultOnly) {
		t.Fatalf("erased comparison admitted retry: %v", err)
	}
	if historical, err := service.InspectOperation(ctx, b, putID); err != nil || historical.State != "committed" || historical.ContentAvailability != "unavailable" {
		t.Fatalf("historical result lost: %+v %v", historical, err)
	}
	if unrelated, err := auth.InspectMemoryOperation(ctx, b.Token, created.OperationID); err != nil || unrelated.Admission.SemanticSHA256 == "" {
		t.Fatalf("unrelated admission erased: %+v %v", unrelated, err)
	}
}
