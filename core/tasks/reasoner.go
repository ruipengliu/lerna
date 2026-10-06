package tasks

import (
	"context"
	"errors"
	"fmt"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/reasoner"
	"google.golang.org/protobuf/proto"
)

type boundModelCaller struct {
	service *Service
	caller  *v1.Caller
	binding *v1.RunModelCallCommand
	result  *v1.ModelCallResult
}

func (b *boundModelCaller) Call(ctx context.Context, c *reasoner.ModelRequest) (*reasoner.ModelResponse, error) {
	if c == nil || c.Position != 0 {
		return nil, command.Fail("MODEL_CALL_LIMIT")
	}
	for _, ref := range c.InputRefs {
		found := false
		for _, allowed := range b.binding.Preparation.InputRefs {
			found = found || proto.Equal(ref, allowed)
		}
		if !found {
			return nil, command.Fail("INVALID_OUTPUT")
		}
	}
	requested, e := normalizedModelSettings(c.Settings)
	if e != nil {
		return nil, e
	}
	fixed, e := normalizedModelSettings(b.binding.Preparation.Settings)
	if e != nil {
		return nil, e
	}
	maxBytes, maxTokens := fixed.MaxInputBytes, fixed.MaxInputTokens
	if maxBytes == 0 {
		maxBytes = 262144
	}
	if maxTokens == 0 {
		maxTokens = 65536
	}
	if requested.ViewPolicy != reasoner.ViewPolicy || requested.MaxInputBytes > maxBytes || requested.MaxInputTokens > maxTokens {
		return nil, command.Fail("INVALID_OUTPUT")
	}
	requested.ViewPolicy = ""
	requested.MaxInputBytes = 0
	requested.MaxInputTokens = 0
	fixed.ViewPolicy = ""
	fixed.MaxInputBytes = 0
	fixed.MaxInputTokens = 0
	if !proto.Equal(requested, fixed) {
		return nil, command.Fail("INVALID_OUTPUT")
	}
	run := proto.Clone(b.binding).(*v1.RunModelCallCommand)
	run.Preparation.Position = c.Position
	run.Preparation.Settings = c.Settings
	run.Preparation.InputRefs = c.InputRefs
	result, e := b.service.RunModelCall(ctx, b.caller, run)
	if e != nil {
		return nil, e
	}
	b.result = result
	response := &reasoner.ModelResponse{Result: proto.Clone(result).(*v1.ModelCallResult)}
	if result.OutputRef != nil {
		content, e := b.service.modelContent.Read(ctx, b.caller, result.OutputRef)
		if e != nil {
			return nil, e
		}
		response.Body = command.ContentBytes(content)
	}
	return response, nil
}

// RunReasoner 受信宿主绑定原请求和权限；恢复只查询原回报，错误不会建立新提议请求。
func (s *Service) RunReasoner(ctx context.Context, caller *v1.Caller, c *v1.RunModelCallCommand, impl reasoner.Reasoner) (*v1.ProposalOutcome, error) {
	if caller.GetIssuerId() != "host" {
		return nil, command.Fail("PERMISSION_DENIED")
	}
	if c.GetPreparation() == nil || impl == nil {
		return nil, command.Fail("INVALID_INPUT")
	}
	request, e := s.QueryProposalRequest(ctx, caller, c.Preparation.RequestRef)
	if e != nil {
		return nil, e
	}
	if request == nil {
		return nil, command.Fail("NOT_FOUND")
	}
	if request.OutcomeReceipt != nil {
		if e = modelReceipt(request.OutcomeReceipt, nil); e != nil {
			return nil, e
		}
		return s.QueryProposalOutcome(ctx, caller, request.OutcomeReceipt.ResultRef)
	}
	description, e := impl.DescribeReasoner(1)
	if e != nil {
		return nil, e
	}
	if description == nil || description.ContractVersion != 1 || description.MaxCallPositions > request.MaxCallPositions || description.MaxPhysicalSends > request.MaxPhysicalSends || description.MaxPlanLength != 1 {
		return nil, command.Fail("UNSUPPORTED_CONTRACT")
	}
	snap, e := s.QuerySnapshot(ctx, caller, request.SnapshotRef)
	if e != nil {
		return nil, e
	}
	binding := &boundModelCaller{service: s, caller: proto.Clone(caller).(*v1.Caller), binding: proto.Clone(c).(*v1.RunModelCallCommand)}
	invocation := &reasoner.Invocation{Model: binding, InputRefs: proto.Clone(c.Preparation).(*v1.PrepareModelCallCommand).InputRefs}
	if snap.RequirementsRef != nil {
		invocation.Requirements, e = s.QueryRequirements(ctx, caller, snap.RequirementsRef)
		if e != nil {
			return nil, e
		}
	}
	for _, ref := range snap.CapabilityRefs {
		capability, e := s.QueryCapability(ctx, caller, ref)
		if e != nil {
			return nil, e
		}
		if capability == nil {
			return nil, command.Fail("PREPARATION_UNRECOVERABLE")
		}
		invocation.Capabilities = append(invocation.Capabilities, capability)
	}
	proposal, computeError := impl.Propose(ctx, proto.Clone(snap).(*v1.ContextSnapshot), invocation)
	outcome := &v1.SubmitProposalOutcomeCommand{Header: s.modelHeader(fmt.Sprintf("reasoner-outcome:%s:%s:%d", request.Ref.Name.LocalId, c.Preparation.Claim.GetProcessInstance(), c.Preparation.Claim.GetClaimEpoch()), s.domain), RequestRef: request.Ref, Claim: c.Preparation.Claim, Proposal: proposal}
	if binding.result != nil {
		outcome.ModelCallRef = binding.result.CallRef
		outcome.OutputRef = binding.result.OutputRef
		outcome.UsageRef = binding.result.UsageRef
	}
	if computeError == nil {
		computeError = s.prepareReasonerProposal(ctx, caller, snap, binding.result, proposal)
	}
	if computeError == nil {
		outcome.Proposal, computeError = s.storeReasonerBody(ctx, caller, snap, binding.result, proposal)
	}
	if computeError != nil {
		outcome.Proposal = nil
		var failure *command.Failure
		if !errors.As(computeError, &failure) {
			return nil, computeError
		}
		if failure.Detail.Category == v1.ErrorCategory_ERROR_CATEGORY_TRANSIENT || failure.Detail.Category == v1.ErrorCategory_ERROR_CATEGORY_INDETERMINATE {
			return nil, computeError
		}
		outcome.ErrorCode = failure.Detail.Code
		switch outcome.ErrorCode {
		case "INVALID_INPUT", "INVALID_ARGUMENTS", "INVALID_MODEL_SETTINGS", "MODEL_CALL_LIMIT", "MODEL_POSITION_CONFLICT":
			outcome.ErrorCode = "INVALID_OUTPUT"
		case "UNKNOWN", "REFUSED", "INCOMPLETE", "INVALID_OUTPUT", "PREPARATION_UNRECOVERABLE", "CONTEXT_TOO_LARGE", "INSUFFICIENT_CONTEXT", "DEPENDENCY_UNAVAILABLE":
		default:
			// 存储结果不确定时保留原请求，不能用同一回报身份固化临时错误。
			return nil, computeError
		}
	}
	receipt, e := s.SubmitProposalOutcome(ctx, caller, outcome)
	if e = modelReceipt(receipt, e); e != nil {
		return nil, e
	}
	return s.QueryProposalOutcome(ctx, caller, receipt.ResultRef)
}
