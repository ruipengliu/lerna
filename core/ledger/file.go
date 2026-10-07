package ledger

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

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
		if raw == nil || raw.Source != "TRUSTED_IO" || raw.Protocol != "FILE" || raw.FileEvidence == nil || raw.FileEvidence.Rule != "managed-file-v1" || !raw.FileEvidence.Published || !command.ManagedFileCommitMatches(raw.FileEvidence.Commit, op) {
			continue
		}
		if c.Version != "" && !proto.Equal(c, raw.FileEvidence.Commit) {
			continue
		}
		if proto.Equal(raw.OperationId, op.Ref.Name) {
			if !command.ManagedFileObservationMatches(raw, op) {
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
