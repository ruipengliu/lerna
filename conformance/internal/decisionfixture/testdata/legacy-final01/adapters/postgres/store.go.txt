// Package postgres implements internal Host storage ports with explicit SQL.
package postgres

import (
	"context"
	"database/sql"
	"time"

	"github.com/ruipengliu/lerna/adapters/postgres/internal/pgstore"
	"github.com/ruipengliu/lerna/contract"
	"github.com/ruipengliu/lerna/runtime"
)

type Config = pgstore.Config

// Store keeps the demo's owner-specific repositories and migration sequence.
type Store struct {
	db     *sql.DB
	config Config
	core   *pgstore.Core
}

func Open(ctx context.Context, cfg Config) (*Store, error) {
	core, err := pgstore.Open(ctx, cfg)
	if core == nil {
		return nil, err
	}
	// Nonempty + error is cleanup-only, never successful service startup.
	return &Store{db: core.DB(), config: cfg, core: core}, err
}
func (s *Store) Close() error                             { return s.core.Close() }
func (s *Store) table(name string) string                 { return s.core.Table(name) }
func (s *Store) CreateSchema(ctx context.Context) error   { return s.core.CreateSchema(ctx) }
func (s *Store) DropTestSchema(ctx context.Context) error { return s.core.DropTestSchema(ctx) }

type transaction = pgstore.Token

func (s *Store) token(ctx context.Context, token runtime.Tx, owner contract.OwnerRef) (*sql.Tx, error) {
	return s.core.SQL(ctx, token, owner)
}
func (s *Store) localToken(ctx context.Context, token runtime.Tx) (*sql.Tx, error) {
	return s.core.LocalSQL(ctx, token)
}
func (s *Store) Within(ctx context.Context, owner contract.OwnerRef, fn func(context.Context, runtime.Tx) error) error {
	return s.core.Within(ctx, owner, fn)
}
func (s *Store) Now(ctx context.Context, token runtime.Tx) (time.Time, error) {
	return s.core.Now(ctx, token)
}

type Settings = pgstore.Settings

func (s *Store) Settings(ctx context.Context, token runtime.Tx) (Settings, error) {
	return s.core.Settings(ctx, token)
}
