package authorization

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"

	"google.golang.org/protobuf/proto"
	wire "lerna/gen/harness/v1"
)

// UseSpec binds one host-controlled object's original local authorization
// requirements before its body is retained. This is not a grant or peer API.
// Source revisions require their own checks. Permits bind already allocated
// signed authority; registration cannot allocate or replace a read.
type UseSpec struct {
	Namespace, ID, Consumer, ConfigSHA256 string
	Until                                 int64
	Actions                               []*wire.AuthorizationAction
	Permits                               []UsePermit `json:",omitempty"`
	Target                                string      `json:",omitempty"`
}

// UseRecord is private authority state. It retains no raw credential or body.
// Invalidated uses never reactivate, even if a later policy restores access.
type UseRecord struct {
	Spec                                      UseSpec
	Subject, CredentialSHA256, SemanticSHA256 string
	CheckedRevision                           uint64
	NextCheck                                 int64
	Notice                                    *UseNotice
	Cleaned                                   bool
}

type UseNotice struct {
	ID       string
	Revision uint64
	At       int64
	Target   string `json:",omitempty"`
}

func validUse(in UseSpec) bool {
	if !validName(in.Namespace) || !validName(in.ID) || !validName(in.Consumer) || !digestValid(in.ConfigSHA256) || in.Until <= 0 || len(in.Actions) < 1 || len(in.Actions) > 16 || len(in.Permits) > 16 || len(in.Target) > 4096 {
		return false
	}
	for _, a := range in.Actions {
		if a == nil || !known(a) || !validName(a.Resource) || !validName(a.Action) || !validName(a.Purpose) || !validName(a.Location) {
			return false
		}
	}
	seen := map[string]bool{}
	for _, p := range in.Permits {
		if p.Namespace != in.Namespace || p.OperationID == "" || len(p.OperationID) > 512 || p.GrantID == "" || p.Subject == "" || p.Units == 0 || !digestValid(p.ActionSHA256) || !digestValid(p.SemanticSHA256) || seen[p.OperationID] {
			return false
		}
		seen[p.OperationID] = true
	}
	return true
}

func consumerConfig(st *State, consumer, config string) error {
	for _, entry := range st.Uses {
		if entry.Spec.Consumer == consumer && entry.Spec.ConfigSHA256 != config {
			return fail(IdentityConflict)
		}
	}
	return nil
}

func (s *Service) useAllowed(st *State, entry UseRecord, now time.Time) (bool, int64, error) {
	p, ok := st.Principals[entry.CredentialSHA256]
	if !ok || p.Subject != entry.Subject || p.Disabled || now.Unix() >= p.Expires || now.Unix() >= entry.Spec.Until {
		return false, 0, nil
	}
	next := entry.Spec.Until
	boundary := func(at int64) {
		if at > now.Unix() && at < next {
			next = at
		}
	}
	boundary(p.Expires)
	for _, rule := range st.Rules {
		if rule.Scope != nil {
			boundary(rule.Scope.ExpiresUnix)
		}
	}
	for _, grant := range st.Grants {
		if grant.Scope != nil {
			boundary(grant.Scope.ExpiresUnix)
		}
	}
	if allowed, err := s.usePermitsAllowed(st, entry, now, boundary); err != nil || !allowed {
		return allowed, 0, err
	}
	for _, action := range entry.Spec.Actions {
		decision, err := s.evaluate(st, p, action, now)
		if err != nil {
			return false, 0, err
		}
		if !decision.Allowed {
			return false, 0, nil
		}
	}
	return true, next, nil
}

func (s *Service) refreshUses(ctx context.Context, st *State, now time.Time) error {
	if len(st.Uses) > 512 {
		return fail(Unavailable)
	}
	for id, entry := range st.Uses {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Notice != nil || (entry.CheckedRevision == st.Revision && now.Unix() < entry.NextCheck) {
			continue
		}
		allowed, next, err := s.useAllowed(st, entry, now)
		if err != nil {
			return err
		}
		entry.CheckedRevision = st.Revision
		entry.NextCheck = next
		if !allowed {
			entry.Notice = &UseNotice{ID: id, Revision: st.Revision, At: now.Unix(), Target: entry.Spec.Target}
		}
		st.Uses[id] = entry
	}
	return nil
}

func (s *Service) useRegistration(token string, in UseSpec) (func(*State, time.Time) error, error) {
	if !validUse(in) {
		return nil, fail(Invalid)
	}
	actions := make([]*wire.AuthorizationAction, len(in.Actions))
	for i, a := range in.Actions {
		actions[i] = proto.Clone(a).(*wire.AuthorizationAction)
	}
	in.Actions = actions
	in.Permits = append([]UsePermit(nil), in.Permits...)
	raw, err := json.Marshal(in)
	if err != nil {
		return nil, fail(Invalid)
	}
	semantic := hex.EncodeToString(CredentialDigest(string(raw)))
	return func(st *State, now time.Time) error {
		p, err := authenticate(st, token, now)
		if err != nil {
			return err
		}
		if st.Namespace != in.Namespace {
			return fail(Denied)
		}
		if err := consumerConfig(st, in.Consumer, in.ConfigSHA256); err != nil {
			return err
		}
		credential := hex.EncodeToString(CredentialDigest(token))
		if old, ok := st.Uses[in.ID]; ok {
			if old.Subject != p.Subject || old.CredentialSHA256 != credential {
				return fail(Denied)
			}
			if old.SemanticSHA256 != semantic {
				return fail(IdentityConflict)
			}
			if old.Notice != nil {
				return fail(ResultOnly)
			}
			return nil
		}
		if len(st.Uses) >= 512 {
			return fail(Unavailable)
		}
		if in.Until > now.Add(24*time.Hour).Unix() || in.Until > p.Expires {
			return fail(Denied)
		}
		entry := UseRecord{Spec: in, Subject: p.Subject, CredentialSHA256: credential, SemanticSHA256: semantic, CheckedRevision: st.Revision}
		allowed, next, err := s.useAllowed(st, entry, now)
		if err != nil {
			return err
		}
		if !allowed {
			return fail(Denied)
		}
		entry.NextCheck = next
		if st.Uses == nil {
			st.Uses = map[string]UseRecord{}
		}
		st.Uses[in.ID] = entry
		return nil
	}, nil
}

func (s *Service) RegisterUse(ctx context.Context, token string, in UseSpec) error {
	fn, err := s.useRegistration(token, in)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return s.update(ctx, fn)
}

// PendingUses is a bounded trusted cleanup inbox, independent from expiring
// management receipts. Only explicit effect confirmation removes a notice.
func (s *Service) PendingUses(ctx context.Context, namespace, consumer, config string, limit int) ([]UseNotice, error) {
	if !validName(namespace) || !validName(consumer) || !digestValid(config) || limit < 1 || limit > 32 {
		return nil, fail(Invalid)
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var out []UseNotice
	err := s.update(ctx, func(st *State, now time.Time) error {
		out = nil
		if st.Namespace != namespace {
			return fail(Denied)
		}
		if err := consumerConfig(st, consumer, config); err != nil {
			return err
		}
		for _, entry := range st.Uses {
			if entry.Spec.Consumer == consumer && entry.Notice != nil && !entry.Cleaned {
				out = append(out, *entry.Notice)
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		if len(out) > limit {
			out = out[:limit]
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ConfirmUseCleaned follows actual controlled cleanup, never message receipt.
// Keep the invalidated identity so stale registrations cannot resurrect it.
func (s *Service) ConfirmUseCleaned(ctx context.Context, namespace, consumer, config string, notice UseNotice) error {
	if !validName(namespace) || !validName(consumer) || !digestValid(config) || !validName(notice.ID) || notice.At <= 0 {
		return fail(Invalid)
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return s.update(ctx, func(st *State, now time.Time) error {
		if st.Namespace != namespace {
			return fail(Denied)
		}
		if err := consumerConfig(st, consumer, config); err != nil {
			return err
		}
		entry, ok := st.Uses[notice.ID]
		if !ok || entry.Spec.Consumer != consumer {
			return fail(NotFound)
		}
		if entry.Notice == nil || *entry.Notice != notice {
			return fail(Conflict)
		}
		entry.Cleaned = true
		st.Uses[notice.ID] = entry
		return nil
	})
}
