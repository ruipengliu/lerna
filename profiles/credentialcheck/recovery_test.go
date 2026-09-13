package credentialcheck

import (
	"bytes"
	"context"
	"encoding/json"
	"lerna/adapters/credentialauth"
	"lerna/adapters/filekeys"
	"lerna/adapters/sqliteauth"
	"lerna/adapters/sqlitecredentials"
	"lerna/authorization"
	"lerna/credentials"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

type recoveryProbe struct {
	Root, Token string
	Binding     credentials.Binding
	Now         time.Time
}

// The child deliberately exits without closing databases, after only the first
// record has moved to the new key. The parent must recover the mixed key set.
func TestRewrapProcessProbe(t *testing.T) {
	path := os.Getenv("HARNESS_REWRAP_PROBE")
	if path == "" {
		return
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	var p recoveryProbe
	if e = json.Unmarshal(raw, &p); e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	db, e := sqliteauth.Open(filepath.Join(p.Root, "auth.db"))
	if e != nil {
		t.Fatal(e)
	}
	snap, e := db.Load(ctx)
	if e != nil {
		t.Fatal(e)
	}
	c := &clock{t: p.Now}
	auth, e := authorization.New(db, c, snap.State.Config)
	if e != nil {
		t.Fatal(e)
	}
	a, e := credentialauth.New(auth, "root")
	if e != nil {
		t.Fatal(e)
	}
	keys, e := filekeys.Open(filepath.Join(p.Root, "keys"), filekeys.MaxUses)
	if e != nil {
		t.Fatal(e)
	}
	store, e := sqlitecredentials.Open(filepath.Join(p.Root, "credentials.db"))
	if e != nil {
		t.Fatal(e)
	}
	broker, e := credentials.New(store, keys, a, c, credentials.Config{Timeout: time.Second, MaxConcurrent: 4})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = keys.Generate(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = broker.Rewrap(ctx, p.Token, "credential", p.Binding, 1); e != nil {
		t.Fatal(e)
	}
	os.Exit(73)
}

func reopenCredentials(t *testing.T, f *fixture) {
	t.Helper()
	var e error
	f.store, e = sqlitecredentials.Open(filepath.Join(f.root, "credentials.db"))
	if e != nil {
		t.Fatal(e)
	}
	f.keys, e = filekeys.Open(filepath.Join(f.root, "keys"), filekeys.MaxUses)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.rebind(); e != nil {
		t.Fatal(e)
	}
}

func TestPartialRewrapSurvivesProcessExit(t *testing.T) {
	ctx := context.Background()
	f, e := newFixture(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer f.close()
	now, _ := f.clock.Now()
	if _, e = f.broker.Put(ctx, f.token, "second", f.binding, 0, now.Add(time.Hour).Unix(), []byte(fixtureSecret)); e != nil {
		t.Fatal(e)
	}
	old, e := f.store.Get(ctx, "second")
	if e != nil {
		t.Fatal(e)
	}
	raw, e := json.Marshal(recoveryProbe{Root: f.root, Token: f.token, Binding: f.binding, Now: now})
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(f.root, "probe.json")
	if e = os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
	f.store.Close()
	f.keys.Close()
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(bounded, os.Args[0], "-test.run=^TestRewrapProcessProbe$")
	cmd.Env = append(os.Environ(), "HARNESS_REWRAP_PROBE="+path)
	err := cmd.Run()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 73 {
		t.Fatalf("child did not reach deliberate interruption: %v", err)
	}
	reopenCredentials(t, f)
	first, e := f.store.Get(ctx, "credential")
	if e != nil {
		t.Fatal(e)
	}
	second, e := f.store.Get(ctx, "second")
	if e != nil {
		t.Fatal(e)
	}
	if first.Revision != 2 || first.KeyVersion == old.KeyVersion || second.Revision != 1 || second.KeyVersion != old.KeyVersion {
		t.Fatal("partial migration not preserved")
	}
	for _, ref := range []string{"credential", "second"} {
		if e = f.use(ctx, ref, "before-"+ref); e != nil {
			t.Fatal(e)
		}
	}
	moved, e := f.broker.Rewrap(ctx, f.token, "second", f.binding, 1)
	if e != nil || moved.KeyVersion != first.KeyVersion {
		t.Fatal("cannot resume partial migration", e)
	}
	for _, ref := range []string{"credential", "second"} {
		if e = f.use(ctx, ref, "after-"+ref); e != nil {
			t.Fatal(e)
		}
	}
	if value, e := f.value(ctx); e != nil || value != 12 {
		t.Fatal("independent effects mismatch", e)
	}
}

func TestDatabaseRollbackDoesNotReuseNonce(t *testing.T) {
	ctx := context.Background()
	f, e := newFixture(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer f.close()
	original, e := f.store.Get(ctx, "credential")
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(f.root, "credentials.db")
	f.store.Close()
	f.keys.Close()
	backup, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	reopenCredentials(t, f)
	if _, e = f.broker.Rewrap(ctx, f.token, "credential", f.binding, 1); e != nil {
		t.Fatal(e)
	}
	before, e := f.store.Get(ctx, "credential")
	if e != nil {
		t.Fatal(e)
	}
	f.store.Close()
	f.keys.Close()
	if e = os.WriteFile(path, backup, 0600); e != nil {
		t.Fatal(e)
	}
	reopenCredentials(t, f)
	restored, e := f.store.Get(ctx, "credential")
	if e != nil || restored.Revision != 1 {
		t.Fatal("backup not restored", e)
	}
	// Trusted restore reconciliation rewraps before enabling use; the independent
	// key directory has not been rolled back with the database.
	if _, e = f.broker.Rewrap(ctx, f.token, "credential", f.binding, 1); e != nil {
		t.Fatal(e)
	}
	after, e := f.store.Get(ctx, "credential")
	if e != nil {
		t.Fatal(e)
	}
	if original.KeyVersion != before.KeyVersion || before.KeyVersion != after.KeyVersion ||
		bytes.Equal(original.Nonce, before.Nonce) || bytes.Equal(before.Nonce, after.Nonce) || bytes.Equal(original.Nonce, after.Nonce) {
		t.Fatal("nonce was reused across database rollback")
	}
	if e = f.use(ctx, "credential", "restored"); e != nil {
		t.Fatal(e)
	}
	if value, e := f.value(ctx); e != nil || value != 3 {
		t.Fatal("restored credential unusable", e)
	}
}
