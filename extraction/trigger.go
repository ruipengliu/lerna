package extraction

import (
	"context"
	"lerna/memory"
	"time"
)

type SourceScope struct{ Kind, Key string }

// TriggerSpec declares finite source-change work. It is supplied through a
// trusted host registration path, never parsed from source text/model output.
type TriggerSpec struct {
	ID, Condition       string
	Sources             []SourceScope
	MaxRounds, MaxSteps uint64
	ExpiresUnix         int64
}
type TriggerRecord struct {
	Namespace, Subject, Location, Purpose, State string
	Spec                                         TriggerSpec
}
type TriggerStore interface {
	RegisterTrigger(context.Context, TriggerRecord) error
	GetTrigger(context.Context, string, string, string) (TriggerRecord, error)
	// CancelTrigger is trusted host control. Cancellation is permanent for the
	// original registration; a public adapter must authenticate its owner.
	CancelTrigger(context.Context, string, string, string) error
}
type TriggerRegistry struct {
	store   TriggerStore
	auth    SaveAuthority
	clock   memory.Clock
	binding memory.Binding
	purpose string
}

func ValidTriggerSpec(s TriggerSpec) bool {
	name := func(s string, max int) bool { return len(s) > 0 && len(s) <= max }
	if !name(s.ID, 256) || s.Condition != "source.changed" || len(s.Sources) == 0 || len(s.Sources) > 16 || s.MaxRounds == 0 || s.MaxRounds > 64 || s.MaxSteps == 0 || s.MaxSteps > 32 || s.ExpiresUnix <= 0 {
		return false
	}
	seen := map[SourceScope]bool{}
	for _, source := range s.Sources {
		if !name(source.Kind, 128) || !name(source.Key, 256) || seen[source] {
			return false
		}
		seen[source] = true
	}
	return true
}
func NewTriggerRegistry(store TriggerStore, auth SaveAuthority, clock memory.Clock, b memory.Binding, purpose string) (*TriggerRegistry, error) {
	if store == nil || auth == nil || clock == nil || b.Token == "" || b.Namespace == "" || b.Subject == "" || b.Location == "" || b.Recipient != b.Location || purpose == "" {
		return nil, memory.Invalid
	}
	return &TriggerRegistry{store, auth, clock, b, purpose}, nil
}

// Register authorizes explicit continuous reading/extraction metadata only.
// It starts no task or listener. Dispatch must recheck current scan authority,
// sources, expiry, cancellation and persisted remaining budget each time.
func (r *TriggerRegistry) Register(ctx context.Context, spec TriggerSpec) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if !ValidTriggerSpec(spec) {
		return memory.Invalid
	}
	spec.Sources = append([]SourceScope(nil), spec.Sources...)
	now, err := r.clock.Now()
	if err != nil {
		return memory.Unavailable
	}
	if spec.ExpiresUnix <= now.Unix() || spec.ExpiresUnix-now.Unix() > 86400 {
		return memory.Denied
	}
	if err = r.auth.Check(ctx, r.binding, "scan"); err != nil {
		return err
	}
	return r.store.RegisterTrigger(ctx, TriggerRecord{Namespace: r.binding.Namespace, Subject: r.binding.Subject, Location: r.binding.Location, Purpose: r.purpose, State: "active", Spec: spec})
}

// Cancel checks independent current control authority and the original owner.
// It deliberately does not require live scan/read permission or an unexpired
// registration: stopping work must remain possible after scanning is revoked.
// Cancellation is permanent/idempotent for this registration identity; it does
// not retract existing candidates, saves, or already-reported task effects.
func (r *TriggerRegistry) Cancel(ctx context.Context, id string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if id == "" || len(id) > 256 {
		return memory.Invalid
	}
	if err := r.auth.Check(ctx, r.binding, "cancel-scan"); err != nil {
		return err
	}
	registration, err := r.store.GetTrigger(ctx, r.binding.Namespace, r.binding.Subject, id)
	if err != nil {
		return err
	}
	if registration.Namespace != r.binding.Namespace || registration.Subject != r.binding.Subject || registration.Spec.ID != id || registration.Location != r.binding.Location || registration.Purpose != r.purpose {
		return memory.Denied
	}
	if err = r.auth.Check(ctx, r.binding, "cancel-scan"); err != nil {
		return err
	}
	return r.store.CancelTrigger(ctx, r.binding.Namespace, r.binding.Subject, id)
}
