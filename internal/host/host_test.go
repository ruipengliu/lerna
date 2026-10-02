package host

import (
	"context"
	"database/sql"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestDependencyHealthNeverOpensBusinessAdmission(t *testing.T) {
	root := t.TempDir()
	state := &serverState{
		config: Config{Role: "worker", InstanceID: "test-worker", ContentRoot: root, ManagedRoot: root, ProbeTimeout: "1s"},
		ping:   func(context.Context) error { return nil },
	}
	check := func(path string, code int) {
		t.Helper()
		response := httptest.NewRecorder()
		state.handler().ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		if response.Code != code {
			t.Fatalf("%s returned %d, want %d: %s", path, response.Code, code, response.Body.String())
		}
	}
	check("/health/live", 200)
	check("/health/startup", 200)
	check("/health/ready", 503)
	check("/v1/connect", 501)
	state.ping = func(context.Context) error { return errors.New("database unavailable") }
	check("/health/live", 200)
	check("/health/startup", 503)
	check("/health/ready", 503)
	state.ping = func(context.Context) error { return nil }
	state.draining.Store(true)
	check("/health/startup", 503)
	if state.status(context.Background()).AcceptingTasks {
		t.Fatal("skeleton opened business admission")
	}
}

func TestSQLiteLockAndPersistenceSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "harness.db")
	config := DatabaseConfig{Driver: "sqlite", Path: path, MaxConnections: 1}
	database, err := openDatabase(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.ping(context.Background()); err != nil {
		t.Fatalf("SQLite persistence settings were not applied: %v", err)
	}
	second, err := openDatabase(context.Background(), config)
	if err == nil {
		second.close()
		database.close()
		t.Fatal("second host acquired the same SQLite database lock")
	}
	// An independent read observes the actual persisted journal mode.
	reader, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	var mode string
	if err := reader.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("SQLite journal mode = %q, error = %v", mode, err)
	}
	reader.Close()
	database.close()
	replacement, err := openDatabase(context.Background(), config)
	if err != nil {
		t.Fatalf("lock was not released: %v", err)
	}
	replacement.close()
}

func TestConfigurationRejectsSharedSQLiteAndPublicManagement(t *testing.T) {
	config, err := LoadConfig("../../dev/config/single.json")
	if err != nil {
		t.Fatal(err)
	}
	config.Role = "worker"
	if config.validate() == nil {
		t.Fatal("accepted SQLite for a multi-process worker")
	}
	config.Role = "all"
	config.AdminAddr = "0.0.0.0:18080"
	if config.validate() == nil {
		t.Fatal("accepted an unrestricted development management address")
	}
}
