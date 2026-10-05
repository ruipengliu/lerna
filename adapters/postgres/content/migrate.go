package content

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

//go:embed migrations/0001_content.sql
var migration string

//go:embed migrations/0002_source_policies.sql
var sourceMigration string

//go:embed migrations/0003_body_cleanup.sql
var bodyMigration string

type MigrationVersion struct {
	Version  int64
	Checksum string
}

func MigrationChecksum() string {
	sum := sha256.Sum256([]byte(migration))
	return "sha256:" + hex.EncodeToString(sum[:])
}
func (s *Store) Migrate(ctx context.Context) error {
	return s.core.Within(ctx, contract.OwnerRef{TenantID: "migration", OwnerID: "content"}, func(ctx context.Context, token runtime.Tx) error {
		tx, err := s.core.LocalSQL(ctx, token)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(0,hashtext($1))`, s.schema+":content-migration"); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `SELECT set_config('search_path',$1,true)`, `"`+s.schema+`"`); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS content_schema_migrations(version bigint PRIMARY KEY,checksum text NOT NULL)`); err != nil {
			return err
		}
		for i, text := range []string{migration, sourceMigration, bodyMigration} {
			version := i + 1
			sum := sha256.Sum256([]byte(text))
			wanted := "sha256:" + hex.EncodeToString(sum[:])
			var checksum string
			err = tx.QueryRowContext(ctx, `SELECT checksum FROM content_schema_migrations WHERE version=$1`, version).Scan(&checksum)
			if err == nil {
				if checksum != wanted {
					return errors.New("Content migration checksum mismatch")
				}
				continue
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if _, err = tx.ExecContext(ctx, text); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO content_schema_migrations(version,checksum)VALUES($1,$2)`, version, wanted); err != nil {
				return err
			}
		}
		return nil
	})
}
func (s *Store) MigrationVersions(ctx context.Context) ([]MigrationVersion, error) {
	var out []MigrationVersion
	err := s.core.Within(ctx, contract.OwnerRef{TenantID: "migration", OwnerID: "content"}, func(ctx context.Context, token runtime.Tx) error {
		tx, err := s.core.LocalSQL(ctx, token)
		if err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT version,checksum FROM `+s.core.Table("content_schema_migrations")+` ORDER BY version`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v MigrationVersion
			if err = rows.Scan(&v.Version, &v.Checksum); err != nil {
				return err
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, err
}
