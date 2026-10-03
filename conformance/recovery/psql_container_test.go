//go:build integration && containerpsql

package recovery_test

import (
	"context"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This explicit CI-client suite requires Docker; native-client integration
// remains usable without it. Both cases identify only their successful CREATE.
func TestContainerPsqlNormalAndCancellationLeaveNoOwnedContainer(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatal("docker is required for the container psql lifecycle suite")
	}
	launcher, err := filepath.Abs("../../scripts/ci-psql.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, cancelled := range []bool{false, true} {
		name := "Normal"
		if cancelled {
			name = "CancelRunning"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cidfile := filepath.Join(t.TempDir(), "container.cid")
			environment := os.Environ()
			arguments := []string{"--version"}
			if cancelled {
				environment, err = historicalPsqlEnvironment(os.Getenv("LERNA_TEST_POSTGRES_DSN"))
				if err != nil {
					t.Fatal(err)
				}
				arguments = []string{"-X", "-v", "ON_ERROR_STOP=1", "--command", "SELECT pg_sleep(60)"}
			}
			type result struct {
				output []byte
				err    error
			}
			answer := make(chan result, 1)
			go func() {
				output, err := runHistoricalPsqlWithCID(ctx, launcher, arguments, environment, cidfile)
				answer <- result{output, err}
			}()
			completed := false
			defer func() {
				cancel()
				if !completed {
					select {
					case <-answer:
					case <-time.After(12 * time.Second):
						t.Error("client cleanup did not finish within its finite bound")
					}
				}
			}()
			if cancelled {
				ticker := time.NewTicker(20 * time.Millisecond)
				defer ticker.Stop()
				running := false
				for !running {
					select {
					case <-ctx.Done():
						t.Fatal("finite container synchronization deadline")
					case value := <-answer:
						completed = true
						t.Fatalf("client exited before cancellation: %v", value.err)
					case <-ticker.C:
					}
					data, err := os.ReadFile(cidfile)
					if errors.Is(err, os.ErrNotExist) {
						continue
					}
					if err != nil {
						t.Fatal(err)
					}
					id := strings.TrimSpace(string(data))
					if id == "" {
						continue
					} // Docker reserves its cidfile before CREATE returns.
					decoded, err := hex.DecodeString(id)
					if err != nil || len(decoded) != 32 {
						t.Fatal("successful CREATE did not record exact ID")
					}
					probe, probeCancel := context.WithTimeout(ctx, 3*time.Second)
					output, err := exec.CommandContext(probe, "docker", "inspect", "--format", "{{.State.Running}}", id).Output()
					probeCancel()
					if err != nil {
						t.Fatal("cannot observe this created container running")
					}
					running = strings.TrimSpace(string(output)) == "true"
				}
				// Real external-process cancellation happens after positive running proof.
				cancel()
			}
			observed := <-answer
			completed = true
			if cancelled && observed.err == nil {
				t.Fatal("cancelled blocking psql reported success")
			}
			if !cancelled && (observed.err != nil || !strings.Contains(string(observed.output), "psql (PostgreSQL) 18.6")) {
				t.Fatalf("normal immutable client failed: %v", observed.err)
			}
			if _, err := os.ReadFile(cidfile); err != nil {
				t.Fatal("client never registered its successful CREATE ID")
			}
			if err := cleanupHistoricalPsqlContainer(cidfile); err != nil {
				t.Fatal(err)
			}
		})
	}
}
