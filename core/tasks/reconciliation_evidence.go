package tasks

import (
	"context"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// queryConditionEvidence 连接原责任与独立读取事实，不把查询观察改标为原写入。
func (s *Service) queryConditionEvidence(ctx context.Context, c *v1.Caller, requirements *v1.Requirements, raw *v1.RawObservation, readProof *v1.EffectInterpretation, original *v1.Operation, admissions map[string]*v1.Admission, ops map[string]*v1.Operation) (string, []*v1.Ref, error) {
	query := ops[raw.GetOperationId().GetLocalId()]
	admission := admissions[raw.GetOperationId().GetLocalId()]
	if query == nil || admission == nil || query.Execution == nil || original.Execution == nil || query.QuerySubject == nil || readProof == nil || readProof.Rule != "reference-query-v1" || readProof.Outcome != "APPLIED" || readProof.LateEffect != "RULED_OUT" || raw.Source != "TRUSTED_IO" || !proto.Equal(raw.TaskId, requirements.TaskId) || !proto.Equal(raw.OperationId, query.Ref.Name) || !proto.Equal(raw.AttemptId, query.Execution.Attempt.Ref.Name) || raw.ExternalKey != query.Execution.Attempt.ExternalKey || !proto.Equal(readProof.ObservationRef, raw.Ref) || !proto.Equal(raw.QuerySubject, query.QuerySubject) || !proto.Equal(admission.QuerySubject, query.QuerySubject) || !proto.Equal(admission.Origin, query.ClosureWorkRef) || admission.WorkCategory != "CLOSURE" {
		return "", nil, nil
	}
	subject := query.QuerySubject
	if !proto.Equal(subject.OperationId, original.Ref.Name) || !proto.Equal(subject.AttemptId, original.Execution.Attempt.Ref.Name) || subject.ExternalKey != original.Execution.Attempt.ExternalKey || subject.TargetScope != original.CapabilitySnapshot.Resource || subject.ExecutorEndpointId != original.ExecutorEndpointId || !proto.Equal(subject.CapabilityRef, original.CapabilitySnapshot.Ref) {
		return "", nil, nil
	}
	for _, ref := range original.ClosureEvidenceRefs {
		if ref.SchemaId != "lerna.v1.ReconciliationFinding" {
			continue
		}
		finding, e := s.completionFacts.QueryReconciliationFinding(ctx, c, ref)
		if e != nil {
			return "", nil, e
		}
		if finding == nil || finding.Rule != "reference-query-subject-v1" || !proto.Equal(finding.OperationId, original.Ref.Name) || !proto.Equal(finding.ObservationRef, raw.Ref) || finding.LateEffect != "RULED_OUT" || (finding.Outcome != "APPLIED" && finding.Outcome != "NOT_APPLIED") || finding.Outcome != original.Effect.Outcome {
			continue
		}
		relation, e := s.completionFacts.QueryReconciliationQuery(ctx, c, finding.QueryRef)
		if e != nil {
			return "", nil, e
		}
		if relation == nil || relation.Work == nil || relation.Work.Purpose != "RECONCILE" || relation.AdmissionReceipt.GetDecision() != v1.Decision_DECISION_ACCEPTED || !proto.Equal(relation.AdmissionReceipt.ResultRef, admission.Ref) || !proto.Equal(relation.QueryOperationRef.GetName(), query.Ref.Name) || !proto.Equal(relation.Work.Ref, admission.Origin) || !proto.Equal(relation.Work.QuerySubject, subject) || !proto.Equal(relation.Work.SourceRef.GetName(), relation.Ref.Name) {
			continue
		}
		if raw.Protocol == "FILE" && finding.Outcome == "APPLIED" && !durableFileCondition(raw) {
			return "", nil, nil
		}
		return finding.Outcome, []*v1.Ref{finding.Ref, relation.Ref, readProof.Ref, raw.Ref, raw.BodyRef}, nil
	}
	return "", nil, nil
}
