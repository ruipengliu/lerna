// Package egress 在实际出口前完成开始门禁和发送记录，不拥有第二份效果事实。
package egress

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

type Starts interface {
	ValidateFileUse(context.Context, *v1.Caller, *v1.StartExecutionCommand) error
	QueryStart(context.Context, *v1.Caller, *v1.Ref) (*v1.StartRecord, error)
	StartExecution(context.Context, *v1.Caller, *v1.StartExecutionCommand) (*v1.CommandReceipt, error)
}
type Ledger interface {
	cancellationLedger
	taskClosingLedger
	QuerySendExecution(context.Context, *v1.Caller, *v1.GlobalName, *v1.Ref) (*v1.Execution, error)
	CloseForCompletion(context.Context, *v1.Caller, *v1.CloseCompletionCommand) (*v1.CommandReceipt, error)
	CloseForGrantRevocation(context.Context, *v1.Caller, *v1.CloseGrantExitCommand) (*v1.CommandReceipt, error)
	ProcessInterpretations(context.Context, *v1.Caller) error
	ProcessReports(context.Context, *v1.Caller) error
	RecordDispatch(context.Context, *v1.Caller, *v1.DispatchCommand) (*v1.CommandReceipt, bool, error)
	QueryExecution(context.Context, *v1.Caller, *v1.GlobalName) (*v1.Execution, error)
	QueryReceipt(context.Context, *v1.Caller, *v1.CommandIdentity) (*v1.ReceiptQuery, error)
}
type Content interface {
	fileContent
	Read(context.Context, *v1.Caller, *v1.Ref) (*v1.Content, error)
	RegisterObservation(context.Context, *v1.Caller, *v1.RegisterObservationCommand) (*v1.CommandReceipt, error)
	ProcessObservations(context.Context, *v1.Caller) error
}

// PreflightIO 是可选预检；缺席时继续原 P4/P5 门禁与实际出口。
type PreflightIO interface {
	Preflight(context.Context, *v1.CallDescriptor) error
}

type IO interface {
	Perform(context.Context, *v1.PhysicalIORequest) (*v1.PhysicalIOResult, error)
}
type CriticalSection interface {
	Enter(context.Context) (func(), error)
}
type Service struct {
	critical CriticalSection
	starts   Starts
	ledger   Ledger
	content  Content
	io       IO
}

func New(starts Starts, ledger Ledger, content Content, io IO, critical CriticalSection) (*Service, error) {
	service := &Service{starts: starts, ledger: ledger, content: content, io: io, critical: critical}
	if err := service.ValidateDependencies(); err != nil {
		return nil, err
	}
	return service, nil
}
func (s *Service) QueryReceipt(ctx context.Context, c *v1.Caller, id *v1.CommandIdentity) (*v1.ReceiptQuery, error) {
	return s.ledger.QueryReceipt(ctx, c, id)
}
func (s *Service) Invoke(ctx context.Context, caller *v1.Caller, c *v1.StartExecutionCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	if caller.GetIssuerId() != "egress" {
		return nil, command.Fail("PERMISSION_DENIED")
	}
	// M1 固定单个执行边界；封闭命令与整个有界使用临界区按同一出口锁排序。
	release, err := s.enter(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	start, e := s.starts.StartExecution(ctx, caller, c)
	if e != nil {
		return nil, e
	}
	if start.Decision != v1.Decision_DECISION_ACCEPTED {
		return start, nil
	}
	startRecord, e := s.starts.QueryStart(ctx, caller, start.ResultRef)
	if e != nil {
		return nil, e
	}
	if startRecord == nil {
		return nil, command.Fail("START_RECEIPT_INVALID")
	}
	header := &v1.CommandHeader{Identity: &v1.CommandIdentity{UserId: caller.UserId, IssuerId: caller.IssuerId, TargetDomainId: c.Binding.OperationId.AuthorityDomainId, CommandId: "dispatch:" + startRecord.SendRef.Name.LocalId}, ContractVersion: 1, FingerprintVersion: 1, SchemaId: "lerna.v1.AdmissionCommands"}
	previous, e := s.ledger.QueryReceipt(ctx, caller, header.Identity)
	if e != nil {
		return nil, e
	}
	if previous.State == v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED {
		return previous.Receipt, nil
	}
	if previous.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND {
		return nil, command.Fail("DEPENDENCY_UNAVAILABLE")
	}
	body, e := s.content.Read(ctx, caller, c.CallDescriptor.ParametersRef)
	if e != nil {
		return nil, e
	}
	if body == nil {
		return nil, command.Fail("CONTENT_UNUSABLE")
	}
	payload := command.ContentBytes(body)
	if c.CallDescriptor.ApiDescriptor != nil {
		if command.BytesDigest(payload) != c.CallDescriptor.ParametersDigest {
			return nil, command.Fail("PREPARATION_UNRECOVERABLE")
		}
		payload, e = command.CompileAPIParameters(payload)
		if e != nil {
			return nil, e
		}
		if c.CallDescriptor.Method == "GET" {
			payload = nil
		}
	}
	if c.CallDescriptor.BodyDigest != "" {
		sum := sha256.Sum256(payload)
		if hex.EncodeToString(sum[:]) != c.CallDescriptor.BodyDigest {
			return nil, command.Fail("PREPARATION_UNRECOVERABLE")
		}
	}
	if preflight, ok := s.io.(PreflightIO); ok {
		if e = preflight.Preflight(ctx, c.CallDescriptor); e != nil {
			return nil, e
		}
	}
	receipt, fresh, e := s.ledger.RecordDispatch(ctx, caller, &v1.DispatchCommand{Header: header, OperationId: c.Binding.OperationId, StartReceipt: start, Claim: c.Claim})
	if e != nil {
		return nil, e
	}
	if !fresh {
		return receipt, nil
	}
	x, e := s.ledger.QuerySendExecution(ctx, caller, c.Binding.OperationId, receipt.ResultRef)
	if e != nil {
		return nil, e
	}
	if x == nil {
		return nil, command.Fail("NOT_FOUND")
	}
	request := &v1.PhysicalIORequest{TaskId: c.Binding.TaskId, OperationId: c.Binding.OperationId, ExecutorEndpointId: c.Binding.ExecutorEndpointId, Attempt: x.Attempt, Send: x.Send, CallDescriptor: x.CallDescriptor, Body: payload}
	var result *v1.PhysicalIOResult
	if request.CallDescriptor.Protocol == "FILE" {
		if e = s.prepareFile(ctx, request); e != nil {
			return receipt, e
		}
		checked, ok := s.io.(CheckedIO)
		if !ok {
			return receipt, command.Fail("UNSUPPORTED_CAPABILITY")
		}
		result, e = checked.PerformChecked(ctx, request, func(useCtx context.Context) error { return s.starts.ValidateFileUse(useCtx, caller, c) })
	} else {
		result, e = s.io.Perform(ctx, request)
	}
	if e != nil {
		return receipt, e
	}
	if result == nil || result.Observation == nil {
		return receipt, command.Fail("OBSERVATION_UNAVAILABLE")
	}
	actor := &v1.Caller{UserId: caller.UserId, IssuerId: "egress-io"}
	oh := &v1.CommandHeader{Identity: &v1.CommandIdentity{UserId: caller.UserId, IssuerId: actor.IssuerId, TargetDomainId: c.CallDescriptor.ParametersRef.Name.AuthorityDomainId, CommandId: "observe:" + result.Observation.Ref.Name.LocalId}, ContractVersion: 1, FingerprintVersion: 1, SchemaId: "lerna.v1.AdmissionCommands"}
	saved, e := s.content.RegisterObservation(ctx, actor, &v1.RegisterObservationCommand{Header: oh, Observation: result.Observation, Body: result.Body})
	if e != nil {
		return receipt, e
	}
	if saved.Decision != v1.Decision_DECISION_ACCEPTED {
		return receipt, command.Fail("OBSERVATION_REJECTED")
	}
	if e = s.content.ProcessObservations(ctx, actor); e != nil {
		return receipt, e
	}
	if e = s.ledger.ProcessReports(ctx, actor); e != nil {
		return receipt, e
	}
	return receipt, s.ledger.ProcessInterpretations(ctx, actor)
}

// CloseForGrantRevocation 等待同一出口内所有已放行的使用退出，再提交不可逆的封闭事实。
func (s *Service) CloseForGrantRevocation(ctx context.Context, caller *v1.Caller, c *v1.CloseGrantExitCommand) (*v1.CommandReceipt, error) {
	release, err := s.enter(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	return s.ledger.CloseForGrantRevocation(ctx, caller, c)
}

// enter 要求宿主提供跨实例的实际使用围栏，不能退化为单对象锁。
func (s *Service) enter(ctx context.Context) (func(), error) {
	if s.critical == nil {
		return nil, command.Fail("DEPENDENCY_UNAVAILABLE")
	}
	return s.critical.Enter(ctx)
}
