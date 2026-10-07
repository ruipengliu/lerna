package egress

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

type fileContent interface {
	PrepareDerivation(context.Context, *v1.Caller, *v1.PrepareDerivationCommand) (*v1.CommandReceipt, error)
	QueryDerivation(context.Context, *v1.Caller, *v1.Ref) (*v1.ContentDerivation, error)
	ReadDerivationInput(context.Context, *v1.Caller, *v1.ReadDerivationInputCommand) (*v1.Content, error)
	SealDerivation(context.Context, *v1.Caller, *v1.SealDerivationCommand) (*v1.CommandReceipt, error)
	CommitDerivation(context.Context, *v1.Caller, *v1.CommitDerivationCommand) (*v1.CommandReceipt, error)
	ProcessRegistrations(context.Context, *v1.Caller) error
	RegisterFileResources(context.Context, *v1.Caller, *v1.RegisterFileResourcesCommand) (*v1.CommandReceipt, error)
	QueryFileResources(context.Context, *v1.Caller, *v1.Ref) (*v1.FileResources, error)
}

// CheckedIO 是可选的受管理文件出口能力，在最终发布点复查当前资格。
// 缺席时 FILE 返回 UNSUPPORTED_CAPABILITY，不调用普通 Perform。
type CheckedIO interface {
	PerformChecked(context.Context, *v1.PhysicalIORequest, func(context.Context) error) (*v1.PhysicalIOResult, error)
}

func (s *Service) prepareFile(ctx context.Context, r *v1.PhysicalIORequest) error {
	p := new(v1.FileParameters)
	if e := protojson.Unmarshal(r.Body, p); e != nil {
		return command.Fail("INVALID_FILE_PARAMETERS")
	}
	if len(p.Data) > 1<<20 {
		return command.Fail("INVALID_FILE_PARAMETERS")
	}
	r.FileParameters = p
	if r.CallDescriptor.Method == "CLEANUP" {
		content := s.content
		actor := &v1.Caller{UserId: r.OperationId.UserId, IssuerId: "egress-io"}
		resources, e := content.QueryFileResources(ctx, actor, p.CleanupResourcesRef)
		if e != nil {
			return e
		}
		if resources == nil || resources.Target != r.CallDescriptor.Target || len(p.Data) > 0 || p.ExpectedVersion != "" {
			return command.Fail("INVALID_FILE_RESOURCES")
		}
		r.FileResources = resources
		r.Body = nil
		return nil
	}
	if r.CallDescriptor.Method != "CREATE" && r.CallDescriptor.Method != "REPLACE" {
		r.Body = nil
		return nil
	}
	if (r.CallDescriptor.Method == "CREATE" && p.ExpectedVersion != "") || (r.CallDescriptor.Method == "REPLACE" && p.ExpectedVersion == "") || p.CleanupResourcesRef != nil {
		return command.Fail("INVALID_FILE_PARAMETERS")
	}
	content := s.content
	host := &v1.Caller{UserId: r.OperationId.UserId, IssuerId: "host"}
	header := func(issuer, step string) *v1.CommandHeader {
		return &v1.CommandHeader{Identity: &v1.CommandIdentity{UserId: host.UserId, IssuerId: issuer, TargetDomainId: r.CallDescriptor.ParametersRef.Name.AuthorityDomainId, CommandId: "file:" + step + ":" + r.Send.Ref.Name.LocalId}, ContractVersion: 1, FingerprintVersion: 1, SchemaId: "lerna.v1.AdmissionCommands"}
	}
	receipt, e := content.PrepareDerivation(ctx, host, &v1.PrepareDerivationCommand{Header: header("host", "derive"), TaskId: r.TaskId, OperationId: r.OperationId, AttemptId: r.Attempt.Ref.Name, GeneratorVersion: "managed-file-v1", OutputKind: "AUXILIARY", MediaType: "application/octet-stream"})
	if e = acceptedFileReceipt(receipt, e); e != nil {
		return e
	}
	derivation, e := content.QueryDerivation(ctx, host, receipt.ResultRef)
	if e != nil {
		return e
	}
	input, e := content.ReadDerivationInput(ctx, host, &v1.ReadDerivationInputCommand{Header: header("host", "input"), DerivationRef: derivation.Ref, HostInstanceId: derivation.HostInstanceId, Generation: derivation.Generation, InputRef: r.CallDescriptor.ParametersRef})
	if e != nil {
		return e
	}
	// 实际计算只使用宿主捕获的精确正文，不能用模型声称的来源列表代替。
	p = new(v1.FileParameters)
	if e = protojson.Unmarshal(command.ContentBytes(input), p); e != nil {
		return command.Fail("INVALID_FILE_PARAMETERS")
	}
	receipt, e = content.SealDerivation(ctx, host, &v1.SealDerivationCommand{Header: header("host", "seal"), DerivationRef: derivation.Ref, HostInstanceId: derivation.HostInstanceId, Generation: derivation.Generation, InputRefs: []*v1.Ref{r.CallDescriptor.ParametersRef}})
	if e = acceptedFileReceipt(receipt, e); e != nil {
		return e
	}
	receipt, e = content.CommitDerivation(ctx, host, &v1.CommitDerivationCommand{Header: header("host", "commit"), DerivationRef: derivation.Ref, HostInstanceId: derivation.HostInstanceId, Generation: derivation.Generation, Body: p.Data})
	if e = acceptedFileReceipt(receipt, e); e != nil {
		return e
	}
	if e = content.ProcessRegistrations(ctx, host); e != nil {
		return e
	}
	actor := &v1.Caller{UserId: host.UserId, IssuerId: "egress-io"}
	receipt, e = content.RegisterFileResources(ctx, actor, &v1.RegisterFileResourcesCommand{Header: header("egress-io", "resources"), DerivationRef: derivation.Ref, SendRef: r.Send.Ref})
	if e = acceptedFileReceipt(receipt, e); e != nil {
		return e
	}
	r.FileResources, e = content.QueryFileResources(ctx, actor, receipt.ResultRef)
	if e != nil {
		return e
	}
	r.Body = p.Data
	r.FileParameters = p
	return nil
}
func acceptedFileReceipt(r *v1.CommandReceipt, e error) error {
	if e != nil {
		return e
	}
	if r == nil || r.Decision != v1.Decision_DECISION_ACCEPTED {
		if r != nil && r.Error != nil {
			return &command.Failure{Detail: r.Error}
		}
		return command.Fail("FILE_RESOURCE_REGISTRATION_REJECTED")
	}
	return nil
}
