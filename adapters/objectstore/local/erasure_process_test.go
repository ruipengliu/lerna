//go:build linux && integration

package local

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	d "github.com/ruipengliu/lerna/domain/content"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func erasureProcessLedger(record any) error {
	bytes, err := json.Marshal(record)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(os.Getenv("LERNA_TEST_OWNED_SCOPE_REGISTRY"), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(append(bytes, '\n'))
	return errors.Join(writeErr, file.Sync(), file.Close())
}
func erasureProcessMarker(root, name, value string) error {
	file, err := os.OpenFile(filepath.Join(root, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.WriteString(value)
	err = errors.Join(writeErr, file.Sync(), file.Close())
	if err != nil {
		return err
	}
	directory, err := os.Open(root)
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}
func erasureProcessIdentity(pid int) (string, string, error) {
	bytes, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", "", err
	}
	fields := strings.Fields(string(bytes)[strings.LastIndex(string(bytes), ")")+1:])
	if len(fields) < 20 {
		return "", "", errors.New("missing actual child identity")
	}
	return fields[19], fields[0], nil
}

// This child pauses only after actual native temp Sync returned. No synthetic
// error substitutes for the real filesystem operation or cross-process flock.
func TestContentObjectProcessHelper(t *testing.T) {
	mode := os.Getenv("LERNA_CONTENT_OBJECT_CHILD")
	if mode != "put-stop" && mode != "erase-stop" && mode != "late-put" {
		return
	}
	deadline, err := time.Parse(time.RFC3339Nano, os.Getenv("LERNA_CONTENT_OBJECT_DEADLINE"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	gate := os.NewFile(3, "owned-effect-gate")
	var release [1]byte
	_, readErr := io.ReadFull(gate, release[:])
	if err := errors.Join(readErr, gate.Close()); err != nil {
		t.Fatal(err)
	}
	root := os.Getenv("LERNA_CONTENT_OBJECT_ROOT")
	var identity d.ErasureIdentity
	if err := json.Unmarshal([]byte(os.Getenv("LERNA_CONTENT_OBJECT_IDENTITY")), &identity); err != nil {
		t.Fatal(err)
	}
	ops := actualNative()
	ops.syncFile = func(file *os.File) error {
		if err := file.Sync(); err != nil {
			return err
		}
		if mode == "put-stop" && filepath.Base(file.Name()) == identity.ObjectKey+".1.tmp" {
			if err := erasureProcessMarker(root, "process-ready", "actual-temp-sync-before-close-and-link"); err != nil {
				return err
			}
			return syscall.Kill(os.Getpid(), syscall.SIGSTOP)
		}
		return nil
	}
	store, err := open(root, ops)
	if err != nil {
		t.Fatal(err)
	}
	if store.Binding() != identity.Binding {
		t.Fatal(d.ErrHolderBinding)
	}
	switch mode {
	case "put-stop":
		if err = store.Put(ctx, identity.ObjectKey, identity.ObjectKey+".1.tmp", identity.Ref.Hash, 6, []byte("alpha\n")); err != nil {
			t.Fatal(err)
		}
	case "erase-stop":
		if _, err = store.FenceAndErase(ctx, identity, []string{identity.ObjectKey + ".1.tmp"}); err != nil {
			t.Fatal(err)
		}
		if err = erasureProcessMarker(root, "process-ready", "actual-durable-fence-before-root-close"); err != nil {
			t.Fatal(err)
		}
		if err = syscall.Kill(os.Getpid(), syscall.SIGSTOP); err != nil {
			t.Fatal(err)
		}
	case "late-put":
		if err = store.Put(ctx, identity.ObjectKey, identity.ObjectKey+".2.tmp", identity.Ref.Hash, 6, []byte("alpha\n")); !errors.Is(err, d.ErrBodySealed) {
			t.Fatalf("late child installed sealed key: %v", err)
		}
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	// Explicit positive logical Close ACK, distinct from later kernel Wait/exit.
	start, _, err := erasureProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	pgid, err := syscall.Getpgid(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if err = erasureProcessLedger(map[string]any{"event": "object_child_logical_close_ack", "root": root, "pid": os.Getpid(), "pgid": pgid, "starttime": start, "mode": mode, "actual_store_close_returned_nil": true}); err != nil {
		t.Fatal(err)
	}
	if err = erasureProcessMarker(root, "process-close-ack-"+mode, "actual-store-close-returned-nil"); err != nil {
		t.Fatal(err)
	}
}

type erasureChild struct {
	cmd           *exec.Cmd
	pid, pgid     int
	start         string
	joined        bool
	waitErr       error
	completionErr error
	root          string
	mode          string
}

func startErasureChild(t *testing.T, ctx context.Context, root string, identity d.ErasureIdentity, mode string) *erasureChild {
	t.Helper()
	deadline, _ := ctx.Deadline()
	if err := erasureProcessLedger(map[string]any{"event": "object_child_prestart_duty", "root": root, "identity": identity, "deadline": deadline, "mode": mode}); err != nil {
		t.Fatal(err)
	}
	readGate, writeGate, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(identity)
	if err != nil {
		t.Fatal(errors.Join(err, readGate.Close(), writeGate.Close()))
	}
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestContentObjectProcessHelper$", "-test.timeout=6s")
	cmd.Env = append(os.Environ(), "LERNA_CONTENT_OBJECT_CHILD="+mode, "LERNA_CONTENT_OBJECT_ROOT="+root, "LERNA_CONTENT_OBJECT_IDENTITY="+string(encoded), "LERNA_CONTENT_OBJECT_DEADLINE="+deadline.Format(time.RFC3339Nano))
	cmd.ExtraFiles = []*os.File{readGate}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.WaitDelay = 100 * time.Millisecond
	if err = cmd.Start(); err != nil {
		t.Fatal(errors.Join(err, readGate.Close(), writeGate.Close()))
	}
	child := &erasureChild{cmd: cmd, pid: cmd.Process.Pid, root: root, mode: mode}
	// Cleanup always stops and actually joins this original handle. A killed
	// holder's missing logical Close remains unknown; root is not released.
	t.Cleanup(func() {
		if !child.joined {
			_ = cmd.Process.Kill()
			_ = child.wait()
		}
	})
	child.pgid, err = syscall.Getpgid(child.pid)
	if err != nil || child.pgid != child.pid {
		t.Fatal(errors.Join(err, readGate.Close(), writeGate.Close()))
	}
	child.start, _, err = erasureProcessIdentity(child.pid)
	if err != nil {
		t.Fatal(errors.Join(err, readGate.Close(), writeGate.Close()))
	}
	if err = erasureProcessLedger(map[string]any{"event": "object_child_start_ack", "root": root, "pid": child.pid, "pgid": child.pgid, "starttime": child.start, "deadline": deadline, "mode": mode}); err != nil {
		t.Fatal(errors.Join(err, readGate.Close(), writeGate.Close()))
	}
	// Actual identity ACK is durable before any child effect is released.
	_, writeErr := writeGate.Write([]byte{1})
	if err = errors.Join(writeErr, readGate.Close(), writeGate.Close()); err != nil {
		t.Fatal(err)
	}
	return child
}
func (c *erasureChild) wait() error {
	if c.joined {
		return c.waitErr
	}
	c.waitErr = c.cmd.Wait()
	c.joined = true
	absent := errors.Is(syscall.Kill(-c.pgid, 0), syscall.ESRCH)
	ledgerErr := erasureProcessLedger(map[string]any{"event": "object_child_kernel_completion", "root": c.root, "pid": c.pid, "pgid": c.pgid, "starttime": c.start, "exit": c.cmd.ProcessState.ExitCode(), "group_absent": absent, "logical_close_inferred": false, "mode": c.mode})
	if !absent {
		ledgerErr = errors.Join(ledgerErr, errors.New("original child process group unconfirmed"))
	}
	c.completionErr = ledgerErr
	c.waitErr = errors.Join(c.waitErr, ledgerErr)
	return c.waitErr
}
func awaitErasureChildStop(ctx context.Context, child *erasureChild, phase string) error {
	for {
		start, state, err := erasureProcessIdentity(child.pid)
		if err != nil {
			return err
		}
		if start != child.start {
			return errors.New("original process identity changed")
		}
		marker, markerErr := os.ReadFile(filepath.Join(child.root, "process-ready"))
		if state == "T" && markerErr == nil && string(marker) == phase {
			return nil
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		timer := time.NewTimer(time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func TestCrossProcessPutFinishesBeforeOriginalErasure(t *testing.T) {
	root, confirmed := ownedLocalRoot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ref := v.ContentRef{Owner: v.OwnerRef{TenantID: "tenant-process", OwnerID: "content-owner"}, ContentID: "artifact", Version: "1", Hash: "sha256:b6a98d9ce9a2d9149288fa3df42d377c3e42737afdcdaf714e33c0a100b51060", MediaType: "text/plain", ByteLength: "6"}
	_, key, err := d.VersionIdentity(ref)
	if err != nil {
		t.Fatal(err)
	}
	store, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	identity := d.ErasureIdentity{Ref: ref, ObjectKey: key, HolderID: "primary", SealID: "process-original-seal", Binding: store.Binding()}
	child := startErasureChild(t, ctx, root, identity, "put-stop")
	if err = awaitErasureChildStop(ctx, child, "actual-temp-sync-before-close-and-link"); err != nil {
		t.Fatal(err)
	}
	temp, err := os.ReadFile(filepath.Join(root, key+".1.tmp"))
	if err != nil || string(temp) != "alpha\n" {
		t.Fatal("actual synced temp bytes missing", err)
	}
	blocked, stop := context.WithTimeout(ctx, 20*time.Millisecond)
	_, err = store.FenceAndErase(blocked, identity, []string{key + ".1.tmp"})
	stop()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("independent eraser bypassed actual child's held key flock: %v", err)
	}
	start, _, err := erasureProcessIdentity(child.pid)
	if err != nil || start != child.start {
		t.Fatal("cannot resume changed child", err)
	}
	if err = syscall.Kill(child.pid, syscall.SIGCONT); err != nil {
		t.Fatal(err)
	}
	if err = child.wait(); err != nil {
		t.Fatal(err)
	}
	proof, err := os.ReadFile(filepath.Join(root, "process-close-ack-put-stop"))
	if err != nil || string(proof) != "actual-store-close-returned-nil" {
		t.Fatal("child logical close unconfirmed", err)
	}
	actual, err := os.ReadFile(filepath.Join(root, key))
	if err != nil || string(actual) != "alpha\n" {
		t.Fatal("normal child installation did not complete", err)
	}
	if _, err = store.FenceAndErase(ctx, identity, []string{key + ".1.tmp"}); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := reopened.ObserveErasure(ctx, identity, "", 2)
	if err != nil || !observed.Erased || !observed.Fenced || len(observed.Residual) != 0 {
		t.Fatalf("independent postprocess erasure: %+v %v", observed, err)
	}
	if _, err = os.ReadFile(filepath.Join(root, key)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("final original body survived", err)
	}
	if err = reopened.Close(); err != nil {
		t.Fatal(err)
	}
	confirmed()
}

func TestCrossProcessClosedKeyRejectsLatePutAfterHolderDeath(t *testing.T) {
	root, _ := ownedLocalRoot(t) // killed original root holder never receives logical Close ACK
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ref := v.ContentRef{Owner: v.OwnerRef{TenantID: "tenant-process", OwnerID: "content-owner"}, ContentID: "artifact", Version: "1", Hash: "sha256:b6a98d9ce9a2d9149288fa3df42d377c3e42737afdcdaf714e33c0a100b51060", MediaType: "text/plain", ByteLength: "6"}
	_, key, err := d.VersionIdentity(ref)
	if err != nil {
		t.Fatal(err)
	}
	normal, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	identity := d.ErasureIdentity{Ref: ref, ObjectKey: key, HolderID: "primary", SealID: "killed-holder-original-seal", Binding: normal.Binding()}
	if err = normal.Put(ctx, key, key+".1.tmp", ref.Hash, 6, []byte("alpha\n")); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(filepath.Join(root, key))
	if err != nil || string(actual) != "alpha\n" {
		t.Fatal("normal independent body absent", err)
	}
	if err = normal.Close(); err != nil {
		t.Fatal(err)
	}
	eraser := startErasureChild(t, ctx, root, identity, "erase-stop")
	if err = awaitErasureChildStop(ctx, eraser, "actual-durable-fence-before-root-close"); err != nil {
		t.Fatal(err)
	}
	late := startErasureChild(t, ctx, root, identity, "late-put")
	if err = late.wait(); err != nil {
		t.Fatal(err)
	}
	proof, err := os.ReadFile(filepath.Join(root, "process-close-ack-late-put"))
	if err != nil || string(proof) != "actual-store-close-returned-nil" {
		t.Fatal("late child's own logical Close unconfirmed", err)
	}
	start, _, err := erasureProcessIdentity(eraser.pid)
	if err != nil || start != eraser.start {
		t.Fatal("cannot stop changed holder generation", err)
	}
	if err = eraser.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	err = eraser.wait()
	if eraser.completionErr != nil {
		t.Fatal("actual kernel completion ledger/absence qualification failed", eraser.completionErr)
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatal("expected actual killed-holder kernel return", err)
	}
	status, ok := exit.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatal("original holder was not killed", err)
	}
	if !errors.Is(syscall.Kill(-eraser.pgid, 0), syscall.ESRCH) {
		t.Fatal("original killed holder group remains unconfirmed")
	}
	// Kernel group absence and the later independent body Truth do not wash away
	// this original child's unknown logical root/directory Close responsibility.
	if _, err = os.ReadFile(filepath.Join(root, "process-close-ack-erase-stop")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("killed original holder manufactured a Close ACK", err)
	}
	if err = erasureProcessLedger(map[string]any{"event": "object_child_logical_close_unknown", "root": root, "pid": eraser.pid, "pgid": eraser.pgid, "starttime": eraser.start, "cause": "SIGKILL before actual Store.Close", "cleanup_allowed": false}); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := reopened.ObserveErasure(ctx, identity, "", 2)
	if err != nil || !observed.Fenced || !observed.Erased || len(observed.Residual) != 0 {
		t.Fatalf("independent dead-holder closed body Truth: %+v %v", observed, err)
	}
	if _, err = os.ReadFile(filepath.Join(root, key)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("late child rebuilt original body", err)
	}
	if _, err = os.ReadFile(filepath.Join(root, key+".2.tmp")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("late child installed temporary body", err)
	}
	if err = reopened.Close(); err != nil {
		t.Fatal(err)
	}
	t.Log("original killed logical holder remains unknown; retain exact owned scope", root)
}
