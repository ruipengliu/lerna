package tasks

import (
	"context"
	"lerna/authorization"
	"strconv"
)

// EnsureLease recovers the same worker's durable plan after lease expiry or an
// answered interaction. It increments the worker generation, fencing old ports;
// immutable invocation qualifications and original model records are retained.
func (p *ActionPort) EnsureLease(ctx context.Context, q Qualification) (RunSnapshot, error) {
	var out RunSnapshot
	e := p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		r, e := p.current(j, tx, q, false)
		if e != nil {
			return e
		}
		out = r
		if QualificationOf(r) != q || r.Actions == nil {
			return failure(authorization.Conflict)
		}
		if terminal(r.Task) || controlIntent(r.Task) != "RUN" {
			return nil
		}
		if r.Task.State == "WAITING" && (len(r.Task.WaitingReasons) != 1 || r.Task.WaitingReasons[0] != "reconciliation") {
			return nil
		}
		if r.Work[0].LeaseUntil > tx.Now().UnixNano() {
			return nil
		}
		if r.Task.Constraints.DeadlineUnix <= tx.Now().Unix() {
			return failure(authorization.Expired)
		}
		if r.Task.Attempts >= r.Limits.MaxAttempts || r.Task.Attempts >= r.Task.Constraints.MaxSteps {
			return generationError("RECOVERY_LIMIT")
		}
		r.Task.Version++
		r.Task.Attempts++
		w := &r.Work[0]
		w.Generation++
		w.LeaseUntil = leaseEnd(tx.Now(), r)
		w.InFlight = true
		w.DecisionVersion = r.Task.Version
		if r.Task.State == "QUEUED" {
			r.Task.State = "RUNNING"
			r.Task.StopReason = ""
		}
		for i := range r.Actions.Actions {
			a := &r.Actions.Actions[i]
			if a.Status == "READY" && r.UpdateVersion > a.BaseVersion {
				a.Status = "SKIPPED"
			}
			if a.Status == "DISPATCHED" {
				a.ExecutionVersion = r.Task.Version
			}
		}
		for i := range r.Actions.Decisions {
			d := &r.Actions.Decisions[i]
			if d.Admitted || d.Rejection != "" || d.Record == nil {
				continue
			}
			if r.UpdateVersion > d.Qualification.Version {
				d.Rejection = "INPUT_INVALIDATED"
			} else {
				d.AdmissionQualification = QualificationOf(r)
			}
		}
		r.Records = append(r.Records, Record{Kind: "action:lease-recovered", Version: r.Task.Version})
		j.Runs[q.Ref.TaskID] = r
		out = r
		return nil
	})
	return out, e
}

func (p *ActionPort) WithInteractions(u *UpdateService) (*ActionPort, error) {
	if u == nil || u.core != p.service {
		return nil, failure(authorization.Invalid)
	}
	copy := *p
	copy.interactions = u
	return &copy, nil
}
func (p *ActionPort) WaitInput(ctx context.Context, q Qualification, n uint32, ref string) (RunSnapshot, error) {
	if p.interactions == nil {
		return RunSnapshot{}, failure(authorization.Unsupported)
	}
	r, e := p.Current(ctx, q.Ref)
	if e != nil {
		return r, e
	}
	if r.Actions == nil || n == 0 || int(n) != len(r.Actions.Decisions) || r.Actions.Decisions[n-1].Record == nil || r.Actions.Decisions[n-1].Record.Proposal.Evidence != ref {
		return r, failure(authorization.Conflict)
	}
	_, e = p.interactions.CreateWait(ctx, p.binding.Token, WaitRequest{ChangeID: q.Ref.TaskID + "/action-wait-" + strconv.FormatUint(uint64(n), 10), Qualification: q, Questions: []Question{{QuestionRef: ref, Constraint: AnswerConstraint{Kind: "text", MaxBytes: 4096}, Dependency: "facts", Responder: r.Task.Subject, ExpiresUnix: r.Task.Constraints.DeadlineUnix}}})
	if e != nil {
		return r, e
	}
	return p.Current(ctx, q.Ref)
}
func actionWait(r RunSnapshot, in WaitRequest) bool {
	if r.Actions == nil || len(r.Actions.Decisions) == 0 || unresolvedActions(r.Actions) || len(in.Questions) != 1 {
		return false
	}
	d := r.Actions.Decisions[len(r.Actions.Decisions)-1]
	q := d.Qualification
	if d.AdmissionQualification != (Qualification{}) {
		q = d.AdmissionQualification
	}
	return !d.Admitted && d.Rejection == "" && d.Record != nil && d.Record.Proposal.Kind == "wait" && d.Record.Proposal.Evidence == in.Questions[0].QuestionRef && q == in.Qualification
}
