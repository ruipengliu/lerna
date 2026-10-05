package tasks

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type StartGrants interface {
	ConsumeCredentialInTransaction(context.Context, *v1.Caller, *v1.Ref, *v1.ExitCredentialBinding, *v1.Admission) error
}
type StartBudget interface {
	ConsumeSendInTransaction(context.Context, *v1.Admission, *v1.Ref, *v1.CommandIdentity) error
}
type StartExecutionFacts interface {
	CheckRecoveryAllowed(context.Context) error
	ValidateStart(context.Context, *v1.Caller, *v1.ExitCredentialBinding, *v1.CallDescriptor, *v1.Job, int64) (*v1.Ref, error)
}
type startStore interface {
	SaveStart(context.Context, *v1.StartRecord) error
	LoadStart(context.Context, *v1.Ref) (*v1.StartRecord, error)
}

func (s *Service) WithStart(g StartGrants, b StartBudget, l StartExecutionFacts) *Service {
	s.startGrants = g
	s.startBudget = b
	s.startExecution = l
	return s
}
func (s *Service) StartExecution(ctx context.Context, caller *v1.Caller, c *v1.StartExecutionCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	return s.decisions.Execute(ctx, caller, c.Header, command.SemanticFingerprint("start-execution", c.AdmissionRef, c.CredentialRef, c.Binding, c.CallDescriptor), "tasks.start", func(tx context.Context) (*v1.Ref, error) {
		if caller.IssuerId != "egress" || c.Binding == nil {
			return nil, command.Fail("PERMISSION_DENIED")
		}
		a, e := s.QueryAdmission(tx, caller, c.AdmissionRef)
		if e != nil {
			return nil, e
		}
		if a == nil {
			return nil, command.Fail("NOT_FOUND")
		}
		_, now, e := s.store.Position(tx)
		if e != nil {
			return nil, e
		}
		send, e := s.startExecution.ValidateStart(tx, caller, c.Binding, c.CallDescriptor, c.Claim, now)
		if e != nil {
			return nil, e
		}
		if c.Header.Identity.CommandId != "start:"+send.Name.LocalId {
			return nil, command.Fail("INVALID_START_IDENTITY")
		}
		// 开始-2：首次开始以准入的控制与输入依据为围栏。
		task, e := s.QueryTask(tx, caller, a.TaskId)
		if e != nil {
			return nil, e
		}
		if task == nil || task.ControlGeneration != a.ControlGeneration {
			return nil, command.Fail("STALE_GENERATION")
		}
		preparation, e := s.checkModelAdmission(tx, caller, a)
		if e != nil {
			return nil, e
		}
		if a.WorkCategory != "CLOSURE" && !preparation && task.BoundInputVersion != task.InputVersion {
			return nil, command.Fail("STALE_INPUT")
		}
		if a.WorkCategory != "CLOSURE" && (task.RequirementsVersion != a.RequirementsVersion || (!preparation && task.RequirementsStatus != v1.RequirementsStatus_REQUIREMENTS_STATUS_ACCEPTED)) {
			return nil, command.Fail("STALE_REQUIREMENT")
		}
		if task.ParentTaskRef != nil || len(a.AncestorControls) > 0 {
			return nil, command.Fail("UNSUPPORTED_FEATURE")
		}
		planning, e := s.store.LoadPlanning(tx, a.TaskId)
		if e != nil {
			return nil, e
		}
		if a.WorkCategory != "CLOSURE" && (task.Lifecycle != v1.TaskLifecycle_TASK_LIFECYCLE_OPEN || task.Control != v1.TaskControl_TASK_CONTROL_ACTIVE || planning.VerificationFreeze != 0) {
			return nil, command.Fail("TASK_NOT_ACTIVE")
		}
		if a.WorkCategory == "CLOSURE" {
			if e = s.validateClosureAdmission(tx, caller, a); e != nil {
				return nil, e
			}
		}
		if e = s.startGrants.ConsumeCredentialInTransaction(tx, caller, c.CredentialRef, c.Binding, a); e != nil {
			return nil, e
		}
		// 开始-4：回到内容负责方检查当前可用性，依赖故障不保存业务拒绝。
		if len(a.MemoryDependencies) > 0 {
			return nil, command.Fail("UNSUPPORTED_FEATURE")
		}
		refs := append([]*v1.Ref{a.ParametersRef, a.CapabilitySnapshot.RateBasisRef}, a.ContentRefs...)
		for _, ref := range refs {
			if e = s.content.CheckUsable(tx, caller, ref); e != nil {
				return nil, e
			}
		}
		if e = s.startBudget.ConsumeSendInTransaction(tx, a, send, c.Header.Identity); e != nil {
			return nil, e
		}
		// 开始-6：当前端侧账本仍满足启动时固定的持久档位。
		if e = s.startExecution.CheckRecoveryAllowed(tx); e != nil {
			return nil, e
		}
		record := &v1.StartRecord{Ref: command.NewRef(s.user, s.domain, "start", "lerna.v1.StartRecord"), Binding: c.Binding, CredentialRef: c.CredentialRef, SendRef: send, Identity: c.Header.Identity, StartedAtUnixMs: now}
		return record.Ref, s.store.(startStore).SaveStart(tx, record)
	})
}
func (s *Service) QueryStart(ctx context.Context, c *v1.Caller, r *v1.Ref) (*v1.StartRecord, error) {
	if r == nil || r.SchemaId != "lerna.v1.StartRecord" || r.Revision != 1 {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	if e := command.CheckName(c, r.Name, s.user, s.domain, "start"); e != nil {
		return nil, e
	}
	v, e := s.store.(startStore).LoadStart(ctx, r)
	if e == nil && v != nil && !proto.Equal(v.Ref, r) {
		return nil, command.Fail("STALE_REFERENCE")
	}
	return v, e
}

// QueryStartReceipt 用原身份查询开始决定，不触发重试。
func (s *Service) QueryStartReceipt(ctx context.Context, c *v1.Caller, id *v1.CommandIdentity) (*v1.ReceiptQuery, error) {
	return s.decisions.(interface {
		QueryReceipt(context.Context, *v1.Caller, *v1.CommandIdentity) (*v1.ReceiptQuery, error)
	}).QueryReceipt(ctx, c, id)
}
