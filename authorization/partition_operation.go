package authorization

import "time"

// partitionOperation shares window/identity rules without merging business data
// partitions or their distinct authorization interfaces.
func (s *Service) partitionOperation(st *State, now time.Time, own *map[string]string, other map[string]string, id, subject string, claim bool) error {
	if _, ok := st.MemoryOperations[id]; ok {
		return fail(IdentityConflict)
	}
	if _, ok := st.ExecutionOperations[id]; ok {
		return fail(IdentityConflict)
	}
	if old, ok := st.ImportedRuntimeOperations[id]; ok {
		if old != subject {
			return fail(Denied)
		}
		if (*own)[id] != subject {
			return fail(IdentityConflict)
		}
		return nil
	}
	epoch, err := windowOf(st, id)
	if err != nil {
		return err
	}
	if st.Signed != nil {
		if _, ok := st.Signed.Uses[id]; ok {
			return fail(IdentityConflict)
		}
		if _, ok := st.Signed.Operations[id]; ok {
			return fail(IdentityConflict)
		}
	}
	if _, ok := other[id]; ok {
		return fail(IdentityConflict)
	}
	if old, ok := (*own)[id]; ok {
		if old != subject {
			return fail(Denied)
		}
		return nil
	}
	if old, ok := st.Operations[id]; ok {
		if old.Subject != subject {
			return fail(Denied)
		}
		return fail(IdentityConflict)
	}
	if epoch <= st.ClosedThrough || epoch != st.Window || now.UnixNano() >= st.WindowExpires {
		return fail(Expired)
	}
	if claim {
		if *own == nil {
			*own = map[string]string{}
		}
		(*own)[id] = subject
	}
	return nil
}
