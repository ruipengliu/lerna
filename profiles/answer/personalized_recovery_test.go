package answer

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"lerna/adapters/sqlitecontext"
	"lerna/tasks"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestPersonalizedAnswerSurvivesProcessExit(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		t.Run(map[bool]string{false: "restore", true: "revoke"}[revoke], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			root := t.TempDir()
			executable, e := os.Executable()
			if e != nil {
				t.Fatal(e)
			}
			child := exec.CommandContext(ctx, executable, "-test.run=^TestPersonalizedAnswerCrashProbe$")
			child.Env = append(os.Environ(), "HARNESS_ANSWER_CONTEXT_PROBE="+root)
			output, e := child.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(e, &exit) || exit.ExitCode() != 73 {
				t.Fatalf("checkpoint crash %v %s", e, output)
			}
			report, e := RestorePersonalizedAnswer(ctx, root, revoke)
			if e != nil {
				t.Fatal(e)
			}
			if revoke && (report.ValidationError != "PERMISSION_DENIED" || report.ModelCalls != 0) {
				t.Fatalf("revoked recovery called model or failed for unrelated reason: %+v", report)
			}
			if !revoke && (report.ValidationError != "" || report.ModelCalls != 1 || report.Answer != "Memory, Brain, Execution") {
				t.Fatalf("recovered original answer: %+v", report)
			}
			if report.ReadAllocated != 1 || report.Restored != true || report.Published == revoke {
				t.Fatalf("recovered answer %+v", report)
			}
		})
	}
}
func TestPersonalizedAnswerCrashProbe(t *testing.T) {
	root := os.Getenv("HARNESS_ANSWER_CONTEXT_PROBE")
	if root == "" {
		t.Skip("subprocess probe")
	}
	if e := CheckpointPersonalizedAnswerAt(context.Background(), root, os.Getenv("HARNESS_ANSWER_CONTEXT_POINT")); e != nil {
		t.Fatal(e)
	}
	t.Fatal("crash probe returned without exiting")
}

func TestPersonalizedRecoveryNeverRepeatsDispatchedModel(t *testing.T) {
	for _, point := range []string{"dispatched", "output"} {
		for _, revoke := range []bool{false, true} {
			t.Run(point+"/"+map[bool]string{false: "restore", true: "revoke"}[revoke], func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
				defer cancel()
				root := t.TempDir()
				executable, e := os.Executable()
				if e != nil {
					t.Fatal(e)
				}
				child := exec.CommandContext(ctx, executable, "-test.run=^TestPersonalizedAnswerCrashProbe$")
				child.Env = append(os.Environ(), "HARNESS_ANSWER_CONTEXT_PROBE="+root, "HARNESS_ANSWER_CONTEXT_POINT="+point)
				output, e := child.CombinedOutput()
				var exit *exec.ExitError
				if !errors.As(e, &exit) || exit.ExitCode() != 73 {
					t.Fatalf("checkpoint crash %v %s", e, output)
				}
				report, e := RestorePersonalizedAnswer(ctx, root, revoke)
				if e != nil {
					t.Fatal(e)
				}
				if report.ModelCalls != 0 || report.ReadAllocated != 1 || !report.Restored {
					t.Fatalf("replaced dispatched decision: %+v", report)
				}
				if point == "dispatched" {
					if report.Published || report.ReservedRequests != 1 || report.UsedRequests != 0 || report.ValidationError == "" {
						t.Fatalf("unknown model was regenerated or refunded: %+v", report)
					}
				} else {
					if report.Published == revoke || report.UsedRequests != 1 || report.ReservedRequests != 0 {
						t.Fatalf("orphaned output: %+v", report)
					}
					if revoke && report.ValidationError != "PERMISSION_DENIED" {
						t.Fatalf("revocation not checked: %+v", report)
					}
					if !revoke && report.Answer != "Memory, Brain, Execution" {
						t.Fatalf("changed recovered output: %+v", report)
					}
				}
			})
		}
	}
}

func TestReferenceOnlyMemoryRestoresAcrossProcessExit(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		t.Run(map[bool]string{false: "restore", true: "revoke"}[revoke], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			root := t.TempDir()
			executable, e := os.Executable()
			if e != nil {
				t.Fatal(e)
			}
			child := exec.CommandContext(ctx, executable, "-test.run=^TestPersonalizedAnswerCrashProbe$")
			child.Env = append(os.Environ(), "HARNESS_ANSWER_CONTEXT_PROBE="+root, "HARNESS_ANSWER_CONTEXT_POINT=reference")
			output, e := child.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(e, &exit) || exit.ExitCode() != 73 {
				t.Fatalf("checkpoint crash %v %s", e, output)
			}
			report, e := RestorePersonalizedAnswer(ctx, root, revoke)
			if e != nil {
				t.Fatal(e)
			}
			if !report.ReferenceOnly || !report.Restored || report.ReadAllocated != 1 || report.Published == revoke {
				t.Fatalf("reference restoration %+v", report)
			}
			if revoke {
				if report.ModelCalls != 0 || report.ValidationError != "PERMISSION_DENIED" {
					t.Fatalf("revoked reference released body: %+v", report)
				}
			} else if report.ModelCalls != 1 || report.Answer != "The three systems are Memory, Brain, and Execution." {
				t.Fatalf("original preference was not re-read: %+v", report)
			}
		})
	}
}

func TestExpiredPersonalizedRecoveryPreservesOriginalDecision(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	root := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.CommandContext(ctx, executable, "-test.run=^TestPersonalizedAnswerCrashProbe$")
	child.Env = append(os.Environ(), "HARNESS_ANSWER_CONTEXT_PROBE="+root, "HARNESS_ANSWER_CONTEXT_POINT=dispatched")
	output, err := child.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 73 {
		t.Fatalf("checkpoint crash %v %s", err, output)
	}
	cp, err := readAnswerCheckpoint(root)
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlitecontext.Open(filepath.Join(root, "context.db"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Read(ctx, contextKey(cp))
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	digest := sha256.Sum256(snapshot.Document)
	cp.Format, cp.SnapshotSHA256 = 1, &digest
	raw, err := json.Marshal(cp)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "personalized-checkpoint.json"), raw, 0600); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if err = store.Retire(ctx, contextKey(cp)); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	// Let the actual durable lease expire; neither the database nor the clock is patched.
	timer := time.NewTimer(time.Until(time.Unix(0, cp.Baseline.Work[0].LeaseUntil)) + 20*time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	for attempt := 0; attempt < 2; attempt++ {
		r, err := RestorePersonalizedAnswer(ctx, root, false)
		if err != nil {
			t.Fatalf("recovery must report rejected qualification: %v", err)
		}
		if r.ValidationError != "VERSION_CONFLICT" || r.Restored || r.Published || r.ModelCalls != 0 || r.UsedRequests != 0 || r.ReservedRequests != 1 {
			t.Fatalf("expired qualification changed original decision: %+v", r)
		}
	}
	migrated, err := readAnswerCheckpoint(root)
	if err != nil {
		t.Fatal(err)
	}
	if migrated.Format != 2 || migrated.SnapshotSHA256 != nil {
		t.Fatal("expired qualification prevented retired checkpoint cleanup")
	}
	h, err := open(ctx, root, cp.Token, cp.Location, tasks.GenerationLimits{Requests: 1, InputTokens: 600, OutputTokens: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer h.close()
	current, err := h.core.Load(ctx, cp.Baseline.Task.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if tasks.QualificationOf(current) != tasks.QualificationOf(cp.Baseline) || len(current.Generations) != 1 || current.Generations[0].OutputOperation != cp.Baseline.Generations[0].OutputOperation || current.Task.State != "RUNNING" || current.Task.Result != "" {
		t.Fatal("rejected recovery replaced original generation or published a result")
	}
}

func TestMissingPreferenceRestoresAcrossProcessExit(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		t.Run(map[bool]string{false: "restore", true: "revoke"}[revoke], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			root := t.TempDir()
			executable, e := os.Executable()
			if e != nil {
				t.Fatal(e)
			}
			child := exec.CommandContext(ctx, executable, "-test.run=^TestPersonalizedAnswerCrashProbe$")
			child.Env = append(os.Environ(), "HARNESS_ANSWER_CONTEXT_PROBE="+root, "HARNESS_ANSWER_CONTEXT_POINT=missing")
			out, e := child.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(e, &exit) || exit.ExitCode() != 73 {
				t.Fatalf("missing checkpoint: %v %s", e, out)
			}
			r, e := RestorePersonalizedAnswer(ctx, root, revoke)
			if e != nil {
				t.Fatal(e)
			}
			if !r.Restored || r.ReadAllocated != 1 || r.Published == revoke {
				t.Fatalf("missing recovery: %+v", r)
			}
			if revoke {
				if r.ModelCalls != 0 || r.ValidationError != "PERMISSION_DENIED" {
					t.Fatalf("revoked missing recovery: %+v", r)
				}
			} else if r.ModelCalls != 1 || r.Answer != "Memory, Brain, Execution (no recorded preference)." {
				t.Fatalf("missing context lost: %+v", r)
			}
		})
	}
}
