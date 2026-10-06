package tasks

import (
	"context"
	"slices"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type OperationProgressSource interface {
	QueryOperationProgressNotice(context.Context, *v1.Caller, *v1.Ref) (*v1.OperationProgressNotice, error)
	QueryReconciliation(context.Context, *v1.Caller, *v1.GlobalName) (*v1.Reconciliation, error)
	QueryOperation(context.Context, *v1.Caller, *v1.GlobalName) (*v1.Operation, error)
}
type progressStore interface {
	SaveTaskOperationProgress(context.Context, *v1.OperationProgressNotice) error
	LoadTaskOperationProgress(context.Context, *v1.Ref) (*v1.OperationProgressNotice, error)
}

func (s *Service) WithOperationProgress(source OperationProgressSource) *Service {
	s.progressSource = source
	return s
}
func (s *Service) AcceptOperationProgress(ctx context.Context, caller *v1.Caller, c *v1.AcceptOperationProgressCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	return s.decisions.Execute(ctx, caller, c.Header, command.SemanticFingerprint("accept-operation-progress", c.Notice), "tasks.operation_progress", func(tx context.Context) (*v1.Ref, error) {
		if caller.IssuerId != "ledger-progress" || s.progressSource == nil || c.Notice == nil || c.Notice.Ref == nil || !proto.Equal(c.Notice.Identity, c.Header.Identity) {
			return nil, command.Fail("INVALID_PROGRESS_NOTICE")
		}
		n, e := s.progressSource.QueryOperationProgressNotice(tx, caller, c.Notice.Ref)
		if e != nil {
			return nil, e
		}
		if n == nil || !proto.Equal(n, c.Notice) {
			return nil, command.Fail("INVALID_PROGRESS_NOTICE")
		}
		task, e := s.QueryTask(tx, caller, n.TaskId)
		if e != nil {
			return nil, e
		}
		if task == nil {
			return nil, command.Fail("NOT_FOUND")
		}
		planning, e := s.store.LoadPlanning(tx, n.TaskId)
		if e != nil {
			return nil, e
		}
		known := false
		for _, r := range planning.AdmissionRefs {
			a, e := s.store.LoadAdmission(tx, r)
			if e != nil {
				return nil, e
			}
			if a != nil && proto.Equal(a.OperationId, n.OperationRef.Name) {
				known = true
			}
		}
		if !known {
			return nil, command.Fail("INVALID_PROGRESS_NOTICE")
		}
		current, e := s.progressSource.QueryReconciliation(tx, caller, n.OperationRef.Name)
		if e != nil {
			return nil, e
		}
		op, e := s.progressSource.QueryOperation(tx, caller, n.OperationRef.Name)
		if e != nil {
			return nil, e
		}
		if current == nil || op == nil || !proto.Equal(current.TaskId, task.TaskId) {
			return nil, command.Fail("INVALID_PROGRESS_NOTICE")
		}
		if task.Lifecycle != v1.TaskLifecycle_TASK_LIFECYCLE_CLOSED {
			before := proto.Clone(task).(*v1.Task)
			wait := "RECONCILIATION:" + op.Ref.Name.LocalId
			task.WaitingOn = slices.DeleteFunc(task.WaitingOn, func(s string) bool { return s == wait })
			if current.State != "COMPLETED" || op.Lifecycle != "SETTLED" {
				task.WaitingOn = append(task.WaitingOn, wait)
			}
			task.Progress = v1.TaskProgress_TASK_PROGRESS_WAITING
			if len(task.WaitingOn) == 0 && task.Control == v1.TaskControl_TASK_CONTROL_ACTIVE && task.BoundInputVersion == task.InputVersion && task.RequirementsStatus == v1.RequirementsStatus_REQUIREMENTS_STATUS_ACCEPTED && planning.VerificationFreeze == 0 {
				task.Progress = v1.TaskProgress_TASK_PROGRESS_RUNNING
			}
			if !proto.Equal(before, task) {
				task.Revision++
				if e = s.saveTask(tx, task); e != nil {
					return nil, e
				}
			}
		}
		return n.Ref, s.saveTaskOperationProgress(tx, n)
	})
}
func (s *Service) QueryOperationProgress(ctx context.Context, c *v1.Caller, r *v1.Ref) (*v1.OperationProgressNotice, error) {
	if r == nil || r.SchemaId != "lerna.v1.OperationProgressNotice" || r.Revision != 1 {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	if e := command.CheckName(c, r.Name, s.user, s.domain+"/ledger", "operation-progress"); e != nil {
		return nil, e
	}
	n, e := s.store.(progressStore).LoadTaskOperationProgress(ctx, r)
	if n != nil && !proto.Equal(n.Ref, r) {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	return n, e
}
func (s *Service) QueryOperationProgressReceipt(ctx context.Context, c *v1.Caller, id *v1.CommandIdentity) (*v1.ReceiptQuery, error) {
	return s.QueryStartReceipt(ctx, c, id)
}

// ProcessOperationProgress 由已保存的核验责任重查；依赖恢复后源通知会再次驱动。
func (s *Service) ProcessOperationProgress(ctx context.Context, c *v1.Caller) error {
	if e := command.CheckCaller(c, s.user); e != nil {
		return e
	}
	return s.ProcessCompletions(ctx, c)
}
