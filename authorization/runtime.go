package authorization

import (
	"context"
	wire "lerna/gen/harness/v1"
	"time"
)

// RuntimeTransaction is a trusted in-process seam, never an RPC. Callbacks may
// be retried after CAS conflicts: no external effects or nested store calls.
// Data and operation claims commit together with the authorization time watermark.
type RuntimeTransaction interface {
	Data() []byte
	SetData([]byte)
	Namespace() string
	Now() time.Time
	Authorize(token, resource, action string) (Identity, error)
	Operation(id, subject string, claim bool) error
}

type runtimeTransaction struct {
	state     *State
	data      []byte
	namespace string
	now       time.Time
	authorize func(string, string, string) (Identity, error)
	operation func(string, string, bool) error
}

func (t *runtimeTransaction) Authorize(token, resource, action string) (Identity, error) {
	return t.authorize(token, resource, action)
}

// Operation validates canonical identity and cross-domain uniqueness. A new
// claim also requires an open window; a retained runtime operation remains queryable.
func (t *runtimeTransaction) Operation(id, subject string, claim bool) error {
	return t.operation(id, subject, claim)
}
func (s *Service) UpdateRuntime(ctx context.Context, fn func(RuntimeTransaction) error) error {
	if fn == nil {
		return fail(Invalid)
	}
	return s.update(ctx, func(st *State, now time.Time) error {
		tx := s.runtime(st, now)
		if err := fn(tx); err != nil {
			return err
		}
		st.RuntimeData = tx.data
		return nil
	})
}

func (t *runtimeTransaction) Data() []byte        { return t.data }
func (t *runtimeTransaction) SetData(data []byte) { t.data = data }
func (t *runtimeTransaction) Namespace() string   { return t.namespace }
func (t *runtimeTransaction) Now() time.Time      { return t.now }

func (s *Service) runtime(st *State, now time.Time) *runtimeTransaction {
	tx := &runtimeTransaction{state: st, data: st.RuntimeData, namespace: st.Namespace, now: now}
	tx.authorize = func(token, resource, action string) (Identity, error) {
		p, err := authenticate(st, token, now)
		if err != nil {
			return Identity{}, err
		}
		decision, err := s.evaluate(st, p, &wire.AuthorizationAction{Resource: resource, Action: action, Purpose: "task", Location: "local"}, now)
		if err != nil {
			return Identity{}, err
		}
		if !decision.Allowed {
			return Identity{}, fail(Denied)
		}
		return Identity{Subject: p.Subject, Namespace: st.Namespace}, nil
	}
	tx.operation = func(id, subject string, claim bool) error {
		return s.partitionOperation(st, now, &st.RuntimeOperations, st.ContentOperations, id, subject, claim)
	}
	return tx
}
