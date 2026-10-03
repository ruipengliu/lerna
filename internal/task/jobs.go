package task

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

func serviceAuth(scope runtime.Scope) runtime.Auth {
	return runtime.Auth{TenantID: scope.TenantID, SubjectID: scope.OwnerID, CredentialGeneration: 1, Roles: []string{"service", "task_admin", "evidence"}}
}
func (s *Service) finish(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work, d runtime.Disposition, fn func(runtime.Tx) error) error {
	return runtime.Finish(ctx, store, scope, s.config.Participants, work, d, fn)
}
func (s *Service) wait(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	return s.finish(ctx, store, scope, work, runtime.Waiting(time.Now().Add(time.Second)), nil)
}
func (s *Service) preIO(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	return store.CheckClaim(ctx, scope, work.Claim)
}
func deferred(err error) bool {
	return api.IsCode(err, "dependency_unavailable") || api.IsCode(err, "not_found") || api.IsCode(err, "effect_unknown") || api.IsCode(err, "accounting_unknown") || api.IsCode(err, "overloaded")
}
func (s *Service) registerJobs(r *runtime.Registry) error {
	handlers := map[string]runtime.JobHandler{JobAdjustment: s.adjustmentJob, JobInput: s.inputJob, JobAdvance: s.advanceJob, JobDispatchDecision: s.decisionJob, JobDispatchOperation: s.operationJob, JobReconcileOperation: s.reconcileOperationJob, JobCoverage: s.coverageJob, JobCheck: s.checkJob, JobControl: s.controlJob, JobBilling: s.billingJob, JobPublishResult: s.publishResultJob, JobSteer: s.steerJob, JobDelegation: s.delegationJob, JobAllocation: s.allocationJob, JobChildPrepare: s.childPrepareJob, JobChildTransfer: s.transferJob}
	for kind, h := range handlers {
		if e := r.RegisterJob(kind, h); e != nil {
			return e
		}
	}
	return nil
}
func (s *Service) advanceJob(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	var t taskState
	_, err := store.Read(ctx, scope, tasks, work.Job.SourceRef.ObjectID, 0, &t)
	if err != nil {
		return err
	}
	if terminal(t) {
		return s.finish(ctx, store, scope, work, runtime.Done(), nil)
	}
	expired := false
	err = s.transaction(ctx, store, scope, func(tx runtime.Tx) error {
		if e := tx.Guard(ctx, work.Claim); e != nil {
			return e
		}
		current, e := getTask(ctx, tx, t.Task.TaskID)
		if e != nil {
			return e
		}
		expired, e = s.expireTx(ctx, tx, &current)
		return e
	})
	if err != nil {
		return err
	}
	if expired {
		return s.finish(ctx, store, scope, work, runtime.Done(), nil)
	}
	if t.Task.Control != "running" || t.PendingGoalCommand != "" || t.Task.RequirementsState == "awaiting_input" {
		return s.finish(ctx, store, scope, work, runtime.Done(), nil)
	}
	var pending candidatePending
	_, err = store.Read(ctx, scope, candidates, t.Task.SubmitCommandID, 0, &pending)
	if err == nil && pending.Delta.BaseGoalRevision == t.Task.GoalRevision {
		if s.ports.Evidence == nil {
			return s.wait(ctx, store, scope, work)
		}
		if e := s.preIO(ctx, store, scope, work); e != nil {
			return e
		}
		report, e := s.ports.Evidence.ValidateRequirements(ctx, scope, t.Task, pending.Delta)
		if e != nil {
			if deferred(e) {
				return s.wait(ctx, store, scope, work)
			}
			return e
		}
		return s.finish(ctx, store, scope, work, runtime.Done(), func(tx runtime.Tx) error {
			_, e := s.AdoptRequirementsTx(ctx, tx, serviceAuth(scope), t.Task.TaskID, pending.SourceKind, pending.Source, pending.Delta, report)
			return e
		})
	}
	if err != nil && !confirmedNotFound(err) {
		return err
	}
	if t.Task.RequirementsState == "validating" {
		return s.finish(ctx, store, scope, work, runtime.Done(), func(tx runtime.Tx) error {
			_, e := raise(ctx, tx, JobCoverage, "coverage/"+t.Task.TaskID, scope.Ref(t.Task.TaskID, t.Task.Revision))
			return e
		})
	}
	if s.ports.Context == nil {
		return s.wait(ctx, store, scope, work)
	}
	if e := s.preIO(ctx, store, scope, work); e != nil {
		return e
	}
	prepared, err := s.ports.Context.Prepare(ctx, scope, runtime.Auth{TenantID: scope.TenantID, SubjectID: t.SubjectID, CredentialGeneration: 1}, t.Task)
	if err != nil {
		if deferred(err) {
			return s.wait(ctx, store, scope, work)
		}
		return err
	}
	return s.finish(ctx, store, scope, work, runtime.Done(), func(tx runtime.Tx) error {
		_, e := s.PrepareDecisionTx(ctx, tx, serviceAuth(scope), prepared)
		return e
	})
}
func (s *Service) decisionJob(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	var d decisionState
	if _, e := store.Read(ctx, scope, decisions, work.Job.SourceRef.ObjectID, 0, &d); e != nil {
		return e
	}
	if d.Consumed {
		return s.finish(ctx, store, scope, work, runtime.Done(), nil)
	}
	if s.ports.Brain == nil {
		return s.wait(ctx, store, scope, work)
	}
	send := false
	err := s.transaction(ctx, store, scope, func(tx runtime.Tx) error {
		if e := tx.Guard(ctx, work.Claim); e != nil {
			return e
		}
		var current decisionState
		if _, e := tx.Get(ctx, decisions, d.Intent.DecisionID, &current); e != nil {
			return e
		}
		t, e := getTask(ctx, tx, d.Snapshot.TaskRef.ObjectID)
		if e != nil {
			return e
		}
		if terminal(t) || t.Task.Control != "running" || t.Task.GoalRevision != d.Snapshot.GoalRevision || t.Task.ControlRevision != d.Snapshot.ControlRevision {
			return nil
		}
		if e = s.CheckCurrent(ctx, tx, t, true); e != nil {
			return e
		}
		send = true
		if !current.Sent {
			current.Sent = true
			current.Revision++
			return tx.Put(ctx, decisions, current.Intent.DecisionID, current.Revision-1, current)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if send {
		if e := s.preIO(ctx, store, scope, work); e != nil {
			return e
		}
		if e := s.ports.Brain.Dispatch(ctx, scope, d.Intent, d.Snapshot); e != nil {
			if deferred(e) {
				return s.wait(ctx, store, scope, work)
			}
			return e
		}
	}
	if !send && !d.Sent {
		return s.finish(ctx, store, scope, work, runtime.Done(), nil)
	}
	if e := s.preIO(ctx, store, scope, work); e != nil {
		return e
	}
	proposal, err := s.ports.Brain.ReadProposal(ctx, scope, d.Intent)
	if err != nil {
		if deferred(err) {
			return s.wait(ctx, store, scope, work)
		}
		return err
	}
	if proposal.DecisionID != d.Intent.DecisionID {
		return api.E("idempotency_conflict", "wrong_decision")
	}
	var report *ValidationReport
	if proposal.RequirementDelta != nil {
		if s.ports.Evidence == nil {
			return s.wait(ctx, store, scope, work)
		}
		var t taskState
		if _, err = store.Read(ctx, scope, tasks, d.Snapshot.TaskRef.ObjectID, 0, &t); err != nil {
			return err
		}
		validation, e := s.ports.Evidence.ValidateRequirements(ctx, scope, t.Task, *proposal.RequirementDelta)
		if e != nil {
			if deferred(e) {
				return s.wait(ctx, store, scope, work)
			}
			return e
		}
		report = &validation
	}
	return s.finish(ctx, store, scope, work, runtime.Done(), func(tx runtime.Tx) error {
		out, e := s.ConsumeProposalTx(ctx, tx, serviceAuth(scope), proposal, report)
		if e != nil {
			return e
		}
		if out.Outcome != "requirements_changed" && out.Outcome != "stale" {
			var t taskState
			if _, e = tx.Get(ctx, tasks, out.TaskID, &t); e != nil {
				return e
			}
			if !terminal(t) && t.Task.RequirementsState != "awaiting_input" {
				if _, e = raise(ctx, tx, JobAdvance, "advance/"+t.Task.TaskID, taskRef(tx, t)); e != nil {
					return e
				}
			}
		}
		return nil
	})
}
func (s *Service) operationJob(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	var intent OperationIntent
	if _, e := store.Read(ctx, scope, intents, work.Job.SourceRef.ObjectID, 0, &intent); e != nil {
		return e
	}
	if s.ports.Execution == nil {
		return s.wait(ctx, store, scope, work)
	}
	var window api.ControlSnapshot
	send := false
	err := s.transaction(ctx, store, scope, func(tx runtime.Tx) error {
		if e := tx.Guard(ctx, work.Claim); e != nil {
			return e
		}
		var d operationDispatch
		if _, e := tx.Get(ctx, dispatches, intent.OperationID, &d); e != nil {
			return e
		}
		if d.PermanentlyClosed {
			return nil
		}
		t, e := getTask(ctx, tx, intent.TaskRef.ObjectID)
		if e != nil {
			return e
		}
		if t.Task.GoalRevision != intent.GoalRevision || t.Task.ControlRevision != intent.ControlRevision || s.CheckCurrent(ctx, tx, t, true) != nil {
			if !d.Sent {
				d.PermanentlyClosed = true
				d.Revision++
				if e = tx.Put(ctx, dispatches, d.OperationID, d.Revision-1, d); e != nil {
					return e
				}
				return s.closeUnsent(ctx, tx, &t)
			}
			return nil
		}
		if intent.AdmissionPurpose == "goal_action" && t.Task.RequirementsState != "ready" {
			return nil
		}
		if e = s.authorize(ctx, tx, serviceAuth(scope), "task.dispatch", intent.ProcessedSourceRefs, intent.UseIntentRefs); e != nil {
			return e
		}
		now, e := tx.Now(ctx)
		if e != nil {
			return e
		}
		window, e = s.controlSnapshot(ctx, tx, t, now)
		if e != nil {
			return e
		}
		send = true
		if !d.Sent {
			d.Sent = true
			d.Revision++
			return tx.Put(ctx, dispatches, d.OperationID, d.Revision-1, d)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if send {
		if e := s.preIO(ctx, store, scope, work); e != nil {
			return e
		}
		if e := s.ports.Execution.Dispatch(ctx, scope, intent, window); e != nil {
			if deferred(e) {
				return s.wait(ctx, store, scope, work)
			}
			return e
		}
	}
	return s.finish(ctx, store, scope, work, runtime.Done(), func(tx runtime.Tx) error {
		_, e := raise(ctx, tx, JobReconcileOperation, "reconcile/"+intent.OperationID, scope.Ref(intent.OperationID, 1))
		return e
	})
}
func (s *Service) reconcileOperationJob(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	var intent OperationIntent
	if _, e := store.Read(ctx, scope, intents, work.Job.SourceRef.ObjectID, 0, &intent); e != nil {
		return e
	}
	var dispatch operationDispatch
	if _, e := store.Read(ctx, scope, dispatches, intent.OperationID, 0, &dispatch); e != nil {
		return e
	}
	if dispatch.PermanentlyClosed && !dispatch.Sent {
		return s.finish(ctx, store, scope, work, runtime.Done(), nil)
	}
	if s.ports.Execution == nil {
		return s.wait(ctx, store, scope, work)
	}
	if e := s.preIO(ctx, store, scope, work); e != nil {
		return e
	}
	operation, err := s.ports.Execution.Read(ctx, scope, api.ObjectRef{TenantID: scope.TenantID, OwnerID: intent.ExecutorID, ObjectID: intent.OperationID, Revision: 1})
	if err != nil {
		if deferred(err) {
			return s.wait(ctx, store, scope, work)
		}
		return err
	}
	disposition := runtime.Waiting(time.Now().Add(time.Second))
	later, ok := operation.MayApplyLater.(bool)
	if operation.ExecutionState == "closed" && operation.Effect != "unknown" && ok && !later {
		disposition = runtime.Done()
	}
	return s.finish(ctx, store, scope, work, disposition, func(tx runtime.Tx) error { return s.MergeOperationTx(ctx, tx, serviceAuth(scope), operation) })
}
func (s *Service) coverageJob(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	var t taskState
	if _, e := store.Read(ctx, scope, tasks, work.Job.SourceRef.ObjectID, 0, &t); e != nil {
		return e
	}
	if terminal(t) {
		return s.finish(ctx, store, scope, work, runtime.Done(), nil)
	}
	if t.Task.Control != "running" || t.PendingGoalCommand != "" {
		return s.finish(ctx, store, scope, work, runtime.Done(), nil)
	}
	if s.ports.Evidence == nil {
		return s.wait(ctx, store, scope, work)
	}
	if e := s.preIO(ctx, store, scope, work); e != nil {
		return e
	}
	coverage, err := s.ports.Evidence.Coverage(ctx, scope, t.Task)
	if err != nil {
		if deferred(err) {
			return s.wait(ctx, store, scope, work)
		}
		return err
	}
	return s.finish(ctx, store, scope, work, runtime.Done(), func(tx runtime.Tx) error { _, e := s.StoreCoverageTx(ctx, tx, serviceAuth(scope), coverage); return e })
}
func (s *Service) checkJob(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	var request CheckRequest
	if _, e := store.Read(ctx, scope, checkRequests, work.Job.SourceRef.ObjectID, 0, &request); e != nil {
		return e
	}
	if request.State != "pending" {
		return s.finish(ctx, store, scope, work, runtime.Done(), nil)
	}
	var t taskState
	if _, e := store.Read(ctx, scope, tasks, request.Input.TaskID, 0, &t); e != nil {
		return e
	}
	if terminal(t) || t.Task.GoalRevision != request.Input.GoalRevision {
		return s.finish(ctx, store, scope, work, runtime.Done(), nil)
	}
	if t.Task.Control != "running" {
		return s.wait(ctx, store, scope, work)
	}
	if s.ports.Evidence == nil {
		return s.wait(ctx, store, scope, work)
	}
	if e := s.preIO(ctx, store, scope, work); e != nil {
		return e
	}
	result, err := s.ports.Evidence.Check(ctx, scope, t.Task, request)
	if err != nil {
		if deferred(err) {
			return s.wait(ctx, store, scope, work)
		}
		return err
	}
	if result.CheckID != request.CheckID {
		return api.E("idempotency_conflict", "wrong_check")
	}
	return s.finish(ctx, store, scope, work, runtime.Done(), func(tx runtime.Tx) error {
		if _, e := s.RecordCheckTx(ctx, tx, serviceAuth(scope), result); e != nil {
			return e
		}
		request.State = "checked"
		request.Revision++
		return tx.Put(ctx, checkRequests, request.CheckID, request.Revision-1, request)
	})
}
func (s *Service) controlJob(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	parts := strings.Split(work.Job.ResponsibilityKey, "/")
	if len(parts) != 3 {
		return invalid("invalid_control_job")
	}
	var t taskState
	if _, e := store.Read(ctx, scope, tasks, parts[1], 0, &t); e != nil {
		return e
	}
	if s.ports.Execution == nil {
		return s.wait(ctx, store, scope, work)
	}
	var window api.ControlSnapshot
	err := s.transaction(ctx, store, scope, func(tx runtime.Tx) error {
		if e := tx.Guard(ctx, work.Claim); e != nil {
			return e
		}
		current, e := getTask(ctx, tx, t.Task.TaskID)
		if e != nil {
			return e
		}
		now, e := tx.Now(ctx)
		if e != nil {
			return e
		}
		window, e = s.controlSnapshot(ctx, tx, current, now)
		return e
	})
	if err != nil {
		return err
	}
	if e := s.preIO(ctx, store, scope, work); e != nil {
		return e
	}
	if e := s.ports.Execution.Control(ctx, scope, parts[2], window); e != nil {
		if deferred(e) {
			return s.wait(ctx, store, scope, work)
		}
		return e
	}
	return s.finish(ctx, store, scope, work, runtime.Done(), nil)
}
func (s *Service) billingJob(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	var reservation Reservation
	if _, e := store.Read(ctx, scope, reservations, work.Job.SourceRef.ObjectID, 0, &reservation); e != nil {
		return e
	}
	var usage api.UsageSnapshot
	var err error
	if e := s.preIO(ctx, store, scope, work); e != nil {
		return e
	}
	switch reservation.SourceKind {
	case "brain_decision":
		if s.ports.Brain == nil {
			return s.wait(ctx, store, scope, work)
		}
		usage, err = s.ports.Brain.Usage(ctx, scope, reservation.SourceRef)
	case "execution_operation":
		if s.ports.Execution == nil {
			return s.wait(ctx, store, scope, work)
		}
		usage, err = s.ports.Execution.Usage(ctx, scope, reservation.SourceRef)
	case "budget_allocation":
		return s.wait(ctx, store, scope, work)
	default:
		return s.wait(ctx, store, scope, work)
	}
	if err != nil {
		if deferred(err) {
			return s.wait(ctx, store, scope, work)
		}
		return err
	}
	disposition := runtime.Waiting(time.Now().Add(time.Second))
	if usage.SpendingClosed && usage.UsageFinal {
		disposition = runtime.Done()
	}
	return s.finish(ctx, store, scope, work, disposition, func(tx runtime.Tx) error {
		_, e := s.ReconcileUsageTx(ctx, tx, serviceAuth(scope), reservation.SourceKind, usage)
		return e
	})
}
func (s *Service) publishResultJob(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	var publication resultPublication
	if _, e := store.Read(ctx, scope, publications, work.Job.SourceRef.ObjectID, 0, &publication); e != nil {
		return e
	}
	if publication.State == "published" {
		return s.finish(ctx, store, scope, work, runtime.Done(), nil)
	}
	if s.ports.Content == nil {
		return s.wait(ctx, store, scope, work)
	}
	var result api.Result
	if _, e := store.Read(ctx, scope, results, publication.ResultRef.ObjectID, publication.ResultRef.Revision, &result); e != nil {
		return e
	}
	if e := s.preIO(ctx, store, scope, work); e != nil {
		return e
	}
	ref, err := s.ports.Content.Publish(ctx, scope, publication.UploadID, "application/json", api.Raw(result))
	if err != nil {
		if deferred(err) {
			return s.wait(ctx, store, scope, work)
		}
		return err
	}
	return s.finish(ctx, store, scope, work, runtime.Done(), func(tx runtime.Tx) error {
		var current resultPublication
		rev, e := tx.Get(ctx, publications, result.ResultID, &current)
		if e != nil {
			return e
		}
		if current.State == "published" {
			if current.ContentRef == nil || !api.Equal(*current.ContentRef, ref) {
				return api.E("idempotency_conflict", "publication_changed")
			}
			return nil
		}
		current.State = "published"
		current.ContentRef = &ref
		current.Revision++
		return tx.Put(ctx, publications, result.ResultID, rev, current)
	})
}
func (s *Service) steerJob(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	var pending pendingSteer
	if _, e := store.Read(ctx, scope, steers, work.Job.SourceRef.ObjectID, 0, &pending); e != nil {
		return e
	}
	if pending.State != "pending" {
		return s.finish(ctx, store, scope, work, runtime.Done(), nil)
	}
	var t taskState
	if _, e := store.Read(ctx, scope, tasks, pending.Input.TaskID, 0, &t); e != nil {
		return e
	}
	if terminal(t) || t.PendingGoalCommand != pending.CommandID {
		return s.finish(ctx, store, scope, work, runtime.Done(), func(tx runtime.Tx) error {
			pending.State = "closed"
			pending.Revision++
			if e := tx.Put(ctx, steers, pending.CommandID, pending.Revision-1, pending); e != nil {
				return e
			}
			return runtime.Decide(ctx, tx, pending.CommandID, nil, api.E("invalid_state", "target_terminal"))
		})
	}
	if s.ports.Content == nil {
		return s.wait(ctx, store, scope, work)
	}
	document := api.GoalDocument{FormatVersion: 1, InitialGoalRef: t.InitialGoalRef, AmendmentRefs: append(append([]api.ContentRef{}, t.Amendments...), pending.Input.AmendmentRef)}
	if e := s.preIO(ctx, store, scope, work); e != nil {
		return e
	}
	ref, err := s.ports.Content.Publish(ctx, scope, pending.UploadID, "application/json", api.Raw(document))
	if err != nil {
		if deferred(err) {
			return s.wait(ctx, store, scope, work)
		}
		return err
	}
	return s.finish(ctx, store, scope, work, runtime.Done(), func(tx runtime.Tx) error {
		current, e := getTask(ctx, tx, pending.Input.TaskID)
		if e != nil {
			return e
		}
		if terminal(current) || current.PendingGoalCommand != pending.CommandID || current.Task.GoalRevision != pending.Input.BaseGoalRevision {
			return api.E("revision_conflict", "goal_changed")
		}
		current.Task.GoalRef = ref
		current.Amendments = document.AmendmentRefs
		current.SourceRefs = append(current.SourceRefs, api.SourceEvidence{ContentRef: pending.Input.AmendmentRef, SourceKind: "user_input", SubmissionRef: &pending.Input.SourceSubmissionRef})
		current.PendingGoalCommand = ""
		if e = s.reviseGoal(ctx, tx, &current, pending.Input.SourceSubmissionRef); e != nil {
			return e
		}
		pending.State = "applied"
		pending.Revision++
		if e = tx.Put(ctx, steers, pending.CommandID, pending.Revision-1, pending); e != nil {
			return e
		}
		return runtime.Decide(ctx, tx, pending.CommandID, output(tx, current), nil)
	})
}
func (s *Service) childPrepareJob(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	var h ChildHandle
	if _, e := store.Read(ctx, scope, children, work.Job.SourceRef.ObjectID, 0, &h); e != nil {
		return e
	}
	if h.ChildSessionRef != nil || h.State == "open" {
		return s.finish(ctx, store, scope, work, runtime.Done(), nil)
	}
	if s.ports.Collaboration == nil {
		return s.wait(ctx, store, scope, work)
	}
	if e := s.preIO(ctx, store, scope, work); e != nil {
		return e
	}
	ref, err := s.ports.Collaboration.CreateSession(ctx, scope, h)
	if err != nil {
		if deferred(err) {
			return s.wait(ctx, store, scope, work)
		}
		return err
	}
	if ref.TenantID != scope.TenantID || ref.OwnerID != h.SessionOwnerID || !api.ValidID(ref.ObjectID) || ref.Revision == 0 {
		return api.E("forbidden", "child_session_scope_mismatch")
	}
	return s.finish(ctx, store, scope, work, runtime.Done(), func(tx runtime.Tx) error {
		var current ChildHandle
		rev, e := tx.Get(ctx, children, h.ChildID, &current)
		if e != nil {
			return e
		}
		if current.ChildSessionRef != nil {
			if refKey(*current.ChildSessionRef) != refKey(ref) {
				return api.E("idempotency_conflict", "child_session_mapping_changed")
			}
			return nil
		}
		current.ChildSessionRef = &ref
		if current.State == "preparing" {
			current.State = "open"
		}
		current.Revision++
		if e = tx.Put(ctx, children, current.ChildID, rev, current); e != nil {
			return e
		}
		return runtime.Decide(ctx, tx, workCommandID(ctx, tx, current.ChildID), ChildOutput{ChildRef: scope.Ref(current.ChildID, current.Revision), ChildSessionRef: &ref}, nil)
	})
}

// 原 child.create 的调用方命令与远端 session.create 命令分别保留。
func workCommandID(ctx context.Context, tx runtime.Tx, childID string) string {
	var p childCommand
	_, err := tx.Get(ctx, childCommands, childID, &p)
	if err != nil {
		return ""
	}
	return p.CommandID
}

const childCommands = "task.child_commands"

type childCommand struct {
	CommandID string `json:"command_id"`
}

func (s *Service) transferJob(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	var tr Transfer
	if _, e := store.Read(ctx, scope, transfers, work.Job.SourceRef.ObjectID, 0, &tr); e != nil {
		return e
	}
	if tr.State == "closed" || tr.State == "applied" {
		return s.finish(ctx, store, scope, work, runtime.Done(), nil)
	}
	if s.ports.Collaboration == nil {
		return s.wait(ctx, store, scope, work)
	}
	if e := s.preIO(ctx, store, scope, work); e != nil {
		return e
	}
	if e := s.ports.Collaboration.Transfer(ctx, scope, tr); e != nil {
		if deferred(e) {
			return s.wait(ctx, store, scope, work)
		}
		return e
	}
	return s.finish(ctx, store, scope, work, runtime.Done(), func(tx runtime.Tx) error {
		var current Transfer
		rev, e := tx.Get(ctx, transfers, tr.TransferID, &current)
		if e != nil {
			return e
		}
		if current.State == "closed" {
			return nil
		}
		current.State = "applied"
		current.Revision++
		return tx.Put(ctx, transfers, tr.TransferID, rev, current)
	})
}
func (s *Service) delegationJob(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	var d Delegation
	if _, e := store.Read(ctx, scope, delegations, work.Job.SourceRef.ObjectID, 0, &d); e != nil {
		return e
	}
	if d.Internal {
		return s.internalDelegationJob(ctx, store, scope, work, d)
	}
	if s.ports.Collaboration == nil {
		return s.wait(ctx, store, scope, work)
	}
	var a Allocation
	if _, e := store.Read(ctx, scope, allocations, d.AllocationRef.ObjectID, 0, &a); e != nil {
		return e
	}
	if d.CloseRequested {
		if e := s.preIO(ctx, store, scope, work); e != nil {
			return e
		}
		if e := s.ports.Collaboration.CloseAllocation(ctx, scope, a); e != nil {
			if deferred(e) {
				return s.wait(ctx, store, scope, work)
			}
			return e
		}
		if !d.Sent {
			return s.finish(ctx, store, scope, work, runtime.Done(), func(tx runtime.Tx) error {
				return s.MergeDelegationTx(ctx, tx, serviceAuth(scope), DelegationFact{DelegationID: d.DelegationID, Revision: d.SourceRevision + 1, GoalWorkClosed: true, EffectsClosed: true, TransfersClosed: true, Gaps: []string{"allocation_closure_pending"}})
			})
		}
		if d.ChildTaskRef != nil {
			if e := s.ports.Collaboration.Control(ctx, scope, d, "cancel"); e != nil {
				if deferred(e) {
					return s.wait(ctx, store, scope, work)
				}
				return e
			}
		}
	}
	var fact DelegationFact
	var err error
	if d.ChildTaskRef == nil && !d.CloseRequested {
		err = s.transaction(ctx, store, scope, func(tx runtime.Tx) error {
			if e := tx.Guard(ctx, work.Claim); e != nil {
				return e
			}
			var current Delegation
			if _, e := tx.Get(ctx, delegations, d.DelegationID, &current); e != nil {
				return e
			}
			if current.CloseRequested {
				return api.E("invalid_state", "delegation_closed")
			}
			t, e := getTask(ctx, tx, d.ParentTaskRef.ObjectID)
			if e != nil {
				return e
			}
			if e = s.CheckCurrent(ctx, tx, t, true); e != nil {
				return e
			}
			if current.ParentGoalRevision != t.Task.GoalRevision {
				return api.E("revision_conflict", "parent_goal_changed")
			}
			current.Sent = true
			current.Revision++
			return tx.Put(ctx, delegations, current.DelegationID, current.Revision-1, current)
		})
		if err != nil {
			return err
		}
		if e := s.preIO(ctx, store, scope, work); e != nil {
			return e
		}
		fact, err = s.ports.Collaboration.Create(ctx, scope, d, a)
	} else {
		if e := s.preIO(ctx, store, scope, work); e != nil {
			return e
		}
		fact, err = s.ports.Collaboration.Read(ctx, scope, d)
	}
	if err != nil {
		if deferred(err) {
			return s.wait(ctx, store, scope, work)
		}
		return err
	}
	disposition := runtime.Waiting(time.Now().Add(time.Second))
	if fact.GoalWorkClosed && fact.EffectsClosed {
		disposition = runtime.Done()
	}
	return s.finish(ctx, store, scope, work, disposition, func(tx runtime.Tx) error { return s.MergeDelegationTx(ctx, tx, serviceAuth(scope), fact) })
}
func (s *Service) internalDelegationJob(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work, d Delegation) error {
	if d.ChildTaskRef == nil {
		return api.E("dependency_unavailable", "internal_child_missing")
	}
	var child taskState
	if _, e := store.Read(ctx, scope, tasks, d.ChildTaskRef.ObjectID, 0, &child); e != nil {
		return e
	}
	return s.finish(ctx, store, scope, work, runtime.Waiting(time.Now().Add(time.Second)), func(tx runtime.Tx) error {
		current, e := getTask(ctx, tx, child.Task.TaskID)
		if e != nil {
			return e
		}
		if d.CloseRequested && !terminal(current) {
			current.Task.Status = "cancelled"
			current.Task.ControlRevision++
			if e = s.closeUnsent(ctx, tx, &current); e != nil {
				return e
			}
			if e = s.saveTask(ctx, tx, &current); e != nil {
				return e
			}
			if e = s.controlJobs(ctx, tx, current); e != nil {
				return e
			}
		}
		if d.CloseRequested || terminal(current) {
			var inc IncomingAllocation
			if _, e = tx.Get(ctx, incoming, current.IncomingAllocationID, &inc); e != nil {
				return e
			}
			if inc.Gate == "open" {
				inc.Gate = "closing"
				inc.Revision++
				if e = tx.Put(ctx, incoming, current.IncomingAllocationID, inc.Revision-1, inc); e != nil {
					return e
				}
			}
			if e = s.refreshIncomingTx(ctx, tx, current); e != nil {
				return e
			}
		}
		rows, e := s.fullRelations(ctx, tx, current.Task.TaskID)
		if e != nil {
			return e
		}
		effectsClosed := true
		for _, r := range rows {
			if r.Kind == "operation" && (!r.Closed || r.MayApplyLater || r.Effect == "unknown") {
				effectsClosed = false
			}
			if r.Kind == "delegation" && !r.Closed {
				effectsClosed = false
			}
		}
		fact := DelegationFact{DelegationID: d.DelegationID, Revision: current.Task.Revision, ChildTaskRef: d.ChildTaskRef, GoalWorkClosed: terminal(current), EffectsClosed: effectsClosed, TransfersClosed: true, UsageFinal: !current.Task.AccountingOpen, Gaps: []string{}}
		if current.IncomingAllocationID != "" {
			var inc IncomingAllocation
			if _, e = tx.Get(ctx, incoming, current.IncomingAllocationID, &inc); e != nil {
				return e
			}
			if inc.ClosureRef != nil && fact.GoalWorkClosed && fact.EffectsClosed && !current.Task.AccountingOpen {
				var allocationClosure api.AllocationClosure
				if e = tx.GetVersion(ctx, closures, inc.ClosureRef.ObjectID, inc.ClosureRef.Revision, &allocationClosure); e != nil {
					return e
				}
				if e = s.ReconcileClosureTx(ctx, tx, serviceAuth(scope), d.AllocationRef.ObjectID, allocationClosure); e != nil {
					return e
				}
				closureKey := d.DelegationID + "/" + fmt.Sprintf("%020d", inc.UsageRevision)
				binding, e := tx.LookupKey(ctx, delegationClosures, closureKey)
				var closureRef api.ObjectRef
				if confirmedNotFound(e) {
					closureID := api.NewID("closure")
					c := DelegationClosure{DelegationID: d.DelegationID, Revision: 1, GoalWorkClosed: true, EffectsClosed: true, AllocationClosureRef: *inc.ClosureRef, TransfersClosed: true, ProofRefs: []api.ContentRef{allocationClosure.ProofRef}, ClosedAt: allocationClosure.ClosedAt}
					if e = tx.Create(ctx, delegationClosures, closureID, d.ParentTaskRef.ObjectID, c); e != nil {
						return e
					}
					digest, e := api.Digest(c)
					if e != nil {
						return e
					}
					if e = tx.Bind(ctx, delegationClosures, closureKey, closureID, digest); e != nil {
						return e
					}
					closureRef = scope.Ref(closureID, 1)
				} else if e != nil {
					return e
				} else {
					closureRef = scope.Ref(binding.ObjectID, 1)
				}
				fact.ClosureRef = &closureRef
			}
		}
		return s.MergeDelegationTx(ctx, tx, serviceAuth(scope), fact)
	})
}
func (s *Service) allocationJob(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	parts := strings.SplitN(work.Job.ResponsibilityKey, "/", 2)
	if len(parts) != 2 {
		return invalid("allocation_job_key")
	}
	if parts[0] == "settle" {
		var a Allocation
		if _, e := store.Read(ctx, scope, allocations, parts[1], 0, &a); e != nil {
			return e
		}
		if a.ClosureRef == nil || s.ports.Collaboration == nil {
			return s.wait(ctx, store, scope, work)
		}
		if e := s.preIO(ctx, store, scope, work); e != nil {
			return e
		}
		closure, err := s.ports.Collaboration.ReadClosure(ctx, scope, *a.ClosureRef)
		if err != nil {
			if deferred(err) {
				return s.wait(ctx, store, scope, work)
			}
			return err
		}
		return s.finish(ctx, store, scope, work, runtime.Done(), func(tx runtime.Tx) error {
			return s.ReconcileClosureTx(ctx, tx, serviceAuth(scope), a.AllocationID, closure)
		})
	}
	if parts[0] == "incoming" {
		var a IncomingAllocation
		if _, e := store.Read(ctx, scope, incoming, parts[1], 0, &a); e != nil {
			return e
		}
		if a.Gate == "closed" {
			return s.finish(ctx, store, scope, work, runtime.Done(), nil)
		}
		if a.TaskRef == nil {
			return s.wait(ctx, store, scope, work)
		}
		return s.finish(ctx, store, scope, work, runtime.Waiting(time.Now().Add(time.Second)), func(tx runtime.Tx) error {
			t, e := getTask(ctx, tx, a.TaskRef.ObjectID)
			if e != nil {
				return e
			}
			return s.refreshIncomingTx(ctx, tx, t)
		})
	}
	if parts[0] == "correction" {
		var a IncomingAllocation
		if _, e := store.Read(ctx, scope, incoming, parts[1], 0, &a); e != nil {
			return e
		}
		if a.ClosureRef == nil {
			return s.wait(ctx, store, scope, work)
		}
		reporter, ok := s.ports.Collaboration.(AllocationReporter)
		if !ok {
			return s.wait(ctx, store, scope, work)
		}
		var closure api.AllocationClosure
		if _, e := store.Read(ctx, scope, closures, a.ClosureRef.ObjectID, a.ClosureRef.Revision, &closure); e != nil {
			return e
		}
		if e := s.preIO(ctx, store, scope, work); e != nil {
			return e
		}
		if e := reporter.ReportClosure(ctx, scope, a.ParentOwner, *a.ClosureRef, closure); e != nil {
			if deferred(e) {
				return s.wait(ctx, store, scope, work)
			}
			return e
		}
		return s.finish(ctx, store, scope, work, runtime.Done(), nil)
	}
	return invalid("allocation_job_kind")
}
