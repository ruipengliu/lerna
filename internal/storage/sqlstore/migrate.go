package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
	"github.com/ruipengliu/lerna/migrations"
)

var ErrSchema = errors.New("durable schema is missing or incompatible; run explicit migrations")

func Migrate(ctx context.Context, db *sql.DB, driver string) error {
	steps, err := fs.Sub(migrations.Files, driver)
	if err != nil {
		return err
	}
	dialect := goose.DialectSQLite3
	if driver == "postgres" {
		dialect = goose.DialectPostgres
	} else if driver != "sqlite" {
		return ErrSchema
	}
	options := []goose.ProviderOption{goose.WithTableName("durable_schema_version")}
	if driver == "postgres" {
		locker, err := lock.NewPostgresSessionLocker()
		if err != nil {
			return err
		}
		options = append(options, goose.WithSessionLocker(locker))
	}
	provider, err := goose.NewProvider(dialect, db, steps, options...)
	if err != nil {
		return err
	}
	_, err = provider.Up(ctx)
	if err != nil {
		return err
	}
	return CheckVersion(ctx, db)
}
func CheckVersion(ctx context.Context, db *sql.DB) error {
	var version int64
	var applied bool
	if err := db.QueryRowContext(ctx, "SELECT version_id,is_applied FROM durable_schema_version ORDER BY id DESC LIMIT 1").Scan(&version, &applied); err != nil || version != 1 || !applied {
		return ErrSchema
	}
	return nil
}
