package task

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// TaskInputContextPreparer只为原已接纳输入取得本次当前范围证明。
// 原receiver CommandID是准入身份，不能用GoalDocument或父SubmissionRef猜测。
// 准备在短元数据Tx之后、正文读取/出版之前执行，不代替最终消费门禁。
type TaskInputContextPreparer interface {
	PrepareTaskInput(context.Context, runtime.Scope, runtime.Auth, api.Task, string) (context.Context, error)
}

func (s *Service) prepareInputEntry(kind string, handler runtime.JobHandler) runtime.JobHandler {
	preparer, configured := s.ports.Gate.(TaskInputContextPreparer)
	if !configured {
		return handler
	}
	return func(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
		if err := store.CheckClaim(ctx, scope, work.Claim); err != nil {
			return err
		}
		commandID := work.Job.SourceRef.ObjectID
		var birthSteer pendingSteer
		var birthInput pendingInput
		var taskID string
		if kind == JobSteer {
			if _, err := store.Read(ctx, scope, steers, commandID, 1, &birthSteer); err != nil {
				return err
			}
			taskID = birthSteer.Input.TaskID
			if birthSteer.CommandID != commandID {
				return api.E("idempotency_conflict", "original_steer_changed")
			}
		} else {
			if _, err := store.Read(ctx, scope, pendingInputs, commandID, 1, &birthInput); err != nil {
				return err
			}
			taskID = birthInput.Input.TaskID
			if birthInput.CommandID != commandID {
				return api.E("idempotency_conflict", "original_input_preparation_changed")
			}
		}
		var actual taskState
		var needsPreparation bool
		err := s.transaction(ctx, store, scope, func(tx runtime.Tx) error {
			// 原命令先于Task根/预算和领域头；已决定的输入不取得新父范围。
			original, err := tx.LoadCommand(ctx, commandID)
			if err != nil {
				return err
			}
			if original.Command.CommandID != commandID || original.Command.TargetID != taskID {
				return api.E("idempotency_conflict", "original_input_command_changed")
			}
			if original.Receipt.Stage != "accepted" {
				return nil
			}
			actual, err = getTask(ctx, tx, taskID)
			if err != nil {
				return err
			}
			now, err := tx.Now(ctx)
			if err != nil {
				return err
			}
			deadline, err := api.ParseTime(actual.Task.Deadline)
			if err != nil {
				return err
			}
			needsPreparation = !terminal(actual) && now.Before(deadline)
			if kind == JobSteer {
				needsPreparation = needsPreparation && actual.PendingGoalCommand == commandID && birthSteer.Input.BaseGoalRevision == actual.Task.GoalRevision
			} else {
				needsPreparation = needsPreparation && birthInput.Input.GoalRevision == actual.Task.GoalRevision
			}
			if !needsPreparation {
				return nil
			}
			// 当前父证明尚未取得，这里只核原提交者，不提前要求完整CheckCurrent。
			if err = s.checkSubmitterTx(ctx, tx, actual); err != nil {
				return err
			}
			if kind == JobSteer {
				var pending pendingSteer
				if _, err = tx.Get(ctx, steers, commandID, &pending); err != nil {
					return err
				}
				// Runtime principal可能是认证peer；领域出生主体是原已映射receiver用户。
				if pending.CommandID != commandID || pending.SubjectID != birthSteer.SubjectID || pending.UploadID != birthSteer.UploadID || !api.Equal(pending.Input, birthSteer.Input) {
					return api.E("idempotency_conflict", "original_steer_changed")
				}
				needsPreparation = pending.State == "pending"
			} else {
				var pending pendingInput
				if _, err = tx.Get(ctx, pendingInputs, commandID, &pending); err != nil {
					return err
				}
				if pending.CommandID != commandID || pending.UploadID != birthInput.UploadID || !api.Equal(pending.Input, birthInput.Input) || !api.Equal(pending.Auth, birthInput.Auth) {
					return api.E("idempotency_conflict", "original_input_preparation_changed")
				}
				needsPreparation = pending.State == "pending"
			}
			return nil
		})
		if err != nil {
			return err
		}
		if needsPreparation {
			if err = store.CheckClaim(ctx, scope, work.Claim); err != nil {
				return err
			}
			ctx, err = preparer.PrepareTaskInput(ctx, scope, submitterAuth(scope, actual), actual.Task, commandID)
			if err != nil {
				return err
			}
			if ctx == nil {
				return api.E("dependency_unavailable", "input_preparation_context_missing")
			}
			if err = store.CheckClaim(ctx, scope, work.Claim); err != nil {
				return err
			}
		}
		// 本次carrier贯穿原读取、出版与Finish；原final Current/Incoming/Claim不减。
		return handler(ctx, store, scope, work)
	}
}
