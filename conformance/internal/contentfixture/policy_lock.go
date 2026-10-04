package contentfixture

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"sync"
	"time"
)

// HoldPolicy is a mechanical lock-wait fixture, never a business oracle.
// It retains both exact connections until their native Close is confirmed.
func (w *World) HoldPolicy(ctx context.Context, ref v.ContentRef) (release func() error, waitBlocked func(context.Context) error) {
	w.t.Helper()
	locker, err := sql.Open("pgx", w.Config.DSN)
	if err != nil {
		w.t.Fatal(err)
	}
	locker.SetMaxOpenConns(1)
	observer, err := sql.Open("pgx", w.Config.DSN)
	if err != nil {
		w.t.Fatal(errors.Join(err, locker.Close()))
	}
	observer.SetMaxOpenConns(1)
	var tx *sql.Tx
	var once sync.Once
	var closeErr error
	release = func() error {
		once.Do(func() {
			if tx != nil {
				err := tx.Rollback()
				if err != nil && !errors.Is(err, sql.ErrTxDone) {
					closeErr = errors.Join(closeErr, err)
				}
			}
			closeErr = errors.Join(closeErr, locker.Close(), observer.Close())
		})
		return closeErr
	}
	w.infrastructureClosers = append(w.infrastructureClosers, release)
	if err = locker.PingContext(ctx); err != nil {
		w.t.Fatal(err)
	}
	if err = observer.PingContext(ctx); err != nil {
		w.t.Fatal(err)
	}
	if err = register("pg_policy_connections " + w.Config.Schema); err != nil {
		w.t.Fatal(err)
	}
	tx, err = locker.BeginTx(ctx, nil)
	if err != nil {
		w.t.Fatal(err)
	}
	var pid int
	if err = tx.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		w.t.Fatal(err)
	}
	if err = register(fmt.Sprintf("pg_policy_holder %s %d", w.Config.Schema, pid)); err != nil {
		w.t.Fatal(err)
	}
	var revision int64
	if err = tx.QueryRowContext(ctx, `SELECT revision FROM "`+w.Config.Schema+`".content_fixture_policies WHERE tenant_id=$1 AND owner_id=$2 AND content_id=$3 AND version=$4 FOR UPDATE`, ref.Owner.TenantID, ref.Owner.OwnerID, ref.ContentID, ref.Version).Scan(&revision); err != nil {
		w.t.Fatal(err)
	}
	waitBlocked = func(ctx context.Context) error {
		timer := time.NewTicker(5 * time.Millisecond)
		defer timer.Stop()
		for {
			var blocked bool
			if err := observer.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`, pid).Scan(&blocked); err != nil {
				return err
			}
			if blocked {
				return nil
			}
			select {
			case <-timer.C:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return release, waitBlocked
}
