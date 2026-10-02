package host

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "modernc.org/sqlite"
)

type databaseHandle struct {
	ping  func(context.Context) error
	close func()
}

func openDatabase(ctx context.Context, config DatabaseConfig) (databaseHandle, error) {
	if config.Driver == "postgres" {
		value := os.Getenv(config.URLEnv)
		if value == "" {
			return databaseHandle{}, errors.New("configured PostgreSQL URL environment variable is missing")
		}
		poolConfig, err := pgxpool.ParseConfig(value)
		if err != nil {
			return databaseHandle{}, errors.New("invalid PostgreSQL connection configuration")
		}
		poolConfig.MaxConns = int32(config.MaxConnections)
		pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
		if err != nil {
			return databaseHandle{}, errors.New("could not open PostgreSQL pool")
		}
		if err := pool.Ping(ctx); err != nil {
			pool.Close()
			return databaseHandle{}, errors.New("PostgreSQL dependency is unavailable; check local database and credentials")
		}
		return databaseHandle{ping: pool.Ping, close: pool.Close}, nil
	}
	// SQLite locking belongs to the single local host, never the PostgreSQL process group.
	if info, err := os.Stat(filepath.Dir(config.Path)); err != nil || !info.IsDir() {
		return databaseHandle{}, errors.New("SQLite data directory is missing; initialize it explicitly")
	}
	lock, err := os.OpenFile(config.Path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return databaseHandle{}, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return databaseHandle{}, errors.New("SQLite host is already running for this database")
	}
	connectionURL := url.URL{Scheme: "file", Path: config.Path}
	query := connectionURL.Query()
	query.Add("_pragma", "journal_mode(WAL)")
	query.Add("_pragma", "synchronous(FULL)")
	query.Add("_pragma", "foreign_keys(ON)")
	query.Add("_pragma", "busy_timeout(2000)")
	connectionURL.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", connectionURL.String())
	if err != nil {
		lock.Close()
		return databaseHandle{}, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	closeDatabase := func() { db.Close(); lock.Close() }
	ping := func(ctx context.Context) error {
		connection, err := db.Conn(ctx)
		if err != nil {
			return err
		}
		defer connection.Close()
		var journal string
		var synchronous, foreignKeys int
		if err := connection.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journal); err != nil {
			return err
		}
		if err := connection.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&synchronous); err != nil {
			return err
		}
		if err := connection.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
			return err
		}
		if journal != "wal" || synchronous != 2 || foreignKeys != 1 {
			return errors.New("SQLite requires WAL, FULL and foreign_keys")
		}
		return nil
	}
	if err := ping(ctx); err != nil {
		closeDatabase()
		return databaseHandle{}, err
	}
	return databaseHandle{ping: ping, close: closeDatabase}, nil
}
