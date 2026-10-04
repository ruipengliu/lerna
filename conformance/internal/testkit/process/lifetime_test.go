package process

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"
)

func TestProcessLifecycleChild(t *testing.T) {
	if os.Getenv("LERNA_PROCESS_LIFETIME_CHILD") != "1" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	pipes, err := OpenInherited()
	if pipes != nil {
		defer func() {
			if e := pipes.Close(); e != nil {
				t.Error(e)
			}
		}()
	}
	if err != nil {
		t.Fatal(err)
	}
	var release struct{ Stop bool }
	if err = pipes.Receive(ctx, &release); err != nil && err != io.EOF {
		t.Fatal(err)
	}
}

func TestChildDoesNotConfirmCleanupWhileStartIsPublishingHandle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	child, err := New(ctx, "TestProcessLifecycleChild", "LERNA_PROCESS_LIFETIME_CHILD=1")
	if child != nil {
		defer func() {
			cleanup, stop := context.WithTimeout(context.Background(), time.Second)
			defer stop()
			_, e := child.Stop(cleanup)
			if e != nil {
				t.Error(e)
			}
		}()
	}
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	child.afterStart = func() {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
		}
	}
	result := make(chan error, 1)
	go func() { result <- child.Start() }()
	select {
	case <-started:
	case <-ctx.Done():
		close(release)
		<-result
		t.Fatal("actual OS Start checkpoint missing")
	}
	bounded, stop := context.WithTimeout(ctx, 20*time.Millisecond)
	cleanupConfirmed, cleanupErr := child.Stop(bounded)
	stop()
	bounded, stop = context.WithTimeout(ctx, 20*time.Millisecond)
	exitConfirmed, waitErr := child.Wait(bounded)
	stop()
	close(release)
	select {
	case err = <-result:
	case <-ctx.Done():
		t.Fatal("actual Start did not join")
	}
	// Actual child/Wait/FD cleanup completes even on the old false-confirmation
	// path; no storage scope is created or guessed from this mechanical failure.
	cleanup, finish := context.WithTimeout(context.Background(), time.Second)
	defer finish()
	done, finalErr := child.Stop(cleanup)
	if !done {
		t.Error("actual child cleanup did not confirm", finalErr)
	}
	if err != nil {
		t.Error("actual Start failed", err)
	}
	if cleanupConfirmed || exitConfirmed || !errors.Is(cleanupErr, context.DeadlineExceeded) || !errors.Is(waitErr, context.DeadlineExceeded) {
		t.Fatalf("Start in progress falsely confirmed: cleanup=%t exit=%t cleanupCause=%v waitCause=%v", cleanupConfirmed, exitConfirmed, cleanupErr, waitErr)
	}
}

func TestChildCleanupBeforeStartClosesAdmission(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	child, err := New(ctx, "TestProcessLifecycleChild", "LERNA_PROCESS_LIFETIME_CHILD=1")
	if child != nil {
		defer func() {
			_, e := child.Stop(ctx)
			if e != nil {
				t.Error(e)
			}
		}()
	}
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := child.Stop(ctx)
	if !confirmed || err != nil {
		t.Fatal("never-started physical cleanup:", err)
	}
	if err = child.Start(); !errors.Is(err, ErrClosing) {
		t.Fatal("Start admitted after completed cleanup", err)
	}
}
