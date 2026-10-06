package tasks

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// ReadConfirmationDescription 按原事项引用临时展示字节，普通、模型和收尾动作使用同一路径。
func (s *Service) ReadConfirmationDescription(ctx context.Context, caller *v1.Caller, c *v1.Confirmation) (string, error) {
	if c.GetMatterType() == "CONDITION_EVALUATION" {
		view, e := s.ReadConditionConfirmation(ctx, caller, c.Ref)
		if e != nil {
			return "", e
		}
		parameters := make([]command.ParameterDescription, 0, len(view.Materials))
		for _, content := range view.Materials {
			parameters = append(parameters, command.DescribeParameters(content))
		}
		return command.RenderConfirmationParameters(c, parameters)
	}
	m := c.GetOperationAdmission()
	if c.GetMatterType() != "OPERATION_ADMISSION" || m == nil {
		return "", command.Fail("CONFIRMATION_INVALID")
	}
	if e := s.content.CheckUsable(ctx, caller, m.ParametersRef); e != nil {
		return "", e
	}
	content, e := s.confirmationContent.Read(ctx, caller, m.ParametersRef)
	if e != nil {
		return "", e
	}
	if content == nil || !proto.Equal(content.Ref, m.ParametersRef) {
		return "", command.Fail("CONTENT_UNUSABLE")
	}
	return command.RenderConfirmationParameters(c, []command.ParameterDescription{command.DescribeParameters(content)})
}
