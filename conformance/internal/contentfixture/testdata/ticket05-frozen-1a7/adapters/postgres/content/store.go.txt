// Package content stores Content-owner facts in its own PostgreSQL namespace.
package content

import (
	"context"
	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/adapters/postgres/internal/pgstore"
)

type Store struct {
	core   *pgstore.Core
	schema string
}

func Open(ctx context.Context, config postgres.Config) (*Store, error) {
	core, err := pgstore.Open(ctx, config)
	if core == nil {
		return nil, err
	}
	return &Store{core: core, schema: config.Schema}, err
}
func (s *Store) Close() error                             { return s.core.Close() }
func (s *Store) CreateSchema(ctx context.Context) error   { return s.core.CreateSchema(ctx) }
func (s *Store) DropTestSchema(ctx context.Context) error { return s.core.DropTestSchema(ctx) }
