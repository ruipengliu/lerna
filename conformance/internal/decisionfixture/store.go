// Package decisionfixture provides durable test identities, never production
// Task, Content, Grant or installation authority.
package decisionfixture

import (
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/ruipengliu/lerna/adapters/postgres"
	decision "github.com/ruipengliu/lerna/components/decision_engine"
	v "github.com/ruipengliu/lerna/contract/v1_1"
	"github.com/ruipengliu/lerna/runtime"
)

type Store struct {
	db      *sql.DB
	cfg     postgres.Config
	owner   v.OwnerRef
	created bool
}

var identifier = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
var testIdentifier = regexp.MustCompile(`^lerna_test_[a-f0-9]{24}$`)

type connectionError struct{ cause error }

func (e *connectionError) Error() string { return "fixture PostgreSQL connection unavailable" }
func (e *connectionError) Unwrap() error { return e.cause }

func Open(ctx context.Context, cfg postgres.Config, owner v.OwnerRef) (*Store, error) {
	if ctx == nil || cfg.DSN == "" || !identifier.MatchString(cfg.Schema) || cfg.TransactionTimeout <= 0 || cfg.StatementTimeout < time.Millisecond || cfg.LockTimeout < time.Millisecond || cfg.StatementTimeout > cfg.TransactionTimeout || cfg.LockTimeout > cfg.StatementTimeout || cfg.MaxOpenConnections < 0 || cfg.MaxOpenConnections > 64 {
		return nil, errors.New("invalid fixture PostgreSQL configuration")
	}
	if _, err := v.Encode(owner); err != nil {
		return nil, err
	}
	bounded, cancel := context.WithTimeout(ctx, cfg.TransactionTimeout)
	defer cancel()
	db, err := sql.Open("pgx", cfg.DSN)
	if err != nil {
		return nil, &connectionError{err}
	}
	connections := cfg.MaxOpenConnections
	if connections == 0 {
		connections = 4
	}
	db.SetMaxOpenConns(connections)
	db.SetMaxIdleConns(connections)
	if err = db.PingContext(bounded); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			// A caller must retain this handle until closure is confirmed.
			return &Store{db: db, cfg: cfg, owner: owner}, errors.Join(&connectionError{err}, closeErr)
		}
		return nil, &connectionError{err}
	}
	return &Store{db: db, cfg: cfg, owner: owner}, nil
}
func (s *Store) Close() error             { return s.db.Close() }
func (s *Store) table(name string) string { return `"` + s.cfg.Schema + `"."` + name + `"` }
func (s *Store) CreateSchema(ctx context.Context) error {
	if ctx == nil {
		return errors.New("fixture context required")
	}
	bounded, cancel := context.WithTimeout(ctx, s.cfg.TransactionTimeout)
	defer cancel()
	_, err := s.db.ExecContext(bounded, `CREATE SCHEMA "`+s.cfg.Schema+`"`)
	if err == nil {
		s.created = true
	}
	return err
}

// Only the live administrative handle that received CREATE success can delete.
func (s *Store) DropTestSchema(ctx context.Context) error {
	if ctx == nil || !s.created || !testIdentifier.MatchString(s.cfg.Schema) {
		return errors.New("fixture schema ownership denied")
	}
	bounded, cancel := context.WithTimeout(ctx, s.cfg.TransactionTimeout)
	defer cancel()
	_, err := s.db.ExecContext(bounded, `DROP SCHEMA "`+s.cfg.Schema+`" CASCADE`)
	if err == nil {
		s.created = false
	}
	return err
}
func (s *Store) within(ctx context.Context, fn func(context.Context, *sql.Tx, time.Time) error) error {
	if ctx == nil || fn == nil {
		return errors.New("fixture finite context required")
	}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.TransactionTimeout)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `SELECT set_config('synchronous_commit','on',true),set_config('statement_timeout',$1,true),set_config('lock_timeout',$2,true)`, fmt.Sprintf("%dms", s.cfg.StatementTimeout.Milliseconds()), fmt.Sprintf("%dms", s.cfg.LockTimeout.Milliseconds()))
	if err != nil {
		return err
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return err
	}
	if err = fn(ctx, tx, now); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		if errors.Is(err, pgx.ErrTxCommitRollback) {
			return err
		}
		return fmt.Errorf("%w: %w", runtime.ErrCommitUnknown, err)
	}
	return nil
}

//go:embed migrations/0001_fixture.sql
var migration string

func (s *Store) Migrate(ctx context.Context) error {
	return s.within(ctx, func(ctx context.Context, tx *sql.Tx, _ time.Time) error {
		// The immutable owner-local migration has no dependency on host migrations.
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(1786614101,hashtext($1))`, s.cfg.Schema); err != nil {
			return err
		}
		source := fmt.Sprintf(migration, s.table("fixture_migrations"), s.table("fixture_decisions"), s.table("fixture_permissions"), s.table("fixture_permissions"), s.table("fixture_objects"), s.table("fixture_publications"), s.table("fixture_permissions"))
		checksum := digest([]byte(migration))
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, s.table("fixture_migrations")).Scan(&exists); err != nil {
			return err
		}
		if exists {
			var stored string
			if err := tx.QueryRowContext(ctx, `SELECT checksum FROM `+s.table("fixture_migrations")+` WHERE version=1`).Scan(&stored); err != nil {
				return err
			}
			if stored != checksum {
				return errors.New("fixture migration checksum mismatch")
			}
			return nil
		}
		if _, err := tx.ExecContext(ctx, source); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO `+s.table("fixture_migrations")+` VALUES(1,$1)`, checksum)
		return err
	})
}
func digest(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}
func (s *Store) Ref(id v.ID, media string, body []byte) v.ContentRef {
	return v.ContentRef{Owner: s.owner, ContentID: id, Version: "1", Hash: digest(body), MediaType: media, ByteLength: v.Revision(fmt.Sprint(len(body)))}
}

var _ decision.Authority = (*Store)(nil)
var _ decision.Source = (*Store)(nil)
var _ decision.Publisher = (*Store)(nil)
