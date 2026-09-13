package catalogcheck

import (
	"context"
	"encoding/json"
	"errors"
	"lerna/adapters/simworkflow"
	"lerna/adapters/sqlitecontext"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestPersonalizedInvocationSurvivesProcessExit(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		t.Run(map[bool]string{false: "restore", true: "revoke"}[revoke], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			root := t.TempDir()
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			child := exec.CommandContext(ctx, executable, "-test.run=^TestPersonalizedActionCrashProbe$")
			child.Env = append(os.Environ(), "HARNESS_ACTION_CONTEXT_PROBE="+root)
			out, err := child.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 73 {
				t.Fatalf("checkpoint: %v %s", err, out)
			}
			r, err := RestorePersonalizedInvocation(ctx, root, revoke)
			if err != nil {
				t.Fatal(err)
			}
			if r.ContextSnapshots != 1 || r.AbsentSnapshots != 0 || r.Corrections != 0 {
				t.Fatalf("original context history changed: %+v", r)
			}
			if !r.OriginalIdentity || r.ReadAllocated != 1 || r.OtherState != "submitted" {
				t.Fatalf("identity replaced: %+v", r)
			}
			if revoke {
				if r.Started || r.Ledger != 1000 || r.State != "submitted" || r.ValidationError == "" {
					t.Fatalf("revoked recovery caused effect: %+v", r)
				}
			} else if !r.Started || r.Ledger != 997 || r.State != "authorized" || r.ValidationError != "" {
				t.Fatalf("recovery lost effect: %+v", r)
			}
		})
	}
}
func TestPersonalizedActionCrashProbe(t *testing.T) {
	root := os.Getenv("HARNESS_ACTION_CONTEXT_PROBE")
	if root == "" {
		t.Skip("subprocess probe")
	}
	if err := CheckpointPersonalizedActionAt(context.Background(), root, os.Getenv("HARNESS_ACTION_CONTEXT_POINT")); err != nil {
		t.Fatal(err)
	}
	t.Fatal("probe returned without exiting")
}

func TestPersonalizedEffectIsNotRepeatedAfterProcessExit(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		t.Run(map[bool]string{false: "restore", true: "revoke"}[revoke], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			root := t.TempDir()
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			child := exec.CommandContext(ctx, executable, "-test.run=^TestPersonalizedActionCrashProbe$")
			child.Env = append(os.Environ(), "HARNESS_ACTION_CONTEXT_PROBE="+root, "HARNESS_ACTION_CONTEXT_POINT=effect")
			out, err := child.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 73 {
				t.Fatalf("effect checkpoint: %v %s", err, out)
			}
			r, err := RestorePersonalizedInvocation(ctx, root, revoke)
			if err != nil {
				t.Fatal(err)
			}
			if r.ContextSnapshots != 1 || r.AbsentSnapshots != 0 || r.Corrections != 0 {
				t.Fatalf("original context history changed: %+v", r)
			}
			if r.InitialEffect != "UNKNOWN" || !r.WasStarted || !r.Started || !r.OriginalIdentity || r.Ledger != 997 || r.State != "authorized" || r.OtherState != "submitted" || r.ReadAllocated != 1 {
				t.Fatalf("effect was repeated/lost: %+v", r)
			}
			if !revoke && (r.ValidationError != "" || r.Effect != "CONFIRMED") {
				t.Fatalf("original effect not reconciled: %+v", r)
			}
		})
	}
}

func TestPersonalizedTaskCompletesAfterProcessExit(t *testing.T) {
	for _, point := range []string{"invoked", "effect"} {
		t.Run(point, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			root := t.TempDir()
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			child := exec.CommandContext(ctx, executable, "-test.run=^TestPersonalizedActionCrashProbe$")
			child.Env = append(os.Environ(), "HARNESS_ACTION_CONTEXT_PROBE="+root, "HARNESS_ACTION_CONTEXT_POINT="+point)
			out, err := child.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 73 {
				t.Fatalf("checkpoint: %v %s", err, out)
			}
			r, err := RestorePersonalizedTask(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			if r.TaskState != "COMPLETED" || r.ModelCalls != 0 || r.GovernedArtifacts != 4 || r.RevokedArtifacts != 4 || r.Ledger != 997 || !r.OriginalIdentity || r.ReadAllocated != 1 {
				t.Fatalf("task recovery: %+v", r)
			}
		})
	}
}

func TestSecondPersonalizedDecisionRestoresOriginalSnapshots(t *testing.T) {
	for _, point := range []string{"second-invoked", "second-effect"} {
		t.Run(point, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			root := t.TempDir()
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			child := exec.CommandContext(ctx, executable, "-test.run=^TestPersonalizedActionCrashProbe$")
			child.Env = append(os.Environ(), "HARNESS_ACTION_CONTEXT_PROBE="+root, "HARNESS_ACTION_CONTEXT_POINT="+point)
			out, err := child.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 73 {
				t.Fatalf("second decision checkpoint: %v %s", err, out)
			}
			r, err := RestorePersonalizedTask(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			if r.TaskState != "COMPLETED" || r.ModelCalls != 0 || r.GovernedArtifacts != 7 || r.RevokedArtifacts != 7 || r.ContextSnapshots != 2 || r.Ledger != 997 || r.ReadAllocated != 1 || !r.OriginalIdentity {
				t.Fatalf("second decision recovery: %+v", r)
			}
		})
	}
}

func TestReassembledInvocationSurvivesProcessExit(t *testing.T) {
	for _, point := range []string{"reassembled-invoked", "reassembled-effect"} {
		t.Run(point, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			root := t.TempDir()
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			child := exec.CommandContext(ctx, executable, "-test.run=^TestPersonalizedActionCrashProbe$")
			child.Env = append(os.Environ(), "HARNESS_ACTION_CONTEXT_PROBE="+root, "HARNESS_ACTION_CONTEXT_POINT="+point)
			out, err := child.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 73 {
				t.Fatalf("reassembled decision checkpoint: %v %s", err, out)
			}
			r, err := RestorePersonalizedTask(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			if r.TaskState != "COMPLETED" || r.ModelCalls != 0 || r.GovernedArtifacts != 4 || r.RevokedArtifacts != 4 || r.ContextSnapshots != 2 || r.Ledger != 995 || r.ReadAllocated != 2 || r.Corrections != 1 || !r.OriginalIdentity {
				t.Fatalf("reassembled decision recovery: %+v", r)
			}
		})
	}
}

func TestUnboundFailedDecisionSurvivesProcessExit(t *testing.T) {
	for _, point := range []string{"gap-invoked", "gap-effect"} {
		t.Run(point, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			root := t.TempDir()
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			child := exec.CommandContext(ctx, executable, "-test.run=^TestPersonalizedActionCrashProbe$")
			child.Env = append(os.Environ(), "HARNESS_ACTION_CONTEXT_PROBE="+root, "HARNESS_ACTION_CONTEXT_POINT="+point)
			out, err := child.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 73 {
				t.Fatalf("reassembled decision checkpoint: %v %s", err, out)
			}
			r, err := RestorePersonalizedTask(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			if r.TaskState != "COMPLETED" || r.ModelCalls != 0 || r.GovernedArtifacts != 4 || r.RevokedArtifacts != 4 || r.ContextSnapshots != 2 || r.Ledger != 995 || r.ReadAllocated != 2 || r.Corrections != 2 || r.AbsentSnapshots != 1 || !r.OriginalIdentity {
				t.Fatalf("reassembled decision recovery: %+v", r)
			}
		})
	}
}

func TestTamperedGapHistoryCannotStartAction(t *testing.T) {
	for _, mutation := range []string{"used-decision", "missing-declaration", "filled-gap", "retired-gap", "retired-current"} {
		t.Run(mutation, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			root := t.TempDir()
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			child := exec.CommandContext(ctx, executable, "-test.run=^TestPersonalizedActionCrashProbe$")
			child.Env = append(os.Environ(), "HARNESS_ACTION_CONTEXT_PROBE="+root, "HARNESS_ACTION_CONTEXT_POINT=gap-invoked")
			output, err := child.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 73 {
				t.Fatalf("checkpoint: %v %s", err, output)
			}
			cp, err := readActionContext(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(cp.AbsentContexts) != 1 || cp.AbsentContexts[0] != 2 {
				t.Fatal("not an unbound second decision")
			}
			switch mutation {
			case "used-decision":
				cp.AbsentContexts = append(cp.AbsentContexts, 1)
				dropCheckpointBinding(&cp, 1)
			case "missing-declaration":
				cp.AbsentContexts = nil
			case "retired-gap":
				cp.AbsentContexts = nil
				cp.RetiredContexts = append(cp.RetiredContexts, 2)
			case "retired-current":
				cp.RetiredContexts = append(cp.RetiredContexts, uint32(cp.Binding.Key.Decision))
				dropCheckpointBinding(&cp, uint32(cp.Binding.Key.Decision))
			case "filled-gap":
				store, err := sqlitecontext.Open(filepath.Join(root, cp.Namespace, "context.db"))
				if err != nil {
					t.Fatal(err)
				}
				snapshot, err := store.Read(ctx, cp.Binding.Key)
				if err == nil {
					snapshot.Key.Decision = 2
					_, err = store.Bind(ctx, snapshot)
				}
				store.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			raw, err := json.Marshal(cp)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(root, "action-context.json"), raw, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err = RestorePersonalizedTask(ctx, root); err == nil {
				t.Fatal("invalid history accepted")
			}
			defs, err := simworkflow.Definitions()
			if err != nil {
				t.Fatal(err)
			}
			h, err := openRuntime(ctx, filepath.Join(root, cp.Namespace), cp.Token, defs[0], false)
			if err != nil {
				t.Fatal(err)
			}
			defer h.close()
			h.clock.advance(cp.Now.Sub(h.now()))
			state, err := h.target.Snapshot(ctx, "alternative")
			if err != nil {
				t.Fatal(err)
			}
			invocation, err := h.services[cp.Request.DescriptorSHA256].GetInvocation(ctx, cp.Request.OperationID)
			if err != nil {
				t.Fatal(err)
			}
			if state.State != "submitted" || state.Ledger != 1000 || invocation.Started {
				t.Fatalf("bad history caused effects: %+v started=%v", state, invocation.Started)
			}
		})
	}
}

func TestBoundButUnusedContextRemainsInRecoveryHistory(t *testing.T) {
	for _, point := range []string{"bound-failure-invoked"} {
		t.Run(point, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			root := t.TempDir()
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			child := exec.CommandContext(ctx, executable, "-test.run=^TestPersonalizedActionCrashProbe$")
			child.Env = append(os.Environ(), "HARNESS_ACTION_CONTEXT_PROBE="+root, "HARNESS_ACTION_CONTEXT_POINT="+point)
			out, err := child.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 73 {
				t.Fatalf("reassembled decision checkpoint: %v %s", err, out)
			}
			r, err := RestorePersonalizedTask(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			if r.TaskState != "COMPLETED" || r.ModelCalls != 0 || r.GovernedArtifacts != 4 || r.RevokedArtifacts != 4 || r.ContextSnapshots != 3 || r.Ledger != 995 || r.ReadAllocated != 3 || r.Corrections != 2 || r.AbsentSnapshots != 0 || !r.OriginalIdentity {
				t.Fatalf("reassembled decision recovery: %+v", r)
			}
		})
	}
}

func TestDeletedMemoryPreservesOriginalEffectAfterProcessExit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	root := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.CommandContext(ctx, executable, "-test.run=^TestPersonalizedActionCrashProbe$")
	child.Env = append(os.Environ(), "HARNESS_ACTION_CONTEXT_PROBE="+root, "HARNESS_ACTION_CONTEXT_POINT=effect")
	out, err := child.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 73 {
		t.Fatalf("effect checkpoint: %v %s", err, out)
	}
	r, err := RestorePersonalizedInvocationAfterDeletion(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if !r.ContextErased || r.InitialEffect != "UNKNOWN" || !r.WasStarted || !r.Started || !r.OriginalIdentity || r.Ledger != 997 || r.State != "authorized" || r.OtherState != "submitted" || r.ReadAllocated != 1 || r.Effect != "CONFIRMED" {
		t.Fatalf("deletion lost or repeated original effect: %+v", r)
	}
	if r.Cleanup.Authority != "committed" || len(r.Cleanup.Derived) != 5 || r.Cleanup.Derived[0].State != "applied" || r.Cleanup.Derived[1].State != "applied" || r.Cleanup.Derived[2].State != "not_covered" || r.Cleanup.Derived[3].Name != "legacy-checkpoints" || r.Cleanup.Derived[3].State != "applied" || r.Cleanup.Derived[4].Name != "controlled-backups" || r.Cleanup.Derived[4].State != "not_covered" || r.Cleanup.Local[0].Name != "admission-comparisons" || r.Cleanup.Local[0].State != "applied" || r.Cleanup.Replicas[0].State != "not_covered" {
		t.Fatalf("cleanup report exceeded confirmed scope: %+v", r.Cleanup)
	}

}

func dropCheckpointBinding(cp *actionContextCheckpoint, n uint32) {
	delete(cp.ContextDigests, n)
	live := cp.BoundContexts[:0]
	for _, number := range cp.BoundContexts {
		if number != n {
			live = append(live, number)
		}
	}
	cp.BoundContexts = live
}
