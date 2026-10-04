package decisionfixture

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/postgres"
	v "github.com/ruipengliu/lerna/contract/v1_1"
	"github.com/ruipengliu/lerna/internal/sqlclosetest"
)

func TestSourceCloseKeepsFirstDriverFailure(t *testing.T) {
	fault := errors.New("mechanical close uncertainty")
	db := sqlclosetest.Open(sqlclosetest.Fault{CloseError: fault})
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	source := &Store{db: db}
	for range 2 {
		if err := source.Close(); !errors.Is(err, fault) {
			t.Fatal("source second close erased original uncertainty", err)
		}
	}
}
func TestSourceConcurrentCloseWaitsForFirstDriverResult(t *testing.T) {
	fault := errors.New("mechanical close uncertainty")
	started := make(chan struct{})
	release := make(chan struct{})
	db := sqlclosetest.Open(sqlclosetest.Fault{CloseError: fault, CloseStarted: started, CloseRelease: release})
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	source := &Store{db: db}
	first := make(chan error, 1)
	second := make(chan error, 1)
	secondStarted := make(chan struct{})
	go func() { first <- source.Close() }()
	select {
	case <-started:
	case <-time.After(time.Second):
		close(release)
		_ = receiveClose(t, first)
		t.Fatal("first driver close did not start")
	}
	go func() { close(secondStarted); second <- source.Close() }()
	select {
	case <-secondStarted:
	case <-time.After(time.Second):
		close(release)
		_ = receiveClose(t, first)
		_ = receiveClose(t, second)
		t.Fatal("second Close did not start")
	}
	var early bool
	var result error
	select {
	case result = <-second:
		early = true
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	firstResult := receiveClose(t, first)
	if !early {
		result = receiveClose(t, second)
	}
	if early || !errors.Is(firstResult, fault) || !errors.Is(result, fault) {
		t.Fatal("concurrent source close returned before first result", result)
	}
}

func receiveClose(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(3 * time.Second):
		return errors.New("mechanical Close result/join deadline")
	}
}

func TestSourceOpenPreservesStartupAndCloseOutcomes(t *testing.T) {
	ping := errors.New("startup-driver-secret")
	closed := errors.New("close-driver-secret")
	for _, name := range []string{"success", "ping_failure", "dual_failure", "cancelled", "open_failure"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			var pingErr, closeErr error
			if name == "ping_failure" || name == "dual_failure" {
				pingErr = ping
			}
			if name == "dual_failure" {
				closeErr = closed
			}
			if name == "cancelled" {
				cancel()
			}
			db := sqlclosetest.Open(sqlclosetest.Fault{PingError: pingErr, CloseError: closeErr})
			cfg := postgres.Config{DSN: "mechanical-config-secret", Schema: "mechanical", TransactionTimeout: time.Second, StatementTimeout: 100 * time.Millisecond, LockTimeout: time.Millisecond}
			source, err := open(ctx, cfg, v.OwnerRef{TenantID: "mechanical", OwnerID: "source"}, func(string, string) (*sql.DB, error) {
				if name == "open_failure" {
					return nil, ping
				}
				return db, nil
			})
			if name == "open_failure" {
				if e := db.Close(); e != nil {
					t.Fatal(e)
				}
			}
			switch name {
			case "success":
				if err != nil || source == nil {
					t.Fatal("startup", err)
				}
				for range 2 {
					if e := source.Close(); e != nil {
						t.Fatal(e)
					}
				}
			case "dual_failure":
				if source == nil || !errors.Is(err, ping) || !errors.Is(err, closed) {
					t.Fatal("partial cleanup handle or original causes lost", err)
				}
				for range 2 {
					if e := source.Close(); !errors.Is(e, closed) {
						t.Fatal(e)
					}
				}
			case "cancelled":
				if source != nil || !errors.Is(err, context.Canceled) {
					t.Fatal("cancelled startup", err)
				}
			default:
				if source != nil || !errors.Is(err, ping) {
					t.Fatal("failed startup with confirmed cleanup", err)
				}
			}
			output := fmt.Sprintf("%v %+v %#v", err, err, err)
			for _, secret := range []string{"startup-driver-secret", "close-driver-secret", "mechanical-config-secret"} {
				if strings.Contains(output, secret) {
					t.Fatal("startup exposed private driver diagnostics")
				}
			}
		})
	}
}
