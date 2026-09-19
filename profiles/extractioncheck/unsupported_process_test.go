package extractioncheck

import (
	"context"
	"encoding/json"
	"errors"
	executionlocal "lerna/adapters/execution/local"
	extractionexecution "lerna/adapters/extraction/execution"
	"lerna/execution"
	"lerna/memory"
	"lerna/sdk"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestUnsupportedAttemptRecoversAfterProcessExit(t *testing.T) {
	for _, mode := range []string{"before-fact", "after-fact", "after-confirmation"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			root := t.TempDir()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestUnsupportedAttemptCrashProbe$")
			cmd.Env = append(os.Environ(), "LERNA_UNSUPPORTED_ROOT="+root, "LERNA_UNSUPPORTED_MODE="+mode)
			output, err := cmd.CombinedOutput()
			var exited *exec.ExitError
			if !errors.As(err, &exited) || exited.ExitCode() != 95 {
				t.Fatalf("child: %v %s", err, output)
			}
			var saved saveProbeConfig
			data, err := os.ReadFile(filepath.Join(root, "unsupported-probe.json"))
			if err != nil || json.Unmarshal(data, &saved) != nil {
				t.Fatalf("probe binding: %v", err)
			}
			h, err := open(ctx, root, saved.Token)
			if err != nil {
				t.Fatal(err)
			}
			defer h.close()
			// Restore the actual provider without recreating the source files.
			// A mistaken retry would now be able to persist a new decline fact.
			installObservedChoices(t, h, false)
			store, saver := bindProcessSave(t, h, "")
			original, err := h.exec.GetInvocation(ctx, saved.Request.OperationID)
			if err != nil {
				t.Fatal(err)
			}
			before, err := h.core.Get(ctx, h.token, saved.Request.Qualification.Ref)
			if err != nil {
				t.Fatal(err)
			}
			state, err := h.candidates.Inspect(ctx, "local", saved.Request.OperationID)
			if mode == "before-fact" {
				if err != memory.Missing {
					t.Fatalf("invented unsupported fact: %v", err)
				}
			} else if err != nil || state.State != "unsupported" || state.Reason != "insufficient_evidence" || state.Committed || state.InvocationSHA256 != saved.Request.Fingerprint() {
				t.Fatalf("original unsupported fact: %+v %v", state, err)
			}
			out, err := h.exec.Reconcile(ctx, saved.Request.OperationID)
			if err != nil {
				t.Fatal(err)
			}
			want := "NOT_OCCURRED"
			if mode == "before-fact" {
				want = "UNKNOWN"
			}
			if out.Effect != want || out.Result == "SUCCESS" || out.OutputOperation != original.OutputOperation {
				t.Fatalf("recovered effect: %s want %s", out.Effect, want)
			}
			if err = h.exec.Drain(ctx, 16); err != nil {
				t.Fatal(err)
			}
			after, err := h.core.Get(ctx, h.token, saved.Request.Qualification.Ref)
			if err != nil || after.State == "COMPLETED" {
				t.Fatalf("unsupported task completed: %v", err)
			}
			if mode == "before-fact" && after.State != "WAITING" {
				t.Fatal("unknown attempt did not remain waiting")
			}
			if mode == "after-confirmation" && after.Version != before.Version {
				t.Fatal("recovery advanced confirmed task")
			}
			replay, err := h.exec.Run(ctx, saved.Request.OperationID)
			if err != nil || replay.Effect != want || replay.Request.Fingerprint() != saved.Request.Fingerprint() || replay.Reference != out.Reference {
				t.Fatalf("replay changed original operation: %v", err)
			}
			current, e := h.candidates.Inspect(ctx, "local", saved.Request.OperationID)
			if mode == "before-fact" && e != memory.Missing || mode != "before-fact" && (e != nil || current != state) {
				t.Fatalf("recovery reexecuted or changed original fact: %+v %v", current, e)
			}
			if _, err = saver.Inspect(ctx, saved.Request.OperationID); err != memory.Missing {
				t.Fatalf("recovery invented save intent: %v", err)
			}
			changes, err := store.ReadChanges(ctx, "local", "personal", 0, 16)
			if err != nil || len(changes) != 0 {
				t.Fatalf("recovery saved Memory: %v", err)
			}
		})
	}
}

func TestUnsupportedAttemptCrashProbe(t *testing.T) {
	root := os.Getenv("LERNA_UNSUPPORTED_ROOT")
	if root == "" {
		t.Skip("subprocess only")
	}
	mode := os.Getenv("LERNA_UNSUPPORTED_MODE")
	ctx := context.Background()
	h, err := open(ctx, root, "")
	if err != nil {
		t.Fatal(err)
	}
	installObservedChoices(t, h)
	_, saver := bindProcessSave(t, h, "")
	driver, err := extractionexecution.WithMemory(unsupportedCrashDriver{Driver: h.target, mode: mode}, saver)
	if err != nil {
		t.Fatal(err)
	}
	h.exec, err = execution.New(h.grants, h.work, h.access, driver, h.binding, h.cap, config(), h.operation)
	if err != nil {
		t.Fatal(err)
	}
	h.client = sdk.NewCapabilityClient(executionlocal.Bind(h.exec, "local"), "local")
	r, grant, err := h.request(ctx, []byte(`{"sources":[{"kind":"choice","key":"one","revision":1},{"kind":"choice","key":"two","revision":1}]}`))
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(saveProbeConfig{Token: h.token, Request: r})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "unsupported-probe.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = h.client.Invoke(ctx, r, grant); err != nil {
		t.Fatal(err)
	}
	out, err := h.exec.Run(ctx, r.OperationID)
	if err != nil || mode != "after-confirmation" || out.Effect != "NOT_OCCURRED" {
		t.Fatalf("crash boundary: %v", err)
	}
	if err = h.exec.Drain(ctx, 16); err != nil {
		t.Fatal(err)
	}
	os.Exit(95)
}

// The real driver persists the fact. Exit skips all defers and confirmation.
type unsupportedCrashDriver struct {
	execution.Driver
	mode string
}

func (d unsupportedCrashDriver) Start(ctx context.Context, c execution.Call) error {
	if d.mode == "before-fact" {
		os.Exit(95)
	}
	if err := d.Driver.Start(ctx, c); err != nil {
		return err
	}
	if d.mode == "after-fact" {
		os.Exit(95)
	}
	return nil
}
