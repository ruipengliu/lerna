package durableworkdemo

import (
	"context"
	"errors"
	"github.com/ruipengliu/lerna/contract"
	"github.com/ruipengliu/lerna/runtime"
	"time"
)

var ErrPolicy = errors.New("invalid durable demonstration policy")
var ErrGate = errors.New("unknown or regressing durable demonstration gate")

type GateCondition struct {
	ID       contract.ID
	Revision int64
}
type Policy struct {
	Identity                         string
	Lane                             string
	ExecutionLimit                   time.Duration
	MaxAttempts                      int64
	BaseBackoff, MaxBackoff, Recheck time.Duration
	Gate                             *GateCondition
	TransientFailures                int64
	PermanentReason                  string
}

func DefaultPolicy() Policy {
	return Policy{Identity: "project-policy-1", Lane: "ordinary", ExecutionLimit: 5 * time.Minute, MaxAttempts: 3, BaseBackoff: 100 * time.Millisecond, MaxBackoff: 5 * time.Second, Recheck: time.Second}
}
func LegacyPolicy() Policy { return DefaultPolicy() }
func (p Policy) Validate() error {
	if len(p.Identity) < 1 || len(p.Identity) > 128 || (p.Lane != "ordinary" && p.Lane != "control" && p.Lane != "reconciliation") || p.ExecutionLimit < time.Millisecond || p.ExecutionLimit > 24*time.Hour || p.MaxAttempts < 1 || p.MaxAttempts > 64 || p.BaseBackoff < time.Millisecond || p.MaxBackoff < p.BaseBackoff || p.MaxBackoff > time.Hour || p.Recheck < time.Millisecond || p.Recheck > time.Hour || p.TransientFailures < 0 || p.TransientFailures > 64 {
		return ErrPolicy
	}
	if p.PermanentReason != "" && p.PermanentReason != "schema_invalid" && p.PermanentReason != "forbidden" && p.PermanentReason != "precondition_failed" {
		return ErrPolicy
	}
	if p.Gate != nil {
		if p.Gate.Revision < 1 {
			return ErrPolicy
		}
		if _, err := contract.Encode(p.Gate.ID); err != nil {
			return ErrPolicy
		}
	}
	return nil
}

type PolicyBinding struct {
	Owner    contract.OwnerRef
	ObjectID contract.ID
	Policy   Policy
}
type Policies struct{ bindings []PolicyBinding }

func NewPolicies(bindings []PolicyBinding) (*Policies, error) {
	if len(bindings) > 64 {
		return nil, ErrPolicy
	}
	result := &Policies{}
	for _, b := range bindings {
		if err := b.Policy.Validate(); err != nil {
			return nil, err
		}
		if _, err := contract.Encode(b.Owner); err != nil {
			return nil, ErrPolicy
		}
		if _, err := contract.Encode(b.ObjectID); err != nil {
			return nil, ErrPolicy
		}
		for _, old := range result.bindings {
			if old.Owner == b.Owner && old.ObjectID == b.ObjectID {
				return nil, ErrPolicy
			}
		}
		if b.Policy.Gate != nil {
			gate := *b.Policy.Gate
			b.Policy.Gate = &gate
		}
		result.bindings = append(result.bindings, b)
	}
	return result, nil
}
func (p *Policies) For(owner contract.OwnerRef, id contract.ID) Policy {
	if p != nil {
		for _, b := range p.bindings {
			if b.Owner == owner && b.ObjectID == id {
				policy := b.Policy
				if policy.Gate != nil {
					gate := *policy.Gate
					policy.Gate = &gate
				}
				return policy
			}
		}
	}
	return DefaultPolicy()
}

type ScheduleState struct {
	Source              string
	Policy              Policy
	AdoptedAt, Deadline time.Time
	Attempts            int64
	StartEpoch          int64
	Outcome, Reason     string
	Due                 time.Time
	Stopped             bool
}
type ScheduleObservation struct {
	State       ScheduleState
	ActiveClaim bool
	Job         runtime.Job
}
type ScheduleRepository interface {
	BindSchedule(context.Context, runtime.Tx, contract.OwnerRef, contract.ID, int64, ScheduleState) error
	LoadSchedule(context.Context, runtime.Tx, contract.OwnerRef, contract.ID, int64) (*ScheduleState, error)
	SaveSchedule(context.Context, runtime.Tx, contract.OwnerRef, contract.ID, int64, ScheduleState) error
	GateRevision(context.Context, runtime.Tx, contract.OwnerRef, contract.ID) (int64, error)
	AdvanceGate(context.Context, runtime.Tx, contract.OwnerRef, contract.ID, int64) error
	ObserveSchedule(context.Context, contract.OwnerRef, contract.ID, int64, runtime.Clock) (ScheduleObservation, error)
}

func (s *Service) ObserveSchedule(ctx context.Context, id contract.ID, revision int64, trusted *contract.SubjectBinding) (ScheduleObservation, error) {
	if _, err := s.Observe(ctx, id, trusted); err != nil {
		return ScheduleObservation{}, err
	}
	if revision < 1 {
		return ScheduleObservation{}, ErrPolicy
	}
	repo, ok := s.Repository.(ScheduleRepository)
	if !ok {
		return ScheduleObservation{}, ErrPolicy
	}
	return repo.ObserveSchedule(ctx, s.Owner, id, revision, s.Clock)
}

// AdvanceGate is a trusted control seam, assembled separately from record.
func (s *Service) AdvanceGate(ctx context.Context, id contract.ID, revision int64) error {
	if !s.ScheduleControl {
		return publicError("forbidden", nil)
	}
	if err := workContext(ctx); err != nil {
		return err
	}
	if revision < 1 {
		return ErrGate
	}
	if _, err := contract.Encode(id); err != nil {
		return ErrGate
	}
	repo, ok := s.Repository.(ScheduleRepository)
	if !ok {
		return ErrGate
	}
	return s.Runner.Within(ctx, s.Owner, func(ctx context.Context, tx runtime.Tx) error {
		return repo.AdvanceGate(ctx, tx, s.Owner, id, revision)
	})
}

// Stop closes only the specified original revision. It is a trusted Host
// control operation, never a record payload field or a public 1.0 method.
func (s *Service) Stop(ctx context.Context, id contract.ID, revision int64) error {
	if !s.ScheduleControl {
		return publicError("forbidden", nil)
	}
	if err := workContext(ctx); err != nil {
		return err
	}
	if revision < 1 {
		return ErrPolicy
	}
	if _, err := contract.Encode(id); err != nil {
		return ErrPolicy
	}
	repo, ok := s.Repository.(ScheduleRepository)
	if !ok {
		return ErrPolicy
	}
	schedule, ok := s.Jobs.(runtime.ScheduleStore)
	if !ok {
		return ErrPolicy
	}
	return s.Runner.Within(ctx, s.Owner, func(ctx context.Context, tx runtime.Tx) error {
		_, _, err := poolLock(ctx, tx, s.Repository)
		if err != nil {
			return err
		}
		input, err := s.Repository.LockInput(ctx, tx, s.Owner, id)
		if err != nil {
			return err
		}
		if input == nil || revision > input.Revision {
			return runtime.ErrWorkBounds
		}
		state, err := repo.LoadSchedule(ctx, tx, s.Owner, id, revision)
		if err != nil {
			return err
		}
		if state == nil {
			return ErrPolicy
		}
		if state.Outcome == "success" || state.Outcome == "permanent" || state.Outcome == "expired" || state.Outcome == "stopped" {
			return nil
		}
		state.Stopped = true
		state.Outcome = "stopped"
		state.Reason = "trusted_control_stop"
		if err = repo.SaveSchedule(ctx, tx, s.Owner, id, revision, *state); err != nil {
			return err
		}
		job := runtime.Job{Object: contract.ObjectRef{TenantID: s.Owner.TenantID, OwnerID: s.Owner.OwnerID, Kind: "durable_work", ID: id}, Phase: "project", WorkRevision: input.Revision}
		return schedule.StopRevision(ctx, tx, job, revision)
	})
}
