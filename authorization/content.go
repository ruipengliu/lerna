package authorization

import (
	"context"
	wire "lerna/gen/harness/v1"
	"time"
)

// ContentTransaction is a trusted in-process seam, never an RPC. Callbacks may
// be retried after CAS conflicts: no external effects or nested store calls.
// Data and operation claims commit together with the authorization time watermark.
type ContentTransaction interface {
	Identity(string) (Identity, error)
	Data() []byte
	SetData([]byte)
	Namespace() string
	Now() time.Time
	Authorize(token string, action *wire.AuthorizationAction) (Identity, error)
	Operation(id, subject string, claim bool) error
	RegisterUse(token string, spec UseSpec) error
	UseStatus(id, consumer, config string) (string, error)
	CompleteUse(id, consumer, config string) error
}

type contentTransaction struct {
	identity    func(string) (Identity, error)
	data        []byte
	namespace   string
	now         time.Time
	authorize   func(string, *wire.AuthorizationAction) (Identity, error)
	operation   func(string, string, bool) error
	registerUse func(string, UseSpec) error
	useStatus   func(string, string, string) (string, error)
	completeUse func(string, string, string) error
}

func (t *contentTransaction) Identity(token string) (Identity, error) { return t.identity(token) }
func (t *contentTransaction) Authorize(token string, action *wire.AuthorizationAction) (Identity, error) {
	return t.authorize(token, action)
}

// Operation validates canonical identity and cross-domain uniqueness. A new
// claim also requires an open window; a retained content operation remains queryable.
func (t *contentTransaction) Operation(id, subject string, claim bool) error {
	return t.operation(id, subject, claim)
}
func (s *Service) UpdateContent(ctx context.Context, fn func(ContentTransaction) error) error {
	if fn == nil {
		return fail(Invalid)
	}
	return s.update(ctx, func(st *State, now time.Time) error {
		tx := &contentTransaction{data: st.ContentData, namespace: st.Namespace, now: now}
		tx.identity = func(token string) (Identity, error) {
			p, err := authenticate(st, token, now)
			if err != nil {
				return Identity{}, err
			}
			return Identity{Subject: p.Subject, Namespace: st.Namespace}, nil
		}
		tx.authorize = func(token string, action *wire.AuthorizationAction) (Identity, error) {
			p, err := authenticate(st, token, now)
			if err != nil {
				return Identity{}, err
			}
			decision, err := s.evaluate(st, p, action, now)
			if err != nil {
				return Identity{}, err
			}
			if !decision.Allowed {
				return Identity{}, fail(Denied)
			}
			return Identity{Subject: p.Subject, Namespace: st.Namespace}, nil
		}
		tx.operation = func(id, subject string, claim bool) error {
			return s.partitionOperation(st, now, &st.ContentOperations, st.RuntimeOperations, id, subject, claim)
		}
		tx.registerUse = func(token string, spec UseSpec) error {
			fn, err := s.useRegistration(token, spec)
			if err != nil {
				return err
			}
			return fn(st, now)
		}
		lookupUse := func(id, consumer, config string) (UseRecord, error) {
			entry, ok := st.Uses[id]
			if !ok {
				return UseRecord{}, fail(NotFound)
			}
			if entry.Spec.Consumer != consumer || entry.Spec.ConfigSHA256 != config {
				return UseRecord{}, fail(IdentityConflict)
			}
			return entry, nil
		}
		tx.useStatus = func(id, consumer, config string) (string, error) {
			entry, err := lookupUse(id, consumer, config)
			if err != nil {
				return "", err
			}
			if entry.Notice != nil {
				return "invalidated", nil
			}
			return "active", nil
		}
		// The caller commits body cleanup in this same ContentTransaction.
		tx.completeUse = func(id, consumer, config string) error {
			entry, err := lookupUse(id, consumer, config)
			if err != nil {
				return err
			}
			if entry.Notice == nil {
				entry.Notice = &UseNotice{ID: id, Revision: st.Revision, At: now.Unix(), Target: entry.Spec.Target}
			}
			entry.Cleaned = true
			st.Uses[id] = entry
			return nil
		}
		if err := fn(tx); err != nil {
			return err
		}
		st.ContentData = tx.data
		return nil
	})
}

func (t *contentTransaction) Data() []byte        { return t.data }
func (t *contentTransaction) SetData(data []byte) { t.data = data }
func (t *contentTransaction) Namespace() string   { return t.namespace }
func (t *contentTransaction) Now() time.Time      { return t.now }

func (t *contentTransaction) RegisterUse(token string, spec UseSpec) error {
	return t.registerUse(token, spec)
}
func (t *contentTransaction) UseStatus(id, consumer, config string) (string, error) {
	return t.useStatus(id, consumer, config)
}
func (t *contentTransaction) CompleteUse(id, consumer, config string) error {
	return t.completeUse(id, consumer, config)
}
