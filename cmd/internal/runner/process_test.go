package runner_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/cmd/internal/bootstrap"
)

func binary(t *testing.T, role string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "harness-"+role)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", path, "./cmd/"+role)
	cmd.Dir = "../../.."
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v %s", role, err, output)
	}
	return path
}

func run(t *testing.T, executable string, args ...string) ([]byte, []byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, args...)
	var out, diagnostic strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &diagnostic
	err := cmd.Run()
	return []byte(out.String()), []byte(diagnostic.String()), err
}

func TestManagementInitializeIsExplicitAndDoesNotResetIdentity(t *testing.T) {
	migrate := binary(t, "migrate")
	root := t.TempDir()
	config := filepath.Join(root, "config.json")
	args := []string{"--config", config, "--development-init", "--data", root, "--driver", "sqlite"}
	out, diagnostic, err := run(t, migrate, args...)
	if err != nil {
		t.Fatalf("initialize: %v %s", err, diagnostic)
	}
	var result struct {
		DatabaseID  string `json:"database_id"`
		Driver      string `json:"driver"`
		Initialized bool   `json:"initialized"`
		Status      string `json:"status"`
	}
	if err = json.Unmarshal(out, &result); err != nil || result.DatabaseID == "" || result.Driver != "sqlite" || !result.Initialized || result.Status != "migrated" {
		t.Fatalf("management result: %s %v", out, err)
	}
	c, err := bootstrap.LoadConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	token, err := os.ReadFile(c.TokenFile)
	if err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	_, diagnostic, err = run(t, migrate, args...)
	if err != nil {
		t.Fatalf("repeat initialize: %v %s", err, diagnostic)
	}
	second, err := os.ReadFile(config)
	if err != nil || string(first) != string(second) {
		t.Fatal("initialization rewrote fixed owner/policy/credentials config")
	}
	secondToken, err := os.ReadFile(c.TokenFile)
	if err != nil || string(token) != string(secondToken) {
		t.Fatal("initialization reset original credential")
	}
	_, diagnostic, err = run(t, migrate, "--config", config)
	if err != nil {
		t.Fatalf("ordinary migration: %v %s", err, diagnostic)
	}
	if strings.Contains(string(diagnostic), string(token)) {
		t.Fatal("credential leaked in diagnostic")
	}
}
