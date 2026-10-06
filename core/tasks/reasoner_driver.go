package tasks

import (
	"context"
	"errors"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/reasoner"
	"google.golang.org/protobuf/proto"
)

type reasonerDriverStore interface {
	SaveReasonerDriver(context.Context, *v1.ReasonerDriver) error
	LoadReasonerDriver(context.Context, *v1.GlobalName) (*v1.ReasonerDriver, error)
	LoadReasonerDriverVersion(context.Context, *v1.Ref) (*v1.ReasonerDriver, error)
	AllReasonerDrivers(context.Context) ([]*v1.ReasonerDriver, error)
}

// ReasonerFactory 只装配固定推理实现，不决定任务的下一步。
type ReasonerFactory func(*v1.ModelSettings) reasoner.Reasoner

func (s *Service) WithReasonerDriver(work ModelWork, factory ReasonerFactory) *Service {
	s.reasonerWork = work
	s.reasonerFactory = factory
	return s
}

// ConfigureReasonerDriver 保存宿主显式承接的责任；配置引用不授予新权限。
func (s *Service) ConfigureReasonerDriver(ctx context.Context, caller *v1.Caller, c *v1.ConfigureReasonerDriverCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	return s.decisions.Execute(ctx, caller, c.Header, command.SemanticFingerprint("reasoner-driver", c.TaskId, c.Policy, c.Replaces, c.Disabled), "tasks.planning", func(tx context.Context) (*v1.Ref, error) {
		if caller.GetIssuerId() != "host" {
			return nil, command.Fail("PERMISSION_DENIED")
		}
		t, e := s.QueryTask(tx, caller, c.TaskId)
		if e != nil {
			return nil, e
		}
		if t == nil {
			return nil, command.Fail("NOT_FOUND")
		}
		if c.Policy == nil || c.Policy.ModelCapabilityRef == nil {
			return nil, command.Fail("INVALID_INPUT")
		}
		if _, e = normalizedModelSettings(c.Policy.Settings); e != nil {
			return nil, e
		}
		if s.reasonerFactory == nil {
			return nil, command.Fail("CONFIGURATION_REQUIRED")
		}
		description, e := s.reasonerFactory(c.Policy.Settings).DescribeReasoner(1)
		if e != nil {
			return nil, e
		}
		if description == nil || description.ContractVersion != 1 || description.ImplementationVersion == "" {
			return nil, command.Fail("UNSUPPORTED_CONTRACT")
		}

		seen := map[string]bool{}
		for _, a := range c.Policy.Actions {
			if a.GetCapabilityRef().GetName() == nil {
				return nil, command.Fail("INVALID_INPUT")
			}
			key := command.SemanticFingerprint("capability", a.CapabilityRef)
			if seen[key] {
				return nil, command.Fail("INVALID_INPUT")
			}
			seen[key] = true
		}
		store := s.store.(reasonerDriverStore)
		d, e := store.LoadReasonerDriver(tx, c.TaskId)
		if e != nil {
			return nil, e
		}
		if d == nil {
			if c.Replaces != nil {
				return nil, command.Fail("STALE_REFERENCE")
			}
			d = &v1.ReasonerDriver{Ref: command.NewRef(s.user, s.domain, "reasoner-driver", "lerna.v1.ReasonerDriver"), TaskId: c.TaskId, ContractVersion: description.ContractVersion, ImplementationVersion: description.ImplementationVersion}
		} else {
			if e = s.checkReasonerDriverVersion(d); e != nil {
				return nil, e
			}
			if !proto.Equal(c.Replaces, d.Ref) {
				return nil, command.Fail("STALE_REFERENCE")
			}
			// 原模型位置固定以后，配置更新只能改变宿主权限，不能偷偷换模型重采样。
			if !proto.Equal(d.Policy.Settings, c.Policy.Settings) || !proto.Equal(d.Policy.ModelCapabilityRef, c.Policy.ModelCapabilityRef) {
				return nil, command.Fail("MODEL_POSITION_CONFLICT")
			}
			d.Ref.Revision++
		}
		d.Policy = proto.Clone(c.Policy).(*v1.ReasonerDriverPolicy)
		d.ConfiguredBy = c.Header.Identity
		d.Enabled = !c.Disabled
		d.State = "READY"
		d.WaitingReason = ""
		if c.Disabled {
			d.State = "STOPPED"
		}
		return d.Ref, s.saveReasonerDriver(tx, d)
	})
}

func (s *Service) QueryReasonerDriver(ctx context.Context, c *v1.Caller, id *v1.GlobalName) (*v1.ReasonerDriver, error) {
	if e := command.CheckName(c, id, s.user, s.domain, "task"); e != nil {
		return nil, e
	}
	return s.store.(reasonerDriverStore).LoadReasonerDriver(ctx, id)
}
func (s *Service) QueryReasonerDriverVersion(ctx context.Context, c *v1.Caller, r *v1.Ref) (*v1.ReasonerDriver, error) {
	if r == nil {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	if e := command.CheckName(c, r.Name, s.user, s.domain, "reasoner-driver"); e != nil {
		return nil, e
	}
	d, e := s.store.(reasonerDriverStore).LoadReasonerDriverVersion(ctx, r)
	if e == nil && d != nil && !proto.Equal(d.Ref, r) {
		return nil, command.Fail("STALE_REFERENCE")
	}
	return d, e
}

func (s *Service) saveReasonerDriver(ctx context.Context, d *v1.ReasonerDriver) error {
	if e := s.store.(reasonerDriverStore).SaveReasonerDriver(ctx, d); e != nil {
		return e
	}
	refs := []*v1.Ref{d.RequestRef, d.OutcomeRef, d.AdmissionRef, d.QuestionRef, d.Policy.ModelCapabilityRef, d.Policy.ModelGrantRef, d.Policy.ModelConfirmationRef}
	for _, a := range d.Policy.Actions {
		refs = append(refs, a.CapabilityRef, a.GrantRef, a.ConfirmationRef)
	}
	return s.store.SaveTraceSource(ctx, "tasks", &v1.TraceEvent{EventType: "REASONER_DRIVER_" + d.State, SourceRecordRef: d.Ref, TaskId: d.TaskId, OriginCommand: d.ConfiguredBy, RelatedRefs: refs, ReasonCode: d.WaitingReason})
}

func (s *Service) updateReasonerDriver(ctx context.Context, expected *v1.ReasonerDriver, change func(*v1.ReasonerDriver)) (*v1.ReasonerDriver, error) {
	var result *v1.ReasonerDriver
	e := s.store.Transaction(ctx, "tasks.planning", func(tx context.Context) error {
		current, e := s.store.(reasonerDriverStore).LoadReasonerDriver(tx, expected.TaskId)
		if e != nil {
			return e
		}
		if current == nil {
			return command.Fail("NOT_FOUND")
		}
		result = current
		if !proto.Equal(current.Ref, expected.Ref) {
			return nil
		}
		next := proto.Clone(current).(*v1.ReasonerDriver)
		change(next)
		if proto.Equal(next, current) {
			return nil
		}
		next.Ref.Revision++
		if e = s.saveReasonerDriver(tx, next); e != nil {
			return e
		}
		result = next
		return nil
	})
	return result, e
}
func (s *Service) waitReasonerDriver(ctx context.Context, d *v1.ReasonerDriver, reason string) (*v1.ReasonerDriver, error) {
	return s.updateReasonerDriver(ctx, d, func(next *v1.ReasonerDriver) { next.State = "WAITING"; next.WaitingReason = reason })
}

// AdvanceReasonerTask 消费原待办；阶段有限，真实进展以外的结果不会产生新调用位置。
func (s *Service) AdvanceReasonerTask(ctx context.Context, caller *v1.Caller, c *v1.AdvanceReasonerTaskRequest) (*v1.ReasonerDriver, error) {
	if caller.GetIssuerId() != "host" {
		return nil, command.Fail("PERMISSION_DENIED")
	}
	if c == nil || len(c.ProtoReflect().GetUnknown()) > 0 || c.Limit > 16 {
		return nil, command.Fail("INVALID_INPUT")
	}
	d, e := s.QueryReasonerDriver(ctx, caller, c.TaskId)
	if e != nil {
		return nil, e
	}
	if d == nil {
		return &v1.ReasonerDriver{TaskId: c.TaskId, State: "WAITING", WaitingReason: "CONFIGURATION_REQUIRED"}, nil
	}
	if e = s.checkReasonerDriverVersion(d); e != nil {
		return nil, e
	}
	limit := c.Limit
	if limit == 0 {
		limit = 8
	}
	process := command.NewRef(s.user, s.domain, "process", "process").Name.LocalId
	for i := uint32(0); i < limit; i++ {
		if !d.Enabled || d.State == "COMPLETED" {
			return d, nil
		}
		var more bool
		var next *v1.ReasonerDriver
		next, more, e = s.advanceReasonerStage(ctx, caller, d, process)
		if e != nil {
			var failure *command.Failure
			if !errors.As(e, &failure) {
				return nil, e
			}
			return s.waitReasonerDriver(ctx, d, failure.Detail.Code)
		}
		d = next
		if !more {
			return d, nil
		}
	}
	return s.updateReasonerDriver(ctx, d, func(next *v1.ReasonerDriver) { next.State = "READY"; next.WaitingReason = "ADVANCE_LIMIT" })
}

// RecoverReasonerDrivers 只继续已保存的启用责任；没有配置的任务不自动取得模型权限。
func (s *Service) RecoverReasonerDrivers(ctx context.Context, caller *v1.Caller) error {
	if command.CheckCaller(caller, s.user) != nil || caller.GetIssuerId() != "host" {
		return command.Fail("PERMISSION_DENIED")
	}
	drivers, e := s.store.(reasonerDriverStore).AllReasonerDrivers(ctx)
	if e != nil {
		return e
	}
	for _, d := range drivers {
		if e = s.checkReasonerDriverVersion(d); e != nil {
			return e
		}
	}
	for _, d := range drivers {
		if !d.Enabled || d.State == "COMPLETED" {
			continue
		}
		if _, e = s.AdvanceReasonerTask(ctx, caller, &v1.AdvanceReasonerTaskRequest{TaskId: d.TaskId}); e != nil {
			return e
		}
	}
	return nil
}

func (s *Service) advanceReasonerStage(ctx context.Context, caller *v1.Caller, d *v1.ReasonerDriver, process string) (*v1.ReasonerDriver, bool, error) {
	t, e := s.QueryTask(ctx, caller, d.TaskId)
	if e != nil {
		return nil, false, e
	}
	if t == nil {
		return nil, false, command.Fail("NOT_FOUND")
	}
	if t.ResultRef != nil {
		next, e := s.updateReasonerDriver(ctx, d, func(next *v1.ReasonerDriver) { next.State = "COMPLETED"; next.WaitingReason = "" })
		return next, false, e
	}
	if t.Lifecycle != v1.TaskLifecycle_TASK_LIFECYCLE_OPEN || t.Control != v1.TaskControl_TASK_CONTROL_ACTIVE {
		return nil, false, command.Fail("TASK_NOT_ACTIVE")
	}
	if s.reasonerFactory == nil || s.reasonerWork == nil {
		return nil, false, command.Fail("CONFIGURATION_REQUIRED")
	}
	if d.RequestRef == nil {
		p, e := s.QueryPlanning(ctx, caller, d.TaskId)
		if e != nil {
			return nil, false, e
		}
		snapshot := p.Snapshot
		if snapshot == nil {
			snapshot, e = s.RequestProposal(ctx, caller, &v1.RequestProposalCommand{Header: s.modelHeader("driver-initial:"+d.Ref.Name.LocalId, s.domain), TaskId: d.TaskId})
			if e != nil {
				return nil, false, e
			}
		}
		next, e := s.updateReasonerDriver(ctx, d, func(next *v1.ReasonerDriver) {
			next.RequestRef = snapshot.RequestRef
			next.State = "READY"
			next.WaitingReason = ""
		})
		return next, true, e
	}
	r, e := s.QueryProposalRequest(ctx, caller, d.RequestRef)
	if e != nil {
		return nil, false, e
	}
	if r == nil {
		return nil, false, command.Fail("NOT_FOUND")
	}
	adopted, changed, e := s.adoptOwnerReasonerRequest(ctx, caller, d, r)
	if e != nil {
		return nil, false, e
	}
	if changed {
		return adopted, true, nil
	}
	if r.OutcomeReceipt == nil {
		if e = s.currentModelRequest(ctx, caller, r); e != nil {
			return nil, false, e
		}
		claim, e := s.reasonerWork.ExecuteJob(ctx, caller, &v1.JobCommand{Identity: s.modelHeader("driver-claim:"+r.Ref.Name.LocalId+":"+process, s.domain).Identity, ContractVersion: 1, Action: "CLAIM", JobRef: r.JobRef, Module: "tasks", AllowedTypes: []string{"PROPOSE"}, Limit: 1, LeaseMs: 60000, ProcessInstance: process})
		if e = modelReceipt(claim, e); e != nil {
			return nil, false, e
		}
		if len(claim.Jobs) != 1 {
			return nil, false, command.Fail("CLAIM_PENDING")
		}
		snap, e := s.QuerySnapshot(ctx, caller, r.SnapshotRef)
		if e != nil {
			return nil, false, e
		}
		run := &v1.RunModelCallCommand{Preparation: &v1.PrepareModelCallCommand{Header: s.modelHeader("driver-prepare:"+r.Ref.Name.LocalId+":"+command.SemanticFingerprint("configured", d.ConfiguredBy), s.domain), RequestRef: r.Ref, Settings: d.Policy.Settings, InputRefs: snap.ContentRefs, CapabilityRef: d.Policy.ModelCapabilityRef, Claim: claim.Jobs[0]}, GrantRef: d.Policy.ModelGrantRef, ConfirmationRef: d.Policy.ModelConfirmationRef}
		outcome, e := s.RunReasoner(ctx, caller, run, s.reasonerFactory(d.Policy.Settings))
		if e != nil {
			// 本次调用已返回，释放自己的推进租约；不改变原模型位置或发送事实。
			release, releaseError := s.reasonerWork.ExecuteJob(ctx, caller, &v1.JobCommand{Identity: s.modelHeader("driver-release:"+r.Ref.Name.LocalId+":"+process, s.domain).Identity, ContractVersion: 1, Action: "PROGRESS", JobRef: claim.Jobs[0].Ref, ClaimEpoch: claim.Jobs[0].ClaimEpoch, ProcessInstance: process, NextState: "WAITING", WaitingReason: "REASONER_DRIVER_WAIT"})
			if releaseError != nil {
				return nil, false, releaseError
			}
			if release.GetError() != nil && release.Error.Code != "STALE_CLAIM" {
				return nil, false, &command.Failure{Detail: release.Error}
			}
			return nil, false, e
		}
		next, e := s.updateReasonerDriver(ctx, d, func(next *v1.ReasonerDriver) {
			next.OutcomeRef = outcome.Ref
			next.State = "READY"
			next.WaitingReason = ""
		})
		return next, true, e
	}
	if e = modelReceipt(r.OutcomeReceipt, nil); e != nil {
		return nil, false, e
	}
	outcome, e := s.QueryProposalOutcome(ctx, caller, r.OutcomeReceipt.ResultRef)
	if e != nil {
		return nil, false, e
	}
	if outcome == nil {
		return nil, false, command.Fail("NOT_FOUND")
	}
	if !proto.Equal(d.OutcomeRef, outcome.Ref) {
		next, e := s.updateReasonerDriver(ctx, d, func(next *v1.ReasonerDriver) { next.OutcomeRef = outcome.Ref })
		return next, true, e
	}
	if outcome.ErrorCode != "" {
		return nil, false, command.Fail(outcome.ErrorCode)
	}
	if !outcome.AcceptedForProgress || outcome.ProposalRef == nil {
		return nil, false, command.Fail("STALE_PROPOSAL")
	}
	proposal, e := s.ReadProposal(ctx, caller, outcome.ProposalRef)
	if e != nil {
		return nil, false, e
	}
	switch proposal.Kind {
	case "QUESTION", "REQUIREMENTS":
		if d.Policy.SessionId == nil {
			return nil, false, command.Fail("SESSION_REQUIRED")
		}
		receipt, e := s.PublishProposalQuestion(ctx, caller, &v1.PublishProposalQuestionCommand{Header: s.modelHeader("driver-question:"+proposal.Ref.Name.LocalId, s.domain), ProposalRef: proposal.Ref, SessionId: d.Policy.SessionId})
		if e = modelReceipt(receipt, e); e != nil {
			return nil, false, e
		}
		next, e := s.updateReasonerDriver(ctx, d, func(next *v1.ReasonerDriver) {
			next.QuestionRef = receipt.ResultRef
			next.State = "WAITING"
			next.WaitingReason = "USER_INPUT_REQUIRED"
		})
		return next, false, e
	case "COMPLETE":
		receipt, e := s.BeginCompletion(ctx, caller, &v1.BeginCompletionCommand{Header: s.modelHeader("driver-complete:"+proposal.Ref.Name.LocalId, s.domain), TaskId: d.TaskId, ProposalRef: proposal.Ref})
		if e = modelReceipt(receipt, e); e != nil {
			return nil, false, e
		}
		if e = s.ProcessCompletions(ctx, caller); e != nil {
			return nil, false, e
		}
		t, e = s.QueryTask(ctx, caller, d.TaskId)
		if e != nil {
			return nil, false, e
		}
		if t.ResultRef == nil {
			return nil, false, command.Fail("COMPLETION_PENDING")
		}
		next, e := s.updateReasonerDriver(ctx, d, func(next *v1.ReasonerDriver) { next.State = "COMPLETED"; next.WaitingReason = "" })
		return next, false, e
	case "ACTION":
		return s.advanceReasonerAction(ctx, caller, d, proposal, process)
	default:
		return nil, false, command.Fail("INVALID_PROPOSAL")
	}
}

// adoptOwnerReasonerRequest 只依据已接受的输入、需求和控制事实继续原责任。
func (s *Service) adoptOwnerReasonerRequest(ctx context.Context, caller *v1.Caller, d *v1.ReasonerDriver, old *v1.ProposalRequest) (*v1.ReasonerDriver, bool, error) {
	planning, e := s.QueryPlanning(ctx, caller, d.TaskId)
	if e != nil {
		return nil, false, e
	}
	current := planning.Snapshot
	task, e := s.QueryTask(ctx, caller, d.TaskId)
	if e != nil {
		return nil, false, e
	}
	previous, e := s.QuerySnapshot(ctx, caller, old.SnapshotRef)
	if e != nil {
		return nil, false, e
	}
	if previous == nil || task == nil || (task.InputVersion <= previous.InputVersion && task.RequirementsVersion <= previous.RequirementsVersion && task.ControlGeneration <= previous.ControlGeneration) {
		return d, false, nil
	}
	if old.OutcomeReceipt != nil {
		outcome, e := s.QueryProposalOutcome(ctx, caller, old.OutcomeReceipt.ResultRef)
		if e != nil {
			return nil, false, e
		}
		if outcome == nil || outcome.ErrorCode == "UNKNOWN" {
			return d, false, nil
		}
	} else if len(old.ModelOperationRefs) > 0 {
		return d, false, nil
	}
	// 完成核验自身递增的控制代次不是用户接受的新依据；继续责任归原核验。
	if planning.VerificationRef != nil {
		verification, err := s.QueryVerification(ctx, caller, planning.VerificationRef)
		if err != nil {
			return nil, false, err
		}
		if verification == nil {
			return nil, false, command.Fail("EXECUTION_FACTS_UNAVAILABLE")
		}
		if verification.Status == "REJECTED" && verification.InputVersion == task.InputVersion && verification.RequirementsVersion == task.RequirementsVersion && verification.ControlGeneration == task.ControlGeneration && proto.Equal(verification.RequirementsRef, planning.Requirements.GetRef()) {
			proposal, err := s.QueryProposal(ctx, caller, verification.ProposalRef)
			if err != nil {
				return nil, false, err
			}
			if proposal == nil {
				return nil, false, command.Fail("EXECUTION_FACTS_UNAVAILABLE")
			}
			if proto.Equal(proposal.RequestRef, old.Ref) {
				if verification.ContinuationRequestRef == nil {
					return nil, false, command.Fail("COMPLETION_PENDING")
				}
				if current == nil || !proto.Equal(current.RequestRef, verification.ContinuationRequestRef) {
					return nil, false, command.Fail("STALE_PROPOSAL")
				}
			}
		}
	}
	// 新输入不是旧未知动作的重发许可；已有责任仍由执行管理阻塞后续准入。
	if e = s.checkReasonerProgress(ctx, caller, planning); e != nil {
		return nil, false, e
	}
	if task.Lifecycle != v1.TaskLifecycle_TASK_LIFECYCLE_OPEN || task.Control != v1.TaskControl_TASK_CONTROL_ACTIVE || planning.VerificationFreeze != 0 {
		return nil, false, command.Fail("TASK_NOT_ACTIVE")
	}
	if current == nil || current.InputVersion != task.InputVersion || current.RequirementsVersion != task.RequirementsVersion || current.ControlGeneration != task.ControlGeneration || current.PlanningGeneration != task.PlanningGeneration {
		current, e = s.requestProposal(ctx, caller, &v1.RequestProposalCommand{Header: s.modelHeader("driver-basis:"+d.Ref.Name.LocalId+":"+command.SemanticFingerprint("basis", task.InputVersion, task.RequirementsVersion, task.ControlGeneration), s.domain), TaskId: d.TaskId}, func(tx context.Context) error {
			check := func() error {
				latest, err := s.QueryReasonerDriver(tx, caller, d.TaskId)
				if err != nil {
					return err
				}
				if latest == nil || !latest.Enabled || !proto.Equal(latest.Ref, d.Ref) {
					return command.Fail("STALE_REFERENCE")
				}
				t, err := s.QueryTask(tx, caller, d.TaskId)
				if err != nil {
					return err
				}
				p, err := s.QueryPlanning(tx, caller, d.TaskId)
				if err != nil {
					return err
				}
				if t == nil || t.Lifecycle != v1.TaskLifecycle_TASK_LIFECYCLE_OPEN || t.Control != v1.TaskControl_TASK_CONTROL_ACTIVE || p.VerificationFreeze != 0 {
					return command.Fail("TASK_NOT_ACTIVE")
				}
				if t.InputVersion != task.InputVersion || t.RequirementsVersion != task.RequirementsVersion || t.ControlGeneration != task.ControlGeneration || !proto.Equal(p.Snapshot, planning.Snapshot) {
					return command.Fail("STALE_PROPOSAL")
				}
				if err = s.checkReasonerProgress(tx, caller, p); err != nil {
					return err
				}
				return s.supersedeProposalRequest(tx, previous)
			}
			err := check()
			var failure *command.Failure
			if errors.As(err, &failure) {
				detail := proto.Clone(failure.Detail).(*v1.ContractError)
				detail.Category = v1.ErrorCategory_ERROR_CATEGORY_TRANSIENT
				detail.CommandAcceptance = v1.CommandAcceptance_COMMAND_ACCEPTANCE_NOT_SUBMITTED
				return &command.Failure{Detail: detail}
			}
			return err
		})
		if e != nil {
			return nil, false, e
		}
	}
	request, e := s.QueryProposalRequest(ctx, caller, current.RequestRef)
	if e != nil {
		return nil, false, e
	}
	if request == nil {
		return nil, false, command.Fail("NOT_FOUND")
	}
	if request.OutcomeReceipt == nil {
		if e = s.currentModelRequest(ctx, caller, request); e != nil {
			return nil, false, e
		}
	}
	next, e := s.updateReasonerDriver(ctx, d, func(next *v1.ReasonerDriver) {
		next.RequestRef = request.Ref
		next.OutcomeRef = nil
		next.AdmissionRef = nil
		next.QuestionRef = nil
		next.State = "READY"
		next.WaitingReason = ""
	})
	return next, true, e
}

func (s *Service) checkReasonerDriverVersion(d *v1.ReasonerDriver) error {
	if d.ContractVersion != 1 || len(d.ProtoReflect().GetUnknown()) > 0 {
		return command.Fail("UNSUPPORTED_CONTRACT")
	}
	if d.Policy == nil || s.reasonerFactory == nil {
		return command.Fail("CONFIGURATION_REQUIRED")
	}
	description, e := s.reasonerFactory(d.Policy.Settings).DescribeReasoner(d.ContractVersion)
	if e != nil {
		return e
	}
	if description == nil || description.ContractVersion != d.ContractVersion || description.ImplementationVersion != d.ImplementationVersion {
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
	return nil
}
