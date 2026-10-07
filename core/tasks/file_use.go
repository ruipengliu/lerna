package tasks

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

type currentFileGrants interface {
	ValidateConsumedCredentialInTransaction(context.Context, *v1.Caller, *v1.Ref, *v1.ExitCredentialBinding, *v1.Admission) error
}
type currentFileExecution interface {
	ValidateFileUse(context.Context, *v1.Caller, *v1.ExitCredentialBinding, *v1.CallDescriptor, *v1.Job, int64) (*v1.Ref, error)
}
type currentFileBudget interface {
	CheckConsumedSendInTransaction(context.Context, *v1.Admission, *v1.Ref) error
}

// ValidateFileUse 在受信文件使用边界检查原消费仍有效；不产生新的发送权。
func (s *Service) ValidateFileUse(ctx context.Context, caller *v1.Caller, c *v1.StartExecutionCommand) error {
	if caller.GetIssuerId() != "egress" || c.GetCallDescriptor().GetProtocol() != "FILE" {
		return command.Fail("PERMISSION_DENIED")
	}
	return s.store.Transaction(ctx, "tasks.file_use", func(tx context.Context) error {
		a, e := s.QueryAdmission(tx, caller, c.AdmissionRef)
		if e != nil {
			return e
		}
		if a == nil {
			return command.Fail("NOT_FOUND")
		}
		_, now, e := s.store.Position(tx)
		if e != nil {
			return e
		}
		ledger := s.startExecution
		send, e := ledger.ValidateFileUse(tx, caller, c.Binding, c.CallDescriptor, c.Claim, now)
		if e != nil {
			return e
		}
		task, e := s.QueryTask(tx, caller, a.TaskId)
		if e != nil {
			return e
		}
		if task == nil || task.ControlGeneration != a.ControlGeneration {
			return command.Fail("STALE_GENERATION")
		}
		if a.WorkCategory != "CLOSURE" && (task.Lifecycle != v1.TaskLifecycle_TASK_LIFECYCLE_OPEN || task.Control != v1.TaskControl_TASK_CONTROL_ACTIVE || task.InputVersion != a.InputVersion || task.BoundInputVersion != task.InputVersion || task.RequirementsVersion != a.RequirementsVersion || task.RequirementsStatus != v1.RequirementsStatus_REQUIREMENTS_STATUS_ACCEPTED) {
			return command.Fail("TASK_NOT_ACTIVE")
		}
		if a.WorkCategory == "CLOSURE" {
			if e = s.validateClosureAdmission(tx, caller, a); e != nil {
				return e
			}
		}
		grants := s.startGrants
		if e = grants.ValidateConsumedCredentialInTransaction(tx, caller, c.CredentialRef, c.Binding, a); e != nil {
			return e
		}
		for _, ref := range append([]*v1.Ref{a.ParametersRef, a.CapabilitySnapshot.RateBasisRef}, a.ContentRefs...) {
			if e = s.content.CheckUsable(tx, caller, ref); e != nil {
				return e
			}
		}
		budget := s.startBudget
		return budget.CheckConsumedSendInTransaction(tx, a, send)
	})
}
