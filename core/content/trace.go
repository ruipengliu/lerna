package content

import (
	"context"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

// TraceSource 将内容关联与原负责方事实一同保存，不参加运行记录域事务。
type TraceSource interface {
	SaveTraceSource(context.Context, string, *v1.TraceEvent) error
}

func (s *Service) saveRegistration(ctx context.Context, r *v1.ContentRegistration) error {
	if err := s.store.SaveContentRegistration(ctx, r); err != nil {
		return err
	}
	v := r.Content
	refs := []*v1.Ref{v.ProducerRef, v.LocationRef, r.StagingLocationRef}
	refs = append(refs, v.DerivedFrom...)
	refs = append(refs, v.PreviousVersionRefs...)
	return s.store.SaveTraceSource(ctx, "content", &v1.TraceEvent{EventType: "CONTENT_" + r.State, SourceRecordRef: v.Ref, TaskId: v.TaskId, OperationId: v.OperationId, AttemptId: v.AttemptId, BodyRef: v.Ref, OriginCommand: r.Identity, RelatedRefs: refs})
}

func (s *Service) saveDerivation(ctx context.Context, v *v1.ContentDerivation) error {
	if err := s.store.SaveContentDerivation(ctx, v); err != nil {
		return err
	}
	refs := []*v1.Ref{v.OutputRef, v.LocationRef, v.PreviousVersionRef}
	refs = append(refs, v.ActualInputRefs...)
	refs = append(refs, v.SealedInputRefs...)
	return s.store.SaveTraceSource(ctx, "content", &v1.TraceEvent{EventType: "DERIVATION_" + v.State, SourceRecordRef: v.Ref, TaskId: v.TaskId, OperationId: v.OperationId, AttemptId: v.AttemptId, OriginCommand: v.Responsibility, RelatedRefs: refs})
}
