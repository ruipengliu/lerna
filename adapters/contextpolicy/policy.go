// Package contextpolicy connects original authorization uses to decision-local
// snapshot retirement. It has no peer-facing authorization or disclosure API.
package contextpolicy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"lerna/authorization"
	"lerna/contextassembly"
)

type Authority interface {
	RegisterUse(context.Context, string, authorization.UseSpec) error
	InspectUse(context.Context, string, string, string, string) (authorization.UseInspection, error)
	PendingUses(context.Context, string, string, string, int) ([]authorization.UseNotice, error)
	ConfirmUseCleaned(context.Context, string, string, string, authorization.UseNotice) error
}

type Config struct {
	Namespace, Consumer, ConfigSHA256 string
	Batch                             int
	Timeout                           time.Duration
}

type Adapter struct {
	authority Authority
	store     contextassembly.RetirementStore
	config    Config
}

func New(a Authority, s contextassembly.RetirementStore, c Config) (*Adapter, error) {
	hash, err := hex.DecodeString(c.ConfigSHA256)
	if a == nil || s == nil || c.Namespace == "" || len(c.Namespace) > 128 || c.Consumer == "" || len(c.Consumer) > 128 || err != nil || len(hash) != 32 || hex.EncodeToString(hash) != c.ConfigSHA256 || c.Batch < 1 || c.Batch > 32 || c.Timeout <= 0 || c.Timeout > 10*time.Second {
		return nil, contextassembly.Invalid
	}
	return &Adapter{a, s, c}, nil
}

func identity(key contextassembly.Key) (string, string) {
	raw, _ := json.Marshal(key)
	hash := sha256.Sum256(raw)
	return "context." + hex.EncodeToString(hash[:]), string(raw)
}
func (a *Adapter) validKey(key contextassembly.Key) bool {
	return key.Namespace == a.config.Namespace && key.TaskID != "" && len(key.TaskID) <= 256 && key.Decision > 0 && key.Decision <= 1<<32
}
func mapped(err error) error {
	if err == nil {
		return nil
	}
	if authorization.Is(err, authorization.ResultOnly) {
		return contextassembly.Invalidated
	}
	if authorization.Is(err, authorization.Denied) || authorization.Is(err, authorization.Unauthenticated) {
		return contextassembly.Denied
	}
	if authorization.Is(err, authorization.IdentityConflict) {
		return contextassembly.IdentityConflict
	}
	return contextassembly.Unavailable
}

// Prepare binds host routing and the exact authorization requirements before
// snapshot Bind. A failed/unknown registration is not permission to retain.
func (a *Adapter) Prepare(ctx context.Context, token string, key contextassembly.Key, spec authorization.UseSpec) error {
	if !a.validKey(key) {
		return contextassembly.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, a.config.Timeout)
	defer cancel()
	spec.ID, spec.Target = identity(key)
	spec.Namespace = a.config.Namespace
	spec.Consumer = a.config.Consumer
	spec.ConfigSHA256 = a.config.ConfigSHA256
	return mapped(a.authority.RegisterUse(ctx, token, spec))
}

// Validate checks the original use, including irreversible invalidation after
// policy restoration. The caller still checks current source/facts permissions.
func (a *Adapter) Validate(ctx context.Context, key contextassembly.Key) error {
	if !a.validKey(key) {
		return contextassembly.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, a.config.Timeout)
	defer cancel()
	id, target := identity(key)
	state, err := a.authority.InspectUse(ctx, a.config.Namespace, a.config.Consumer, a.config.ConfigSHA256, id)
	if err != nil {
		return mapped(err)
	}
	if state.Target != target {
		return contextassembly.IdentityConflict
	}
	if !state.Active {
		return contextassembly.Invalidated
	}
	return nil
}

// Clean processes one bounded notice batch. Retirement must commit before the
// matching notice is confirmed; unknown outcomes replay the original target.
func (a *Adapter) Clean(ctx context.Context) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, a.config.Timeout)
	defer cancel()
	notices, err := a.authority.PendingUses(ctx, a.config.Namespace, a.config.Consumer, a.config.ConfigSHA256, a.config.Batch)
	if err != nil {
		return 0, mapped(err)
	}
	if len(notices) > a.config.Batch {
		return 0, contextassembly.Invalid
	}
	keys := make([]contextassembly.Key, len(notices))
	for i, n := range notices {
		if len(n.Target) > 4096 || json.Unmarshal([]byte(n.Target), &keys[i]) != nil || !a.validKey(keys[i]) || n.At <= 0 {
			return 0, contextassembly.Invalid
		}
		id, target := identity(keys[i])
		if id != n.ID || target != n.Target {
			return 0, contextassembly.Invalid
		}
	}
	for i, n := range notices {
		if err := a.store.Retire(ctx, keys[i]); err != nil {
			return i, err
		}
		if err := a.authority.ConfirmUseCleaned(ctx, a.config.Namespace, a.config.Consumer, a.config.ConfigSHA256, n); err != nil {
			return i, mapped(err)
		}
	}
	return len(notices), nil
}
