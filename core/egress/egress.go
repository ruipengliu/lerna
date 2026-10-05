// Package egress 在实际出口前完成开始门禁和发送记录，不拥有第二份效果事实。
package egress

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

type Starts interface {
	StartExecution(context.Context, *v1.Caller, *v1.StartExecutionCommand) (*v1.CommandReceipt, error)
}
type Ledger interface {
	CloseForCompletion(context.Context, *v1.Caller, *v1.CloseCompletionCommand) (*v1.CommandReceipt, error)
	CloseForGrantRevocation(context.Context, *v1.Caller, *v1.CloseGrantExitCommand) (*v1.CommandReceipt, error)
	ProcessInterpretations(context.Context, *v1.Caller) error
	ProcessReports(context.Context, *v1.Caller) error
	RecordDispatch(context.Context, *v1.Caller, *v1.DispatchCommand) (*v1.CommandReceipt, bool, error)
	QueryExecution(context.Context, *v1.Caller, *v1.GlobalName) (*v1.Execution, error)
	QueryReceipt(context.Context, *v1.Caller, *v1.CommandIdentity) (*v1.ReceiptQuery, error)
}
type Content interface {
	Read(context.Context, *v1.Caller, *v1.Ref) (*v1.Content, error)
	RegisterObservation(context.Context, *v1.Caller, *v1.RegisterObservationCommand) (*v1.CommandReceipt, error)
	ProcessObservations(context.Context, *v1.Caller) error
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

func New(starts Starts, ledger Ledger, content Content, io IO, critical CriticalSection) *Service {
	return &Service{starts: starts, ledger: ledger, content: content, io: io, critical: critical}
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
	body, e := s.content.Read(ctx, caller, c.CallDescriptor.ParametersRef)
	if e != nil {
		return nil, e
	}
	if body == nil {
		return nil, command.Fail("CONTENT_UNUSABLE")
	}
	x, e := s.ledger.QueryExecution(ctx, caller, c.Binding.OperationId)
	if e != nil {
		return nil, e
	}
	if x == nil {
		return nil, command.Fail("NOT_FOUND")
	}
	header := &v1.CommandHeader{Identity: &v1.CommandIdentity{UserId: caller.UserId, IssuerId: caller.IssuerId, TargetDomainId: c.Binding.OperationId.AuthorityDomainId, CommandId: "dispatch:" + x.Send.Ref.Name.LocalId}, ContractVersion: 1, FingerprintVersion: 1, SchemaId: "lerna.v1.AdmissionCommands"}
	receipt, fresh, e := s.ledger.RecordDispatch(ctx, caller, &v1.DispatchCommand{Header: header, OperationId: c.Binding.OperationId, StartReceipt: start, Claim: c.Claim})
	if e != nil {
		return nil, e
	}
	if !fresh {
		return receipt, nil
	}
	x, e = s.ledger.QueryExecution(ctx, caller, c.Binding.OperationId)
	if e != nil {
		return nil, e
	}
	payload := command.ContentBytes(body)
	result, e := s.io.Perform(ctx, &v1.PhysicalIORequest{TaskId: c.Binding.TaskId, OperationId: c.Binding.OperationId, ExecutorEndpointId: c.Binding.ExecutorEndpointId, Attempt: x.Attempt, Send: x.Send, CallDescriptor: x.CallDescriptor, Body: payload})
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
