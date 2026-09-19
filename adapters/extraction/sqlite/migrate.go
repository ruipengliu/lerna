package sqlite

import (
	"context"
	"database/sql"
)

// Old retired facts lack proof of an execution binding. Preserve an empty
// binding rather than manufacturing one during migration.
func migrateInvocation(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE candidate_lock SET version=version WHERE id=1`); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(retired_candidates)`)
	if err != nil {
		return err
	}
	found := false
	reasonFound := false
	for rows.Next() {
		var ordinal, notnull, pk int
		var name, kind string
		var value sql.NullString
		if err = rows.Scan(&ordinal, &name, &kind, &notnull, &value, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == "reason" {
			reasonFound = true
		}
		if name == "invocation" {
			found = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if !found {
		if _, err = tx.ExecContext(ctx, `ALTER TABLE retired_candidates ADD COLUMN invocation TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	if !reasonFound {
		if _, err = tx.ExecContext(ctx, `ALTER TABLE retired_candidates ADD COLUMN reason TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	return tx.Commit()
}
