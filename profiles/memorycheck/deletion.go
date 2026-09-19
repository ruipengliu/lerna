package memorycheck

import (
	"context"
	"crypto/sha256"
	"fmt"
	"google.golang.org/protobuf/proto"
	memorycleanup "lerna/adapters/memory/cleanup"
	memorylocal "lerna/adapters/memory/local"
	"lerna/cleanup"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"lerna/sdk"
	"os"
	"time"
)

// CheckDeletion exercises the SDK and actual local cleanup. The report covers
// admission comparison cleanup only; no remote or derived consumer is implied.
func CheckDeletion(ctx context.Context) (*wire.MemoryDeletionState, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	f, err := newFixture(ctx)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(f.config.Root)
	defer func() { f.close() }()
	hash := sha256.Sum256([]byte("memory-deletion-profile:admission-comparisons:v1"))
	binding := memory.ConsumerBinding{Namespace: "local", Collection: "personal", Consumer: "admission-comparisons", ConfigSHA256: fmt.Sprintf("%x", hash)}
	configure := func() error {
		reporter, e := memory.NewDeletionReporter(f.store, f.store, []memory.CleanupTarget{{Name: "admission-comparisons", Dimension: memory.LocalCleanup, Binding: &binding}})
		if e != nil {
			return e
		}
		service, e := memory.New(f.store, f.authority, f.schemas, clock{}, memory.Config{Location: "device-a", Timeout: 5 * time.Second})
		if e != nil {
			return e
		}
		service, e = service.WithDeletionReporter(reporter)
		if e != nil {
			return e
		}
		reader, e := memory.NewReader(service, f.permits)
		if e != nil {
			return e
		}
		f.client = sdk.NewMemoryClient(memorylocal.Bind(service, reader, f.binding), "local")
		return nil
	}
	if err = configure(); err != nil {
		return nil, err
	}
	if err = f.put(ctx); err != nil {
		return nil, err
	}
	if _, err = f.read(ctx); err != nil {
		return nil, err
	}
	id, err := f.auth.NewOperation(ctx, f.config.Token)
	if err != nil {
		return nil, err
	}
	ref := f.write(f.config.WriteID, 0, "concise").Write.Ref
	deletion := &wire.MemoryRequest{Method: "DELETE", Delete: &wire.MemoryDelete{OperationId: id, Ref: ref, ExpectedRevision: 1, Purpose: "assist"}}
	first, err := f.client.Exchange(ctx, deletion)
	if err != nil {
		return nil, err
	}
	repeat, err := f.client.Exchange(ctx, deletion)
	if err != nil || !proto.Equal(first.GetReceipt(), repeat.GetReceipt()) {
		return nil, fmt.Errorf("deletion receipt did not replay: %v", err)
	}
	if _, err = f.read(ctx); err == nil {
		return nil, fmt.Errorf("original query disclosed deleted memory")
	}
	query := &wire.MemoryRequest{Method: "DELETION_STATUS", DeletionQuery: &wire.MemoryDeletionQuery{OperationId: id, Ref: ref, Purpose: "assist"}}
	pending, err := f.client.Exchange(ctx, query)
	if err != nil {
		return nil, err
	}
	if pending.Deletion.Report.Local[0].State != "pending" {
		return nil, fmt.Errorf("unconfirmed cleanup reported applied")
	}
	sink, err := memorycleanup.NewAdmissions(f.store, f.auth)
	if err != nil {
		return nil, err
	}
	consumer, err := memory.NewSourceConsumer(f.store, f.store, sink, memory.ConsumerConfig{Binding: binding, Batch: 16, Timeout: 5 * time.Second})
	if err != nil {
		return nil, err
	}
	var progress memory.ConsumerResult
	worker, err := cleanup.New([]cleanup.Job{{Name: binding.Consumer, Run: func(ctx context.Context) error {
		var e error
		progress, e = consumer.Run(ctx)
		return e
	}}}, cleanup.Config{Interval: time.Second, Timeout: 5 * time.Second})
	if err != nil {
		return nil, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		results, e := worker.Sweep(ctx)
		if e != nil || len(results) != 1 || results[0].State != "succeeded" || progress.Position != 2 || progress.Pending || attempt == 1 && progress.Applied != 0 {
			return nil, fmt.Errorf("scheduled cleanup incomplete: %+v %+v %v", results, progress, e)
		}
	}
	config := f.config
	f.close()
	f, err = openFixture(ctx, config)
	if err != nil {
		return nil, err
	}
	if err = configure(); err != nil {
		return nil, err
	}
	if _, err = f.client.Exchange(ctx, f.write(f.config.WriteID, 0, "concise")); err != memory.ReplayUnavailable {
		return nil, fmt.Errorf("erased original payload was resubmitted: %v", err)
	}
	original, err := f.client.Exchange(ctx, &wire.MemoryRequest{Method: "LOOKUP", OperationId: f.config.WriteID})
	if err != nil || original.GetOperation().GetContentAvailability() != "unavailable" || original.GetOperation().GetReceipt().GetSemanticSha256() != "" {
		return nil, fmt.Errorf("historical receipt leaked retained comparison: %v", err)
	}
	out, err := f.client.Exchange(ctx, query)
	if err != nil {
		return nil, err
	}
	r := out.Deletion.Report
	if out.Deletion.State != "committed" || r.Local[0].State != "applied" || r.Local[0].Position != 2 || r.Replicas[0].State != "not_covered" || r.DerivedArchives[0].State != "not_covered" {
		return nil, fmt.Errorf("reopened deletion scope inconsistent")
	}
	return out.Deletion, nil
}
