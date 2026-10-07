package command

import (
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// ManagedFileObservationMatches 只比较原观察、固定协议与保存的发送绑定，不裁决发布终局。
func ManagedFileObservationMatches(raw *v1.RawObservation, op *v1.Operation) bool {
	if raw == nil || op == nil || op.Execution == nil || op.Execution.Attempt == nil || op.CapabilitySnapshot == nil || raw.GetSendRef().GetName() == nil {
		return false
	}
	cap := op.Execution.Attempt.Capabilities
	boundSend := false
	for _, send := range append([]*v1.PhysicalSend{op.Execution.Send}, op.Execution.PreviousSends...) {
		if send != nil && proto.Equal(raw.SendRef.Name, send.GetRef().GetName()) {
			boundSend = true
			break
		}
	}
	return boundSend && cap != nil && cap.ProtocolVersion == "lerna-managed-file-v1" && cap.VerificationBasis == "managed-file-v1" && op.CapabilitySnapshot.AdapterRef.GetName().GetLocalId() == "managed-file" && op.CapabilitySnapshot.AdapterRef.Revision == 1 && raw.Protocol == "FILE" && raw.Source == "TRUSTED_IO" && raw.FileEvidence != nil && raw.FileEvidence.Rule == "managed-file-v1" && raw.Target == op.CapabilitySnapshot.Resource && proto.Equal(raw.OperationId, op.Ref.GetName()) && proto.Equal(raw.AttemptId, op.Execution.Attempt.Ref.GetName()) && raw.ExternalKey == op.Execution.Attempt.ExternalKey
}

// ManagedFileCommitMatches 只核验原提交的身份和版本引用，不以对象存在或内容相同推断发布。
func ManagedFileCommitMatches(c *v1.FileCommit, op *v1.Operation) bool {
	if op == nil || op.CapabilitySnapshot == nil || op.Execution == nil || op.Execution.Attempt == nil {
		return false
	}
	return c != nil && c.Version != "" && c.Digest != "" && c.ObjectName != "" && c.ContentRef != nil && c.ResourcesRef != nil && c.RootIdentity != "" && c.Target == op.CapabilitySnapshot.Resource && proto.Equal(c.OperationId, op.Ref.Name) && proto.Equal(c.AttemptId, op.Execution.Attempt.Ref.Name) && c.ExternalKey == op.Execution.Attempt.ExternalKey
}
