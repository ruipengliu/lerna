package postgres

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"

	"github.com/ruipengliu/lerna/adapters/internal/durable"
	postgresdb "github.com/ruipengliu/lerna/adapters/postgres/gen"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

//go:embed migrations/002_query_bindings.sql
var queryBindingMigration string

var _ runtime.QueryBindingStore = (*Store)(nil)

func migrateQueryBindings(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, queryBindingMigration); err != nil {
		return err
	}
	digest := api.Hash([]byte(queryBindingMigration))
	var old string
	err := tx.QueryRowContext(ctx, "SELECT artifact_digest FROM harness_migrations WHERE migration_id=2").Scan(&old)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if old != "" && old != digest {
		return api.E("invalid_state", "migration_artifact_changed")
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO harness_migrations(migration_id,artifact_digest,state,checkpoint) VALUES(2,$1,'applied','schema_ready') ON CONFLICT(migration_id) DO NOTHING", digest)
	return err
}
func queryRow(row postgresdb.GetQueryBindingForUpdateRow) runtime.QueryBinding {
	return runtime.QueryBinding{QueryID: row.QueryID, BindingID: row.BindingID, PrincipalID: row.PrincipalID, CredentialGeneration: uint64(row.CredentialGeneration), RolesDigest: row.RolesDigest, QueryDigest: row.QueryDigest, ResultDigest: row.ResultDigest, ExpiresAt: api.Time(row.ExpiresAt.UTC())}
}
func (s *Store) BindQuery(ctx context.Context, scope runtime.Scope, in runtime.QueryBindingInput) (runtime.QueryBinding, runtime.CommitStatus, error) {
	if err := durable.QueryInput(in); err != nil {
		return runtime.QueryBinding{}, runtime.RolledBack, err
	}
	var result runtime.QueryBinding
	status, err := s.Within(ctx, scope, []string{"runtime"}, func(value runtime.Tx) error {
		tx := value.(*transaction)
		if err := tx.queries.ExpireQueryBinding(ctx, postgresdb.ExpireQueryBindingParams{TenantID: scope.TenantID, OwnerID: scope.OwnerID, QueryID: in.QueryID}); err != nil {
			return err
		}
		if err := tx.queries.InsertQueryBinding(ctx, postgresdb.InsertQueryBindingParams{TenantID: scope.TenantID, OwnerID: scope.OwnerID, QueryID: in.QueryID, BindingID: api.NewID("binding"), PrincipalID: in.PrincipalID, CredentialGeneration: int64(in.CredentialGeneration), RolesDigest: in.RolesDigest, QueryDigest: in.QueryDigest, TtlMilliseconds: in.TTL.Milliseconds()}); err != nil {
			return err
		}
		row, err := tx.queries.GetQueryBindingForUpdate(ctx, postgresdb.GetQueryBindingForUpdateParams{TenantID: scope.TenantID, OwnerID: scope.OwnerID, QueryID: in.QueryID})
		if err != nil {
			return err
		}
		result = queryRow(row)
		if !durable.QueryMatches(result, in) {
			return api.E("idempotency_conflict", "query_id_conflict")
		}
		tx.queryExpiries = append(tx.queryExpiries, row.ExpiresAt.UTC())
		return nil
	})
	if status != runtime.Committed || err != nil {
		return runtime.QueryBinding{}, status, err
	}
	return result, status, nil
}
func (s *Store) SealQuery(ctx context.Context, scope runtime.Scope, expected runtime.QueryBinding, resultDigest string) (runtime.QueryBinding, runtime.CommitStatus, error) {
	if err := durable.QueryExpected(expected); err != nil {
		return runtime.QueryBinding{}, runtime.RolledBack, err
	}
	if err := durable.QueryDigest(resultDigest); err != nil {
		return runtime.QueryBinding{}, runtime.RolledBack, err
	}
	var result runtime.QueryBinding
	status, err := s.Within(ctx, scope, []string{"runtime"}, func(value runtime.Tx) error {
		tx := value.(*transaction)
		row, err := tx.queries.GetQueryBindingForUpdate(ctx, postgresdb.GetQueryBindingForUpdateParams{TenantID: scope.TenantID, OwnerID: scope.OwnerID, QueryID: expected.QueryID})
		if errors.Is(err, sql.ErrNoRows) {
			return api.E("cursor_expired", "query_binding_expired")
		}
		if err != nil {
			return err
		}
		result = queryRow(row)
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		if !durable.QueryOriginal(result, expected) || !now.Before(row.ExpiresAt) {
			return api.E("cursor_expired", "query_binding_expired")
		}
		if result.ResultDigest != "" && result.ResultDigest != resultDigest {
			return api.E("cursor_expired", "query_snapshot_changed")
		}
		if result.ResultDigest == "" {
			n, err := tx.queries.SealQueryBinding(ctx, postgresdb.SealQueryBindingParams{TenantID: scope.TenantID, OwnerID: scope.OwnerID, QueryID: expected.QueryID, BindingID: expected.BindingID, ResultDigest: sql.NullString{String: resultDigest, Valid: true}})
			if err != nil {
				return err
			}
			if n != 1 {
				return api.E("cursor_expired", "query_binding_expired")
			}
			result.ResultDigest = resultDigest
		}
		tx.queryExpiries = append(tx.queryExpiries, row.ExpiresAt.UTC())
		return nil
	})
	if status != runtime.Committed || err != nil {
		return runtime.QueryBinding{}, status, err
	}
	return result, status, nil
}
func (s *Store) PruneQueries(ctx context.Context, scope runtime.Scope, limit int) (int, runtime.CommitStatus, error) {
	if err := durable.Limits(limit); err != nil {
		return 0, runtime.RolledBack, err
	}
	count := 0
	status, err := s.Within(ctx, scope, []string{"runtime"}, func(value runtime.Tx) error {
		tx := value.(*transaction)
		rows, err := tx.queries.PruneQueryBindings(ctx, postgresdb.PruneQueryBindingsParams{TenantID: scope.TenantID, OwnerID: scope.OwnerID, BatchLimit: int32(limit)})
		count = len(rows)
		return err
	})
	if status != runtime.Committed || err != nil {
		return 0, status, err
	}
	return count, status, nil
}
