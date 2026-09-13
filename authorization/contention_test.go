package authorization_test

import (
	"context"
	"errors"
	"lerna/adapters/sqliteauth"
	"lerna/authorization"
	"path/filepath"
	"testing"
)

type conflictingStore struct {
	authorization.Store
	attempts int
	cancel   context.CancelFunc
}

func (s *conflictingStore) Commit(context.Context, uint64, authorization.State) error {
	s.attempts++
	if s.cancel != nil {
		s.cancel()
	}
	return &authorization.Error{Code: authorization.Conflict}
}
func TestCASContentionIsBoundedAndCancellable(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "bounded", true: "cancelled"}[cancelled], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			db, e := sqliteauth.Open(filepath.Join(t.TempDir(), "authority.db"))
			if e != nil {
				t.Fatal(e)
			}
			defer db.Close()
			store := &conflictingStore{Store: db}
			if cancelled {
				store.cancel = cancel
			}
			svc, e := authorization.New(store, authorization.SystemClock{}, config())
			if e != nil {
				t.Fatal(e)
			}
			_, e = svc.Bootstrap(ctx, "local", "operator")
			if cancelled {
				if !errors.Is(e, context.Canceled) || store.attempts != 1 {
					t.Fatal(store.attempts, e)
				}
			} else if !authorization.Is(e, authorization.Unavailable) || store.attempts != 8 {
				t.Fatal(store.attempts, e)
			}
			snapshot, e := db.Load(context.Background())
			if e != nil || snapshot.State.Format != 0 {
				t.Fatal("failed admission partially persisted", e)
			}
		})
	}
}
