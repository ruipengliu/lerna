package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/internal/sqlclosetest"
)

func TestSQLiteMechanicalCloseFailureKeepsWriterReleaseUnattempted(t *testing.T) {
	fault := errors.New("mechanical database/sql close uncertainty")
	db := sqlclosetest.Open(sqlclosetest.Fault{CloseError: fault})
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	shutdown, stop := context.WithCancel(context.Background())
	released := false
	store := &Store{db: db, config: Config{TransactionTimeout: time.Second}, writer: make(chan struct{}, 1), closer: make(chan struct{}, 1), shutdown: shutdown, stop: stop, releaseWriter: func() error { released = true; return nil }}
	for range 2 {
		if err := store.Close(); !errors.Is(err, fault) {
			t.Fatal("native Close unknown erased", err)
		}
	}
	if released {
		t.Fatal("native Close uncertainty released writer exclusion")
	}
}

func TestSQLiteMechanicalOpenPreservesStartupAndReleaseOutcomes(t *testing.T) {
	ping := errors.New("startup-driver-secret")
	closed := errors.New("close-driver-secret")
	releaseErr := errors.New("writer-release-secret")
	for _, name := range []string{"success", "ping_failure", "dual_failure", "cancelled", "open_failure", "release_failure", "acquire_cleanup_failure"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			var pingErr, closeErr error
			if name == "ping_failure" || name == "dual_failure" || name == "release_failure" {
				pingErr = ping
			}
			if name == "dual_failure" {
				closeErr = closed
			}
			if name == "cancelled" {
				cancel()
			}
			done := make(chan struct{})
			db := sqlclosetest.Open(sqlclosetest.Fault{PingError: pingErr, CloseError: closeErr, CloseDone: done})
			released := false
			releaseAttempts := 0
			release := func() error {
				releaseAttempts++
				released = true
				if name == "release_failure" || name == "acquire_cleanup_failure" {
					return releaseErr
				}
				return nil
			}
			acquire := func(ctx context.Context, _ string) (func() error, error) {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				if name == "acquire_cleanup_failure" {
					return release, ping
				}
				return release, nil
			}
			cfg := Config{Path: "mechanical-no-physical-file", TransactionTimeout: time.Second, BusyTimeout: time.Millisecond}
			store, err := open(ctx, cfg, func(string, string) (*sql.DB, error) {
				if name == "open_failure" {
					return nil, ping
				}
				if name == "acquire_cleanup_failure" {
					t.Error("failed acquisition reached database construction")
				}
				return db, nil
			}, acquire)
			if name == "open_failure" || name == "cancelled" || name == "acquire_cleanup_failure" {
				if e := db.Close(); e != nil {
					t.Fatal(e)
				}
			}
			switch name {
			case "success":
				if err != nil || store == nil {
					t.Fatal("startup", err)
				}
				for range 2 {
					if e := store.Close(); e != nil {
						t.Fatal(e)
					}
				}
				if !released {
					t.Fatal("confirmed close did not release writer")
				}
				select {
				case <-done:
				default:
					t.Fatal("writer released before actual database close")
				}
			case "dual_failure":
				if store == nil || !errors.Is(err, ping) || !errors.Is(err, closed) || released {
					t.Fatal("startup lost uncertainty or released exclusion", err)
				}
				for range 2 {
					if e := store.Close(); !errors.Is(e, closed) {
						t.Fatal(e)
					}
				}
				if released {
					t.Fatal("unknown native close released writer")
				}
			case "release_failure", "acquire_cleanup_failure":
				if store == nil || !errors.Is(err, ping) || !errors.Is(err, releaseErr) || !released {
					t.Fatal("release uncertainty not retained", err)
				}
				for range 2 {
					if e := store.Close(); !errors.Is(e, releaseErr) {
						t.Fatal(e)
					}
				}
			case "cancelled":
				if store != nil || !errors.Is(err, context.Canceled) || released {
					t.Fatal("cancelled acquisition", err)
				}
			default:
				if store != nil || !errors.Is(err, ping) || !released {
					t.Fatal("failed startup with confirmed cleanup", err)
				}
			}
			if releaseAttempts > 1 {
				t.Fatal("repeated Close retried an unknown writer release")
			}
			output := fmt.Sprintf("%v %+v %#v", err, err, err)
			for _, secret := range []string{"startup-driver-secret", "close-driver-secret", "writer-release-secret"} {
				if strings.Contains(output, secret) {
					t.Fatal("startup exposed driver/release diagnostics")
				}
			}
		})
	}
}
