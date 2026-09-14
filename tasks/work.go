package tasks

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"io"
	"lerna/authorization"
	"reflect"
	"time"
	"unicode/utf8"
)

// RunLimits are frozen with the first claim and must match on recovery.
// MaxAttempts includes the first decision and every failed/unknown attempt.
type RunLimits struct {
	Lease, RenewEvery, DecisionTimeout, IOTimeout time.Duration
	MaxAttempts                                   uint32
	MaxConcurrent                                 int
}

func (c RunLimits) valid() bool {
	return c.Lease >= 100*time.Millisecond && c.Lease <= time.Minute && c.RenewEvery >= time.Millisecond && c.RenewEvery <= c.Lease/3 && c.DecisionTimeout >= time.Millisecond && c.DecisionTimeout <= time.Minute && c.IOTimeout >= time.Millisecond && c.IOTimeout <= time.Minute && c.MaxAttempts >= 1 && c.MaxAttempts <= 3 && c.MaxConcurrent >= 1 && c.MaxConcurrent <= 4
}

// WorkerBinding is a trusted assembly input, never a task RPC. The token must
// currently authorize task.execute for Subject on the service resource.
type WorkerBinding struct {
	Token, Subject, WorkerID string
	AllowEffectEvidence      bool
}
type WorkPort struct {
	service        *Service
	binding        WorkerBinding
	limits         RunLimits
	actionRecovery Qualification
}

func (s *Service) BindWorker(b WorkerBinding, c RunLimits) (*WorkPort, error) {
	if !name(b.Subject) || !name(b.WorkerID) || b.Token == "" || !c.valid() {
		return nil, failure(authorization.Invalid)
	}
	return &WorkPort{service: s, binding: b, limits: c}, nil
}

// WithActionRecovery binds this host incarnation to a current worker lease
// while preserving the original immutable invocation qualification. Actual
// execution compares this separate binding with Core's live worker generation.
func (p *WorkPort) WithActionRecovery(q Qualification) *WorkPort {
	copy := *p
	copy.actionRecovery = q
	return &copy
}

type Qualification struct {
	Ref            Ref
	Owner          string
	Epoch, Version uint64
	WorkID         string
	Generation     uint64
}

func QualificationOf(s RunSnapshot) Qualification {
	q := Qualification{Ref: s.Task.Ref, Owner: s.Task.Owner, Epoch: s.Task.OwnerEpoch, Version: s.Task.Version}
	if len(s.Work) == 1 {
		q.WorkID = s.Work[0].ID
		q.Generation = s.Work[0].Generation
	}
	return q
}

type Proposal struct {
	Kind        string
	BaseVersion uint64
	Complete    bool
	Result      string
}
type WorkChange struct {
	ChangeID, Kind string
	Qualification  Qualification
	Proposal       Proposal
	Reason         string
	Finished       bool
}

// Commit accepts claim, start, renew, complete and stop. The binding supplies
// credentials and limits; neither is controlled by Brain or public task requests.
func (p *WorkPort) Commit(ctx context.Context, c WorkChange) (RunSnapshot, error) {
	if c.Kind == "claim" || c.Kind == "start" || c.Kind == "renew" {
		if e := p.checkChild(ctx, c.Qualification.Ref); e != nil {
			return RunSnapshot{}, e
		}
	}
	normalized, digest := proposalIdentity(c)
	var out RunSnapshot
	var denied error
	err := p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		denied = nil
		q := c.Qualification
		if !name(c.ChangeID) || q.Ref.Namespace != p.service.config.Namespace {
			return failure(authorization.Invalid)
		}
		r, ok := j.Runs[q.Ref.TaskID]
		if !ok {
			return failure(authorization.NotFound)
		}
		if r.Task.Subject != p.binding.Subject || q.Owner != p.service.config.Owner || q.Owner != r.Task.Owner || q.Epoch != r.Task.OwnerEpoch {
			return failure(authorization.Denied)
		}
		if r.Parent != nil && p.service.childPolicy == nil {
			return failure(authorization.Denied)
		}
		identity, authErr := tx.Authorize(p.binding.Token, r.Task.Resource, "task.execute")
		if authErr == nil && identity.Subject != p.binding.Subject {
			authErr = failure(authorization.Denied)
		}
		if old, exists := j.Commits[q.Ref.TaskID][c.ChangeID]; exists {
			if authErr != nil {
				return authErr
			}
			if old.WorkChange == nil || old.WorkerID != p.binding.WorkerID || old.WorkLimits != p.limits || old.ProposalDigest != digest || !reflect.DeepEqual(*old.WorkChange, normalized) {
				return failure(authorization.IdentityConflict)
			}
			out = old.Snapshot
			return nil
		}
		if q.Version != r.Task.Version || len(r.Work) != 1 || q.WorkID != r.Work[0].ID || q.Generation != r.Work[0].Generation || r.Work[0].Done || (r.Task.State != "QUEUED" && r.Task.State != "RUNNING") {
			return failure(authorization.Conflict)
		}
		if r.Limits != (RunLimits{}) && r.Limits != p.limits {
			return failure(authorization.Invalid)
		}
		if len(j.Commits[q.Ref.TaskID]) >= 256 {
			return failure(authorization.Unavailable)
		}
		if controlIntent(r.Task) != "RUN" {
			return failure(authorization.Conflict)
		}
		now := tx.Now()
		w := &r.Work[0]
		switch c.Kind {
		case "claim":
			if w.LeaseUntil > now.UnixNano() || w.InFlight {
				return failure(authorization.Conflict)
			}
		case "start", "renew", "complete", "stop":
			if r.Task.State != "RUNNING" || w.Worker != p.binding.WorkerID || (w.LeaseUntil <= now.UnixNano() && now.Unix() < r.Task.Constraints.DeadlineUnix) {
				return failure(authorization.Conflict)
			}
		default:
			return failure(authorization.Unsupported)
		}
		if (c.Finished && c.Kind != "complete" && c.Kind != "stop") || c.Kind != "complete" && c.Proposal != (Proposal{}) || c.Kind != "stop" && c.Reason != "" {
			return failure(authorization.Invalid)
		}
		if w.ExecutionOperation != "" && (c.Kind == "complete" || (c.Kind == "stop" && c.Finished)) {
			return failure(authorization.Unsupported)
		}
		if r.Actions != nil && r.Actions.AnswerQualification == nil && c.Kind != "renew" {
			return failure(authorization.Unsupported)
		}
		if c.Kind == "complete" || (c.Kind == "stop" && c.Finished) {
			w.InFlight = false
		}
		if r.Limits == (RunLimits{}) {
			r.Limits = p.limits
		}
		reason := ""
		if authErr != nil {
			reason = "authorization"
			denied = authErr
		} else if now.Unix() >= r.Task.Constraints.DeadlineUnix {
			reason = "deadline"
		} else if c.Kind == "claim" && r.Task.Attempts >= r.Task.Constraints.MaxSteps {
			reason = "budget"
		} else if c.Kind == "claim" && r.Task.Attempts >= r.Limits.MaxAttempts {
			reason = "retry_limit"
		}
		if reason != "" {
			stopRun(&r, reason)
		} else {
			switch c.Kind {
			case "claim":
				r.Task.State = "RUNNING"
				r.Task.StopReason = ""
				r.Task.Version++
				r.Task.Attempts++
				w.DecisionVersion = r.Task.Version
				w.EffectAware = p.binding.AllowEffectEvidence
				w.Worker = p.binding.WorkerID
				w.ExecutionOperation = ""
				w.Generation++
				w.LeaseUntil = leaseEnd(now, r)
			case "start":
				if w.InFlight {
					return failure(authorization.Conflict)
				}
				w.InFlight = true
			case "renew":
				w.LeaseUntil = leaseEnd(now, r) // Metadata only: preserve the decision version.
			case "complete":
				v := c.Proposal
				if v.BaseVersion != r.Task.Version {
					return failure(authorization.Conflict)
				}
				if (v.Kind != "result" && v.Kind != "answer") || (r.Task.Constraints.ModelRequests > 0 && v.Kind != "answer") || (v.Kind == "answer" && (r.Task.Constraints.ModelRequests == 0 || len(v.Result) > 256 || !preparedAnswer(r, c, p.binding.WorkerID))) || !v.Complete || len(v.Result) == 0 || len(v.Result) > 65536 || !utf8.ValidString(v.Result) {
					stopRun(&r, "invalid_proposal")
				} else {
					r.Task.State = "COMPLETED"
					r.Task.Result = v.Result
					r.Task.Version++
					w.InFlight = false
					w.Done = true
					w.LeaseUntil = 0
				}
			case "stop":
				switch c.Reason {
				case "unavailable", "timeout", "interrupted", "brain_failure":
				default:
					if !decisionReason(c.Reason) {
						return failure(authorization.Invalid)
					}
				}
				if c.Finished {
					w.InFlight = false
				}
				stopRun(&r, c.Reason)
			}
		}
		r.Records = append(r.Records, Record{Kind: c.Kind + ":" + r.Task.State, Version: r.Task.Version})
		r.CheckedAt = now.UnixNano()
		r.LastCommit = CommitReceipt{Ref: r.Task.Ref, ChangeID: c.ChangeID, Version: r.Task.Version}
		j.Runs[q.Ref.TaskID] = r
		copyChange := normalized
		j.Commits[q.Ref.TaskID][c.ChangeID] = commit{Receipt: r.LastCommit, WorkerID: p.binding.WorkerID, WorkLimits: p.limits, ProposalDigest: digest, WorkChange: &copyChange, Snapshot: r}
		out = r
		return nil
	})
	if err != nil {
		return RunSnapshot{}, err
	}
	if denied != nil {
		return RunSnapshot{}, denied
	}
	return out, nil
}
func leaseEnd(now time.Time, r RunSnapshot) int64 {
	end := now.Add(r.Limits.Lease)
	if deadline := time.Unix(r.Task.Constraints.DeadlineUnix, 0); deadline.Before(end) {
		end = deadline
	}
	return end.UnixNano()
}
func stopRun(r *RunSnapshot, reason string) {
	r.Task.State = "WAITING"
	r.Task.StopReason = reason
	r.Task.Version++
	r.Work[0].LeaseUntil = 0
	switch reason {
	case "deadline", "invalid_proposal", "brain_failure":
		r.Task.State = "FAILED"
		r.Work[0].Done = true
	case "unavailable", "timeout":
		if r.Task.Attempts >= r.Task.Constraints.MaxSteps {
			r.Task.StopReason = "budget"
		} else if r.Task.Attempts >= r.Limits.MaxAttempts {
			r.Task.StopReason = "retry_limit"
		} else {
			r.Task.State = "QUEUED"
		}
	}
	if r.Work[0].InFlight && (r.Work[0].EffectAware || r.Task.State == "FAILED") {
		r.Task.State = "WAITING"
		r.Work[0].Done = false
		r.Task.WaitingReasons = []string{r.Task.StopReason, "reconciliation"}
		return
	}
	r.Task.WaitingReasons = nil
	if r.Task.State == "WAITING" {
		r.Task.WaitingReasons = []string{r.Task.StopReason}
	}
}

// Lookup reconciles a particular original commit, not the current work version.
// A negative lookup is not permission to create a new logical change.
func (p *WorkPort) Lookup(ctx context.Context, ref Ref, id string) (RunSnapshot, error) {
	var out RunSnapshot
	err := p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		if ref.Namespace != p.service.config.Namespace {
			return failure(authorization.Denied)
		}
		r, ok := j.Runs[ref.TaskID]
		if !ok {
			return failure(authorization.NotFound)
		}
		ident, err := tx.Authorize(p.binding.Token, r.Task.Resource, "task.execute")
		if err != nil {
			return err
		}
		if ident.Subject != p.binding.Subject || r.Task.Subject != p.binding.Subject {
			return failure(authorization.Denied)
		}
		c, ok := j.Commits[ref.TaskID][id]
		if !ok {
			return failure(authorization.NotFound)
		}
		if c.WorkChange == nil || c.WorkerID != p.binding.WorkerID || c.WorkLimits != p.limits {
			return failure(authorization.IdentityConflict)
		}
		out = c.Snapshot
		return nil
	})
	return out, err
}

// Preserve exact byte semantics for replay without retaining rejected or huge
// proposal payloads in the durable journal. The result itself lives in Task
// only after valid completion. Length-prefixing separates kind from result.
func proposalIdentity(c WorkChange) (WorkChange, [32]byte) {
	h := sha256.New()
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(c.Proposal.Kind)))
	h.Write(size[:])
	io.WriteString(h, c.Proposal.Kind)
	io.WriteString(h, c.Proposal.Result)
	var digest [32]byte
	copy(digest[:], h.Sum(nil))
	c.Proposal.Kind = ""
	c.Proposal.Result = ""
	return c, digest
}

// Only fixed, non-sensitive domain codes may enter ordinary task diagnostics.
func decisionReason(s string) bool {
	switch s {
	case "CAPABILITY_UNAVAILABLE", "PROCESSING_DENIED", "INPUT_INVALIDATED", "INPUT_BUDGET_EXCEEDED", "OUTPUT_INVALID", "OUTPUT_TRUNCATED", "GENERATION_CANCELLED", "GENERATION_BUDGET_EXCEEDED", "MODEL_USAGE_INVALID", "MODEL_UNAVAILABLE", "MODEL_LIMIT_UNSUPPORTED":
		return true
	}
	return false
}

func (p *WorkPort) checkChild(ctx context.Context, ref Ref) error {
	return p.checkChildBoundary(ctx, ref, "execute")
}
func (p *WorkPort) checkChildBoundary(ctx context.Context, ref Ref, boundary string) error {
	if p.service.quarantined {
		return failure(authorization.Unavailable)
	}
	if p.service.childPolicy == nil {
		return nil
	}
	bounded, cancel := context.WithTimeout(ctx, p.limits.IOTimeout)
	defer cancel()
	r, e := p.service.Load(bounded, ref)
	if e != nil {
		return e
	}
	if r.Parent == nil {
		return nil
	}
	if p.service.childPolicy == nil {
		return failure(authorization.Denied)
	}
	return p.service.childPolicy(bounded, *r.Parent, boundary)
}
