package contentfixture

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/domain/content"
	"strings"
	"sync"
	"time"
)

// HoldPolicy is a mechanical lock-wait fixture, never a business oracle.
// It retains both exact connections until their native Close is confirmed.
func (w *World) HoldPolicy(ctx context.Context, ref v.ContentRef) (func() error, func(context.Context) error) {
	return w.holdReadRow(ctx, `SELECT 1 FROM "`+w.Config.Schema+`".content_fixture_policies WHERE tenant_id=$1 AND owner_id=$2 AND content_id=$3 AND version=$4 FOR UPDATE`, ref.Owner.TenantID, ref.Owner.OwnerID, ref.ContentID, ref.Version)
}
func (w *World) HoldVersion(ctx context.Context, ref v.ContentRef) (func() error, func(context.Context) error) {
	return w.holdReadRow(ctx, `SELECT 1 FROM "`+w.Config.Schema+`".content_versions WHERE tenant_id=$1 AND owner_id=$2 AND content_id=$3 AND version=$4 FOR UPDATE`, ref.Owner.TenantID, ref.Owner.OwnerID, ref.ContentID, ref.Version)
}
func (w *World) HoldCommandReader(ctx context.Context, owner v.OwnerRef) (func() error, func(context.Context) error) {
	return w.holdReadRow(ctx, `SELECT 1 FROM "`+w.Config.Schema+`".content_fixture_command_readers WHERE tenant_id=$1 AND owner_id=$2 FOR UPDATE`, owner.TenantID, owner.OwnerID)
}

func (w *World) HoldCommand(ctx context.Context, ref v.CommandRef) (func() error, func(context.Context) error) {
	key, err := v.Encode(ref)
	if err != nil {
		w.t.Fatal(err)
	}
	return w.holdReadRow(ctx, `SELECT 1 FROM (SELECT pg_advisory_xact_lock(1,hashtext($1))) held`, w.Config.Schema+string(key))
}

// Only the concrete lock controls above can supply this private query.
func (w *World) holdReadRow(ctx context.Context, lockQuery string, args ...any) (release func() error, waitBlocked func(context.Context) error) {
	w.t.Helper()
	locker, err := sql.Open("pgx", w.Config.DSN)
	if err != nil {
		w.t.Fatal(err)
	}
	locker.SetMaxOpenConns(1)
	var observer *sql.DB
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
			closeErr = errors.Join(closeErr, locker.Close())
			if observer != nil {
				closeErr = errors.Join(closeErr, observer.Close())
			}
		})
		return closeErr
	}
	w.infrastructureClosers = append(w.infrastructureClosers, release)
	observer, err = sql.Open("pgx", w.Config.DSN)
	if err != nil {
		w.t.Fatal(err)
	}
	observer.SetMaxOpenConns(1)
	if err = locker.PingContext(ctx); err != nil {
		w.t.Fatal(err)
	}
	if err = observer.PingContext(ctx); err != nil {
		w.t.Fatal(err)
	}
	if err = w.register("pg_policy_connections " + w.Config.Schema); err != nil {
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
	if err = w.register(fmt.Sprintf("pg_policy_holder %s %d", w.Config.Schema, pid)); err != nil {
		w.t.Fatal(err)
	}
	var revision int64
	if err = tx.QueryRowContext(ctx, lockQuery, args...).Scan(&revision); err != nil {
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

// FailResponsibilityWrites installs a test-only native PostgreSQL constraint.
// It injects a real transactional write refusal, never reads business state.
func (w *World) FailResponsibilityWrites(ctx context.Context) func() error {
	w.t.Helper()
	db, err := sql.Open("pgx", w.Config.DSN)
	if err != nil {
		w.t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	var once sync.Once
	var closeErr error
	release := func() error {
		once.Do(func() {
			bounded, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, err := db.ExecContext(bounded, `ALTER TABLE "`+w.Config.Schema+`".content_cleanup_responsibilities DROP CONSTRAINT test_refuse_responsibility`)
			closeErr = errors.Join(err, db.Close())
		})
		return closeErr
	}
	w.infrastructureClosers = append(w.infrastructureClosers, release)
	if err = db.PingContext(ctx); err != nil {
		w.t.Fatal(err)
	}
	var pid int
	if err = db.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		w.t.Fatal(err)
	}
	if err = w.register(fmt.Sprintf("pg_policy_holder %s %d", w.Config.Schema, pid)); err != nil {
		w.t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `ALTER TABLE "`+w.Config.Schema+`".content_cleanup_responsibilities ADD CONSTRAINT test_refuse_responsibility CHECK(false) NOT VALID`); err != nil {
		w.t.Fatal(err)
	}
	return release
}

// PolicyClockPlan observes only the execution plan of the exact proposed query.
// It reads no policy bodies and logs no query parameters. The existing World
// owns this connection and confirms its Close before destructive cleanup.
func (w *World) PolicyClockPlan(ctx context.Context, policy content.FixturePolicy) ([]string, error) {
	db, err := sql.Open("pgx", w.Config.DSN)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	var once sync.Once
	var closeErr error
	closeDB := func() error { once.Do(func() { closeErr = db.Close() }); return closeErr }
	w.infrastructureClosers = append(w.infrastructureClosers, closeDB)
	if err = db.PingContext(ctx); err != nil {
		return nil, err
	}
	var pid int
	if err = db.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		return nil, err
	}
	if err = w.register(fmt.Sprintf("pg_policy_holder %s %d", w.Config.Schema, pid)); err != nil {
		return nil, err
	}
	subject, err := v.Encode(policy.Subject)
	if err != nil {
		return nil, err
	}
	key := sha256.Sum256(subject)
	query := `EXPLAIN (VERBOSE, COSTS OFF) WITH locked_policy AS MATERIALIZED (SELECT body FROM "` + w.Config.Schema + `".content_fixture_policies WHERE tenant_id=$1 AND owner_id=$2 AND subject_key=$3 AND content_id=$4 AND version=$5 AND purpose=$6 FOR SHARE) SELECT body,clock_timestamp() FROM locked_policy`
	rows, err := db.QueryContext(ctx, query, policy.Ref.Owner.TenantID, policy.Ref.Owner.OwnerID, fmt.Sprintf("%x", key), policy.Ref.ContentID, policy.Ref.Version, policy.Purpose)
	if err != nil {
		return nil, err
	}
	var plan []string
	for rows.Next() {
		var line string
		if err = rows.Scan(&line); err != nil {
			break
		}
		if strings.Contains(line, "Cond:") || strings.Contains(line, "Filter:") {
			continue
		}
		plan = append(plan, line)
	}
	err = errors.Join(err, rows.Err(), rows.Close())
	return plan, err
}
