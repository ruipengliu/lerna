package authorization

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"

	"google.golang.org/protobuf/proto"
	wire "lerna/gen/harness/v1"
)

// OfflineConfig bounds an exact-operation authorization view, never a Store dump.
type OfflineConfig struct {
	Window, Skew, IOTimeout, Retention time.Duration
	Records, PageSize, Bytes           int
}

func (c OfflineConfig) valid() bool {
	return c.Window >= time.Second && c.Window <= time.Hour && c.Skew >= 0 && c.Skew < c.Window && c.IOTimeout >= time.Millisecond && c.IOTimeout <= 5*time.Second && c.Retention >= c.IOTimeout && c.Retention <= time.Hour && c.Records > 0 && c.Records <= 128 && c.PageSize > 0 && c.PageSize <= 16 && c.PageSize <= c.Records && c.Bytes >= 4096 && c.Bytes <= 1<<20
}
func recipient(p GrantPresentation) GrantPresentation {
	p.OperationID = ""
	p.SemanticSHA256 = ""
	return p
}
func validRecipient(p GrantPresentation) bool {
	return validName(p.Namespace) && validTarget(&wire.GrantDelegateTarget{Subject: p.Subject, Audience: p.Audience, Presenter: p.Presenter, CertificateSha256: p.CertificateSHA256})
}

type offlineAllocation struct {
	Permit  UsePermit
	Action  *wire.AuthorizationAction
	Offline bool
}

// OfflineDecision is the current effective policy for one already allocated
// operation. Ancestor/policy denial is explicit; grants and signing keys are not copied.
type OfflineDecision struct {
	Permit           UsePermit
	Action           *wire.AuthorizationAction
	Offline, Allowed bool
	Expires          int64
	Revision         uint64
}
type offlineVersion struct {
	Revision  uint64
	Created   int64
	Decisions []OfflineDecision
}
type offlineSource struct {
	Config      OfflineConfig
	Recipient   GrantPresentation
	Allocations map[string]offlineAllocation
	Versions    []offlineVersion
	Applied     uint64
}
type offlineState struct {
	Sources  map[string]*offlineSource
	Replicas map[string]*offlineReplicaState
}
type OfflineView struct {
	grant  *GrantAuthority
	id     string
	peer   GrantPresentation
	config OfflineConfig
}

func (g *GrantAuthority) OfflineView(id string, p GrantPresentation, c OfflineConfig) (*OfflineView, error) {
	if g == nil || !validName(id) || !validRecipient(p) || !c.valid() {
		return nil, fail(Invalid)
	}
	return &OfflineView{g, id, recipient(p), c}, nil
}
func offlinePartition(st *State) *offlineState {
	if st.Offline == nil {
		st.Offline = &offlineState{}
	}
	if st.Offline.Sources == nil {
		st.Offline.Sources = map[string]*offlineSource{}
	}
	if st.Offline.Replicas == nil {
		st.Offline.Replicas = map[string]*offlineReplicaState{}
	}
	return st.Offline
}
func (v *OfflineView) source(st *State) (*offlineSource, error) {
	partition := offlinePartition(st)
	s := partition.Sources[v.id]
	if s == nil {
		if len(partition.Sources) >= 16 {
			return nil, fail(Unavailable)
		}
		s = &offlineSource{Config: v.config, Recipient: v.peer, Allocations: map[string]offlineAllocation{}}
		partition.Sources[v.id] = s
	}
	if s.Config != v.config || s.Recipient != v.peer {
		return nil, fail(Denied)
	}
	return s, nil
}

// Allocate reserves exactly one unit under the original operation. A failed
// view commit leaves the authority reservation intact; retry never refunds it.
func (v *OfflineView) Allocate(ctx context.Context, material string, p GrantPresentation, a *wire.AuthorizationAction, offline bool) error {
	if recipient(p) != v.peer {
		return fail(Denied)
	}
	permit, e := v.grant.ReserveUse(ctx, material, p, a, 1)
	if e != nil {
		return e
	}
	return v.grant.service.update(ctx, func(st *State, now time.Time) error {
		s, e := v.source(st)
		if e != nil {
			return e
		}
		allocation := offlineAllocation{permit, proto.Clone(a).(*wire.AuthorizationAction), offline}
		if old, ok := s.Allocations[p.OperationID]; ok {
			if old.Permit != permit || old.Offline != offline || !proto.Equal(old.Action, a) {
				return fail(IdentityConflict)
			}
			return nil
		}
		if len(s.Allocations) >= v.config.Records {
			return fail(Unavailable)
		}
		s.Allocations[p.OperationID] = allocation
		return offlineSize(st, v.config.Bytes)
	})
}
func offlineSize(st *State, limit int) error {
	b, e := json.Marshal(st.Offline)
	if e != nil {
		return fail(Invalid)
	}
	if len(b) > limit {
		return fail(Unavailable)
	}
	return nil
}

// OfflineQuery binds every read to a fresh receiver challenge. Revision is a
// completed snapshot boundary; PageRevision pins subsequent snapshot pages.
type OfflineQuery struct {
	Applied             uint64
	Challenge           string
	After, PageRevision uint64
	Offset              int
}
type OfflinePage struct {
	Head           uint64
	View           string
	Recipient      GrantPresentation
	Challenge      string
	Revision, Base uint64
	Offset, Total  int
	Now, Until     int64 `json:",string"`
	Decisions      []OfflineDecision
}

func (v *OfflineView) Read(ctx context.Context, q OfflineQuery) (string, error) {
	if !digestValid(q.Challenge) || q.Offset < 0 || q.Offset > v.config.Records {
		return "", fail(Invalid)
	}
	ctx, cancel := context.WithTimeout(ctx, v.config.IOTimeout)
	defer cancel()
	var page OfflinePage
	e := v.grant.service.update(ctx, func(st *State, now time.Time) error {
		s, e := v.source(st)
		if e != nil {
			return e
		}
		if e = v.refresh(st, s, now); e != nil {
			return e
		}
		current := s.Versions[len(s.Versions)-1]
		if q.Applied > q.After || q.Applied > current.Revision {
			return fail(Conflict)
		}
		if q.Applied > s.Applied {
			s.Applied = q.Applied
		}
		selected := current
		if q.PageRevision != 0 {
			found := false
			for _, version := range s.Versions {
				if version.Revision == q.PageRevision {
					selected = version
					found = true
				}
			}
			if !found {
				return fail(Expired)
			}
		}
		if q.After > current.Revision {
			return fail(Conflict)
		}
		if q.After != 0 {
			found := false
			for _, version := range s.Versions {
				if version.Revision == q.After {
					found = true
				}
			}
			if !found {
				return fail(Expired)
			}
		}
		// Every increment is a complete replacement of this small exact-operation
		// view. This retains deletions/denials and avoids partial policy application.
		if q.Offset > len(selected.Decisions) {
			return fail(Invalid)
		}
		end := q.Offset + v.config.PageSize
		if end > len(selected.Decisions) {
			end = len(selected.Decisions)
		}
		page = OfflinePage{Head: current.Revision, View: v.id, Recipient: v.peer, Challenge: q.Challenge, Revision: selected.Revision, Base: q.After, Offset: q.Offset, Total: len(selected.Decisions), Now: now.UnixNano(), Until: now.Add(v.config.Window).UnixNano(), Decisions: selected.Decisions[q.Offset:end]}
		return offlineSize(st, v.config.Bytes)
	})
	if e != nil {
		return "", e
	}
	raw, e := json.Marshal(page)
	if e != nil {
		return "", e
	}
	if len(raw) > 16384 {
		return "", fail(Unavailable)
	}
	return v.grant.crypto.Sign(ctx, raw)
}
func actionDigest(a *wire.AuthorizationAction) string {
	b, e := (proto.MarshalOptions{Deterministic: true}).Marshal(a)
	if e != nil {
		return ""
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// MatchRecipient authenticates the node receiving the allocated view. The
// original grant presenter may differ from this execution recipient.
func (v *OfflineView) MatchRecipient(p GrantPresentation) error {
	if p.Namespace != v.peer.Namespace || p.Subject != v.peer.Subject || p.Presenter != v.peer.Audience {
		return fail(Denied)
	}
	return nil
}

func offlineExpiry(st *State, id string, now time.Time) int64 {
	until := int64(1 << 62)
	clamp := func(t int64) {
		if t < until {
			until = t
		}
	}
	for depth := 0; id != "" && depth < 17; depth++ {
		entry, ok := st.Signed.Grants[id]
		if !ok {
			return now.Unix()
		}
		spec := entry.Record.Spec
		clamp(spec.Scope.ExpiresUnix)
		for _, principal := range st.Principals {
			if principal.Subject == spec.Subject && !principal.Disabled && principal.Expires > now.Unix() {
				clamp(principal.Expires)
			}
		}
		if st.Nodes != nil {
			clamp(st.Nodes.Records[spec.Presenter].Expires)
			clamp(st.Nodes.Records[spec.Audience].Expires)
		}
		id = entry.Record.Parent
	}
	// A cached positive decision cannot outlive the policy used to establish it.
	for _, rule := range st.Rules {
		if rule.Scope != nil && rule.Scope.ExpiresUnix > now.Unix() {
			clamp(rule.Scope.ExpiresUnix)
		}
	}
	return until
}

type OfflineAuthorityProgress struct {
	Current, Applied uint64
	Pending          bool
}

func (v *OfflineView) Progress(ctx context.Context) (OfflineAuthorityProgress, error) {
	var p OfflineAuthorityProgress
	e := v.grant.service.update(ctx, func(st *State, now time.Time) error {
		s, e := v.source(st)
		if e != nil {
			return e
		}
		if e = v.refresh(st, s, now); e != nil {
			return e
		}
		if len(s.Versions) > 0 {
			p.Current = s.Versions[len(s.Versions)-1].Revision
		}
		p.Applied = s.Applied
		p.Pending = p.Current != p.Applied
		return nil
	})
	return p, e
}

func (v *OfflineView) refresh(st *State, s *offlineSource, now time.Time) error {
	// Current authorization is recomputed even with no delivered notification.
	keys := make([]string, 0, len(s.Allocations))
	for id := range s.Allocations {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	decisions := make([]OfflineDecision, 0, len(keys))
	for _, id := range keys {
		a := s.Allocations[id]
		p := v.peer
		p.OperationID = id
		p.SemanticSHA256 = a.Permit.SemanticSHA256
		tx := &executionTransaction{v.grant.service.runtime(st, now), st, v.grant}
		_, ok := st.Signed.Grants[a.Permit.GrantID]
		if !ok {
			return fail(Denied)
		}
		decisions = append(decisions, OfflineDecision{a.Permit, a.Action, a.Offline, tx.ValidateUse(a.Permit, p, a.Action) == nil, offlineExpiry(st, a.Permit.GrantID, now), st.Revision})
	}
	raw, _ := json.Marshal(decisions)
	var old []byte
	if len(s.Versions) > 0 {
		old, _ = json.Marshal(s.Versions[len(s.Versions)-1].Decisions)
	}
	if string(raw) != string(old) || len(s.Versions) == 0 {
		rev := uint64(1)
		if len(s.Versions) > 0 {
			rev = s.Versions[len(s.Versions)-1].Revision + 1
		}
		s.Versions = append(s.Versions, offlineVersion{rev, now.UnixNano(), decisions})
	}
	for len(s.Versions) > 1 && (len(s.Versions) > v.config.Records || s.Versions[0].Created < now.Add(-v.config.Retention).UnixNano()) {
		s.Versions = s.Versions[1:]
	}

	return offlineSize(st, v.config.Bytes)
}
