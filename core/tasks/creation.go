package tasks

import (
	"context"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

// CreateFromGoalInTransaction 在原裁决事务内建立完整初始任务与输入依据。
// 调用方已验证目标和会话；原命令身份只取自初始输入，会话保留提交序和关联。
func (s *Service) CreateFromGoalInTransaction(ctx context.Context, caller *v1.Caller, goal *v1.Ref, input *v1.SessionInput, conditions []*v1.Requirement) (*v1.Ref, error) {
	task, err := s.createInTransaction(ctx, goal)
	if err != nil {
		return nil, err
	}
	if len(conditions) > 0 {
		if err = s.acceptExplicitInTransaction(ctx, caller, task, conditions, input.CommandIdentity, &v1.Ref{Name: input.InputId, Revision: 1, SchemaId: "lerna.v1.SessionInput"}); err != nil {
			return nil, err
		}
	}
	if err = s.recordGoalInTransaction(ctx, caller, task, input, conditions); err != nil {
		return nil, err
	}
	return task, nil
}
