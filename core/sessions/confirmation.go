package sessions

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

// ConsumeAdmissionConfirmation 在可信交互流程实现之前，要求确认的动作明确拒绝。
func (s *Service) ConsumeAdmissionConfirmation(_ context.Context, ref *v1.Ref, _ *v1.Admission, required bool) error {
	if required || ref != nil {
		return command.Fail("CONFIRMATION_INVALID")
	}
	return nil
}
