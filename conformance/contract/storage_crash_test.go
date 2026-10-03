package contract_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

type crashInput struct {
	Scope                                                            runtime.Scope
	Backend, Location, Phase, Marker, CommandID, TargetID, Principal string
}

// 子进程只跨公开 Store/Tx seam，父进程在真实 COMMIT 边界杀掉写者。
func TestStorageCrashChild(t *testing.T) {
	raw := os.Getenv("HARNESS_STORAGE_CRASH_INPUT")
	if raw == "" {
		t.Skip("invoked only by process interruption contract")
	}
	var input crashInput
	if err := json.Unmarshal([]byte(raw), &input); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	hook := func(phase string) error {
		if phase == input.Phase {
			if err := os.WriteFile(input.Marker, []byte("commit boundary reached"), 0600); err != nil {
				return err
			}
			for {
				time.Sleep(time.Hour)
			}
		}
		return nil
	}
	var store runtime.Store
	if input.Backend == "postgres" {
		s, err := postgres.Open(ctx, input.Location, postgres.WithCommitFault(func(p postgres.CommitPhase) error { return hook(string(p)) }))
		if err != nil {
			t.Fatal(err)
		}
		store = s
	} else {
		s, err := sqlite.Open(input.Location, sqlite.WithCommitFault(func(p sqlite.CommitPhase) error { return hook(string(p)) }))
		if err != nil {
			t.Fatal(err)
		}
		store = s
	}
	defer store.Close()
	if store.ID() != input.Scope.DatabaseID {
		t.Fatal("original database was not recovered")
	}
	original := stored(input.Scope, input.CommandID, input.TargetID, input.Principal)
	_, err := store.Within(ctx, input.Scope, []string{"task"}, func(tx runtime.Tx) error {
		if _, err := tx.LoadCommand(ctx, input.CommandID); !errors.Is(err, runtime.ErrNotFound) {
			return err
		}
		if err := tx.Create(ctx, "task.tasks", input.TargetID, "", testRecord{Value: "original"}); err != nil {
			return err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		if _, err = tx.Raise(ctx, "task.progress", input.TargetID, tx.Scope().Ref(input.TargetID, 1), now); err != nil {
			return err
		}
		return tx.SaveCommand(ctx, original)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Fatal("commit boundary was not reached")
}

func TestStoreSurvivesWriterProcessInterruption(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		for _, phase := range []string{"before_commit", "after_commit"} {
			t.Run(backend+"/"+phase, func(t *testing.T) {
				f := fixture(t, backend, nil)
				input := crashInput{Scope: f.scope, Backend: backend, Location: f.location, Phase: phase, Marker: filepath.Join(t.TempDir(), "boundary"), CommandID: api.NewID("command"), TargetID: api.NewID("task"), Principal: api.NewID("subject")}
				if err := f.store.Close(); err != nil {
					t.Fatal(err)
				}
				encoded, err := json.Marshal(input)
				if err != nil {
					t.Fatal(err)
				}
				cmd := exec.Command(os.Args[0], "-test.run=^TestStorageCrashChild$", "-test.v=false")
				cmd.Env = append(os.Environ(), "HARNESS_STORAGE_CRASH_INPUT="+string(encoded))
				if err = cmd.Start(); err != nil {
					t.Fatal(err)
				}
				wait := make(chan error, 1)
				go func() { wait <- cmd.Wait(); close(wait) }()
				defer func() {
					cmd.Process.Kill()
					select {
					case <-wait:
					case <-time.After(time.Second):
					}
				}()
				deadline := time.NewTimer(10 * time.Second)
				defer deadline.Stop()
				poll := time.NewTicker(5 * time.Millisecond)
				defer poll.Stop()
				for {
					if _, err = os.Stat(input.Marker); err == nil {
						break
					}
					select {
					case err := <-wait:
						t.Fatalf("writer exited before boundary: %v", err)
					case <-deadline.C:
						t.Fatal("writer failed to reach commit boundary")
					case <-poll.C:
					}
				}
				if err = cmd.Process.Kill(); err != nil {
					t.Fatal(err)
				}
				select {
				case err = <-wait:
					if err == nil {
						t.Fatal("writer was not killed")
					}
				case <-time.After(5 * time.Second):
					t.Fatal("killed writer did not exit")
				}
				f.store = f.open(nil)
				ctx := context.Background()
				var value testRecord
				_, recordErr := f.store.Read(ctx, f.scope, "task.tasks", input.TargetID, 0, &value)
				_, commandErr := f.store.LookupCommand(ctx, f.scope, input.CommandID)
				works, status, jobErr := f.store.Claim(ctx, f.scope, api.NewID("boot"), []string{"task.progress"}, 10, time.Second)
				if phase == "before_commit" {
					if !errors.Is(recordErr, runtime.ErrNotFound) || !errors.Is(commandErr, runtime.ErrNotFound) || jobErr != nil || status != runtime.Committed || len(works) != 0 {
						t.Fatalf("uncommitted responsibility escaped process crash: record=%v receipt=%v work=%d status=%s error=%v", recordErr, commandErr, len(works), status, jobErr)
					}
				} else {
					if recordErr != nil || commandErr != nil || value.Value != "original" || jobErr != nil || status != runtime.Committed || len(works) != 1 {
						t.Fatalf("confirmed original lost after writer crash: record=%v receipt=%v work=%d status=%s error=%v", recordErr, commandErr, len(works), status, jobErr)
					}
				}
			})
		}
	}
}
