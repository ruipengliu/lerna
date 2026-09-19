package sqlite_test

import (
	"context"
	"errors"
	"google.golang.org/protobuf/proto"
	sqliteextraction "lerna/adapters/extraction/sqlite"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestOriginalMemorySaveIntentSurvivesReopenAndCandidateRetirement(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "candidates.db")
	s, err := sqliteextraction.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	r := processRecord(t)
	if err = s.Commit(ctx, r); err != nil {
		t.Fatal(err)
	}
	write := &wire.MemoryWrite{OperationId: "original-memory-operation", Ref: &wire.MemoryRef{Namespace: "local", Collection: "personal", Key: "candidate-original"}, Spec: &wire.MemorySpec{Kind: r.Candidate.Kind, About: r.Candidate.About, Conditions: r.Candidate.Conditions, Sources: r.Candidate.Sources, Confidence: r.Candidate.Confidence, RecordedAt: 1900000000, RetainUntil: 1900001000, PolicyRef: "private", Purpose: "assist", Content: &wire.DynamicPayload{TypeName: "preference", SchemaId: "urn:preference", SchemaVersion: "1", SchemaDigest: "sha256:test", Json: []byte(`{"format":"concise"}`)}}}
	if err = s.ReserveSave(ctx, "local", "alice", "original", write); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = sqliteextraction.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.LookupSave(ctx, "local", "alice", "original")
	if err != nil || got.State != "pending" || !proto.Equal(got.Request, write) {
		t.Fatalf("lost intent: %+v %v", got, err)
	}
	if err = s.ReserveSave(ctx, "local", "alice", "original", write); err != nil {
		t.Fatal(err)
	}
	changed := proto.Clone(write).(*wire.MemoryWrite)
	changed.OperationId = "replacement-memory-operation"
	if err = s.ReserveSave(ctx, "local", "alice", "original", changed); err != memory.IdentityConflict {
		t.Fatalf("replaced intent: %v", err)
	}
	changed = proto.Clone(write).(*wire.MemoryWrite)
	changed.Spec.RecordedAt++
	if err = s.ReserveSave(ctx, "local", "alice", "original", changed); err != memory.IdentityConflict {
		t.Fatalf("changed semantic request: %v", err)
	}
	if _, err = s.LookupSave(ctx, "local", "mallory", "original"); err != memory.Denied {
		t.Fatalf("cross subject: %v", err)
	}
	if err = s.Retire(ctx, "local", "alice", "original"); err != nil {
		t.Fatal(err)
	}
	got, err = s.LookupSave(ctx, "local", "alice", "original")
	if err != nil || got.State != "retired" || got.Request.Spec != nil || got.Request.OperationId != write.OperationId || !proto.Equal(got.Request.Ref, write.Ref) {
		t.Fatalf("retirement lost original identity or retained body: %+v %v", got, err)
	}
	if err = s.ReserveSave(ctx, "local", "alice", "original", write); err != memory.ReplayUnavailable {
		t.Fatalf("revived save intent: %v", err)
	}
}

func TestSaveIntentSurvivesAbruptProcessExit(t *testing.T) {
	for _, mode := range []string{"pending", "retired"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			path := filepath.Join(t.TempDir(), "candidates.db")
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSaveIntentCrashProbe$")
			cmd.Env = append(os.Environ(), "LERNA_SAVE_INTENT_PATH="+path, "LERNA_SAVE_INTENT_MODE="+mode)
			output, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 91 {
				t.Fatalf("probe: %v %s", err, output)
			}
			s, err := sqliteextraction.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			got, err := s.LookupSave(ctx, "local", "alice", "original")
			if err != nil || got.State != mode || got.Request.OperationId != "original-save" || got.Request.Ref.Key != "original" {
				t.Fatalf("lost original intent: %+v %v", got, err)
			}
			if (got.Request.Spec == nil) != (mode == "retired") {
				t.Fatal("wrong retained body state")
			}
		})
	}
}

func TestSaveIntentCrashProbe(t *testing.T) {
	path := os.Getenv("LERNA_SAVE_INTENT_PATH")
	if path == "" {
		t.Skip("child only")
	}
	s, err := sqliteextraction.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err = s.Commit(ctx, processRecord(t)); err != nil {
		t.Fatal(err)
	}
	request := &wire.MemoryWrite{OperationId: "original-save", Ref: &wire.MemoryRef{Namespace: "local", Collection: "personal", Key: "original"}, Spec: &wire.MemorySpec{About: "alice"}}
	if err = s.ReserveSave(ctx, "local", "alice", "original", request); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("LERNA_SAVE_INTENT_MODE") == "retired" {
		if err = s.Retire(ctx, "local", "alice", "original"); err != nil {
			t.Fatal(err)
		}
	}
	os.Exit(91)
}
