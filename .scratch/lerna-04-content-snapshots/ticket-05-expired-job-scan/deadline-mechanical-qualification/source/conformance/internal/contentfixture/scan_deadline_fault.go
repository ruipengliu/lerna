package contentfixture

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	v "github.com/ruipengliu/lerna/contract/v1_2"
)

// CorruptBodyCleanupDeadline is a Host-only stored-data fault, not a business
// observation. It changes only the two persisted deadline representations of
// an existing sealed version, and restores their original bytes before Close.
func (w *World) CorruptBodyCleanupDeadline(ctx context.Context, ref v.ContentRef) func() error {
	w.t.Helper()
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	db, err := sql.Open("pgx", w.Config.DSN)
	if err != nil {
		w.t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	var body, seal []byte
	installed := false
	var once sync.Once
	var releaseErr error
	release := func() error {
		once.Do(func() {
			if installed {
				bounded, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				restore := func() error {
					tx, err := db.BeginTx(bounded, nil)
					if err != nil {
						return err
					}
					if _, err = tx.ExecContext(bounded, `SET LOCAL statement_timeout='2s'; SET LOCAL lock_timeout='1s'`); err != nil {
						return errors.Join(err, tx.Rollback())
					}
					result, err := tx.ExecContext(bounded, `UPDATE "`+w.Config.Schema+`".content_versions SET body=$5,body_seal=$6 WHERE tenant_id=$1 AND owner_id=$2 AND content_id=$3 AND version=$4`, ref.Owner.TenantID, ref.Owner.OwnerID, ref.ContentID, ref.Version, body, seal)
					if err != nil {
						return errors.Join(err, tx.Rollback())
					}
					count, err := result.RowsAffected()
					if err != nil || count != 1 {
						return errors.Join(errors.New("deadline restoration did not target exactly one version"), err, tx.Rollback())
					}
					return tx.Commit()
				}
				releaseErr = restore()
			}
			releaseErr = errors.Join(releaseErr, db.Close())
		})
		return releaseErr
	}
	w.infrastructureClosers = append(w.infrastructureClosers, release)
	var pid int
	if err = db.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		w.t.Fatal(err)
	}
	if err = w.register(fmt.Sprintf("pg_scan_deadline_fault %s %d", w.Config.Schema, pid)); err != nil {
		w.t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		w.t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `SET LOCAL statement_timeout='2s'; SET LOCAL lock_timeout='1s'`); err != nil {
		w.t.Fatal(errors.Join(err, tx.Rollback()))
	}
	// Capturing bytes is fixture restoration, never an acceptance oracle.
	err = tx.QueryRowContext(ctx, `SELECT body,body_seal FROM "`+w.Config.Schema+`".content_versions WHERE tenant_id=$1 AND owner_id=$2 AND content_id=$3 AND version=$4 FOR UPDATE`, ref.Owner.TenantID, ref.Owner.OwnerID, ref.ContentID, ref.Version).Scan(&body, &seal)
	if err != nil || seal == nil {
		w.t.Fatal("deadline fault requires an existing sealed version", errors.Join(err, tx.Rollback()))
	}
	result, err := tx.ExecContext(ctx, `UPDATE "`+w.Config.Schema+`".content_versions SET body=convert_to(jsonb_set(convert_from(body,'UTF8')::jsonb,'{body_seal,deadline}','"not-a-time"'::jsonb)::text,'UTF8'),body_seal=convert_to(jsonb_set(convert_from(body_seal,'UTF8')::jsonb,'{deadline}','"not-a-time"'::jsonb)::text,'UTF8') WHERE tenant_id=$1 AND owner_id=$2 AND content_id=$3 AND version=$4`, ref.Owner.TenantID, ref.Owner.OwnerID, ref.ContentID, ref.Version)
	if err != nil {
		w.t.Fatal(errors.Join(err, tx.Rollback()))
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		w.t.Fatal("deadline fault did not target exactly one version", errors.Join(err, tx.Rollback()))
	}
	// A commit error may still have applied; restoration remains owned.
	installed = true
	if err = tx.Commit(); err != nil {
		w.t.Fatal(err)
	}
	return release
}
