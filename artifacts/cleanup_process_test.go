package artifacts_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"lerna/artifacts"
	wire "lerna/gen/harness/v1"
)

type exitAfterRemoval struct{ artifacts.Blobs }

func (b exitAfterRemoval) Remove(ctx context.Context, key string) error {
	if err := b.Blobs.Remove(ctx, key); err != nil {
		return err
	}
	// The file is gone, but Clean has not committed this object's cleaned state.
	os.Exit(76)
	return nil
}

func TestProcessExitDuringFileCleanupBatchPreservesPendingWork(t *testing.T) {
	for _, mode := range []string{"range", "exact"} {
		t.Run(mode, func(t *testing.T) { checkFileCleanupProcess(t, mode == "exact") })
	}
}
func checkFileCleanupProcess(t *testing.T, exact bool) {
	h := newHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var refs []*wire.ContentRef
	for range 17 {
		body := []byte("Public derived body larger than the inline threshold")
		hash := sha256.Sum256(body)
		op, err := h.auth.NewOperation(ctx, h.token)
		if err != nil {
			t.Fatal(err)
		}
		out, err := h.content.Call(ctx, h.binding, &wire.ContentRequest{Method: "PUT", OperationId: op, Data: body, Spec: &wire.ContentSpec{Kind: "artifact", Resource: "root", Purpose: "research", Sources: []*wire.ContentSource{{Kind: "input", Key: "source", Revision: 1}}, AcquiredAt: h.now.Unix(), MediaType: "text/plain", Size: uint64(len(body)), Sha256: hex.EncodeToString(hash[:]), RetainUntil: h.now.Add(time.Minute).Unix()}})
		if err != nil {
			t.Fatal(err)
		}
		refs = append(refs, out.Record.Ref)
	}
	change := artifacts.SourceInvalidation{Namespace: "local", Kind: "input", Key: "source", ThroughRevision: 1}
	invalidate := func() (artifacts.InvalidationStatus, error) {
		if exact {
			return h.content.InvalidateRevision(ctx, artifacts.SourceRevisionInvalidation{Namespace: change.Namespace, Kind: change.Kind, Key: change.Key, Revision: change.ThroughRevision})
		}
		return h.content.InvalidateSource(ctx, change)
	}

	if status, err := invalidate(); err != nil || status.Cleaning != 17 {
		t.Fatalf("initial invalidation: %+v %v", status, err)
	}
	if err := h.st.Close(); err != nil {
		t.Fatal(err)
	}
	if err := h.blobs.Close(); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.CommandContext(ctx, executable, "-test.run=^TestFileCleanupCrashProbe$")
	child.Env = append(os.Environ(), "HARNESS_FILE_CLEANUP_ROOT="+h.root, "HARNESS_FILE_CLEANUP_TIME="+strconv.FormatInt(h.now.Unix(), 10))
	output, err := child.CombinedOutput()
	var exited *exec.ExitError
	if !errors.As(err, &exited) || exited.ExitCode() != 76 {
		t.Fatalf("file cleanup boundary: %v %s", err, output)
	}
	h.open(t)
	if status, err := invalidate(); err != nil || status.Cleaning != 17 || status.Cleaned != 0 {
		t.Fatalf("unknown cleanup incorrectly confirmed: %+v %v", status, err)
	}
	countFiles := func() int {
		unlock, err := h.blobs.Lock(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer unlock()
		files, err := h.blobs.List(ctx, 64)
		if err != nil {
			t.Fatal(err)
		}
		return len(files)
	}
	if count := countFiles(); count != 16 {
		t.Fatalf("file removal did not occur before exit: %d", count)
	}
	for _, ref := range refs {
		if _, err := h.content.Call(ctx, h.binding, &wire.ContentRequest{Method: "READ", Ref: ref, Purpose: "research", Limit: 1}); err == nil {
			t.Fatal("pending cleanup disclosed old body")
		}
	}
	if err := h.content.Clean(ctx); err != nil {
		t.Fatal(err)
	}
	if status, err := invalidate(); err != nil || status.Cleaned != 16 || status.Cleaning != 1 {
		t.Fatalf("bounded recovery batch: %+v %v", status, err)
	}
	if count := countFiles(); count != 1 {
		t.Fatalf("remaining body count: %d", count)
	}
	if err := h.content.Clean(ctx); err != nil {
		t.Fatal(err)
	}
	if status, err := invalidate(); err != nil || status.Cleaned != 17 || status.Cleaning != 0 {
		t.Fatalf("final cleanup: %+v %v", status, err)
	}
	if count := countFiles(); count != 0 {
		t.Fatalf("body files remain: %d", count)
	}
}

func TestFileCleanupCrashProbe(t *testing.T) {
	root := os.Getenv("HARNESS_FILE_CLEANUP_ROOT")
	if root == "" {
		t.Skip("subprocess probe")
	}
	at, err := strconv.ParseInt(os.Getenv("HARNESS_FILE_CLEANUP_TIME"), 10, 64)
	if err != nil || at <= 0 {
		t.Fatal("invalid clock")
	}
	h := &harness{root: root, now: time.Unix(at, 0)}
	h.open(t)
	content, err := artifacts.New(h.auth, exitAfterRemoval{h.blobs}, sources{}, artifacts.Config{Inline: 16, MaxObject: 1 << 20, MaxTotal: 2 << 20, MaxRecords: 32, MaxChunk: 65536, MaxFiles: 64, CleanupBatch: 16, Timeout: time.Second, Retention: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if err = content.Clean(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Fatal("file removal boundary not reached")
}
