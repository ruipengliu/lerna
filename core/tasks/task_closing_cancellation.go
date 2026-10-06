package tasks

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// checkClosingCancellation 核验原停止责任；当前关闭授权由新的可信 Begin 独立保存。
func (s *Service) checkClosingCancellation(ctx context.Context, caller *v1.Caller, task *v1.Task, ref *v1.Ref, admissions []*v1.Ref, requireAcknowledgements bool) (bool, error) {
	scope, e := s.QueryCancellation(ctx, caller, task.TaskId)
	if e != nil {
		return false, e
	}
	if ref == nil || scope == nil || !proto.Equal(scope.Ref, ref) || ref.SchemaId != "lerna.v1.Cancellation" || ref.Revision != 1 || !proto.Equal(scope.TaskId, task.TaskId) || scope.ControlIdentity == nil || scope.ControlGeneration == 0 || scope.ControlGeneration > task.ControlGeneration || task.Control != v1.TaskControl_TASK_CONTROL_CANCELLING || len(scope.AdmissionRefs) != len(scope.ClosureIntentRefs) {
		return false, command.Fail("INVALID_CLOSE_BASIS")
	}
	ready := true
	seen := map[string]bool{}
	for i, admission := range scope.AdmissionRefs {
		included := false
		for _, current := range admissions {
			if proto.Equal(current, admission) {
				included = true
			}
		}
		if !included || admission.GetName() == nil || seen[admission.Name.LocalId] {
			return false, command.Fail("INVALID_CLOSE_BASIS")
		}
		seen[admission.Name.LocalId] = true
		intent, e := s.QueryCancellationIntent(ctx, caller, scope.ClosureIntentRefs[i])
		if e != nil {
			return false, e
		}
		if intent == nil || intent.Command == nil || !proto.Equal(intent.TaskId, task.TaskId) || !proto.Equal(intent.Command.AdmissionRef, admission) || !proto.Equal(intent.Command.CancellationRef, ref) {
			return false, command.Fail("INVALID_CLOSE_BASIS")
		}
		if e = command.ValidateHeader(intent.Command.Header, intent.Command); e != nil {
			return false, e
		}
		if _, e = s.ValidateCancellationClosure(ctx, &v1.Caller{UserId: s.user, IssuerId: "tasks-cancellation"}, intent.Command); e != nil {
			return false, e
		}
		if !requireAcknowledgements {
			continue
		}
		r := intent.RecipientReceipt
		if r == nil {
			ready = false
			continue
		}
		c := intent.Command
		if r.Decision != v1.Decision_DECISION_ACCEPTED || r.Phase != v1.ReceiptPhase_RECEIPT_PHASE_DECIDED || !proto.Equal(r.Identity, c.Header.Identity) || r.ResponsibleDomainId != c.OperationId.AuthorityDomainId || r.Fingerprint != command.SemanticFingerprint("cancellation-seal", c) || r.ResultRef.GetSchemaId() != "lerna.v1.CancellationSeal" {
			return false, command.Fail("HANDOFF_RECEIPT_INVALID")
		}
		seal, e := s.taskCloseFacts.QueryCancellationSeal(ctx, caller, r.ResultRef)
		if e != nil {
			return false, e
		}
		if seal == nil || !proto.Equal(seal.Ref, r.ResultRef) || !proto.Equal(seal.IntentRef, intent.Ref) || !proto.Equal(seal.CancellationRef, ref) || !proto.Equal(seal.AdmissionRef, admission) || !proto.Equal(seal.OperationId, c.OperationId) || seal.ExecutorEndpointId != c.ExecutorEndpointId || seal.NoSendProven && seal.PhysicalSendWasPossible {
			return false, command.Fail("INVALID_CLOSURE")
		}
	}
	return ready, nil
}
