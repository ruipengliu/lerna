package contextassembly_test

import (
	"bytes"
	"context"
	"errors"
	"lerna/adapters/sqlitecontext"
	"lerna/contextassembly"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const processBody = "Public recovery preference: concise output"

func processRequest() contextassembly.Request {
	return contextassembly.Request{Key: contextassembly.Key{Namespace: "local", TaskID: "process-task", Decision: 1}, Subject: "alice", Purpose: "assist", Location: "model", Storage: "device", FactsVersion: 1, PolicyVersion: "process-v1", MaxBytes: 8192, Candidates: []contextassembly.Candidate{{Reference: contextassembly.Reference{Namespace: "local", Collection: "personal", Key: "style", Revision: 1}, Required: true}}}
}
func TestProcessExitPreservesGovernedContextIdentity(t *testing.T) {
	for _, mode := range []string{"retained", "reference"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			path := filepath.Join(t.TempDir(), "context.db")
			executable, e := os.Executable()
			if e != nil {
				t.Fatal(e)
			}
			child := exec.CommandContext(ctx, executable, "-test.run=^TestContextCrashProbe$")
			child.Env = append(os.Environ(), "HARNESS_CONTEXT_CRASH_PATH="+path, "HARNESS_CONTEXT_CRASH_MODE="+mode)
			output, e := child.CombinedOutput()
			var exited *exec.ExitError
			if !errors.As(e, &exited) || exited.ExitCode() != 73 {
				t.Fatalf("child did not crash at committed context: %v %s", e, output)
			}
			store, e := sqlitecontext.Open(path)
			if e != nil {
				t.Fatal(e)
			}
			defer store.Close()
			req := processRequest()
			before, e := store.Read(ctx, req.Key)
			if e != nil {
				t.Fatal(e)
			}
			if strings.Contains(string(before.Document), processBody) != (mode == "retained") {
				t.Fatal("crash snapshot violated retention policy")
			}
			source := &sources{text: processBody, retain: mode == "retained"}
			assembly, e := contextassembly.New(store, facts{}, source)
			if e != nil {
				t.Fatal(e)
			}
			restored, e := assembly.Assemble(ctx, req)
			if e != nil || len(restored.Input.Blocks) != 1 || restored.Input.Blocks[0].Text != processBody {
				t.Fatalf("restore original context %+v %v", restored, e)
			}
			if mode == "reference" {
				source.text = "unauthorized replacement summary"
				if _, e = assembly.Assemble(ctx, req); e != contextassembly.Invalidated {
					t.Fatalf("reference restore accepted changed body: %v", e)
				}
				source.text = processBody
			}
			source.denied = true
			if _, e = assembly.Assemble(ctx, req); e != contextassembly.Denied {
				t.Fatalf("restart released revoked context: %v", e)
			}
			after, e := store.Read(ctx, req.Key)
			if e != nil {
				t.Fatal(e)
			}
			if before.SemanticSHA256 != after.SemanticSHA256 || !bytes.Equal(before.Document, after.Document) {
				t.Fatal("recovery rewrote original decision")
			}
		})
	}
}

// TestContextCrashProbe runs only in the parent's explicitly configured child.
// os.Exit skips Close/deferred cleanup, exercising committed SQLite/WAL recovery.
func TestContextCrashProbe(t *testing.T) {
	path := os.Getenv("HARNESS_CONTEXT_CRASH_PATH")
	if path == "" {
		t.Skip("subprocess probe")
	}
	mode := os.Getenv("HARNESS_CONTEXT_CRASH_MODE")
	if mode != "retained" && mode != "reference" {
		t.Fatal("unknown recovery mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	store, e := sqlitecontext.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	source := &sources{text: processBody, retain: mode == "retained"}
	assembly, e := contextassembly.New(store, facts{}, source)
	if e != nil {
		t.Fatal(e)
	}
	out, e := assembly.Assemble(ctx, processRequest())
	if e != nil || len(out.Input.Blocks) != 1 || out.Input.Blocks[0].Text != processBody {
		t.Fatalf("commit before exit: %+v %v", out, e)
	}
	os.Exit(73)
}
