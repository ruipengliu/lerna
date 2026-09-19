package credentials_test

import (
	"context"
	credentialbackups "lerna/adapters/credentials/backups"
	"lerna/adapters/credentials/filekeys"
	sqlitecredentials "lerna/adapters/credentials/sqlite"
	"lerna/credentials"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type lostRotationCommit struct {
	credentials.LifecycleStore
	armed bool
}

func (s *lostRotationCommit) CommitLifecycle(ctx context.Context, expected uint64, change credentials.LifecycleChange) error {
	e := s.LifecycleStore.CommitLifecycle(ctx, expected, change)
	if e == nil && change.Record != nil && s.armed {
		s.armed = false
		return credentials.Unavailable
	}
	return e
}

type lostArchivePut struct {
	credentials.BackupArchive
	armed bool
}

func (a *lostArchivePut) Put(ctx context.Context, id string, raw []byte) (string, error) {
	digest, e := a.BackupArchive.Put(ctx, id, raw)
	if e == nil && a.armed {
		a.armed = false
		return "", credentials.Unavailable
	}
	return digest, e
}

func TestRotationResumesOriginalOperationWithDurableProgress(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	dir := filepath.Join(root, "keys")
	if e := os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	source, e := filekeys.Open(dir, 64)
	if e != nil {
		t.Fatal(e)
	}
	defer source.Close()
	old, e := source.Generate(ctx)
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(root, "credentials.db")
	db, e := sqlitecredentials.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	c := &clock{time.Unix(1800000000, 0)}
	cfg := credentials.Config{Timeout: time.Second, MaxConcurrent: 4}
	broker, e := credentials.New(db, source, auth{}, c, cfg)
	if e != nil {
		t.Fatal(e)
	}
	target := credentials.Binding{Namespace: "local", Subject: "alice", Driver: "orders-v1", Service: "orders", Account: "alice", Purpose: "task", Location: "local"}
	for _, ref := range []string{"one", "two"} {
		if _, e = broker.Put(ctx, "trusted", ref, target, 0, c.now.Add(time.Hour).Unix(), []byte("rotation-fixture")); e != nil {
			t.Fatal(e)
		}
	}
	lost := &lostRotationCommit{LifecycleStore: db, armed: true}
	manager, e := credentials.NewLifecycle(lost, source, auth{}, c, cfg)
	if e != nil {
		t.Fatal(e)
	}
	other := target
	other.Account = "other-account"
	if _, e = broker.Put(ctx, "trusted", "other", other, 0, c.now.Add(time.Hour).Unix(), []byte("other-fixture")); e != nil {
		t.Fatal(e)
	}
	backupDir := filepath.Join(root, "backups")
	if e = os.Mkdir(backupDir, 0700); e != nil {
		t.Fatal(e)
	}
	archive, e := credentialbackups.Open(backupDir, "local-archive")
	if e != nil {
		t.Fatal(e)
	}
	defer archive.Close()
	manager, e = manager.WithArchive(&lostArchivePut{BackupArchive: archive, armed: true})
	if e != nil {
		t.Fatal(e)
	}
	_, e = manager.CreateBackup(ctx, "trusted", "before-rotation", target)
	if e != credentials.Unavailable {
		t.Fatal("lost archive reply not reported", e)
	}
	// The live credential changes while the durable backup intent still holds
	// its original snapshot. A retry must not substitute this newer record.
	if _, e = broker.Put(ctx, "trusted", "two", target, 1, c.now.Add(time.Hour).Unix(), []byte("rotation-fixture")); e != nil {
		t.Fatal(e)
	}
	backup, e := manager.CreateBackup(ctx, "trusted", "before-rotation", target)
	if e != nil {
		t.Fatal("pending backup did not resume original snapshot", e)
	}
	if backup.Phase != "available" {
		t.Fatal("backup not durable")
	}
	r, e := manager.StartRotation(ctx, "trusted", "rotation", target)
	if e != nil {
		t.Fatal(e)
	}
	if r.Phase != "pending" {
		t.Fatal("unexpected initial phase")
	}
	_, e = manager.StepRotation(ctx, "trusted", "rotation", target)
	if e != credentials.Unavailable {
		t.Fatal("lost commit did not report unknown", e)
	}
	r, e = manager.StartRotation(ctx, "trusted", "rotation", target)
	if e != nil {
		t.Fatal(e)
	}
	if r.KeyVersion == old || len(r.Completed) != 1 {
		t.Fatal("first bounded step did not migrate one record")
	}
	key := r.KeyVersion
	db.Close()
	db, e = sqlitecredentials.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	manager, e = credentials.NewLifecycle(db, source, auth{}, c, cfg)
	if e != nil {
		t.Fatal(e)
	}
	resumed, e := manager.StartRotation(ctx, "trusted", "rotation", target)
	if e != nil || resumed.KeyVersion != key || len(resumed.Completed) != 1 {
		t.Fatal("lost original operation", e)
	}
	for i := 0; i < 3; i++ {
		r, e = manager.StepRotation(ctx, "trusted", "rotation", target)
		if e != nil {
			t.Fatal(e)
		}
		if r.Phase == "completed" {
			break
		}
	}
	if r.Phase != "completed" || len(r.Completed) != 2 {
		t.Fatal("rotation did not finish")
	}
	for _, ref := range []string{"one", "two"} {
		record, e := db.Get(ctx, ref)
		expected := uint64(2)
		if ref == "two" {
			expected = 3
		}
		if e != nil || record.KeyVersion != key || record.Revision != expected {
			t.Fatal("record migration mismatch", e)
		}
	}
	t.Run("backup holds old material until disposition", func(t *testing.T) {
		manager, e = manager.WithArchive(archive)
		if e != nil {
			t.Fatal(e)
		}
		if e = manager.RetireKey(ctx, "trusted", old, target); e != credentials.Denied {
			t.Fatal("backup did not protect key", e)
		}
		if _, e = source.Read(ctx, old); e != nil {
			t.Fatal("necessary key removed", e)
		}
		if _, e = manager.DisposeBackup(ctx, "trusted", "before-rotation", target); e != nil {
			t.Fatal(e)
		}
		if e = archive.Check(ctx, backup.StorageID, backup.Digest); e != credentials.Missing {
			t.Fatal("backup material not removed", e)
		}
		if e = manager.RetireKey(ctx, "trusted", old, target); e != credentials.Conflict {
			t.Fatal("live record did not protect old key", e)
		}
		if _, e = manager.StartRotation(ctx, "trusted", "other-rotation", other); e != nil {
			t.Fatal(e)
		}
		for i := 0; i < 3; i++ {
			r, e := manager.StepRotation(ctx, "trusted", "other-rotation", other)
			if e != nil {
				t.Fatal(e)
			}
			if r.Phase == "completed" {
				break
			}
		}
		if e = manager.RetireKey(ctx, "trusted", old, target); e != nil {
			t.Fatal(e)
		}
		if _, e = source.Read(ctx, old); e != credentials.KeyUnavailable {
			t.Fatal("old material retained", e)
		}
		if e = manager.RetireKey(ctx, "trusted", old, target); e != nil {
			t.Fatal("retire retry", e)
		}
	})
	t.Run("failed record CAS rolls back progress", func(t *testing.T) {
		before, e := db.LoadLifecycle(ctx)
		if e != nil {
			t.Fatal(e)
		}
		duplicate, e := db.Get(ctx, "other")
		if e != nil {
			t.Fatal(e)
		}
		duplicate.Revision = 1
		next := before
		next.Revision++
		e = db.CommitLifecycle(ctx, before.Revision, credentials.LifecycleChange{State: next, Record: &duplicate, ExpectedRecord: 0})
		if e != credentials.Conflict {
			t.Fatal("duplicate record unexpectedly committed", e)
		}
		after, e := db.LoadLifecycle(ctx)
		if e != nil || after.Revision != before.Revision {
			t.Fatal("progress committed without ciphertext", e)
		}
		record, e := db.Get(ctx, "other")
		if e != nil || record.Revision != 2 {
			t.Fatal("failed transaction changed record", e)
		}
	})

}
