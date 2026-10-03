//go:build integration

package recovery_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/adapters/sqlite"
)

// ownedFixture is a physical test scope. Store returns the actual product
// writer, never a proxy: Host transactions retain their same-Store identity.
// Administrative ownership survives every business writer generation.
type ownedFixture struct {
	backend   string
	pg        postgres.Config
	sq        sqlite.Config
	admin     *postgres.Store
	owns      bool
	directory string
	current   waitStore
	peers     []*postgres.Store
	holders   []*pgTransactionHold
	closing   bool
	child     *hostProcess
}

func newOwnedFixture(t *testing.T, backend string, migrate bool) *ownedFixture {
	t.Helper()
	f, err := createOwnedFixture(contextFor(t), backend, migrate)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.Cleanup(); err != nil {
			t.Error(err)
		}
	})
	return f
}

// Setup failures retain their cause and run independent, bounded cleanup before
// returning. No CREATE acknowledgement means no deletion capability.
func createOwnedFixture(ctx context.Context, backend string, migrate bool) (f *ownedFixture, err error) {
	f = &ownedFixture{backend: backend}
	defer func() {
		if err != nil {
			err = errors.Join(err, f.Cleanup())
		}
	}()
	if backend == "postgres" {
		var nonce [12]byte
		if _, err = rand.Read(nonce[:]); err != nil {
			return f, err
		}
		f.pg = postgres.Config{DSN: os.Getenv("LERNA_TEST_POSTGRES_DSN"), Schema: "lerna_test_" + hex.EncodeToString(nonce[:]), TransactionTimeout: 3 * time.Second, StatementTimeout: 2 * time.Second, LockTimeout: time.Second}
		if f.pg.DSN == "" {
			return f, errors.New("LERNA_TEST_POSTGRES_DSN is required")
		}
		if f.admin, err = postgres.Open(ctx, f.pg); err != nil {
			return f, err
		}
		if err = f.admin.CreateSchema(ctx); err != nil {
			return f, fmt.Errorf("fixture postgres create: %w", err)
		}
		f.owns = true
		if err = registerOwnedScope("postgres", f.pg.Schema); err != nil {
			return f, err
		}
	} else if backend == "sqlite" {
		if f.directory, err = os.MkdirTemp("", "lerna-owned-fixture-"); err != nil {
			return f, err
		}
		f.owns = true
		f.sq = sqlite.Config{Path: filepath.Join(f.directory, "ledger.sqlite"), TransactionTimeout: 3 * time.Second, BusyTimeout: 100 * time.Millisecond}
		if err = registerOwnedScope("sqlite", f.directory); err != nil {
			return f, err
		}
	} else {
		return f, errors.New("unsupported fixture backend")
	}
	if migrate {
		var store waitStore
		if store, err = f.Open(ctx); err != nil {
			return f, err
		}
		if err = store.(interface{ Migrate(context.Context) error }).Migrate(ctx); err != nil {
			return f, fmt.Errorf("fixture %s migrate: %w", backend, err)
		}
	}
	return f, nil
}
func (f *ownedFixture) Store() waitStore      { return f.current }
func (f *ownedFixture) PG() *postgres.Store   { return f.current.(*postgres.Store) }
func (f *ownedFixture) SQLite() *sqlite.Store { return f.current.(*sqlite.Store) }
func (f *ownedFixture) Open(ctx context.Context) (waitStore, error) {
	if f.closing {
		return nil, errors.New("fixture is closing")
	}
	if f.current != nil {
		return nil, errors.New("fixture writer already open")
	}
	if f.child != nil && !f.child.waited {
		return nil, errors.New("fixture child exit unconfirmed")
	}
	var store waitStore
	var err error
	if f.backend == "postgres" {
		store, err = postgres.Open(ctx, f.pg)
	} else {
		store, err = sqlite.Open(ctx, f.sq)
	}
	if err != nil {
		return nil, fmt.Errorf("fixture %s open: %w", f.backend, err)
	}
	f.current = store
	return store, nil
}
func (f *ownedFixture) CloseWriter() error {
	if f.current == nil {
		return nil
	}
	if err := f.current.Close(); err != nil {
		return fmt.Errorf("fixture %s close: %w", f.backend, err)
	}
	f.current = nil
	return nil
}
func (f *ownedFixture) Replace(t *testing.T) waitStore {
	t.Helper()
	if err := f.CloseWriter(); err != nil {
		t.Fatal(err)
	}
	store, err := f.Open(contextFor(t))
	if err != nil {
		t.Fatal(err)
	}
	return store
}
func (f *ownedFixture) Cleanup() error {
	f.closing = true
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := f.joinPGHolders(); err != nil {
		return err
	}
	if f.child != nil {
		if err := f.child.stop(ctx); err != nil {
			return err
		}
	}
	var errs []error
	if err := f.CloseWriter(); err != nil {
		errs = append(errs, err)
	}
	for i, peer := range f.peers {
		if peer == nil {
			continue
		}
		if err := peer.Close(); err != nil {
			errs = append(errs, err)
		} else {
			f.peers[i] = nil
		}
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	if f.owns {
		var err error
		if f.backend == "postgres" {
			err = f.admin.DropTestSchema(ctx)
		} else {
			err = os.RemoveAll(f.directory)
		}
		if err != nil {
			return fmt.Errorf("fixture %s cleanup: %w", f.backend, err)
		}
		f.owns = false
	}
	if f.admin != nil {
		if err := f.admin.Close(); err != nil {
			return err
		}
		f.admin = nil
	}
	return nil
}

func registerOwnedScope(backend, scope string) error {
	path := os.Getenv("LERNA_TEST_OWNED_SCOPE_REGISTRY")
	if path == "" {
		return nil
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("owned fixture registry open: %w", err)
	}
	_, err = fmt.Fprintln(file, backend, scope)
	return errors.Join(err, file.Sync(), file.Close())
}

// These are the actual PG variants used by independent connection, wait and
// fault stories. No peer receives administrative deletion authority.
func (f *ownedFixture) PGPeer(t *testing.T, connections int, timeout time.Duration) *postgres.Store {
	t.Helper()
	cfg := f.pg
	if connections != 0 {
		cfg.MaxOpenConnections = connections
	}
	if timeout != 0 {
		cfg.TransactionTimeout = timeout
	}
	return f.openPGPeer(t, cfg)
}
func (f *ownedFixture) openPGPeer(t *testing.T, cfg postgres.Config) *postgres.Store {
	t.Helper()
	if f.closing {
		t.Fatal("fixture is closing")
	}
	peer, err := postgres.Open(contextFor(t), cfg)
	if err != nil {
		t.Fatal(err)
	}
	f.peers = append(f.peers, peer)
	return peer
}
func (f *ownedFixture) ConcurrentWriter(t *testing.T) waitStore {
	if f.backend == "postgres" {
		return f.PGPeer(t, 0, 0)
	}
	return f.Store()
}

func (f *ownedFixture) CopySQLite(t *testing.T) *ownedFixture {
	t.Helper()
	if f.backend != "sqlite" {
		t.Fatal("SQLite copy requires SQLite scope")
	}
	if err := f.CloseWriter(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(f.sq.Path)
	if err != nil {
		t.Fatal(err)
	}
	copy := newOwnedFixture(t, "sqlite", false)
	if err = os.WriteFile(copy.sq.Path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = copy.Open(contextFor(t)); err != nil {
		t.Fatal(err)
	}
	return copy
}

func (f *ownedFixture) prepareChild() error {
	if f.closing {
		return errors.New("fixture is closing")
	}
	if f.child != nil && !f.child.waited {
		return errors.New("fixture child exit unconfirmed")
	}
	return f.CloseWriter()
}
