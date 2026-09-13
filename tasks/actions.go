package tasks

import (
	"context"
	"lerna/authorization"
	"reflect"
)

// ActionLimits are frozen per task. Model tokens include every dispatched
// request, including failed and unknown calls. Queries are charged before I/O.
type ActionLimits struct {
	MaxOperations, MaxQueries, MaxCorrections uint32
	InputTokens, OutputTokens                 uint64
}
type Action struct {
	BaseVersion                       uint64
	ExecutionVersion                  uint64
	Key                               string
	DependsOn                         []string
	OperationID, Descriptor, InputRef string
	// ControlVersion is zero for capabilities without shared resource control.
	// Execution validates it against the actual target binding at admission.
	ResourceVersion, ControlVersion uint64
	Write                           bool
	Qualification                   Qualification
	Status                          string
	// RecoveryStart binds only the internally authorized WAITING-to-RUNNING
	// start. It never replaces the original invocation Qualification.
	RecoveryStart Qualification `json:",omitzero"`
}
type ActionProposal struct {
	Kind, Reason, Evidence string
	Actions                []Action
}

// A context failure may have no Evidence: only its closed error code and usage
// are retained when current authority forbids storing the original bytes.
type DecisionRecord struct {
	Evidence, Error string
	Usage           GenerationUsage
	Proposal        ActionProposal
}
type ActionDecision struct {
	AdmissionQualification Qualification
	Number                 uint32
	Qualification          Qualification
	Record                 *DecisionRecord
	Admitted               bool
	Rejection              string
}
type ActionState struct {
	// AnswerQualification records the one-way phase transition while preserving all action history.
	AnswerQualification         *Qualification
	FailureEvidence             string
	Limits                      ActionLimits
	Decisions                   []ActionDecision
	Actions                     []Action
	Queries                     []string
	Corrections                 uint32
	NeedsCorrection, WrongWrite bool
}
type ActionPort struct {
	*WorkPort
	limits       ActionLimits
	interactions *UpdateService
}

func (p *WorkPort) Actions(l ActionLimits) (*ActionPort, error) {
	if !p.binding.AllowEffectEvidence || l.MaxOperations < 1 || l.MaxOperations > 12 || l.MaxQueries < 1 || l.MaxQueries > 128 || l.MaxCorrections > 2 || !(GenerationLimits{Requests: 1, InputTokens: l.InputTokens, OutputTokens: l.OutputTokens}).valid() {
		return nil, failure(authorization.Invalid)
	}
	return &ActionPort{WorkPort: p, limits: l}, nil
}
func (p *ActionPort) current(j *journal, tx authorization.RuntimeTransaction, q Qualification, live bool) (RunSnapshot, error) {
	r, ok := j.Runs[q.Ref.TaskID]
	if !ok || q.Ref.Namespace != p.service.config.Namespace {
		return r, failure(authorization.Denied)
	}
	id, e := tx.Authorize(p.binding.Token, r.Task.Resource, "task.execute")
	if e != nil {
		return r, e
	}
	if id.Subject != p.binding.Subject || r.Task.Subject != id.Subject || r.Task.Owner != q.Owner || r.Task.OwnerEpoch != q.Epoch {
		return r, failure(authorization.Denied)
	}
	if len(r.Work) != 1 || r.Work[0].Worker != p.binding.WorkerID || !r.Work[0].EffectAware || r.Limits != p.WorkPort.limits {
		return r, failure(authorization.Conflict)
	}
	if r.Actions != nil && r.Actions.Limits != p.limits {
		return r, failure(authorization.IdentityConflict)
	}
	if live && r.Actions != nil && r.Actions.AnswerQualification != nil {
		return r, failure(authorization.Conflict)
	}
	if live && (QualificationOf(r) != q || r.Task.State != "RUNNING" || controlIntent(r.Task) != "RUN" || !r.Work[0].InFlight || r.Work[0].LeaseUntil <= tx.Now().UnixNano() || r.Task.Constraints.DeadlineUnix <= tx.Now().Unix()) {
		return r, failure(authorization.Conflict)
	}
	return r, nil
}
func (p *ActionPort) Initialize(ctx context.Context, q Qualification) (RunSnapshot, error) {
	var out RunSnapshot
	e := p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		r, e := p.current(j, tx, q, true)
		if e != nil {
			return e
		}
		if r.Actions == nil {
			if r.Task.Constraints.ModelRequests == 0 || len(r.Generations) > 0 || r.Work[0].ExecutionOperation != "" || p.limits.MaxOperations > r.Task.Constraints.MaxSteps {
				return failure(authorization.Conflict)
			}
			r.Actions = &ActionState{Limits: p.limits}
			j.Runs[q.Ref.TaskID] = r
		}
		out = r
		return nil
	})
	return out, e
}

// Reserve durably spends one request before dispatch. A pending reservation is
// returned for inspection only: callers must not dispatch it a second time.
func (p *ActionPort) Reserve(ctx context.Context, q Qualification) (ActionDecision, error) {
	var out ActionDecision
	e := p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		r, e := p.current(j, tx, q, true)
		if e != nil {
			return e
		}
		a := r.Actions
		if a == nil {
			return failure(authorization.Conflict)
		}
		if unresolvedActions(a) {
			return failure(authorization.Conflict)
		}
		if len(a.Decisions) > 0 {
			d := a.Decisions[len(a.Decisions)-1]
			if d.Record == nil || !d.Admitted && d.Rejection == "" {
				return failure(authorization.Conflict)
			}
		}
		if a.WrongWrite || a.NeedsCorrection && a.Corrections >= a.Limits.MaxCorrections || len(a.Decisions) >= int(r.Task.Constraints.ModelRequests) {
			return generationError("CORRECTION_LIMIT")
		}
		unit := a.Limits.InputTokens + a.Limits.OutputTokens
		if r.Task.ModelUsedRequests+r.Task.ModelReservedRequests >= r.Task.Constraints.ModelRequests || r.Task.ModelUsedTokens+r.Task.ModelReservedTokens+unit > r.Task.Constraints.ModelTokens {
			return generationError("GENERATION_BUDGET_EXCEEDED")
		}
		if a.NeedsCorrection {
			a.Corrections++
			a.NeedsCorrection = false
		}
		r.Task.ModelReservedRequests++
		r.Task.ModelReservedTokens += unit
		out = ActionDecision{Number: uint32(len(a.Decisions) + 1), Qualification: q}
		a.Decisions = append(a.Decisions, out)
		r.Records = append(r.Records, Record{Kind: "action:request-reserved", Version: r.Task.Version})
		j.Runs[q.Ref.TaskID] = r
		return nil
	})
	return out, e
}
func unresolvedActions(a *ActionState) bool {
	for _, x := range a.Actions {
		if x.Status != "DONE" && x.Status != "SKIPPED" {
			return true
		}
	}
	return false
}

// Record is a trusted settlement port. It retains the first response unchanged,
// even after input invalidation; only Admit can give its proposal execution rights.
func (p *ActionPort) Record(ctx context.Context, q Qualification, n uint32, in DecisionRecord) error {
	metadataOnly := in.Evidence == "" && (in.Error == "CONTEXT_INVALIDATED" || in.Error == "CONTEXT_DENIED") && in.Proposal.Kind == "" && in.Proposal.Reason == "" && in.Proposal.Evidence == "" && len(in.Proposal.Actions) == 0
	if n == 0 || (!name(in.Evidence) && !metadataOnly) || len(in.Error) > 64 || len(in.Proposal.Actions) > 9 || len(in.Proposal.Reason) > 128 || len(in.Proposal.Evidence) > 128 {
		return failure(authorization.Invalid)
	}
	return p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		r, ok := j.Runs[q.Ref.TaskID]
		if !ok || q.Ref.Namespace != p.service.config.Namespace || r.Actions == nil || int(n) > len(r.Actions.Decisions) || r.Task.Subject != p.binding.Subject || r.Work[0].Worker != p.binding.WorkerID || r.Actions.Limits != p.limits {
			return failure(authorization.Denied)
		}
		d := &r.Actions.Decisions[n-1]
		if d.Qualification != q {
			return failure(authorization.IdentityConflict)
		}
		if d.Record != nil {
			if reflect.DeepEqual(*d.Record, in) {
				return nil
			}
			return failure(authorization.IdentityConflict)
		}
		u := in.Usage
		unit := p.limits.InputTokens + p.limits.OutputTokens
		if u.Requests > 1 || u.UnknownRequests > u.Requests || u.Tokens > uint64(u.Requests-u.UnknownRequests)*unit {
			return failure(authorization.Invalid)
		}
		if u.UnknownRequests == 0 {
			r.Task.ModelReservedRequests--
			r.Task.ModelReservedTokens -= unit
			r.Task.ModelUsedRequests += u.Requests
			r.Task.ModelUsedTokens += u.Tokens
		}
		d.Record = &in
		if in.Error != "" {
			d.Rejection = in.Error
			r.Actions.NeedsCorrection = true
		}
		j.Runs[q.Ref.TaskID] = r
		return nil
	})
}
func validActions(a *ActionState, p ActionProposal) bool {
	if p.Kind != "actions" || p.Reason != "" || p.Evidence != "" || len(p.Actions) == 0 || len(p.Actions) > 8 || uint32(len(a.Actions)+len(p.Actions)) > a.Limits.MaxOperations {
		return false
	}
	keys := map[string]int{}
	ops := map[string]bool{}
	for _, old := range a.Actions {
		ops[old.OperationID] = true
	}
	for i, x := range p.Actions {
		if !name(x.Key) || len(x.OperationID) == 0 || len(x.OperationID) > 512 || !name(x.Descriptor) || !name(x.InputRef) || x.ResourceVersion == 0 || x.BaseVersion != 0 || x.ExecutionVersion != 0 || x.Qualification != (Qualification{}) || x.RecoveryStart != (Qualification{}) || x.Status != "" || len(x.DependsOn) > 8 || ops[x.OperationID] {
			return false
		}
		if _, ok := keys[x.Key]; ok {
			return false
		}
		keys[x.Key] = i
		ops[x.OperationID] = true
	}
	visited := make([]uint8, len(p.Actions))
	var visit func(int) bool
	visit = func(i int) bool {
		if visited[i] == 1 {
			return false
		}
		if visited[i] == 2 {
			return true
		}
		visited[i] = 1
		seen := map[string]bool{}
		for _, k := range p.Actions[i].DependsOn {
			j, ok := keys[k]
			if !ok || seen[k] || !visit(j) {
				return false
			}
			seen[k] = true
		}
		visited[i] = 2
		return true
	}
	for i := range p.Actions {
		if !visit(i) {
			return false
		}
	}
	return true
}

// Admit replays the recorded decision number after an uncertain commit. IDs
// and payload references are already fixed by Record, never regenerated here.
func (p *ActionPort) Admit(ctx context.Context, q Qualification, n uint32) (RunSnapshot, error) {
	var out RunSnapshot
	var rejected bool
	e := p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		rejected = false
		r, e := p.current(j, tx, q, false)
		if e != nil {
			return e
		}
		a := r.Actions
		if a == nil || n == 0 || int(n) > len(a.Decisions) {
			return failure(authorization.NotFound)
		}
		d := &a.Decisions[n-1]
		if d.Qualification != q && d.AdmissionQualification != q {
			return failure(authorization.IdentityConflict)
		}
		if d.Admitted {
			out = r
			return nil
		}
		if d.Rejection != "" {
			return generationError("PROPOSAL_REJECTED")
		}
		if r.UpdateVersion > d.Qualification.Version {
			return failure(authorization.Conflict)
		}
		if _, e = p.current(j, tx, q, true); e != nil {
			return e
		}
		if d.Record == nil || unresolvedActions(a) {
			return failure(authorization.Conflict)
		}
		if !validActions(a, d.Record.Proposal) {
			d.Rejection = "INVALID_ACTION_BATCH"
			a.NeedsCorrection = true
			rejected = true
		} else {
			for _, x := range d.Record.Proposal.Actions {
				x.BaseVersion = q.Version
				x.Status = "READY"
				a.Actions = append(a.Actions, x)
			}
			d.Admitted = true
			r.Task.Version++
			r.Work[0].DecisionVersion = r.Task.Version
			r.Records = append(r.Records, Record{Kind: "action:batch-admitted", Version: r.Task.Version})
		}
		j.Runs[q.Ref.TaskID] = r
		out = r
		return nil
	})
	if e == nil && rejected {
		e = generationError("PROPOSAL_REJECTED")
	}
	return out, e
}

// Next serializes the reference execution strategy while retaining the full
// dependency graph. Repeated delivery returns exactly the same dispatch.
func (p *ActionPort) Next(ctx context.Context, q Qualification) (Action, error) {
	var out Action
	e := p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		r, e := p.current(j, tx, q, true)
		if e != nil {
			return e
		}
		if r.Actions == nil {
			return failure(authorization.Conflict)
		}
		a := r.Actions
		for _, x := range a.Actions {
			if x.Status == "DISPATCHED" {
				out = x
				return nil
			}
		}
		// Keys are batch-local: derive the batch's bounds from admitted decisions.
		offset := 0
		for _, d := range a.Decisions {
			if !d.Admitted {
				continue
			}
			count := len(d.Record.Proposal.Actions)
			batch := a.Actions[offset : offset+count]
			offset += count
			for i := range batch {
				x := &batch[i]
				if x.Status != "READY" {
					continue
				}
				if r.UpdateVersion > x.BaseVersion {
					return failure(authorization.Conflict)
				}
				ready, failed := true, false
				for _, dep := range x.DependsOn {
					for _, parent := range batch {
						if parent.Key != dep {
							continue
						}
						fact := r.ExecutionReports[parent.OperationID]
						if parent.Status == "SKIPPED" || parent.Status == "DONE" && (fact.Result != "SUCCESS" || fact.Effect != "CONFIRMED" || fact.Conflict) {
							failed = true
						}
						if parent.Status != "DONE" {
							ready = false
						}
					}
				}
				if failed {
					x.Status = "SKIPPED"
					continue
				}
				if !ready {
					continue
				}
				x.Qualification = q
				x.ExecutionVersion = q.Version
				x.Status = "DISPATCHED"
				out = *x
				j.Runs[q.Ref.TaskID] = r
				return nil
			}
		}
		j.Runs[q.Ref.TaskID] = r
		return nil
	})
	return out, e
}
func (p *ActionPort) ChargeQuery(ctx context.Context, q Qualification, key string) error {
	if !name(key) {
		return failure(authorization.Invalid)
	}
	return p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		r, e := p.current(j, tx, q, false)
		if e != nil {
			return e
		}
		if QualificationOf(r) != q || terminal(r.Task) {
			return failure(authorization.Conflict)
		}
		if r.Task.State != "WAITING" {
			// Read accounting remains available in the answer phase, while
			// action mutations stay closed. Keep the same live worker fence.
			if r.Task.State != "RUNNING" || controlIntent(r.Task) != "RUN" || !r.Work[0].InFlight || r.Work[0].LeaseUntil <= tx.Now().UnixNano() || r.Task.Constraints.DeadlineUnix <= tx.Now().Unix() {
				return failure(authorization.Conflict)
			}
		} else if len(r.Task.WaitingReasons) != 1 || r.Task.WaitingReasons[0] != "reconciliation" {
			return failure(authorization.Conflict)
		}
		if r.Actions == nil {
			return failure(authorization.Conflict)
		}
		a := r.Actions
		if e := appendActionQuery(a, key); e != nil {
			return e
		}
		j.Runs[q.Ref.TaskID] = r
		return nil
	})
}

// Finish is a trusted goal-verifier seam, never callable by model output. A
// confirmed wrong write permanently excludes success, including compensation.
func (p *ActionPort) Finish(ctx context.Context, q Qualification, evidence string, wrong bool) (RunSnapshot, error) {
	var out RunSnapshot
	e := p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		r, e := p.current(j, tx, q, true)
		if e != nil {
			return e
		}
		if r.Actions == nil || !wrong && unresolvedActions(r.Actions) {
			return failure(authorization.Conflict)
		}
		if wrong {
			if !name(evidence) {
				return failure(authorization.Invalid)
			}
			r.Actions.WrongWrite = true
			r.Actions.FailureEvidence = evidence
			for i, a := range r.Actions.Actions {
				if a.Status == "DISPATCHED" {
					return failure(authorization.Conflict)
				}
				if a.Status == "READY" {
					r.Actions.Actions[i].Status = "SKIPPED"
				}
			}
		}
		if r.Actions.WrongWrite {
			r.Task.State = "FAILED"
			r.Task.StopReason = "wrong_write"
		} else {
			if !name(evidence) || len(r.Actions.Actions) == 0 {
				return failure(authorization.Invalid)
			}
			r.Task.State = "COMPLETED"
			r.Task.Result = evidence
		}
		r.Task.Version++
		r.Work[0].Done = true
		r.Work[0].InFlight = false
		r.Work[0].LeaseUntil = 0
		j.Runs[q.Ref.TaskID] = r
		out = r
		return nil
	})
	return out, e
}
func (p *ActionPort) Wait(ctx context.Context, q Qualification, reason string) (RunSnapshot, error) {
	switch reason {
	case "model_unknown", "reconciliation", "missing_input", "budget", "no_progress", "input_invalidated", "unavailable":
	default:
		return RunSnapshot{}, failure(authorization.Invalid)
	}
	var out RunSnapshot
	e := p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		r, e := p.current(j, tx, q, false)
		if e != nil {
			return e
		}
		if QualificationOf(r) != q || terminal(r.Task) {
			return failure(authorization.Conflict)
		}
		r.Task.State = "WAITING"
		addWait(&r.Task, reason)
		syncWait(&r.Task)
		r.Task.Version++
		j.Runs[q.Ref.TaskID] = r
		out = r
		return nil
	})
	return out, e
}
func (p *ActionPort) Current(ctx context.Context, ref Ref) (RunSnapshot, error) {
	return p.service.Load(ctx, ref)
}

// KeepAlive renews metadata only; it cannot revive an expired or fenced worker.
func (p *ActionPort) KeepAlive(ctx context.Context, q Qualification) error {
	return p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		r, e := p.current(j, tx, q, true)
		if e != nil {
			return e
		}
		r.Work[0].LeaseUntil = leaseEnd(tx.Now(), r)
		j.Runs[q.Ref.TaskID] = r
		return nil
	})
}

// CheckDecision verifies the live task execution authority without advancing
// state. Context validation uses it alongside an independently read snapshot.
func (p *ActionPort) CheckDecision(ctx context.Context, q Qualification) error {
	return p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		_, e := p.current(j, tx, q, true)
		return e
	})
}
