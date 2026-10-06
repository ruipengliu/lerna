package sessions

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// ReadConfirmation 临时读取原事项的治理正文；不重查事项当前可消费性，也不改写历史。
func (s *Service) ReadConfirmation(ctx context.Context, caller *v1.Caller, ref *v1.Ref) (*v1.Confirmation, error) {
	c, e := s.QueryConfirmation(ctx, caller, ref)
	if e != nil || c == nil {
		return c, e
	}
	var description string
	switch c.MatterType {
	case "OPERATION_ADMISSION", "CONDITION_EVALUATION":
		description, e = s.operationFacts.ReadConfirmationDescription(ctx, caller, c)
	case "GRANT_ISSUANCE":
		description, e = s.grantFacts.ReadConfirmationDescription(ctx, caller, c)
	default:
		return nil, command.Fail("CONFIRMATION_INVALID")
	}
	if e != nil {
		return nil, e
	}
	c = proto.Clone(c).(*v1.Confirmation)
	c.Description = description
	return c, nil
}

// ReadCurrentConfirmation 固定本次查询取得的版本后读取展示，不以展示结果替代确认。
func (s *Service) ReadCurrentConfirmation(ctx context.Context, caller *v1.Caller, name *v1.GlobalName) (*v1.Confirmation, error) {
	c, e := s.QueryCurrentConfirmation(ctx, caller, name)
	if e != nil || c == nil {
		return c, e
	}
	return s.ReadConfirmation(ctx, caller, c.Ref)
}
