//go:build integration

package recovery_test

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgconn"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The immutable container client writes this invocation's successful CREATE ID.
// Native psql ignores the variable. No cleanup is inferred from image or name.
func runHistoricalPsql(t *testing.T, ctx context.Context, path string, args []string, environment []string) ([]byte, error) {
	t.Helper()
	cidfile := filepath.Join(t.TempDir(), "container.cid")
	return runHistoricalPsqlWithCID(ctx, path, args, environment, cidfile)
}
func runHistoricalPsqlWithCID(ctx context.Context, path string, args []string, environment []string, cidfile string) ([]byte, error) {
	command := boundedPsqlCommand(ctx, path, args, environment, cidfile)
	output, err := command.CombinedOutput()
	return output, errors.Join(err, cleanupHistoricalPsqlContainer(cidfile))
}
func boundedPsqlCommand(ctx context.Context, path string, args []string, environment []string, cidfile string) *exec.Cmd {
	command := exec.CommandContext(ctx, path, args...)
	for _, entry := range environment {
		if !strings.HasPrefix(entry, "LERNA_PSQL_CIDFILE=") {
			command.Env = append(command.Env, entry)
		}
	}
	command.Env = append(command.Env, "LERNA_PSQL_CIDFILE="+cidfile)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if err == syscall.ESRCH {
			return os.ErrProcessDone
		}
		return err
	}
	command.WaitDelay = time.Second
	return command
}
func cleanupHistoricalPsqlContainer(cidfile string) error {
	data, err := os.ReadFile(cidfile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.New("cannot read owned psql container ID")
	}
	id := strings.TrimSpace(string(data))
	bytes, err := hex.DecodeString(id)
	if err != nil || len(bytes) != 32 || strings.ToLower(id) != id {
		return errors.New("invalid owned psql container ID")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// --rm may already have removed a normally exited container; only an actual
	// successful list of this exact ID can establish its final absence.
	exec.CommandContext(ctx, "docker", "rm", "--force", id).Run()
	output, err := exec.CommandContext(ctx, "docker", "container", "ls", "--all", "--no-trunc", "--filter", "id="+id, "--format", "{{.ID}}").Output()
	if err != nil {
		return errors.New("cannot confirm owned psql container cleanup")
	}
	if strings.TrimSpace(string(output)) != "" {
		return fmt.Errorf("owned psql container remains: %s", id)
	}
	return nil
}

func historicalPsqlEnvironment(dsn string) ([]string, error) {
	configuration, err := pgconn.ParseConfig(dsn)
	if err != nil {
		return nil, errors.New("invalid dedicated psql configuration")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		return nil, errors.New("historical psql requires a postgres URI DSN")
	}
	variables := map[string]string{"PGHOST": configuration.Host, "PGPORT": strconv.Itoa(int(configuration.Port)), "PGUSER": configuration.User, "PGPASSWORD": configuration.Password, "PGDATABASE": configuration.Database, "PGCONNECT_TIMEOUT": "5"}
	for key, value := range parsed.Query() {
		if key == "sslmode" || key == "sslrootcert" || key == "sslcert" || key == "sslkey" {
			variables["PG"+strings.ToUpper(key)] = value[len(value)-1]
		}
	}
	var environment []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(key, "PG") && key != "LERNA_TEST_POSTGRES_DSN" {
			environment = append(environment, entry)
		}
	}
	for key, value := range variables {
		environment = append(environment, key+"="+value)
	}
	return environment, nil
}
