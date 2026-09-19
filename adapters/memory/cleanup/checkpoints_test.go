package cleanup_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"lerna/adapters/context/checkpointfile"
	memorycleanup "lerna/adapters/memory/cleanup"
	sqlitememory "lerna/adapters/memory/sqlite"
	"lerna/memory"
)

var legacyCheckpoint = []byte(`{"format":1,"comparison":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`)
var cleanCheckpoint = []byte(`{"format":2}`)
var checkpointBinding = memory.ConsumerBinding{Namespace: "local", Collection: "personal", Consumer: "fixed-test-checkpoint", ConfigSHA256: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}

type checkpointProgressPort interface {
	memory.ConsumerProgress
	memory.ConsumerInspection
}

// The fixture is an owner-supplied maintenance boundary over the real CAS file
// adapter. Full original-task validation is exercised by the actual host tests.
func checkpointWorker(t *testing.T, root string, store *sqlitememory.Store, progress checkpointProgressPort) *memorycleanup.Checkpoints {
	t.Helper()
	files, err := checkpointfile.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { files.Close() })
	worker, err := memorycleanup.NewCheckpoints(store, progress, memorycleanup.CheckpointFiles{
		Maintain: func(ctx context.Context) error {
			raw, err := os.ReadFile(filepath.Join(root, "checkpoint.json"))
			if err != nil {
				return err
			}
			if bytes.Equal(raw, cleanCheckpoint) {
				return nil
			}
			if !bytes.Equal(raw, legacyCheckpoint) {
				return memory.IdentityConflict
			}
			return files.Replace(ctx, "checkpoint.json", legacyCheckpoint, cleanCheckpoint)
		},
		Inspect: func(ctx context.Context) (bool, error) {
			if err := ctx.Err(); err != nil {
				return false, err
			}
			raw, err := os.ReadFile(filepath.Join(root, "checkpoint.json"))
			return bytes.Equal(raw, cleanCheckpoint), err
		},
	}, memory.ConsumerConfig{Binding: checkpointBinding, Batch: 16, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return worker
}

func prepareCheckpointSource(t *testing.T, root string) {
	t.Helper()
	store, err := sqlitememory.Open(filepath.Join(root, "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ref := memory.Ref{Namespace: "local", Collection: "personal", Key: "preference"}
	_, err = store.Commit(context.Background(), memory.Change{OperationID: "put", Subject: "operator", SemanticSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Record: memory.Revision{Ref: ref, Revision: 1, Document: []byte(`{"text":"synthetic"}`)}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Delete(context.Background(), memory.Deletion{OperationID: "delete", Subject: "operator", SemanticSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Ref: ref, Expected: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "checkpoint.json"), legacyCheckpoint, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestCheckpointConfirmationSurvivesProcessExit(t *testing.T) {
	for _, phase := range []string{"before", "after"} {
		t.Run(phase, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Chmod(root, 0700); err != nil {
				t.Fatal(err)
			}
			prepareCheckpointSource(t, root)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestCheckpointConfirmationCrashProbe$")
			cmd.Env = append(os.Environ(), "HARNESS_CHECKPOINT_CONFIRM_ROOT="+root, "HARNESS_CHECKPOINT_CONFIRM_PHASE="+phase)
			raw, err := cmd.CombinedOutput()
			var exited *exec.ExitError
			if !errors.As(err, &exited) || exited.ExitCode() != 71 {
				t.Fatalf("process %s: %v %s", phase, err, raw)
			}
			store, err := sqlitememory.Open(filepath.Join(root, "memory.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			worker := checkpointWorker(t, root, store, store)
			position, err := worker.InspectConsumer(ctx, checkpointBinding)
			expected := uint64(0)
			if phase == "after" {
				expected = 1
			}
			if err != nil || position != expected {
				t.Fatalf("unknown confirmation treated as complete: %d %v", position, err)
			}
			if result, err := worker.Run(ctx); err != nil || result.Position != 2 {
				t.Fatalf("original event resume: %+v %v", result, err)
			}
			if position, err = worker.InspectConsumer(ctx, checkpointBinding); err != nil || position != 2 {
				t.Fatalf("completed cleanup not confirmed: %d %v", position, err)
			}
			// A malformed late file prevents a formerly applied cursor from claiming
			// completion. The report itself must not repair or acknowledge it.
			if err = os.WriteFile(filepath.Join(root, "checkpoint.json"), []byte(`{"unknown":"comparison"}`), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err = worker.InspectConsumer(ctx, checkpointBinding); err != memory.Missing {
				t.Fatalf("late file reused completion: %v", err)
			}
			if _, err = worker.Run(ctx); err != memory.IdentityConflict {
				t.Fatalf("unknown legacy contents accepted: %v", err)
			}
			if stored, err := store.InspectConsumer(ctx, checkpointBinding); err != nil || stored != 2 {
				t.Fatalf("failed maintenance fabricated progress: %d %v", stored, err)
			}
			if err = os.WriteFile(filepath.Join(root, "checkpoint.json"), legacyCheckpoint, 0600); err != nil {
				t.Fatal(err)
			}
			if result, err := worker.Run(ctx); err != nil || result.Position != 2 || result.Applied != 0 {
				t.Fatalf("late file replaced original events: %+v %v", result, err)
			}
			if position, err = worker.InspectConsumer(ctx, checkpointBinding); err != nil || position != 2 {
				t.Fatalf("repaired late file not confirmed: %d %v", position, err)
			}
		})
	}
}

type checkpointCrashProgress struct {
	*sqlitememory.Store
	phase string
}

func (p checkpointCrashProgress) AckEvent(ctx context.Context, b memory.ConsumerBinding, old, next uint64) error {
	if p.phase == "before" {
		os.Exit(71)
	}
	if err := p.Store.AckEvent(ctx, b, old, next); err != nil {
		return err
	}
	os.Exit(71)
	return nil
}
func TestCheckpointConfirmationCrashProbe(t *testing.T) {
	root := os.Getenv("HARNESS_CHECKPOINT_CONFIRM_ROOT")
	if root == "" {
		t.Skip("subprocess probe")
	}
	store, err := sqlitememory.Open(filepath.Join(root, "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	worker := checkpointWorker(t, root, store, checkpointCrashProgress{store, os.Getenv("HARNESS_CHECKPOINT_CONFIRM_PHASE")})
	if _, err = worker.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Fatal("did not reach actual confirmation boundary")
}

type checkpointReadMutation struct {
	*sqlitememory.Store
	root string
}

func (p checkpointReadMutation) InspectConsumer(ctx context.Context, b memory.ConsumerBinding) (uint64, error) {
	position, err := p.Store.InspectConsumer(ctx, b)
	if err != nil {
		return 0, err
	}
	if err = os.WriteFile(filepath.Join(p.root, "checkpoint.json"), legacyCheckpoint, 0600); err != nil {
		return 0, err
	}
	return position, nil
}

func TestCheckpointConfirmationRechecksFileAfterReadingPosition(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	prepareCheckpointSource(t, root)
	store, err := sqlitememory.Open(filepath.Join(root, "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	worker := checkpointWorker(t, root, store, store)
	if _, err = worker.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	raced := checkpointWorker(t, root, store, checkpointReadMutation{store, root})
	if position, err := raced.InspectConsumer(context.Background(), checkpointBinding); err != memory.Missing || position != 0 {
		t.Fatalf("file arriving during inspection reused completion: %d %v", position, err)
	}
}
