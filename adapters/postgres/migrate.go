package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"errors"

	"github.com/ruipengliu/lerna/contract"
	"github.com/ruipengliu/lerna/runtime"
)

//go:embed migrations/host/0001_admission.sql
var migrationV1 string

func MigrationV1Checksum() string {
	digest := sha256.Sum256([]byte(migrationV1))
	return "sha256:" + hex.EncodeToString(digest[:])
}
func (s *Store) Migrate(ctx context.Context) error {
	owner := contract.OwnerRef{TenantID: "migration", OwnerID: "host"}
	return s.Within(ctx, owner, func(ctx context.Context, token runtime.Tx) error {
		tx, err := s.token(ctx, token, owner)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(0,hashtext($1))`, s.config.Schema+":migration"); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `SELECT set_config('search_path',$1,true)`, `"`+s.config.Schema+`"`); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version bigint PRIMARY KEY, checksum text NOT NULL)`); err != nil {
			return err
		}
		var checksum string
		err = tx.QueryRowContext(ctx, `SELECT checksum FROM schema_migrations WHERE version=1`).Scan(&checksum)
		if err == nil {
			if checksum != MigrationV1Checksum() {
				return errors.New("migration checksum mismatch")
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err = tx.ExecContext(ctx, migrationV1); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(version,checksum)VALUES(1,$1)`, MigrationV1Checksum())
		return err
	})
}
