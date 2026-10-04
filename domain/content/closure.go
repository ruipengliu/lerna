package content

import (
	"context"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/runtime"
	"sort"
)

// registeredClosure preserves exact intermediate versions. Declarations remain
// unchanged: this traversal is current qualification, never generation evidence.
func (s *Service) registeredClosure(ctx context.Context, tx runtime.Tx, target v.ContentRef, roots []v.ContentRef, subject v.SubjectBinding, purpose string, actions []string) ([]v.ContentRef, error) {
	refs := map[string]v.ContentRef{}
	active := map[string]bool{}
	targetID, _, err := VersionIdentity(target)
	if err != nil {
		return nil, err
	}
	var visit func(v.ContentRef) error
	visit = func(ref v.ContentRef) error {
		if ref.Owner.TenantID != s.config.Owner.TenantID {
			return refusal("forbidden")
		}
		if ref.Owner != s.config.Owner {
			return refusal("unsupported")
		}
		id, _, err := VersionIdentity(ref)
		if err != nil {
			return err
		}
		if id == targetID || active[id] {
			return refusal("integrity")
		}
		if old, ok := refs[id]; ok {
			if old != ref {
				return refusal("integrity")
			}
			return nil
		}
		if len(refs) >= 64 {
			return refusal("input_over_limit")
		}
		now, err := s.config.Store.Now(ctx, tx)
		if err != nil {
			return err
		}
		for _, action := range actions {
			p, err := s.config.Store.CheckPolicy(ctx, tx, subject, ref, purpose, action, now)
			if err != nil {
				return err
			}
			if p == nil || p.Ref != ref {
				return refusal("forbidden")
			}
		}
		record, err := s.config.Store.LockVersion(ctx, tx, ref)
		if err != nil {
			return err
		}
		if record == nil || record.Ref != ref || record.Publication != "published" {
			return refusal("source_unavailable")
		}
		now, err = s.config.Store.Now(ctx, tx)
		if err != nil {
			return err
		}
		if len(actions) > 0 && !now.Before(cutoff(record.CurrentRetainUntil)) {
			return refusal("expired")
		}
		refs[id] = ref
		active[id] = true
		children := append([]v.ContentRef{}, record.Sources...)
		sortRefs(children)
		for _, child := range children {
			if err := visit(child); err != nil {
				return err
			}
		}
		delete(active, id)
		return nil
	}
	roots = append([]v.ContentRef{}, roots...)
	sortRefs(roots)
	for _, root := range roots {
		if err := visit(root); err != nil {
			return nil, err
		}
	}
	out := make([]v.ContentRef, 0, len(refs))
	for _, ref := range refs {
		out = append(out, ref)
	}
	sortRefs(out)
	return out, nil
}
func sortRefs(refs []v.ContentRef) {
	sort.Slice(refs, func(i, j int) bool {
		a, _, _ := VersionIdentity(refs[i])
		b, _, _ := VersionIdentity(refs[j])
		return a < b
	})
}
func qualificationCode(err error) (v.ErrorCode, bool) {
	e, ok := err.(*v.ContractError)
	if ok {
		return e.Code, true
	}
	return "", false
}

// AuthorizeUse is a trusted online qualification seam. It returns no reusable
// permit and performs no transfer; each actual operation must check again.
func (s *Service) AuthorizeUse(ctx context.Context, subject *v.SubjectBinding, ref v.ContentRef, purpose, action string) error {
	if action != "read" && action != "process" && action != "save" && action != "sync" && action != "disclose" {
		return refusal("unsupported")
	}
	principal, ok := trustedSubject(subject, s.config.Owner)
	if !ok || ref.Owner != s.config.Owner {
		return refusal("forbidden")
	}
	if !finite(ctx) {
		return ErrUnavailable
	}
	return s.config.Store.Within(ctx, owner(s.config.Owner), func(ctx context.Context, tx runtime.Tx) error {
		now, err := s.config.Store.Now(ctx, tx)
		if err != nil {
			return err
		}
		p, err := s.config.Store.CheckPolicy(ctx, tx, principal, ref, purpose, action, now)
		if err != nil {
			return err
		}
		if p == nil {
			return refusal("forbidden")
		}
		record, err := s.config.Store.LockVersion(ctx, tx, ref)
		if err != nil {
			return err
		}
		if record != nil && p.Ref != record.Ref || record == nil && p.Ref != ref {
			return refusal("forbidden")
		}
		if record == nil || record.Ref != ref || record.Publication != "published" {
			return refusal("source_unavailable")
		}
		refs, err := s.registeredClosure(ctx, tx, ref, record.Sources, principal, purpose, []string{action})
		if err != nil {
			return err
		}
		bound := earlier(earlier(p.ValidUntil, p.RetainUntil), cutoff(record.CurrentRetainUntil))
		for _, source := range refs {
			policy, err := s.config.Store.CheckPolicy(ctx, tx, principal, source, purpose, action, now)
			if err != nil {
				return err
			}
			if policy == nil || policy.Ref != source {
				return refusal("forbidden")
			}
			r, err := s.config.Store.LockVersion(ctx, tx, source)
			if err != nil {
				return err
			}
			if r == nil {
				return refusal("source_unavailable")
			}
			bound = earlier(bound, earlier(earlier(policy.ValidUntil, policy.RetainUntil), cutoff(r.CurrentRetainUntil)))
		}
		now, err = s.config.Store.Now(ctx, tx)
		if err != nil {
			return err
		}
		if !now.Before(bound) {
			return refusal("expired")
		}
		return nil
	})
}
