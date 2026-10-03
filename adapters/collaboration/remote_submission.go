package collaboration

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// IsDelegationSubmission只辨认真实已创建Child的原命令，不替代本人目标或来源许可。
// Foreign原Command不是本方Session Submission；其它引用不能因此跳过History资格。
func (r *Remote) IsDelegationSubmission(ctx context.Context, scope runtime.Scope, actual api.Task, source api.SourceEvidence) (bool, error) {
	if err := r.checkScope(scope); err != nil {
		return false, err
	}
	if source.SubmissionRef == nil || source.SubmissionRef.OwnerID == scope.OwnerID {
		return false, nil
	}
	if actual.TenantID != scope.TenantID || actual.OrchestratorID != scope.OwnerID {
		return false, api.E("forbidden", "remote_submission_task_scope_mismatch")
	}
	var origin remoteChildOrigin
	if _, err := r.cfg.Store.Read(ctx, scope, "collaboration.remote_children", actual.TaskID, 1, &origin); err != nil {
		if api.IsCode(err, "not_found") {
			return false, nil
		}
		return false, err
	}
	var original remoteReceived
	if _, err := r.cfg.Store.Read(ctx, scope, remoteIncoming, origin.PacketID, 1, &original); err != nil {
		return false, err
	}
	if original.Packet.ChildTaskID != actual.TaskID || packetID(original.Packet) != origin.PacketID {
		return false, api.E("idempotency_conflict", "original_remote_submission_changed")
	}
	if _, err := r.validatePacket(original.Packet); err != nil {
		return false, err
	}
	return *source.SubmissionRef == original.Packet.OriginalCommandRef && source.ContentRef == original.Packet.Input.GoalRef && source.SourceKind == "user_input" && source.Locator == "", nil
}
