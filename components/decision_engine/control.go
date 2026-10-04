package decision_engine

import (
	"context"
	"encoding/json"
	"errors"
	v "github.com/ruipengliu/lerna/contract/v1_1"
	"github.com/ruipengliu/lerna/runtime"
	"math/big"
	"reflect"
	"strconv"
	"time"
)

func (s DecisionStop) Current() v.DecisionCurrentControl {
	return v.DecisionCurrentControl{TaskRef: s.TaskRef, DecisionInputDigest: s.InputDigest, ControlBasis: s.ControlBasis, Scope: "local_decision_work"}
}
func stopDigest(ref v.DecisionRef, task v.TaskObjectRef, input v.SchemaDigest, basis v.ControlBasis) (string, error) {
	body, err := json.Marshal(struct {
		Ref   v.DecisionRef   `json:"decision_ref"`
		Task  v.TaskObjectRef `json:"task_ref"`
		Input v.SchemaDigest  `json:"input_digest"`
		Basis v.ControlBasis  `json:"control_basis"`
	}{ref, task, input, basis})
	if err != nil {
		return "", err
	}
	return hash(append([]byte("lerna-decision-stop-1\n"), body...)), nil
}
func (s DecisionStop) Validate() error {
	if _, err := v.Encode(s.Ref); err != nil {
		return err
	}
	if _, err := v.Encode(s.Current()); err != nil {
		return err
	}
	if _, err := v.Encode(s.Subject); err != nil {
		return err
	}
	if _, err := v.Encode(s.CommandRef); err != nil {
		return err
	}
	if s.CommandRef.Owner != decisionOwner(s.Ref) || s.Subject.TenantID != s.Ref.TenantID || s.TaskRef.TenantID != s.Ref.TenantID || s.ControlBasis.IssuerOwner != (v.OwnerRef{TenantID: s.TaskRef.TenantID, OwnerID: s.TaskRef.OwnerID}) || s.ControlBasis.ProofRef.Owner != s.ControlBasis.IssuerOwner {
		return ErrUnavailable
	}
	digest, err := stopDigest(s.Ref, s.TaskRef, s.InputDigest, s.ControlBasis)
	if err != nil || digest != s.BindingDigest {
		return ErrUnavailable
	}
	return nil
}
func compareRevision(a, b v.Revision) (int, error) {
	if v.Validate("Revision", string(a)) != nil || v.Validate("Revision", string(b)) != nil {
		return 0, ErrUnavailable
	}
	left, ok := new(big.Int).SetString(string(a), 10)
	if !ok {
		return 0, ErrUnavailable
	}
	right, ok := new(big.Int).SetString(string(b), 10)
	if !ok {
		return 0, ErrUnavailable
	}
	return left.Cmp(right), nil
}
func (s *Service) controlAccess(ctx context.Context, trusted *v.SubjectBinding, ref v.DecisionRef, purpose string) (ControlAccess, error) {
	if err := finite(ctx); err != nil {
		return ControlAccess{}, err
	}
	if s.config.ControlAuthority == nil {
		return ControlAccess{}, ErrUnavailable
	}
	original, err := cloneSubject(trusted)
	if err != nil {
		return ControlAccess{}, err
	}
	if decisionOwner(ref) != s.config.Owner || original.TenantID != ref.TenantID {
		return ControlAccess{}, ErrForbidden
	}
	delegated, err := cloneSubject(&original)
	if err != nil {
		return ControlAccess{}, err
	}
	observed, err := s.config.ControlAuthority.AuthorizeControl(ctx, delegated, ref, purpose)
	if err != nil {
		return ControlAccess{}, err
	}
	body, err := json.Marshal(observed)
	if err != nil {
		return ControlAccess{}, ErrUnavailable
	}
	var access ControlAccess
	if err = json.Unmarshal(body, &access); err != nil {
		return ControlAccess{}, ErrUnavailable
	}
	if _, err = v.Encode(access.Subject); err != nil {
		return ControlAccess{}, ErrForbidden
	}
	if !reflect.DeepEqual(access.Subject, original) || access.DecisionOwner != s.config.Owner || access.ValidUntil.IsZero() {
		return ControlAccess{}, ErrForbidden
	}
	if purpose == "command.get" {
		if access.DecisionRef != nil {
			return ControlAccess{}, ErrForbidden
		}
	} else if access.DecisionRef == nil || *access.DecisionRef != ref {
		return ControlAccess{}, ErrForbidden
	}
	found := false
	seen := map[string]bool{}
	for _, p := range access.Purposes {
		if seen[p] || (p != "cancel" && p != "get" && p != "command.get") || (p == "command.get") != (access.DecisionRef == nil) {
			return ControlAccess{}, ErrForbidden
		}
		seen[p] = true
		if p == purpose {
			found = true
		}
	}
	if !found {
		return ControlAccess{}, ErrForbidden
	}
	return access, nil
}
func (s *Service) verifyControl(ctx context.Context, subject v.SubjectBinding, payload v.DecisionCancelPayload, input *v.DecisionDecidePayload) (*v.Revision, error) {
	delegated, err := cloneSubject(&subject)
	if err != nil {
		return nil, err
	}
	body, err := v.Encode(payload)
	if err != nil {
		return nil, err
	}
	payload, err = v.Decode[v.DecisionCancelPayload](body)
	if err != nil {
		return nil, err
	}
	if input != nil {
		body, err = v.Encode(*input)
		if err != nil {
			return nil, err
		}
		detached, err := v.Decode[v.DecisionDecidePayload](body)
		if err != nil {
			return nil, err
		}
		input = &detached
	}
	floor, err := s.config.ControlAuthority.VerifyControl(ctx, delegated, payload, input)
	if err != nil {
		return nil, err
	}
	if floor != nil {
		copy := *floor
		if v.Validate("Revision", string(copy)) != nil {
			return nil, ErrUnavailable
		}
		floor = &copy
	}
	if (input == nil) != (floor == nil) {
		return nil, ErrUnavailable
	}
	return floor, nil
}
func (s *Service) readAccess(ctx context.Context, trusted *v.SubjectBinding, ref v.DecisionRef, purpose string) (time.Time, error) {
	permission, err := s.authorize(ctx, trusted, ref, purpose, nil)
	if err == nil {
		return permission.ValidUntil, nil
	}
	if s.config.ControlAuthority == nil {
		return time.Time{}, err
	}
	access, controlErr := s.controlAccess(ctx, trusted, ref, purpose)
	if controlErr == nil {
		return access.ValidUntil, nil
	}
	if errors.Is(err, ErrForbidden) && errors.Is(controlErr, ErrForbidden) {
		return time.Time{}, ErrForbidden
	}
	return time.Time{}, ErrUnavailable
}
func readTask(record *Record) v.TaskObjectRef {
	if record.CloseTaskRef != nil {
		return *record.CloseTaskRef
	}
	if record.Input != nil {
		return record.Input.TaskRef
	}
	return v.TaskObjectRef{}
}
func applied(command v.CommandRef, record *Record) v.CommandReceipt {
	return v.NewCommandReceiptApplied(v.CommandReceiptApplied{CommandRef: command, ObjectRef: v.ObjectRef{TenantID: record.Ref.TenantID, OwnerID: record.Ref.OwnerID, Kind: "decision", ID: record.Ref.ID}, Revision: v.Revision(strconv.FormatInt(record.Revision, 10))})
}

var errControlInputAppeared = errors.New("control needs immutable input floor")

// Cancel adopts trusted stop control for this owner only. Source proof I/O is
// outside owner locks, and an in-flight independent publication may still end.
func controlFailure(err error) error {
	if errors.Is(err, ErrForbidden) {
		return refusal("forbidden", err)
	}
	return refusal("dependency_unavailable", err)
}

func (s *Service) Cancel(ctx context.Context, data []byte, trusted *v.SubjectBinding) (v.TransportOutcome, error) {
	request, err := v.DecodeCancel(data)
	if err != nil {
		return v.TransportOutcome{}, err
	}
	data, err = v.Encode(request)
	if err != nil {
		return v.TransportOutcome{}, err
	}
	access, err := s.controlAccess(ctx, trusted, request.Target, "cancel")
	if err != nil {
		return v.TransportOutcome{}, controlFailure(err)
	}
	principal, err := v.Encode(access.Subject)
	if err != nil {
		return v.TransportOutcome{}, err
	}
	digest, err := v.CommandDigest(data, principal)
	if err != nil {
		return v.TransportOutcome{}, err
	}
	command := v.CommandRef{Owner: s.config.Owner, CommandID: request.CommandID}
	cutoff, err := fixedTime(request.AcceptBefore)
	if err != nil {
		return v.TransportOutcome{}, err
	}
	proofUntil, err := fixedTime(request.Payload.ControlBasis.ValidUntil)
	if err != nil {
		return v.TransportOutcome{}, err
	}
	binding, err := stopDigest(request.Target, request.Payload.TaskRef, request.Payload.DecisionInputDigest, request.Payload.ControlBasis)
	if err != nil {
		return v.TransportOutcome{}, err
	}
	var receipt v.CommandReceipt
	var input *v.DecisionDecidePayload
	replay := false
	err = s.config.Store.Within(ctx, oldOwner(s.config.Owner), func(ctx context.Context, tx runtime.Tx) error {
		prior, err := s.config.Store.LockCommand(ctx, tx, command)
		if err != nil {
			return err
		}
		now, err := s.config.Store.Now(ctx, tx)
		if err != nil {
			return err
		}
		if !now.Before(access.ValidUntil) {
			return refusal("forbidden", ErrForbidden)
		}
		if prior != nil {
			if prior.Digest != digest {
				return refusal("idempotency_conflict", nil)
			}
			receipt = prior.Receipt
			replay = true
			return nil
		}
		record, err := s.config.Store.ReadDecision(ctx, tx, request.Target)
		if err != nil {
			return err
		}
		if record != nil && record.InputDigest == string(request.Payload.DecisionInputDigest) && readTask(record) == request.Payload.TaskRef {
			input = record.Input
		}
		return nil
	})
	if err != nil {
		return v.TransportOutcome{}, err
	}
	if replay {
		return v.NewTransportOutcomeReceived(v.TransportOutcomeReceived{Receipt: receipt}), nil
	}
	// Only input absence can require a repeat; immutable identity, rather than
	// normal worker revision, determines when the verified floor remains valid.
	for attempt := 0; attempt < 3; attempt++ {
		floor, err := s.verifyControl(ctx, access.Subject, request.Payload, input)
		if err != nil {
			return v.TransportOutcome{}, controlFailure(err)
		}
		err = s.config.Store.Within(ctx, oldOwner(s.config.Owner), func(ctx context.Context, tx runtime.Tx) error {
			prior, err := s.config.Store.LockCommand(ctx, tx, command)
			if err != nil {
				return err
			}
			now, err := s.config.Store.Now(ctx, tx)
			if err != nil {
				return err
			}
			if !now.Before(access.ValidUntil) {
				return refusal("forbidden", ErrForbidden)
			}
			if prior != nil {
				if prior.Digest != digest {
					return refusal("idempotency_conflict", nil)
				}
				receipt = prior.Receipt
				return nil
			}
			if _, err = s.config.Store.LockPool(ctx, tx); err != nil {
				return err
			}
			record, err := s.config.Store.LockDecision(ctx, tx, request.Target)
			if err != nil {
				return err
			}
			stop, err := s.config.Store.ReadStop(ctx, tx, request.Target)
			if err != nil {
				return err
			}
			now, err = s.config.Store.Now(ctx, tx)
			if err != nil {
				return err
			}
			if !now.Before(access.ValidUntil) {
				return refusal("forbidden", ErrForbidden)
			}
			if !now.Before(cutoff) {
				receipt = rejected(command, "expired")
			} else if !now.Before(proofUntil) {
				return refusal("forbidden", ErrForbidden)
			} else if (record != nil && (record.InputDigest != string(request.Payload.DecisionInputDigest) || readTask(record) != request.Payload.TaskRef)) || (stop != nil && (stop.InputDigest != request.Payload.DecisionInputDigest || stop.TaskRef != request.Payload.TaskRef)) {
				receipt = rejected(command, "decision_mismatch")
			} else {
				if record != nil && record.Input != nil && (input == nil || !reflect.DeepEqual(input, record.Input)) {
					input = record.Input
					return errControlInputAppeared
				}
				if stop != nil {
					if record == nil {
						return ErrUnavailable
					}
					order, err := compareRevision(request.Payload.ControlBasis.ControlRevision, stop.ControlBasis.ControlRevision)
					if err != nil {
						return err
					}
					if order < 0 {
						receipt = rejected(command, "revision_changed")
					} else if order == 0 {
						if stop.BindingDigest != binding {
							receipt = rejected(command, "decision_mismatch")
						} else {
							receipt = applied(command, record)
						}
					}
				} else if floor != nil {
					order, err := compareRevision(request.Payload.ControlBasis.ControlRevision, *floor)
					if err != nil {
						return err
					}
					if order <= 0 {
						receipt = rejected(command, "revision_changed")
					}
				}
				if _, rejected := receipt.AsRejected(); !rejected {
					if _, already := receipt.AsApplied(); !already {
						if record == nil {
							task := request.Payload.TaskRef
							basis := request.Payload.ControlBasis
							record = &Record{Ref: request.Target, Subject: access.Subject, InputDigest: string(request.Payload.DecisionInputDigest), Revision: 1, Status: "cancelled", ControlBasis: &basis, CloseTaskRef: &task, Reason: request.Payload.Reason, Usage: zeroUsage("fixture"), ArtifactRefs: []v.ContentRef{}}
							if err = s.config.Store.SaveDecision(ctx, tx, *record); err != nil {
								return err
							}
						} else if !terminal(record.Status) {
							record.Revision++
							record.Status = "cancelled"
							basis := request.Payload.ControlBasis
							record.ControlBasis = &basis
							record.Reason = request.Payload.Reason
							record.Proposal = nil
							record.ProposalRef = nil
							if err = s.config.Store.SaveDecision(ctx, tx, *record); err != nil {
								return err
							}
							job, err := s.config.Store.LockDecisionJob(ctx, tx, request.Target)
							if err != nil {
								return err
							}
							if job == nil {
								return ErrUnavailable
							}
							if err = s.config.Store.StopRevision(ctx, tx, *job, job.WorkRevision); err != nil {
								return err
							}
						}
						adopted := DecisionStop{Ref: request.Target, TaskRef: request.Payload.TaskRef, InputDigest: request.Payload.DecisionInputDigest, ControlBasis: request.Payload.ControlBasis, BindingDigest: binding, Subject: access.Subject, CommandRef: command}
						if err = s.config.Store.SaveStop(ctx, tx, adopted); err != nil {
							return err
						}
						receipt = applied(command, record)
					}
				}
			}
			return s.config.Store.SaveCommand(ctx, tx, command, CommandRecord{MetadataVersion: "2", Method: "cancel", Digest: digest, CancelRequest: &request, Subject: access.Subject, Receipt: receipt})
		})
		if errors.Is(err, errControlInputAppeared) {
			continue
		}
		if errors.Is(err, runtime.ErrCommitUnknown) {
			return v.NewTransportOutcomeCommitUnknown(v.TransportOutcomeCommitUnknown{CommandRef: command, NextAction: "query_or_retransmit_original"}), nil
		}
		if err != nil {
			return v.TransportOutcome{}, err
		}
		return v.NewTransportOutcomeReceived(v.TransportOutcomeReceived{Receipt: receipt}), nil
	}
	return v.TransportOutcome{}, ErrUnavailable
}
