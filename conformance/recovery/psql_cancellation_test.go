//go:build integration

package recovery_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type psqlReadyOutput struct {
	sync.Mutex
	data  bytes.Buffer
	ready chan struct{}
	once  sync.Once
}

func (output *psqlReadyOutput) Write(data []byte) (int, error) {
	output.Lock()
	defer output.Unlock()
	n, err := output.data.Write(data)
	if strings.Contains(output.data.String(), "lerna_psql_ready") {
		output.once.Do(func() { close(output.ready) })
	}
	return n, err
}

// This tests the configured client: native psql locally, the explicit container
// launcher in CI. The actual process first proves its SQL connection works.
func TestPsqlClientCancellationReapsProcessAfterSuccessfulConnection(t *testing.T) {
	psql, err := exec.LookPath("psql")
	if err != nil {
		t.Fatal("psql required")
	}
	environment, err := historicalPsqlEnvironment(os.Getenv("LERNA_TEST_POSTGRES_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cidfile := filepath.Join(t.TempDir(), "container.cid")
	args := []string{"-X", "-v", "ON_ERROR_STOP=1", "--tuples-only", "--no-align", "--command", "SELECT 'lerna_psql_ready'", "--command", "SELECT pg_sleep(60)"}
	command := boundedPsqlCommand(ctx, psql, args, environment, cidfile)
	output := &psqlReadyOutput{ready: make(chan struct{})}
	command.Stdout = output
	command.Stderr = &bytes.Buffer{}
	if err = command.Start(); err != nil {
		t.Fatal("client start failed")
	}
	answer := make(chan error, 1)
	go func() { answer <- command.Wait() }()
	joined := false
	defer func() {
		cancel()
		if !joined {
			select {
			case <-answer:
			case <-time.After(2 * time.Second):
				t.Error("owned client process did not terminate")
			}
		}
		if err := cleanupHistoricalPsqlContainer(cidfile); err != nil {
			t.Error(err)
		}
	}()
	select {
	case <-output.ready:
	case err := <-answer:
		joined = true
		t.Fatalf("client exited before successful connection: %v", err)
	case <-ctx.Done():
		t.Fatal("finite client connection synchronization deadline")
	}
	cancel()
	select {
	case err := <-answer:
		joined = true
		if err == nil {
			t.Fatal("cancelled blocking client returned success")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled client exceeded finite process exit bound")
	}
	// Wait has reaped precisely the child PID created above; no process-name scan.
	if err = command.Process.Signal(syscall.Signal(0)); !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("client process not reaped: %v", err)
	}
}
