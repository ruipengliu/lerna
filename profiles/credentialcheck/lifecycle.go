package credentialcheck

import (
	"bytes"
	"context"
	"lerna/adapters/credentialauth"
	"lerna/credentials"
	"time"
)

func lifecycleManager(f *fixture, store credentials.LifecycleStore, keys credentials.KeyLifecycle) (*credentials.Lifecycle, error) {
	a, e := credentialauth.New(f.auth, "root")
	if e != nil {
		return nil, e
	}
	return credentials.NewLifecycle(store, keys, a, f.clock, credentials.Config{Timeout: time.Second, MaxConcurrent: 4})
}

type pausedCredentialWrite struct {
	credentials.Store
	entered, release chan struct{}
}

func (s *pausedCredentialWrite) Swap(ctx context.Context, expected uint64, r credentials.Record) error {
	close(s.entered)
	select {
	case <-s.release:
	case <-ctx.Done():
		return credentials.Unavailable
	}
	return s.Store.Swap(ctx, expected, r)
}
func lateRotationWrite(ctx context.Context) error {
	f, e := newFixture(ctx)
	if e != nil {
		return e
	}
	defer f.close()
	target := f.binding
	target.Account = "new-account"
	gate := &pausedCredentialWrite{Store: f.store, entered: make(chan struct{}), release: make(chan struct{})}
	a, e := credentialauth.New(f.auth, "root")
	if e != nil {
		return e
	}
	broker, e := credentials.New(gate, f.keys, a, f.clock, credentials.Config{Timeout: time.Second, MaxConcurrent: 4})
	if e != nil {
		return e
	}
	now, _ := f.clock.Now()
	done := make(chan error, 1)
	go func() {
		_, err := broker.Put(ctx, f.token, "late", target, 0, now.Add(time.Hour).Unix(), []byte(fixtureSecret))
		done <- err
	}()
	released := false
	defer func() {
		if !released {
			close(gate.release)
		}
	}()
	select {
	case <-gate.entered:
	case <-time.After(time.Second):
		return credentials.Unavailable
	}
	manager, e := lifecycleManager(f, f.store, f.keys)
	if e != nil {
		return e
	}
	if _, e = manager.StartRotation(ctx, f.token, "rotate-empty-binding", target); e != nil {
		return e
	}
	rotated, e := manager.StepRotation(ctx, f.token, "rotate-empty-binding", target)
	if e != nil || rotated.Phase != "completed" {
		return credentials.Invalid
	}
	close(gate.release)
	released = true
	if e = <-done; e != credentials.Conflict {
		return credentials.Invalid
	}
	if _, e = f.store.Get(ctx, "late"); e != credentials.Missing {
		return credentials.Invalid
	}
	fresh, e := f.broker.Put(ctx, f.token, "fresh", target, 0, now.Add(time.Hour).Unix(), []byte(fixtureSecret))
	if e != nil || fresh.KeyVersion != rotated.KeyVersion {
		return credentials.Invalid
	}
	return f.use(ctx, "credential", "existing-old-key-read")
}

func finishRotation(ctx context.Context, f *fixture, l *credentials.Lifecycle) (credentials.Rotation, error) {
	if _, e := l.StartRotation(ctx, f.token, "rotation", f.binding); e != nil {
		return credentials.Rotation{}, e
	}
	for i := 0; i < 3; i++ {
		r, e := l.StepRotation(ctx, f.token, "rotation", f.binding)
		if e != nil {
			return r, e
		}
		if r.Phase == "completed" {
			return r, nil
		}
	}
	return credentials.Rotation{}, credentials.Invalid
}

type defectiveLifecycleKeys struct {
	credentials.KeyLifecycle
	mode, prepared string
	activations    int
}

func (k *defectiveLifecycleKeys) Prepare(ctx context.Context, op string) (string, error) {
	id, e := k.KeyLifecycle.Prepare(ctx, op)
	k.prepared = id
	return id, e
}
func (k *defectiveLifecycleKeys) Read(ctx context.Context, id string) ([]byte, error) {
	if k.mode == "bad-material" && id == k.prepared {
		return []byte{1}, nil
	}
	return k.KeyLifecycle.Read(ctx, id)
}
func (k *defectiveLifecycleKeys) Activate(ctx context.Context, op, id string) error {
	k.activations++
	if k.mode == "activation-failure" {
		return credentials.KeyUnavailable
	}
	return k.KeyLifecycle.Activate(ctx, op, id)
}

type lostLifecycleReply struct {
	credentials.LifecycleStore
	lost bool
}

func (s *lostLifecycleReply) CommitLifecycle(ctx context.Context, expected uint64, c credentials.LifecycleChange) error {
	e := s.LifecycleStore.CommitLifecycle(ctx, expected, c)
	if e == nil && c.Record != nil && !s.lost {
		s.lost = true
		return credentials.Unavailable
	}
	return e
}
func LifecycleCheck(ctx context.Context, mode string) error {
	if mode == "late-write" {
		return lateRotationWrite(ctx)
	}
	if mode == "rollback-quarantine" {
		return lifecycleRollback(ctx)
	}
	f, e := newFixture(ctx)
	if e != nil {
		return e
	}
	defer f.close()
	manager, e := lifecycleManager(f, f.store, f.keys)
	if e != nil {
		return e
	}
	original, e := f.store.Get(ctx, "credential")
	if e != nil {
		return e
	}
	switch mode {
	case "repeat-operation":
		r, e := finishRotation(ctx, f, manager)
		if e != nil {
			return e
		}
		after, e := manager.StartRotation(ctx, f.token, "rotation", f.binding)
		if e != nil || after.KeyVersion != r.KeyVersion || after.Phase != "completed" {
			return credentials.Invalid
		}
		after, e = manager.StepRotation(ctx, f.token, "rotation", f.binding)
		if e != nil || after.KeyVersion != r.KeyVersion {
			return credentials.Invalid
		}
		row, e := f.store.Get(ctx, "credential")
		if e != nil || row.Revision != 2 {
			return credentials.Invalid
		}
		wrong := f.binding
		wrong.Account = "other"
		if _, e = manager.StartRotation(ctx, f.token, "rotation", wrong); e != credentials.Denied {
			return credentials.Invalid
		}
		return f.use(ctx, "credential", "repeat-use")
	case "bad-material", "activation-failure":
		broken := &defectiveLifecycleKeys{KeyLifecycle: f.keys, mode: mode}
		manager, e = lifecycleManager(f, f.store, broken)
		if e != nil {
			return e
		}
		if _, e = manager.StartRotation(ctx, f.token, "rotation", f.binding); e != nil {
			return e
		}
		if _, e = manager.StepRotation(ctx, f.token, "rotation", f.binding); e != credentials.KeyUnavailable {
			return credentials.Invalid
		}
		active, e := f.keys.Active(ctx)
		if e != nil || active != original.KeyVersion {
			return credentials.Invalid
		}
		if mode == "bad-material" && broken.activations != 0 {
			return credentials.Invalid
		}
		if e = f.use(ctx, "credential", "before-key-recovery"); e != nil {
			return e
		}
		manager, e = lifecycleManager(f, f.store, f.keys)
		if e != nil {
			return e
		}
		r, e := finishRotation(ctx, f, manager)
		if e != nil || r.KeyVersion != broken.prepared {
			return credentials.Invalid
		}
		return f.use(ctx, "credential", "after-key-recovery")
	case "lost-commit":
		lost := &lostLifecycleReply{LifecycleStore: f.store}
		manager, e = lifecycleManager(f, lost, f.keys)
		if e != nil {
			return e
		}
		if _, e = manager.StartRotation(ctx, f.token, "rotation", f.binding); e != nil {
			return e
		}
		if _, e = manager.StepRotation(ctx, f.token, "rotation", f.binding); e != credentials.Unavailable {
			return credentials.Invalid
		}
		row, e := f.store.Get(ctx, "credential")
		if e != nil || row.Revision != 2 {
			return credentials.Invalid
		}
		if _, e = finishRotation(ctx, f, manager); e != nil {
			return e
		}
		after, e := f.store.Get(ctx, "credential")
		if e != nil || after.Revision != 2 || !bytes.Equal(after.Nonce, row.Nonce) {
			return credentials.Invalid
		}
		return f.use(ctx, "credential", "lost-commit-use")
	case "missing-old-key", "damaged-ciphertext":
		if mode == "missing-old-key" {
			if _, e = f.keys.Generate(ctx); e != nil {
				return e
			}
			if e = f.keys.Retire(ctx, original.KeyVersion); e != nil {
				return e
			}
		} else {
			original.Revision++
			original.Ciphertext[0] ^= 1
			if e = f.store.Swap(ctx, original.Revision-1, original); e != nil {
				return e
			}
		}
		if _, e = manager.StartRotation(ctx, f.token, "rotation", f.binding); e != nil {
			return e
		}
		_, e = manager.StepRotation(ctx, f.token, "rotation", f.binding)
		if (mode == "missing-old-key" && e != credentials.KeyUnavailable) || (mode == "damaged-ciphertext" && e != credentials.Denied) {
			return credentials.Invalid
		}
		s, e := f.store.LoadLifecycle(ctx)
		if e != nil || len(s.Rotations) != 1 || len(s.Rotations[0].Completed) != 0 {
			return credentials.Invalid
		}
		if e = f.use(ctx, "credential", "bad-use"); e == nil || f.sent() != 0 {
			return credentials.Invalid
		}
		now, _ := f.clock.Now()
		if _, e = f.broker.Put(ctx, f.token, "independent", f.binding, 0, now.Add(time.Hour).Unix(), []byte(fixtureSecret)); e != nil {
			return e
		}
		return f.use(ctx, "independent", "independent-after-corruption")
	case "backup-retirement", "archive-clone", "damaged-backup":
		return lifecycleBackup(ctx, f, manager, mode, original.KeyVersion)
	default:
		return credentials.Invalid
	}
}
