package main_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLocalCLIInitializesWithoutPrintingCredentials(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "authctl")
	if out, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	args := []string{"-db", filepath.Join(dir, "auth.db"), "-credential-file", filepath.Join(dir, "credential"), "-config", "../../examples/local-auth/config.json"}
	initArgs := append([]string{"init"}, args...)
	out, err := exec.Command(binary, initArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("init: %v %s", err, out)
	}
	token, err := os.ReadFile(filepath.Join(dir, "credential"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out, bytes.TrimSpace(token)) {
		t.Fatal("credential printed")
	}
	if out, err = exec.Command(binary, initArgs...).CombinedOutput(); err == nil {
		t.Fatal("reinitialization accepted")
	}
	cmd := exec.Command(binary, append([]string{"call"}, args...)...)
	cmd.Stdin = bytes.NewBufferString(`{"messageId":"m1","namespace":"local","getPolicy":true}`)
	if out, err = cmd.CombinedOutput(); err != nil || !bytes.Contains(out, []byte(`"policy"`)) {
		t.Fatalf("policy query: %v %s", err, out)
	}
}
