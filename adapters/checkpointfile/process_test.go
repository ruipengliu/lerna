package checkpointfile_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"lerna/adapters/checkpointfile"
)

var oldCheckpoint = []byte(`{"operation":"original","digest":"old"}`)
var newCheckpoint = []byte(`{"operation":"original","format":2}`)

func TestProcessExitKeepsOriginalOrCommittedCheckpoint(t *testing.T) {
	for _, phase := range []string{"before", "after"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			root := t.TempDir()
			path := filepath.Join(root, "checkpoint.json")
			if err := os.WriteFile(path, oldCheckpoint, 0600); err != nil {
				t.Fatal(err)
			}
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			child := exec.CommandContext(ctx, executable, "-test.run=^TestCheckpointReplaceCrashProbe$")
			child.Env = append(os.Environ(), "HARNESS_CHECKPOINT_REPLACE_ROOT="+root, "HARNESS_CHECKPOINT_REPLACE_PHASE="+phase)
			output, err := child.CombinedOutput()
			var exited *exec.ExitError
			if !errors.As(err, &exited) || exited.ExitCode() != 77 {
				t.Fatalf("process exit: %v %s", err, output)
			}
			got, err := os.ReadFile(path)
			expected := oldCheckpoint
			if phase == "after" {
				expected = newCheckpoint
			}
			if err != nil || !bytes.Equal(got, expected) {
				t.Fatal("wrong commit boundary")
			}
			store, err := checkpointfile.Open(root)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			if err = store.Replace(ctx, "checkpoint.json", oldCheckpoint, newCheckpoint); err != nil {
				t.Fatal(err)
			}
			got, err = os.ReadFile(path)
			if err != nil || !bytes.Equal(got, newCheckpoint) {
				t.Fatal("recovery lost original operation")
			}
		})
	}
}
func TestCheckpointReplaceCrashProbe(t *testing.T) {
	root := os.Getenv("HARNESS_CHECKPOINT_REPLACE_ROOT")
	if root == "" {
		t.Skip("subprocess probe")
	}
	phase := os.Getenv("HARNESS_CHECKPOINT_REPLACE_PHASE")
	if phase != "before" && phase != "after" {
		t.Fatal("unknown phase")
	}
	store, err := checkpointfile.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if phase == "after" {
		if err = store.Replace(context.Background(), "checkpoint.json", oldCheckpoint, newCheckpoint); err != nil {
			t.Fatal(err)
		}
	}
	os.Exit(77)
}
