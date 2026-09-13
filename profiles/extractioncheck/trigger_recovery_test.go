package extractioncheck

import (
	"context"
	"encoding/json"
	"errors"
	"lerna/adapters/contentpolicy"
	"lerna/adapters/extractionauth"
	"lerna/authorization"
	"lerna/extraction"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"lerna/tasks"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestTriggerSubmissionRecoversAfterProcessExit(t *testing.T) {
	for _, mode := range []string{"before-submit", "after-submit"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			root := t.TempDir()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestTriggerSubmissionCrashProbe$")
			cmd.Env = append(os.Environ(), "LERNA_TRIGGER_ROOT="+root, "LERNA_TRIGGER_MODE="+mode)
			output, err := cmd.CombinedOutput()
			var exited *exec.ExitError
			if !errors.As(err, &exited) || exited.ExitCode() != 94 {
				t.Fatalf("child: %v %s", err, output)
			}
			var token string
			data, err := os.ReadFile(filepath.Join(root, "trigger-probe.json"))
			if err != nil || json.Unmarshal(data, &token) != nil {
				t.Fatalf("probe identity: %v", err)
			}
			h, err := open(ctx, root, token)
			if err != nil {
				t.Fatal(err)
			}
			defer h.close()
			event := &wire.ContentSource{Kind: "note", Key: "one", Revision: 1}
			before, err := h.candidates.GetTriggerRound(ctx, "local", "operator", "recover", event)
			if err != nil {
				t.Fatal(err)
			}
			original, originalErr := h.core.LookupOperation(ctx, token, "local", before.Submission.OperationID)
			if mode == "after-submit" && originalErr != nil {
				t.Fatal(originalErr)
			}
			if mode == "before-submit" && !authorization.Is(originalErr, authorization.NotFound) {
				t.Fatalf("expected absent Core submission: %v", originalErr)
			}
			dispatcher := recoveryDispatcher(t, h, h.core)
			poller, err := extraction.NewTriggerPoller(dispatcher, h.currentSources)
			if err != nil {
				t.Fatal(err)
			}
			scope := extraction.SourceScope{Kind: "note", Key: "one"}
			recovered, err := poller.Poll(ctx, "recover", scope)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "after-submit" && (recovered.Ref != original.Ref || recovered.Version != original.Version) {
				t.Fatal("recovery replaced or advanced original task")
			}
			mapped, err := h.core.LookupOperation(ctx, token, "local", before.Submission.OperationID)
			if err != nil || mapped.Ref != recovered.Ref || mapped.Version != recovered.Version {
				t.Fatalf("recovery did not use original operation: %v", err)
			}
			replay, err := poller.Poll(ctx, "recover", scope)
			if err != nil || replay.Ref != recovered.Ref || replay.Version != recovered.Version {
				t.Fatalf("duplicate event: %v", err)
			}
			after, err := h.candidates.GetTriggerRound(ctx, "local", "operator", "recover", event)
			if err != nil || !reflect.DeepEqual(before.Submission, after.Submission) {
				t.Fatalf("original submission changed: %v", err)
			}
			// The new revision must pass its own metadata policy before it can
			// reach the persisted one-round budget check.
			if err = h.policy.Replace([]contentpolicy.Rule{{Kind: "note", Key: "one", Revision: 2, Actions: []string{"store", "process", "retain", "discover", "disclose", "delete"}, Purposes: []string{"task"}, Locations: []string{"local"}, RetainUntil: h.now().Add(time.Hour).Unix()}}); err != nil {
				t.Fatal(err)
			}
			if _, err = dispatcher.Dispatch(ctx, "recover", &wire.ContentSource{Kind: "note", Key: "one", Revision: 2}); err != memory.Capacity {
				t.Fatalf("restart reset one-round budget: %v", err)
			}
		})
	}
}

func TestTriggerSubmissionCrashProbe(t *testing.T) {
	root := os.Getenv("LERNA_TRIGGER_ROOT")
	if root == "" {
		t.Skip("subprocess only")
	}
	ctx := context.Background()
	h, err := open(ctx, root, "")
	if err != nil {
		t.Fatal(err)
	}
	defer h.close()
	enableScan(t, ctx, h)
	authority, err := extractionauth.New(h.auth, extractionauth.Scope{Namespace: "local", Resource: "root", Purpose: "task", Location: "local"})
	if err != nil {
		t.Fatal(err)
	}
	b := memory.Binding{Token: h.token, Namespace: "local", Subject: "operator", Location: "local", Recipient: "local"}
	registry, err := extraction.NewTriggerRegistry(h.candidates, authority, h.clock, b, "task")
	if err != nil {
		t.Fatal(err)
	}
	if err = registry.Register(ctx, extraction.TriggerSpec{ID: "recover", Condition: "source.changed", Sources: []extraction.SourceScope{{Kind: "note", Key: "one"}}, MaxRounds: 1, MaxSteps: 3, ExpiresUnix: h.now().Add(10 * time.Minute).Unix()}); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(h.token)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "trigger-probe.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	dispatcher := recoveryDispatcher(t, h, crashTriggerHost{h.core, os.Getenv("LERNA_TRIGGER_MODE")})
	poller, err := extraction.NewTriggerPoller(dispatcher, h.currentSources)
	if err != nil {
		t.Fatal(err)
	}
	_, err = poller.Poll(ctx, "recover", extraction.SourceScope{Kind: "note", Key: "one"})
	t.Fatalf("crash boundary not reached: %v", err)
}

type crashTriggerHost struct {
	extraction.TriggerTaskHost
	mode string
}

func recoveryDispatcher(t *testing.T, h *harness, host extraction.TriggerTaskHost) *extraction.TriggerDispatcher {
	t.Helper()
	authority, err := extractionauth.New(h.auth, extractionauth.Scope{Namespace: "local", Resource: "root", Purpose: "task", Location: "local"})
	if err != nil {
		t.Fatal(err)
	}
	d, err := extraction.NewTriggerDispatcher(h.candidates, authority, h.clock, memory.Binding{Token: h.token, Namespace: "local", Subject: "operator", Location: "local", Recipient: "local"}, host, triggerInputs{h}, h.operation)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func (h crashTriggerHost) Submit(ctx context.Context, token string, s tasks.Submission) (tasks.Task, error) {
	if h.mode == "before-submit" {
		os.Exit(94)
	}
	task, err := h.TriggerTaskHost.Submit(ctx, token, s)
	if err == nil && h.mode == "after-submit" {
		os.Exit(94)
	}
	return task, err
}
