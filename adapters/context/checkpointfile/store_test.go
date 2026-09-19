package checkpointfile_test

import (
	"bytes"
	"context"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"testing"
	"time"

	"lerna/adapters/context/checkpointfile"
)

func TestNonRegularCheckpointCannotBlockMaintenance(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "checkpoint.json")
	if err := unix.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := checkpointfile.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- store.Replace(ctx, "checkpoint.json", []byte(`{"old":true}`), []byte(`{"new":true}`)) }()
	select {
	case err = <-done:
		if err != checkpointfile.Invalid {
			t.Fatalf("non-regular checkpoint: %v", err)
		}
	case <-time.After(time.Second):
		// Unblock the real FIFO on failure so the test does not leak a worker.
		peer, e := os.OpenFile(path, os.O_RDWR|unix.O_NONBLOCK, 0)
		if e != nil {
			t.Fatal(e)
		}
		defer peer.Close()
		<-done
		t.Fatal("non-regular checkpoint blocked beyond cancellation")
	}
}

func TestHardLinkedCheckpointIsNotReportedAsCleaned(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "checkpoint.json")
	original := []byte(`{"operation":"original","digest":"old"}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(path, filepath.Join(root, "copy.json")); err != nil {
		t.Fatal(err)
	}
	store, err := checkpointfile.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.Replace(context.Background(), "checkpoint.json", original, []byte(`{"operation":"original","format":2}`)); err != checkpointfile.Invalid {
		t.Fatalf("untracked hard link accepted: %v", err)
	}
	for _, name := range []string{"checkpoint.json", "copy.json"} {
		got, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || !bytes.Equal(got, original) {
			t.Fatal("rejected file set changed")
		}
	}
}

func TestReplacementPreservesOriginalIdentityAndRejectsStaleWriter(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "checkpoint.json")
	original := []byte(`{"operation":"original","digest":"old"}`)
	cleaned := []byte(`{"operation":"original","format":2}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := checkpointfile.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.Replace(ctx, "checkpoint.json", original, cleaned); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, cleaned) {
		t.Fatalf("replacement: %s %v", got, err)
	}
	if err = store.Replace(ctx, "checkpoint.json", original, cleaned); err != nil {
		t.Fatalf("unknown outcome retry: %v", err)
	}
	if err = store.Replace(ctx, "checkpoint.json", original, []byte(`{"operation":"replacement"}`)); err != checkpointfile.Conflict {
		t.Fatalf("stale writer replaced original decision: %v", err)
	}
	got, err = os.ReadFile(path)
	if err != nil || !bytes.Equal(got, cleaned) {
		t.Fatal("stale writer changed file")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("replacement lost private mode")
	}
}

func TestConcurrentWritersHaveOneCheckpointWinner(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "checkpoint.json")
	original := []byte(`{"operation":"original"}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	a, err := checkpointfile.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := checkpointfile.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	start := make(chan struct{})
	results := make(chan error, 2)
	first := []byte(`{"operation":"original","version":"a"}`)
	second := []byte(`{"operation":"original","version":"b"}`)
	go func() { <-start; results <- a.Replace(context.Background(), "checkpoint.json", original, first) }()
	go func() { <-start; results <- b.Replace(context.Background(), "checkpoint.json", original, second) }()
	close(start)
	success, conflict := 0, 0
	for range 2 {
		switch err := <-results; err {
		case nil:
			success++
		case checkpointfile.Conflict:
			conflict++
		default:
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("writers: success=%d conflict=%d", success, conflict)
	}
	got, err := os.ReadFile(path)
	if err != nil || (!bytes.Equal(got, first) && !bytes.Equal(got, second)) {
		t.Fatal("partial checkpoint published")
	}
}

func TestPrivateFileBoundaryRejectsEscapeAndPublicPayload(t *testing.T) {
	root := t.TempDir()
	original := []byte(`{"original":true}`)
	cleaned := []byte(`{"cleaned":true}`)
	path := filepath.Join(root, "checkpoint.json")
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	store, err := checkpointfile.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.Replace(context.Background(), "checkpoint.json", original, cleaned); err != checkpointfile.Invalid {
		t.Fatalf("public checkpoint accepted: %v", err)
	}
	if err = store.Replace(context.Background(), "../checkpoint.json", original, cleaned); err != checkpointfile.Invalid {
		t.Fatalf("escaped root: %v", err)
	}
	if err = os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = store.Replace(ctx, "checkpoint.json", original, cleaned); err == nil {
		t.Fatal("cancelled replacement accepted")
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, original) {
		t.Fatal("rejection changed original file")
	}
	if err = os.Symlink(path, filepath.Join(root, "link.json")); err != nil {
		t.Fatal(err)
	}
	if err = store.Replace(context.Background(), "link.json", original, cleaned); err == nil {
		t.Fatal("followed symlink")
	}
}
