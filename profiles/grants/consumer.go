package grants

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"lerna/authorization"
	_ "modernc.org/sqlite"
	"path/filepath"
)

// A test-only business consumer: permit and effect count share one SQLite tx.
// It does not stand in for production ExecutionStore or remote authentication.
func consumeFixture(ctx context.Context, dir string, p authorization.UsePermit) error {
	path := filepath.Join(dir, "consumer.db")
	apply := func() error {
		db, err := sql.Open("sqlite", path)
		if err != nil {
			return err
		}
		defer db.Close()
		if _, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS uses (operation TEXT PRIMARY KEY, permit TEXT NOT NULL, effects INTEGER NOT NULL)`); err != nil {
			return err
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		encoded, err := json.Marshal(p)
		if err != nil {
			return err
		}
		var previous string
		err = tx.QueryRowContext(ctx, `SELECT permit FROM uses WHERE operation=?`, p.OperationID).Scan(&previous)
		if err == nil {
			if previous != string(encoded) {
				return fmt.Errorf("changed permit")
			}
			return tx.Commit()
		}
		if err != sql.ErrNoRows {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO uses VALUES(?,?,1)`, p.OperationID, string(encoded)); err != nil {
			return err
		}
		return tx.Commit()
	}
	if err := apply(); err != nil {
		return err
	}
	if err := apply(); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	var count int
	if err = db.QueryRowContext(ctx, `SELECT SUM(effects) FROM uses WHERE operation=?`, p.OperationID).Scan(&count); err != nil {
		return err
	}
	return require(count == 1, "replay consumed twice")
}
