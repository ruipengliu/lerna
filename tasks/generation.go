package tasks

import (
	"context"
	"lerna/authorization"
	"strconv"
)

type GenerationLimits struct {
	Requests                  uint32
	InputTokens, OutputTokens uint64
}

func (l GenerationLimits) valid() bool {
	return l.Requests >= 1 && l.Requests <= 3 && l.InputTokens > 0 && l.OutputTokens > 0 && l.InputTokens <= 1048576 && l.OutputTokens <= 1048576-l.InputTokens
}

type GenerationUsage struct {
	Requests, UnknownRequests uint32
	Tokens                    uint64
}
type GenerationReservation struct {
	OutputOperation string
	Worker          string
	Started         uint32
	Publication     *WorkChange
	Qualification   Qualification
	Limits          GenerationLimits
	Settled         bool
	Usage           GenerationUsage
}
type GenerationPort struct {
	newOutputOperation func(context.Context) (string, error)
	*WorkPort
	generationLimits GenerationLimits
}

func (p *WorkPort) Generations(l GenerationLimits) (*GenerationPort, error) {
	if !l.valid() {
		return nil, failure(authorization.Invalid)
	}
	return &GenerationPort{WorkPort: p, generationLimits: l}, nil
}
func (p *GenerationPort) ReserveDecision(ctx context.Context, in RunSnapshot) (RunSnapshot, error) {
	var out RunSnapshot
	outputOperation := ""
	if p.newOutputOperation != nil {
		var err error
		outputOperation, err = p.newOutputOperation(ctx)
		if err != nil {
			return out, err
		}
	}
	q := QualificationOf(in)
	err := p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		r, ok := j.Runs[q.Ref.TaskID]
		if !ok || q.Ref.Namespace != p.service.config.Namespace {
			return failure(authorization.Denied)
		}
		id, err := tx.Authorize(p.binding.Token, r.Task.Resource, "task.execute")
		if err != nil {
			return err
		}
		if id.Subject != p.binding.Subject || r.Task.Subject != id.Subject || q.Owner != r.Task.Owner || q.Epoch != r.Task.OwnerEpoch {
			return failure(authorization.Denied)
		}
		for _, old := range r.Generations {
			if old.Qualification.WorkID == q.WorkID && old.Qualification.Generation == q.Generation {
				if old.Worker != p.binding.WorkerID || old.Limits != p.generationLimits || old.Qualification.Version != q.Version+1 {
					return failure(authorization.IdentityConflict)
				}
				out = r
				return nil
			}
		}
		if QualificationOf(r) != q || r.Task.State != "RUNNING" || controlIntent(r.Task) != "RUN" || len(r.Work) != 1 || !r.Work[0].InFlight || r.Work[0].Worker != p.binding.WorkerID || r.Work[0].LeaseUntil <= tx.Now().UnixNano() || tx.Now().Unix() >= r.Task.Constraints.DeadlineUnix {
			return failure(authorization.Conflict)
		}
		if r.Actions != nil && r.Actions.AnswerQualification == nil {
			return failure(authorization.Conflict)
		}
		l := p.generationLimits
		tokens := uint64(l.Requests) * (l.InputTokens + l.OutputTokens)
		if r.Task.ModelUsedRequests+r.Task.ModelReservedRequests+l.Requests > r.Task.Constraints.ModelRequests || r.Task.ModelUsedTokens+r.Task.ModelReservedTokens+tokens > r.Task.Constraints.ModelTokens {
			return generationError("GENERATION_BUDGET_EXCEEDED")
		}
		r.Task.ModelReservedRequests += l.Requests
		r.Task.ModelReservedTokens += tokens
		r.Task.Version++
		r.Work[0].DecisionVersion = r.Task.Version
		r.Generations = append(r.Generations, GenerationReservation{Worker: p.binding.WorkerID, OutputOperation: outputOperation, Qualification: QualificationOf(r), Limits: l})
		r.Records = append(r.Records, Record{Kind: "generation:reserved", Version: r.Task.Version})
		r.CheckedAt = tx.Now().UnixNano()
		j.Runs[q.Ref.TaskID] = r
		out = r
		return nil
	})
	return out, err
}

// Settle is a trusted completion port. Unknown requests retain their entire
// per-request upper bound. It never authorizes publication of the proposal.
func (p *GenerationPort) Settle(ctx context.Context, q Qualification, u GenerationUsage) error {
	return p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		r, ok := j.Runs[q.Ref.TaskID]
		if !ok || q.Ref.Namespace != p.service.config.Namespace || r.Task.Subject != p.binding.Subject || q.Owner != r.Task.Owner || q.Epoch != r.Task.OwnerEpoch {
			return failure(authorization.Denied)
		}
		// Trusted bound worker may finish accounting after its business permission
		// expires, but cannot disclose data or start any work through this port.
		for i, g := range r.Generations {
			if g.Qualification != q {
				continue
			}
			if g.Worker != p.binding.WorkerID || g.Limits != p.generationLimits {
				return failure(authorization.IdentityConflict)
			}
			l := g.Limits
			unit := l.InputTokens + l.OutputTokens
			if u.Requests != g.Started || u.UnknownRequests > u.Requests || u.Tokens > uint64(u.Requests-u.UnknownRequests)*unit {
				return failure(authorization.Invalid)
			}
			released := l.Requests - u.UnknownRequests
			usedRequests := u.Requests - u.UnknownRequests
			usedTokens := u.Tokens
			if g.Settled {
				if g.Usage == u {
					return nil
				}
				if g.Usage.Requests != u.Requests || u.UnknownRequests >= g.Usage.UnknownRequests || u.Tokens < g.Usage.Tokens {
					return failure(authorization.IdentityConflict)
				}
				released = g.Usage.UnknownRequests - u.UnknownRequests
				usedRequests = released
				usedTokens = u.Tokens - g.Usage.Tokens
				if usedTokens > uint64(released)*unit {
					return failure(authorization.Invalid)
				}
			}
			r.Task.ModelReservedRequests -= released
			r.Task.ModelReservedTokens -= uint64(released) * unit
			r.Task.ModelUsedRequests += usedRequests
			r.Task.ModelUsedTokens += usedTokens
			r.Generations[i].Settled = true
			r.Generations[i].Usage = u
			r.Records = append(r.Records, Record{Kind: "generation:settled", Version: r.Task.Version})
			j.Runs[q.Ref.TaskID] = r
			return nil
		}
		return failure(authorization.NotFound)
	})
}

// CheckDecision gates each actual request and publication using the reserved
// decision version, live worker lease and current task control/authorization.
func (p *GenerationPort) CheckDecision(ctx context.Context, q Qualification) error {
	return p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		_, err := p.decision(j, tx, q)
		return err
	})
}
func (p *GenerationPort) decision(j *journal, tx authorization.RuntimeTransaction, q Qualification) (RunSnapshot, error) {
	r, ok := j.Runs[q.Ref.TaskID]
	if !ok || q.Ref.Namespace != p.service.config.Namespace {
		return RunSnapshot{}, failure(authorization.Denied)
	}
	id, err := tx.Authorize(p.binding.Token, r.Task.Resource, "task.execute")
	if err != nil {
		return RunSnapshot{}, err
	}
	if id.Subject != p.binding.Subject || r.Task.Subject != id.Subject {
		return RunSnapshot{}, failure(authorization.Denied)
	}
	if QualificationOf(r) != q || r.Task.State != "RUNNING" || controlIntent(r.Task) != "RUN" || len(r.Work) != 1 || r.Work[0].Worker != p.binding.WorkerID || !r.Work[0].InFlight || r.Work[0].LeaseUntil <= tx.Now().UnixNano() || tx.Now().Unix() >= r.Task.Constraints.DeadlineUnix {
		return RunSnapshot{}, failure(authorization.Conflict)
	}
	for _, g := range r.Generations {
		if g.Qualification == q && g.Limits == p.generationLimits && g.Worker == p.binding.WorkerID {
			return r, nil
		}
	}
	return RunSnapshot{}, failure(authorization.NotFound)
}

// WithOutputIdentities binds the existing operation issuer at trusted assembly.
func (p *GenerationPort) WithOutputIdentities(issue func(context.Context) (string, error)) *GenerationPort {
	copy := *p
	copy.newOutputOperation = issue
	return &copy
}

func (p *GenerationPort) Current(ctx context.Context, ref Ref) (RunSnapshot, error) {
	return p.service.Load(ctx, ref)
}

// BeginRequest persists a single-use dispatch ordinal before leaving the process.
// An unknown commit is never permission to dispatch. Replayed ordinals fail closed.
func (p *GenerationPort) BeginRequest(ctx context.Context, q Qualification, ordinal uint32) error {
	return p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		r, err := p.decision(j, tx, q)
		if err != nil {
			return err
		}
		for i, g := range r.Generations {
			if g.Qualification == q && g.Worker == p.binding.WorkerID && g.Limits == p.generationLimits {
				if g.Settled || g.Started != ordinal || ordinal >= g.Limits.Requests {
					return failure(authorization.Conflict)
				}
				r.Generations[i].Started++
				r.Records = append(r.Records, Record{Kind: "generation:request:" + strconv.FormatUint(uint64(ordinal), 10), Version: r.Task.Version})
				j.Runs[q.Ref.TaskID] = r
				return nil
			}
		}
		return failure(authorization.NotFound)
	})
}

// PreparePublication records the exact Core submission for process recovery. The
// trusted answer binding validates controlled content before using this port.
func (p *GenerationPort) PreparePublication(ctx context.Context, c WorkChange) error {
	if c.Kind != "complete" || c.Proposal.Kind != "answer" || !c.Proposal.Complete || !name(c.ChangeID) || c.Proposal.BaseVersion != c.Qualification.Version || len(c.Proposal.Result) > 256 {
		return failure(authorization.Invalid)
	}
	return p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		r, err := p.decision(j, tx, c.Qualification)
		if err != nil {
			return err
		}
		for i, g := range r.Generations {
			if g.Qualification == c.Qualification && g.Worker == p.binding.WorkerID && g.OutputOperation != "" {
				if g.Publication != nil {
					if *g.Publication != c {
						return failure(authorization.IdentityConflict)
					}
					return nil
				}
				copy := c
				r.Generations[i].Publication = &copy
				j.Runs[r.Task.Ref.TaskID] = r
				return nil
			}
		}
		return failure(authorization.NotFound)
	})
}
func preparedAnswer(r RunSnapshot, c WorkChange, worker string) bool {
	for _, g := range r.Generations {
		if g.Qualification == c.Qualification && g.Worker == worker && g.Publication != nil && *g.Publication == c {
			return true
		}
	}
	return false
}

type generationError string

func (e generationError) Error() string          { return string(e) }
func (e generationError) DecisionReason() string { return string(e) }
