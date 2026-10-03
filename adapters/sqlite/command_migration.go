package sqlite

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"

	"github.com/ruipengliu/lerna/api"
)

//go:embed migrations/003_command_envelopes.sql
var commandEnvelopeMigration string

func migrateCommandEnvelopes(ctx context.Context, tx *sql.Tx) error {
	digest := api.Hash([]byte(commandEnvelopeMigration))
	var old string
	err := tx.QueryRowContext(ctx, "SELECT artifact_digest FROM harness_migrations WHERE migration_id=3").Scan(&old)
	if err == nil {
		if old != digest {
			return api.E("invalid_state", "migration_artifact_changed")
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if _, err = tx.ExecContext(ctx, commandEnvelopeMigration); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO harness_migrations(migration_id,artifact_digest,state,checkpoint) VALUES(3,?,'applied','schema_ready')", digest)
	return err
}
