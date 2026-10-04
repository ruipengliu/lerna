package decision_engine

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/ruipengliu/lerna/contract"
	v "github.com/ruipengliu/lerna/contract/v1_1"
	"github.com/ruipengliu/lerna/runtime"
	"github.com/ruipengliu/lerna/runtime/workpool"
)

type Config struct {
	Owner       v.OwnerRef
	Store       Store
	Authority   Authority
	Source      Source
	Publisher   Publisher
	Component   v.ComponentRef
	Worker      string
	Lease       time.Duration
	PoolControl bool
}
type Service struct{ config Config }

func supportedRuleVersion(version string) bool {
	return version == "fixture-rule/2" || version == "fixture-rule/3"
}

func New(cfg Config) (*Service, error) {
	if _, err := v.Encode(cfg.Owner); err != nil {
		return nil, err
	}
	if _, err := v.Encode(cfg.Component); err != nil {
		return nil, err
	}
	if cfg.Store == nil || cfg.Authority == nil || cfg.Source == nil || cfg.Publisher == nil || cfg.Worker == "" || len(cfg.Worker) > 128 || cfg.Lease <= 0 || cfg.Lease > time.Minute {
		return nil, runtime.ErrWorkBounds
	}
	return &Service{config: cfg}, nil
}
func finite(ctx context.Context) error {
	if ctx == nil {
		return ErrUnavailable
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrUnavailable
	}
	return ctx.Err()
}
func oldOwner(owner v.OwnerRef) contract.OwnerRef {
	return contract.OwnerRef{TenantID: contract.ID(owner.TenantID), OwnerID: contract.ID(owner.OwnerID)}
}
func decisionOwner(ref v.DecisionRef) v.OwnerRef {
	return v.OwnerRef{TenantID: ref.TenantID, OwnerID: ref.OwnerID}
}
func oldObject(ref v.DecisionRef) contract.ObjectRef {
	return contract.ObjectRef{TenantID: contract.ID(ref.TenantID), OwnerID: contract.ID(ref.OwnerID), Kind: "decision", ID: contract.ID(ref.ID)}
}
func fromObject(ref contract.ObjectRef) v.DecisionRef {
	return v.DecisionRef{TenantID: v.ID(ref.TenantID), OwnerID: v.ID(ref.OwnerID), Kind: "decision", ID: v.ID(ref.ID)}
}
func fixedTime(value v.Time) (time.Time, error) {
	return time.Parse("2006-01-02T15:04:05.000000Z", string(value))
}
func zeroUsage(unit string) v.DecisionUsage {
	return v.DecisionUsage{InputBytes: "0", OutputBytes: "0", RuleSteps: "0", ModelRequests: "0", RuleStarts: "0", MeasurementsComplete: true, Cost: v.Amount{Unit: unit, IntegerValue: "0"}}
}
func refusal(code v.ErrorCode, cause error) error {
	return &v.ContractError{PublicError: v.PublicError{Code: code}, Cause: cause}
}
func cloneSubject(trusted *v.SubjectBinding) (v.SubjectBinding, error) {
	if trusted == nil {
		return v.SubjectBinding{}, ErrForbidden
	}
	data, err := v.Encode(*trusted)
	if err != nil {
		return v.SubjectBinding{}, ErrForbidden
	}
	return v.Decode[v.SubjectBinding](data)
}
func (s *Service) authorize(ctx context.Context, trusted *v.SubjectBinding, ref v.DecisionRef, purpose string, input *v.DecisionDecidePayload) (Permission, error) {
	if trusted == nil || trusted.TenantID != ref.TenantID || decisionOwner(ref) != s.config.Owner {
		return Permission{}, ErrForbidden
	}
	return s.observePermission(ctx, *trusted, ref, purpose, input)
}

// Authorizers receive independent snapshots. A callback cannot rewrite bytes
// already bound to the original Command or a durable Decision input.
func (s *Service) observePermission(ctx context.Context, subject v.SubjectBinding, ref v.DecisionRef, purpose string, input *v.DecisionDecidePayload) (Permission, error) {
	if err := finite(ctx); err != nil {
		return Permission{}, err
	}
	principal, err := cloneSubject(&subject)
	if err != nil {
		return Permission{}, err
	}
	if principal.TenantID != ref.TenantID {
		return Permission{}, ErrForbidden
	}
	delegate, err := cloneSubject(&principal)
	if err != nil {
		return Permission{}, err
	}
	var payload *v.DecisionDecidePayload
	if input != nil {
		data, err := v.Encode(*input)
		if err != nil {
			return Permission{}, err
		}
		copy, err := v.Decode[v.DecisionDecidePayload](data)
		if err != nil {
			return Permission{}, err
		}
		payload = &copy
	}
	permission, err := s.config.Authority.Authorize(ctx, delegate, ref, purpose, payload)
	if err != nil {
		return Permission{}, err
	}
	// Freeze the returned observation before another injected boundary executes.
	data, err := json.Marshal(permission)
	if err != nil {
		return Permission{}, err
	}
	var frozen Permission
	if err = json.Unmarshal(data, &frozen); err != nil {
		return Permission{}, err
	}
	permission = frozen
	left, err := v.Encode(permission.Subject)
	if err != nil {
		return Permission{}, ErrForbidden
	}
	right, err := v.Encode(principal)
	if err != nil {
		return Permission{}, ErrForbidden
	}
	if string(left) != string(right) || permission.DecisionOwner != decisionOwner(ref) {
		return Permission{}, ErrForbidden
	}
	return permission, nil
}
func permissionCurrent(permission Permission, now time.Time) error {
	if !now.Before(permission.ValidUntil) {
		return ErrForbidden
	}
	return nil
}
func rejected(ref v.CommandRef, reason v.ErrorCode) v.CommandReceipt {
	next := "resolve_rejection"
	return v.NewCommandReceiptRejected(v.CommandReceiptRejected{CommandRef: ref, Reason: reason, NextAction: &next})
}
func accepted(ref v.CommandRef, decision v.DecisionRef) v.CommandReceipt {
	revision := v.Revision("1")
	return v.NewCommandReceiptAccepted(v.CommandReceiptAccepted{CommandRef: ref, ObjectRef: v.ObjectRef{TenantID: decision.TenantID, OwnerID: decision.OwnerID, Kind: "decision", ID: decision.ID}, Revision: &revision})
}

// Decide persists one fixed input and one original receipt in the same owner
// transaction as its Job. Immutable fixture I/O is performed outside that Tx.
func (s *Service) Decide(ctx context.Context, data []byte, trusted *v.SubjectBinding) (v.TransportOutcome, error) {
	request, err := v.DecodeDecide(data)
	if err != nil {
		return v.TransportOutcome{}, err
	}
	data, err = v.Encode(request)
	if err != nil {
		return v.TransportOutcome{}, err
	}
	permission, err := s.authorize(ctx, trusted, request.Target, "decide", &request.Payload)
	if err != nil {
		return v.TransportOutcome{}, refusal("forbidden", err)
	}
	subjectJSON, err := v.Encode(permission.Subject)
	if err != nil {
		return v.TransportOutcome{}, err
	}
	digest, err := v.CommandDigest(data, subjectJSON)
	if err != nil {
		return v.TransportOutcome{}, err
	}
	inputDigest, err := v.DecisionInputDigest(request, permission.Subject)
	if err != nil {
		return v.TransportOutcome{}, err
	}
	cutoff, err := fixedTime(request.AcceptBefore)
	if err != nil {
		return v.TransportOutcome{}, err
	}
	command := v.CommandRef{Owner: s.config.Owner, CommandID: request.CommandID}
	var receipt v.CommandReceipt
	err = s.config.Store.Within(ctx, oldOwner(s.config.Owner), func(ctx context.Context, tx runtime.Tx) error {
		prior, err := s.config.Store.LockCommand(ctx, tx, command)
		if err != nil {
			return err
		}
		if prior != nil {
			if prior.Digest != digest {
				return refusal("idempotency_conflict", nil)
			}
			receipt = prior.Receipt
			return nil
		}
		now, err := s.config.Store.Now(ctx, tx)
		if err != nil {
			return err
		}
		if err = permissionCurrent(permission, now); err != nil {
			return err
		}
		// Reconfiguration retires new use of the old binding; an authenticated
		// original Command above retains its immutable receipt.
		if !now.Before(cutoff) {
			receipt = rejected(command, "expired")
		} else if request.Payload.ComponentRef != s.config.Component || permission.ChargeBasis != "durable_rule_start" || !supportedRuleVersion(permission.RuleVersion) {
			receipt = rejected(command, "unsupported")
		}
		if _, refused := receipt.AsRejected(); refused {
			return s.config.Store.SaveCommand(ctx, tx, command, CommandRecord{Digest: digest, Request: request, Subject: permission.Subject, Receipt: receipt})
		}
		state, err := s.config.Store.LockPool(ctx, tx)
		if err != nil {
			return err
		}
		now, err = s.config.Store.Now(ctx, tx)
		if err != nil {
			return err
		}
		if err = permissionCurrent(permission, now); err != nil {
			return err
		}
		original, err := s.config.Store.LockDecision(ctx, tx, request.Target)
		if err != nil {
			return err
		}
		now, err = s.config.Store.Now(ctx, tx)
		if err != nil {
			return err
		}
		if err = permissionCurrent(permission, now); err != nil {
			return err
		}
		if !now.Before(cutoff) {
			receipt = rejected(command, "expired")
		} else if permission.ChargeBasis != "durable_rule_start" || !supportedRuleVersion(permission.RuleVersion) {
			receipt = rejected(command, "unsupported")
		} else if original != nil {
			if original.InputDigest != inputDigest {
				receipt = rejected(command, "decision_mismatch")
			} else if original.Status == "cancelled" {
				receipt = rejected(command, "decision_cancelled")
			} else {
				receipt = accepted(command, request.Target)
			}
		} else {
			if _, _, err = s.config.Store.PoolQueue(ctx, tx, state, oldObject(request.Target), "decide", "ordinary"); err != nil {
				return err
			}
			record := Record{Ref: request.Target, Input: &request.Payload, Subject: permission.Subject, InputDigest: inputDigest, Revision: 1, Status: "accepted", ArtifactRefs: []v.ContentRef{}, Usage: zeroUsage(request.Payload.Limits.MaxCost.Unit), OriginalPermission: &permission}
			if err = s.config.Store.SaveDecision(ctx, tx, record); err != nil {
				return err
			}
			if _, err = s.config.Store.Trigger(ctx, tx, oldObject(request.Target), "decide", 1, now); err != nil {
				return err
			}
			receipt = accepted(command, request.Target)
		}
		return s.config.Store.SaveCommand(ctx, tx, command, CommandRecord{Digest: digest, Request: request, Subject: permission.Subject, Receipt: receipt})
	})
	if errors.Is(err, runtime.ErrCommitUnknown) {
		return v.NewTransportOutcomeCommitUnknown(v.TransportOutcomeCommitUnknown{CommandRef: command, NextAction: "query_or_retransmit_original"}), nil
	}
	if err != nil {
		return v.TransportOutcome{}, err
	}
	return v.NewTransportOutcomeReceived(v.TransportOutcomeReceived{Receipt: receipt}), nil
}
func (r Record) Public() (v.Decision, error) {
	if r.Revision < 1 {
		return v.Decision{}, ErrUnavailable
	}
	revision := v.Revision(strconv.FormatInt(r.Revision, 10))
	digest := v.SchemaDigest(r.InputDigest)
	if r.Status == "cancelled" {
		if r.ControlBasis == nil {
			return v.Decision{}, ErrUnavailable
		}
		var task v.TaskObjectRef
		if r.CloseTaskRef != nil {
			task = *r.CloseTaskRef
		} else if r.Input != nil {
			task = r.Input.TaskRef
		}
		return v.NewDecisionCancelled(v.DecisionCancelled{DecisionRef: r.Ref, Revision: revision, InputDigest: digest, TaskRef: task, ControlBasis: *r.ControlBasis, Reason: r.Reason, Usage: r.Usage, Input: r.Input}), nil
	}
	if r.Input == nil {
		return v.Decision{}, ErrUnavailable
	}
	switch r.Status {
	case "accepted":
		return v.NewDecisionAccepted(v.DecisionAccepted{DecisionRef: r.Ref, Revision: revision, InputDigest: digest, Input: *r.Input, Usage: r.Usage}), nil
	case "waiting":
		return v.NewDecisionWaiting(v.DecisionWaiting{DecisionRef: r.Ref, Revision: revision, InputDigest: digest, Input: *r.Input, Usage: r.Usage, WakeAt: r.WakeAt, Reason: r.Reason}), nil
	case "running":
		return v.NewDecisionRunning(v.DecisionRunning{DecisionRef: r.Ref, Revision: revision, InputDigest: digest, Input: *r.Input, Usage: r.Usage}), nil
	case "completed":
		if r.Proposal == nil || r.ProposalRef == nil {
			return v.Decision{}, ErrUnavailable
		}
		return v.NewDecisionCompleted(v.DecisionCompleted{DecisionRef: r.Ref, Revision: revision, InputDigest: digest, Input: *r.Input, Usage: r.Usage, Proposal: *r.Proposal, ProposalRef: *r.ProposalRef, ArtifactRefs: r.ArtifactRefs}), nil
	case "failed":
		if r.Failure == nil {
			return v.Decision{}, ErrUnavailable
		}
		return v.NewDecisionFailed(v.DecisionFailed{DecisionRef: r.Ref, Revision: revision, InputDigest: digest, Input: *r.Input, Usage: r.Usage, Failure: *r.Failure}), nil
	default:
		return v.Decision{}, ErrUnavailable
	}
}
func (s *Service) Get(ctx context.Context, data []byte, trusted *v.SubjectBinding) (v.DecisionGetResponse, error) {
	request, err := v.DecodeGet(data)
	if err != nil {
		return v.DecisionGetResponse{}, err
	}
	rejectedView := func(reason v.ErrorCode) v.DecisionGetResponse {
		return v.NewDecisionGetResponseRejected(v.DecisionGetResponseRejected{Reason: reason})
	}
	unavailable := func() v.DecisionGetResponse {
		return v.NewDecisionGetResponseUnavailable(v.DecisionGetResponseUnavailable{DecisionRef: request.Target, Reason: "dependency_unavailable"})
	}
	permission, err := s.authorize(ctx, trusted, request.Target, "get", nil)
	if err != nil {
		if errors.Is(err, ErrForbidden) {
			return rejectedView("forbidden"), nil
		}
		return unavailable(), nil
	}
	cutoff, err := fixedTime(request.AcceptBefore)
	if err != nil {
		return v.DecisionGetResponse{}, err
	}
	var result v.DecisionGetResponse
	err = s.config.Store.Within(ctx, oldOwner(s.config.Owner), func(ctx context.Context, tx runtime.Tx) error {
		now, err := s.config.Store.Now(ctx, tx)
		if err != nil {
			return err
		}
		if !now.Before(cutoff) {
			result = rejectedView("expired")
			return nil
		}
		if err = permissionCurrent(permission, now); err != nil {
			result = rejectedView("forbidden")
			return nil
		}
		record, err := s.config.Store.ReadDecision(ctx, tx, request.Target)
		if err != nil {
			return err
		}
		if record == nil {
			result = v.NewDecisionGetResponseResultUnavailable(v.DecisionGetResponseResultUnavailable{DecisionRef: request.Target})
			return nil
		}
		public, err := record.Public()
		if err != nil {
			return err
		}
		result = v.NewDecisionGetResponseFound(v.DecisionGetResponseFound{DecisionRef: request.Target, Decision: public})
		return nil
	})
	if err != nil {
		return unavailable(), nil
	}
	encoded, err := v.Encode(result)
	if err != nil {
		return unavailable(), nil
	}
	return v.Decode[v.DecisionGetResponse](encoded)
}
func (s *Service) InstallPool(ctx context.Context, cfg workpool.Config, expected int64) error {
	if !s.config.PoolControl {
		return ErrForbidden
	}
	if err := finite(ctx); err != nil {
		return err
	}
	return s.config.Store.Within(ctx, oldOwner(s.config.Owner), func(ctx context.Context, tx runtime.Tx) error {
		now, err := s.config.Store.Now(ctx, tx)
		if err != nil {
			return err
		}
		return s.config.Store.InstallPool(ctx, tx, cfg, expected, now)
	})
}

func (s *Service) ObservePool(ctx context.Context) (workpool.Observation, error) {
	result := workpool.Observation{Active: map[string]int64{}, Queued: map[string]int64{}}
	if !s.config.PoolControl {
		return result, ErrForbidden
	}
	if err := finite(ctx); err != nil {
		return result, err
	}
	err := s.config.Store.Within(ctx, oldOwner(s.config.Owner), func(ctx context.Context, tx runtime.Tx) error {
		state, err := s.config.Store.LockPool(ctx, tx)
		if err != nil {
			return err
		}
		result.State = state
		result.Scope, err = s.config.Store.PoolScope(ctx, tx)
		if err != nil {
			return err
		}
		now, err := s.config.Store.Now(ctx, tx)
		if err != nil {
			return err
		}
		for _, limit := range state.Config.Limits {
			active, _, queued, err := s.config.Store.PoolCounts(ctx, tx, state, limit.Lane, contract.ID(s.config.Owner.TenantID), now)
			if err != nil {
				return err
			}
			result.Active[limit.Lane] = active
			result.Queued[limit.Lane] = queued
		}
		return nil
	})
	return result, err
}
