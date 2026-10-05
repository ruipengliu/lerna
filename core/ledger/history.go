package ledger

import (
	"context"

	"google.golang.org/protobuf/proto"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

type historyStore interface {
	LoadAttemptVersion(context.Context, *v1.Ref) (*v1.ExecutionAttempt, error)
	LoadSendVersion(context.Context, *v1.Ref) (*v1.PhysicalSend, error)
	LoadEffectVersion(context.Context, *v1.Ref) (*v1.Effect, error)
	LoadOperationVersion(context.Context, *v1.Ref) (*v1.Operation, error)
}

func (s *Service) checkHistory(c *v1.Caller, r *v1.Ref, kind, schema string) error {
	if r == nil || r.Revision == 0 || r.SchemaId != schema {
		return command.Fail("INVALID_REFERENCE")
	}
	return command.CheckName(c, r.Name, s.user, s.domain, kind)
}
func (s *Service) QueryAttempt(ctx context.Context, c *v1.Caller, r *v1.Ref) (*v1.ExecutionAttempt, error) {
	if e := s.checkHistory(c, r, "attempt", "lerna.v1.ExecutionAttempt"); e != nil {
		return nil, e
	}
	v, e := s.store.(historyStore).LoadAttemptVersion(ctx, r)
	if v != nil && !proto.Equal(v.Ref, r) {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	return v, e
}
func (s *Service) QuerySend(ctx context.Context, c *v1.Caller, r *v1.Ref) (*v1.PhysicalSend, error) {
	if e := s.checkHistory(c, r, "send", "lerna.v1.PhysicalSend"); e != nil {
		return nil, e
	}
	v, e := s.store.(historyStore).LoadSendVersion(ctx, r)
	if v != nil && !proto.Equal(v.Ref, r) {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	return v, e
}
func (s *Service) QueryEffect(ctx context.Context, c *v1.Caller, r *v1.Ref) (*v1.Effect, error) {
	if e := s.checkHistory(c, r, "effect", "lerna.v1.Effect"); e != nil {
		return nil, e
	}
	v, e := s.store.(historyStore).LoadEffectVersion(ctx, r)
	if v != nil && !proto.Equal(v.Ref, r) {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	return v, e
}
func (s *Service) QueryOperationVersion(ctx context.Context, c *v1.Caller, r *v1.Ref) (*v1.Operation, error) {
	if e := s.checkHistory(c, r, "operation", "lerna.v1.Operation"); e != nil {
		return nil, e
	}
	v, e := s.store.(historyStore).LoadOperationVersion(ctx, r)
	if v != nil && !proto.Equal(v.Ref, r) {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	return v, e
}

// QuerySendExecution 返回该动作中原发送的当前投影；请求引用本身必须是存在的不可变版本。
func (s *Service) QuerySendExecution(ctx context.Context, c *v1.Caller, operation *v1.GlobalName, r *v1.Ref) (*v1.Execution, error) {
	original, e := s.QuerySend(ctx, c, r)
	if e != nil || original == nil {
		return nil, e
	}
	op, e := s.QueryOperation(ctx, c, operation)
	if e != nil || op == nil {
		return nil, e
	}
	if op.Execution == nil || !proto.Equal(original.AttemptId, op.Execution.Attempt.Ref.Name) {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	send := executionSend(op.Execution, r)
	if send == nil {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	return &v1.Execution{Attempt: op.Execution.Attempt, Send: send, CallDescriptor: op.Execution.CallDescriptor}, nil
}

func executionSend(x *v1.Execution, r *v1.Ref) *v1.PhysicalSend {
	if x == nil || r == nil {
		return nil
	}
	if proto.Equal(x.Send.GetRef().GetName(), r.Name) {
		return x.Send
	}
	for _, send := range x.PreviousSends {
		if proto.Equal(send.Ref.Name, r.Name) {
			return send
		}
	}
	return nil
}

func executionMayHaveSent(x *v1.Execution) bool {
	if x == nil {
		return false
	}
	for _, send := range append([]*v1.PhysicalSend{x.Send}, x.PreviousSends...) {
		if send.Phase != "REGISTERED" && send.Phase != "CLOSED" {
			return true
		}
	}
	return false
}
