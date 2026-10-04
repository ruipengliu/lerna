//go:build integration

package contentfixture

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestContentMigrationEmptyRepeatAndChecksum(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	w := New(t, ctx)
	const expected = "sha256:00363b79dafb6eb1f373be9ef08e915fe23cc3db0fa55345e8ae6c051ce0c1ed"
	if err := w.Store().Migrate(ctx); err != nil {
		t.Fatal("repeat migration", err)
	}
	versions, err := w.Store().MigrationVersions(ctx)
	if err != nil || len(versions) != 1 || versions[0].Version != 1 || versions[0].Checksum != expected {
		t.Fatal("empty/repeat migration lost fixed checksum", err)
	}
	// Infrastructure corruption seam: no business table is an oracle.
	db, err := sql.Open("pgx", w.Config.DSN)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	var once sync.Once
	var closeErr error
	closeDB := func() error { once.Do(func() { closeErr = db.Close() }); return closeErr }
	w.infrastructureClosers = append(w.infrastructureClosers, closeDB)
	if err = db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	if err = w.register("pg_migration_connection " + w.Config.Schema); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE "`+w.Config.Schema+`".content_schema_migrations SET checksum=$1 WHERE version=1`, "sha256:0000000000000000000000000000000000000000000000000000000000000000"); err != nil {
		t.Fatal(err)
	}
	if err = w.Store().Migrate(ctx); err == nil {
		t.Fatal("wrong migration checksum silently reapplied")
	}
	if _, err = db.ExecContext(ctx, `UPDATE "`+w.Config.Schema+`".content_schema_migrations SET checksum=$1 WHERE version=1`, expected); err != nil {
		t.Fatal(err)
	}
	if err = closeDB(); err != nil {
		t.Fatal(err)
	}
	w.Reopen(ctx)
	if err = w.Store().Migrate(ctx); err != nil {
		t.Fatal("normal checksum reopen refused", err)
	}
	versions, err = w.Store().MigrationVersions(ctx)
	if err != nil || len(versions) != 1 || versions[0].Checksum != expected {
		t.Fatal(errors.Join(err, errors.New("restored checksum not durable")))
	}
}
