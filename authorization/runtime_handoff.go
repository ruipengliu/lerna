package authorization

// RuntimeHandoffTransaction is an optional trusted storage seam. Import is
// called only after the task owner has verified a sealed, signed checkpoint.
// It carries exact original operations, never an issuer's signing secret.
type RuntimeHandoffTransaction interface {
	RuntimeTransaction
	ExportRuntimeAdmission([]string) (RuntimeAdmission, error)
	ImportRuntimeAdmission(RuntimeAdmission) error
}
type RuntimeAdmission struct {
	Scopes                map[string]string
	Authority             string
	Window, ClosedThrough uint64
	WindowExpires         int64
	Operations            map[string]string
}

func (t *runtimeTransaction) ExportRuntimeAdmission(ids []string) (RuntimeAdmission, error) {
	s := t.state
	out := RuntimeAdmission{Authority: s.Authority, Window: s.Window, ClosedThrough: s.ClosedThrough, WindowExpires: s.WindowExpires, Operations: map[string]string{}, Scopes: map[string]string{}}
	if len(ids) > 1024 {
		return out, fail(Unavailable)
	}
	for _, id := range ids {
		subject, ok := s.RuntimeOperations[id]
		if !ok {
			return out, fail(NotFound)
		}
		out.Operations[id] = subject
		if scope := s.RuntimeOperationScopes[id]; scope != "" {
			out.Scopes[id] = scope
		}
	}
	return out, nil
}
func (t *runtimeTransaction) ImportRuntimeAdmission(in RuntimeAdmission) error {
	s := t.state
	if in.Authority == "" || len(in.Authority) > 128 || in.Window == 0 || in.ClosedThrough > in.Window || in.WindowExpires <= 0 || len(in.Operations) > 1024 {
		return fail(Invalid)
	}
	if s.ImportedRuntimeOperations == nil {
		s.ImportedRuntimeOperations = map[string]string{}
	}
	additional := 0
	for id := range in.Operations {
		if _, exists := s.ImportedRuntimeOperations[id]; !exists {
			additional++
		}
	}
	if len(s.ImportedRuntimeOperations)+additional > 10000 {
		return fail(Unavailable)
	}
	for id, subject := range in.Operations {
		if id == "" || len(id) > 512 || subject == "" {
			return fail(Invalid)
		}
		if old, ok := s.ImportedRuntimeOperations[id]; ok && old != subject {
			return fail(IdentityConflict)
		}
		if _, ok := s.ExecutionOperations[id]; ok {
			return fail(IdentityConflict)
		}
		if _, ok := s.ContentOperations[id]; ok {
			return fail(IdentityConflict)
		}
		if _, ok := s.MemoryOperations[id]; ok {
			return fail(IdentityConflict)
		}
		if _, ok := s.Operations[id]; ok {
			return fail(IdentityConflict)
		}
		if s.Signed != nil {
			if _, ok := s.Signed.Uses[id]; ok {
				return fail(IdentityConflict)
			}
			if _, ok := s.Signed.Operations[id]; ok {
				return fail(IdentityConflict)
			}
		}
		if old, ok := s.RuntimeOperations[id]; ok && old != subject {
			return fail(IdentityConflict)
		}
		if e := t.CheckRuntimeScope(id, in.Scopes[id]); e != nil {
			return e
		}
		s.ImportedRuntimeOperations[id] = subject
		if s.RuntimeOperations == nil {
			s.RuntimeOperations = map[string]string{}
		}
		s.RuntimeOperations[id] = subject
		if scope := in.Scopes[id]; scope != "" {
			if e := t.BindRuntimeScope(id, scope); e != nil {
				return e
			}
		}
	}
	if s.ImportedRuntimeWindows == nil {
		s.ImportedRuntimeWindows = map[string]RuntimeAdmission{}
	}
	old, exists := s.ImportedRuntimeWindows[in.Authority]
	if !exists && len(s.ImportedRuntimeWindows) >= 32 {
		return fail(Unavailable)
	}
	if exists && old.Window == in.Window && old.WindowExpires != in.WindowExpires {
		return fail(IdentityConflict)
	}
	if !exists || in.Window >= old.Window {
		meta := in
		meta.Operations = nil
		meta.Scopes = nil
		meta.ClosedThrough = max(meta.ClosedThrough, old.ClosedThrough)
		s.ImportedRuntimeWindows[in.Authority] = meta
	}
	return nil
}

// RuntimeOperationScope keeps a forwarded identity restricted to its original
// task. It is optional for adapters that do not implement owner migration.
type RuntimeOperationScope interface {
	CheckRuntimeScope(string, string) error
	BindRuntimeScope(string, string) error
}

func (t *runtimeTransaction) CheckRuntimeScope(id, scope string) error {
	if bound := t.state.RuntimeOperationScopes[id]; bound != "" && bound != scope {
		return fail(Denied)
	}
	return nil
}
func (t *runtimeTransaction) BindRuntimeScope(id, scope string) error {
	if scope == "" || len(scope) > 512 || t.state.RuntimeOperations[id] == "" {
		return fail(Invalid)
	}
	if e := t.CheckRuntimeScope(id, scope); e != nil {
		return e
	}
	if t.state.RuntimeOperationScopes == nil {
		t.state.RuntimeOperationScopes = map[string]string{}
	}
	t.state.RuntimeOperationScopes[id] = scope
	return nil
}
