package josegrant

import (
	"context"
	"crypto/ecdsa"
	"lerna/authorization"
	"time"
)

// VerificationKey comes from trusted deployment configuration, never a token.
// Old keys have explicit acceptance deadlines, including during rotation overlap.
type VerificationKey struct {
	Public *ecdsa.PublicKey
	Until  time.Time
}

func NewRotating(kid string, private *ecdsa.PrivateKey, trusted map[string]VerificationKey, clock authorization.Clock) (*Adapter, error) {
	if clock == nil {
		return nil, &authorization.Error{Code: authorization.Invalid}
	}
	now, err := clock.Now()
	if err != nil {
		return nil, err
	}
	keys := map[string]*ecdsa.PublicKey{}
	until := map[string]time.Time{}
	for id, key := range trusted {
		if !key.Until.After(now) || key.Until.After(now.Add(24*time.Hour)) {
			return nil, &authorization.Error{Code: authorization.Invalid}
		}
		keys[id] = key.Public
		until[id] = key.Until
	}
	a, err := New(kid, private, keys)
	if err != nil {
		return nil, err
	}
	a.clock = clock
	a.until = until
	a.kid = kid
	return a, nil
}
func (a *Adapter) current(ctx context.Context, kid string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.clock != nil {
		now, err := a.clock.Now()
		if err != nil {
			return &authorization.Error{Code: authorization.TimeUntrusted}
		}
		if !now.Before(a.until[kid]) {
			return &authorization.Error{Code: authorization.Denied}
		}
	}
	return nil
}
