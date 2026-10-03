package durableworkdemo

import (
	"context"
	"math"
	"strconv"
	"time"

	"github.com/ruipengliu/lerna/contract"
	"github.com/ruipengliu/lerna/runtime"
)

type Input struct {
	ID                   contract.ID
	Revision             int64
	Text                 string
	CreatedAt, UpdatedAt time.Time
}
type Observation struct {
	Input      Input
	Job        runtime.Job
	Projection *Projection
}

// Repository belongs to this demo consumer. It shares the supplied opaque Tx;
// concrete SQL and table names belong exclusively to its adapter.
type Repository interface {
	LockInput(context.Context, runtime.Tx, contract.OwnerRef, contract.ID) (*Input, error)
	SaveInput(context.Context, runtime.Tx, contract.OwnerRef, Input) error
	ObserveInput(context.Context, contract.OwnerRef, contract.ID) (Observation, error)
}
type Service struct {
	Owner           contract.OwnerRef
	Runner          runtime.TxRunner
	Commands        runtime.CommandStore
	Jobs            runtime.JobStore
	Clock           runtime.Clock
	Repository      Repository
	Permissions     *Permissions
	Reader          contract.CommandFactReader
	Policies        *Policies
	ScheduleControl bool
}

func (s *Service) Record(ctx context.Context, data []byte, trusted *contract.SubjectBinding) (contract.TransportOutcome, error) {
	record, err := DecodeRecord(data)
	if err != nil {
		return contract.TransportOutcome{}, err
	}
	envelope := record.Envelope
	targetOwner := contract.OwnerRef{TenantID: envelope.Target.TenantID, OwnerID: envelope.Target.OwnerID}
	if trusted == nil || targetOwner != s.Owner {
		return contract.TransportOutcome{}, publicError("forbidden", nil)
	}
	subjectJSON, err := contract.Encode(*trusted)
	if err != nil {
		return contract.TransportOutcome{}, publicError("forbidden", nil)
	}
	subject, err := contract.Decode[contract.SubjectBinding](subjectJSON)
	if err != nil || subject.TenantID != targetOwner.TenantID || !s.Permissions.Allows(subject, targetOwner, "record") {
		return contract.TransportOutcome{}, publicError("forbidden", nil)
	}
	if ctx == nil {
		return contract.TransportOutcome{}, publicError("dependency_unavailable", nil)
	}
	if _, finite := ctx.Deadline(); !finite || ctx.Err() != nil {
		return contract.TransportOutcome{}, publicError("dependency_unavailable", ctx.Err())
	}
	digest, err := contract.CommandDigest(data, subjectJSON)
	if err != nil {
		return contract.TransportOutcome{}, err
	}
	ref := contract.CommandRef{Owner: targetOwner, CommandID: envelope.CommandID}
	cutoff, _ := time.Parse("2006-01-02T15:04:05.000000Z", string(envelope.AcceptBefore))
	result, err := runtime.Admit(ctx, s.Runner, s.Commands, s.Clock, ref, digest, runtime.CommandMetadata{Version: envelope.ContractVersion, Profile: envelope.Profile, Method: envelope.Method, Target: envelope.Target, Subject: subject, AcceptBefore: envelope.AcceptBefore, ExpectedRevision: envelope.ExpectedRevision}, cutoff, func(ctx context.Context, tx runtime.Tx, now time.Time) (contract.CommandReceipt, error) {
		input, err := s.Repository.LockInput(ctx, tx, s.Owner, envelope.Target.ID)
		if err != nil {
			return contract.CommandReceipt{}, err
		}
		if input == nil && envelope.ExpectedRevision != nil || input != nil && (envelope.ExpectedRevision == nil || string(*envelope.ExpectedRevision) != strconv.FormatInt(input.Revision, 10)) {
			return runtime.Rejected(ref, "revision_changed"), nil
		}
		if input != nil && input.Revision == math.MaxInt64 {
			return runtime.Rejected(ref, "unsupported"), nil
		}
		if input == nil {
			input = &Input{ID: envelope.Target.ID, CreatedAt: now}
		}
		input.Revision++
		input.Text = record.Text
		input.UpdatedAt = now
		if err = s.Repository.SaveInput(ctx, tx, s.Owner, *input); err != nil {
			return contract.CommandReceipt{}, err
		}
		if repo, ok := s.Repository.(ScheduleRepository); ok {
			policy := s.Policies.For(s.Owner, input.ID)
			if err = repo.BindSchedule(ctx, tx, s.Owner, input.ID, input.Revision, ScheduleState{Source: "admission", Policy: policy, AdoptedAt: now, Deadline: now.Add(policy.ExecutionLimit), Due: now}); err != nil {
				return contract.CommandReceipt{}, err
			}
		}
		if _, err = s.Jobs.Trigger(ctx, tx, envelope.Target, "project", input.Revision, now); err != nil {
			return contract.CommandReceipt{}, err
		}
		revision := contract.Revision(strconv.FormatInt(input.Revision, 10))
		object := envelope.Target
		object.Revision = &revision
		next := "query_original"
		return contract.NewCommandReceiptApplied(contract.CommandReceiptApplied{CommandRef: ref, ObjectRef: object, Revision: revision, NextAction: &next}), nil
	})
	if err != nil {
		if classified, ok := err.(*contract.ContractError); ok {
			return contract.TransportOutcome{}, classified
		}
		return contract.TransportOutcome{}, publicError("dependency_unavailable", err)
	}
	if _, err = contract.Encode(result); err != nil {
		return contract.TransportOutcome{}, publicError("dependency_unavailable", err)
	}
	return result, nil
}
func (s *Service) Observe(ctx context.Context, id contract.ID, trusted *contract.SubjectBinding) (Observation, error) {
	if trusted == nil {
		return Observation{}, publicError("forbidden", nil)
	}
	encoded, err := contract.Encode(*trusted)
	if err != nil {
		return Observation{}, publicError("forbidden", nil)
	}
	subject, err := contract.Decode[contract.SubjectBinding](encoded)
	if err != nil || !s.Permissions.Allows(subject, s.Owner, "read") {
		return Observation{}, publicError("forbidden", nil)
	}
	if _, err = contract.Encode(id); err != nil {
		return Observation{}, publicError("schema_invalid", err)
	}
	if ctx == nil {
		return Observation{}, publicError("dependency_unavailable", nil)
	}
	if _, finite := ctx.Deadline(); !finite {
		return Observation{}, publicError("dependency_unavailable", nil)
	}
	observation, err := s.Repository.ObserveInput(ctx, s.Owner, id)
	if err != nil {
		return Observation{}, publicError("dependency_unavailable", err)
	}
	return observation, nil
}
func (s *Service) ResolveCommandOwner(_ context.Context, owner contract.OwnerRef) (contract.ResolvedCommandOwner, error) {
	if owner != s.Owner {
		return contract.ResolvedCommandOwner{}, publicError("dependency_unavailable", nil)
	}
	return contract.ResolvedCommandOwner{Owner: s.Owner, Reader: s.Reader}, nil
}
