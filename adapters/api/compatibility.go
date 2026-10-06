package api

import (
	"encoding/hex"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// CheckRecoverySupported 只读原固定参考协议，不编译、读取内容、解析凭据或重新授权。
func (Adapter) CheckRecoverySupported(op *v1.Operation) error {
	cap := op.GetCapabilitySnapshot()
	if cap == nil || op.Ref.GetName() == nil || cap.Ref.GetName() == nil || op.ParametersRef.GetName() == nil || cap.AdapterRef.GetName() == nil || cap.AdapterRef.Name.UserId != op.Ref.Name.UserId || cap.AdapterRef.Name.AuthorityDomainId != "adapter" || cap.AdapterRef.Name.ObjectKind != "adapter" || cap.AdapterRef.Name.LocalId != "api-reference-v1" || cap.AdapterRef.Revision != 1 || cap.AdapterRef.SchemaId != "lerna.v1.Adapter" || !proto.Equal(op.AdapterRef, cap.AdapterRef) || op.ExecutorEndpointId == "" || cap.ExecutorEndpointId != op.ExecutorEndpointId || cap.Ref.Name.UserId != op.Ref.Name.UserId || op.ParametersRef.Name.UserId != op.Ref.Name.UserId {
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
	if e := command.CheckSavedHeaders(op); e != nil {
		return e
	}
	if command.ValidateAPIDescriptor(cap.ApiDescriptor, op.Ref.Name.UserId, cap.Resource) != nil || cap.IdempotencyRetentionMs < 0 || !cap.ApiDescriptor.Idempotent && cap.IdempotencyRetentionMs != 0 {
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
	method := "POST"
	supported := &v1.ExecutionCapabilities{Effect: "ATOMIC_WRITE", ProtocolVersion: "lerna-reference-api-v1", DeclarationVersion: "1", VerificationBasis: "reference-api-v1", IdempotencyScope: cap.Resource, Idempotent: cap.ApiDescriptor.Idempotent, Queryable: cap.ApiDescriptor.Queryable, AccountScope: op.Ref.Name.UserId}
	if cap.ApiDescriptor.Idempotent {
		supported.IdempotencyMechanism = "NATIVE_KEY"
		supported.ConcurrencyGuarantee = "SAME_KEY_ALL_SENDS"
		supported.ParameterBinding = "EXACT_REQUEST"
		supported.RetentionMs = cap.IdempotencyRetentionMs
		supported.RejectsExpiredKeys = true
	}
	switch cap.Action {
	case "CREATE":
		if cap.UseRight != "INVOKE" || op.QuerySubject != nil {
			return command.Fail("PREPARATION_UNRECOVERABLE")
		}
	case "QUERY":
		subject := op.QuerySubject
		if cap.UseRight != "READ" || !cap.ApiDescriptor.Queryable || op.ClosureWorkRef == nil || subject == nil || subject.OperationId == nil || subject.AttemptId == nil || subject.ExternalKey == "" || subject.CapabilityRef == nil || subject.TargetScope != cap.Resource || subject.ExecutorEndpointId != cap.ExecutorEndpointId || subject.OperationId.UserId != op.Ref.Name.UserId || subject.AttemptId.UserId != op.Ref.Name.UserId || subject.CapabilityRef.GetName().GetUserId() != op.Ref.Name.UserId {
			return command.Fail("PREPARATION_UNRECOVERABLE")
		}
		method = "GET"
		// 原编译在 QUERY 分支只改这四项；此前的机制、并发和保留属性必须保留。
		supported.Effect, supported.ProtocolVersion = "READ", "lerna-reference-api-query-v1"
		supported.Idempotent, supported.Queryable = false, false
	default:
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
	if op.Execution == nil {
		return nil
	}
	x := op.Execution
	if x.Attempt.GetRef().GetName() == nil || x.Send.GetRef().GetName() == nil || x.CallDescriptor == nil || !savedExecutionRef(x.Attempt.Ref, op.Ref.Name, "attempt", "lerna.v1.ExecutionAttempt") || !savedExecutionRef(x.Send.Ref, op.Ref.Name, "send", "lerna.v1.PhysicalSend") || !proto.Equal(x.Attempt.OperationId, op.Ref.Name) || x.Attempt.ExternalKey == "" || x.Attempt.ExternalKeyScope != cap.Resource || !proto.Equal(x.Send.AttemptId, x.Attempt.Ref.Name) || !proto.Equal(x.Attempt.Capabilities, supported) {
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
	for _, send := range x.PreviousSends {
		if !savedExecutionRef(send.GetRef(), op.Ref.Name, "send", "lerna.v1.PhysicalSend") || !proto.Equal(send.AttemptId, x.Attempt.Ref.Name) {
			return command.Fail("PREPARATION_UNRECOVERABLE")
		}
	}
	// 到期不影响读取旧历史；原可选性和值均保留，不续期或读取当前时间。
	if (cap.IdempotencyRetentionMs > 0) != (x.Attempt.KeyValidUntilUnixMs != nil) {
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
	d := x.CallDescriptor
	if d.Protocol != "HTTP" || d.Method != method || d.Target != cap.Resource || !proto.Equal(d.ParametersRef, op.ParametersRef) || !proto.Equal(d.CapabilityRef, cap.Ref) || !proto.Equal(d.ApiDescriptor, cap.ApiDescriptor) || d.ExternalKey != x.Attempt.ExternalKey || !proto.Equal(d.QuerySubject, op.QuerySubject) || (d.KeyValidUntilUnixMs == nil) != (x.Attempt.KeyValidUntilUnixMs == nil) || d.GetKeyValidUntilUnixMs() != x.Attempt.GetKeyValidUntilUnixMs() || !savedDigest(d.ParametersDigest) || !savedDigest(d.BodyDigest) {
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
	if method == "GET" && d.BodyDigest != command.BytesDigest(nil) {
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
	original := proto.Clone(d).(*v1.CallDescriptor)
	original.Digest = ""
	if d.Digest == "" || d.Digest != command.SemanticFingerprint("call-descriptor-v1", original) {
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
	return nil
}

func savedDigest(value string) bool {
	b, e := hex.DecodeString(value)
	return e == nil && len(b) == 32
}

// savedExecutionRef 只比较原身份归属及固定消息类型，不创建或替换引用。
func savedExecutionRef(ref *v1.Ref, owner *v1.GlobalName, kind, schema string) bool {
	n := ref.GetName()
	return n != nil && n.UserId == owner.UserId && n.AuthorityDomainId == owner.AuthorityDomainId && n.ObjectKind == kind && n.LocalId != "" && ref.Revision > 0 && ref.SchemaId == schema
}
