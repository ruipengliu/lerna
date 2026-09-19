package credentialcheck

import (
	"bytes"
	"context"
	credentialbackups "lerna/adapters/credentials/backups"
	sqlitecredentials "lerna/adapters/credentials/sqlite"
	"lerna/credentials"
	"os"
	"path/filepath"
)

func lifecycleBackup(ctx context.Context, f *fixture, manager *credentials.Lifecycle, mode, old string) error {
	dir := filepath.Join(f.root, "backups")
	if e := os.Mkdir(dir, 0700); e != nil {
		return credentials.Unavailable
	}
	archive, e := credentialbackups.Open(dir, "archive")
	if e != nil {
		return e
	}
	defer archive.Close()
	manager, e = manager.WithArchive(archive)
	if e != nil {
		return e
	}
	b, e := manager.CreateBackup(ctx, f.token, "backup", f.binding)
	if e != nil {
		return e
	}
	if _, e = finishRotation(ctx, f, manager); e != nil {
		return e
	}
	if e = manager.RetireKey(ctx, f.token, old, f.binding); e != credentials.Denied {
		return credentials.Invalid
	}
	if mode == "archive-clone" {
		copied := filepath.Join(f.root, "copy")
		if os.Mkdir(copied, 0700) != nil {
			return credentials.Unavailable
		}
		raw, e := os.ReadFile(filepath.Join(dir, b.StorageID+".json"))
		if e != nil {
			return credentials.Unavailable
		}
		if os.WriteFile(filepath.Join(copied, b.StorageID+".json"), raw, 0600) != nil {
			return credentials.Unavailable
		}
		other, e := credentialbackups.Open(copied, "archive")
		if e != nil {
			return e
		}
		defer other.Close()
		clone, e := manager.WithArchive(other)
		if e != nil {
			return e
		}
		if _, e = clone.DisposeBackup(ctx, f.token, "backup", f.binding); e != credentials.Denied {
			return credentials.Invalid
		}
		return archive.Check(ctx, b.StorageID, b.Digest)
	}
	if mode == "damaged-backup" {
		if os.WriteFile(filepath.Join(dir, b.StorageID+".json"), []byte("damaged ciphertext fixture"), 0600) != nil {
			return credentials.Unavailable
		}
		if _, e = manager.DisposeBackup(ctx, f.token, "backup", f.binding); e != credentials.Unavailable {
			return credentials.Invalid
		}
		if e = manager.RetireKey(ctx, f.token, old, f.binding); e != credentials.Denied {
			return credentials.Invalid
		}
		return nil
	}
	if _, e = manager.DisposeBackup(ctx, f.token, "backup", f.binding); e != nil {
		return e
	}
	if e = archive.Check(ctx, b.StorageID, b.Digest); e != credentials.Missing {
		return credentials.Invalid
	}
	if e = manager.RetireKey(ctx, f.token, old, f.binding); e != nil {
		return e
	}
	if _, e = f.keys.Read(ctx, old); e != credentials.KeyUnavailable {
		return credentials.Invalid
	}
	if e = manager.RetireKey(ctx, f.token, old, f.binding); e != nil {
		return e
	}
	return f.use(ctx, "credential", "after-retirement")
}
func lifecycleRollback(ctx context.Context) error {
	f, e := newFixture(ctx)
	if e != nil {
		return e
	}
	defer f.close()
	path := filepath.Join(f.root, "credentials.db")
	f.store.Close()
	backup, e := os.ReadFile(path)
	if e != nil {
		return credentials.Unavailable
	}
	reopen := func() error {
		var err error
		f.store, err = sqlitecredentials.Open(path)
		if err != nil {
			return err
		}
		return f.rebind()
	}
	if e = reopen(); e != nil {
		return e
	}
	if _, e = f.broker.Rewrap(ctx, f.token, "credential", f.binding, 1); e != nil {
		return e
	}
	advanced, e := f.store.Get(ctx, "credential")
	if e != nil {
		return e
	}
	// Restore is an explicit trusted operation: revoke use in the independent
	// authority before loading a stale credential DB. AEAD is not freshness proof.
	if e = f.setPolicy(ctx, []string{"credential.manage"}); e != nil {
		return e
	}
	f.store.Close()
	if os.WriteFile(path, backup, 0600) != nil {
		return credentials.Unavailable
	}
	if e = reopen(); e != nil {
		return e
	}
	if e = f.use(ctx, "credential", "before-reconciliation"); e != credentials.Denied || f.sent() != 0 {
		return credentials.Invalid
	}
	if _, e = f.broker.Rewrap(ctx, f.token, "credential", f.binding, 1); e != nil {
		return e
	}
	restored, e := f.store.Get(ctx, "credential")
	if e != nil || advanced.KeyVersion != restored.KeyVersion || bytes.Equal(advanced.Nonce, restored.Nonce) {
		return credentials.Invalid
	}
	if e = f.setPolicy(ctx, []string{"credential.manage", "credential.use"}); e != nil {
		return e
	}
	return f.use(ctx, "credential", "after-explicit-reconciliation")
}
