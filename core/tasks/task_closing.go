package tasks

import (
	"context"
	"slices"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type taskClosingStore interface {
	SaveTaskClosing(context.Context, *v1.TaskClosing) error
	LoadTaskClosing(context.Context, *v1.Ref) (*v1.TaskClosing, error)
	TaskClosingForTask(context.Context, *v1.GlobalName) (*v1.TaskClosing, error)
	AllTaskClosings(context.Context) ([]*v1.TaskClosing, error)
}

type TaskCloseFacts interface {
	QueryCancellationSeal(context.Context, *v1.Caller, *v1.Ref) (*v1.CancellationSeal, error)
	QueryTaskClosureSeal(context.Context, *v1.Caller, *v1.Ref) (*v1.TaskClosureSeal, error)
	QueryExecutionFollowup(context.Context, *v1.Caller, *v1.Ref) (*v1.ExecutionFollowup, error)
	QueryJobs(context.Context, *v1.Caller, *v1.GlobalName) ([]*v1.Job, error)
}

func (s *Service) checkTaskNotClosing(ctx context.Context, t *v1.Task) error {
	closing, e := s.store.(taskClosingStore).TaskClosingForTask(ctx, t.TaskId)
	if e != nil {
		return e
	}
	if closing != nil {
		return command.Fail("TASK_CLOSING")
	}
	return nil
}

type TaskCloseBudget interface {
	RetainSettlementInTransaction(context.Context, *v1.Caller, *v1.TaskClosing, *v1.Admission) (*v1.SettlementFollowup, error)
	QuerySettlementFollowup(context.Context, *v1.Caller, *v1.Ref) (*v1.SettlementFollowup, error)
	QueryReservations(context.Context, *v1.Caller, *v1.GlobalName) ([]*v1.Reservation, error)
}

func (s *Service) WithTaskClosingFacts(f TaskCloseFacts, b TaskCloseBudget) *Service {
	s.taskCloseFacts = f
	s.taskCloseBudget = b
	return s
}

// BeginTaskClose 固定独立停止依据；非成功关闭不得伪造通过的完成核验。
func (s *Service) BeginTaskClose(ctx context.Context, caller *v1.Caller, c *v1.BeginTaskCloseCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	return s.decisions.Execute(ctx, caller, c.Header, command.SemanticFingerprint("begin-task-close", c), "tasks.closing", func(tx context.Context) (*v1.Ref, error) {
		if caller.GetIssuerId() != "host" && caller.GetIssuerId() != "local-cli" {
			return nil, command.Fail("PERMISSION_DENIED")
		}
		if c.TaskRef == nil || c.TaskRef.SchemaId != "lerna.v1.Task" {
			return nil, command.Fail("INVALID_REFERENCE")
		}
		t, e := s.QueryTask(tx, caller, c.TaskRef.Name)
		if e != nil {
			return nil, e
		}
		if t == nil {
			return nil, command.Fail("NOT_FOUND")
		}
		if t.Lifecycle != v1.TaskLifecycle_TASK_LIFECYCLE_OPEN || t.Revision != c.TaskRef.Revision || t.ControlGeneration != c.ExpectedControlGeneration {
			return nil, command.Fail("STALE_GENERATION")
		}
		if c.Outcome != "FAILED" && c.Outcome != "CANCELLED" || !slices.Contains([]string{"UNABLE_TO_COMPLETE", "DEADLINE_REACHED", "USER_STOPPED"}, c.CloseReason) || c.Outcome == "FAILED" && (c.CancellationRef != nil || t.Control == v1.TaskControl_TASK_CONTROL_CANCELLING) || c.Outcome == "CANCELLED" && t.Control != v1.TaskControl_TASK_CONTROL_CANCELLING {
			return nil, command.Fail("INVALID_CLOSE_BASIS")
		}
		old, e := s.store.(taskClosingStore).TaskClosingForTask(tx, t.TaskId)
		if e != nil {
			return nil, e
		}
		if old != nil {
			return nil, command.Fail("TASK_CLOSE_IN_PROGRESS")
		}
		p, e := s.store.LoadPlanning(tx, t.TaskId)
		if e != nil {
			return nil, e
		}
		if c.Outcome == "CANCELLED" {
			if _, e = s.checkClosingCancellation(tx, caller, t, c.CancellationRef, p.AdmissionRefs, false); e != nil {
				return nil, e
			}
		}
		if e = s.supersedeProposalRequest(tx, p.Snapshot); e != nil {
			return nil, e
		}
		if e = s.invalidatePlanning(tx, t, p); e != nil {
			return nil, e
		}
		if c.Outcome == "FAILED" {
			t.Control = v1.TaskControl_TASK_CONTROL_PAUSED
		}
		t.ControlGeneration++
		t.Revision++
		setCompletionWaiting(t, []string{"COMPLETION:NON_SUCCESS_CLOSURE"})
		_, now, e := s.store.Position(tx)
		if e != nil {
			return nil, e
		}
		closing := &v1.TaskClosing{Ref: command.NewRef(s.user, s.domain, "task-closing", "lerna.v1.TaskClosing"), TaskId: t.TaskId, RequestedBy: c.Header.Identity, Outcome: c.Outcome, CloseReason: c.CloseReason, CancellationRef: c.CancellationRef, RequirementsRef: p.Requirements.GetRef(), RequirementsVersion: t.RequirementsVersion, InputVersion: t.InputVersion, ControlGeneration: t.ControlGeneration, AdmissionRefs: p.AdmissionRefs, SavedAtUnixMs: now}
		if e = s.addTaskClosures(tx, closing, closing.AdmissionRefs, closing.Ref); e != nil {
			return nil, e
		}
		for _, ref := range closing.AdmissionRefs {
			a, e := s.QueryAdmission(tx, caller, ref)
			if e != nil {
				return nil, e
			}
			followup, e := s.taskCloseBudget.RetainSettlementInTransaction(tx, caller, closing, a)
			if e != nil {
				return nil, e
			}
			closing.SettlementFollowupRefs = append(closing.SettlementFollowupRefs, followup.Ref)
		}
		if e = s.saveTaskClosing(tx, closing); e != nil {
			return nil, e
		}
		if e = s.store.SavePlanning(tx, p); e != nil {
			return nil, e
		}
		return closing.Ref, s.saveTask(tx, t)
	})
}

func (s *Service) QueryTaskClosing(ctx context.Context, c *v1.Caller, r *v1.Ref) (*v1.TaskClosing, error) {
	if r == nil || r.Revision != 1 || r.SchemaId != "lerna.v1.TaskClosing" {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	if e := command.CheckName(c, r.Name, s.user, s.domain, "task-closing"); e != nil {
		return nil, e
	}
	v, e := s.store.(taskClosingStore).LoadTaskClosing(ctx, r)
	if e == nil && v != nil && !proto.Equal(v.Ref, r) {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	return v, e
}

// QueryTaskClosingView 将固定 Result 与原负责方的当前事实分别返回。
func (s *Service) QueryTaskClosingView(ctx context.Context, c *v1.Caller, id *v1.GlobalName) (*v1.TaskClosingView, error) {
	if e := command.CheckName(c, id, s.user, s.domain, "task"); e != nil {
		return nil, e
	}
	closing, e := s.store.(taskClosingStore).TaskClosingForTask(ctx, id)
	if e != nil {
		return nil, e
	}
	if closing == nil {
		return nil, command.Fail("NOT_FOUND")
	}
	v := &v1.TaskClosingView{Closing: closing}
	v.Task, e = s.QueryTask(ctx, c, id)
	if e != nil {
		return nil, e
	}
	v.Result, e = s.QueryResult(ctx, c, id)
	if e != nil {
		return nil, e
	}
	for _, ref := range closing.ClosureIntentRefs {
		intent, e := s.QueryTaskClosureIntent(ctx, c, ref)
		if e != nil {
			return nil, e
		}
		if intent == nil {
			return nil, command.Fail("INVARIANT_VIOLATION")
		}
		v.ClosureIntents = append(v.ClosureIntents, intent)
		var seal *v1.TaskClosureSeal
		if intent.RecipientReceipt == nil {
			v.PendingClosureRefs = append(v.PendingClosureRefs, ref)
		} else {
			seal, e = s.taskCloseFacts.QueryTaskClosureSeal(ctx, c, intent.RecipientReceipt.ResultRef)
			if e != nil {
				return nil, e
			}
			if seal == nil || !proto.Equal(seal.Ref, intent.RecipientReceipt.ResultRef) || !proto.Equal(seal.IntentRef, intent.Ref) || !proto.Equal(seal.TaskClosingRef, closing.Ref) || !proto.Equal(seal.AdmissionRef, intent.Command.AdmissionRef) || !proto.Equal(seal.OperationId, intent.Command.OperationId) || seal.ExecutorEndpointId != intent.Command.ExecutorEndpointId {
				return nil, command.Fail("EXECUTION_FACTS_UNAVAILABLE")
			}
			v.ClosureSeals = append(v.ClosureSeals, seal)
			if seal.GetExecutionFollowupRef() != nil {
				f, e := s.taskCloseFacts.QueryExecutionFollowup(ctx, c, seal.ExecutionFollowupRef)
				if e != nil {
					return nil, e
				}
				if f == nil {
					return nil, command.Fail("FOLLOWUP_RESPONSIBILITY_MISSING")
				}
				v.ExecutionFollowups = append(v.ExecutionFollowups, f)
			}
		}
		op, e := s.completionFacts.QueryOperation(ctx, c, intent.Command.OperationId)
		if e != nil {
			return nil, e
		}
		if op == nil {
			if seal != nil && (seal.OperationRef != nil || !seal.NoSendProven || seal.PhysicalSendWasPossible) {
				return nil, command.Fail("EXECUTION_FACTS_UNAVAILABLE")
			}
			v.AwaitingOperationAdmissionRefs = append(v.AwaitingOperationAdmissionRefs, intent.Command.AdmissionRef)
		} else {
			v.Operations = append(v.Operations, op)
		}
		jobs, e := s.taskCloseFacts.QueryJobs(ctx, c, intent.Command.OperationId)
		if e != nil {
			return nil, e
		}
		for _, j := range jobs {
			if j.JobType == "RECONCILE_UNRESOLVED_OPERATION" {
				v.FollowupJobs = append(v.FollowupJobs, j)
			}
		}
	}
	for _, ref := range closing.SettlementFollowupRefs {
		f, e := s.taskCloseBudget.QuerySettlementFollowup(ctx, c, ref)
		if e != nil {
			return nil, e
		}
		if f == nil {
			return nil, command.Fail("FOLLOWUP_RESPONSIBILITY_MISSING")
		}
		v.SettlementFollowups = append(v.SettlementFollowups, f)
		job, e := s.taskClosingJobs.QueryJob(ctx, c, f.JobRef.Name)
		if e != nil {
			return nil, e
		}
		v.FollowupJobs = append(v.FollowupJobs, job)
	}
	return v, nil
}

// ProcessTaskClosings 由原关闭记录恢复；固定的 Result 不再被任何后续事实替换。
func (s *Service) ProcessTaskClosings(ctx context.Context, c *v1.Caller) error {
	if e := command.CheckCaller(c, s.user); e != nil {
		return e
	}
	if e := s.ProcessTaskClosures(ctx, c); e != nil {
		return e
	}
	closings, e := s.store.(taskClosingStore).AllTaskClosings(ctx)
	if e != nil {
		return e
	}
	for _, closing := range closings {
		if e = s.store.Transaction(ctx, "tasks.close_final", func(tx context.Context) error {
			t, e := s.QueryTask(tx, c, closing.TaskId)
			if e != nil {
				return e
			}
			if t.Lifecycle == v1.TaskLifecycle_TASK_LIFECYCLE_CLOSED {
				return nil
			}
			if t.ControlGeneration != closing.ControlGeneration || t.InputVersion != closing.InputVersion || t.RequirementsVersion != closing.RequirementsVersion {
				return command.Fail("STALE_CLOSE_BASIS")
			}
			if closing.Outcome == "CANCELLED" {
				ready, e := s.checkClosingCancellation(tx, c, t, closing.CancellationRef, closing.AdmissionRefs, true)
				if e != nil || !ready {
					return e
				}
			}
			_, now, e := s.store.Position(tx)
			if e != nil {
				return e
			}
			result := &v1.Result{Ref: command.NewRef(s.user, s.domain, "result", "lerna.v1.Result"), TaskId: t.TaskId, Outcome: closing.Outcome, CloseReason: closing.CloseReason, RequirementsRef: closing.RequirementsRef, TaskClosingRef: closing.Ref, ClosedAtUnixMs: now}
			if len(closing.AdmissionRefs) != len(closing.ClosureIntentRefs) || len(closing.AdmissionRefs) != len(closing.SettlementFollowupRefs) {
				return command.Fail("INVARIANT_VIOLATION")
			}
			admissions := map[string]*v1.Admission{}
			ops := map[string]*v1.Operation{}
			for _, ref := range closing.ClosureIntentRefs {
				intent, e := s.QueryTaskClosureIntent(tx, c, ref)
				if e != nil {
					return e
				}
				if intent.RecipientReceipt == nil {
					return nil
				}
				seal, e := s.taskCloseFacts.QueryTaskClosureSeal(tx, c, intent.RecipientReceipt.ResultRef)
				if e != nil {
					return e
				}
				if seal == nil || !proto.Equal(seal.IntentRef, intent.Ref) || !proto.Equal(seal.TaskClosingRef, closing.Ref) || !proto.Equal(seal.AdmissionRef, intent.Command.AdmissionRef) || !proto.Equal(seal.OperationId, intent.Command.OperationId) || seal.ExecutorEndpointId != intent.Command.ExecutorEndpointId {
					return command.Fail("INVALID_CLOSURE")
				}
				a, e := s.QueryAdmission(tx, c, seal.AdmissionRef)
				if e != nil {
					return e
				}
				h, e := s.store.LoadHandoff(tx, a.Ref)
				if e != nil {
					return e
				}
				op, e := s.completionFacts.QueryOperation(tx, c, a.OperationId)
				if e != nil {
					return e
				}
				if op == nil || h == nil || h.RecipientReceipt == nil {
					return nil
				}
				if op.Dispatch != "SEALED" || !proto.Equal(op.AdmissionRef, a.Ref) || op.ExecutorEndpointId != a.ExecutorEndpointId {
					return command.Fail("INVALID_CLOSURE")
				}
				admissions[a.OperationId.LocalId] = a
				ops[a.OperationId.LocalId] = op
				result.OperationRefs = append(result.OperationRefs, op.Ref)
				if op.Effect == nil || op.Effect.Outcome == "UNKNOWN" || op.Effect.LateEffect != "RULED_OUT" || op.Effect.EvidenceConflict || op.Lifecycle != "SETTLED" {
					followup, e := s.taskCloseFacts.QueryExecutionFollowup(tx, c, seal.ExecutionFollowupRef)
					if e != nil {
						return e
					}
					if followup == nil || !proto.Equal(followup.OperationId, a.OperationId) || !proto.Equal(followup.AdmissionRef, a.Ref) || !proto.Equal(followup.TaskClosingRef, closing.Ref) || followup.ExecutorEndpointId != a.ExecutorEndpointId {
						return command.Fail("FOLLOWUP_RESPONSIBILITY_MISSING")
					}
					result.ExecutionFollowupRefs = append(result.ExecutionFollowupRefs, followup.Ref)
				}
				if op.Effect == nil || op.Effect.Outcome == "UNKNOWN" {
					result.UnknownOperationRefs = append(result.UnknownOperationRefs, op.Ref)
				}
			}
			reservations, e := s.taskCloseBudget.QueryReservations(tx, c, t.TaskId)
			if e != nil {
				return e
			}
			for _, ref := range closing.SettlementFollowupRefs {
				followup, e := s.taskCloseBudget.QuerySettlementFollowup(tx, c, ref)
				if e != nil {
					return e
				}
				if followup == nil || !proto.Equal(followup.TaskClosingRef, closing.Ref) || !proto.Equal(followup.TaskId, t.TaskId) {
					return command.Fail("FOLLOWUP_RESPONSIBILITY_MISSING")
				}
				for _, r := range reservations {
					if proto.Equal(r.OperationId, followup.OperationId) {
						result.ReservationRefs = append(result.ReservationRefs, r.Ref)
					}
				}
			}
			p, e := s.store.LoadPlanning(tx, t.TaskId)
			if e != nil {
				return e
			}
			if !proto.Equal(p.Requirements.GetRef(), closing.RequirementsRef) {
				return command.Fail("STALE_CLOSE_BASIS")
			}
			for _, condition := range p.Requirements.GetConditions() {
				var candidates []*v1.CompletionEvidence
				for _, ref := range closing.AdmissionRefs {
					a, e := s.QueryAdmission(tx, c, ref)
					if e != nil {
						return e
					}
					scope := condition.GetTargetRecord()
					if a.RequirementsVersion == p.Requirements.RequirementsVersion && proto.Equal(a.CapabilityRef, scope.GetCapabilityRef()) && proto.Equal(a.ParametersRef, scope.GetParametersRef()) {
						candidates = append(candidates, &v1.CompletionEvidence{ConditionId: condition.ConditionId, OperationId: a.OperationId})
					}
				}
				finding, e := s.checkCondition(tx, c, p.Requirements, condition, candidates, admissions, ops, nil)
				if e != nil {
					return e
				}
				if t.RequirementsStatus != v1.RequirementsStatus_REQUIREMENTS_STATUS_ACCEPTED || t.BoundInputVersion != t.InputVersion {
					finding.Conclusion, finding.Gap = "UNKNOWN", "REQUIREMENTS_STALE_INPUT"
					finding.OperationRef, finding.EvidenceRefs = nil, nil
				}
				result.Conditions = append(result.Conditions, finding)
			}
			result.UsageSnapshot, e = s.completionBudget.QueryBudget(tx, c, t.TaskId)
			if e != nil {
				return e
			}
			t.Lifecycle = v1.TaskLifecycle_TASK_LIFECYCLE_CLOSED
			t.ResultRef = result.Ref
			t.Revision++
			setCompletionWaiting(t, nil)
			if e = s.saveResult(tx, result); e != nil {
				return e
			}
			return s.saveTask(tx, t)
		}); e != nil {
			return e
		}
	}
	return nil
}
