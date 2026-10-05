package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// 规则：R6
func TestFormatScopesToProjectFiles(t *testing.T) {
	root := t.TempDir()
	makefile, err := filepath.Abs("../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	run := func(wantFailure bool, args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if (err != nil) != wantFailure {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	write := func(name, body string) {
		t.Helper()
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	run(false, "git", "init", "--quiet")
	write(".gitignore", ".reference/\n")
	write(".reference/ref.go", "not Go code\n")
	write("contracts/gen/go/generated.go", "not Go code\n")
	write("fresh.go", "package fresh;var Value=1\n")
	run(true, "make", "-f", makefile, "check-fmt")
	run(false, "make", "-f", makefile, "fmt")
	run(false, "make", "-f", makefile, "check-fmt")
	for _, name := range []string{".reference/ref.go", "contracts/gen/go/generated.go"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "not Go code\n" {
			t.Fatalf("modified excluded file %s", name)
		}
	}
}
