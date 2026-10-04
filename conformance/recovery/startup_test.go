//go:build integration

package recovery_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/adapters/sqlite"
)

func TestPostgresStartupPreservesContextCause(t *testing.T) {
	dsn := os.Getenv("LERNA_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Fatal("LERNA_TEST_POSTGRES_DSN is required")
	}
	cfg := postgres.Config{DSN: dsn, Schema: "lerna_startup", TransactionTimeout: time.Second, StatementTimeout: 500 * time.Millisecond, LockTimeout: 100 * time.Millisecond}
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(cause.Error(), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			if cause == context.DeadlineExceeded {
				ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			}
			cancel()
			store, err := postgres.Open(ctx, cfg)
			if store != nil {
				if closeErr := store.Close(); closeErr != nil {
					t.Fatal(errors.Join(err, closeErr))
				}
			}
			if err == nil {
				t.Fatal("startup unexpectedly succeeded")
			}
			if !errors.Is(err, cause) {
				t.Fatalf("startup lost context cause: %v", err)
			}
			if strings.Contains(fmt.Sprintf("%+v", err), dsn) {
				t.Fatal("startup exposed connection configuration")
			}
			sq, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "ledger.sqlite"), TransactionTimeout: time.Second, BusyTimeout: 100 * time.Millisecond})
			if sq != nil {
				if closeErr := sq.Close(); closeErr != nil {
					t.Fatal(errors.Join(err, closeErr))
				}
			}
			if err == nil {
				t.Fatal("SQLite canceled startup succeeded")
			}
			if !errors.Is(err, cause) {
				t.Fatalf("SQLite startup cause: %v", err)
			}
		})
	}
	store, err := postgres.Open(contextFor(t), cfg)
	if err != nil {
		if store != nil {
			err = errors.Join(err, store.Close())
		}
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresStartupPreservesDriverClassificationWithoutSecrets(t *testing.T) {
	cfg := postgres.Config{DSN: "postgres://startup-user:startup-secret@localhost/db?connect_timeout=invalid", Schema: "lerna_startup", TransactionTimeout: time.Second, StatementTimeout: 500 * time.Millisecond, LockTimeout: 100 * time.Millisecond}
	store, err := postgres.Open(contextFor(t), cfg)
	if store != nil {
		if closeErr := store.Close(); closeErr != nil {
			t.Fatal(errors.Join(err, closeErr))
		}
	}
	if err == nil {
		t.Fatal("invalid driver configuration succeeded")
	}
	var cause *pgconn.ParseConfigError
	if !errors.As(err, &cause) {
		t.Fatalf("driver classification lost: %v", err)
	}
	if errors.Unwrap(err) == nil {
		t.Fatal("driver cause lost")
	}
	for _, secret := range []string{cfg.DSN, "startup-user", "startup-secret", "connect_timeout"} {
		if strings.Contains(fmt.Sprintf("%v %+v %#v", err, err, err), secret) {
			t.Fatalf("startup output exposed configuration field %q", secret)
		}
	}
}
