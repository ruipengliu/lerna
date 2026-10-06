package grants

import (
	"context"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

type TraceSource interface {
	SaveTraceSource(context.Context, string, *v1.TraceEvent) error
}

func (s *Service) saveGrant(ctx context.Context, v *v1.Grant) error {
	if err := s.store.SaveGrant(ctx, v); err != nil {
		return err
	}
	source := s.store
	return source.SaveTraceSource(ctx, "grants", &v1.TraceEvent{EventType: "GRANT_CHANGED", SourceRecordRef: v.Ref, TaskId: v.Subject, RelatedRefs: []*v1.Ref{v.RevocationRef}})
}

func (s *Service) saveGrantUse(ctx context.Context, v *v1.GrantUse) error {
	if err := s.store.SaveGrantUse(ctx, v); err != nil {
		return err
	}
	source := s.store
	return source.SaveTraceSource(ctx, "grants", &v1.TraceEvent{EventType: "GRANT_USED", SourceRecordRef: v.Ref, TaskId: v.TaskId, OperationId: v.OperationId, RelatedRefs: []*v1.Ref{v.GrantRef, v.AdmissionRef}})
}

func (s *Service) saveGrantRevocation(ctx context.Context, v *v1.GrantRevocation) error {
	if err := s.store.SaveGrantRevocation(ctx, v); err != nil {
		return err
	}
	g, err := s.store.LoadGrant(ctx, v.GrantRef)
	if err != nil {
		return err
	}
	refs := []*v1.Ref{v.GrantRef}
	for _, closure := range v.Closures {
		refs = append(refs, closure.Command.CredentialRef)
		if closure.RecipientReceipt != nil {
			refs = append(refs, closure.RecipientReceipt.ResultRef)
		}
	}
	return s.store.SaveTraceSource(ctx, "grants", &v1.TraceEvent{EventType: "GRANT_REVOKED", SourceRecordRef: v.Ref, TaskId: g.Subject, OriginCommand: v.AcceptedBy, RelatedRefs: refs})
}
