package contentfixture

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

//go:embed testdata/legacy-v1/* testdata/legacy-many-policies/* testdata/legacy-expired-ancestor/*
var legacyFiles embed.FS

type LegacyObservation struct {
	Requests         []v.ContentPutRequest
	Receipts         []v.CommandReceipt
	States           []string
	SourceValidUntil time.Time
}

// NewLegacy restores an actual stopped 1a7 writer export, preserving its original
// 0001 checksum, receipts, exact direct-source metadata, failed staging and bytes.
func NewLegacy(t *testing.T, ctx context.Context) (*World, LegacyObservation) {
	return newLegacy(t, ctx, "legacy-v1")
}

func NewLegacyPolicies(t *testing.T, ctx context.Context) (*World, LegacyObservation) {
	return newLegacy(t, ctx, "legacy-many-policies")
}

func NewLegacyExpired(t *testing.T, ctx context.Context) (*World, LegacyObservation) {
	return newLegacy(t, ctx, "legacy-expired-ancestor")
}

func newLegacy(t *testing.T, ctx context.Context, archive string) (*World, LegacyObservation) {
	t.Helper()
	base := "testdata/" + archive + "/"
	var observation LegacyObservation
	raw, err := legacyFiles.ReadFile(base + "observation.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &observation); err != nil {
		t.Fatal(err)
	}
	w := newWorld(t, ctx, func(w *World) {
		raw, err := legacyFiles.ReadFile(base + "restore.sql")
		if err != nil {
			t.Fatal(err)
		}
		db, err := sql.Open("pgx", w.Config.DSN)
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		var once sync.Once
		var closeErr error
		closeDB := func() error { once.Do(func() { closeErr = db.Close() }); return closeErr }
		w.infrastructureClosers = append(w.infrastructureClosers, closeDB)
		if err = w.register("pg_legacy_restore_connection " + w.Config.Schema); err != nil {
			t.Fatal(err)
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, err = tx.ExecContext(ctx, strings.ReplaceAll(string(raw), "legacy_content", w.Config.Schema))
		if err != nil {
			t.Fatal(errors.Join(err, tx.Rollback()))
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if err = closeDB(); err != nil {
			t.Fatal(err)
		}
		versions, err := w.current.MigrationVersions(ctx)
		if err != nil || len(versions) != 1 || versions[0].Version != 1 {
			t.Fatal("original 0001 export not restored", err, versions)
		}
		for _, name := range []string{"76f0298c5432c6b1e28ae5a2c2cc4e8624fd32cffa6112173de32a68c90652c4", "da1384e8c6e31d165b9f0442b2d2f1f57a875a20156041bfedfcc4ce779654dc"} {
			body, err := legacyFiles.ReadFile(base + name)
			if err != nil {
				t.Fatal(err)
			}
			f, err := os.OpenFile(filepath.Join(w.Directory, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				t.Fatal(err)
			}
			closeFile := w.ownSetupFile(f)
			_, err = f.Write(body)
			if err = errors.Join(err, f.Sync(), closeFile()); err != nil {
				t.Fatal(err)
			}
		}
		dir, err := os.Open(w.Directory)
		if err != nil {
			t.Fatal(err)
		}
		closeDir := w.ownSetupFile(dir)
		if err = errors.Join(dir.Sync(), closeDir()); err != nil {
			t.Fatal(err)
		}
	})
	return w, observation
}
