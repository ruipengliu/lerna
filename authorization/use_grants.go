package authorization

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"google.golang.org/protobuf/proto"
)

func (s *Service) usePermitsAllowed(st *State, entry UseRecord, now time.Time, boundary func(int64)) (bool, error) {
	if len(entry.Spec.Permits) == 0 {
		return true, nil
	}
	if st.Signed == nil {
		return false, nil
	}
	// chain only checks current state; it neither signs nor reserves units.
	grants := GrantAuthority{service: s, config: st.Signed.Config}
	for _, permit := range entry.Spec.Permits {
		original, ok := st.Signed.Uses[permit.OperationID]
		if !ok || original != permit || permit.Subject != entry.Subject || permit.Namespace != entry.Spec.Namespace {
			return false, nil
		}
		matched := false
		for _, action := range entry.Spec.Actions {
			raw, err := (proto.MarshalOptions{Deterministic: true}).Marshal(action)
			if err != nil {
				return false, fail(Invalid)
			}
			hash := sha256.Sum256(raw)
			if hex.EncodeToString(hash[:]) == permit.ActionSHA256 {
				matched = true
				break
			}
		}
		if !matched {
			return false, nil
		}
		if err := grants.chain(st, permit.GrantID, now); err != nil {
			if Is(err, Denied) {
				return false, nil
			}
			return false, err
		}
		// Include every ancestor and participating principal expiry, not only
		// the leaf, when scheduling the next time-based recheck.
		id := permit.GrantID
		for depth := 0; id != ""; depth++ {
			if depth > grants.config.MaxDepth {
				return false, fail(Unavailable)
			}
			record := st.Signed.Grants[id].Record
			if record == nil || record.Spec == nil || record.Spec.Scope == nil {
				return false, fail(Unavailable)
			}
			boundary(record.Spec.Scope.ExpiresUnix)
			for _, principal := range st.Principals {
				if principal.Subject == record.Spec.Subject {
					boundary(principal.Expires)
				}
			}
			id = record.Parent
		}
	}
	return true, nil
}
