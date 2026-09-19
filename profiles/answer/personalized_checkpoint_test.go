package answer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	sqlitecontext "lerna/adapters/context/sqlite"
	"lerna/brain"
	"lerna/contextassembly"
	"lerna/memory"
	"lerna/tasks"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestAnswerHostMigratesLegacyCheckpointAfterDeletion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	limits := tasks.GenerationLimits{Requests: 1, InputTokens: 600, OutputTokens: 100}
	h, err := fresh(ctx, limits)
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	state, err := h.submit(ctx, limits)
	if err != nil {
		t.Fatal(err)
	}
	state, err = start(ctx, h, state)
	if err != nil {
		t.Fatal(err)
	}
	p, err := h.personalize(ctx, state, "concise", true)
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()
	if _, err = p.session.Assemble(ctx, state.Task, h.location, brain.MaxInputBytes); err != nil {
		t.Fatal(err)
	}
	key := contextKey(p.checkpoint)
	snapshot, err := p.snapshots.Read(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(snapshot.Document)
	legacy := *p.checkpoint
	legacy.Format, legacy.SnapshotSHA256 = 1, &digest
	raw, err := json.Marshal(&legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err = p.change(ctx, h, "delete"); err != nil {
		t.Fatal(err)
	}
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for p.snapshots.VerifyCheckpoint(ctx, key) != contextassembly.Invalidated {
		select {
		case <-ctx.Done():
			t.Fatal("host did not retire deleted context")
		case <-tick.C:
		}
	}
	// A legacy writer's already prepared file arrives after source retirement.
	// The running host must erase its comparison without a recovery/model call.
	path := filepath.Join(h.root, "personalized-checkpoint.json")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	for {
		migrated, e := readAnswerCheckpoint(h.root)
		if e != nil {
			t.Fatal(e)
		}
		if migrated.Format == 2 {
			if migrated.SnapshotSHA256 != nil {
				t.Fatal("retired comparison retained")
			}
			expected := legacy
			expected.Format, expected.SnapshotSHA256 = 2, nil
			migrated.fileImage = nil
			if !reflect.DeepEqual(*migrated, expected) {
				t.Fatal("migration changed original task or read identity")
			}
			if e = p.snapshots.VerifyCheckpoint(ctx, key); e != contextassembly.Invalidated {
				t.Fatalf("retired checkpoint resurrected: %v", e)
			}

			position, inspectErr := p.checkpoints.InspectConsumer(ctx, p.checkpointBinding)

			if (inspectErr == nil && position < 2) || inspectErr == memory.Missing {
				select {
				case <-ctx.Done():
					t.Fatal("checkpoint source position did not advance")
				case <-tick.C:
				}
				continue
			}
			if inspectErr != nil || position != 2 {
				t.Fatalf("checkpoint completion was not durably registered: %d %v", position, inspectErr)
			}
			if err = p.cleanup.Close(); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err = p.checkpoints.InspectConsumer(ctx, p.checkpointBinding); err != memory.Missing {
				t.Fatalf("late legacy file reused old completion: %v", err)
			}
			if _, err = p.checkpoints.Run(ctx); err != nil {
				t.Fatal(err)
			}
			if position, err = p.checkpoints.InspectConsumer(ctx, p.checkpointBinding); err != nil || position != 2 {
				t.Fatalf("late file was not reconciled at original position: %d %v", position, err)
			}
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("host left retired comparison in legacy file")
		case <-tick.C:
		}
	}
}

func TestAnswerCheckpointDoesNotDuplicateSnapshotDigest(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	root := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.CommandContext(ctx, executable, "-test.run=^TestPersonalizedAnswerCrashProbe$")
	child.Env = append(os.Environ(), "HARNESS_ANSWER_CONTEXT_PROBE="+root)
	output, err := child.CombinedOutput()
	var exited *exec.ExitError
	if !errors.As(err, &exited) || exited.ExitCode() != 73 {
		t.Fatalf("checkpoint: %v %s", err, output)
	}
	raw, err := os.ReadFile(filepath.Join(root, "personalized-checkpoint.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(`"SnapshotSHA256"`)) {
		t.Fatal("checkpoint duplicated snapshot comparison")
	}
	report, err := RestorePersonalizedAnswer(ctx, root, false)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Restored || !report.Published || report.ModelCalls != 1 || report.ReadAllocated != 1 || report.Answer != "Memory, Brain, Execution" {
		t.Fatalf("recovery changed original decision: %+v", report)
	}
}

func TestAnswerRecoveryRequiresOriginalCheckpointIntegrity(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "legacy-comparison"}[legacy], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			root := t.TempDir()
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			child := exec.CommandContext(ctx, executable, "-test.run=^TestPersonalizedAnswerCrashProbe$")
			child.Env = append(os.Environ(), "HARNESS_ANSWER_CONTEXT_PROBE="+root)
			output, err := child.CombinedOutput()
			var exited *exec.ExitError
			if !errors.As(err, &exited) || exited.ExitCode() != 73 {
				t.Fatalf("checkpoint: %v %s", err, output)
			}
			cp, err := readAnswerCheckpoint(root)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "context.db")
			original, err := sqlitecontext.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := original.Read(ctx, contextKey(cp))
			if err != nil {
				original.Close()
				t.Fatal(err)
			}
			if err = original.Close(); err != nil {
				t.Fatal(err)
			}
			replacement := filepath.Join(root, "replacement.db")
			fresh, err := sqlitecontext.Open(replacement)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = fresh.Bind(ctx, snapshot); err != nil {
				fresh.Close()
				t.Fatal(err)
			}
			if err = fresh.Close(); err != nil {
				t.Fatal(err)
			}
			if err = os.Rename(replacement, path); err != nil {
				t.Fatal(err)
			}
			if legacy {
				digest := sha256.Sum256(snapshot.Document)
				cp.Format = 1
				cp.SnapshotSHA256 = &digest
				raw, err := json.Marshal(cp)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(root, "personalized-checkpoint.json"), raw, 0600); err != nil {
					t.Fatal(err)
				}
				report, err := RestorePersonalizedAnswer(ctx, root, false)
				if err != nil {
					t.Fatal(err)
				}
				if !report.Restored || !report.Published || report.ModelCalls != 1 || report.ReadAllocated != 1 || report.Answer != "Memory, Brain, Execution" {
					t.Fatalf("legacy comparison lost original recovery: %+v", report)
				}
				migrated, err := os.ReadFile(filepath.Join(root, "personalized-checkpoint.json"))
				if err != nil {
					t.Fatal(err)
				}
				if bytes.Contains(migrated, []byte(`"SnapshotSHA256"`)) {
					t.Fatal("legacy recovery left original comparison in file")
				}
				return
			}
			if _, err = RestorePersonalizedAnswer(ctx, root, false); err == nil {
				t.Fatal("recovery invented missing checkpoint integrity")
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
			if current.Task.ModelUsedRequests != 0 || current.Generations[0].Started != 0 || current.Task.Result != "" {
				t.Fatal("missing proof still allowed model work")
			}
		})
	}
}
