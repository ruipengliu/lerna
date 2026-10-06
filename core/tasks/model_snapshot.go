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
		// 原模型提示按原调用可查；下一轮保留结构事实，不递归嵌入旧提示正文。
		if a.ModelDescriptorDigest == "" {
			snap.ContentRefs = append(snap.ContentRefs, a.ParametersRef)
		}
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
				matches, e := s.snapshotEvidenceMatches(ctx, caller, t, op, raw)
				if e != nil {
					return e
				}
				if !matches {
					return command.Fail("INVARIANT_VIOLATION")
				}
				snap.ContentRefs = append(snap.ContentRefs, raw.BodyRef)
			}
		}
		snap.ProgressFacts = append(snap.ProgressFacts, fact)
	}
	if s.conditionConfirmations != nil {
		var e error
		snap.Confirmations, e = s.conditionConfirmations.QueryTaskConfirmations(ctx, caller, t.TaskId)
		if e != nil {
			return e
		}
		for _, confirmation := range snap.Confirmations {
			snap.ContentRefs = append(snap.ContentRefs, confirmation.EvidenceRefs...)
		}
	}
	for _, progress := range snap.ProgressFacts {
		if progress.OperationRef != nil {
			snap.ProgressWatermarks = append(snap.ProgressWatermarks, progress.OperationRef)
		}
	}
	snap.ContentRefs = uniqueRefs(snap.ContentRefs)
	return nil
}

// snapshotEvidenceMatches 保留独立查询的原身份，通过其封闭准入连接原责任。
func (s *Service) snapshotEvidenceMatches(ctx context.Context, caller *v1.Caller, task *v1.Task, original *v1.Operation, raw *v1.RawObservation) (bool, error) {
	if raw == nil || !proto.Equal(raw.TaskId, task.TaskId) {
		return false, nil
	}
	if proto.Equal(raw.OperationId, original.Ref.Name) {
		return true, nil
	}
	if raw.QuerySubject == nil || original.Execution == nil || original.CapabilitySnapshot == nil || raw.Source != "TRUSTED_IO" {
		return false, nil
	}
	query, e := s.modelLedger.QueryOperation(ctx, caller, raw.OperationId)
	if e != nil {
		return false, e
	}
	if query == nil || query.Execution == nil || query.ClosureWorkRef == nil || query.QuerySubject == nil || query.CapabilitySnapshot == nil || query.CapabilitySnapshot.Action != "QUERY" || !proto.Equal(raw.OperationId, query.Ref.Name) || !proto.Equal(raw.AttemptId, query.Execution.Attempt.GetRef().GetName()) || raw.ExternalKey != query.Execution.Attempt.ExternalKey || !proto.Equal(raw.QuerySubject, query.QuerySubject) {
		return false, nil
	}
	admission, e := s.store.LoadAdmission(ctx, query.AdmissionRef)
	if e != nil {
		return false, e
	}
	if admission == nil || admission.WorkCategory != "CLOSURE" || !proto.Equal(admission.TaskId, task.TaskId) || !proto.Equal(admission.OperationId, query.Ref.Name) || !proto.Equal(admission.Origin, query.ClosureWorkRef) || !proto.Equal(admission.QuerySubject, query.QuerySubject) || !proto.Equal(admission.CapabilityRef, query.CapabilitySnapshot.Ref) {
		return false, nil
	}
	subject := query.QuerySubject
	return proto.Equal(subject.OperationId, original.Ref.Name) && proto.Equal(subject.AttemptId, original.Execution.Attempt.GetRef().GetName()) && subject.ExternalKey == original.Execution.Attempt.ExternalKey && subject.TargetScope == original.CapabilitySnapshot.Resource && subject.ExecutorEndpointId == original.ExecutorEndpointId && proto.Equal(subject.CapabilityRef, original.CapabilitySnapshot.Ref), nil
}
