package tasks

import (
	"context"
	"lerna/authorization"
	"reflect"
)

type ChildRemote interface {
	Accept(context.Context, ChildIntent) (Task, error)
	Lookup(context.Context, ChildReference) (Task, error)
	Cancel(context.Context, ChildReference) (ControlReceipt, error)
	Report(context.Context, ChildReference) (ChildReport, error)
}
type ChildResolver func(string) (ChildRemote, error)
type ChildGrant func(context.Context, ChildIntent) (string, error)
type ChildAssessment func(context.Context, ChildSpec, ChildReport) (string, error)

// Advance performs one bounded handoff/reconciliation step, rotating among
// children so an unknown branch cannot starve an independent branch.
func (p *DelegationPort) Advance(ctx context.Context, q Qualification, resolve ChildResolver, grant ChildGrant, assess ChildAssessment) (RunSnapshot, error) {
	if resolve == nil || grant == nil || assess == nil {
		return RunSnapshot{}, failure(authorization.Invalid)
	}
	ctx, cancel := context.WithTimeout(ctx, p.limits.IOTimeout)
	defer cancel()
	var out RunSnapshot
	index := -1
	var intent ChildIntent
	cancelChild := false
	needGrant := false
	exhausted := false
	e := p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		r, e := p.current(j, tx, q.Ref)
		if e != nil {
			return e
		}
		d := r.Delegations
		if d == nil || q.Owner != r.Task.Owner || q.Epoch != r.Task.OwnerEpoch || q.WorkID != r.Work[0].ID || q.Generation != r.Work[0].Generation {
			return failure(authorization.Conflict)
		}

		cancelChild = controlIntent(r.Task) == "CANCEL" || r.Task.Constraints.DeadlineUnix <= tx.Now().Unix()
		checks := &d.Checks
		if cancelChild {
			checks = &d.DispositionChecks
		}
		if *checks >= uint32(p.limits.MaxChecks) {
			exhausted = true
			r.Task.State = "WAITING"
			addWait(&r.Task, "reconciliation")
			addWait(&r.Task, "budget")
			syncWait(&r.Task)
			r.Task.Version++
			j.Runs[q.Ref.TaskID] = r
			out = r
			return nil
		}
		*checks++

		active := 0
		for _, c := range d.Children {
			if c.Status == "SENT" || c.Status == "ACTIVE" || c.Status == "CONFLICT" {
				active++
			}
		}
		for n := 0; n < len(d.Children); n++ {
			i := (d.Next + n) % len(d.Children)
			c := &d.Children[i]
			if childDisposed(*c) {
				continue
			}
			if cancelChild && c.Status == "QUEUED" && c.Attempts == 0 {
				c.Status = "UNSENT"
				if e := settleChildBudget(&r, c, Constraints{}); e != nil {
					return e
				}
				continue
			}
			if c.Status == "QUEUED" && !cancelChild {
				ready := true
				failed := false
				for _, key := range c.Spec.DependsOn {
					for _, dep := range d.Children {
						if dep.Spec.Key == key {
							if dep.Status == "FAILED" || dep.Status == "STOPPED" || dep.Status == "UNSENT" {
								failed = true
							}
							if dep.Status != "ACCEPTED" {
								ready = false
							}
						}
					}
				}
				if failed {
					c.Status = "UNSENT"
					if e := settleChildBudget(&r, c, Constraints{}); e != nil {
						return e
					}
					continue
				}
				if !ready || active >= p.limits.MaxConcurrent {
					continue
				}
				if r.Task.State != "RUNNING" || controlIntent(r.Task) != "RUN" || !r.Work[0].InFlight || r.Work[0].Worker != p.binding.WorkerID || r.Work[0].LeaseUntil <= tx.Now().UnixNano() {
					continue
				}
			}
			childChecks := &c.Checks
			if cancelChild {
				childChecks = &c.DispositionChecks
			}
			if *childChecks >= uint32(p.limits.MaxChecks) {
				continue
			}
			*childChecks++
			index = i
			d.Next = (i + 1) % len(d.Children)
			if c.Intent == nil {
				ancestors := []Ref{}
				if r.Parent != nil {
					ancestors = append(ancestors, r.Parent.Ancestors...)
				}
				ancestors = append(ancestors, r.Task.Ref)
				c.Intent = &ChildIntent{Parent: r.Task.Ref, ParentOwner: r.Task.Owner, ParentEpoch: r.Task.OwnerEpoch, Depth: len(ancestors), Ancestors: ancestors, Spec: c.Spec}
			}
			intent = *c.Intent
			needGrant = intent.GrantMaterial == ""
			break
		}
		if controlIntent(r.Task) == "CANCEL" && !delegationPending(r) && !r.Work[0].InFlight {
			settleControl(&r, tx, p.binding.Token)
		}
		r.Task.Version++
		j.Runs[q.Ref.TaskID] = r
		out = r
		return nil
	})
	if exhausted {
		return out, failure(authorization.Unavailable)
	}
	if e != nil || index < 0 {
		return out, e
	}
	remote, e := resolve(intent.Spec.Agent)
	if e != nil {
		return out, e
	}
	if remote == nil {
		return out, failure(authorization.Unavailable)
	}
	if !cancelChild {
		if e := p.policy(ctx, out.Task, intent.Spec, nil); e != nil {
			return out, e
		}
	}
	if needGrant {
		if !cancelChild {
			if e := p.checkChildBoundary(ctx, q.Ref, "delegate"); e != nil {
				return out, e
			}
		}
		material, e := grant(ctx, intent)
		if e != nil {
			return out, e
		}
		if material == "" || len(material) > 32768 {
			return out, failure(authorization.Invalid)
		}
		intent.GrantMaterial = material
		e = p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
			r, e := p.current(j, tx, q.Ref)
			if e != nil {
				return e
			}
			if !delegationFence(r, q) {
				return failure(authorization.Conflict)
			}
			c := &r.Delegations.Children[index]
			if c.Intent.GrantMaterial != "" && c.Intent.GrantMaterial != material {
				return failure(authorization.IdentityConflict)
			}
			c.Intent = &intent
			j.Runs[q.Ref.TaskID] = r
			out = r
			return nil
		})
		if e != nil {
			return out, e
		}
	}
	if cancelChild {
		receipt, e := remote.Cancel(ctx, ChildReferenceOf(intent))
		if e != nil {
			return out, e
		}
		if receipt.Outcome == "APPLIED" && receipt.Version == 0 && receipt.Ref == (Ref{intent.Parent.Namespace, childID(intent)}) && receipt.Intent == "CANCEL" {
			return p.recordChild(ctx, q, index, nil, "STOPPED")
		}
	}
	child, e := remote.Lookup(ctx, ChildReferenceOf(intent))
	if authorization.Is(e, authorization.NotFound) && !cancelChild {
		if e := p.checkChildBoundary(ctx, q.Ref, "delegate"); e != nil {
			return out, e
		}
		e = p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
			r, e := p.current(j, tx, q.Ref)
			if e != nil {
				return e
			}
			if !delegationFence(r, q) {
				return failure(authorization.Conflict)
			}
			c := &r.Delegations.Children[index]
			if !p.delegationLive(r, tx) || tx.Now().Unix() >= c.Spec.Budget.DeadlineUnix {
				return failure(authorization.Conflict)
			}
			if c.Attempts >= 3 {
				return failure(authorization.Unavailable)
			}
			if childDisposed(*c) {
				return failure(authorization.Conflict)
			}
			active := 0
			for _, other := range r.Delegations.Children {
				if other.Status != "QUEUED" && !childDisposed(other) {
					active++
				}
			}
			if c.Status == "QUEUED" && active >= p.limits.MaxConcurrent {
				return failure(authorization.Unavailable)
			}
			c.Attempts++
			c.Status = "SENT"
			j.Runs[q.Ref.TaskID] = r
			out = r
			return nil
		})
		if e != nil {
			return out, e
		}
		child, e = remote.Accept(ctx, intent)
	}
	if e != nil {
		return out, e
	}
	if child.Owner != intent.Spec.Agent || child.Ref.Namespace != q.Ref.Namespace || child.Ref.TaskID == "" {
		return out, failure(authorization.Denied)
	}
	report, e := remote.Report(ctx, ChildReferenceOf(intent))
	if e != nil {
		return out, e
	}
	if !validChildEvidence(ChildEvidence{Evidence: report.Evidence, Source: report.Source, Coverage: report.Coverage, Effect: report.Effect, Unresolved: report.Unresolved}) || report.Child != child.Ref || report.Parent != q.Ref || report.OperationID != intent.Spec.OperationID || report.Owner != intent.Spec.Agent || report.Digest != specDigest(intent.Spec) || report.Version == 0 || report.TaskVersion < child.Version || (report.TaskVersion == child.Version && report.State != child.State) || len(report.Result) > 16384 {
		return out, failure(authorization.Denied)
	}
	if e := p.policy(ctx, out.Task, intent.Spec, &report); e != nil {
		return out, e
	}
	status := "ACTIVE"
	if report.State == "COMPLETED" || report.State == "FAILED" || report.State == "CANCELLED" {
		if !report.UsageKnown || (report.Effect != "NONE" && report.Effect != "CONFIRMED") || report.Evidence == "" || report.Source == "" || len(report.Unresolved) > 0 {
			status = "CONFLICT"
		} else {
			if report.State == "COMPLETED" {
				if e = delegationSchema(intent.Spec.ResultSchema, report.Result); e != nil {
					return out, e
				}
			}
			status, e = assess(ctx, intent.Spec, report)
			if e != nil {
				return out, e
			}
			if status != "ACCEPTED" && status != "FAILED" && status != "STOPPED" && status != "CONFLICT" {
				return out, failure(authorization.Invalid)
			}
			if status == "ACCEPTED" && report.State != "COMPLETED" || status == "STOPPED" && report.State != "CANCELLED" {
				return out, failure(authorization.Denied)
			}
		}
	}
	return p.recordChild(ctx, q, index, &report, status)
}
func (p *DelegationPort) recordChild(ctx context.Context, q Qualification, index int, report *ChildReport, status string) (RunSnapshot, error) {

	before, e := p.snapshot(ctx, q.Ref)
	if e != nil {
		return RunSnapshot{}, e
	}
	if !delegationFence(before, q) || index < 0 || index >= len(before.Delegations.Children) {
		return RunSnapshot{}, failure(authorization.Conflict)
	}
	if report != nil {
		if e = p.policy(ctx, before.Task, before.Delegations.Children[index].Spec, report); e != nil {
			return RunSnapshot{}, e
		}
	}
	var out RunSnapshot
	e = p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		r, e := p.current(j, tx, q.Ref)
		if e != nil {
			return e
		}
		if !delegationFence(r, q) || r.UpdateVersion != before.UpdateVersion {
			return failure(authorization.Conflict)
		}
		c := &r.Delegations.Children[index]
		if childDisposed(*c) {
			out = r
			return nil
		}
		if report != nil {
			if c.Report != nil {
				if report.Version < c.Report.Version {
					return failure(authorization.Conflict)
				}
				if report.Version == c.Report.Version && !reflect.DeepEqual(*report, *c.Report) {
					return failure(authorization.IdentityConflict)
				}
			}
			c.Child = report.Child
			if c.Report == nil || report.Version != c.Report.Version {
				c.Reports = append(c.Reports, *report)
			}
			c.Report = report
			if report.UsageKnown && (status == "ACCEPTED" || status == "FAILED" || status == "STOPPED") && !c.Settled {
				if e := settleChildBudget(&r, c, report.Used); e != nil {
					return e
				}
			}
		}
		if report == nil && status == "STOPPED" {
			if e := settleChildBudget(&r, c, Constraints{}); e != nil {
				return e
			}
		}
		c.Status = status
		if controlIntent(r.Task) == "CANCEL" && !delegationPending(r) && !r.Work[0].InFlight {
			settleControl(&r, tx, p.binding.Token)
		}
		r.Task.Version++
		j.Runs[q.Ref.TaskID] = r
		out = r
		return nil
	})
	return out, e
}

// Approve requires a separate parent-goal verifier after all child obligations
// are disposed. It never treats child completion labels as goal evidence.
func (p *DelegationPort) Approve(ctx context.Context, q Qualification, evidence string, verify func(context.Context, RunSnapshot, string) error) (RunSnapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, p.limits.IOTimeout)
	defer cancel()
	if evidence == "" || len(evidence) > 1024 || verify == nil {
		return RunSnapshot{}, failure(authorization.Invalid)
	}
	r, e := p.snapshot(ctx, q.Ref)
	if e != nil {
		return r, e
	}
	if r.Delegations == nil || r.Delegations.ResultsReceipt == nil || delegationPending(r) {
		return r, failure(authorization.Conflict)
	}
	for _, c := range r.Delegations.Children {
		if c.Spec.Required && c.Status != "ACCEPTED" {
			return r, failure(authorization.Conflict)
		}
	}
	if _, e = p.checkedReports(ctx, r); e != nil {
		return r, e
	}
	if e = verify(ctx, r, evidence); e != nil {
		return r, e
	}
	var out RunSnapshot
	e = p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		current, e := p.current(j, tx, q.Ref)
		if e != nil {
			return e
		}
		if QualificationOf(current) != q || current.Task.Version != r.Task.Version || !p.delegationLive(current, tx) || delegationPending(current) {
			return failure(authorization.Conflict)
		}
		current.Delegations.Approved = true
		current.Delegations.ApprovedAt = current.Task.Version
		current.Delegations.Evidence = evidence
		current.Task.Version++
		j.Runs[q.Ref.TaskID] = current
		out = current
		return nil
	})
	return out, e
}

func delegationFence(r RunSnapshot, q Qualification) bool {
	return r.Delegations != nil && len(r.Work) == 1 && q.Ref == r.Task.Ref && q.Owner == r.Task.Owner && q.Epoch == r.Task.OwnerEpoch && q.WorkID == r.Work[0].ID && q.Generation == r.Work[0].Generation
}
func (p *DelegationPort) delegationLive(r RunSnapshot, tx authorization.RuntimeTransaction) bool {
	if _, e := tx.Authorize(p.binding.Token, r.Task.Resource, "task.execute"); e != nil {
		return false
	}
	return len(r.Work) == 1 && r.Task.State == "RUNNING" && controlIntent(r.Task) == "RUN" && r.Task.Constraints.DeadlineUnix > tx.Now().Unix() && r.Work[0].InFlight && r.Work[0].Worker == p.binding.WorkerID && r.Work[0].LeaseUntil > tx.Now().UnixNano()
}
func childDisposed(c ChildLink) bool {
	return c.Status == "ACCEPTED" || c.Status == "FAILED" || c.Status == "STOPPED" || c.Status == "UNSENT"
}

// PublishResults appends the immutable, source-bearing child reports through
// SubmitUpdate. Saving and update admission may be retried independently. The
// ordinary input invalidation rules apply: the parent must make a fresh decision.
func (p *DelegationPort) PublishResults(ctx context.Context, q Qualification, u *UpdateService, save func(context.Context, Task, []ChildReport) (string, error), operation func(context.Context) (string, error)) (InputReceipt, error) {
	if u == nil || u.core != p.service || save == nil || operation == nil {
		return InputReceipt{}, failure(authorization.Invalid)
	}
	ctx, cancel := context.WithTimeout(ctx, p.limits.IOTimeout)
	defer cancel()
	r, e := p.snapshot(ctx, q.Ref)
	if e != nil {
		return InputReceipt{}, e
	}
	if r.Delegations == nil || delegationPending(r) {
		return InputReceipt{}, failure(authorization.Conflict)
	}
	if r.Delegations.ResultsUpdate == nil {
		reports, e := p.checkedReports(ctx, r)
		if e != nil {
			return InputReceipt{}, e
		}
		ref, e := save(ctx, r.Task, reports)
		if e != nil {
			return InputReceipt{}, e
		}
		op, e := operation(ctx)
		if e != nil {
			return InputReceipt{}, e
		}
		e = p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
			current, e := p.current(j, tx, q.Ref)
			if e != nil {
				return e
			}
			if QualificationOf(current) != q || delegationPending(current) {
				return failure(authorization.Conflict)
			}
			if current.Delegations.ResultsUpdate == nil {
				current.Delegations.ResultsUpdate = &UpdateRequest{OperationID: op, Ref: q.Ref, ExpectedVersion: current.Task.Version, Intent: "append", InputRefs: []string{ref}}
			}
			j.Runs[q.Ref.TaskID] = current
			r = current
			return nil
		})
		if e != nil {
			return InputReceipt{}, e
		}
	}
	receipt, e := u.SubmitUpdate(ctx, p.binding.Token, *r.Delegations.ResultsUpdate)
	if e != nil {
		return receipt, e
	}
	e = p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		current, e := p.current(j, tx, q.Ref)
		if e != nil {
			return e
		}
		if current.Delegations == nil || current.Delegations.ResultsUpdate == nil || current.Delegations.ResultsUpdate.OperationID != receipt.OperationID {
			return failure(authorization.Conflict)
		}
		current.Delegations.ResultsReceipt = &receipt
		j.Runs[q.Ref.TaskID] = current
		return nil
	})
	return receipt, e
}

func settleChildBudget(r *RunSnapshot, c *ChildLink, used Constraints) error {
	if c.Settled {
		return nil
	}
	b := c.Spec.Budget
	if used.MaxSteps > b.MaxSteps || used.ModelRequests > b.ModelRequests || used.ModelTokens > b.ModelTokens {
		return failure(authorization.Denied)
	}
	r.Task.DelegatedSteps -= b.MaxSteps - used.MaxSteps
	r.Task.DelegatedRequests -= b.ModelRequests - used.ModelRequests
	r.Task.DelegatedTokens -= b.ModelTokens - used.ModelTokens
	c.Settled = true
	return nil
}

// Complete publishes the already governed collaboration-result reference after
// independent parent-goal approval. A delegation decision is not an answer
// generation, so this phase does not fabricate an answer publication record.
func (p *DelegationPort) Complete(ctx context.Context, q Qualification) (RunSnapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, p.limits.IOTimeout)
	defer cancel()
	r, e := p.snapshot(ctx, q.Ref)
	if e != nil {
		return r, e
	}
	if r.Delegations == nil || !r.Delegations.Approved || r.Delegations.ResultsReceipt == nil || delegationPending(r) {
		return r, failure(authorization.Conflict)
	}
	if _, e = p.checkedReports(ctx, r); e != nil {
		return r, e
	}
	var out RunSnapshot
	e = p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		current, e := p.current(j, tx, q.Ref)
		if e != nil {
			return e
		}
		if QualificationOf(current) != q || !p.delegationLive(current, tx) || current.Task.ModelReservedRequests != 0 || current.Task.ModelReservedTokens != 0 {
			return failure(authorization.Conflict)
		}
		current.Task.State = "COMPLETED"
		current.Task.Result = current.Delegations.ResultsUpdate.InputRefs[0]
		current.Task.Version++
		current.Task.WaitingReasons = nil
		current.Task.StopReason = ""
		current.Work[0].Done = true
		current.Work[0].InFlight = false
		current.Work[0].LeaseUntil = 0
		current.Records = append(current.Records, Record{Kind: "delegation:completed", Version: current.Task.Version})
		j.Runs[q.Ref.TaskID] = current
		out = current
		return nil
	})
	return out, e
}

func (p *DelegationPort) checkedReports(ctx context.Context, r RunSnapshot) ([]ChildReport, error) {
	var reports []ChildReport
	for _, c := range r.Delegations.Children {
		history := c.Reports
		if len(history) == 0 && c.Report != nil {
			history = []ChildReport{*c.Report}
		}
		for _, report := range history {
			if e := p.policy(ctx, r.Task, c.Spec, &report); e != nil {
				return nil, e
			}
			reports = append(reports, report)
		}
	}
	return reports, nil
}
