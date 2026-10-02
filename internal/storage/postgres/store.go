package postgres

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/ruipengliu/lerna/internal/durable"
	"github.com/ruipengliu/lerna/internal/storage/sqlstore"
)

type Store struct {
	*sqlstore.Store
	db     *sql.DB
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// Open fixes the connection budget. Notifications consume one extra connection.
// Its scope list comes from trusted assembly, not command payloads.
func Open(ctx context.Context, url string, scopes []durable.Scope, maxConnections int, notifications bool) (*Store, error) {
	if maxConnections < 1 || maxConnections > 16 {
		return nil, durable.ErrPrecondition
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, errors.New("invalid PostgreSQL configuration")
	}
	db.SetMaxOpenConns(maxConnections)
	db.SetMaxIdleConns(maxConnections)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, errors.New("PostgreSQL is unavailable")
	}
	if err := sqlstore.CheckVersion(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	base, err := sqlstore.New(db, "postgres", queries, scopes)
	if err != nil {
		db.Close()
		return nil, err
	}
	lifecycle, cancel := context.WithCancel(context.Background())
	s := &Store{Store: base, db: db, cancel: cancel}
	if notifications {
		hints := make(chan struct{}, 1)
		base.NotifyFunc = func() {
			select {
			case hints <- struct{}{}:
			default:
			}
		}
		s.wg.Add(2)
		go func() {
			defer s.wg.Done()
			for {
				select {
				case <-lifecycle.Done():
					return
				case <-hints:
					short, stop := context.WithTimeout(lifecycle, 200*time.Millisecond)
					_, _ = db.ExecContext(short, "SELECT pg_notify('lerna_durable','')")
					stop()
				}
			}
		}()
		go func() { defer s.wg.Done(); s.listen(lifecycle, url) }()
	}
	return s, nil
}
func (s *Store) listen(ctx context.Context, url string) {
	for ctx.Err() == nil {
		connect, stop := context.WithTimeout(ctx, 2*time.Second)
		conn, err := pgx.Connect(connect, url)
		stop()
		if err == nil {
			_, err = conn.Exec(ctx, "LISTEN lerna_durable")
			if err == nil {
				s.Signal()
				for ctx.Err() == nil {
					_, err = conn.WaitForNotification(ctx)
					if err != nil {
						break
					}
					s.Signal()
				}
			}
			closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			_ = conn.Close(closeCtx)
			cancel()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
}
func (s *Store) Close() error { s.cancel(); s.wg.Wait(); return s.db.Close() }
func Migrate(ctx context.Context, url string) error {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return errors.New("invalid PostgreSQL migration configuration")
	}
	defer db.Close()
	db.SetMaxOpenConns(2)
	return sqlstore.Migrate(ctx, db, "postgres")
}
