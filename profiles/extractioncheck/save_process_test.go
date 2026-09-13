package extractioncheck

import (
	"context"
	"encoding/json"
	"errors"
	"lerna/adapters/extractionauth"
	"lerna/adapters/extractionexecution"
	"lerna/adapters/memoryauth"
	"lerna/adapters/sqlitememory"
	"lerna/execution"
	"lerna/extraction"
	"lerna/memory"
	"lerna/schema"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

type saveProbeConfig struct {
	Token   string
	Request execution.Request
}

func bindProcessSave(t *testing.T, h *harness, mode string) (*sqlitememory.Store, *extraction.Saver) {
	t.Helper()
	d := extraction.SavedSchema()
	schemas, err := schema.New([]schema.Resource{d})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := memoryauth.New(h.auth, h.source, []memoryauth.Collection{{Namespace: "local", Name: "personal", Resource: "root", PolicyRef: "local-extracted", Purpose: "task", Storage: []string{"local"}, Processing: []string{"local"}, Recipients: []string{"local"}, Schemas: map[string]string{d.Type: schema.Digest(d.Document)}}})
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlitememory.Open(filepath.Join(h.root, "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	var writes memory.Store = store
	if mode == "before-commit" || mode == "after-commit" {
		writes = processSaveStore{Store: store, mode: mode}
	}
	service, err := memory.New(writes, policy, schemas, h.clock, memory.Config{Location: "local", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	auth, err := extractionauth.New(h.auth, extractionauth.Scope{Namespace: "local", Resource: "root", Purpose: "task", Location: "local"})
	if err != nil {
		t.Fatal(err)
	}
	saver, err := extraction.NewSaver(h.candidates, auth, h.source, service, h.clock, memory.Binding{Token: h.token, Namespace: "local", Subject: "operator", Location: "local", Recipient: "local"}, extraction.SaveTarget{Collection: "personal", PolicyRef: "local-extracted", Purpose: "task"}, h.operation)
	if err != nil {
		t.Fatal(err)
	}
	driver, err := extractionexecution.WithMemory(h.target, saver)
	if err != nil {
		t.Fatal(err)
	}
	h.exec, err = execution.New(h.grants, h.work, h.access, driver, h.binding, h.cap, config(), h.operation)
	if err != nil {
		t.Fatal(err)
	}
	return store, saver
}

func TestMemorySaveTaskRecoversAfterAbruptProcessExit(t *testing.T) {
	for _, mode := range []string{"before-commit", "after-commit", "after-confirmation"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			root := t.TempDir()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMemorySaveTaskCrashProbe$")
			cmd.Env = append(os.Environ(), "LERNA_SAVE_TASK_ROOT="+root, "LERNA_SAVE_TASK_MODE="+mode)
			output, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 92 {
				t.Fatalf("child: %v %s", err, output)
			}
			var config saveProbeConfig
			raw, err := os.ReadFile(filepath.Join(root, "save-probe.json"))
			if err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal(raw, &config); err != nil {
				t.Fatal(err)
			}
			h, err := open(ctx, root, config.Token)
			if err != nil {
				t.Fatal(err)
			}
			defer h.close()
			store, saver := bindProcessSave(t, h, "")
			intent, err := h.candidates.LookupSave(ctx, "local", "operator", config.Request.OperationID)
			if err != nil {
				t.Fatal(err)
			}
			state, err := saver.Inspect(ctx, config.Request.OperationID)
			if err != nil {
				t.Fatal(err)
			}
			want := "committed"
			if mode == "before-commit" {
				want = "unknown"
			}
			if state.State != want {
				t.Fatalf("original save state: %s want %s", state.State, want)
			}
			before, err := h.core.Get(ctx, h.token, config.Request.Qualification.Ref)
			if err != nil {
				t.Fatal(err)
			}
			if mode != "after-confirmation" && before.State == "COMPLETED" {
				t.Fatal("task completed before confirmation")
			}
			if _, err = h.exec.Reconcile(ctx, config.Request.OperationID); err != nil {
				t.Fatal(err)
			}
			if err = h.exec.Drain(ctx, 16); err != nil {
				t.Fatal(err)
			}
			task, err := h.core.Get(ctx, h.token, config.Request.Qualification.Ref)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "before-commit" && task.State == "COMPLETED" || mode != "before-commit" && task.State != "COMPLETED" {
				t.Fatalf("recovered task: %s", task.State)
			}
			if mode == "after-confirmation" && task.Version != before.Version {
				t.Fatal("reconciliation changed an already confirmed task")
			}
			replay, err := saver.Save(ctx, config.Request.OperationID)
			if err != nil || replay.State != want {
				t.Fatalf("save replay: %s %v", replay.State, err)
			}
			after, err := h.candidates.LookupSave(ctx, "local", "operator", config.Request.OperationID)
			if err != nil || after.Request.OperationId != intent.Request.OperationId {
				t.Fatal("replacement save operation")
			}
			changes, err := store.ReadChanges(ctx, "local", "personal", 0, 16)
			if err != nil {
				t.Fatal(err)
			}
			count := 1
			if mode == "before-commit" {
				count = 0
			}
			if len(changes) != count {
				t.Fatalf("Memory changes: %d want %d", len(changes), count)
			}
			if count == 1 && (changes[0].OperationID != intent.Request.OperationId || replay.Receipt == nil || *replay.Receipt != changes[0]) {
				t.Fatal("original receipt mismatch")
			}
		})
	}
}

func TestMemorySaveTaskCrashProbe(t *testing.T) {
	root := os.Getenv("LERNA_SAVE_TASK_ROOT")
	if root == "" {
		t.Skip("child only")
	}
	mode := os.Getenv("LERNA_SAVE_TASK_MODE")
	ctx := context.Background()
	h, err := open(ctx, root, "")
	if err != nil {
		t.Fatal(err)
	}
	r, grant, err := h.request(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(saveProbeConfig{Token: h.token, Request: r})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "save-probe.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	bindProcessSave(t, h, mode)
	// The existing SDK still refers to the same authoritative journal. It
	// admits only; the newly bound execution host performs the actual effect.
	if _, err = h.client.Invoke(ctx, r, grant); err != nil {
		t.Fatal(err)
	}
	if _, err = h.exec.Run(ctx, r.OperationID); err != nil {
		t.Fatal(err)
	}
	if mode != "after-confirmation" {
		t.Fatal("did not reach requested crash boundary")
	}
	if err = h.exec.Drain(ctx, 16); err != nil {
		t.Fatal(err)
	}
	task, err := h.core.Get(ctx, h.token, r.Qualification.Ref)
	if err != nil || task.State != "COMPLETED" {
		t.Fatalf("confirmation: %v", err)
	}
	os.Exit(92)
}

type processSaveStore struct {
	memory.Store
	mode string
}

func (s processSaveStore) Commit(ctx context.Context, c memory.Change) (memory.Receipt, error) {
	if s.mode == "before-commit" {
		os.Exit(92)
	}
	r, err := s.Store.Commit(ctx, c)
	if err != nil {
		return r, err
	}
	os.Exit(92)
	return r, nil
}
