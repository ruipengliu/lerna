package sqlite

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

//go:embed migrations/host/0002_claims.sql
var migrationV2 string

//go:embed migrations/host/0003_retention.sql
var migrationV3 string

//go:embed migrations/host/0004_waits.sql
var migrationV4 string

//go:embed migrations/host/0005_pools.sql
var migrationV5 string

func MigrationV5Checksum() string {
	digest := sha256.Sum256([]byte(migrationV5))
	return "sha256:" + hex.EncodeToString(digest[:])
}
func MigrationV4Checksum() string {
	digest := sha256.Sum256([]byte(migrationV4))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func MigrationV3Checksum() string {
	digest := sha256.Sum256([]byte(migrationV3))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func MigrationV2Checksum() string {
	digest := sha256.Sum256([]byte(migrationV2))
	return "sha256:" + hex.EncodeToString(digest[:])
}

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
		if _, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version bigint PRIMARY KEY, checksum text NOT NULL)`); err != nil {
			return err
		}
		for index, migration := range []string{migrationV1, migrationV2, migrationV3, migrationV4, migrationV5} {
			version := index + 1
			digest := sha256.Sum256([]byte(migration))
			expected := "sha256:" + hex.EncodeToString(digest[:])
			var checksum string
			err = tx.QueryRowContext(ctx, `SELECT checksum FROM schema_migrations WHERE version=$1`, version).Scan(&checksum)
			if err == nil {
				if checksum != expected {
					return errors.New("migration checksum mismatch")
				}
				continue
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if _, err = tx.ExecContext(ctx, migration); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(version,checksum)VALUES($1,$2)`, version, expected); err != nil {
				return err
			}
		}
		return nil
	})
}

// MigrationStatus observes the adapter's recorded immutable migration identity.
type Migration struct {
	Version  int64
	Checksum string
}

func (s *Store) MigrationStatus(ctx context.Context) (Migration, error) {
	var out Migration
	owner := contract.OwnerRef{TenantID: "migration", OwnerID: "host"}
	err := s.Within(ctx, owner, func(ctx context.Context, token runtime.Tx) error {
		tx, err := s.token(ctx, token, owner)
		if err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, "SELECT version,checksum FROM schema_migrations ORDER BY version DESC LIMIT 1").Scan(&out.Version, &out.Checksum)
	})
	return out, err
}

// MigrationVersions reports every immutable applied artifact, including v1
// after a forward upgrade. It is an internal Host configuration observation.
func (s *Store) MigrationVersions(ctx context.Context) ([]Migration, error) {
	var versions []Migration
	err := s.Within(ctx, contract.OwnerRef{TenantID: "migration", OwnerID: "host"}, func(ctx context.Context, token runtime.Tx) error {
		tx, err := s.localToken(ctx, token)
		if err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT version,checksum FROM schema_migrations ORDER BY version`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var version Migration
			if err = rows.Scan(&version.Version, &version.Checksum); err != nil {
				return err
			}
			versions = append(versions, version)
		}
		return rows.Err()
	})
	return versions, err
}
