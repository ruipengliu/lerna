package tasks

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

func uniqueRefs(refs []*v1.Ref) []*v1.Ref {
	out := make([]*v1.Ref, 0, len(refs))
	for _, ref := range refs {
		found := false
		for _, old := range out {
			if proto.Equal(old, ref) {
				found = true
				break
			}
		}
		if !found {
			out = append(out, ref)
		}
	}
	return out
}

// completeModelSnapshot 固定必要条件和进展依据；缺少执行回报明确显示为待纳入，不推断没有发生。
func (s *Service) completeModelSnapshot(ctx context.Context, caller *v1.Caller, snap *v1.ContextSnapshot, t *v1.Task, inputs *v1.TaskInputHistory) error {
	snap.BoundInputVersion = t.BoundInputVersion
	snap.RequirementsStatus = t.RequirementsStatus
	snap.MaxModelPositions = 1
	snap.MaxModelSends = 1
	snap.AllowedPurposes = []string{"PLAN"}
	if t.RequirementsStatus != v1.RequirementsStatus_REQUIREMENTS_STATUS_ACCEPTED || t.BoundInputVersion != t.InputVersion {
		snap.AllowedPurposes = []string{"INTERPRET_INPUT"}
	}
	for _, input := range inputs.Inputs {
		if input.InputVersion > t.BoundInputVersion {
			snap.UnprocessedInputs = append(snap.UnprocessedInputs, &v1.SnapshotInput{InputRef: input.InputRef, ContentRef: input.ContentRef, InputVersion: input.InputVersion, ProcessingStatus: input.ProcessingStatus, ChangesBasis: input.ChangesBasis})
		}
	}
	if snap.RequirementsRef != nil {
		requirements, e := s.store.LoadRequirements(ctx, snap.RequirementsRef)
		if e != nil {
			return e
		}
		if requirements == nil {
			return command.Fail("INVARIANT_VIOLATION")
		}
		for _, r := range requirements.Conditions {
			snap.ContentRefs = append(snap.ContentRefs, r.DescriptionRef)
			if r.TargetRecord != nil {
				snap.ContentRefs = append(snap.ContentRefs, r.TargetRecord.ParametersRef)
			}
		}
	}
	for _, ref := range snap.ProgressRefs {
		a, e := s.store.LoadAdmission(ctx, ref)
		if e != nil {
			return e
		}
		if a == nil {
			return command.Fail("INVARIANT_VIOLATION")
		}
		fact := &v1.SnapshotProgress{AdmissionRef: ref, OperationRef: &v1.Ref{Name: a.OperationId, Revision: 1, SchemaId: "lerna.v1.Operation"}, CapabilityRef: a.CapabilityRef, ParametersRef: a.ParametersRef, EffectOutcome: "UNKNOWN", LateEffect: "MAY_OCCUR", ExecutionReportPending: true}
		snap.ContentRefs = append(snap.ContentRefs, a.ParametersRef)
		if s.modelLedger == nil {
			return command.Fail("EXECUTION_FACTS_UNAVAILABLE")
		}
		op, e := s.modelLedger.QueryOperation(ctx, caller, a.OperationId)
		if e != nil {
			return e
		}
		if op != nil {
			fact.OperationRef = op.Ref
			fact.Dispatch = op.Dispatch
			fact.Lifecycle = op.Lifecycle
			if op.Effect != nil {
				fact.EffectRef = op.Effect.Ref
				fact.EffectOutcome = op.Effect.Outcome
				fact.LateEffect = op.Effect.LateEffect
				fact.EvidenceConflict = op.Effect.EvidenceConflict
				fact.EvidenceRefs = op.Effect.EvidenceRefs
			}
			fact.ExecutionReportPending = op.Execution != nil && op.Execution.Send.Phase == "DISPATCH_POSSIBLE"
			for _, evidence := range fact.EvidenceRefs {
				raw, e := s.modelLedger.QueryObservation(ctx, caller, evidence)
				if e != nil {
					return e
				}
				if raw == nil || !proto.Equal(raw.TaskId, t.TaskId) {
					return command.Fail("INVARIANT_VIOLATION")
				}
				if !proto.Equal(raw.OperationId, a.OperationId) {
					// 核对观察保留独立查询身份；原动作的已接纳证据引用只提供关联。
					query, err := s.modelLedger.QueryOperation(ctx, caller, raw.OperationId)
					if err != nil {
						return err
					}
					subject := raw.QuerySubject
					if query == nil || query.ClosureWorkRef == nil || subject == nil || !proto.Equal(query.QuerySubject, subject) || !proto.Equal(subject.OperationId, a.OperationId) || op.Execution == nil || !proto.Equal(subject.AttemptId, op.Execution.Attempt.Ref.Name) || subject.ExternalKey != op.Execution.Attempt.ExternalKey || subject.TargetScope != op.CapabilitySnapshot.Resource || subject.ExecutorEndpointId != op.ExecutorEndpointId || !proto.Equal(subject.CapabilityRef, op.CapabilitySnapshot.Ref) {
						return command.Fail("INVARIANT_VIOLATION")
					}
				}
				snap.ContentRefs = append(snap.ContentRefs, raw.BodyRef)
			}
		}
		snap.ProgressFacts = append(snap.ProgressFacts, fact)
	}
	snap.ContentRefs = uniqueRefs(snap.ContentRefs)
	return nil
}
