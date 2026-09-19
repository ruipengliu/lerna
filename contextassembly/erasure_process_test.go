package contextassembly_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	sqlitecontext "lerna/adapters/context/sqlite"
	memorycleanup "lerna/adapters/memory/cleanup"
	"lerna/contextassembly"
	"lerna/memory"
)

func processInvalidation() contextassembly.SourceInvalidation {
	return contextassembly.SourceInvalidation{Namespace: "local", Collection: "personal", Key: "style", ThroughRevision: 1}
}

func TestProcessExitPreservesContextErasureAndRejectsRebinding(t *testing.T) {
	for _, phase := range []string{"before-cleanup", "after-cleanup", "after-exact-cleanup"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			path := filepath.Join(t.TempDir(), "context.db")
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			child := exec.CommandContext(ctx, executable, "-test.run=^TestContextErasureCrashProbe$")
			child.Env = append(os.Environ(), "HARNESS_ERASURE_PATH="+path, "HARNESS_ERASURE_PHASE="+phase)
			output, err := child.CombinedOutput()
			var exited *exec.ExitError
			if !errors.As(err, &exited) || exited.ExitCode() != 74 {
				t.Fatalf("child exit: %v %s", err, output)
			}
			store, err := sqlitecontext.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			req := processRequest()
			_, err = store.Read(ctx, req.Key)
			if phase == "before-cleanup" && err != nil || phase != "before-cleanup" && err != contextassembly.Invalidated {
				t.Fatalf("recovered cleanup boundary: %v", err)
			}
			err = store.VerifyCheckpoint(ctx, req.Key)
			if phase == "before-cleanup" && err != nil || phase != "before-cleanup" && err != contextassembly.Invalidated {
				t.Fatalf("checkpoint integrity after process exit: %v", err)
			}
			if phase == "after-exact-cleanup" {
				restored, e := contextassembly.New(store, facts{}, &sources{text: processBody, retain: true})
				if e != nil {
					t.Fatal(e)
				}
				late := processRequest()
				late.Key.TaskID = "late-exact-source"
				if _, e = restored.Assemble(ctx, late); e != contextassembly.Invalidated {
					t.Fatalf("exact fence lost after process exit: %v", e)
				}
				late.Candidates[0].Reference.Revision = 2
				if _, e = restored.Assemble(ctx, late); e != nil {
					t.Fatalf("exact fence blocked independent revision: %v", e)
				}
			}
			count, err := store.InvalidateSource(ctx, processInvalidation())
			if err != nil || phase == "before-cleanup" && count != 1 || phase != "before-cleanup" && count != 0 {
				t.Fatalf("cleanup recovery: %d %v", count, err)
			}
			if err = store.VerifyCheckpoint(ctx, req.Key); err != contextassembly.Invalidated {
				t.Fatalf("resumed cleanup retained comparison: %v", err)
			}
			assembly, err := contextassembly.New(store, facts{}, &sources{text: processBody, retain: true})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = assembly.Assemble(ctx, req); err != contextassembly.Invalidated {
				t.Fatalf("old identity resurrected: %v", err)
			}
			req.Key.TaskID = "stale-after-crash"
			if _, err = assembly.Assemble(ctx, req); err != contextassembly.Invalidated {
				t.Fatalf("fresh identity bypassed source watermark: %v", err)
			}
		})
	}
}

// Explicit exit skips Close and all deferred cleanup. The source fixture is
// public deterministic input; storage and restart are real SQLite/WAL operations.
func TestContextErasureCrashProbe(t *testing.T) {
	path := os.Getenv("HARNESS_ERASURE_PATH")
	if path == "" {
		t.Skip("subprocess probe")
	}
	phase := os.Getenv("HARNESS_ERASURE_PHASE")
	if phase != "before-cleanup" && phase != "after-cleanup" && phase != "after-exact-cleanup" {
		t.Fatal("unknown phase")
	}
	store, err := sqlitecontext.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	assembly, err := contextassembly.New(store, facts{}, &sources{text: processBody, retain: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = assembly.Assemble(context.Background(), processRequest()); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Read(context.Background(), processRequest().Key)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.BindCheckpoint(context.Background(), processRequest().Key, sha256.Sum256(snapshot.Document)); err != nil {
		t.Fatal(err)
	}
	sink, err := memorycleanup.NewContexts(store)
	if err != nil {
		t.Fatal(err)
	}
	event := memory.SourceEvent{Ref: memory.Ref{Namespace: "local", Collection: "personal", Key: "style"}, Kind: memory.SourceCreated, Revision: 1, Position: 1}
	if complete, err := sink.Apply(context.Background(), event); err != nil || !complete {
		t.Fatalf("created event: %v %v", complete, err)
	}
	if _, err = store.Read(context.Background(), processRequest().Key); err != nil {
		t.Fatalf("created event removed current source: %v", err)
	}
	if phase != "before-cleanup" {
		event.Kind = memory.SourceDeleted
		event.Revision = 2
		event.Position = 2
		if phase == "after-exact-cleanup" {
			event.Kind = memory.SourceErased
			event.Revision = 1
		}
		if complete, err := sink.Apply(context.Background(), event); err != nil || !complete {
			t.Fatalf("deleted event: %v %v", complete, err)
		}
	}
	os.Exit(74)
}
