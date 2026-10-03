package runtime

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ruipengliu/lerna/api"
)

// A handler sees only locally confirmed renewals of its original ownership.
// Unknown renewals never extend the authority to make calls or commit facts.
// This wrapper does not allow a Tx or a Claim to escape its original scope.
type ownedStore struct {
	Store
	scope     Scope
	mu        sync.RWMutex
	confirmed api.Claim
}

func (s *ownedStore) claim(c api.Claim) api.Claim {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if c.JobID == s.confirmed.JobID && c.HolderID == s.confirmed.HolderID && c.LeaseEpoch == s.confirmed.LeaseEpoch && c.ObservedWorkRevision == s.confirmed.ObservedWorkRevision {
		c.LeaseUntil = s.confirmed.LeaseUntil
	}
	return c
}
func (s *ownedStore) current() api.Claim { s.mu.RLock(); defer s.mu.RUnlock(); return s.confirmed }
func (s *ownedStore) Within(ctx context.Context, scope Scope, parts []string, fn func(Tx) error) (CommitStatus, error) {
	if scope != s.scope {
		return RolledBack, api.E("forbidden", "worker_scope_changed")
	}
	return s.Store.Within(ctx, scope, parts, func(tx Tx) error { return fn(ownedTx{Tx: tx, owner: s}) })
}
func (s *ownedStore) CheckClaim(ctx context.Context, scope Scope, c api.Claim) error {
	if scope != s.scope {
		return api.E("forbidden", "worker_scope_changed")
	}
	return s.Store.CheckClaim(ctx, scope, s.claim(c))
}

type ownedTx struct {
	Tx
	owner *ownedStore
}

func (t ownedTx) Guard(ctx context.Context, c api.Claim) error {
	return t.Tx.Guard(ctx, t.owner.claim(c))
}
func (t ownedTx) Finish(ctx context.Context, c api.Claim, d Disposition) error {
	return t.Tx.Finish(ctx, t.owner.claim(c), d)
}
func (t ownedTx) Savepoint(ctx context.Context, fn func(Tx) error) error {
	return t.Tx.Savepoint(ctx, func(tx Tx) error { return fn(ownedTx{Tx: tx, owner: t.owner}) })
}

func (w *Worker) runOwned(parent context.Context, scope Scope, work Work, handler JobHandler) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	owner := &ownedStore{Store: w.Store, scope: scope, confirmed: work.Claim}
	stop := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(w.Lease / 3)
		defer ticker.Stop()
		for {
			known := owner.current()
			until, err := api.ParseTime(known.LeaseUntil)
			if err != nil || !time.Now().Before(until) {
				cancel()
				return
			}
			expiry := time.NewTimer(time.Until(until))
			select {
			case <-ctx.Done():
				expiry.Stop()
				return
			case <-stop:
				expiry.Stop()
				return
			case <-expiry.C:
				cancel()
				return
			case <-ticker.C:
				expiry.Stop()
			}
			renewCtx, stopRenew := context.WithDeadline(ctx, until)
			next, status, err := w.Store.Renew(renewCtx, scope, known, w.Lease)
			stopRenew()
			if errors.Is(err, ErrClaimLost) {
				cancel()
				return
			}
			if status == Committed && err == nil {
				owner.mu.Lock()
				owner.confirmed = next
				owner.mu.Unlock()
			}
			// RolledBack/CommitUnknown retain the previous locally confirmed deadline.
		}
	}()
	err := handler(ctx, owner, scope, work)
	close(stop)
	<-stopped
	return err
}
