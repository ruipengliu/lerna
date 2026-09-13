package sqlitememory_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"lerna/adapters/recoveryproof"
	"lerna/adapters/sqlitememory"
	"lerna/memory"
)

// Public synthetic test key, supplied by the maintenance host, never recovered
// from the backup. Production keys must be independently provisioned.
func processRecoveryBinding(t *testing.T, s *sqlitememory.Store) memory.RecoveryBinding {
	t.Helper()
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{19}, ed25519.SeedSize))
	scope := memory.RecoveryScope{Namespace: "local", Collection: "personal"}
	source, e := recoveryproof.NewIssuer(s, recoveryproof.IssuerConfig{Authority: "process-test", Epoch: 1, Scope: scope, Key: key})
	if e != nil {
		t.Fatal(e)
	}
	verifier, e := recoveryproof.NewVerifier(recoveryproof.VerifierConfig{Authority: "process-test", Epoch: 1, Scope: scope, Key: key.Public().(ed25519.PublicKey), MinPosition: 516})
	if e != nil {
		t.Fatal(e)
	}
	return memory.RecoveryBinding{Scope: scope, Source: source, Verifier: verifier}
}

func TestRestorePagingAndCleanupSurviveProcessExit(t *testing.T) {
	for _, exact := range []bool{false, true} {
		t.Run(fmt.Sprintf("exact=%v", exact), func(t *testing.T) { checkRestoreProcess(t, exact) })
	}
}

func checkRestoreProcess(t *testing.T, exact bool) {
	root := t.TempDir()
	path := filepath.Join(root, "current.db")
	current, e := sqlitememory.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { current.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	old := memory.Ref{Namespace: "local", Collection: "personal", Key: "old"}
	for revision := uint64(1); revision <= 512; revision++ {
		_, e = current.Commit(ctx, memory.Change{OperationID: fmt.Sprintf("old-%d", revision), Subject: "operator", Expected: revision - 1, SemanticSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Record: memory.Revision{Ref: old, Revision: revision, Document: []byte(`{"value":"old"}`)}})
		if e != nil {
			t.Fatal(e)
		}
	}
	_, e = current.Delete(ctx, memory.Deletion{OperationID: "delete-old", Subject: "operator", SemanticSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Ref: old, Expected: 512})
	if e != nil {
		t.Fatal(e)
	}
	for _, key := range []string{"removed", "retained"} {
		ref := memory.Ref{Namespace: "local", Collection: "personal", Key: key}
		_, e = current.Commit(ctx, memory.Change{OperationID: "put-" + key, Subject: "operator", SemanticSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Record: memory.Revision{Ref: ref, Revision: 1, Document: []byte(fmt.Sprintf(`{"spec":{"sources":[{"ref":{"kind":"note","key":%q,"revision":"1"}}]}}`, key))}})
		if e != nil {
			t.Fatal(e)
		}
	}
	if e = current.Close(); e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	backup := filepath.Join(root, "backup.db")
	if e = os.WriteFile(backup, raw, 0600); e != nil {
		t.Fatal(e)
	}
	current, e = sqlitememory.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	if exact {
		_, e = current.EraseSource(ctx, memory.SourceErasure{Namespace: "local", Kind: "note", Key: "removed", ThroughRevision: 1})
	} else {
		_, e = current.Delete(ctx, memory.Deletion{OperationID: "delete-removed", Subject: "operator", SemanticSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Ref: memory.Ref{Namespace: "local", Collection: "personal", Key: "removed"}, Expected: 1})
	}
	if e != nil {
		t.Fatal(e)
	}
	executable, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	for _, phase := range []string{"page", "clean"} {
		child := exec.CommandContext(ctx, executable, "-test.run=^TestRestoreReconcileCrashProbe$")
		child.Env = append(os.Environ(), "HARNESS_RECOVERY_PROCESS_ROOT="+root, "HARNESS_RECOVERY_PROCESS_PHASE="+phase)
		output, e := child.CombinedOutput()
		var exited *exec.ExitError
		if !errors.As(e, &exited) || exited.ExitCode() != 71 {
			t.Fatalf("%s process: %v %s", phase, e, output)
		}
		restored, e := sqlitememory.OpenRestored(backup)
		if e != nil {
			t.Fatal(e)
		}
		p, e := restored.InspectRecovery(ctx, memory.RecoveryScope{Namespace: "local", Collection: "personal"})
		restored.Close()
		if e != nil {
			t.Fatal(e)
		}
		if phase == "page" && (p.State != "verifying" || p.VerifiedPosition != 512 || p.OriginPosition != 515 || p.AppliedPosition != 0) {
			t.Fatalf("history progress lost: %+v", p)
		}
		if phase == "clean" && (p.State != "sanitized" || p.VerifiedPosition != 515 || p.AppliedPosition != 516 || p.Retained != 1 || p.Missing != 0) {
			t.Fatalf("cleanup lost: %+v", p)
		}
	}
	restored, e := sqlitememory.OpenRestored(backup)
	if e != nil {
		t.Fatal(e)
	}
	defer restored.Close()
	binding := processRecoveryBinding(t, current)
	retained := memory.VersionRef{Ref: memory.Ref{Namespace: "local", Collection: "personal", Key: "retained"}, Revision: 1}
	got, e := restored.ReadVerified(ctx, binding, retained)
	if e != nil || string(got.Document) != `{"spec":{"sources":[{"ref":{"kind":"note","key":"retained","revision":"1"}}]}}` {
		t.Fatalf("retained data after crash: %v", e)
	}
	retained.Ref.Key = "removed"
	if got, e = restored.ReadVerified(ctx, binding, retained); e != memory.Missing || len(got.Document) != 0 {
		t.Fatalf("deleted data returned after crash: %v", e)
	}
}

func TestRestoreReconcileCrashProbe(t *testing.T) {
	root := os.Getenv("HARNESS_RECOVERY_PROCESS_ROOT")
	if root == "" {
		t.Skip("subprocess probe")
	}
	current, e := sqlitememory.Open(filepath.Join(root, "current.db"))
	if e != nil {
		t.Fatal(e)
	}
	restored, e := sqlitememory.OpenRestored(filepath.Join(root, "backup.db"))
	if e != nil {
		t.Fatal(e)
	}
	binding := processRecoveryBinding(t, current)
	p, e := restored.Reconcile(context.Background(), binding)
	if e != nil {
		t.Fatal(e)
	}
	phase := os.Getenv("HARNESS_RECOVERY_PROCESS_PHASE")
	if phase == "page" && (p.State != "verifying" || p.VerifiedPosition != 512) {
		t.Fatalf("first bounded page: %+v", p)
	}
	if phase == "clean" && p.State != "sanitized" {
		t.Fatalf("resume did not reach cleanup: %+v", p)
	}
	os.Exit(71)
}
