package authorization

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	wire "lerna/gen/harness/v1"
	"lerna/internal/randomid"
	"time"
)

type Service struct {
	updates chan struct{}
	store   Store
	clock   Clock
	config  Config
}

func New(store Store, clock Clock, config Config) (*Service, error) {
	if store == nil || clock == nil {
		return nil, fail(Invalid)
	}
	if err := config.validate(); err != nil {
		return nil, err
	}
	return &Service{updates: make(chan struct{}, 1), store: store, clock: clock, config: config}, nil
}
func NewCredential() (string, error) {
	value, err := randomid.New()
	if err != nil {
		return "", fail(Unavailable)
	}
	return value, nil
}

func CredentialDigest(token string) []byte { sum := sha256.Sum256([]byte(token)); return sum[:] }

// Bootstrap is a privileged installation port. Never expose it as a business RPC.
func (s *Service) Bootstrap(ctx context.Context, namespace, administrator string) (string, error) {
	token, err := NewCredential()
	if err != nil {
		return "", err
	}
	if err = s.Initialize(ctx, namespace, administrator, token); err != nil {
		return "", err
	}
	return token, nil
}

// Initialize accepts a credential already durably held by the trusted installer.
// This lets the installer authenticate and reconcile if the initialization reply is lost.
func (s *Service) Initialize(ctx context.Context, namespace, administrator, token string) error {
	decoded, err := hex.DecodeString(token)
	if !validName(namespace) || !validName(administrator) || err != nil || len(decoded) != 32 {
		return fail(Invalid)
	}
	authority, err := randomid.New()
	if err != nil {
		return err
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return fail(Unavailable)
	}
	return s.update(ctx, func(st *State, now time.Time) error {
		if st.Format != 0 {
			return fail(Conflict)
		}
		*st = State{Format: 1, Namespace: namespace, Authority: authority, Secret: secret, Config: s.config,
			Principals: map[string]Principal{hex.EncodeToString(CredentialDigest(token)): {Subject: administrator, Administrator: true, Expires: now.Add(s.config.CredentialTTL).Unix()}},
			Resources:  map[string]string{"root": ""}, Grants: make(map[string]*wire.LocalGrant), Operations: make(map[string]OperationRecord), LastTime: now.UnixNano()}
		return nil
	})
}

func (s *Service) update(ctx context.Context, fn func(*State, time.Time) error) error {
	// Fair, cancellable admission prevents local polling/content readers from
	// exhausting each other's CAS retries. Separate authorities/processes still
	// arbitrate through the store version; callbacks must not nest store calls.
	select {
	case s.updates <- struct{}{}:
		defer func() { <-s.updates }()
	case <-ctx.Done():
		return ctx.Err()
	}
	for attempt := 0; attempt < 8; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		snap, err := s.store.Load(ctx)
		if err != nil {
			return err
		}
		now, err := s.clock.Now()
		if err != nil || now.UnixNano() <= 0 || now.UnixNano() < snap.State.LastTime {
			return fail(TimeUntrusted)
		}
		if snap.State.Format != 0 && (snap.State.Format != 1 || snap.State.Config != s.config) {
			return fail(Invalid)
		}
		snap.State.LastTime = now.UnixNano()
		if err := s.refreshUses(ctx, &snap.State, now); err != nil {
			return err
		}
		if err := fn(&snap.State, now); err != nil {
			return err
		}
		if err := s.refreshUses(ctx, &snap.State, now); err != nil {
			return err
		}
		err = s.store.Commit(ctx, snap.Version, snap.State)
		if Is(err, Conflict) {
			// Independent handles still contend through CAS. Give the winner's
			// bounded transaction sequence time to finish instead of spending
			// all eight attempts in the same burst. Caller deadlines still win.
			if attempt < 7 {
				timer := time.NewTimer(time.Duration(1<<attempt) * time.Millisecond)
				select {
				case <-timer.C:
				case <-ctx.Done():
					timer.Stop()
					return ctx.Err()
				}
			}
			continue
		}
		return err
	}
	return fail(Unavailable)
}
func authenticate(st *State, token string, now time.Time) (Principal, error) {
	if len(token) != 64 {
		return Principal{}, fail(Unauthenticated)
	}
	p, ok := st.Principals[hex.EncodeToString(CredentialDigest(token))]
	if !ok || p.Disabled || now.Unix() >= p.Expires {
		return Principal{}, fail(Unauthenticated)
	}
	return p, nil
}
func (s *Service) Authenticate(ctx context.Context, token string) (Identity, error) {
	var out Identity
	err := s.update(ctx, func(st *State, now time.Time) error {
		p, err := authenticate(st, token, now)
		if err != nil {
			return err
		}
		out = Identity{p.Subject, st.Namespace}
		return nil
	})
	return out, err
}
