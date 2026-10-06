// Package api 编译固定版本的参考协议，不持有网络或凭据解析能力。
package api

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type Content interface {
	Read(context.Context, *v1.Caller, *v1.Ref) (*v1.Content, error)
}
type Adapter struct{ Content Content }

func (a Adapter) Compile(op *v1.Operation, attempt *v1.ExecutionAttempt) (*v1.CallDescriptor, *v1.ExecutionCapabilities, error) {
	return a.CompileContext(context.Background(), op, attempt)
}
func (a Adapter) CompileContext(ctx context.Context, op *v1.Operation, attempt *v1.ExecutionAttempt) (*v1.CallDescriptor, *v1.ExecutionCapabilities, error) {
	cap := op.GetCapabilitySnapshot()
	if cap == nil || cap.AdapterRef == nil || cap.AdapterRef.Name == nil || cap.AdapterRef.Name.UserId != op.GetRef().GetName().GetUserId() || cap.AdapterRef.Name.AuthorityDomainId != "adapter" || cap.AdapterRef.Name.ObjectKind != "adapter" || cap.AdapterRef.Name.LocalId != "api-reference-v1" || cap.AdapterRef.Revision != 1 || cap.AdapterRef.SchemaId != "lerna.v1.Adapter" || (cap.Action != "CREATE" && cap.Action != "QUERY") || attempt == nil || attempt.ExternalKey == "" || op.ParametersRef == nil || a.Content == nil {
		return nil, nil, command.Fail("UNSUPPORTED_CAPABILITY")
	}
	if e := command.ValidateAPIDescriptor(cap.ApiDescriptor, op.Ref.Name.UserId, cap.Resource); e != nil {
		return nil, nil, e
	}
	body, e := a.Content.Read(ctx, &v1.Caller{UserId: op.Ref.Name.UserId, IssuerId: "host"}, op.ParametersRef)
	if e != nil {
		return nil, nil, e
	}
	if body == nil || body.Status != "AVAILABLE" {
		return nil, nil, command.Fail("CONTENT_UNUSABLE")
	}
	payload := command.ContentBytes(body)
	wire, e := command.CompileAPIParameters(payload)
	if e != nil {
		return nil, nil, e
	}
	d := &v1.CallDescriptor{Protocol: "HTTP", Method: "POST", Target: cap.Resource, ParametersRef: op.ParametersRef, CapabilityRef: cap.Ref, ExternalKey: attempt.ExternalKey, KeyValidUntilUnixMs: attempt.KeyValidUntilUnixMs, ApiDescriptor: proto.Clone(cap.ApiDescriptor).(*v1.ApiDescriptor), ParametersDigest: command.BytesDigest(payload), BodyDigest: command.BytesDigest(wire)}
	declaration := &v1.ExecutionCapabilities{Effect: "ATOMIC_WRITE", ProtocolVersion: cap.ApiDescriptor.ProtocolVersion, DeclarationVersion: cap.ApiDescriptor.Version, VerificationBasis: "reference-api-v1", IdempotencyScope: cap.Resource, Idempotent: cap.ApiDescriptor.Idempotent, Queryable: cap.ApiDescriptor.Queryable, AccountScope: op.Ref.Name.UserId}
	if declaration.Idempotent {
		declaration.IdempotencyMechanism = "NATIVE_KEY"
		declaration.ConcurrencyGuarantee = "SAME_KEY_ALL_SENDS"
		declaration.ParameterBinding = "EXACT_REQUEST"
		declaration.RetentionMs = cap.IdempotencyRetentionMs
		declaration.RejectsExpiredKeys = true
		if cap.IdempotencyRetentionMs < 0 || cap.IdempotencyRetentionMs > 0 && attempt.KeyValidUntilUnixMs == nil {
			return nil, nil, command.Fail("UNSUPPORTED_CAPABILITY")
		}
	} else if cap.IdempotencyRetentionMs != 0 {
		return nil, nil, command.Fail("UNSUPPORTED_CAPABILITY")
	}
	if cap.Action == "QUERY" {
		if cap.UseRight != "READ" || !cap.ApiDescriptor.Queryable || op.QuerySubject == nil || op.ClosureWorkRef == nil || op.QuerySubject.TargetScope != cap.Resource || op.QuerySubject.ExecutorEndpointId != cap.ExecutorEndpointId {
			return nil, nil, command.Fail("UNSUPPORTED_CAPABILITY")
		}
		d.Method = "GET"
		d.QuerySubject = op.QuerySubject
		d.BodyDigest = command.BytesDigest(nil)
		declaration.Effect = "READ"
		declaration.ProtocolVersion = "lerna-reference-api-query-v1"
		declaration.Idempotent = false
		declaration.Queryable = false
	}
	d.Digest = command.SemanticFingerprint("call-descriptor-v1", d)
	return d, declaration, nil
}
