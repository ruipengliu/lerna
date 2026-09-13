package cleanup_test

import (
	"context"
	"lerna/cleanup"
	"testing"
	"time"
)

func TestWorkerSerializesAndJoinsActiveSteps(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	entered := make(chan struct{})
	exited := make(chan struct{})
	worker, err := cleanup.New([]cleanup.Job{{Name: "active", Run: func(ctx context.Context) error { close(entered); <-ctx.Done(); close(exited); return ctx.Err() }}}, cleanup.Config{Interval: time.Second, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	<-entered
	if _, err = worker.Sweep(ctx); err != cleanup.Busy {
		t.Fatalf("overlapping sweep: %v", err)
	}
	cancel()
	if err = <-done; err != context.Canceled {
		t.Fatalf("cancellation: %v", err)
	}
	select {
	case <-exited:
	default:
		t.Fatal("worker detached active step")
	}
}
func TestExpiredStepDoesNotStarveNextStep(t *testing.T) {
	called := false
	worker, err := cleanup.New([]cleanup.Job{
		{Name: "timeout", Run: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }},
		{Name: "next", Run: func(context.Context) error { called = true; return nil }},
	}, cleanup.Config{Interval: time.Second, Timeout: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	results, err := worker.Sweep(context.Background())
	if err != nil || !called || results[0].Err != context.DeadlineExceeded || results[1].State != "succeeded" {
		t.Fatalf("starved after timeout: %+v %v", results, err)
	}
}

func TestManagedCleanupCloseJoinsAndIsRepeatable(t *testing.T) {
	entered := make(chan struct{})
	exited := make(chan struct{})
	running, err := cleanup.Start([]cleanup.Job{{Name: "active", Run: func(ctx context.Context) error { close(entered); <-ctx.Done(); close(exited); return ctx.Err() }}}, cleanup.Config{Interval: time.Second, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	if err = running.Close(); err != nil {
		t.Fatal(err)
	}
	if err = running.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-exited:
	default:
		t.Fatal("managed cleanup left a job active")
	}
}
