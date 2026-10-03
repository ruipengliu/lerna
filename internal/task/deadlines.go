package task

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// 原deadline使用独立责任键，不能被立即advance的完成或minDue吞掉。
// 与原Task/命令共同提交，重开后仍用已冻结期限；无需新Job kind。
func (s *Service) scheduleDeadlineTx(ctx context.Context, tx runtime.Tx, t taskState) error {
	deadline, err := api.ParseTime(t.Task.Deadline)
	if err != nil {
		return err
	}
	if plan, ok := tx.(JobIntentPlanner); ok {
		return plan.AddJobIntent(ctx, JobAdvance, "deadline/"+t.Task.TaskID, taskRef(tx, t), deadline)
	}
	_, err = tx.Raise(ctx, JobAdvance, "deadline/"+t.Task.TaskID, taskRef(tx, t), deadline)
	return err
}

// 已存旧版Task可能没有deadline责任；原Decision关闭时补核原期限，不能重开目标。
func (s *Service) finishClosedDecision(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work, taskID string) error {
	return s.finish(ctx, store, scope, work, runtime.Done(), func(tx runtime.Tx) error {
		t, err := getTask(ctx, tx, taskID)
		if err != nil {
			return err
		}
		if terminal(t) {
			return nil
		}
		closed, err := s.expireTx(ctx, tx, &t)
		if err != nil || closed {
			return err
		}
		return s.scheduleDeadlineTx(ctx, tx, t)
	})
}

// RecoverDeadline 是宿主对已知原Task的受信维护用例，不是新公开线方法。
// 旧版本缺失责任时只补原期限；已经到期则关闭原目标，未知效果/费用仍保留。
func (s *Service) RecoverDeadline(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, taskID string) error {
	if auth.TenantID != scope.TenantID || (!auth.HasRole("service") && !auth.HasRole("task_admin")) {
		return api.E("forbidden", "deadline_maintenance_denied")
	}
	return s.transaction(ctx, store, scope, func(tx runtime.Tx) error {
		t, err := getTask(ctx, tx, taskID)
		if err != nil {
			return err
		}
		if err = principal(auth, t); err != nil {
			return err
		}
		if terminal(t) {
			return nil
		}
		closed, err := s.expireTx(ctx, tx, &t)
		if err != nil || closed {
			return err
		}
		return s.scheduleDeadlineTx(ctx, tx, t)
	})
}
