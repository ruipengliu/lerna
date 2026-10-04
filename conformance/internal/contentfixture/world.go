// Package contentfixture owns real PG and object roots for Content conformance.
package contentfixture

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/objectstore/local"
	pg "github.com/ruipengliu/lerna/adapters/postgres"
	pgcontent "github.com/ruipengliu/lerna/adapters/postgres/content"
)

type World struct {
	t                     *testing.T
	Config                pg.Config
	Directory             string
	device, inode         uint64
	admin, current        *pgcontent.Store
	Objects               *local.Store
	owns                  bool
	closing               bool
	setupClosers          []func() error
	setupCloseErr         error
	setupNativeClose      func(*os.File) error
	retainedInvocation    bool
	infrastructureClosers []func() error
}

func New(t *testing.T, ctx context.Context) *World {
	t.Helper()
	w := &World{t: t}
	t.Cleanup(func() {
		if err := w.Cleanup(); err != nil {
			t.Error(err)
		}
	})
	registry := os.Getenv("LERNA_TEST_OWNED_SCOPE_REGISTRY")
	dsn := os.Getenv("LERNA_TEST_POSTGRES_DSN")
	if dsn == "" || !filepath.IsAbs(registry) || !filepath.IsAbs(os.Getenv("TMPDIR")) {
		t.Fatal("real PostgreSQL and absolute owned scope/TMPDIR required")
	}
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	w.Config = pg.Config{DSN: dsn, Schema: "lerna_test_" + hex.EncodeToString(nonce[:]), MaxOpenConnections: 1, TransactionTimeout: 3 * time.Second, StatementTimeout: 2 * time.Second, LockTimeout: time.Second}
	var err error
	w.admin, err = pgcontent.Open(ctx, w.Config)
	if err != nil {
		t.Fatal(err)
	}
	if err = w.admin.CreateSchema(ctx); err != nil {
		t.Fatal(err)
	}
	w.owns = true
	if err = w.register("postgres " + w.Config.Schema); err != nil {
		t.Fatal(err)
	}
	w.Directory, err = os.MkdirTemp("", "lerna-content-objects-")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(w.Directory)
	if err != nil {
		t.Fatal(err)
	}
	st := info.Sys().(*syscall.Stat_t)
	w.device = uint64(st.Dev)
	w.inode = st.Ino
	directory, err := os.Open(w.Directory)
	if err != nil {
		t.Fatal(err)
	}
	closeDirectory := w.ownSetupFile(directory)
	parent, err := os.Open(filepath.Dir(w.Directory))
	if err != nil {
		t.Fatal(errors.Join(err, closeDirectory()))
	}
	closeParent := w.ownSetupFile(parent)
	if err = errors.Join(directory.Sync(), parent.Sync(), closeDirectory(), closeParent()); err != nil {
		t.Fatal(err)
	}
	if err = w.register(fmt.Sprintf("objects %s %d %d", w.Directory, w.device, w.inode)); err != nil {
		t.Fatal(err)
	}
	w.current, err = pgcontent.Open(ctx, w.Config)
	if err != nil {
		t.Fatal(err)
	}
	if err = w.current.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	w.Objects, err = local.Open(w.Directory)
	if err != nil {
		t.Fatal(err)
	}
	return w
}
func (w *World) register(line string) (err error) {
	defer func() {
		if err != nil {
			w.setupCloseErr = errors.Join(w.setupCloseErr, err)
		}
	}()
	path := os.Getenv("LERNA_TEST_OWNED_SCOPE_REGISTRY")
	if !filepath.IsAbs(path) {
		return errors.New("absolute owned registry required")
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	closeFile := w.ownSetupFile(f)
	_, err = fmt.Fprintln(f, line)
	err = errors.Join(err, f.Sync(), closeFile())
	if err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	closeDirectory := w.ownSetupFile(dir)
	return errors.Join(dir.Sync(), closeDirectory())
}
func (w *World) Store() *pgcontent.Store { return w.current }
func (w *World) Reopen(ctx context.Context) {
	w.t.Helper()
	if w.closing {
		w.t.Fatal("world closing")
	}
	if err := w.current.Close(); err != nil {
		w.t.Fatal(err)
	}
	w.current = nil
	if err := w.Objects.Close(); err != nil {
		w.t.Fatal(err)
	}
	w.Objects = nil
	var err error
	w.current, err = pgcontent.Open(ctx, w.Config)
	if err != nil {
		w.t.Fatal(err)
	}
	w.Objects, err = local.Open(w.Directory)
	if err != nil {
		w.t.Fatal(err)
	}
}
func (w *World) Cleanup() error {
	w.closing = true
	if w.retainedInvocation {
		return errors.New("retain exact scope: public invocation exit unconfirmed")
	}
	for _, closeInfrastructure := range w.infrastructureClosers {
		if err := closeInfrastructure(); err != nil {
			return err
		}
	}
	if w.current != nil {
		if err := w.current.Close(); err != nil {
			return err
		}
		w.current = nil
	}
	if w.Objects != nil {
		if err := w.Objects.Close(); err != nil {
			return err
		}
		w.Objects = nil
	}
	for _, closeFile := range w.setupClosers {
		_ = closeFile()
	}
	if w.setupCloseErr != nil {
		return w.setupCloseErr
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if w.owns {
		if err := w.admin.DropTestSchema(ctx); err != nil {
			return err
		}
		w.owns = false
	}
	if w.admin != nil {
		if err := w.admin.Close(); err != nil {
			return err
		}
		w.admin = nil
	}
	if w.Directory != "" {
		info, err := os.Lstat(w.Directory)
		if err != nil {
			return err
		}
		st := info.Sys().(*syscall.Stat_t)
		if uint64(st.Dev) != w.device || st.Ino != w.inode {
			return errors.New("owned object root identity changed")
		}
		if err = os.RemoveAll(w.Directory); err != nil {
			return err
		}
		w.Directory = ""
	}
	return nil
}

func (w *World) ownSetupFile(file *os.File) func() error {
	var once sync.Once
	var firstErr error
	nativeClose := file.Close
	if w.setupNativeClose != nil {
		close := w.setupNativeClose
		nativeClose = func() error { return close(file) }
	}
	closeFile := func() error {
		once.Do(func() {
			firstErr = nativeClose()
			if firstErr != nil {
				w.setupCloseErr = errors.Join(w.setupCloseErr, firstErr)
			}
		})
		return firstErr
	}
	w.setupClosers = append(w.setupClosers, closeFile)
	return closeFile
}

// RetainInvocation seals destructive cleanup when a finite wait cannot prove exit.
func (w *World) RetainInvocation() { w.retainedInvocation = true }
