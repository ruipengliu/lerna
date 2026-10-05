package tasks

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type Grants interface {
	OccupyInTransaction(context.Context, *v1.Ref, *v1.GlobalName, *v1.GlobalName, *v1.Ref, *v1.Capability, *v1.Ref) (*v1.GrantUse, bool, error)
}
type Budget interface {
	ReserveInTransaction(context.Context, *v1.GlobalName, *v1.GlobalName, *v1.Ref, *v1.Capability) (*v1.BudgetBasis, error)
}
type Content interface {
	CheckUsable(context.Context, *v1.Caller, *v1.Ref) error
}
type Confirmations interface {
	ConsumeAdmissionConfirmation(context.Context, *v1.Ref, *v1.Admission, bool) error
}
type Scheduling interface {
	EnqueueHandoffInTransaction(context.Context, *v1.Admission) (*v1.Ref, error)
}
type ExecutionFacts interface {
	BlocksAdmission(context.Context, *v1.Caller, []*v1.Ref, []*v1.Ref) (bool, error)
}

func (s *Service) WithAdmission(g Grants, b Budget, c Content, confirm Confirmations, j Scheduling, execution ExecutionFacts) *Service {
	s.grants = g
	s.budget = b
	s.content = c
	s.confirmations = confirm
	s.scheduling = j
	s.execution = execution
	return s
}
func (s *Service) ConfigureCapability(ctx context.Context, caller *v1.Caller, c *v1.ConfigureCapabilityCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	fingerprint := command.SemanticFingerprint("configure-capability", c.Capability)
	if c.Replaces != nil {
		fingerprint = command.SemanticFingerprint("replace-capability", c.Capability, c.Replaces)
	}
	return s.decisions.Execute(ctx, caller, c.Header, fingerprint, "tasks.planning", func(tx context.Context) (*v1.Ref, error) {
		cap := c.Capability
		if caller.IssuerId != "host" {
			return nil, command.Fail("PERMISSION_DENIED")
		}
		if cap == nil || cap.Ref != nil || cap.ApprovedBy != nil || cap.Action == "" || cap.Resource == "" || cap.ExecutorEndpointId == "" || cap.AdapterRef == nil || cap.AdapterRef.Revision == 0 || cap.AdapterRef.Name == nil || cap.AdapterRef.Name.UserId != s.user || cap.AdapterRef.Name.ObjectKind != "adapter" || cap.AdapterRef.Name.LocalId == "" || cap.AdapterRef.SchemaId != "lerna.v1.Adapter" || (cap.UseRight != "INVOKE" && (cap.Action != "QUERY" || cap.UseRight != "READ")) || cap.ProcessingPurpose != "CURRENT_TASK" {
			return nil, command.Fail("INVALID_CAPABILITY")
		}
		cap = proto.Clone(cap).(*v1.Capability)
		cap.ApprovedBy = c.Header.Identity
		cap.Ref = command.NewRef(s.user, s.domain, "capability", "lerna.v1.Capability")
		if c.Replaces != nil {
			current, e := s.QueryCurrentCapability(tx, caller, c.Replaces)
			if e != nil {
				return nil, e
			}
			if current == nil || !proto.Equal(current.Ref, c.Replaces) {
				return nil, command.Fail("STALE_REFERENCE")
			}
			cap.Ref = proto.Clone(current.Ref).(*v1.Ref)
			cap.Ref.Revision++
		}
		return cap.Ref, s.store.SaveCapability(tx, cap)
	})
}
func (s *Service) Admit(ctx context.Context, caller *v1.Caller, c *v1.AdmitCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	return s.decisions.Execute(ctx, caller, c.Header, command.SemanticFingerprint("admit", c.TaskId, c.ProposalRef, c.GrantRef, c.ConfirmationRef), "tasks.admit", func(tx context.Context) (*v1.Ref, error) {
		t, e := s.QueryTask(tx, caller, c.TaskId)
		if e != nil {
			return nil, e
		}
		if t == nil {
			return nil, command.Fail("NOT_FOUND")
		}
		p, e := s.store.LoadPlanning(tx, c.TaskId)
		if e != nil {
			return nil, e
		}
		_, now, e := s.store.Position(tx)
		if e != nil {
			return nil, e
		}
		// 准入-1：原决定已由持久工作处理，本处检查当前请求及未消费步骤。
		q := p.Proposal
		snap := p.Snapshot
		if q == nil || snap == nil || p.ProposalConsumed || !proto.Equal(c.ProposalRef, q.Ref) || !proto.Equal(q.RequestRef, snap.RequestRef) || !proto.Equal(q.ContextSnapshotRef, snap.Ref) || now >= snap.ExpiresAtUnixMs || q.PlanningGeneration != t.PlanningGeneration || q.PlanningGeneration != snap.PlanningGeneration {
			return nil, command.Fail("STALE_PROPOSAL")
		}
		// 准入-2：M1 只有一个具体步骤，不接纳依赖其他输出的参数。
		if q.RequirementsVersion != t.RequirementsVersion || q.RequirementsVersion != snap.RequirementsVersion {
			return nil, command.Fail("STALE_REQUIREMENT")
		}
		if q.InputVersion != t.InputVersion || q.InputVersion != snap.InputVersion {
			return nil, command.Fail("STALE_INPUT")
		}
		if q.ControlGeneration != t.ControlGeneration || q.ControlGeneration != snap.ControlGeneration {
			return nil, command.Fail("STALE_GENERATION")
		}
		step := q.Step
		if step == nil || step.ParametersRef == nil || len(step.Dependencies) > 0 {
			return nil, command.Fail("UNSATISFIED_DEPENDENCY")
		}
		// 普通提议始终属于目标类；准备、收尾只能由后续核心工作入口建立。
		if step.WorkCategory != "" && step.WorkCategory != "TARGET" {
			return nil, command.Fail("UNSUPPORTED_FEATURE")
		}
		// 准入-3。
		if t.RequirementsStatus != v1.RequirementsStatus_REQUIREMENTS_STATUS_ACCEPTED || t.BoundInputVersion != t.InputVersion {
			return nil, command.Fail("REQUIREMENTS_NOT_ACCEPTED")
		}
		// 准入-4：子任务不在 M1 支持范围，不可默默跳过祖先控制。
		if t.ParentTaskRef != nil {
			return nil, command.Fail("UNSUPPORTED_FEATURE")
		}
		if t.Lifecycle != v1.TaskLifecycle_TASK_LIFECYCLE_OPEN || t.Control != v1.TaskControl_TASK_CONTROL_ACTIVE || p.VerificationFreeze != 0 {
			return nil, command.Fail("TASK_NOT_ACTIVE")
		}
		// 准入-5：读取所有责任对应的权威执行事实；没有历史责任才是空集。
		operations := make([]*v1.Ref, 0, len(p.AdmissionRefs))
		for _, ref := range p.AdmissionRefs {
			a, e := s.store.LoadAdmission(tx, ref)
			if e != nil {
				return nil, e
			}
			if a == nil {
				return nil, command.Fail("INVARIANT_VIOLATION")
			}
			operations = append(operations, &v1.Ref{Name: a.OperationId, Revision: 1, SchemaId: "lerna.v1.Operation"})
		}
		if len(operations) > 0 || len(p.RejectedVerificationOperations) > 0 {
			if s.execution == nil {
				return nil, command.Fail("EXECUTION_FACTS_UNAVAILABLE")
			}
			blocked, e := s.execution.BlocksAdmission(tx, caller, operations, p.RejectedVerificationOperations)
			if e != nil {
				return nil, e
			}
			if blocked {
				return nil, command.Fail("BLOCKING_OPERATION")
			}
		}
		// 准入-6：长期记忆依赖不在 M1 支持范围。
		if len(step.MemoryDependencies) > 0 {
			return nil, command.Fail("UNSUPPORTED_FEATURE")
		}
		if step.CapabilityRef == nil || command.CheckName(caller, step.CapabilityRef.Name, s.user, s.domain, "capability") != nil {
			return nil, command.Fail("CAPABILITY_INVALID")
		}
		cap, e := s.store.LoadCapability(tx, step.CapabilityRef)
		if e != nil {
			return nil, e
		}
		if cap == nil || !proto.Equal(cap.Ref, step.CapabilityRef) {
			return nil, command.Fail("CAPABILITY_INVALID")
		}
		if cap.Action == "MODEL_INFER" {
			return nil, command.Fail("MODEL_HOST_REQUIRED")
		}
		known := false
		for _, ref := range snap.CapabilityRefs {
			if proto.Equal(ref, cap.Ref) {
				known = true
			}
		}
		if !known {
			return nil, command.Fail("CAPABILITY_INVALID")
		}
		ref := command.NewRef(s.user, s.domain, "admission", "lerna.v1.Admission")
		op := command.NewRef(s.user, s.domain+"/ledger", "operation", "lerna.v1.Operation")
		a := &v1.Admission{Ref: ref, Origin: q.Ref, StepId: step.StepId, TaskId: t.TaskId, RequirementsVersion: t.RequirementsVersion, InputVersion: t.InputVersion, ControlGeneration: t.ControlGeneration, OperationId: op.Name, LedgerDomainId: s.domain + "/ledger", ExecutorEndpointId: cap.ExecutorEndpointId, ParametersRef: step.ParametersRef, CapabilityRef: cap.Ref, CapabilitySnapshot: cap, WorkCategory: "TARGET", ContentRefs: step.ContentRefs, HandoffIdentity: &v1.CommandIdentity{UserId: s.user, IssuerId: "tasks-handoff", TargetDomainId: s.domain + "/ledger", CommandId: op.Name.LocalId}}
		// 准入-7、8、9、10 全部在保存点内，最后一道门禁拒绝撤回前面的使用与预留。
		use, required, e := s.grants.OccupyInTransaction(tx, c.GrantRef, t.TaskId, op.Name, ref, cap, step.ParametersRef)
		if e != nil {
			return nil, e
		}
		a.GrantRefs = []*v1.Ref{use.GrantRef}
		a.GrantUseRef = use.Ref
		a.BudgetBasis, e = s.budget.ReserveInTransaction(tx, t.TaskId, op.Name, ref, cap)
		if e != nil {
			return nil, e
		}
		a.ConfirmationRef = c.ConfirmationRef
		if e = s.confirmations.ConsumeAdmissionConfirmation(tx, c.ConfirmationRef, a, required); e != nil {
			return nil, e
		}
		refs := append([]*v1.Ref{step.ParametersRef, cap.RateBasisRef}, step.ContentRefs...)
		for _, r := range refs {
			if e = s.content.CheckUsable(tx, caller, r); e != nil {
				return nil, e
			}
		}
		if e = s.store.SaveAdmission(tx, a); e != nil {
			return nil, e
		}
		job, e := s.scheduling.EnqueueHandoffInTransaction(tx, a)
		if e != nil {
			return nil, e
		}
		outbox := &v1.Handoff{Ref: command.NewRef(s.user, s.domain, "handoff", "lerna.v1.Handoff"), AdmissionRef: ref, Identity: a.HandoffIdentity, JobRef: job, State: "PENDING"}
		if e = s.store.SaveHandoff(tx, outbox); e != nil {
			return nil, e
		}
		p.ProposalConsumed = true
		p.AdmissionRefs = append(p.AdmissionRefs, ref)
		if e = s.store.SavePlanning(tx, p); e != nil {
			return nil, e
		}
		return ref, nil
	})
}
func (s *Service) QueryAdmission(ctx context.Context, c *v1.Caller, r *v1.Ref) (*v1.Admission, error) {
	if r == nil {
		return nil, command.Fail("INVALID_INPUT")
	}
	if e := command.CheckName(c, r.Name, s.user, s.domain, "admission"); e != nil {
		return nil, e
	}
	a, e := s.store.LoadAdmission(ctx, r)
	if e == nil && a != nil && !proto.Equal(r, a.Ref) {
		return nil, command.Fail("STALE_REFERENCE")
	}
	return a, e
}
func (s *Service) QueryCapability(ctx context.Context, c *v1.Caller, r *v1.Ref) (*v1.Capability, error) {
	if r == nil {
		return nil, command.Fail("INVALID_INPUT")
	}
	if e := command.CheckName(c, r.Name, s.user, s.domain, "capability"); e != nil {
		return nil, e
	}
	cap, e := s.store.(interface {
		LoadCapabilityVersion(context.Context, *v1.Ref) (*v1.Capability, error)
	}).LoadCapabilityVersion(ctx, r)
	if e == nil && cap != nil && !proto.Equal(cap.Ref, r) {
		return nil, command.Fail("STALE_REFERENCE")
	}
	return cap, e
}

// QueryCurrentCapability 返回当前登记声明，历史准入仍保留自己的不可变版本。
func (s *Service) QueryCurrentCapability(ctx context.Context, c *v1.Caller, r *v1.Ref) (*v1.Capability, error) {
	if r == nil || r.SchemaId != "lerna.v1.Capability" || r.Revision == 0 {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	if e := command.CheckName(c, r.Name, s.user, s.domain, "capability"); e != nil {
		return nil, e
	}
	return s.store.LoadCapability(ctx, r)
}
