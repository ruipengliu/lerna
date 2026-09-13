// Package fetchqueries binds acquisition observations to an original execution
// action. Control-ledger writes retain their existing transaction semantics.
package fetchqueries

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"lerna/adapters/fetchcontent"
	"lerna/adapters/taskcontent"
	"lerna/artifacts"
	"lerna/execution"
	"lerna/fetch"
	"lerna/tasks"
	"sync"
)

type Queries interface {
	ChargeExecutionQuery(context.Context, tasks.ActionBinding, string) error
}

type Guard interface {
	Check(context.Context, execution.Request) error
}

type Scope struct {
	guard      Guard
	mu         sync.Mutex
	completed  map[execution.Request]fetch.Outcome
	store      fetch.OutcomeStore
	content    fetchcontent.Content
	evidence   *fetchcontent.Adapter
	binding    artifacts.Binding
	capability execution.Capability
	queries    Queries
}

// New creates a private length table; independent observers must not prewarm
// execution reads. Bind shares lengths and successfully committed finite facts, never body or authority.
func New(store fetch.OutcomeStore, content fetchcontent.Content, binding artifacts.Binding, config fetchcontent.Config, capability execution.Capability, queries Queries, guard Guard) (*Scope, error) {
	if store == nil || guard == nil || queries == nil || capability.Name == "" || capability.Version == "" || capability.Implementation == "" || capability.ImplementationVersion == "" {
		return nil, fetch.Invalid
	}
	evidence, err := fetchcontent.New(content, binding, config)
	if err != nil {
		return nil, err
	}
	return &Scope{store: store, content: content, evidence: evidence, binding: binding, capability: capability, queries: queries, guard: guard, completed: make(map[execution.Request]fetch.Outcome)}, nil
}

func (s *Scope) Bind(r execution.Request) (fetch.OutcomeStore, fetch.Evidence, error) {
	c := s.capability
	if r.OperationID == "" || r.InputRef == "" || r.Qualification.Ref.Namespace != s.binding.Namespace || r.Capability != c.Name || r.Version != c.Version || r.Implementation != c.Implementation || r.ImplementationVersion != c.ImplementationVersion || r.DescriptorSHA256 != c.Digest() {
		return nil, nil, fetch.Denied
	}
	budget := actionBudget{s.queries, tasks.ActionBinding{Qualification: r.Qualification, OperationID: r.OperationID, Descriptor: r.DescriptorSHA256, InputRef: r.InputRef, ResourceVersion: r.ResourceVersion, ControlVersion: r.ControlVersion}}
	content, err := taskcontent.New(s.content, budget, r.Qualification)
	if err != nil {
		return nil, nil, err
	}
	evidence, err := s.evidence.WithContent(content)
	if err != nil {
		return nil, nil, err
	}
	return &outcomes{OutcomeStore: s.store, budget: budget, scope: s, request: r}, evidence, nil
}

type actionBudget struct {
	queries Queries
	action  tasks.ActionBinding
}

func (b actionBudget) ChargeQuery(ctx context.Context, q tasks.Qualification, key string) error {
	if q != b.action.Qualification {
		return fetch.Denied
	}
	return b.queries.ChargeExecutionQuery(ctx, b.action, key)
}

type outcomes struct {
	scope   *Scope
	request execution.Request
	fetch.OutcomeStore
	budget actionBudget
}

func (s *outcomes) Outcome(ctx context.Context, namespace, operation string) (fetch.Outcome, bool, error) {
	action := s.budget.action
	if namespace != action.Qualification.Ref.Namespace || operation != action.OperationID {
		return fetch.Outcome{}, false, fetch.Denied
	}
	s.scope.mu.Lock()
	cached, known := s.scope.completed[s.request]
	s.scope.mu.Unlock()
	if known {
		// A successful local Complete supplies immutable facts, not authority.
		// Every reuse still checks the original current execution qualification.
		if err := s.scope.guard.Check(ctx, s.request); err != nil {
			return s.controlFact(ctx, namespace, operation, err)
		}
		return cached, true, nil
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return fetch.Outcome{}, false, err
	}
	if err := s.budget.ChargeQuery(ctx, action.Qualification, "outcome/"+hex.EncodeToString(id[:])); err != nil {
		return s.controlFact(ctx, namespace, operation, err)
	}
	return s.OutcomeStore.Outcome(ctx, namespace, operation)
}

func (s *outcomes) Complete(ctx context.Context, in fetch.AttemptIntent, out fetch.Outcome) error {
	if in.Task != s.request.Qualification.Ref || in.OperationID != s.request.OperationID || in.Fingerprint != s.request.Fingerprint() {
		return fetch.IdentityConflict
	}
	if err := s.OutcomeStore.Complete(ctx, in, out); err != nil {
		return err
	}
	// Failed or interrupted commits never populate the cache. The store owns
	// terminal immutability; do not cache unknown observations or response bytes.
	s.scope.mu.Lock()
	defer s.scope.mu.Unlock()
	if len(s.scope.completed) < 12 {
		s.scope.completed[s.request] = out
	}
	return nil
}

// This fallback retrieves only finite execution facts during PAUSE/CANCEL. Current
// task authority and the original query allowance are checked before store I/O.
func (s *outcomes) controlFact(ctx context.Context, namespace, operation string, original error) (fetch.Outcome, bool, error) {
	facts, ok := s.scope.queries.(interface {
		ChargeExecutionFactQuery(context.Context, tasks.ActionBinding, string) error
	})
	if !ok {
		return fetch.Outcome{}, false, original
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return fetch.Outcome{}, false, err
	}
	if err := facts.ChargeExecutionFactQuery(ctx, s.budget.action, "outcome-fact/"+hex.EncodeToString(id[:])); err != nil {
		return fetch.Outcome{}, false, err
	}
	out, known, err := s.OutcomeStore.Outcome(ctx, namespace, operation)
	if err != nil {
		return fetch.Outcome{}, false, err
	}
	if !known {
		return fetch.Outcome{}, false, nil
	}
	if out.Status == "acquired" && out.Reference != "" {
		return fetch.Outcome{}, false, &fetch.ControlledAcquisition{Mode: out.Mode, Requests: out.Requests}
	}
	if !fetch.IsFailureStatus(out.Status) || out.Reference != "" {
		return fetch.Outcome{}, false, fetch.Denied
	}
	return out, true, nil
}
