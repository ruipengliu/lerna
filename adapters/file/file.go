// Package file 编译固定受管理文件协议，不持有文件描述符或效果裁决权。
package file

import (
	"net/url"
	"strings"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

type Adapter struct{}

func (Adapter) Compile(op *v1.Operation, attempt *v1.ExecutionAttempt) (*v1.CallDescriptor, *v1.ExecutionCapabilities, error) {
	cap := op.GetCapabilitySnapshot()
	if cap == nil || cap.AdapterRef.GetName().GetLocalId() != "managed-file" || cap.AdapterRef.Revision != 1 || op.ParametersRef == nil || attempt == nil || attempt.ExternalKey == "" {
		return nil, nil, command.Fail("UNSUPPORTED_CAPABILITY")
	}
	if !cap.Nonbillable || cap.FeeCeiling == nil || cap.GetFeeCeiling() != 0 || cap.ExecutorEndpointId != "local-file" {
		return nil, nil, command.Fail("UNSUPPORTED_CAPABILITY")
	}
	if (cap.Action == "READ" || cap.Action == "QUERY") && cap.UseRight != "READ" || (cap.Action != "READ" && cap.Action != "QUERY") && cap.UseRight != "INVOKE" {
		return nil, nil, command.Fail("UNSUPPORTED_CAPABILITY")
	}
	if cap.Action != "CREATE" && cap.Action != "REPLACE" && cap.Action != "READ" && cap.Action != "QUERY" && cap.Action != "CLEANUP" {
		return nil, nil, command.Fail("UNSUPPORTED_CAPABILITY")
	}
	u, err := url.Parse(cap.Resource)
	if err != nil || u.Scheme != "managed" || u.User != nil || u.Host == "" || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || !simpleName(u.Host) || !simpleName(strings.TrimPrefix(u.Path, "/")) {
		return nil, nil, command.Fail("TARGET_SCOPE_MISMATCH")
	}
	d := &v1.CallDescriptor{Protocol: "FILE", Method: cap.Action, Target: cap.Resource, ParametersRef: op.ParametersRef, CapabilityRef: cap.Ref, ExternalKey: attempt.ExternalKey}
	declaration := &v1.ExecutionCapabilities{Effect: "ATOMIC_WRITE", Queryable: true, ProtocolVersion: "lerna-managed-file-v1", DeclarationVersion: "1", VerificationBasis: "managed-file-v1"}
	if cap.Action == "READ" || cap.Action == "QUERY" {
		declaration.Effect = "READ"
		declaration.Queryable = false
	}
	if cap.Action == "CLEANUP" {
		declaration.Effect = "PARTIAL_WRITE"
		declaration.Queryable = false
	}
	if cap.Action == "QUERY" {
		if cap.UseRight != "READ" || op.QuerySubject == nil || op.ClosureWorkRef == nil || op.QuerySubject.TargetScope != cap.Resource || op.QuerySubject.ExecutorEndpointId != cap.ExecutorEndpointId {
			return nil, nil, command.Fail("UNSUPPORTED_CAPABILITY")
		}
		d.QuerySubject = op.QuerySubject
	}
	d.Digest = command.SemanticFingerprint("call-descriptor-v1", d)
	return d, declaration, nil
}

func simpleName(s string) bool {
	if s == "" || strings.HasPrefix(s, ".") || len(s) > 128 {
		return false
	}
	for _, c := range s {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' && c != '_' && c != '.' {
			return false
		}
	}
	return true
}
