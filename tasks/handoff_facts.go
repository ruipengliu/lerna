package tasks

import (
	"context"
	"encoding/json"
	"lerna/authorization"
	"reflect"
)

func handoffOriginalReport(r RunSnapshot, in ExecutionReport) bool {
	if q, ok := r.ExecutionOrigins[in.OperationID]; ok && q == in.Qualification {
		return true
	}
	if r.Task.Ref != in.Qualification.Ref || r.Task.Owner != in.Qualification.Owner || r.Task.OwnerEpoch != in.Qualification.Epoch {
		return false
	}
	for _, w := range r.Work {
		if w.ExecutionOperation == in.OperationID && w.ID == in.Qualification.WorkID && w.Generation == in.Qualification.Generation {
			return true
		}
	}
	if r.Actions != nil {
		for _, a := range r.Actions.Actions {
			if a.OperationID == in.OperationID && a.Qualification == in.Qualification {
				return true
			}
		}
	}
	return false
}
func queueHandoffReport(j *journal, r RunSnapshot, in ExecutionReport) error {
	h := j.Handoffs[r.Task.Ref.TaskID]
	if !handoffOriginalReport(r, in) {
		return failure(authorization.IdentityConflict)
	}
	for _, old := range h.Reports {
		if old.OperationID == in.OperationID && old.Revision == in.Revision {
			if !reflect.DeepEqual(old, in) {
				return failure(authorization.IdentityConflict)
			}
			return nil
		}
	}
	if len(h.Reports) >= 32 {
		return failure(authorization.Unavailable)
	}
	h.Reports = append(h.Reports, in)
	j.Handoffs[r.Task.Ref.TaskID] = h
	return nil
}
func migratedReport(j *journal, r RunSnapshot, in ExecutionReport) bool {
	h := j.Handoffs[r.Task.Ref.TaskID]
	if h.Phase != "ACTIVE" {
		return false
	}
	var c handoffCheckpoint
	return json.Unmarshal(h.Checkpoint, &c) == nil && handoffOriginalReport(c.Run, in)
}

// ponytail: retain at most 32 facts per handoff; add acknowledged compaction
// before supporting longer-lived transfers. Never discard outstanding facts.
// Facts retains its bounded source queue even after successful forwarding.
// Repeated delivery is safe through the original operation/revision identity.
func (p *HandoffPort) Facts(ctx context.Context, ref Ref, op string) (HandoffEnvelope, error) {
	h, e := p.Status(ctx, ref, op)
	if e != nil {
		return HandoffEnvelope{}, e
	}
	if h.Phase != "SEALED" {
		return HandoffEnvelope{}, failure(authorization.Conflict)
	}
	var c handoffCheckpoint
	if json.Unmarshal(h.Checkpoint, &c) != nil {
		return HandoffEnvelope{}, failure(authorization.Invalid)
	}
	if e = p.checkReports(ctx, "facts", c.Run, h.Reports, h.Request.Target); e != nil {
		return HandoffEnvelope{}, e
	}
	m := handoffMessageOf(h, "facts")
	m.Checkpoint, e = json.Marshal(h.Reports)
	if e != nil {
		return HandoffEnvelope{}, e
	}
	return p.sign(m)
}
func (p *HandoffPort) ReceiveFacts(ctx context.Context, in HandoffEnvelope, w *WorkPort) error {
	m, e := p.verify(in, "facts")
	if e != nil {
		return e
	}
	if w == nil || w.service.config != p.service.config {
		return failure(authorization.Denied)
	}
	h, e := p.Status(ctx, m.Request.Ref, m.Request.OperationID)
	if e != nil {
		return e
	}
	if !matchesHandoff(h, m) || h.Phase != "ACTIVE" || m.Request.Target != p.service.config.Owner {
		return failure(authorization.Conflict)
	}
	var reports []ExecutionReport
	var c handoffCheckpoint
	if json.Unmarshal(m.Checkpoint, &reports) != nil || len(reports) > 32 || json.Unmarshal(h.Checkpoint, &c) != nil {
		return failure(authorization.Invalid)
	}
	if e = p.checkReports(ctx, "receive-facts", c.Run, reports, m.Source); e != nil {
		return e
	}
	for _, r := range reports {
		if !handoffOriginalReport(c.Run, r) {
			return failure(authorization.IdentityConflict)
		}
		if e = w.ConsumeExecution(ctx, r); e != nil {
			return e
		}
	}
	return nil
}

// Check every retained revision, including superseded reports with distinct
// references. Collapsing them by operation would hide data from disclosure policy.
func (p *HandoffPort) checkReports(ctx context.Context, phase string, r RunSnapshot, reports []ExecutionReport, peer string) error {
	ctx, cancel := context.WithTimeout(ctx, p.config.IOTimeout)
	defer cancel()
	if e := p.check(ctx, phase, r, peer); e != nil {
		return e
	}
	for _, report := range reports {
		r.ExecutionReports = map[string]ExecutionReport{report.OperationID: report}
		if e := p.check(ctx, phase, r, peer); e != nil {
			return e
		}
	}
	return nil
}

// DrainAborted reconciles facts accepted during preparation after a safe abort.
func (p *HandoffPort) DrainAborted(ctx context.Context, ref Ref, op string, w *WorkPort) error {
	h, e := p.Status(ctx, ref, op)
	if e != nil {
		return e
	}
	if h.Phase != "ABORTED" || w == nil || w.service.config != p.service.config {
		return failure(authorization.Conflict)
	}
	r, e := p.authorizedRun(ctx, ref)
	if e != nil {
		return e
	}
	if e = p.checkReports(ctx, "aborted-facts", r, h.Reports, h.Source); e != nil {
		return e
	}
	for _, report := range h.Reports {
		if e = w.ConsumeExecution(ctx, report); e != nil {
			return e
		}
	}
	return nil
}
