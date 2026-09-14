package tasks

import (
	"context"
	"lerna/authorization"
	"reflect"
)

// ExecutionReport is admitted only by the trusted execution consumer. The
// reference identifies controlled output/evidence, never arbitrary result text.
type ExecutionReport struct {
	CompletedAt                      int64
	PreStart                         bool
	Conflict                         bool
	OperationID                      string
	Qualification                    Qualification
	Revision                         uint64
	Phase, Result, Effect, Reference string
}

// GuardExecution participates in the execution authority's local transaction.
// Each already-budgeted, effect-aware work generation may start ONE operation.
func (p *WorkPort) GuardExecution(tx authorization.RuntimeTransaction, q Qualification, op string, consume bool) error {
	return p.service.runtime(tx, func(j *journal, tx authorization.RuntimeTransaction) error {
		r, ok := j.Runs[q.Ref.TaskID]
		if !ok || q.Ref.Namespace != p.service.config.Namespace {
			return failure(authorization.Denied)
		}
		id, e := tx.Authorize(p.binding.Token, r.Task.Resource, "task.execute")
		if e != nil {
			return e
		}
		if id.Subject != p.binding.Subject || r.Task.Subject != id.Subject {
			return failure(authorization.Denied)
		}
		if r.Actions != nil {
			return p.guardAction(j, tx, &r, q, op, consume)
		}
		if len(r.Work) != 1 || QualificationOf(r) != q || r.Task.State != "RUNNING" || controlIntent(r.Task) != "RUN" || r.Task.Constraints.ModelRequests != 0 {
			return failure(authorization.Conflict)
		}
		w := &r.Work[0]
		if !w.EffectAware || !p.binding.AllowEffectEvidence || !w.InFlight || w.Worker != p.binding.WorkerID || w.LeaseUntil <= tx.Now().UnixNano() || r.Task.Constraints.DeadlineUnix <= tx.Now().Unix() || r.Task.Attempts > r.Task.Constraints.MaxSteps || r.Limits != p.limits {
			return failure(authorization.Conflict)
		}
		if w.ExecutionOperation != "" && w.ExecutionOperation != op {
			return failure(authorization.Conflict)
		}
		if consume {
			if w.ExecutionOperation != "" {
				return failure(authorization.Conflict)
			}
			w.ExecutionOperation = op
			j.Runs[q.Ref.TaskID] = r
		}
		return nil
	})
}
func (p *WorkPort) ConsumeExecution(ctx context.Context, in ExecutionReport) error {
	return p.service.store.UpdateRuntime(ctx, func(tx authorization.RuntimeTransaction) error { return p.ConsumeExecutionIn(tx, in) })
}

// ConsumeExecutionIn applies a trusted execution report inside the caller's
// runtime transaction, allowing reliable consumption and its reply to commit
// with the task. It performs no external I/O.
func (p *WorkPort) ConsumeExecutionIn(tx authorization.RuntimeTransaction, in ExecutionReport) error {
	if in.OperationID == "" || in.Revision < 1 || in.Revision > 32 || len(in.Reference) > 256 {
		return failure(authorization.Invalid)
	}
	if in.Phase != "IN_PROGRESS" && in.Phase != "UNKNOWN" && in.Phase != "FINISHED" && in.Phase != "NOT_STARTED" {
		return failure(authorization.Invalid)
	}
	if in.Result != "SUCCESS" && in.Result != "FAILURE" && in.Result != "UNKNOWN" {
		return failure(authorization.Invalid)
	}
	if in.Effect != "CONFIRMED" && in.Effect != "NOT_OCCURRED" && in.Effect != "UNKNOWN" {
		return failure(authorization.Invalid)
	}
	if in.Effect == "CONFIRMED" && (in.Phase != "FINISHED" || (in.Result == "SUCCESS" && in.Reference == "")) {
		return failure(authorization.Invalid)
	}
	return p.service.runtime(tx, func(j *journal, tx authorization.RuntimeTransaction) error {
		q := in.Qualification
		r, ok := j.Runs[q.Ref.TaskID]
		if !ok || q.Ref.Namespace != p.service.config.Namespace {
			return failure(authorization.Denied)
		}
		id, e := tx.Authorize(p.binding.Token, r.Task.Resource, "task.reconcile")
		if e != nil {
			return e
		}
		if id.Subject != p.binding.Subject || r.Task.Subject != id.Subject {
			return failure(authorization.Denied)
		}
		if old, ok := r.ExecutionReports[in.OperationID]; ok {
			if in.Revision < old.Revision {
				return nil
			}
			if in.Revision == old.Revision {
				if !reflect.DeepEqual(old, in) {
					return failure(authorization.IdentityConflict)
				}
				return nil
			}
			if old.Effect != "UNKNOWN" && old.Effect != in.Effect && !in.Conflict {
				return failure(authorization.Conflict)
			}
		}
		if len(r.ExecutionReports) >= 32 {
			return failure(authorization.Unavailable)
		}
		if r.ExecutionReports == nil {
			r.ExecutionReports = map[string]ExecutionReport{}
		}
		// Original fact always survives a task version change. Only its own worker
		// generation may be released, and only the current goal may be completed.
		r.ExecutionReports[in.OperationID] = in
		if r.Actions != nil {
			return p.consumeAction(j, tx, &r, in)
		}
		currentDecision := r.Task.Version == q.Version || r.Task.Version == r.ExecutionReportVersions[in.OperationID]
		w := &r.Work[0]
		originalWork := QualificationOf(r)
		originalWork.Version = q.Version
		if in.PreStart && w.ExecutionOperation == "" && originalWork == q {
			w.ExecutionOperation = in.OperationID
		}
		if w.Generation == q.Generation && w.ID == q.WorkID && w.Worker == p.binding.WorkerID && w.ExecutionOperation == in.OperationID && !terminal(r.Task) {
			if in.Effect == "UNKNOWN" {
				r.Task.State = "WAITING"
				addWait(&r.Task, "reconciliation")
				syncWait(&r.Task)
			} else {
				w.InFlight = false
				w.LeaseUntil = 0
				removeWait(&r.Task, "reconciliation")
				completedBeforeControl := in.CompletedAt > 0 && in.CompletedAt <= tx.Now().UnixNano() && in.CompletedAt <= r.Task.Control.AcceptedAt && r.UpdateVersion <= q.Version
				if in.Effect == "CONFIRMED" && !in.Conflict && (currentDecision && controlIntent(r.Task) == "RUN" || completedBeforeControl) && in.Result == "SUCCESS" && tx.Now().Unix() < r.Task.Constraints.DeadlineUnix {
					r.Task.State = "COMPLETED"
					r.Task.Result = in.Reference
					r.Task.WaitingReasons = nil
					r.Task.StopReason = ""
					if r.Task.Control.OperationID != "" {
						r.Task.Control.Progress = "NOT_PREVENTED"
					}
					w.Done = true
				} else {
					settleControl(&r, tx, p.binding.Token)
				}
			}
			r.Task.Version++
			if r.ExecutionReportVersions == nil {
				r.ExecutionReportVersions = map[string]uint64{}
			}
			if currentDecision {
				r.ExecutionReportVersions[in.OperationID] = r.Task.Version
			} else {
				r.ExecutionReportVersions[in.OperationID] = 0
			}
		}
		r.Records = append(r.Records, Record{Kind: "execution-report", Version: r.Task.Version})
		j.Runs[q.Ref.TaskID] = r
		return nil
	})
}
