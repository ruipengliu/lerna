package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"syscall"

	"github.com/ruipengliu/lerna/internal/durable"
	"github.com/ruipengliu/lerna/internal/storage/sqlstore"
	_ "modernc.org/sqlite"
)

type Store struct {
	*sqlstore.Store
	db   *sql.DB
	lock *os.File
}

func openFile(ctx context.Context, path string, create bool) (*sql.DB, *os.File, error) {
	if !filepath.IsAbs(path) {
		return nil, nil, durable.ErrPrecondition
	}
	if info, err := os.Stat(filepath.Dir(path)); err != nil || !info.IsDir() {
		return nil, nil, errors.New("original SQLite directory is missing")
	}
	if !create {
		if _, err := os.Stat(path); err != nil {
			return nil, nil, errors.New("original SQLite database is missing")
		}
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, nil, err
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return nil, nil, errors.New("SQLite database is owned by another host or migration")
	}
	if create {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			lock.Close()
			return nil, nil, err
		}
		f.Close()
	}
	u := url.URL{Scheme: "file", Path: path}
	q := u.Query()
	for _, p := range []string{"journal_mode(WAL)", "synchronous(FULL)", "foreign_keys(ON)", "busy_timeout(0)"} {
		q.Add("_pragma", p)
	}
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		lock.Close()
		return nil, nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	var journal string
	var sync, fk int
	if err = db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journal); err == nil {
		err = db.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&sync)
	}
	if err == nil {
		err = db.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk)
	}
	if err != nil || journal != "wal" || sync != 2 || fk != 1 {
		db.Close()
		lock.Close()
		return nil, nil, errors.New("SQLite requires WAL, FULL and foreign_keys")
	}
	return db, lock, nil
}
func Open(ctx context.Context, path string, scopes []durable.Scope) (*Store, error) {
	db, lock, err := openFile(ctx, path, false)
	if err != nil {
		return nil, err
	}
	if err = sqlstore.CheckVersion(ctx, db); err != nil {
		db.Close()
		lock.Close()
		return nil, err
	}
	base, err := sqlstore.New(db, "sqlite", queries, scopes)
	if err != nil {
		db.Close()
		lock.Close()
		return nil, err
	}
	return &Store{base, db, lock}, nil
}
func (s *Store) Close() error { err := s.db.Close(); return errors.Join(err, s.lock.Close()) }
func Migrate(ctx context.Context, path string) error {
	db, lock, err := openFile(ctx, path, true)
	if err != nil {
		return err
	}
	defer lock.Close()
	defer db.Close()
	return sqlstore.Migrate(ctx, db, "sqlite")
}
