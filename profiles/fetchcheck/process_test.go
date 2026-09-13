package fetchcheck

import (
	"context"
	"encoding/json"
	"errors"
	"lerna/adapters/acquisitionexecution"
	"lerna/adapters/fetchtask"
	"lerna/execution"
	"lerna/fetch"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

type processConfig struct {
	Token, Mode string
	URLs        []string
	Request     execution.Request
}

func TestAcquisitionRecoversAcrossActualProcessExit(t *testing.T) {
	for _, mode := range []string{"reserved", "response", "content", "outcome"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			var hits atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				if r.URL.Path == "/start" {
					http.Redirect(w, r, "/final", http.StatusFound)
					return
				}
				w.Header().Set("Content-Type", "text/plain")
				w.Write([]byte("hello"))
			}))
			defer server.Close()
			urls := []string{server.URL + "/start", server.URL + "/final"}
			root := t.TempDir()
			h, err := open(ctx, root, "", urls)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := json.Marshal(map[string]any{"url": urls[0], "max_bytes": 1024, "max_requests": 2, "timeout_ms": 1000})
			request, grant, err := h.request(ctx, body)
			if err != nil {
				h.close()
				t.Fatal(err)
			}
			if _, err = h.client.Invoke(ctx, request, grant); err != nil {
				h.close()
				t.Fatal(err)
			}
			saved := processConfig{Token: h.token, Mode: mode, URLs: urls, Request: request}
			raw, _ := json.Marshal(saved)
			if err = os.WriteFile(filepath.Join(root, "probe.json"), raw, 0600); err != nil {
				h.close()
				t.Fatal(err)
			}
			h.close()
			child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAcquisitionCrashProbe$")
			child.Env = append(os.Environ(), "LERNA_FETCH_PROCESS_ROOT="+root)
			output, err := child.CombinedOutput()
			var exited *exec.ExitError
			if !errors.As(err, &exited) || exited.ExitCode() != 75 {
				t.Fatalf("child exit: %v %s", err, output)
			}
			h, err = open(ctx, root, saved.Token, urls)
			if err != nil {
				t.Fatal(err)
			}
			defer h.close()
			intent, err := h.attempts.Inspect(ctx, "local", request.OperationID)
			if err != nil {
				t.Fatal(err)
			}
			if intent.EvidenceOperation == "" || intent.Fingerprint != request.Fingerprint() {
				t.Fatal("lost original acquisition identity")
			}
			fact, known, err := h.attempts.Outcome(ctx, "local", request.OperationID)
			if err != nil || known != (mode == "outcome") {
				t.Fatal("unexpected committed result before recovery")
			}
			var original fetch.Result
			var originalRef string
			recoverable := mode == "content" || mode == "outcome"
			if recoverable {
				originalRef, err = h.evidence.Lookup(ctx, intent.EvidenceOperation)
				if err != nil {
					t.Fatal(err)
				}
				original, err = h.evidence.Read(ctx, originalRef)
				if err != nil || string(original.Body) != "hello" {
					t.Fatal("lost saved response")
				}
			}
			wantHits := int32(2)
			if mode == "reserved" {
				wantHits = 0
			}
			if hits.Load() != wantHits {
				t.Fatal("unexpected actual pre-recovery requests")
			}
			originalInvocation, err := h.exec.GetInvocation(ctx, request.OperationID)
			if err != nil || originalInvocation.OutputOperation == "" {
				t.Fatal("missing original Execution output identity")
			}
			result, err := h.client.Reconcile(ctx, request.OperationID)
			if err != nil {
				t.Fatal(err)
			}
			currentInvocation, e := h.exec.GetInvocation(ctx, request.OperationID)
			if e != nil || currentInvocation.OutputOperation != originalInvocation.OutputOperation {
				t.Fatal("recovery replaced Execution output identity")
			}
			if recoverable {
				if result.Result != "SUCCESS" || result.Effect != "CONFIRMED" {
					t.Fatalf("saved acquisition not recovered: %s %s", result.Result, result.Effect)
				}
				restored, err := h.evidence.Read(ctx, originalRef)
				if err != nil || !reflect.DeepEqual(restored, original) {
					t.Fatal("recovery changed original response or time")
				}
				fact, known, err = h.attempts.Outcome(ctx, "local", request.OperationID)
				if err != nil || !known || fact.Status != "acquired" || fact.Requests != 2 || fact.Reference != originalRef {
					t.Fatal("original content not bound to recovered outcome")
				}
			} else if result.Result != "UNKNOWN" || result.Effect != "UNKNOWN" || result.Reference != "" {
				t.Fatal("unsaved response fabricated a known result")
			}
			if _, err = h.exec.Run(ctx, request.OperationID); err != nil {
				t.Fatal(err)
			}
			if err = h.exec.Drain(ctx, 16); err != nil {
				t.Fatal(err)
			}
			task, err := h.core.Get(ctx, h.token, request.Qualification.Ref)
			if err != nil {
				t.Fatal(err)
			}
			wantState := "WAITING"
			if recoverable {
				wantState = "COMPLETED"
			}
			if task.State != wantState {
				t.Fatalf("recovered Core state=%s want=%s", task.State, wantState)
			}
			if hits.Load() != wantHits {
				t.Fatal("recovery dispatched replacement HTTP requests")
			}
			budget, err := h.attempts.Budget(ctx, request.Qualification.Ref)
			if err != nil || budget.Charged != 2 {
				t.Fatal("process exit reset request reservation")
			}
		})
	}
}

type crashFetcher struct {
	fetch.Fetcher
	mode string
}

func (f crashFetcher) Fetch(ctx context.Context, in fetch.Request) (fetch.Result, error) {
	if f.mode == "reserved" {
		os.Exit(75)
	}
	out, err := f.Fetcher.Fetch(ctx, in)
	if err == nil && f.mode == "response" {
		os.Exit(75)
	}
	return out, err
}

type crashOutcome struct {
	fetch.OutcomeStore
	mode string
}

func (s crashOutcome) Complete(ctx context.Context, in fetch.AttemptIntent, out fetch.Outcome) error {
	if out.Status == "acquired" && s.mode == "content" {
		os.Exit(75)
	}
	err := s.OutcomeStore.Complete(ctx, in, out)
	if err == nil && out.Status == "acquired" && s.mode == "outcome" {
		os.Exit(75)
	}
	return err
}
func TestAcquisitionCrashProbe(t *testing.T) {
	root := os.Getenv("LERNA_FETCH_PROCESS_ROOT")
	if root == "" {
		t.Skip("subprocess probe")
	}
	raw, err := os.ReadFile(filepath.Join(root, "probe.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved processConfig
	if json.Unmarshal(raw, &saved) != nil {
		t.Fatal("invalid probe configuration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	h, err := open(ctx, root, saved.Token, saved.URLs)
	if err != nil {
		t.Fatal(err)
	}
	defer h.close()
	guard, err := fetchtask.New(h.auth, h.work, h.core, h.token)
	if err != nil {
		t.Fatal(err)
	}
	driver, err := acquisitionexecution.NewPage(crashFetcher{h.http, saved.Mode}, crashOutcome{h.attempts, saved.Mode}, h.evidence, h.auth, acquisitionexecution.Config{Guard: guard, Token: h.token, Namespace: "local", Subject: "operator", Capability: h.cap, MaxBytes: 1024, MaxRequests: 2, TaskLimit: 2, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	h.exec, err = execution.New(h.grants, h.work, h.access, driver, h.binding, h.cap, config(), h.operation)
	if err != nil {
		t.Fatal(err)
	}
	result, err := h.exec.Run(ctx, saved.Request.OperationID)
	t.Fatalf("crash window not reached: result=%s error=%v", result.Result, err)
}
