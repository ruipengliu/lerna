package filekeys_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"lerna/adapters/credentials/filekeys"
	"lerna/credentials"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

func TestDurableNonceLimitAndRotation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	a, e := filekeys.Open(dir, 16)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	id, e := a.Generate(ctx)
	if e != nil {
		t.Fatal(e)
	}
	b, e := filekeys.Open(dir, 16)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	var wg sync.WaitGroup
	nonces := make(chan string, 16)
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			source := a
			if i%2 != 0 {
				source = b
			}
			v, key, nonce, e := source.Reserve(ctx)
			defer clear(key)
			if e != nil || v != id {
				errs <- e
				return
			}
			nonces <- hex.EncodeToString(nonce)
		}(i)
	}
	wg.Wait()
	close(errs)
	close(nonces)
	for e := range errs {
		t.Fatal(e)
	}
	seen := map[string]bool{}
	for n := range nonces {
		if seen[n] {
			t.Fatal("nonce reuse")
		}
		seen[n] = true
	}
	if len(seen) != 16 {
		t.Fatal("missing reservations")
	}
	if _, _, _, e = a.Reserve(ctx); e != credentials.Exhausted {
		t.Fatal("usage limit bypass", e)
	}
	id2, e := b.Generate(ctx)
	if e != nil || id2 == id {
		t.Fatal("rotation", e)
	}
	old, e := a.Read(ctx, id)
	if e != nil || len(old) != 32 {
		t.Fatal("old key lost before rewrap", e)
	}
	clear(old)
	if _, k, _, e := a.Reserve(ctx); e != nil {
		t.Fatal(e)
	} else {
		clear(k)
	}
	c, e := filekeys.Open(dir, 17)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	if _, _, _, e = c.Reserve(ctx); e != credentials.KeyUnavailable {
		t.Fatal("usage cap changed on reopen", e)
	}
}
func TestUnsafeKeyFilesFailClosed(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	s, e := filekeys.Open(dir, 2)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if _, e = s.Generate(context.Background()); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, "keys.json")
	if e = os.Chmod(path, 0644); e != nil {
		t.Fatal(e)
	}
	if _, _, _, e = s.Reserve(context.Background()); e != credentials.KeyUnavailable {
		t.Fatal("insecure key accepted", e)
	}
}

func TestNonceProcessProbe(t *testing.T) {
	dir := os.Getenv("HARNESS_KEY_PROBE_DIR")
	if dir == "" {
		t.Skip("child only")
	}
	s, e := filekeys.Open(dir, 3)
	if e != nil {
		os.Exit(2)
	}
	_, key, nonce, e := s.Reserve(context.Background())
	clear(key)
	if e != nil {
		os.Exit(3)
	}
	if os.WriteFile(filepath.Join(dir, "observed-nonce"), nonce, 0600) != nil {
		os.Exit(4)
	}
	os.Exit(73)
}
func TestNonceReservationSurvivesProcessExit(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	s, e := filekeys.Open(dir, 3)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if _, e = s.Generate(context.Background()); e != nil {
		t.Fatal(e)
	}
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	child := exec.Command(exe, "-test.run=^TestNonceProcessProbe$")
	child.Env = append(os.Environ(), "HARNESS_KEY_PROBE_DIR="+dir)
	e = child.Run()
	var status *exec.ExitError
	if !errors.As(e, &status) || status.ExitCode() != 73 {
		t.Fatal("child did not exit at reserved nonce", e)
	}
	before, e := os.ReadFile(filepath.Join(dir, "observed-nonce"))
	if e != nil {
		t.Fatal(e)
	}
	_, key, after, e := s.Reserve(context.Background())
	clear(key)
	if e != nil || bytes.Equal(before, after) {
		t.Fatal("nonce reused after process exit", e)
	}
}
