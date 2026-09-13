package tasks

import "lerna/authorization"

func (p *WorkPort) guardAction(j *journal, tx authorization.RuntimeTransaction, r *RunSnapshot, q Qualification, op string, consume bool) error {
	recovery := p.actionRecovery == QualificationOf(*r)
	if !consume && !recovery {
		recovery = p.ownsRecoveryStart(*r, q, op)
	}
	canRecover := recovery && r.Task.State == "WAITING" && len(r.Task.WaitingReasons) == 1 && r.Task.WaitingReasons[0] == "reconciliation"
	if (QualificationOf(*r) != q && !recovery) || (r.Task.State != "RUNNING" && !canRecover) || controlIntent(r.Task) != "RUN" || r.Limits != p.limits || !p.binding.AllowEffectEvidence {
		return failure(authorization.Conflict)
	}
	w := &r.Work[0]
	if !w.InFlight || w.Worker != p.binding.WorkerID || !w.EffectAware || w.LeaseUntil <= tx.Now().UnixNano() || r.Task.Constraints.DeadlineUnix <= tx.Now().Unix() {
		return failure(authorization.Conflict)
	}
	for i, x := range r.Actions.Actions {
		if x.OperationID != op {
			continue
		}
		if x.Qualification != q || x.Status != "DISPATCHED" || r.UpdateVersion > x.BaseVersion || w.ExecutionOperation != "" && w.ExecutionOperation != op {
			return failure(authorization.Conflict)
		}
		if consume {
			if w.ExecutionOperation != "" {
				return failure(authorization.Conflict)
			}
			if canRecover {
				r.Actions.Actions[i].RecoveryStart = p.actionRecovery
				r.Task.State = "RUNNING"
				r.Task.StopReason = ""
				removeWait(&r.Task, "reconciliation")
				r.Task.Version++
			}
			r.Actions.Actions[i].ExecutionVersion = r.Task.Version
			w.ExecutionOperation = op
			j.Runs[q.Ref.TaskID] = *r
		}
		return nil
	}
	return failure(authorization.Denied)
}

// GuardActionRequest binds the actual execution payload to Core's admitted
// action. Execution invokes this optional stronger gate for action-aware Core.
type ActionBinding struct {
	Qualification                     Qualification
	OperationID, Descriptor, InputRef string
	ResourceVersion, ControlVersion   uint64
}

func (p *WorkPort) GuardActionRequest(tx authorization.RuntimeTransaction, in ActionBinding) error {
	q := in.Qualification
	return p.service.runtime(tx, func(j *journal, tx authorization.RuntimeTransaction) error {
		r, ok := j.Runs[q.Ref.TaskID]
		if !ok || q.Ref.Namespace != p.service.config.Namespace {
			return failure(authorization.Denied)
		}
		if r.Actions == nil {
			return nil
		}
		for _, a := range r.Actions.Actions {
			if matchesActionBinding(a, in) {
				return nil
			}
		}
		return failure(authorization.IdentityConflict)
	})
}

func matchesActionBinding(a Action, in ActionBinding) bool {
	return (ActionBinding{Qualification: a.Qualification, OperationID: a.OperationID, Descriptor: a.Descriptor, InputRef: a.InputRef, ResourceVersion: a.ResourceVersion, ControlVersion: a.ControlVersion}) == in
}

func (p *WorkPort) consumeAction(j *journal, tx authorization.RuntimeTransaction, r *RunSnapshot, in ExecutionReport) error {
	var action *Action
	for i := range r.Actions.Actions {
		if r.Actions.Actions[i].OperationID == in.OperationID {
			action = &r.Actions.Actions[i]
			break
		}
	}
	if action == nil || action.Qualification != in.Qualification || action.Status == "READY" || action.Status == "SKIPPED" {
		return failure(authorization.Conflict)
	}
	w := &r.Work[0]
	// Late facts stay attached to their original action; they cannot complete a
	// revised goal or reopen a paused/cancelled/terminal task.
	effectiveVersion := action.ExecutionVersion
	if effectiveVersion == 0 {
		effectiveVersion = action.Qualification.Version
	}
	current := r.UpdateVersion <= action.BaseVersion && (r.Task.Version == effectiveVersion || r.Task.Version == r.ExecutionReportVersions[in.OperationID])
	if in.Effect == "UNKNOWN" {
		if !terminal(r.Task) {
			r.Task.State = "WAITING"
			addWait(&r.Task, "reconciliation")
			syncWait(&r.Task)
		}
	} else {
		action.Status = "DONE"
		if in.Result != "SUCCESS" || in.Conflict {
			r.Actions.NeedsCorrection = true
		}
		if in.Conflict && action.Write {
			r.Actions.WrongWrite = true
		}
		if w.ExecutionOperation == in.OperationID {
			w.ExecutionOperation = ""
		}
		if !terminal(r.Task) {
			removeWait(&r.Task, "reconciliation")
			if current && controlIntent(r.Task) == "RUN" && len(r.Task.WaitingReasons) == 0 && r.Task.Constraints.DeadlineUnix > tx.Now().Unix() {
				r.Task.State = "RUNNING"
				r.Task.StopReason = ""
			} else {
				w.InFlight = false
				w.LeaseUntil = 0
				settleControl(r, tx, p.binding.Token)
			}
		}
	}
	r.Task.Version++
	if r.ExecutionReportVersions == nil {
		r.ExecutionReportVersions = map[string]uint64{}
	}
	if current {
		r.ExecutionReportVersions[in.OperationID] = r.Task.Version
	} else {
		r.ExecutionReportVersions[in.OperationID] = 0
	}
	r.Records = append(r.Records, Record{Kind: "action:execution-fact", Version: r.Task.Version})
	j.Runs[r.Task.Ref.TaskID] = *r
	return nil
}

// ownsRecoveryStart permits this exact host to observe the execution it just
// started. It grants no new start and cannot cross another task or lease change.
func (p *WorkPort) ownsRecoveryStart(r RunSnapshot, q Qualification, op string) bool {
	start := p.actionRecovery
	if start == (Qualification{}) || start.Version == ^uint64(0) || r.Task.State != "RUNNING" || len(r.Work) != 1 || r.Work[0].ExecutionOperation != op {
		return false
	}
	next := start
	next.Version++
	if next != QualificationOf(r) {
		return false
	}
	for _, action := range r.Actions.Actions {
		if action.OperationID == op && action.Qualification == q && action.RecoveryStart == start && action.ExecutionVersion == next.Version {
			return true
		}
	}
	return false
}
