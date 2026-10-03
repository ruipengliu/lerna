//go:build linux

package integration_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/ruipengliu/lerna/adapters/wasi"
	"github.com/ruipengliu/lerna/api"
	domain "github.com/ruipengliu/lerna/internal/execution"
)

// Driver 的公开 Prepare/Reconcile、真实受限宿主和原目录是此故障边界。
// 9998 项有界历史文件只预置容量前态，不声称运行过 9998 次用户程序。
func TestRestrictedWASIUnknownJournalRenameRetainsCapacityAndOriginalAttempt(t *testing.T) {
	f, host, cfg := newWASIFixtureConfig(t)
	env := wasiEnvironment(t, f, host, wasi.DefaultLimits())
	prepare := func() domain.AttemptRequest {
		invoke := wasiInvoke(t, f, env, wasiWriteModule([]byte(`{"format":"harness-passive-namespace/1","bindings":[]}`)))
		var intent domain.ExecutionIntent
		body, err := f.content.ReadBytes(context.Background(), f.sc, f.auth, invoke.IntentRef, "prepare", "device")
		if err != nil || api.Decode(body, &intent) != nil {
			t.Fatalf("original intent %v", err)
		}
		arguments, err := f.content.ReadBytes(context.Background(), f.sc, f.auth, intent.ArgumentsRef, "prepare", "device")
		if err != nil {
			t.Fatal(err)
		}
		frozen, err := host.Prepare(context.Background(), f.sc, f.auth, invoke, intent, arguments)
		if err != nil {
			t.Fatal(err)
		}
		return domain.AttemptRequest{Scope: f.sc, Auth: f.auth, Invoke: invoke, Intent: intent, Attempt: domain.Attempt{AttemptID: api.NewID("attempt"), OperationID: invoke.OperationID, Prepared: frozen, ControlWindowID: api.NewID("window")}}
	}
	known := prepare()
	want, err := host.Reconcile(context.Background(), known)
	if err != nil || want.Effect != "not_applied" || !want.UsageFinal {
		t.Fatalf("original absent entrance %+v %v", want, err)
	}
	path := filepath.Join(cfg.Root, known.Attempt.AttemptID+".json")
	native, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var history map[string]any
	if err = api.Decode(native, &history); err != nil {
		t.Fatal(err)
	}
	if err = host.Close(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 9998; i++ {
		id := api.NewID("attempt")
		history["attempt_id"] = id
		if err = os.WriteFile(filepath.Join(cfg.Root, id+".json"), api.Raw(history), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var armed atomic.Bool
	var faults atomic.Int32
	cfg.JournalFault = func(phase wasi.JournalPhase, name string) error {
		if phase == wasi.AfterJournalRename && strings.HasSuffix(name, ".json") && armed.CompareAndSwap(true, false) {
			faults.Add(1)
			return syscall.EIO
		}
		return nil
	}
	host, err = wasi.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := host.Close(); err != nil {
			t.Error(err)
		}
	})
	original, next := prepare(), prepare()
	armed.Store(true)
	if _, err = host.Reconcile(context.Background(), original); !errors.Is(err, syscall.EIO) || faults.Load() != 1 {
		t.Fatalf("actual post-rename failure lost original cause %v", err)
	}
	if _, err = os.ReadFile(filepath.Join(cfg.Root, original.Attempt.AttemptID+".json")); err != nil {
		t.Fatalf("native rename did not retain original journal %v", err)
	}
	if _, err = host.Reconcile(context.Background(), next); !api.IsCode(err, "overloaded") {
		t.Fatalf("unknown original journal did not consume capacity; new original admitted: %v", err)
	}
	if _, err = os.Stat(filepath.Join(cfg.Root, next.Attempt.AttemptID+".json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("capacity rejection created a new responsibility %v", err)
	}
	recovered, err := host.Reconcile(context.Background(), original)
	if err != nil || recovered.Effect != "not_applied" || !recovered.UsageFinal || !api.Equal(recovered.Usage, want.Usage) {
		t.Fatalf("original renamed no-entry journal did not recover %+v %v", recovered, err)
	}
	if err = host.Close(); err != nil {
		t.Fatal(err)
	}
	host, err = wasi.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := host.Reconcile(context.Background(), original)
	if err != nil || !api.Equal(reopened, recovered) {
		t.Fatalf("reopen changed original attempt evidence %+v %v", reopened, err)
	}
	if _, err = host.Reconcile(context.Background(), next); !api.IsCode(err, "overloaded") {
		t.Fatalf("reopen released a persisted original journal slot: %v", err)
	}
}
