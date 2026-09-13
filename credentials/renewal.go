package credentials

import (
	"bytes"
	"context"
	"encoding/json"
)

// RenewalProvider is a trusted, fixed exit. Inspect must reconcile the original
// provider operation, not infer success from a transport acknowledgement.
// ReplaySafe means identical operation IDs cannot create another effect.
type RenewalProvider interface {
	ID() string
	Target() Binding
	ReplaySafe() bool
	Renew(context.Context, string, []byte) error
	Inspect(context.Context, string, []byte) (RenewalObservation, error)
}

// RenewalObservation is confined to trusted composition; it is never returned
// by the lifecycle API or persisted in plaintext.
type RenewalObservation struct {
	State       string
	Secret      []byte
	ExpiresUnix int64
}
type Renewal struct {
	OperationID, Ref, ProviderID, Phase string
	Binding                             Binding
	ReplaySafe                          bool
	Attempts, Checks                    uint32
}
type RenewalEntry struct {
	Renewal  Renewal
	Original *Record
}

func validRenewalState(s LifecycleState) bool {
	if len(s.Renewals) > 32 {
		return false
	}
	seen, held := map[string]bool{}, map[string]bool{}
	for _, entry := range s.Renewals {
		r := entry.Renewal
		if !validLifecycleOperation(r.OperationID) || !validRef(r.Ref) || !validLifecycleOperation(r.ProviderID) || !r.Binding.Valid() || seen[r.OperationID] || r.Attempts > 3 || r.Checks > 8 {
			return false
		}
		seen[r.OperationID] = true
		switch r.Phase {
		case "pending":
			if r.Attempts != 0 || r.Checks != 0 {
				return false
			}
		case "checking", "needs_reconciliation":
			if r.Attempts == 0 {
				return false
			}
		case "completed", "failed":
			if entry.Original != nil {
				return false
			}
		default:
			return false
		}
		if r.Phase != "completed" && r.Phase != "failed" {
			if held[r.Ref] || entry.Original == nil || !ValidRecord(*entry.Original) || entry.Original.Ref != r.Ref || entry.Original.Binding != r.Binding {
				return false
			}
			held[r.Ref] = true
			for _, retired := range s.Retirements {
				if retired.KeyVersion == entry.Original.KeyVersion {
					return false
				}
			}
		}
	}
	return true
}
func (l *Lifecycle) WithProvider(p RenewalProvider) (*Lifecycle, error) {
	if p == nil || !validLifecycleOperation(p.ID()) || !p.Target().Valid() {
		return nil, Invalid
	}
	copy := *l
	copy.provider = p
	return &copy, nil
}
func renewalIndex(s LifecycleState, op string) int {
	for i, r := range s.Renewals {
		if r.Renewal.OperationID == op {
			return i
		}
	}
	return -1
}
func (l *Lifecycle) renewalAuthority(ctx context.Context, token string, target Binding) error {
	if l.provider == nil || l.provider.Target() != target {
		return Denied
	}
	if e := l.broker.authorize(ctx, token, target, "manage"); e != nil {
		return e
	}
	return l.broker.authorize(ctx, token, target, "use")
}
func (l *Lifecycle) StartRenewal(ctx context.Context, token, operation, ref string, target Binding) (Renewal, error) {
	ctx, cancel := context.WithTimeout(ctx, l.broker.config.Timeout)
	defer cancel()
	if !validLifecycleOperation(operation) || !validRef(ref) {
		return Renewal{}, Invalid
	}
	if e := l.renewalAuthority(ctx, token, target); e != nil {
		return Renewal{}, e
	}
	s, e := l.store.LoadLifecycle(ctx)
	if e != nil {
		return Renewal{}, storeError(e)
	}
	if i := renewalIndex(s, operation); i >= 0 {
		r := s.Renewals[i].Renewal
		if r.Ref != ref || r.Binding != target || r.ProviderID != l.provider.ID() || r.ReplaySafe != l.provider.ReplaySafe() {
			return Renewal{}, Denied
		}
		return r, nil
	}
	if len(s.Renewals) >= 32 {
		return Renewal{}, Exhausted
	}
	for _, r := range s.Renewals {
		if r.Renewal.Ref == ref && r.Original != nil {
			return Renewal{}, Conflict
		}
	}
	original, e := l.store.Get(ctx, ref)
	if e != nil {
		return Renewal{}, storeError(e)
	}
	if original.Binding != target {
		return Renewal{}, Denied
	}
	secret, e := l.broker.open(ctx, original)
	clear(secret)
	if e != nil {
		return Renewal{}, e
	}
	r := Renewal{OperationID: operation, Ref: ref, Binding: target, ProviderID: l.provider.ID(), ReplaySafe: l.provider.ReplaySafe(), Phase: "pending"}
	s.Renewals = append(s.Renewals, RenewalEntry{Renewal: r, Original: &original})
	if e = l.save(ctx, token, target, &s, nil, 0); e != nil {
		return Renewal{}, storeError(e)
	}
	return r, nil
}
func renewalOperation(r Renewal) string {
	raw, _ := json.Marshal(struct {
		Kind, Op, Ref, Provider string
		Binding                 Binding
	}{"renewal", r.OperationID, r.Ref, r.ProviderID, r.Binding})
	return digest(raw)
}

// StepRenewal performs at most one lookup and one permitted send. Every send is
// preceded by a durable attempt; unknown outcomes never create another identity.
func (l *Lifecycle) StepRenewal(ctx context.Context, token, operation string, target Binding) (out Renewal, err error) {
	defer func() {
		if recover() != nil {
			out = Renewal{}
			err = TargetUnavailable
		}
	}()

	ctx, cancel := context.WithTimeout(ctx, l.broker.config.Timeout)
	defer cancel()
	if !validLifecycleOperation(operation) {
		return Renewal{}, Invalid
	}
	if e := l.renewalAuthority(ctx, token, target); e != nil {
		return Renewal{}, e
	}
	s, e := l.store.LoadLifecycle(ctx)
	if e != nil {
		return Renewal{}, storeError(e)
	}
	i := renewalIndex(s, operation)
	if i < 0 {
		return Renewal{}, Missing
	}
	entry := s.Renewals[i]
	r := entry.Renewal
	if r.Binding != target || r.ProviderID != l.provider.ID() || r.ReplaySafe != l.provider.ReplaySafe() {
		return Renewal{}, Denied
	}
	if r.Phase == "completed" || r.Phase == "failed" || r.Phase == "needs_reconciliation" {
		return r, nil
	}
	original, e := l.broker.open(ctx, *entry.Original)
	if e != nil {
		return Renewal{}, e
	}
	defer clear(original)
	if r.Phase == "checking" {
		if r.Checks >= 8 {
			return l.holdRenewal(ctx, token, &s, i)
		}
		r.Checks++
		s.Renewals[i].Renewal = r
		if e = l.save(ctx, token, target, &s, nil, 0); e != nil {
			return Renewal{}, storeError(e)
		}
		if e = l.renewalAuthority(ctx, token, target); e != nil {
			return Renewal{}, e
		}
		observed, err := l.provider.Inspect(ctx, renewalOperation(r), original)
		defer clear(observed.Secret)
		if err != nil || observed.State == "unknown" {
			if r.Checks == 8 {
				return l.holdRenewal(ctx, token, &s, i)
			}
			return r, nil
		}
		switch observed.State {
		case "applied":
			return l.completeRenewal(ctx, token, &s, i, original, observed)
		case "not_occurred":
			if len(observed.Secret) != 0 {
				return Renewal{}, Invalid
			}
			if !r.ReplaySafe {
				return l.holdRenewal(ctx, token, &s, i)
			}
			if r.Attempts >= 3 {
				// A lookup that did not find the operation is not a terminal provider
				// fence. A previously reserved send can still arrive; retain its identity.
				return l.holdRenewal(ctx, token, &s, i)
			}
		default:
			return Renewal{}, Invalid
		}
	}
	r.Attempts++
	r.Phase = "checking"
	s.Renewals[i].Renewal = r
	if e = l.save(ctx, token, target, &s, nil, 0); e != nil {
		return Renewal{}, storeError(e)
	}
	if e = l.renewalAuthority(ctx, token, target); e != nil {
		return Renewal{}, e
	}
	// The transport return is deliberately not effect confirmation. A later
	// bounded step inspects this same operation even if Renew returned success.
	_ = l.provider.Renew(ctx, renewalOperation(r), original)
	return r, nil
}
func (l *Lifecycle) holdRenewal(ctx context.Context, token string, s *LifecycleState, i int) (Renewal, error) {
	s.Renewals[i].Renewal.Phase = "needs_reconciliation"
	r := s.Renewals[i].Renewal
	if e := l.save(ctx, token, r.Binding, s, nil, 0); e != nil {
		return Renewal{}, storeError(e)
	}
	return r, nil
}
func (l *Lifecycle) completeRenewal(ctx context.Context, token string, s *LifecycleState, i int, original []byte, observed RenewalObservation) (Renewal, error) {
	r := s.Renewals[i].Renewal
	now, e := l.broker.now()
	if e != nil {
		return Renewal{}, e
	}
	if len(observed.Secret) == 0 || len(observed.Secret) > 4096 || observed.ExpiresUnix <= now || observed.ExpiresUnix-now > 31536000 {
		return Renewal{}, Invalid
	}
	current, e := l.store.Get(ctx, r.Ref)
	if e != nil {
		return Renewal{}, storeError(e)
	}
	if current.Binding != r.Binding {
		return Renewal{}, Denied
	}
	currentSecret, e := l.broker.open(ctx, current)
	if e != nil {
		return Renewal{}, e
	}
	unchanged := bytes.Equal(currentSecret, original) && current.ExpiresUnix == s.Renewals[i].Original.ExpiresUnix
	clear(currentSecret)
	if !unchanged {
		return l.holdRenewal(ctx, token, s, i)
	}
	expected := current.Revision
	if expected >= 1<<32-1 {
		return Renewal{}, Exhausted
	}
	current.Revision++
	current.ExpiresUnix = observed.ExpiresUnix
	current, e = l.broker.seal(ctx, current, observed.Secret)
	if e != nil {
		return Renewal{}, e
	}
	if e = l.renewalAuthority(ctx, token, r.Binding); e != nil {
		return Renewal{}, e
	}
	r.Phase = "completed"
	s.Renewals[i] = RenewalEntry{Renewal: r}
	if e = l.save(ctx, token, r.Binding, s, &current, expected); e != nil {
		return Renewal{}, storeError(e)
	}
	return r, nil
}
