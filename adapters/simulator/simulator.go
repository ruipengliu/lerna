// Package simulator 只声明参考协议和编译描述，不持有网络能力或写入效果事实。
package simulator

import (
	"net"
	"net/url"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

type Adapter struct{}

func (Adapter) Compile(op *v1.Operation, attempt *v1.ExecutionAttempt) (*v1.CallDescriptor, *v1.ExecutionCapabilities, error) {
	cap := op.GetCapabilitySnapshot()
	if cap == nil || cap.AdapterRef == nil || cap.AdapterRef.Name == nil || cap.AdapterRef.Revision != 1 || (cap.Action != "CREATE" && cap.Action != "QUERY") || op.ParametersRef == nil || attempt == nil || attempt.ExternalKey == "" {
		return nil, nil, command.Fail("UNSUPPORTED_CAPABILITY")
	}
	declaration := &v1.ExecutionCapabilities{Effect: "ATOMIC_WRITE", ProtocolVersion: "lerna-simulator-v1", DeclarationVersion: "1", VerificationBasis: "reference-target-v1", IdempotencyScope: cap.Resource}
	switch cap.AdapterRef.Name.LocalId {
	case "simulator-idempotent":
		declaration.Idempotent = true
	case "simulator-queryable":
		declaration.Queryable = true
	case "simulator-opaque":
	default:
		return nil, nil, command.Fail("UNSUPPORTED_CAPABILITY")
	}
	target, e := url.Parse(cap.Resource)
	if e != nil || target.Scheme != "http" || target.User != nil || target.Fragment != "" || net.ParseIP(target.Hostname()) == nil || !net.ParseIP(target.Hostname()).IsLoopback() {
		return nil, nil, command.Fail("TARGET_SCOPE_MISMATCH")
	}
	d := &v1.CallDescriptor{Protocol: "HTTP", Method: "POST", Target: cap.Resource, ParametersRef: op.ParametersRef, CapabilityRef: cap.Ref, ExternalKey: attempt.ExternalKey}
	if cap.Action == "QUERY" {
		if cap.AdapterRef.Name.LocalId != "simulator-queryable" || cap.UseRight != "READ" || op.QuerySubject == nil || op.ClosureWorkRef == nil || op.QuerySubject.TargetScope != cap.Resource || op.QuerySubject.ExecutorEndpointId != cap.ExecutorEndpointId {
			return nil, nil, command.Fail("UNSUPPORTED_CAPABILITY")
		}
		d.Method = "GET"
		d.QuerySubject = op.QuerySubject
		declaration.Effect = "READ"
		declaration.ProtocolVersion = "lerna-simulator-query-v1"
		declaration.VerificationBasis = "reference-query-v1"
		declaration.Queryable = false
	}
	d.Digest = command.SemanticFingerprint("call-descriptor-v1", d)
	return d, declaration, nil
}
