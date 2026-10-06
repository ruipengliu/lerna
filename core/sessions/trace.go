package sessions

import (
	"context"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

type TraceSource interface {
	SaveTraceSource(context.Context, string, *v1.TraceEvent) error
}

func (s *Service) saveInputDelivery(ctx context.Context, v *v1.InputDelivery) error {
	if err := s.store.(DeliveryStore).SaveInputDelivery(ctx, v); err != nil {
		return err
	}
	source := s.store
	return source.SaveTraceSource(ctx, "sessions", &v1.TraceEvent{EventType: "INPUT_RECORDED", SourceRecordRef: v.Ref, TaskId: v.Input.TaskId, OriginCommand: v.Input.CommandIdentity, RelatedRefs: []*v1.Ref{v.Input.ContentRef, v.Input.RequestRef, v.Input.ConfirmationRef}})
}
