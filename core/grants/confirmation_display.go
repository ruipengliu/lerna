package grants

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// ReadConfirmationDescription 只读取原授权事项的 EXACT 参数；ANY 范围没有正文依赖。
func (s *Service) ReadConfirmationDescription(ctx context.Context, caller *v1.Caller, c *v1.Confirmation) (string, error) {
	m := c.GetGrantIssuance()
	if c.GetMatterType() != "GRANT_ISSUANCE" || m == nil || m.Grant == nil {
		return "", command.Fail("CONFIRMATION_INVALID")
	}
	parameters := make([]command.ParameterDescription, len(m.Grant.Permissions))
	for i, permission := range m.Grant.Permissions {
		if permission.ParameterMode != "EXACT" {
			continue
		}
		if e := s.confirmationContent.CheckUsable(ctx, caller, permission.ParametersRef); e != nil {
			return "", e
		}
		content, e := s.confirmationContent.Read(ctx, caller, permission.ParametersRef)
		if e != nil {
			return "", e
		}
		if content == nil || !proto.Equal(content.Ref, permission.ParametersRef) {
			return "", command.Fail("CONTENT_UNUSABLE")
		}
		parameters[i] = command.DescribeParameters(content)
	}
	return command.RenderConfirmationParameters(c, parameters)
}
