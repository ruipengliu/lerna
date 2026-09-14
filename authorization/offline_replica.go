package authorization

import (
	"bytes"
	"context"
	"encoding/json"
	"lerna/internal/jsonvalue"
	"sync"
	"time"

	wire "lerna/gen/harness/v1"
)

// OfflineClock exposes wall time and a process-local elapsed-time source.
// The host is trusted; detectable suspend/drift invalidates the current anchor.
type OfflineClock interface {
	Sample() (time.Time, time.Duration, error)
}
type SystemOfflineClock struct{}

var offlineProcessStart = time.Now()

func (SystemOfflineClock) Sample() (time.Time, time.Duration, error) {
	return time.Now().Round(0), time.Since(offlineProcessStart), nil
}

type offlineReplicaState struct {
	Config    OfflineConfig
	Recipient GrantPresentation
	Session   uint64
	Received  *OfflinePage
	Pending   []OfflineDecision
	Applied   uint64
	Decisions []OfflineDecision
}
type OfflineReplica struct {
	retain       func(context.Context) error
	service      *Service
	view         string
	peer         GrantPresentation
	config       OfflineConfig
	crypto       GrantCrypto
	clock        OfflineClock
	mu           sync.Mutex
	session      uint64
	challenge    string
	wall         time.Time
	elapsed      time.Duration
	validUntil   time.Duration
	anchored     bool
	authorityNow int64
	lastElapsed  time.Duration
}

func NewOfflineReplica(s *Service, view string, p GrantPresentation, c OfflineConfig, crypto GrantCrypto, clock OfflineClock, retain func(context.Context) error) (*OfflineReplica, error) {
	if s == nil || !validName(view) || !validRecipient(p) || !c.valid() || crypto == nil || clock == nil || retain == nil {
		return nil, fail(Invalid)
	}
	r := &OfflineReplica{retain: retain, service: s, view: view, peer: recipient(p), config: c, crypto: crypto, clock: clock}
	ctx, cancel := context.WithTimeout(context.Background(), c.IOTimeout)
	defer cancel()
	e := s.update(ctx, func(st *State, now time.Time) error {
		partition := offlinePartition(st)
		state := partition.Replicas[view]
		if state == nil {
			if len(partition.Replicas) >= 16 {
				return fail(Unavailable)
			}
			state = &offlineReplicaState{Config: c, Recipient: r.peer}
			partition.Replicas[view] = state
		}
		if state.Config != c || state.Recipient != r.peer {
			return fail(Denied)
		}
		state.Session++
		r.session = state.Session
		return nil
	})
	return r, e
}
func (r *OfflineReplica) state(st *State) (*offlineReplicaState, error) {
	s := offlinePartition(st).Replicas[r.view]
	if s == nil || s.Session != r.session || s.Config != r.config || s.Recipient != r.peer {
		return nil, fail(Denied)
	}
	return s, nil
}
func (r *OfflineReplica) Begin(ctx context.Context) (OfflineQuery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	challenge, e := NewCredential()
	if e != nil {
		return OfflineQuery{}, e
	}
	wall, elapsed, e := r.clock.Sample()
	if e != nil {
		return OfflineQuery{}, fail(TimeUntrusted)
	}
	var after uint64
	e = r.service.update(ctx, func(st *State, now time.Time) error {
		s, e := r.state(st)
		if e != nil {
			return e
		}
		after = s.Applied
		s.Pending = nil
		s.Received = nil
		return nil
	})
	if e != nil {
		return OfflineQuery{}, e
	}
	r.challenge = challenge
	r.wall = wall
	r.elapsed = elapsed
	r.lastElapsed = elapsed
	r.anchored = false
	return OfflineQuery{Challenge: challenge, After: after}, nil
}

// Receive persists authenticated pages without authorizing execution. Its key
// verifier is pinned by the installer, never supplied by a page.
func (r *OfflineReplica) Receive(ctx context.Context, material string) error {
	if e := r.retain(ctx); e != nil {
		return e
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(material) > 65536 {
		return fail(Invalid)
	}
	ctx, cancel := context.WithTimeout(ctx, r.config.IOTimeout)
	defer cancel()
	raw, e := r.crypto.Verify(ctx, material)
	if e != nil {
		return fail(Denied)
	}
	if _, e = jsonvalue.Decode(raw); e != nil {
		return fail(Invalid)
	}
	var page OfflinePage
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&page) != nil {
		return fail(Invalid)
	}
	if page.View != r.view || page.Recipient != r.peer || page.Challenge != r.challenge || r.challenge == "" || page.Revision == 0 || page.Total < 0 || page.Total > r.config.Records || page.Offset < 0 || len(page.Decisions) > r.config.PageSize || page.Now <= 0 || page.Until <= page.Now || page.Until-page.Now > int64(r.config.Window) {
		return fail(Denied)
	}
	for _, item := range page.Decisions {
		if !item.Permit.matches(GrantPresentation{Namespace: r.peer.Namespace, Subject: r.peer.Subject, Audience: r.peer.Audience, Presenter: r.peer.Presenter, CertificateSHA256: r.peer.CertificateSHA256, OperationID: item.Permit.OperationID, SemanticSHA256: item.Permit.SemanticSHA256}) || item.Permit.Units != 1 || !known(item.Action) || actionDigest(item.Action) != item.Permit.ActionSHA256 || item.Expires <= 0 {
			return fail(Denied)
		}
	}
	return r.service.update(ctx, func(st *State, now time.Time) error {
		s, e := r.state(st)
		if e != nil {
			return e
		}
		if (page.Base != 0 && page.Base != s.Applied) || page.Revision < s.Applied {
			return fail(Conflict)
		}
		if s.Received != nil {
			old, _ := json.Marshal(s.Received)
			incoming, _ := json.Marshal(page)
			if bytes.Equal(old, incoming) {
				return nil
			}
		}
		if page.Offset != len(s.Pending) {
			return fail(Conflict)
		}
		if s.Received != nil && (page.Revision != s.Received.Revision || page.Total != s.Received.Total) {
			return fail(Conflict)
		}
		s.Pending = append(s.Pending, page.Decisions...)
		s.Received = &page
		if len(s.Pending) > page.Total {
			return fail(Invalid)
		}
		return offlineSize(st, r.config.Bytes)
	})
}
func (r *OfflineReplica) Next(ctx context.Context) (OfflineQuery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var q OfflineQuery
	e := r.service.update(ctx, func(st *State, now time.Time) error {
		s, e := r.state(st)
		if e != nil {
			return e
		}
		if s.Received == nil {
			return fail(NotFound)
		}
		q = OfflineQuery{Challenge: r.challenge, After: s.Received.Base, PageRevision: s.Received.Revision, Offset: len(s.Pending)}
		return nil
	})
	return q, e
}
func (r *OfflineReplica) Apply(ctx context.Context) error {
	if e := r.retain(ctx); e != nil {
		return e
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	wall, elapsed, e := r.clock.Sample()
	if e != nil {
		return fail(TimeUntrusted)
	}
	delta := elapsed - r.elapsed
	drift := wall.Round(0).Sub(r.wall.Round(0)) - delta
	fresh := r.challenge != "" && delta >= 0 && delta <= r.config.IOTimeout && drift <= r.config.Skew && drift >= -r.config.Skew
	var duration time.Duration
	var current bool
	e = r.service.update(ctx, func(st *State, now time.Time) error {
		s, e := r.state(st)
		if e != nil {
			return e
		}
		p := s.Received
		if p == nil || len(s.Pending) != p.Total || (r.challenge != "" && p.Challenge != r.challenge) {
			return fail(Conflict)
		}
		current = p.Revision == p.Head
		duration = time.Duration(p.Until-p.Now) - r.config.Skew - delta
		r.authorityNow = p.Now
		s.Decisions = s.Pending
		s.Pending = nil
		s.Applied = p.Revision
		return nil
	})
	if e != nil {
		return e
	}
	r.validUntil = elapsed + duration
	r.anchored = duration > 0 && current && fresh
	return nil
}

// Check is an additional remote-authority gate. Local task qualification,
// operation idempotency and current local policy remain mandatory at execution.
func (r *OfflineReplica) Check(ctx context.Context, p GrantPresentation, a *wire.AuthorizationAction) error {
	return r.check(ctx, p, a, false)
}
func (r *OfflineReplica) check(ctx context.Context, p GrantPresentation, a *wire.AuthorizationAction, online bool) error {
	if e := r.retain(ctx); e != nil {
		return e
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if recipient(p) != r.peer || !known(a) {
		return fail(Denied)
	}
	wall, elapsed, e := r.clock.Sample()
	drift := wall.Round(0).Sub(r.wall.Round(0)) - (elapsed - r.elapsed)
	if e != nil || !r.anchored || elapsed < r.lastElapsed || elapsed >= r.validUntil || drift > r.config.Skew || drift < -r.config.Skew {
		r.anchored = false
		return fail(TimeUntrusted)
	}
	r.lastElapsed = elapsed
	return r.service.update(ctx, func(st *State, now time.Time) error {
		s, e := r.state(st)
		if e != nil {
			return e
		}
		for _, item := range s.Decisions {
			if item.Permit.OperationID == p.OperationID {
				if item.Expires*int64(time.Second) <= r.authorityNow+int64(elapsed-r.elapsed)+int64(r.config.Skew) || !item.Allowed || (!item.Offline && !online) || !item.Permit.matches(p) || actionDigest(a) != item.Permit.ActionSHA256 {
					return fail(Denied)
				}
				return nil
			}
		}
		return fail(Denied)
	})
}

// OfflineProgress distinguishes transport persistence from atomic application.
type OfflineProgress struct {
	Received, Applied uint64
	Pending           bool
}

func (r *OfflineReplica) Progress(ctx context.Context) (OfflineProgress, error) {
	var p OfflineProgress
	e := r.service.update(ctx, func(st *State, now time.Time) error {
		s, e := r.state(st)
		if e != nil {
			return e
		}
		p.Applied = s.Applied
		if s.Received != nil {
			p.Received = s.Received.Revision
		}
		p.Pending = p.Received != p.Applied || len(s.Pending) > 0
		return nil
	})
	return p, e
}

// Synchronize completes a bounded snapshot/replacement and atomically applies
// it. An expired cursor restarts once from a fresh snapshot challenge.
type OfflineRead func(context.Context, OfflineQuery) (string, error)

func (r *OfflineReplica) Synchronize(ctx context.Context, read OfflineRead) error {
	if read == nil {
		return fail(Invalid)
	}
	ctx, cancel := context.WithTimeout(ctx, r.config.IOTimeout)
	defer cancel()
	q, e := r.Begin(ctx)
	if e != nil {
		return e
	}
	restarted := false
	for attempt := 0; attempt <= 2*r.config.Records+1; attempt++ {
		material, e := read(ctx, q)
		if Is(e, Expired) && !restarted {
			restarted = true
			q = OfflineQuery{Challenge: q.Challenge}
			e = r.resetSnapshot(ctx)
			if e != nil {
				return e
			}
			continue
		}
		if e != nil {
			return e
		}
		if e = r.Receive(ctx, material); e != nil {
			return e
		}
		next, e := r.Next(ctx)
		if e != nil {
			return e
		}
		done := false
		e = r.service.update(ctx, func(st *State, now time.Time) error {
			s, e := r.state(st)
			if e != nil {
				return e
			}
			done = s.Received != nil && len(s.Pending) == s.Received.Total
			return nil
		})
		if e != nil {
			return e
		}
		if done {
			return r.Apply(ctx)
		}
		q = next
	}
	return fail(Unavailable)
}
func (r *OfflineReplica) resetSnapshot(ctx context.Context) error {
	return r.service.update(ctx, func(st *State, now time.Time) error {
		s, e := r.state(st)
		if e != nil {
			return e
		}
		s.Pending = nil
		s.Received = nil
		return nil
	})
}

// Authorize permits online-required work only after a fresh successful exchange
// in this call. Without a reader, only the original finite offline lease applies.
func (r *OfflineReplica) Authorize(ctx context.Context, p GrantPresentation, a *wire.AuthorizationAction, read OfflineRead) error {
	if read != nil {
		if e := r.Synchronize(ctx, read); e != nil {
			return e
		}
		return r.check(ctx, p, a, true)
	}
	return r.Check(ctx, p, a)
}

// Confirm reports the durable application boundary over the authenticated
// channel. Failure leaves application intact and confirmation pending for retry.
func (r *OfflineReplica) Confirm(ctx context.Context, read OfflineRead) error {
	if read == nil {
		return fail(Invalid)
	}
	p, e := r.Progress(ctx)
	if e != nil {
		return e
	}
	if p.Applied == 0 {
		return fail(Conflict)
	}
	id, e := NewCredential()
	if e != nil {
		return e
	}
	_, e = read(ctx, OfflineQuery{Challenge: id, After: p.Applied, Applied: p.Applied})
	return e
}
