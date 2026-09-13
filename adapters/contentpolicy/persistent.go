package contentpolicy

import (
	"context"
	"lerna/artifacts"
	wire "lerna/gen/harness/v1"
	"time"
)

// RuleSet is a durable host-controlled policy revision. Empty Rules revoke all
// sources; an empty policy is never interpreted as missing configuration.
type RuleSet struct {
	Version uint64
	Rules   []Rule
}

// RuleStore initializes only an absent policy and atomically replaces complete
// rule sets with a strictly increasing version. Load must return current state.
type RuleStore interface {
	Initialize(context.Context, []Rule) error
	Load(context.Context) (RuleSet, error)
	Replace(context.Context, []Rule) error
}

type Persistent struct{ store RuleStore }

func NewPersistent(ctx context.Context, store RuleStore, defaults []Rule) (*Persistent, error) {
	if store == nil {
		return nil, artifacts.Error("INVALID_ARGUMENT")
	}
	if _, err := New(defaults); err != nil {
		return nil, err
	}
	bounded, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := store.Initialize(bounded, defaults); err != nil {
		return nil, err
	}
	return OpenPersistent(bounded, store)
}

// OpenPersistent loads an existing policy without seeding missing state.
// Recovery must not turn loss or corruption into a new grant of defaults.
func OpenPersistent(ctx context.Context, store RuleStore) (*Persistent, error) {
	if store == nil {
		return nil, artifacts.Error("INVALID_ARGUMENT")
	}
	bounded, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	p := &Persistent{store}
	if _, err := p.current(bounded); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Persistent) current(ctx context.Context) (*Policy, error) {
	state, err := p.store.Load(ctx)
	if err != nil {
		return nil, err
	}
	if state.Version == 0 {
		return nil, artifacts.Error("UNAVAILABLE")
	}
	current, err := New(state.Rules)
	if err != nil {
		return nil, err
	}
	current.revision = state.Version
	return current, nil
}

// Replace remains a privileged host port, never an SDK capability.
func (p *Persistent) Replace(rules []Rule) error {
	if _, err := New(rules); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return p.store.Replace(ctx, rules)
}
func (p *Persistent) Check(ctx context.Context, source *wire.ContentSource, action, purpose, location string, until int64) error {
	bounded, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	current, err := p.current(bounded)
	if err != nil {
		return err
	}
	return current.Check(bounded, source, action, purpose, location, until)
}
func (p *Persistent) Revision(ctx context.Context, at int64) (string, error) {
	bounded, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	current, err := p.current(bounded)
	if err != nil {
		return "", err
	}
	return current.Revision(bounded, at)
}
