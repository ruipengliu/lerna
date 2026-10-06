package simulator

import (
	"net"
	"net/url"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// CheckRecoverySupported 核验完整固定声明与原绑定，不重编、续期或改写历史。
func (Adapter) CheckRecoverySupported(op *v1.Operation) error {
	cap := op.GetCapabilitySnapshot()
	if cap == nil || cap.Ref == nil || cap.AdapterRef.GetName() == nil || cap.AdapterRef.Revision != 1 || !proto.Equal(cap.AdapterRef, op.AdapterRef) || op.ParametersRef == nil || cap.ExecutorEndpointId != op.ExecutorEndpointId {
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
	if e := command.CheckSavedHeaders(op); e != nil {
		return e
	}
	supported, e := fixedDeclaration(op)
	if e != nil {
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
	target, e := url.Parse(cap.Resource)
	if e != nil || target.Scheme != "http" || target.User != nil || target.Fragment != "" || net.ParseIP(target.Hostname()) == nil || !net.ParseIP(target.Hostname()).IsLoopback() {
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
	if cap.Action != "QUERY" && op.QuerySubject != nil {
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
	if op.Execution == nil {
		return nil
	}
	x := op.Execution
	if x.Attempt == nil || x.Attempt.Ref == nil || x.Attempt.ExternalKey == "" || x.Send == nil || x.CallDescriptor == nil || !proto.Equal(x.Attempt.OperationId, op.Ref.GetName()) || !proto.Equal(x.Send.AttemptId, x.Attempt.Ref.Name) || x.Attempt.ExternalKeyScope != cap.Resource || !proto.Equal(x.Attempt.Capabilities, supported) {
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
	for _, send := range x.PreviousSends {
		if send == nil || !proto.Equal(send.AttemptId, x.Attempt.Ref.Name) {
			return command.Fail("PREPARATION_UNRECOVERABLE")
		}
	}
	// 到期只限制新发送，不能阻止读取原历史，也不能在恢复时续期。
	if (supported.RetentionMs > 0) != (x.Attempt.KeyValidUntilUnixMs != nil) {
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
	method, bodyDigest := "POST", ""
	if cap.Action == "QUERY" {
		method = "GET"
	}
	if cap.Action == "MODEL_INFER" {
		bodyDigest = op.ModelDescriptorDigest
	}
	d := x.CallDescriptor
	if d.Protocol != "HTTP" || d.Method != method || d.Target != cap.Resource || !proto.Equal(d.ParametersRef, op.ParametersRef) || !proto.Equal(d.CapabilityRef, cap.Ref) || d.ExternalKey != x.Attempt.ExternalKey || !proto.Equal(d.QuerySubject, op.QuerySubject) || d.BodyDigest != bodyDigest || (d.KeyValidUntilUnixMs == nil) != (x.Attempt.KeyValidUntilUnixMs == nil) || d.GetKeyValidUntilUnixMs() != x.Attempt.GetKeyValidUntilUnixMs() {
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
	descriptor := proto.Clone(d).(*v1.CallDescriptor)
	descriptor.Digest = ""
	if d.Digest == "" || command.SemanticFingerprint("call-descriptor-v1", descriptor) != d.Digest {
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
	return nil
}
