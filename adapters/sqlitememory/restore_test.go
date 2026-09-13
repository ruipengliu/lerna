package sqlitememory_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"lerna/adapters/sqlitememory"
	"lerna/memory"
)

func TestRestoreWaitsForRuntimeAndExcludesOrdinaryOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	runtime, err := sqlitememory.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	if restored, err := sqlitememory.OpenRestored(path); err != memory.Unavailable || restored != nil {
		if restored != nil {
			restored.Close()
		}
		t.Fatalf("restoration overlapped active runtime: %v", err)
	}
	if err = runtime.Close(); err != nil {
		t.Fatal(err)
	}
	if err = runtime.Close(); err != nil {
		t.Fatalf("repeated close changed the Store contract: %v", err)
	}
	restored, err := sqlitememory.OpenRestored(path)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if opened, err := sqlitememory.Open(path); err != memory.Unavailable || opened != nil {
		if opened != nil {
			opened.Close()
		}
		t.Fatalf("ordinary runtime overlapped restoration: %v", err)
	}
}

func TestRestoreQuarantineSurvivesProcessExit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "memory.db")
	original, err := sqlitememory.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = original.Close(); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.CommandContext(ctx, executable, "-test.run=^TestRestoreQuarantineCrashProbe$")
	child.Env = append(os.Environ(), "HARNESS_MEMORY_RESTORE_PROBE="+path)
	output, err := child.CombinedOutput()
	var exited *exec.ExitError
	if !errors.As(err, &exited) || exited.ExitCode() != 71 {
		t.Fatalf("restore process: %v %s", err, output)
	}
	opened, err := sqlitememory.Open(path)
	if opened != nil {
		opened.Close()
		t.Fatal("process exit released quarantined data")
	}
	if err != memory.Quarantined {
		t.Fatalf("durable quarantine: %v", err)
	}
	restored, err := sqlitememory.OpenRestored(path)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if status, err := restored.Status(ctx); err != nil || status != "quarantined" {
		t.Fatalf("restart state: %s %v", status, err)
	}
}

func TestRestoreQuarantineCrashProbe(t *testing.T) {
	path := os.Getenv("HARNESS_MEMORY_RESTORE_PROBE")
	if path == "" {
		t.Skip("subprocess probe")
	}
	restored, err := sqlitememory.OpenRestored(path)
	if err != nil {
		t.Fatal(err)
	}
	if status, err := restored.Status(context.Background()); err != nil || status != "quarantined" {
		t.Fatalf("quarantine commit: %s %v", status, err)
	}
	os.Exit(71)
}

func TestRestoredBackupCannotOpenWithoutCurrentAuthority(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	currentPath, backupPath := filepath.Join(root, "current.db"), filepath.Join(root, "backup.db")
	current, err := sqlitememory.Open(currentPath)
	if err != nil {
		t.Fatal(err)
	}
	ref := memory.Ref{Namespace: "local", Collection: "personal", Key: "preference"}
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte("original operation")))
	_, err = current.Commit(ctx, memory.Change{OperationID: "put-original", Subject: "operator", SemanticSHA256: digest, Record: memory.Revision{Ref: ref, Revision: 1, Document: []byte(`{"private":"synthetic old preference"}`)}})
	if err != nil {
		current.Close()
		t.Fatal(err)
	}
	if err = current.Close(); err != nil {
		t.Fatal(err)
	}
	// Copy a closed, checkpointed real database before the actual deletion.
	image, err := os.ReadFile(currentPath)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(backupPath, image, 0600); err != nil {
		t.Fatal(err)
	}
	current, err = sqlitememory.Open(currentPath)
	if err != nil {
		t.Fatal(err)
	}
	defer current.Close()
	_, err = current.Delete(ctx, memory.Deletion{OperationID: "delete-original", Subject: "operator", SemanticSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("delete metadata"))), Ref: ref, Expected: 1})
	if err != nil {
		t.Fatal(err)
	}
	restored, err := sqlitememory.OpenRestored(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	status, err := restored.Status(ctx)
	if err != nil || status != "quarantined" {
		restored.Close()
		t.Fatalf("restore state: %s %v", status, err)
	}
	if err = restored.Close(); err != nil {
		t.Fatal(err)
	}
	// Neither reopening nor the backup's own historical state establishes trust.
	for range 2 {
		store, err := sqlitememory.Open(backupPath)
		if store != nil {
			old, readErr := store.Read(ctx, ref, 1)
			store.Close()
			if readErr == nil && len(old.Document) != 0 {
				t.Fatal("old backup released the deleted body")
			}
			t.Fatal("old backup exposed an ordinary Memory store")
		}
		if err != memory.Quarantined {
			t.Fatalf("quarantine disappeared on reopen: %v", err)
		}
	}
	restored, err = sqlitememory.OpenRestored(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if status, err = restored.Status(ctx); err != nil || status != "quarantined" {
		t.Fatalf("old state self-released: %s %v", status, err)
	}
}
