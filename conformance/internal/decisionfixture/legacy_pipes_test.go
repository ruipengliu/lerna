//go:build integration

package decisionfixture

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"testing"
)

func TestUpgradePipeAllocationRefusalRetainsBothOwnedEnds(t *testing.T) {
	child := exec.Command("unused-no-process-is-started")
	child.Stdout = io.Discard // Real StdoutPipe allocation refusal; no FD exhaustion.
	stdin, stdout, cleanup, err := openUpgradePipes(child)
	if err == nil || stdin == nil || stdout != nil {
		t.Fatal("wrong allocation refusal", err)
	}
	childEnd, ok := child.Stdin.(*os.File)
	if !ok {
		t.Fatal("actual stdin child end missing")
	}
	if cleanup == nil {
		// The test owns exactly these acquired ends even on the initial red path.
		closeErr := errors.Join(stdin.Close(), childEnd.Close())
		t.Fatal("allocation refusal lost owned pipe cleanup", closeErr)
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := stdin.Write([]byte("unstarted")); !errors.Is(err, os.ErrClosed) {
		t.Fatal("parent stdin end remained open", err)
	}
	if _, err := childEnd.Read(make([]byte, 1)); !errors.Is(err, os.ErrClosed) {
		t.Fatal("child stdin end remained open", err)
	}
	if err := cleanup(); err != nil {
		t.Fatal("repeated cleanup", err)
	}
}

func TestUpgradeUnstartedPipesCloseAllFourEnds(t *testing.T) {
	child := exec.Command("unused-no-process-is-started")
	stdin, stdout, cleanup, err := openUpgradePipes(child)
	if err != nil {
		t.Fatal(err)
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := stdin.Write([]byte("unstarted")); !errors.Is(err, os.ErrClosed) {
		t.Fatal(err)
	}
	if _, err := stdout.Read(make([]byte, 1)); !errors.Is(err, os.ErrClosed) {
		t.Fatal(err)
	}
	if _, err := child.Stdin.(*os.File).Read(make([]byte, 1)); !errors.Is(err, os.ErrClosed) {
		t.Fatal(err)
	}
	if _, err := child.Stdout.(*os.File).Write([]byte("unstarted")); !errors.Is(err, os.ErrClosed) {
		t.Fatal(err)
	}
}

func TestUpgradePrestartCleanupPreservesFirstPipeCloseCause(t *testing.T) {
	child := exec.Command("unused-no-process-is-started")
	stdin, _, cleanup, err := openUpgradePipes(child)
	if err != nil {
		t.Fatal(err)
	}
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	// A real already-closed parent end is a Close cause, not a simulated native
	// failure. Cleanup must still close the other three exact owned ends.
	for range 2 {
		if err := cleanup(); !errors.Is(err, os.ErrClosed) {
			t.Fatal("pipe Close cause lost", err)
		}
	}
	if _, err := child.Stdout.(*os.File).Write([]byte("unstarted")); !errors.Is(err, os.ErrClosed) {
		t.Fatal("other owned end remained open", err)
	}
}
