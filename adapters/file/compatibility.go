package file

import (
	"net/url"
	"strings"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// CheckRecoverySupported 只核验原文件协议和封存绑定，不读文件、不重编或升级声明。
func (Adapter) CheckRecoverySupported(op *v1.Operation) error {
	cap := op.GetCapabilitySnapshot()
	if cap == nil || cap.AdapterRef.GetName().GetLocalId() != "managed-file" || cap.AdapterRef.Revision != 1 || !proto.Equal(cap.AdapterRef, op.AdapterRef) || op.ParametersRef == nil || cap.Ref == nil || cap.ExecutorEndpointId != "local-file" || op.ExecutorEndpointId != cap.ExecutorEndpointId || !cap.Nonbillable || cap.FeeCeiling == nil || cap.GetFeeCeiling() != 0 {
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
	if err := command.CheckSavedHeaders(op); err != nil {
		return err
	}
	effect, queryable, right := "ATOMIC_WRITE", true, "INVOKE"
	switch cap.Action {
	case "CREATE", "REPLACE":
	case "READ", "QUERY":
		effect, queryable, right = "READ", false, "READ"
	case "CLEANUP":
		effect, queryable = "PARTIAL_WRITE", false
	default:
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
	if cap.UseRight != right {
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
	u, err := url.Parse(cap.Resource)
	if err != nil || u.Scheme != "managed" || u.User != nil || u.Host == "" || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || !simpleName(u.Host) || !simpleName(strings.TrimPrefix(u.Path, "/")) {
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
	if cap.Action == "QUERY" {
		if op.QuerySubject == nil || op.ClosureWorkRef == nil || op.QuerySubject.TargetScope != cap.Resource || op.QuerySubject.ExecutorEndpointId != cap.ExecutorEndpointId {
			return command.Fail("PREPARATION_UNRECOVERABLE")
		}
	} else if op.QuerySubject != nil {
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
	if op.Execution == nil {
		return nil
	}
	x := op.Execution
	if x.Attempt == nil || x.Attempt.Ref == nil || x.Attempt.ExternalKey == "" || x.Send == nil || x.CallDescriptor == nil || !proto.Equal(x.Attempt.OperationId, op.Ref.GetName()) || !proto.Equal(x.Send.AttemptId, x.Attempt.Ref.Name) || x.Attempt.ExternalKeyScope != cap.Resource {
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
	// 完整比较固定版本的声明，未知幂等、并发、保留期或账户保证不能被静默忽略。
	supported := &v1.ExecutionCapabilities{Effect: effect, Queryable: queryable, ProtocolVersion: "lerna-managed-file-v1", DeclarationVersion: "1", VerificationBasis: "managed-file-v1"}
	if !proto.Equal(x.Attempt.Capabilities, supported) {
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
	d := x.CallDescriptor
	if d.Protocol != "FILE" || d.Method != cap.Action || d.Target != cap.Resource || !proto.Equal(d.ParametersRef, op.ParametersRef) || !proto.Equal(d.CapabilityRef, cap.Ref) || d.ExternalKey != x.Attempt.ExternalKey || !proto.Equal(d.QuerySubject, op.QuerySubject) || d.BodyDigest != "" || d.KeyValidUntilUnixMs != nil {
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
	saved := proto.Clone(d).(*v1.CallDescriptor)
	saved.Digest = ""
	if d.Digest == "" || command.SemanticFingerprint("call-descriptor-v1", saved) != d.Digest {
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
	return nil
}
