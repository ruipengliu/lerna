package sqlite_test

import (
	"context"
	"errors"
	localextraction "lerna/adapters/extraction/rules"
	sqliteextraction "lerna/adapters/extraction/sqlite"
	"lerna/extraction"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func processRecord(t *testing.T) extraction.CandidateRecord {
	t.Helper()
	fragment := "paragraph:1"
	result, err := (localextraction.Rules{}).Extract(context.Background(), extraction.Input{About: "alice", Materials: []extraction.Material{{Speaker: "alice", Text: "回答时，我偏好简洁的说明。", Source: &wire.MemorySource{Ref: &wire.ContentSource{Kind: "note", Key: "one", Revision: 1}, Method: "authenticated-note", Fragment: &fragment}}}})
	if err != nil || len(result.Candidates) != 1 {
		t.Fatalf("extraction: %+v %v", result, err)
	}
	return extraction.CandidateRecord{Namespace: "local", Subject: "alice", OperationID: "original", Candidate: result.Candidates[0], Restrictions: extraction.Restrictions{Storage: []string{"device-a"}, Processing: []string{"device-a"}, Recipients: []string{"device-a"}, Purposes: []string{"assist"}, RetainUntil: 1900001000}}
}
func TestCandidateStateSurvivesProcessExit(t *testing.T) {
	for _, mode := range []string{"before-commit", "after-commit", "after-retire", "retire-before-commit"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			path := filepath.Join(t.TempDir(), "candidate.db")
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCandidateCrashProbe$")
			cmd.Env = append(os.Environ(), "LERNA_CANDIDATE_CRASH_PATH="+path, "LERNA_CANDIDATE_CRASH_MODE="+mode)
			output, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 73 {
				t.Fatalf("crash probe: %v %s", err, output)
			}
			s, err := sqliteextraction.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			r := processRecord(t)
			state, err := s.Inspect(ctx, "local", "original")
			switch mode {
			case "before-commit":
				if err != memory.Missing {
					t.Fatalf("invented commit: %+v %v", state, err)
				}
				if err = s.Commit(ctx, r); err != nil {
					t.Fatal(err)
				}
			case "after-commit":
				if err != nil || state.State != "retained" || !state.Committed {
					t.Fatalf("lost commit: %+v %v", state, err)
				}
				if err = s.Commit(ctx, r); err != nil {
					t.Fatal(err)
				}
				got, e := s.Lookup(ctx, "local", "original")
				if e != nil || got.Candidate.Value != "concise" {
					t.Fatalf("lost body: %+v %v", got, e)
				}
			default:
				if err != nil || state.State != "retired" || state.Committed != (mode == "after-retire") {
					t.Fatalf("wrong original effect: %+v %v", state, err)
				}
				if _, err = s.Lookup(ctx, "local", "original"); err != memory.Missing {
					t.Fatalf("retired body: %v", err)
				}
				if err = s.Commit(ctx, r); err != memory.ReplayUnavailable {
					t.Fatalf("revived body: %v", err)
				}
				if err = s.Retire(ctx, "local", "alice", "original"); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
func TestCandidateCrashProbe(t *testing.T) {
	path := os.Getenv("LERNA_CANDIDATE_CRASH_PATH")
	if path == "" {
		t.Skip("child only")
	}
	mode := os.Getenv("LERNA_CANDIDATE_CRASH_MODE")
	s, err := sqliteextraction.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if mode == "before-commit" {
		os.Exit(73)
	}
	if mode == "retire-before-commit" {
		if err = s.Retire(ctx, "local", "alice", "original"); err != nil {
			t.Fatal(err)
		}
		os.Exit(73)
	}
	if err = s.Commit(ctx, processRecord(t)); err != nil {
		t.Fatal(err)
	}
	if mode == "after-commit" {
		os.Exit(73)
	}
	if mode != "after-retire" {
		t.Fatal("unknown mode")
	}
	if err = s.Retire(ctx, "local", "alice", "original"); err != nil {
		t.Fatal(err)
	}
	os.Exit(73)
}
