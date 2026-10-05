package main_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 规则：G3、R6
func TestRequiredPackagesNeedAnnotatedTest(t *testing.T) {
	root := t.TempDir()
	run := func(wantFailure bool) {
		t.Helper()
		cmd := exec.Command("go", "run", "main.go", root)
		output, err := cmd.CombinedOutput()
		if (err != nil) != wantFailure {
			t.Fatalf("failure=%v: %v\n%s", wantFailure, err, output)
		}
		if wantFailure && !strings.Contains(string(output), "core/durable") {
			t.Fatalf("missing package diagnostic: %s", output)
		}
	}
	run(false)
	dir := filepath.Join(root, "core/durable")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("store.go", "package durable\n")
	run(true)
	write("store_test.go", "package durable\n// 规则：G3\nvar unrelated = true\nfunc TestStore(t *testing.T) {}\n")
	run(true)
	write("store_test.go", "package durable\n// 规则：G3\nfunc TestHelper() {}\n")
	run(true)
	write("store_test.go", "package durable\n// 规则：G3\nfunc TestStore(t *testing.T) {}\n")
	run(false)
}
