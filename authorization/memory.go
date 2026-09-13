package authorization

import (
	"context"
	"google.golang.org/protobuf/proto"
	wire "lerna/gen/harness/v1"
	"time"
)

// MemoryAdmission reserves an operation's namespace and semantic identity.
// It is not a MemoryStore commit receipt. Expiry remains fixed after retries.
type MemoryAdmission struct {
	Subject, SemanticSHA256 string
	Action                  *wire.AuthorizationAction
	ExpiresUnixNano         int64
}
type MemoryOperationStatus struct {
	State     string
	Admission MemoryAdmission
}

func copyMemoryAdmission(in MemoryAdmission) MemoryAdmission {
	if in.Action != nil {
		in.Action = proto.Clone(in.Action).(*wire.AuthorizationAction)
	}
	return in
}
func memoryForeign(st *State, id string) bool {
	if _, ok := st.Operations[id]; ok {
		return true
	}
	if _, ok := st.ContentOperations[id]; ok {
		return true
	}
	if _, ok := st.RuntimeOperations[id]; ok {
		return true
	}
	if _, ok := st.ExecutionOperations[id]; ok {
		return true
	}
	if st.Signed != nil {
		if _, ok := st.Signed.Operations[id]; ok {
			return true
		}
		if _, ok := st.Signed.Uses[id]; ok {
			return true
		}
	}
	return false
}
func (s *Service) ReserveMemoryOperation(ctx context.Context, token, id, semantic string, action *wire.AuthorizationAction) (MemoryAdmission, error) {
	if !digestValid(semantic) || action == nil || !known(action) || (action.Action != "memory.put" && action.Action != "memory.correct" && action.Action != "memory.delete") {
		return MemoryAdmission{}, fail(Invalid)
	}
	action = proto.Clone(action).(*wire.AuthorizationAction)
	var out MemoryAdmission
	err := s.update(ctx, func(st *State, now time.Time) error {
		principal, e := authenticate(st, token, now)
		if e != nil {
			return e
		}
		decision, e := s.evaluate(st, principal, action, now)
		if e != nil {
			return e
		}
		if !decision.Allowed {
			return fail(Denied)
		}
		epoch, e := windowOf(st, id)
		if e != nil {
			return e
		}
		if memoryForeign(st, id) {
			return fail(IdentityConflict)
		}
		if old, ok := st.MemoryOperations[id]; ok {
			if old.Subject != principal.Subject {
				return fail(Denied)
			}
			if old.SemanticSHA256 == "" {
				return fail(ResultOnly)
			}
			if old.SemanticSHA256 != semantic || !proto.Equal(old.Action, action) {
				return fail(IdentityConflict)
			}
			if now.UnixNano() >= old.ExpiresUnixNano || epoch <= st.ClosedThrough {
				return fail(Expired)
			}
			out = copyMemoryAdmission(old)
			return nil
		}
		if epoch <= st.ClosedThrough || epoch != st.Window || now.UnixNano() >= st.WindowExpires {
			return fail(Expired)
		}
		if len(st.MemoryOperations) >= 512 {
			return fail(Unavailable)
		}
		if st.MemoryOperations == nil {
			st.MemoryOperations = map[string]MemoryAdmission{}
		}
		out = MemoryAdmission{Subject: principal.Subject, SemanticSHA256: semantic, Action: action, ExpiresUnixNano: st.WindowExpires}
		st.MemoryOperations[id] = copyMemoryAdmission(out)
		return nil
	})
	if err != nil {
		return MemoryAdmission{}, err
	}
	return out, nil
}

// InspectMemoryOperation reports only admission state. The caller must query
// MemoryStore for authoritative commit status, including after admission expiry.
func (s *Service) InspectMemoryOperation(ctx context.Context, token, id string) (MemoryOperationStatus, error) {
	var out MemoryOperationStatus
	err := s.update(ctx, func(st *State, now time.Time) error {
		principal, e := authenticate(st, token, now)
		if e != nil {
			return e
		}
		epoch, e := windowOf(st, id)
		if e != nil {
			return e
		}
		if memoryForeign(st, id) {
			return fail(IdentityConflict)
		}
		if entry, ok := st.MemoryOperations[id]; ok {
			if entry.Subject != principal.Subject {
				return fail(Denied)
			}
			decision, e := s.evaluate(st, principal, entry.Action, now)
			if e != nil {
				return e
			}
			if !decision.Allowed {
				return fail(Denied)
			}
			out = MemoryOperationStatus{State: "reserved", Admission: copyMemoryAdmission(entry)}
			if epoch <= st.ClosedThrough || now.UnixNano() >= entry.ExpiresUnixNano {
				out.State = "expired"
			}
			return nil
		}
		out.State = "not_admitted"
		if epoch <= st.ClosedThrough || epoch != st.Window || now.UnixNano() >= st.WindowExpires {
			out.State = "expired"
		}
		return nil
	})
	if err != nil {
		return MemoryOperationStatus{}, err
	}
	return out, nil
}
