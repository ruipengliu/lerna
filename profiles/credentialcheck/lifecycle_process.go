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
	"strings"
	"time"
)

type lifecycleProbeConfig struct {
	Root, Token string
	Binding     credentials.Binding
	Now         time.Time
}
type lifecycleMarker struct {
	Version string
	Nonce   []byte
}

func exitLifecycle(root string, m lifecycleMarker) {
	raw, e := json.Marshal(m)
	if e != nil {
		os.Exit(74)
	}
	f, e := os.OpenFile(filepath.Join(root, "phase.json"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if e != nil {
		os.Exit(74)
	}
	if _, e = f.Write(raw); e != nil {
		os.Exit(74)
	}
	if f.Sync() != nil {
		os.Exit(74)
	}
	f.Close()
	os.Exit(73)
}

type crashingLifecycleKeys struct {
	credentials.KeyLifecycle
	root, mode string
}

func (k *crashingLifecycleKeys) Prepare(ctx context.Context, op string) (string, error) {
	id, e := k.KeyLifecycle.Prepare(ctx, op)
	if e == nil && k.mode == "after-prepare" {
		exitLifecycle(k.root, lifecycleMarker{Version: id})
	}
	return id, e
}
func (k *crashingLifecycleKeys) Activate(ctx context.Context, op, id string) error {
	if k.mode == "before-activate" {
		exitLifecycle(k.root, lifecycleMarker{Version: id})
	}
	e := k.KeyLifecycle.Activate(ctx, op, id)
	if e == nil && k.mode == "after-activate" {
		exitLifecycle(k.root, lifecycleMarker{Version: id})
	}
	return e
}

type crashingLifecycleStore struct {
	credentials.LifecycleStore
	root, mode string
}

func (s *crashingLifecycleStore) CommitLifecycle(ctx context.Context, expected uint64, c credentials.LifecycleChange) error {
	if c.Record != nil && s.mode == "before-row" {
		exitLifecycle(s.root, lifecycleMarker{Version: c.Record.KeyVersion, Nonce: c.Record.Nonce})
	}
	e := s.LifecycleStore.CommitLifecycle(ctx, expected, c)
	if e == nil && c.Record != nil && s.mode == "after-row" {
		exitLifecycle(s.root, lifecycleMarker{Version: c.Record.KeyVersion, Nonce: c.Record.Nonce})
	}
	return e
}
func RunLifecycleProbe(ctx context.Context, path, mode string) error {
	raw, e := os.ReadFile(path)
	if e != nil || len(raw) > 16384 {
		return credentials.Invalid
	}
	var p lifecycleProbeConfig
	if json.Unmarshal(raw, &p) != nil {
		return credentials.Invalid
	}
	db, e := sqliteauth.Open(filepath.Join(p.Root, "auth.db"))
	if e != nil {
		return credentials.Unavailable
	}
	defer db.Close()
	snapshot, e := db.Load(ctx)
	if e != nil {
		return credentials.Unavailable
	}
	c := &clock{t: p.Now}
	auth, e := authorization.New(db, c, snapshot.State.Config)
	if e != nil {
		return credentials.Unavailable
	}
	authority, e := credentialauth.New(auth, "root")
	if e != nil {
		return e
	}
	keys, e := filekeys.Open(filepath.Join(p.Root, "keys"), filekeys.MaxUses)
	if e != nil {
		return e
	}
	defer keys.Close()
	store, e := sqlitecredentials.Open(filepath.Join(p.Root, "credentials.db"))
	if e != nil {
		return e
	}
	defer store.Close()
	manager, e := credentials.NewLifecycle(&crashingLifecycleStore{LifecycleStore: store, root: p.Root, mode: mode}, &crashingLifecycleKeys{KeyLifecycle: keys, root: p.Root, mode: mode}, authority, c, credentials.Config{Timeout: time.Second, MaxConcurrent: 4})
	if e != nil {
		return e
	}
	if _, e = manager.StartRotation(ctx, p.Token, "rotation", p.Binding); e != nil {
		return e
	}
	if _, e = manager.StepRotation(ctx, p.Token, "rotation", p.Binding); e != nil {
		return e
	}
	return credentials.Invalid
}
func LifecycleProcessCheck(ctx context.Context, mode string) error {
	switch mode {
	case "after-prepare", "before-activate", "after-activate", "before-row", "after-row":
	default:
		return credentials.Invalid
	}
	f, e := newFixture(ctx)
	if e != nil {
		return e
	}
	defer f.close()
	now, _ := f.clock.Now()
	if _, e = f.broker.Put(ctx, f.token, "second", f.binding, 0, now.Add(time.Hour).Unix(), []byte(fixtureSecret)); e != nil {
		return e
	}
	original, e := f.store.Get(ctx, "credential")
	if e != nil {
		return e
	}
	raw, e := json.Marshal(lifecycleProbeConfig{Root: f.root, Token: f.token, Binding: f.binding, Now: now})
	if e != nil {
		return credentials.Invalid
	}
	path := filepath.Join(f.root, "probe.json")
	if os.WriteFile(path, raw, 0600) != nil {
		return credentials.Unavailable
	}
	f.store.Close()
	f.keys.Close()
	exe, e := os.Executable()
	if e != nil {
		return credentials.Unavailable
	}
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(bounded, exe, "lifecycle-crash-probe", path, mode)
	if strings.HasSuffix(exe, ".test") {
		cmd = exec.CommandContext(bounded, exe, "-test.run=^TestLifecycleProbeProcess$")
		cmd.Env = append(os.Environ(), "HARNESS_LIFECYCLE_PROBE="+path, "HARNESS_LIFECYCLE_MODE="+mode)
	}
	err := cmd.Run()
	exited, ok := err.(*exec.ExitError)
	if !ok || exited.ExitCode() != 73 {
		return credentials.Unavailable
	}
	f.store, e = sqlitecredentials.Open(filepath.Join(f.root, "credentials.db"))
	if e != nil {
		return e
	}
	f.keys, e = filekeys.Open(filepath.Join(f.root, "keys"), filekeys.MaxUses)
	if e != nil {
		return e
	}
	if e = f.rebind(); e != nil {
		return e
	}
	markerRaw, e := os.ReadFile(filepath.Join(f.root, "phase.json"))
	if e != nil {
		return credentials.Unavailable
	}
	var marker lifecycleMarker
	if json.Unmarshal(markerRaw, &marker) != nil {
		return credentials.Invalid
	}
	state, e := f.store.LoadLifecycle(ctx)
	if e != nil || len(state.Rotations) != 1 {
		return credentials.Invalid
	}
	record, e := f.store.Get(ctx, "credential")
	if e != nil {
		return e
	}
	expectedProgress := 0
	expectedRevision := uint64(1)
	if mode == "after-row" {
		expectedProgress = 1
		expectedRevision = 2
	}
	if len(state.Rotations[0].Completed) != expectedProgress || record.Revision != expectedRevision {
		return credentials.Invalid
	}
	active, e := f.keys.Active(ctx)
	if e != nil {
		return e
	}
	if (mode == "after-prepare" || mode == "before-activate") && active != original.KeyVersion {
		return credentials.Invalid
	}
	if mode != "after-prepare" && mode != "before-activate" && active != marker.Version {
		return credentials.Invalid
	}
	for _, ref := range []string{"credential", "second"} {
		if e = f.use(ctx, ref, "before-recovery-"+ref); e != nil {
			return e
		}
	}
	manager, e := lifecycleManager(f, f.store, f.keys)
	if e != nil {
		return e
	}
	result, e := finishRotation(ctx, f, manager)
	if e != nil || result.KeyVersion != marker.Version || len(result.Completed) != 2 {
		return credentials.Invalid
	}
	for _, ref := range []string{"credential", "second"} {
		row, e := f.store.Get(ctx, ref)
		if e != nil || row.Revision != 2 || row.KeyVersion != marker.Version {
			return credentials.Invalid
		}
		if ref == "credential" && mode == "before-row" && bytes.Equal(row.Nonce, marker.Nonce) {
			return credentials.Invalid
		}
		if ref == "credential" && mode == "after-row" && !bytes.Equal(row.Nonce, marker.Nonce) {
			return credentials.Invalid
		}
		if e = f.use(ctx, ref, "after-recovery-"+ref); e != nil {
			return e
		}
	}
	value, e := f.value(ctx)
	if e != nil || value != 12 {
		return credentials.Invalid
	}
	return nil
}
