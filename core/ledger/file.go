package ledger

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

func validFileEvidence(raw *v1.RawObservation, op *v1.Operation) bool {
	if raw == nil || op == nil || op.Execution == nil || op.CapabilitySnapshot == nil {
		return false
	}
	cap := op.Execution.Attempt.Capabilities
	send := executionSend(op.Execution, raw.SendRef)
	return send != nil && cap != nil && cap.ProtocolVersion == "lerna-managed-file-v1" && cap.VerificationBasis == "managed-file-v1" && op.CapabilitySnapshot.AdapterRef.GetName().GetLocalId() == "managed-file" && op.CapabilitySnapshot.AdapterRef.Revision == 1 && raw.Protocol == "FILE" && raw.Source == "TRUSTED_IO" && raw.FileEvidence != nil && raw.FileEvidence.Rule == "managed-file-v1" && raw.Target == op.CapabilitySnapshot.Resource && proto.Equal(raw.OperationId, op.Ref.Name) && proto.Equal(raw.AttemptId, op.Execution.Attempt.Ref.Name) && raw.ExternalKey == op.Execution.Attempt.ExternalKey && proto.Equal(raw.SendRef.Name, send.Ref.Name)
}
func fileCommitMatches(c *v1.FileCommit, op *v1.Operation) bool {
	return c != nil && c.Version != "" && c.Digest != "" && c.ObjectName != "" && c.ContentRef != nil && c.ResourcesRef != nil && c.RootIdentity != "" && c.Target == op.CapabilitySnapshot.Resource && proto.Equal(c.OperationId, op.Ref.Name) && proto.Equal(c.AttemptId, op.Execution.Attempt.Ref.Name) && c.ExternalKey == op.Execution.Attempt.ExternalKey
}
func interpretFile(raw *v1.RawObservation, op *v1.Operation) *v1.EffectInterpretation {
	r := &v1.EffectInterpretation{ObservationRef: raw.Ref, Rule: "managed-file-v1", Outcome: "UNKNOWN", LateEffect: "MAY_OCCUR", Reason: "INSUFFICIENT_EVIDENCE"}
	if !validFileEvidence(raw, op) {
		return r
	}
	e := raw.FileEvidence
	if !e.Terminal {
		return r
	}
	r.LateEffect = "RULED_OUT"
	if e.NegativeProof && !e.Published {
		r.Outcome = "NOT_APPLIED"
		r.Reason = e.ErrorCode
		return r
	}
	if op.Execution.CallDescriptor.Method == "READ" && e.ReadTerminal && e.ReadbackVerified && e.ErrorCode == "" {
		r.Outcome = "APPLIED"
		r.Reason = "FILE_READ_VERIFIED"
		return r
	}
	if op.Execution.CallDescriptor.Method == "CLEANUP" && e.Stage == "CLEANED" && e.DurabilityConfirmed && e.ReadbackVerified && e.ErrorCode == "" {
		r.Outcome = "APPLIED"
		r.Reason = "FILE_CLEANUP_VERIFIED"
		return r
	}
	if e.Published && e.DurabilityConfirmed && e.ReadbackVerified && e.ErrorCode == "" && fileCommitMatches(e.Commit, op) && proto.Equal(e.Commit.SendRef.Name, raw.SendRef.Name) && e.Commit.RootIdentity == raw.ActualAddress {
		r.Outcome = "APPLIED"
		r.Reason = "FILE_DURABLE_READBACK"
	}
	return r
}

// QueryFilePublication 只读执行账本已接纳的可归因观察；不可变对象存在不在此判据内。
func (s *Service) QueryFilePublication(ctx context.Context, caller *v1.Caller, c *v1.FileCommit) (*v1.RawObservation, error) {
	if caller.GetIssuerId() != "egress-io" || c == nil || c.OperationId == nil || c.AttemptId == nil {
		return nil, command.Fail("PERMISSION_DENIED")
	}
	op, e := s.QueryOperation(ctx, caller, c.OperationId)
	if e != nil {
		return nil, e
	}
	if op == nil || op.Execution == nil || op.CapabilitySnapshot.Resource != c.Target || !proto.Equal(op.Execution.Attempt.Ref.Name, c.AttemptId) || op.Execution.Attempt.ExternalKey != c.ExternalKey {
		return nil, command.Fail("FILE_COMMIT_INVALID")
	}
	for i := len(op.Effect.EvidenceRefs) - 1; i >= 0; i-- {
		raw, e := s.QueryObservation(ctx, caller, op.Effect.EvidenceRefs[i])
		if e != nil {
			return nil, e
		}
		if raw == nil || raw.Source != "TRUSTED_IO" || raw.Protocol != "FILE" || raw.FileEvidence == nil || raw.FileEvidence.Rule != "managed-file-v1" || !raw.FileEvidence.Published || !fileCommitMatches(raw.FileEvidence.Commit, op) {
			continue
		}
		if c.Version != "" && !proto.Equal(c, raw.FileEvidence.Commit) {
			continue
		}
		if proto.Equal(raw.OperationId, op.Ref.Name) {
			if !validFileEvidence(raw, op) {
				continue
			}
		} else {
			if raw.QuerySubject == nil || !proto.Equal(raw.QuerySubject.OperationId, op.Ref.Name) || !proto.Equal(raw.QuerySubject.AttemptId, c.AttemptId) || raw.QuerySubject.ExternalKey != c.ExternalKey || raw.QuerySubject.TargetScope != c.Target {
				continue
			}
		}
		return raw, nil
	}
	return nil, nil
}

func parseFileQuery(raw *v1.RawObservation, op *v1.Operation) *queryResponse {
	if !validFileEvidence(raw, op) || op.QuerySubject == nil || !proto.Equal(raw.QuerySubject, op.QuerySubject) || op.Execution.CallDescriptor.Method != "QUERY" {
		return nil
	}
	e := raw.FileEvidence
	subject := op.QuerySubject
	read, applied, terminal := e.ReadTerminal, false, false
	if e.Published && e.ReadbackVerified && e.ErrorCode == "" && e.Commit != nil && proto.Equal(e.Commit.OperationId, subject.OperationId) && proto.Equal(e.Commit.AttemptId, subject.AttemptId) && e.Commit.ExternalKey == subject.ExternalKey && e.Commit.Target == subject.TargetScope {
		applied = true
		terminal = e.Terminal
	}
	return &queryResponse{Protocol: "lerna-managed-file-v1", QueryExternalKey: raw.ExternalKey, QueryAttemptID: raw.AttemptId.LocalId, QueryOperationID: raw.OperationId.LocalId, ReadTerminal: &read, SubjectExternalKey: subject.ExternalKey, SubjectAttemptID: subject.AttemptId.LocalId, SubjectOperationID: subject.OperationId.LocalId, SubjectScope: subject.TargetScope, Applied: &applied, Terminal: &terminal}
}

// CheckFileCleanup 要求原责任已收尾；当前指针的保留还由同一原生锁内检查。
func (s *Service) CheckFileCleanup(ctx context.Context, caller *v1.Caller, r *v1.FileResources) error {
	if caller.GetIssuerId() != "egress-io" || r == nil {
		return command.Fail("PERMISSION_DENIED")
	}
	op, e := s.QueryOperation(ctx, caller, r.OperationId)
	if e != nil {
		return e
	}
	if op == nil || op.Execution == nil || op.Lifecycle != "SETTLED" || op.Effect.LateEffect != "RULED_OUT" || op.Effect.Outcome == "UNKNOWN" || !proto.Equal(op.Execution.Attempt.Ref.Name, r.AttemptId) || executionSend(op.Execution, r.SendRef) == nil || op.CapabilitySnapshot.Resource != r.Target {
		return command.Fail("FILE_RESOURCE_IN_USE")
	}
	return nil
}
