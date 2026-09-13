// Package contextcheck reports the implemented personalized-context contracts.
// Passing this profile is not a claim that every ticket-18 requirement is complete.
package contextcheck

import (
	"context"
	"errors"
	"fmt"
	"lerna/conformance"
	"lerna/profiles/answer"
	"lerna/profiles/catalogcheck"
	"os"
	"os/exec"
	"strings"
	"time"
)

const ID = "personalized-context-v1"

func Profile(executable string) conformance.Profile {
	p := conformance.Profile{ID: ID, Version: "1", Configuration: map[string]string{"language": "Go", "storage": "real SQLite authorization, Memory, context, business target; controlled file artifacts", "models": "deterministic contract models only; no external provider calls", "limits": "up to three decisions and two persisted corrections per personalized fixture; single-use read per selected revision; 32KiB input; bounded publication; 60s per case", "recovery": "actual child exit 73 before cleanup; original credentials/read identity; current worker qualification required"}, Limitations: []string{"This profile covers its listed implemented contracts, not complete ticket-18 acceptance or 90%/95% model quality.", "Bounded action reassembly is covered below; real-model answer evaluation has not passed and is reported separately. Action recovery uses a restored deterministic fixture clock; it does not prove wall-clock worker takeover.", "Reference-only is an additional trusted host storage restriction; it does not grant Memory or output rights.", "Current permissions and revisions are checked across independent stores; no globally atomic revocation is claimed."}}
	add := func(id, evidence string, check func(context.Context) error) {
		p.Cases = append(p.Cases, conformance.Case{ID: id, Evidence: evidence, Required: true, Input: id, Expected: "verified", Check: func(ctx context.Context) (string, error) {
			ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
			defer cancel()
			if e := check(ctx); e != nil {
				return "", e
			}
			return "verified", nil
		}})
	}
	add("answer-missing", "actual_authorized_absence_and_answer_publication", func(ctx context.Context) error {
		r, e := answer.RunPersonalizedAnswer(ctx, "missing", true, nil)
		if e != nil {
			return e
		}
		return require(r.State == "COMPLETED" && r.Answer == "Memory, Brain, Execution (no recorded preference)." && r.MissingSources == 1 && r.MemorySources == 0 && r.ReadAllocated == 1)
	})
	for _, tc := range []struct {
		id, style, text string
		applicable      bool
	}{{"concise", "concise", "Memory, Brain, Execution", true}, {"detailed", "detailed", "The three systems are Memory, Brain, and Execution.", true}, {"inapplicable", "detailed", "Memory, Brain, Execution", false}} {
		add("answer-"+tc.id, "actual_memory_context_and_publication", func(ctx context.Context) error {
			r, e := answer.RunPersonalizedAnswer(ctx, tc.style, tc.applicable, nil)
			if e != nil {
				return e
			}
			return require(r.State == "COMPLETED" && r.Answer == tc.text && r.MemorySources == 1 && r.ReadAllocated == 1)
		})
	}
	for _, tc := range []struct {
		mode, record string
		ledger       int64
	}{{"personalized-item", "item", 997}, {"personalized-alternative", "alternative", 995}, {"personalized-inapplicable", "item", 997}} {
		add("action-"+tc.mode, "actual_memory_sdk_invocation_and_independent_target", func(ctx context.Context) error {
			r, e := catalogcheck.RunActionCase(ctx, tc.mode, nil)
			if e != nil {
				return e
			}
			return require(r.State == "COMPLETED" && r.SelectedRecord == tc.record && r.Ledger == tc.ledger && r.Operations == 1 && r.OtherState == "submitted" && r.MemoryReadAllocated == 1 && r.GovernedArtifacts == 4 && r.RevokedArtifacts == 4)
		})
	}
	add("action-personalized-multistep", "actual_two_decisions_with_current_business_facts_and_governed_artifacts", func(ctx context.Context) error {
		r, e := catalogcheck.RunActionCase(ctx, "personalized-multistep", nil)
		if e != nil {
			return e
		}
		return require(r.State == "COMPLETED" && r.FinalState == "authorized" && r.Ledger == 997 && r.OtherState == "submitted" && r.Decisions == 2 && r.Operations == 2 && r.ContextSnapshots == 2 && r.MemoryReadAllocated == 1 && r.GovernedArtifacts == 7 && r.RevokedArtifacts == 7)
	})
	for _, phase := range []string{"model", "publication"} {
		for _, change := range []string{"related", "unrelated", "revoke"} {
			add("answer-"+phase+"-"+change, "actual_memory_mutation_and_publication_boundary", func(ctx context.Context) error {
				r, e := answer.RunPersonalizedInvalidation(ctx, phase, change)
				if e != nil {
					return e
				}
				wanted := change == "unrelated"
				return require(r.Published == wanted && r.ModelCalls == 1 && r.ReadAllocated == 1 && (r.ValidationError == "") == wanted && (phase != "publication" || r.OutputSaved))
			})
		}
	}
	for _, phase := range []string{"admitted", "invoked"} {
		for _, change := range []string{"related", "unrelated", "revoke"} {
			add("action-"+phase+"-"+change, "actual_memory_mutation_and_first_start_boundary", func(ctx context.Context) error {
				r, e := catalogcheck.RunActionCase(ctx, "personalized-"+change+"-"+phase, nil)
				if e != nil {
					return e
				}
				if r.Decisions != 1 || r.Operations != 1 || r.MemoryReadAllocated != 1 || r.OtherState != "submitted" {
					return fmt.Errorf("changed identity, read usage or other target")
				}
				if change == "unrelated" {
					return require(r.State == "COMPLETED" && r.Ledger == 997 && r.StartedInvocations == 1)
				}
				if r.State != "WAITING" || r.Ledger != 1000 || r.FinalState != "submitted" || r.StartedInvocations != 0 {
					return fmt.Errorf("stale proposal caused effects")
				}
				if phase == "admitted" {
					return require(r.ReadyOperations == 1 && r.DispatchedOperations == 0 && r.Invocations == 0)
				}
				return require(r.Invocations == 1 && r.DispatchedOperations == 1)
			})
		}
	}
	for _, point := range []string{"ready", "reference", "dispatched", "output", "missing"} {
		for _, revoke := range []bool{false, true} {
			name := point
			if revoke {
				name += "-revoked"
			}
			add("process-"+name, "actual_child_exit_and_original_memory_host_recovery", func(ctx context.Context) error { return process(ctx, executable, point, revoke) })
		}
	}
	for _, point := range []string{"invoked", "effect", "second-invoked", "second-effect"} {
		for _, revoke := range []bool{false, true} {
			name := "action-process-" + point
			if revoke {
				name += "-revoked"
			}
			add(name, "actual_invocation_process_recovery_and_governed_completion", func(ctx context.Context) error { return actionProcess(ctx, executable, point, revoke) })
		}
	}
	registerReassembly(add)
	for _, point := range []string{"reassembled-invoked", "reassembled-effect", "gap-invoked", "gap-effect", "bound-failure-invoked"} {
		add("action-process-"+point, "actual_reassembly_process_exit_and_original_snapshot_history", func(ctx context.Context) error { return actionProcess(ctx, executable, point, false) })
	}
	return p
}
func require(ok bool) error {
	if !ok {
		return fmt.Errorf("personalized context contract failed")
	}
	return nil
}
func process(ctx context.Context, executable, point string, revoke bool) error {
	root, e := os.MkdirTemp("", "personalized-process-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(root)
	child := exec.CommandContext(ctx, executable, "personalized-answer-crash-probe", root, point)
	output, e := child.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(e, &exit) || exit.ExitCode() != 73 {
		return fmt.Errorf("child did not exit at checkpoint: %v %s", e, output)
	}
	r, e := answer.RestorePersonalizedAnswer(ctx, root, revoke)
	if e != nil {
		return e
	}
	if !r.Restored || r.ReadAllocated != 1 {
		return fmt.Errorf("original snapshot/read identity lost")
	}
	if point == "dispatched" {
		return require(r.ModelCalls == 0 && !r.Published && r.ReservedRequests == 1 && r.UsedRequests == 0 && r.ValidationError != "")
	}
	if revoke {
		return require(r.ModelCalls == 0 && !r.Published && r.ValidationError == "PERMISSION_DENIED")
	}
	text := "Memory, Brain, Execution"
	calls := 1
	if point == "missing" {
		text = "Memory, Brain, Execution (no recorded preference)."
	}
	if point == "reference" {
		text = "The three systems are Memory, Brain, and Execution."
		if !r.ReferenceOnly {
			return fmt.Errorf("reference mode lost")
		}
	}
	if point == "output" {
		calls = 0
	}
	return require(r.Published && r.ModelCalls == calls && r.Answer == text && r.UsedRequests == 1 && r.ReservedRequests == 0 && r.ValidationError == "")
}

func actionProcess(ctx context.Context, executable, point string, revoke bool) error {
	root, e := os.MkdirTemp("", "personalized-action-process-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(root)
	child := exec.CommandContext(ctx, executable, "personalized-action-crash-probe", root, point)
	output, e := child.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(e, &exit) || exit.ExitCode() != 73 {
		return fmt.Errorf("action child did not exit at checkpoint: %v %s", e, output)
	}
	var r catalogcheck.ActionRecoveryReport
	if revoke {
		r, e = catalogcheck.RestorePersonalizedInvocation(ctx, root, true)
	} else {
		r, e = catalogcheck.RestorePersonalizedTask(ctx, root)
	}
	if e != nil {
		return e
	}
	reads, snapshots, absent, corrections, artifacts, ledger := uint64(1), 1, 0, uint32(0), 4, int64(997)
	switch point {
	case "second-invoked", "second-effect":
		snapshots, artifacts = 2, 7
	case "reassembled-invoked", "reassembled-effect":
		reads, snapshots, corrections, ledger = 2, 2, 1, 995
	case "gap-invoked", "gap-effect":
		reads, snapshots, absent, corrections, ledger = 2, 2, 1, 2, 995
	case "bound-failure-invoked":
		reads, snapshots, corrections, ledger = 3, 3, 2, 995
	}
	if !r.OriginalIdentity || r.ReadAllocated != reads || r.OtherState != "submitted" || r.ModelCalls != 0 {
		return fmt.Errorf("action recovery replaced original identity or work")
	}
	invoked := strings.HasSuffix(point, "invoked")
	if r.ContextSnapshots != snapshots || r.AbsentSnapshots != absent || r.Corrections != corrections {
		return fmt.Errorf("original context history changed")
	}
	if !invoked && (!r.WasStarted || r.InitialEffect != "UNKNOWN") {
		return fmt.Errorf("effect checkpoint not unknown")
	}
	if invoked && r.WasStarted {
		return fmt.Errorf("invoked checkpoint already started")
	}
	if revoke && invoked {
		return require(!r.Started && r.Ledger == 1000 && r.State == "submitted" && r.ValidationError != "")
	}
	if !r.Started || r.Ledger != ledger || r.State != "authorized" {
		return fmt.Errorf("lost or repeated business effect")
	}
	if !revoke {
		return require(r.TaskState == "COMPLETED" && r.GovernedArtifacts == artifacts && r.RevokedArtifacts == artifacts && r.Effect == "CONFIRMED" && r.ValidationError == "")
	}
	return nil
}
