package memorycheck

import (
	"context"
	memorycleanup "lerna/adapters/memory/cleanup"
	"lerna/cleanup"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestCleanupWorkerContinuesIndependentConsumerAndStops(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	f, err := newFixture(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(f.config.Root)
	defer f.close()
	if err = f.put(ctx); err != nil {
		t.Fatal(err)
	}
	id, err := f.auth.NewOperation(ctx, f.config.Token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.client.Exchange(ctx, &wire.MemoryRequest{Method: "DELETE", Delete: &wire.MemoryDelete{OperationId: id, Ref: f.write(f.config.WriteID, 0, "concise").Write.Ref, ExpectedRevision: 1, Purpose: "assist"}}); err != nil {
		t.Fatal(err)
	}
	sink, err := memorycleanup.NewAdmissions(f.store, f.auth)
	if err != nil {
		t.Fatal(err)
	}
	binding := memory.ConsumerBinding{Namespace: "local", Collection: "personal", Consumer: "worker-admissions", ConfigSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	consumer, err := memory.NewSourceConsumer(f.store, f.store, sink, memory.ConsumerConfig{Binding: binding, Batch: 16, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	observed := make(chan struct{}, 2)
	worker, err := cleanup.New([]cleanup.Job{
		{Name: "unavailable-target", Run: func(context.Context) error { calls.Add(1); return memory.Unavailable }},
		{Name: "admissions", Run: func(ctx context.Context) error {
			_, err := consumer.Run(ctx)
			if err == nil {
				select {
				case observed <- struct{}{}:
				default:
				}
			}
			return err
		}},
	}, cleanup.Config{Interval: 10 * time.Millisecond, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	for range 2 {
		select {
		case <-observed:
		case <-ctx.Done():
			t.Fatal("independent consumer blocked")
		}
	}
	cancel()
	if err = <-done; err != context.Canceled {
		t.Fatalf("worker did not join cancellation: %v", err)
	}
	position, err := f.store.InspectConsumer(context.Background(), binding)
	if err != nil || position != 2 || calls.Load() < 2 {
		t.Fatalf("consumer progress/retry: %d %d %v", position, calls.Load(), err)
	}
	admission, err := f.auth.InspectMemoryOperation(context.Background(), f.config.Token, f.config.WriteID)
	if err != nil || admission.Admission.SemanticSHA256 != "" {
		t.Fatalf("worker did not erase comparison: %+v %v", admission, err)
	}
	status := worker.Status()
	if len(status) != 2 || status[0].Err != memory.Unavailable || status[1].Err != nil || status[1].State != "succeeded" {
		t.Fatalf("lost independent outcomes: %+v", status)
	}
}
