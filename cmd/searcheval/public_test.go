package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublicRunRejectsOversizeConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{}`+strings.Repeat(" ", 65535)+`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	err := runPublic(t.TempDir(), path, nil)
	if err == nil || err.Error() != "public configuration exceeds 65536 bytes" {
		t.Fatalf("oversized configuration not rejected: %v", err)
	}
}
