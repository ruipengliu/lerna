package contextassembly_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sqlitecontext "lerna/adapters/context/sqlite"
	memorycleanup "lerna/adapters/memory/cleanup"
	sqlitememory "lerna/adapters/memory/sqlite"
	"lerna/contextassembly"
	"lerna/memory"
)

func cleanupConsumerConfig() memory.ConsumerConfig {
	return memory.ConsumerConfig{Binding: memory.ConsumerBinding{Namespace: "local", Collection: "personal", Consumer: "contexts", ConfigSHA256: strings.Repeat("a", 64)}, Batch: 2, Timeout: 5 * time.Second}
}

// Only the acknowledgment boundary is instrumented. Event storage, applied
// position and context effects all use their real persistent implementations.
type crashAcknowledgment struct {
	memory.ConsumerProgress
	phase string
}

func (p crashAcknowledgment) AckEvent(ctx context.Context, b memory.ConsumerBinding, expected, next uint64) error {
	if p.phase == "before-ack" {
		os.Exit(75)
	}
	if err := p.ConsumerProgress.AckEvent(ctx, b, expected, next); err != nil {
		return err
	}
	os.Exit(75)
	return nil
}

func TestProcessExitReconcilesDeletionCleanupAcknowledgment(t *testing.T) {
	for _, phase := range []string{"after-delete", "before-ack", "after-ack"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			root := t.TempDir()
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			child := exec.CommandContext(ctx, executable, "-test.run=^TestCleanupAcknowledgmentCrashProbe$")
			child.Env = append(os.Environ(), "HARNESS_CLEANUP_ROOT="+root, "HARNESS_CLEANUP_PHASE="+phase)
			output, err := child.CombinedOutput()
			var exited *exec.ExitError
			if !errors.As(err, &exited) || exited.ExitCode() != 75 {
				t.Fatalf("child boundary: %v %s", err, output)
			}
			store, err := sqlitememory.Open(filepath.Join(root, "memory.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			contexts, err := sqlitecontext.Open(filepath.Join(root, "context.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer contexts.Close()
			ref := memory.Ref{Namespace: "local", Collection: "personal", Key: "style"}
			if revision, err := store.Head(ctx, ref); err != nil || revision != 2 {
				t.Fatalf("deletion revision: %d %v", revision, err)
			}
			if _, err := store.Read(ctx, ref, 1); err != memory.Missing {
				t.Fatalf("deleted body restored: %v", err)
			}
			config := cleanupConsumerConfig()
			expected := uint64(1)
			if phase == "after-ack" {
				expected = 2
			}
			if position, err := store.BindConsumer(ctx, config.Binding); err != nil || position != expected {
				t.Fatalf("durable acknowledgment: %d %v", position, err)
			}
			_, err = contexts.Read(ctx, processRequest().Key)
			if phase == "after-delete" && err != nil || phase != "after-delete" && err != contextassembly.Invalidated {
				t.Fatalf("cleanup before acknowledgment: %v", err)
			}
			sink, err := memorycleanup.NewContexts(contexts)
			if err != nil {
				t.Fatal(err)
			}
			consumer, err := memory.NewSourceConsumer(store, store, sink, config)
			if err != nil {
				t.Fatal(err)
			}
			result, err := consumer.Run(ctx)
			if err != nil || result.Position != 2 || result.Applied != int(2-expected) || result.Pending {
				t.Fatalf("recovery: %+v %v", result, err)
			}
			if _, err := contexts.Read(ctx, processRequest().Key); err != contextassembly.Invalidated {
				t.Fatalf("recovered cleanup disclosed body: %v", err)
			}
			if repeat, err := consumer.Run(ctx); err != nil || repeat.Applied != 0 || repeat.Position != 2 {
				t.Fatalf("replayed confirmed event: %+v %v", repeat, err)
			}
		})
	}
}

func TestCleanupAcknowledgmentCrashProbe(t *testing.T) {
	root := os.Getenv("HARNESS_CLEANUP_ROOT")
	if root == "" {
		t.Skip("subprocess probe")
	}
	phase := os.Getenv("HARNESS_CLEANUP_PHASE")
	if phase != "after-delete" && phase != "before-ack" && phase != "after-ack" {
		t.Fatal("unknown phase")
	}
	ctx := context.Background()
	store, err := sqlitememory.Open(filepath.Join(root, "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	ref := memory.Ref{Namespace: "local", Collection: "personal", Key: "style"}
	if _, err = store.Commit(ctx, memory.Change{OperationID: "put", Subject: "alice", SemanticSHA256: strings.Repeat("b", 64), Record: memory.Revision{Ref: ref, Revision: 1, Document: []byte(`{"text":"public concise preference"}`)}}); err != nil {
		t.Fatal(err)
	}
	contexts, err := sqlitecontext.Open(filepath.Join(root, "context.db"))
	if err != nil {
		t.Fatal(err)
	}
	assembly, err := contextassembly.New(contexts, facts{}, &sources{text: processBody, retain: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = assembly.Assemble(ctx, processRequest()); err != nil {
		t.Fatal(err)
	}
	sink, err := memorycleanup.NewContexts(contexts)
	if err != nil {
		t.Fatal(err)
	}
	consumer, err := memory.NewSourceConsumer(store, store, sink, cleanupConsumerConfig())
	if err != nil {
		t.Fatal(err)
	}
	if result, err := consumer.Run(ctx); err != nil || result.Position != 1 {
		t.Fatalf("created event acknowledgment: %+v %v", result, err)
	}
	if _, err = store.Delete(ctx, memory.Deletion{OperationID: "delete", Subject: "alice", SemanticSHA256: strings.Repeat("c", 64), Ref: ref, Expected: 1}); err != nil {
		t.Fatal(err)
	}
	if phase == "after-delete" {
		os.Exit(75)
	}
	consumer, err = memory.NewSourceConsumer(store, crashAcknowledgment{store, phase}, sink, cleanupConsumerConfig())
	if err != nil {
		t.Fatal(err)
	}
	result, err := consumer.Run(ctx)
	t.Fatalf("did not reach acknowledgment boundary: %+v %v", result, err)
}
