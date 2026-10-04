package governance

import (
	"context"
	"sort"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

type PendingActivation struct {
	ID                string            `json:"id"`
	Revision          uint64            `json:"revision"`
	Activation        Activation        `json:"activation"`
	SubjectID         string            `json:"subject_id"`
	State             string            `json:"state"`
	CandidateEvidence *InstanceEvidence `json:"candidate_evidence,omitempty"`
	BlockedReason     string            `json:"blocked_reason,omitempty"`
}
type InstanceStop struct {
	ID           string         `json:"id"`
	Revision     uint64         `json:"revision"`
	ActivationID string         `json:"activation_id"`
	InstanceID   string         `json:"instance_id"`
	Generation   uint64         `json:"generation"`
	State        string         `json:"state"`
	Evidence     *FenceEvidence `json:"evidence,omitempty"`
}
type RolloutObservation struct {
	ApprovalRef          api.ObjectRef  `json:"approval_ref"`
	TargetID             string         `json:"target_id"`
	Samples              uint64         `json:"samples"`
	ObservationStartedAt string         `json:"observation_started_at"`
	ErrorRate            string         `json:"error_rate"`
	UnknownEffects       uint64         `json:"unknown_effects"`
	EvidenceRef          api.ContentRef `json:"evidence_ref"`
}
type rolloutFact struct {
	Revision    uint64             `json:"revision"`
	Observation RolloutObservation `json:"observation"`
	InstanceRef api.ObjectRef      `json:"instance_ref"`
	Generation  uint64             `json:"generation"`
	BatchIndex  uint64             `json:"batch_index"`
}

func prepareKey(lock api.ComponentRef, target string) string {
	return digestID("prepare", []any{lock, target})
}
func (s *Service) registerExtensions(r *runtime.Registry) error {
	for _, fn := range []func() error{
		func() error {
			return registerCommand[TargetRegister, BindingHead](s, r, "extensions.target.register", false, false, s.registerTarget)
		},
		func() error {
			return registerCommand[PrepareRequest, StateOutput](s, r, "extensions.prepare", false, true, s.prepare)
		},
		func() error {
			return registerCommand[ActivateRequest, StateOutput](s, r, "extensions.activate", true, true, s.activate)
		},
		func() error {
			return registerCommand[DeactivateRequest, StateOutput](s, r, "extensions.deactivate", true, false, s.deactivate)
		},
		func() error {
			return registerCommand[ReopenRequest, StateOutput](s, r, "extensions.reopen", true, true, s.reopen)
		},
		func() error {
			return registerCommand[DisposeRequest, StateOutput](s, r, "extensions.dispose", true, true, s.dispose)
		},
		func() error { return registerQuery[IDInput, ExtensionRead](r, "extensions.read", s.readExtension) },
		func() error {
			return registerQuery[api.ListInput, api.Page[BindingHead]](r, "extensions.list", queryPage[BindingHead]("heads", "maintainer"))
		},
		func() error {
			return registerQuery[IDInput, InstanceStop](r, "extensions.instance_stop.read", queryByID[InstanceStop]("instance_stops", func(a runtime.Auth, v InstanceStop) error { return requireRole(a, "maintainer") }))
		},
		func() error {
			return registerCommand[ApprovalCreate, ConfirmedOutput](s, r, "release.approval.create", false, true, s.createApproval)
		},
		func() error {
			return registerCommand[RefInput, StateOutput](s, r, "release.approval.revoke", true, false, s.revokeApproval)
		},
		func() error {
			return registerQuery[IDInput, ReleaseApproval](r, "release.approval.read", queryByID[ReleaseApproval]("approvals", func(a runtime.Auth, v ReleaseApproval) error { return requireRole(a, "release_approver") }))
		},
		func() error {
			return registerCommand[ApprovalUseRequest, ApprovalUse](s, r, "release.approval.use", false, false, func(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in ApprovalUseRequest) (runtime.Outcome, error) {
				if err := requireRole(a, "service"); err != nil {
					return runtime.Outcome{}, err
				}
				out, err := s.ApprovalUseTx(ctx, tx, in)
				return runtime.Applied(out), err
			})
		},
		func() error {
			return registerCommand[RolloutObservation, StateOutput](s, r, "release.rollout.advance", true, false, s.advanceRollout)
		},
	} {
		if err := fn(); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) registerTarget(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in TargetRegister) (runtime.Outcome, error) {
	if err := requireRole(a, "maintainer"); err != nil {
		return runtime.Outcome{}, err
	}
	if in.TargetID != c.TargetID || !api.ValidID(in.TargetID) || in.DataFormat == "" {
		return runtime.Outcome{}, api.E("invalid_request", "target_registration_invalid")
	}
	head := BindingHead{TargetID: in.TargetID, Revision: 1, DataFormat: in.DataFormat}
	if err := tx.Create(ctx, ns("heads"), in.TargetID, "", head); err != nil {
		return runtime.Outcome{}, err
	}
	return runtime.Applied(head), nil
}
func (s *Service) prepare(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in PrepareRequest) (runtime.Outcome, error) {
	if err := requireRole(a, "maintainer"); err != nil {
		return runtime.Outcome{}, err
	}
	if s.Ports.Lifecycle == nil {
		return runtime.Outcome{}, api.E("unsupported", "trusted_lifecycle_unavailable")
	}
	if admission, ok := s.Ports.Lifecycle.(InstallationAdmission); ok {
		if err := admission.CheckInstallation(in.Installation); err != nil {
			return runtime.Outcome{}, err
		}
	}
	if err := ownerRef(tx.Scope(), in.TargetRef); err != nil {
		return runtime.Outcome{}, err
	}
	var head BindingHead
	if _, err := tx.Get(ctx, ns("heads"), in.TargetRef.ObjectID, &head); err != nil {
		return runtime.Outcome{}, err
	}
	if in.Installation.Profile != api.Profile || len(in.Installation.Artifacts) == 0 || len(in.Installation.Artifacts) > 32 || len(in.Installation.DependencyRefs) > 32 {
		return runtime.Outcome{}, api.E("invalid_request", "install_lock_invalid")
	}
	for _, ref := range in.Installation.Artifacts {
		if ref.TenantID != a.TenantID {
			return runtime.Outcome{}, api.E("forbidden", "artifact_scope_mismatch")
		}
	}
	lockID := componentKey(in.Installation.InstallLockRef)
	var existing PreparedInstall
	_, err := tx.Get(ctx, ns("installations"), lockID, &existing)
	if errMissing(err) {
		existing = PreparedInstall{ID: lockID, Revision: 1, Installation: in.Installation, TargetRef: in.TargetRef, State: "preparing"}
		if err = tx.Create(ctx, ns("installations"), lockID, "", existing); err != nil {
			return runtime.Outcome{}, err
		}
	} else if err != nil {
		return runtime.Outcome{}, err
	} else if existing.Disposing || !api.Equal(existing.Installation, in.Installation) {
		return runtime.Outcome{}, api.E("invalid_state", "install_lock_closed_or_changed")
	}
	id := prepareKey(in.Installation.InstallLockRef, in.TargetRef.ObjectID)
	prepared := PreparedInstall{ID: id, Revision: 1, Installation: in.Installation, TargetRef: in.TargetRef, State: "preparing", CommandID: c.CommandID}
	var prior PreparedInstall
	prev, priorErr := tx.Get(ctx, ns("preparations"), id, &prior)
	if priorErr == nil {
		if prior.State == "prepared" {
			return runtime.Applied(StateOutput{Ref: tx.Scope().Ref(prior.ID, prior.Revision), State: "prepared"}), nil
		}
		if prior.State == "preparing" {
			return runtime.Outcome{}, api.E("revision_conflict", "preparation_in_progress")
		}
		prepared.Revision = prev + 1
		if err = tx.Put(ctx, ns("preparations"), id, prev, prepared); err != nil {
			return runtime.Outcome{}, err
		}
	} else if errMissing(priorErr) {
		if err = tx.Create(ctx, ns("preparations"), id, c.CommandID, prepared); err != nil {
			return runtime.Outcome{}, err
		}
	} else {
		return runtime.Outcome{}, priorErr
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if _, err = tx.Raise(ctx, "governance.prepare", id, tx.Scope().Ref(id, 1), now); err != nil {
		return runtime.Outcome{}, err
	}
	return runtime.Accepted(StateOutput{Ref: tx.Scope().Ref(id, 1), State: "preparing"}), nil
}
func (s *Service) continuePrepare(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	var prepared PreparedInstall
	if _, err := store.Read(ctx, scope, ns("preparations"), work.Job.ResponsibilityKey, 0, &prepared); err != nil {
		return err
	}
	var evidence PreparationEvidence
	var ioErr error
	if s.Ports.Lifecycle == nil {
		ioErr = api.E("unsupported", "trusted_lifecycle_unavailable")
	} else {
		evidence, ioErr = s.Ports.Lifecycle.Prepare(ctx, prepared.Installation)
	}
	return finish(ctx, store, scope, s.participants(), work, runtime.Done(), func(tx runtime.Tx) error {
		if _, err := tx.LoadCommand(ctx, prepared.CommandID); err != nil {
			return err
		}
		var current PreparedInstall
		rev, err := tx.Get(ctx, ns("preparations"), prepared.ID, &current)
		if err != nil {
			return err
		}
		if current.State != "preparing" {
			return nil
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		current.Revision = rev + 1
		if ioErr != nil || !evidence.Compatible || !evidence.IsolationVerified || evidence.ArtifactDigest != current.Installation.InstallLockRef.Digest || evidence.ConfigDigest != current.Installation.ConfigRef.Digest {
			current.State = "blocked"
			current.BlockedReason = "artifact_isolation_or_compatibility_unverified"
			if ioErr != nil {
				current.BlockedReason = ioErr.Error()
			}
		} else {
			current.State = "prepared"
			current.Evidence = &evidence
		}
		if err = tx.Put(ctx, ns("preparations"), current.ID, rev, current); err != nil {
			return err
		}
		lockID := componentKey(current.Installation.InstallLockRef)
		var catalog PreparedInstall
		lrev, err := tx.Get(ctx, ns("installations"), lockID, &catalog)
		if err != nil {
			return err
		}
		if !catalog.Disposing {
			catalog.Revision = lrev + 1
			catalog.State = current.State
			catalog.BlockedReason = current.BlockedReason
			catalog.Evidence = current.Evidence
			if err = tx.Put(ctx, ns("installations"), lockID, lrev, catalog); err != nil {
				return err
			}
		}
		_ = now
		if current.State == "blocked" {
			return runtime.Decide(ctx, tx, current.CommandID, nil, api.E("unsupported", "artifact_isolation_or_compatibility_unverified"))
		}
		return runtime.Decide(ctx, tx, current.CommandID, StateOutput{Ref: scope.Ref(current.ID, current.Revision), State: current.State}, nil)
	})
}

func (s *Service) currentApproval(ctx context.Context, tx runtime.Tx, ref api.ObjectRef, lock api.ComponentRef, target string) (ReleaseApproval, error) {
	if err := ownerRef(tx.Scope(), ref); err != nil {
		return ReleaseApproval{}, err
	}
	var approval ReleaseApproval
	if _, err := tx.Get(ctx, ns("approvals"), ref.ObjectID, &approval); err != nil {
		return approval, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return approval, err
	}
	if approval.Revision < ref.Revision || approval.State != "active" || !api.Equal(approval.InstallLockRef, lock) || !contains(approval.Rollout.TargetIDs, target) || before(now, approval.ExpiresAt) != nil {
		return approval, api.E("forbidden", "approval_invalid")
	}
	if approval.Purpose == "improvement" {
		for _, er := range approval.EvidenceRefs {
			if err = ownerRef(tx.Scope(), er); err != nil {
				return approval, api.E("unsupported", "cross_owner_release_evidence_not_supported")
			}
			if err = s.reportEligibleTx(ctx, tx, er); err != nil {
				return approval, err
			}
		}
	}
	allowed := uint64(0)
	for i := uint64(0); i <= approval.BatchIndex && i < uint64(len(approval.Rollout.BatchSizes)); i++ {
		allowed += approval.Rollout.BatchSizes[i]
	}
	index := uint64(len(approval.Rollout.TargetIDs))
	for i, id := range approval.Rollout.TargetIDs {
		if id == target {
			index = uint64(i)
			break
		}
	}
	if index >= allowed {
		return approval, api.E("forbidden", "rollout_batch_not_open")
	}
	return approval, nil
}

func (s *Service) activate(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in ActivateRequest) (runtime.Outcome, error) {
	if err := requireRole(a, "maintainer"); err != nil {
		return runtime.Outcome{}, err
	}
	var head BindingHead
	rev, err := tx.Get(ctx, ns("heads"), in.TargetID, &head)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if c.TargetID != in.TargetID {
		return runtime.Outcome{}, api.E("invalid_request", "target_mismatch")
	}
	if err = requireCAS(c, rev); err != nil {
		return runtime.Outcome{}, err
	}
	if head.Generation != in.ExpectedGeneration {
		return runtime.Outcome{}, api.E("revision_conflict", "generation_changed")
	}
	var prepared PreparedInstall
	if _, err = tx.Get(ctx, ns("preparations"), prepareKey(in.InstallLockRef, in.TargetID), &prepared); err != nil {
		return runtime.Outcome{}, err
	}
	if prepared.State != "prepared" || prepared.Disposing || !api.Equal(prepared.Installation.ConfigRef, in.ConfigRef) || !contains(prepared.Installation.ReadFormats, head.DataFormat) || !contains(prepared.Installation.WriteFormats, head.DataFormat) {
		return runtime.Outcome{}, api.E("invalid_state", "format_incompatible")
	}
	var catalog PreparedInstall
	if _, err = tx.Get(ctx, ns("installations"), componentKey(in.InstallLockRef), &catalog); err != nil {
		return runtime.Outcome{}, err
	}
	if catalog.Disposing {
		return runtime.Outcome{}, api.E("invalid_state", "install_lock_closed")
	}
	if _, err = s.currentApproval(ctx, tx, in.ApprovalRef, in.InstallLockRef, in.TargetID); err != nil {
		return runtime.Outcome{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if err = before(now, in.PrepareDeadline); err != nil {
		return runtime.Outcome{}, err
	}
	id := digestID("activation", c.CommandID)
	activation := Activation{ActivationID: id, Revision: 1, TargetID: in.TargetID, InstallLockRef: in.InstallLockRef, ConfigRef: in.ConfigRef, ApprovalRef: in.ApprovalRef, Generation: head.Generation + 1, State: "preparing", InstanceID: digestID("instance", c.CommandID), ExpectedHeadRevision: rev, ExpectedGeneration: head.Generation, PrepareDeadline: in.PrepareDeadline, CommandID: c.CommandID, ResidualRefs: []api.ObjectRef{}, ReadinessRefs: []api.ObjectRef{}}
	pending := PendingActivation{ID: c.CommandID, Revision: 1, Activation: activation, SubjectID: a.SubjectID, State: "preparing"}
	if err = tx.Create(ctx, ns("activation_pending"), c.CommandID, in.TargetID, pending); err != nil {
		return runtime.Outcome{}, err
	}
	request := InstanceRequest{TargetID: in.TargetID, ActivationID: id, InstanceID: activation.InstanceID, Generation: activation.Generation, Installation: prepared.Installation, ConfigRef: in.ConfigRef, Deadline: in.PrepareDeadline}
	if err = tx.Create(ctx, ns("instance_requests"), activation.InstanceID, id, request); err != nil {
		return runtime.Outcome{}, err
	}
	if _, err = tx.Raise(ctx, "governance.activate", c.CommandID, tx.Scope().Ref(c.CommandID, 1), now); err != nil {
		return runtime.Outcome{}, err
	}
	return runtime.Accepted(StateOutput{Ref: tx.Scope().Ref(id, 1), State: "preparing"}), nil
}

func (s *Service) reopen(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in ReopenRequest) (runtime.Outcome, error) {
	if err := requireRole(a, "maintainer"); err != nil {
		return runtime.Outcome{}, err
	}
	for _, ref := range []api.ObjectRef{in.ActivationRef, in.ExpectedInstanceRef, in.OldInstanceFenceRef} {
		if err := ownerRef(tx.Scope(), ref); err != nil {
			return runtime.Outcome{}, err
		}
	}
	var head BindingHead
	rev, err := tx.Get(ctx, ns("heads"), in.TargetID, &head)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if err = requireCAS(c, rev); err != nil {
		return runtime.Outcome{}, err
	}
	if !head.Enabled || head.Generation != in.ExpectedGeneration || head.CurrentActivationRef == nil || head.CurrentActivationRef.ObjectID != in.ActivationRef.ObjectID || head.ReadinessRef == nil || head.ReadinessRef.ObjectID != in.ExpectedInstanceRef.ObjectID {
		return runtime.Outcome{}, api.E("revision_conflict", "activation_replaced")
	}
	var fence InstanceStop
	if _, err = tx.Get(ctx, ns("instance_stops"), in.OldInstanceFenceRef.ObjectID, &fence); err != nil {
		return runtime.Outcome{}, api.E("invalid_state", "old_instance_not_fenced")
	}
	if fence.Revision != in.OldInstanceFenceRef.Revision || fence.State != "stopped" || fence.InstanceID != in.ExpectedInstanceRef.ObjectID || fence.Generation != head.Generation || fence.Evidence == nil || !fence.Evidence.Exited || fence.Evidence.MayApplyLater {
		return runtime.Outcome{}, api.E("invalid_state", "old_instance_not_fenced")
	}
	var original Activation
	if _, err = tx.Get(ctx, ns("activations"), in.ActivationRef.ObjectID, &original); err != nil {
		return runtime.Outcome{}, err
	}
	if _, err = s.currentApproval(ctx, tx, original.ApprovalRef, original.InstallLockRef, in.TargetID); err != nil {
		return runtime.Outcome{}, err
	}
	candidate := original
	candidate.InstanceID = digestID("instance", c.CommandID)
	candidate.CommandID = c.CommandID
	candidate.ExpectedHeadRevision = rev
	candidate.ExpectedGeneration = head.Generation
	candidate.PrepareDeadline = in.PrepareDeadline
	candidate.Reopen = true
	candidate.ExpectedInstanceRef = &in.ExpectedInstanceRef
	candidate.FenceRef = &in.OldInstanceFenceRef
	pending := PendingActivation{ID: c.CommandID, Revision: 1, Activation: candidate, SubjectID: a.SubjectID, State: "preparing"}
	if err = tx.Create(ctx, ns("activation_pending"), c.CommandID, in.TargetID, pending); err != nil {
		return runtime.Outcome{}, err
	}
	var prepared PreparedInstall
	if _, err = tx.Get(ctx, ns("preparations"), prepareKey(candidate.InstallLockRef, candidate.TargetID), &prepared); err != nil {
		return runtime.Outcome{}, err
	}
	request := InstanceRequest{TargetID: candidate.TargetID, ActivationID: candidate.ActivationID, InstanceID: candidate.InstanceID, Generation: candidate.Generation, Installation: prepared.Installation, ConfigRef: candidate.ConfigRef, Deadline: candidate.PrepareDeadline}
	if err = tx.Create(ctx, ns("instance_requests"), candidate.InstanceID, candidate.ActivationID, request); err != nil {
		return runtime.Outcome{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if _, err = tx.Raise(ctx, "governance.activate", c.CommandID, tx.Scope().Ref(c.CommandID, 1), now); err != nil {
		return runtime.Outcome{}, err
	}
	return runtime.Accepted(StateOutput{Ref: tx.Scope().Ref(original.ActivationID, original.Revision), State: "reopening"}), nil
}

func (s *Service) continueActivate(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	var pending PendingActivation
	if _, err := store.Read(ctx, scope, ns("activation_pending"), work.Job.ResponsibilityKey, 0, &pending); err != nil {
		return err
	}
	if pending.State != "preparing" {
		return finish(ctx, store, scope, s.participants(), work, runtime.Done(), nil)
	}
	activation := pending.Activation
	var prepared PreparedInstall
	if _, err := store.Read(ctx, scope, ns("preparations"), prepareKey(activation.InstallLockRef, activation.TargetID), 0, &prepared); err != nil {
		return err
	}
	request := InstanceRequest{TargetID: activation.TargetID, ActivationID: activation.ActivationID, InstanceID: activation.InstanceID, Generation: activation.Generation, Installation: prepared.Installation, ConfigRef: activation.ConfigRef, Deadline: activation.PrepareDeadline}
	var evidence InstanceEvidence
	var ioErr error
	if s.Ports.Lifecycle == nil {
		ioErr = api.E("unsupported", "trusted_lifecycle_unavailable")
	} else {
		evidence, ioErr = s.Ports.Lifecycle.Initialize(ctx, request)
	}
	err := finish(ctx, store, scope, s.participants(), work, runtime.Done(), func(tx runtime.Tx) error {
		if _, err := tx.LoadCommand(ctx, activation.CommandID); err != nil {
			return err
		}
		var current PendingActivation
		prev, err := tx.Get(ctx, ns("activation_pending"), pending.ID, &current)
		if err != nil {
			return err
		}
		if current.State != "preparing" {
			return nil
		}
		var head BindingHead
		hrev, err := tx.Get(ctx, ns("heads"), activation.TargetID, &head)
		if err != nil {
			return err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		reason := ""
		if ioErr != nil || !evidence.Ready || evidence.InstanceID != activation.InstanceID || evidence.Generation != activation.Generation || evidence.ArtifactDigest != activation.InstallLockRef.Digest || evidence.ConfigDigest != activation.ConfigRef.Digest || before(now, evidence.ExpiresAt) != nil {
			reason = "instance_not_ready"
		}
		if before(now, activation.PrepareDeadline) != nil {
			reason = "prepare_deadline_expired"
		}
		if hrev != activation.ExpectedHeadRevision || head.Generation != activation.ExpectedGeneration || activation.Reopen && (!head.Enabled || head.ReadinessRef == nil || activation.ExpectedInstanceRef == nil || head.ReadinessRef.ObjectID != activation.ExpectedInstanceRef.ObjectID) {
			reason = "generation_changed"
		}
		if _, e := s.currentApproval(ctx, tx, activation.ApprovalRef, activation.InstallLockRef, activation.TargetID); e != nil {
			reason = "approval_invalid"
		}
		var catalog PreparedInstall
		if _, e := tx.Get(ctx, ns("installations"), componentKey(activation.InstallLockRef), &catalog); e != nil || catalog.Disposing {
			reason = "install_lock_closed"
		}
		if activation.Reopen {
			var fence InstanceStop
			if activation.FenceRef == nil {
				reason = "old_instance_not_fenced"
			} else if _, e := tx.Get(ctx, ns("instance_stops"), activation.FenceRef.ObjectID, &fence); e != nil || fence.State != "stopped" {
				reason = "old_instance_not_fenced"
			}
		}
		current.Revision = prev + 1
		current.CandidateEvidence = &evidence
		if reason != "" {
			current.State = "rejected"
			current.BlockedReason = reason
			if err = tx.Put(ctx, ns("activation_pending"), current.ID, prev, current); err != nil {
				return err
			}
			stop := InstanceStop{ID: activation.InstanceID, Revision: 1, ActivationID: activation.ActivationID, InstanceID: activation.InstanceID, Generation: activation.Generation, State: "pending"}
			if err = tx.Create(ctx, ns("instance_stops"), stop.ID, activation.ActivationID, stop); err != nil {
				return err
			}
			if err = runtime.Decide(ctx, tx, activation.CommandID, nil, api.E("revision_conflict", reason)); err != nil {
				return err
			}
			_, err = tx.Raise(ctx, "governance.stop", stop.ID, scope.Ref(stop.ID, 1), now)
			return err
		}
		ready := InstanceReadiness{InstanceID: evidence.InstanceID, Revision: 1, TargetID: activation.TargetID, ActivationID: activation.ActivationID, Generation: activation.Generation, ConfigDigest: evidence.ConfigDigest, ArtifactDigest: evidence.ArtifactDigest, SelfTestRef: evidence.SelfTestRef, ApprovalRef: activation.ApprovalRef, IssuedAt: api.Time(now), ExpiresAt: minTime(evidence.ExpiresAt, activation.PrepareDeadline), State: "ready"}
		if err = tx.Create(ctx, ns("readiness"), ready.InstanceID, activation.ActivationID, ready); err != nil {
			return err
		}
		holder := InstallHolder{HolderID: digestID("install_holder", ready.InstanceID), Revision: 1, InstallLockRef: activation.InstallLockRef, ConsumerRef: scope.Ref(ready.InstanceID, 1), State: "registered"}
		if err = s.RegisterInstallHolderTx(ctx, tx, holder); err != nil {
			return err
		}
		activation.State = "active"
		activation.ReadinessRefs = append(activation.ReadinessRefs, scope.Ref(ready.InstanceID, 1))
		if activation.Reopen {
			var old Activation
			arev, e := tx.Get(ctx, ns("activations"), activation.ActivationID, &old)
			if e != nil {
				return e
			}
			activation.Revision = arev + 1
			if err = tx.Put(ctx, ns("activations"), activation.ActivationID, arev, activation); err != nil {
				return err
			}
		} else {
			if err = tx.Create(ctx, ns("activations"), activation.ActivationID, activation.TargetID, activation); err != nil {
				return err
			}
		}
		bindingID := digestID("binding", activation.CommandID)
		binding := Binding{BindingID: bindingID, Revision: 1, TargetRef: scope.Ref(head.TargetID, hrev+1), InstallLockRef: activation.InstallLockRef, ConfigRef: activation.ConfigRef, ReadinessRef: scope.Ref(ready.InstanceID, 1)}
		if err = tx.Create(ctx, ns("bindings"), bindingID, head.TargetID, binding); err != nil {
			return err
		}
		var stopAfterCommit *InstanceStop
		if head.ReadinessRef != nil && !activation.Reopen {
			oldStop := InstanceStop{ID: head.ReadinessRef.ObjectID, Revision: 1, InstanceID: head.ReadinessRef.ObjectID, Generation: head.Generation, State: "pending"}
			if head.CurrentActivationRef != nil {
				oldStop.ActivationID = head.CurrentActivationRef.ObjectID
			}
			var existingStop InstanceStop
			if _, e := tx.Get(ctx, ns("instance_stops"), oldStop.ID, &existingStop); errMissing(e) {
				if err = tx.Create(ctx, ns("instance_stops"), oldStop.ID, oldStop.ActivationID, oldStop); err != nil {
					return err
				}
				stopAfterCommit = &oldStop
			} else if e != nil {
				return e
			}
		}
		head.Revision = hrev + 1
		head.Generation = activation.Generation
		head.Enabled = true
		aref := scope.Ref(activation.ActivationID, activation.Revision)
		bref := scope.Ref(bindingID, 1)
		rref := scope.Ref(ready.InstanceID, 1)
		head.CurrentActivationRef = &aref
		head.BindingRef = &bref
		head.ReadinessRef = &rref
		if err = tx.Put(ctx, ns("heads"), head.TargetID, hrev, head); err != nil {
			return err
		}
		current.State = "active"
		if err = tx.Put(ctx, ns("activation_pending"), current.ID, prev, current); err != nil {
			return err
		}
		if err = runtime.Decide(ctx, tx, activation.CommandID, StateOutput{Ref: scope.Ref(activation.ActivationID, activation.Revision), State: "active"}, nil); err != nil {
			return err
		}
		if stopAfterCommit != nil {
			_, err = tx.Raise(ctx, "governance.stop", stopAfterCommit.ID, scope.Ref(stopAfterCommit.ID, 1), now)
		}
		return err
	})
	return err
}
func (s *Service) deactivate(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in DeactivateRequest) (runtime.Outcome, error) {
	if err := requireRole(a, "maintainer"); err != nil {
		return runtime.Outcome{}, err
	}
	if err := ownerRef(tx.Scope(), in.ActivationRef); err != nil {
		return runtime.Outcome{}, err
	}
	var head BindingHead
	rev, err := tx.Get(ctx, ns("heads"), in.TargetID, &head)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if err = requireCAS(c, rev); err != nil {
		return runtime.Outcome{}, err
	}
	if head.Generation != in.ExpectedGeneration || head.CurrentActivationRef == nil || head.CurrentActivationRef.ObjectID != in.ActivationRef.ObjectID {
		return runtime.Outcome{}, api.E("revision_conflict", "activation_replaced")
	}
	if !head.Enabled {
		return runtime.Applied(StateOutput{Ref: tx.Scope().Ref(head.TargetID, head.Revision), State: "disabled"}), nil
	}
	head.Enabled = false
	head.Revision = rev + 1
	if err = tx.Put(ctx, ns("heads"), head.TargetID, rev, head); err != nil {
		return runtime.Outcome{}, err
	}
	var activation Activation
	arev, err := tx.Get(ctx, ns("activations"), in.ActivationRef.ObjectID, &activation)
	if err != nil {
		return runtime.Outcome{}, err
	}
	activation.State = "deactivated"
	activation.Revision = arev + 1
	if err = tx.Put(ctx, ns("activations"), activation.ActivationID, arev, activation); err != nil {
		return runtime.Outcome{}, err
	}
	if head.ReadinessRef != nil {
		if err = s.requestStopTx(ctx, tx, activation, head.ReadinessRef.ObjectID); err != nil {
			return runtime.Outcome{}, err
		}
	}
	return runtime.Applied(StateOutput{Ref: tx.Scope().Ref(head.TargetID, head.Revision), State: "disabled"}), nil
}
func (s *Service) requestStopTx(ctx context.Context, tx runtime.Tx, a Activation, instanceID string) error {
	var stop InstanceStop
	rev, err := tx.Get(ctx, ns("instance_stops"), instanceID, &stop)
	if errMissing(err) {
		stop = InstanceStop{ID: instanceID, Revision: 1, ActivationID: a.ActivationID, InstanceID: instanceID, Generation: a.Generation, State: "pending"}
		if err = tx.Create(ctx, ns("instance_stops"), instanceID, a.ActivationID, stop); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if stop.State == "stopped" {
		return nil
	} else {
		stop.Revision = rev + 1
		if err = tx.Put(ctx, ns("instance_stops"), instanceID, rev, stop); err != nil {
			return err
		}
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return err
	}
	_, err = tx.Raise(ctx, "governance.stop", instanceID, tx.Scope().Ref(instanceID, stop.Revision), now)
	return err
}
func (s *Service) continueStop(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	var stop InstanceStop
	if _, err := store.Read(ctx, scope, ns("instance_stops"), work.Job.ResponsibilityKey, 0, &stop); err != nil {
		return err
	}
	var request InstanceRequest
	if _, err := store.Read(ctx, scope, ns("instance_requests"), stop.InstanceID, 0, &request); err != nil {
		return err
	}
	if s.Ports.Lifecycle == nil {
		return api.E("dependency_unavailable", "trusted_lifecycle_unavailable")
	}
	evidence, err := s.Ports.Lifecycle.Fence(ctx, request)
	if err != nil {
		return err
	}
	return finish(ctx, store, scope, s.participants(), work, runtime.Done(), func(tx runtime.Tx) error {
		var current InstanceStop
		rev, err := tx.Get(ctx, ns("instance_stops"), stop.ID, &current)
		if err != nil {
			return err
		}
		if evidence.InstanceID != current.InstanceID || evidence.Generation != current.Generation {
			return api.E("forbidden", "instance_fence_mismatch")
		}
		current.Revision = rev + 1
		current.Evidence = &evidence
		if evidence.Exited && !evidence.MayApplyLater {
			current.State = "stopped"
		} else {
			current.State = "unknown"
		}
		if err = tx.Put(ctx, ns("instance_stops"), stop.ID, rev, current); err != nil {
			return err
		}
		if current.State == "stopped" {
			holderID := digestID("install_holder", current.InstanceID)
			var holder InstallHolder
			if hrev, e := tx.Get(ctx, ns("install_holders"), holderID, &holder); e == nil {
				holder.Revision = hrev + 1
				holder.State = "released"
				if err = tx.Put(ctx, ns("install_holders"), holderID, hrev, holder); err != nil {
					return err
				}
			} else if !errMissing(e) {
				return e
			}
		}
		var ready InstanceReadiness
		if rrev, e := tx.Get(ctx, ns("readiness"), current.InstanceID, &ready); e == nil {
			ready.Revision = rrev + 1
			ready.State = "not_ready"
			if err = tx.Put(ctx, ns("readiness"), ready.InstanceID, rrev, ready); err != nil {
				return err
			}
		} else if !errMissing(e) {
			return e
		}
		if current.State == "unknown" {
			now, err := tx.Now(ctx)
			if err != nil {
				return err
			}
			return tx.Hint(ctx, work.Job.JobID, now.Add(time.Second))
		}
		return nil
	})
}

func (s *Service) createApproval(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in ApprovalCreate) (runtime.Outcome, error) {
	if err := requireRole(a, "release_approver"); err != nil {
		return runtime.Outcome{}, err
	}
	if in.Purpose != "compatibility" && in.Purpose != "improvement" {
		return runtime.Outcome{}, api.E("invalid_request", "invalid_release_purpose")
	}
	if in.ApprovalID != c.TargetID || len(in.Rollout.TargetIDs) == 0 || len(in.Rollout.TargetIDs) > 100 || len(in.Rollout.BatchSizes) == 0 || in.Rollout.MaximumStartWindowSeconds == 0 || in.Rollout.MaximumStartWindowSeconds > 3600 || in.Rollout.MinimumSamples == 0 || in.Rollout.ObservationSeconds == 0 || in.Rollout.ObservationSeconds > 31536000 {
		return runtime.Outcome{}, api.E("invalid_request", "bounded_rollout_required")
	}
	if cmp, err := api.CompareDecimal(in.Rollout.MaxErrorRate, "1"); err != nil || cmp > 0 {
		return runtime.Outcome{}, api.E("invalid_request", "invalid_error_rate")
	}
	seen := map[string]bool{}
	targets := append([]string{}, in.Rollout.TargetIDs...)
	sort.Strings(targets)
	for _, target := range targets {
		if seen[target] || !api.ValidID(target) {
			return runtime.Outcome{}, api.E("invalid_request", "duplicate_target")
		}
		seen[target] = true
		var head BindingHead
		if _, err := tx.Get(ctx, ns("heads"), target, &head); err != nil {
			return runtime.Outcome{}, err
		}
	}
	total := uint64(0)
	for _, batch := range in.Rollout.BatchSizes {
		if batch == 0 || batch > 100 || total > 100-batch {
			return runtime.Outcome{}, api.E("invalid_request", "invalid_batch")
		}
		total += batch
	}
	if total != uint64(len(in.Rollout.TargetIDs)) {
		return runtime.Outcome{}, api.E("invalid_request", "rollout_target_count_mismatch")
	}
	if in.Purpose == "compatibility" && len(in.CompatibilityEvidenceRefs) == 0 || in.Purpose == "improvement" && len(in.EvidenceRefs) == 0 {
		return runtime.Outcome{}, api.E("invalid_request", "release_evidence_required")
	}
	if in.Purpose == "improvement" {
		for _, e := range in.EvidenceRefs {
			if err := ownerRef(tx.Scope(), e); err != nil {
				return runtime.Outcome{}, api.E("unsupported", "cross_owner_release_evidence_not_supported")
			}
			if err := s.reportEligibleTx(ctx, tx, e); err != nil {
				return runtime.Outcome{}, err
			}
			var report EvaluationReport
			if _, err := tx.Get(ctx, ns("reports"), e.ObjectID, &report); err != nil {
				return runtime.Outcome{}, err
			}
			var plan EvaluationPlan
			if _, err := tx.Get(ctx, ns("plans"), report.PlanID, &plan); err != nil {
				return runtime.Outcome{}, err
			}
			if !api.Equal(plan.CandidateRef, in.InstallLockRef) {
				return runtime.Outcome{}, api.E("forbidden", "release_candidate_evidence_mismatch")
			}
		}
	}
	if (in.RollbackInstallLockRef == nil) != (in.RollbackApprovalRef == nil) {
		return runtime.Outcome{}, api.E("invalid_request", "independent_rollback_approval_required")
	}
	if in.RollbackApprovalRef != nil {
		var rollback ReleaseApproval
		if err := ownerRef(tx.Scope(), *in.RollbackApprovalRef); err != nil {
			return runtime.Outcome{}, err
		}
		if _, err := tx.Get(ctx, ns("approvals"), in.RollbackApprovalRef.ObjectID, &rollback); err != nil {
			return runtime.Outcome{}, err
		}
		if rollback.ApprovalID == in.ApprovalID || !api.Equal(rollback.InstallLockRef, in.RollbackInstallLockRef) {
			return runtime.Outcome{}, api.E("invalid_request", "independent_rollback_approval_required")
		}
	}
	confirm, pending, err := s.beginConfirmed(ctx, tx, a, c, in.PreviewRefs, in.ExpiresAt)
	if err != nil || confirm == nil {
		return pending, err
	}
	if err = s.consumeConfirmation(ctx, tx, a, confirm, c.CommandID); err != nil {
		return runtime.Outcome{}, err
	}
	approval := ReleaseApproval{ApprovalID: in.ApprovalID, Revision: 1, ApproverRef: a.Ref(tx.Scope().OwnerID), InstallLockRef: in.InstallLockRef, Purpose: in.Purpose, EvidenceRefs: in.EvidenceRefs, CompatibilityEvidenceRefs: in.CompatibilityEvidenceRefs, Rollout: in.Rollout, ExpiresAt: in.ExpiresAt, State: "active", RollbackInstallLockRef: in.RollbackInstallLockRef, RollbackApprovalRef: in.RollbackApprovalRef, ConfirmationRef: tx.Scope().Ref(confirm.RequestID, confirm.Revision+1)}
	if err = tx.Create(ctx, ns("approvals"), approval.ApprovalID, "", approval); err != nil {
		return runtime.Outcome{}, err
	}
	ref := tx.Scope().Ref(approval.ApprovalID, 1)
	return runtime.Applied(ConfirmedOutput{Ref: &ref, State: "active"}), nil
}
func (s *Service) revokeApproval(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in RefInput) (runtime.Outcome, error) {
	if err := requireRole(a, "release_approver"); err != nil {
		return runtime.Outcome{}, err
	}
	if err := ownerRef(tx.Scope(), in.Ref); err != nil {
		return runtime.Outcome{}, err
	}
	var approval ReleaseApproval
	rev, err := tx.Get(ctx, ns("approvals"), in.Ref.ObjectID, &approval)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if err = requireCAS(c, rev); err != nil {
		return runtime.Outcome{}, err
	}
	if approval.State == "revoked" {
		return runtime.Applied(StateOutput{Ref: tx.Scope().Ref(approval.ApprovalID, approval.Revision), State: approval.State}), nil
	}
	approval.Revision = rev + 1
	approval.State = "revoked"
	if err = tx.Put(ctx, ns("approvals"), approval.ApprovalID, rev, approval); err != nil {
		return runtime.Outcome{}, err
	}
	id := digestID("revoke", approval.ApprovalID)
	impact := ApprovalStopImpact{ID: id, Revision: 1, ApprovalID: approval.ApprovalID}
	if err = tx.Create(ctx, ns("approval_stop_impacts"), id, approval.ApprovalID, impact); err != nil {
		return runtime.Outcome{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if _, err = tx.Raise(ctx, "governance.approval_stop", id, tx.Scope().Ref(approval.ApprovalID, approval.Revision), now); err != nil {
		return runtime.Outcome{}, err
	}
	return runtime.Applied(StateOutput{Ref: tx.Scope().Ref(approval.ApprovalID, approval.Revision), State: "revoked"}), nil
}
func (s *Service) ApprovalUseTx(ctx context.Context, tx runtime.Tx, in ApprovalUseRequest) (ApprovalUse, error) {
	digest, e := api.Digest(in)
	if e != nil {
		return ApprovalUse{}, e
	}
	var old ApprovalUse
	_, err := tx.Get(ctx, ns("approval_uses"), in.UseID, &old)
	if err == nil {
		if old.RequestDigest != digest {
			return old, api.E("idempotency_conflict", "approval_use_changed")
		}
		return old, nil
	}
	if !errMissing(err) {
		return old, err
	}
	approval, err := s.currentApproval(ctx, tx, in.ApprovalRef, in.InstallLockRef, in.TargetRef.ObjectID)
	if err != nil {
		return old, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return old, err
	}
	expires := minTime(in.StartBefore, approval.ExpiresAt)
	expires = minTime(expires, api.Time(now.Add(time.Duration(approval.Rollout.MaximumStartWindowSeconds)*time.Second)))
	if err = before(now, expires); err != nil {
		return old, err
	}
	out := ApprovalUse{UseID: in.UseID, ApprovalRef: in.ApprovalRef, TargetRef: in.TargetRef, InstallLockRef: in.InstallLockRef, IssuedAt: api.Time(now), StartBefore: expires, RequestDigest: digest}
	if s.Ports.Proof != nil {
		digest, e := ApprovalUseDigest(out)
		if e != nil {
			return old, e
		}
		out.Proof, err = s.Ports.Proof.SignLocal(ProofStatement{TenantID: tx.Scope().TenantID, IssuerID: tx.Scope().OwnerID, AudienceID: in.TargetRef.OwnerID, Purpose: "approval_use", ObjectRef: tx.Scope().Ref(in.UseID, 1), Digest: digest, IssuedAt: out.IssuedAt, StartBefore: expires})
		if err != nil {
			return old, err
		}
	}
	err = tx.Create(ctx, ns("approval_uses"), in.UseID, in.ApprovalRef.ObjectID, out)
	return out, err
}
func ApprovalUseDigest(out ApprovalUse) (string, error) {
	out.Proof = ""
	return api.Digest(out)
}
func (s *Service) advanceRollout(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in RolloutObservation) (runtime.Outcome, error) {
	if err := requireRole(a, "rollout_observer"); err != nil {
		return runtime.Outcome{}, err
	}
	if err := ownerRef(tx.Scope(), in.ApprovalRef); err != nil {
		return runtime.Outcome{}, err
	}
	if c.ExpectedRevision == nil {
		return runtime.Outcome{}, api.E("revision_conflict", "revision_required")
	}
	if in.EvidenceRef.TenantID != tx.Scope().TenantID {
		return runtime.Outcome{}, api.E("forbidden", "observation_scope_mismatch")
	}
	var snapshot ReleaseApproval
	if err := tx.GetVersion(ctx, ns("approvals"), in.ApprovalRef.ObjectID, *c.ExpectedRevision, &snapshot); err != nil {
		return runtime.Outcome{}, err
	}
	if snapshot.BatchIndex+1 >= uint64(len(snapshot.Rollout.BatchSizes)) {
		return runtime.Outcome{}, api.E("invalid_state", "rollout_not_expandable")
	}
	open := uint64(0)
	for i := uint64(0); i <= snapshot.BatchIndex; i++ {
		open += snapshot.Rollout.BatchSizes[i]
	}
	if open > uint64(len(snapshot.Rollout.TargetIDs)) || !contains(snapshot.Rollout.TargetIDs[:open], in.TargetID) {
		return runtime.Outcome{}, api.E("forbidden", "rollout_batch_not_open")
	}
	targets := append([]string{}, snapshot.Rollout.TargetIDs[:open]...)
	sort.Strings(targets)
	heads := map[string]BindingHead{}
	for _, target := range targets {
		var head BindingHead
		if _, err := tx.Get(ctx, ns("heads"), target, &head); err != nil {
			return runtime.Outcome{}, err
		}
		heads[target] = head
	}
	var approval ReleaseApproval
	rev, err := tx.Get(ctx, ns("approvals"), in.ApprovalRef.ObjectID, &approval)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if err = requireCAS(c, rev); err != nil {
		return runtime.Outcome{}, err
	}
	if approval.State != "active" || approval.BatchIndex != snapshot.BatchIndex {
		return runtime.Outcome{}, api.E("invalid_state", "rollout_not_expandable")
	}
	head := heads[in.TargetID]
	if !head.Enabled || head.ReadinessRef == nil {
		return runtime.Outcome{}, api.E("invalid_state", "instance_not_ready")
	}
	var ready InstanceReadiness
	if _, err = tx.Get(ctx, ns("readiness"), head.ReadinessRef.ObjectID, &ready); err != nil {
		return runtime.Outcome{}, err
	}
	if ready.State != "ready" || ready.Generation != head.Generation || ready.ApprovalRef.ObjectID != approval.ApprovalID || ready.ArtifactDigest != approval.InstallLockRef.Digest {
		return runtime.Outcome{}, api.E("invalid_state", "instance_not_ready")
	}
	if _, err = s.currentApproval(ctx, tx, in.ApprovalRef, approval.InstallLockRef, in.TargetID); err != nil {
		return runtime.Outcome{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return runtime.Outcome{}, err
	}
	observed, err := api.ParseTime(in.ObservationStartedAt)
	if err != nil {
		return runtime.Outcome{}, err
	}
	issued, err := api.ParseTime(ready.IssuedAt)
	if err != nil || observed.Before(issued) || before(now, ready.ExpiresAt) != nil {
		return runtime.Outcome{}, api.E("invalid_state", "observation_not_current")
	}
	cmp, err := api.CompareDecimal(in.ErrorRate, approval.Rollout.MaxErrorRate)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if in.Samples < approval.Rollout.MinimumSamples || now.Before(observed.Add(time.Duration(approval.Rollout.ObservationSeconds)*time.Second)) || cmp > 0 || in.UnknownEffects > 0 {
		return runtime.Outcome{}, api.E("invalid_state", "rollout_observation_insufficient")
	}
	key := digestID("rollout", []any{approval.ApprovalID, approval.BatchIndex, in.TargetID})
	fact := rolloutFact{Revision: 1, Observation: in, InstanceRef: *head.ReadinessRef, Generation: head.Generation, BatchIndex: approval.BatchIndex}
	var old rolloutFact
	if fRev, e := tx.Get(ctx, ns("rollout_observations"), key, &old); errMissing(e) {
		err = tx.Create(ctx, ns("rollout_observations"), key, approval.ApprovalID, fact)
	} else if e != nil {
		return runtime.Outcome{}, e
	} else {
		fact.Revision = fRev + 1
		err = tx.Put(ctx, ns("rollout_observations"), key, fRev, fact)
	}
	if err != nil {
		return runtime.Outcome{}, err
	}
	for _, target := range targets {
		head := heads[target]
		var observed rolloutFact
		_, e := tx.Get(ctx, ns("rollout_observations"), digestID("rollout", []any{approval.ApprovalID, approval.BatchIndex, target}), &observed)
		if errMissing(e) || !head.Enabled || head.ReadinessRef == nil || !api.Equal(head.ReadinessRef, observed.InstanceRef) || head.Generation != observed.Generation {
			return runtime.Applied(StateOutput{Ref: tx.Scope().Ref(approval.ApprovalID, rev), State: "observed"}), nil
		}
		if e != nil {
			return runtime.Outcome{}, e
		}
		var current InstanceReadiness
		if _, e = tx.Get(ctx, ns("readiness"), head.ReadinessRef.ObjectID, &current); e != nil {
			return runtime.Outcome{}, e
		}
		if current.State != "ready" || before(now, current.ExpiresAt) != nil {
			return runtime.Outcome{}, api.E("invalid_state", "instance_not_ready")
		}
	}
	approval.Revision = rev + 1
	approval.BatchIndex++
	if err = tx.Put(ctx, ns("approvals"), approval.ApprovalID, rev, approval); err != nil {
		return runtime.Outcome{}, err
	}
	return runtime.Applied(StateOutput{Ref: tx.Scope().Ref(approval.ApprovalID, approval.Revision), State: "batch_open"}), nil
}

func (s *Service) RegisterInstallHolderTx(ctx context.Context, tx runtime.Tx, holder InstallHolder) error {
	var catalog PreparedInstall
	if _, err := tx.Get(ctx, ns("installations"), componentKey(holder.InstallLockRef), &catalog); err != nil {
		return err
	}
	if catalog.Disposing {
		return api.E("invalid_state", "install_lock_closed")
	}
	if holder.Revision != 1 || holder.State != "registered" || holder.ConsumerRef.TenantID != tx.Scope().TenantID {
		return api.E("invalid_request", "install_holder_invalid")
	}
	return tx.Create(ctx, ns("install_holders"), holder.HolderID, componentKey(holder.InstallLockRef), holder)
}
func (s *Service) ReleaseInstallHolderTx(ctx context.Context, tx runtime.Tx, ref api.ObjectRef, closed bool) error {
	if !closed {
		return api.E("effect_unknown", "holder_work_not_closed")
	}
	if err := ownerRef(tx.Scope(), ref); err != nil {
		return err
	}
	var holder InstallHolder
	rev, err := tx.Get(ctx, ns("install_holders"), ref.ObjectID, &holder)
	if err != nil {
		return err
	}
	holder.Revision = rev + 1
	holder.State = "released"
	return tx.Put(ctx, ns("install_holders"), holder.HolderID, rev, holder)
}
func (s *Service) dispose(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in DisposeRequest) (runtime.Outcome, error) {
	if err := requireRole(a, "maintainer"); err != nil {
		return runtime.Outcome{}, err
	}
	id := componentKey(in.InstallLockRef)
	var catalog PreparedInstall
	rev, err := tx.Get(ctx, ns("installations"), id, &catalog)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if err = requireCAS(c, rev); err != nil {
		return runtime.Outcome{}, err
	}
	if in.ExpectedRevision != rev {
		return runtime.Outcome{}, api.E("revision_conflict", "revision_changed")
	}
	if catalog.Disposing {
		return runtime.Outcome{}, api.E("invalid_state", "install_lock_disposal_already_started")
	}
	catalog.Revision = rev + 1
	catalog.Disposing = true
	catalog.State = "disposing"
	catalog.CommandID = c.CommandID
	if err = tx.Put(ctx, ns("installations"), id, rev, catalog); err != nil {
		return runtime.Outcome{}, err
	}
	if err = tx.Create(ctx, ns("disposal_progress"), c.CommandID, id, disposalProgress{Revision: 1, CommandID: c.CommandID, InstallationID: id}); err != nil {
		return runtime.Outcome{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if _, err = tx.Raise(ctx, "governance.dispose", id, tx.Scope().Ref(id, catalog.Revision), now); err != nil {
		return runtime.Outcome{}, err
	}
	return runtime.Accepted(StateOutput{Ref: tx.Scope().Ref(id, catalog.Revision), State: "disposing"}), nil
}

// 原安装锁关闭后，新增 holder 被拒绝，released 不可逆；历史证明可逐页持久累计。
type disposalProgress struct {
	Revision       uint64 `json:"revision"`
	CommandID      string `json:"command_id"`
	InstallationID string `json:"installation_id"`
	After          string `json:"after"`
	Complete       bool   `json:"complete"`
}

func (s *Service) continueDispose(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	var catalog PreparedInstall
	if _, err := store.Read(ctx, scope, ns("installations"), work.Job.ResponsibilityKey, 0, &catalog); err != nil {
		return err
	}
	if catalog.State == "disposed" {
		return finish(ctx, store, scope, s.participants(), work, runtime.Done(), nil)
	}
	if !catalog.Disposing || catalog.CommandID == "" {
		return api.E("invalid_state", "install_lock_not_disposing")
	}
	progress := disposalProgress{CommandID: catalog.CommandID, InstallationID: catalog.ID}
	_, err := store.Read(ctx, scope, ns("disposal_progress"), catalog.CommandID, 0, &progress)
	if err != nil && !errMissing(err) {
		return err
	}
	if progress.CommandID != catalog.CommandID || progress.InstallationID != catalog.ID {
		return api.E("idempotency_conflict", "disposal_progress_changed")
	}
	if !progress.Complete {
		rows, err := store.List(ctx, scope, ns("install_holders"), catalog.ID, progress.After, 100)
		if err != nil {
			return err
		}
		for _, row := range rows {
			var holder InstallHolder
			if err = row.Decode(&holder); err != nil {
				return err
			}
			if holder.State != "released" {
				return api.E("dependency_unavailable", "install_holder_unresolved")
			}
		}
		return finish(ctx, store, scope, s.participants(), work, runtime.Ready(time.Now()), func(tx runtime.Tx) error {
			if _, err := tx.LoadCommand(ctx, catalog.CommandID); err != nil {
				return err
			}
			var current PreparedInstall
			if _, err := tx.Get(ctx, ns("installations"), catalog.ID, &current); err != nil {
				return err
			}
			if !current.Disposing || current.CommandID != catalog.CommandID || !api.Equal(current.Installation, catalog.Installation) {
				return api.E("revision_conflict", "disposal_installation_changed")
			}
			var saved disposalProgress
			rev, err := tx.Get(ctx, ns("disposal_progress"), catalog.CommandID, &saved)
			if err != nil && !errMissing(err) {
				return err
			}
			if err == nil && !api.Equal(saved, progress) {
				return api.E("revision_conflict", "disposal_progress_changed")
			}
			// 老格式 accepted 责任从第一页开始，绝不因升级跳过历史。
			if errMissing(err) {
				progress.Revision = 1
				if err = tx.Create(ctx, ns("disposal_progress"), catalog.CommandID, catalog.ID, progress); err != nil {
					return err
				}
				rev = 1
			}
			progress.Revision = rev + 1
			if len(rows) > 0 {
				progress.After = rows[len(rows)-1].ID
			}
			progress.Complete = len(rows) < 100
			return tx.Put(ctx, ns("disposal_progress"), catalog.CommandID, rev, progress)
		})
	}
	if s.Ports.Lifecycle == nil {
		return api.E("dependency_unavailable", "trusted_lifecycle_unavailable")
	}
	if err = store.CheckClaim(ctx, scope, work.Claim); err != nil {
		return err
	}
	evidence, err := s.Ports.Lifecycle.Dispose(ctx, catalog.Installation)
	if err != nil {
		return err
	}
	return finish(ctx, store, scope, s.participants(), work, runtime.Done(), func(tx runtime.Tx) error {
		if _, err := tx.LoadCommand(ctx, catalog.CommandID); err != nil {
			return err
		}
		var current PreparedInstall
		rev, err := tx.Get(ctx, ns("installations"), catalog.ID, &current)
		if err != nil {
			return err
		}
		if !current.Disposing || current.CommandID != catalog.CommandID || !api.Equal(current.Installation, catalog.Installation) {
			return api.E("invalid_state", "install_lock_not_disposing")
		}
		var saved disposalProgress
		if _, err = tx.Get(ctx, ns("disposal_progress"), catalog.CommandID, &saved); err != nil {
			return err
		}
		if !saved.Complete || !api.Equal(saved, progress) {
			return api.E("revision_conflict", "disposal_history_not_closed")
		}
		current.Revision = rev + 1
		if evidence.Exited && len(evidence.ResidualRefs) == 0 {
			current.State = "disposed"
		} else {
			current.State = "residual"
		}
		if err = tx.Put(ctx, ns("installations"), current.ID, rev, current); err != nil {
			return err
		}
		if current.State != "disposed" {
			return tx.Hint(ctx, work.Job.JobID, time.Now().Add(time.Second))
		}
		return runtime.Decide(ctx, tx, current.CommandID, StateOutput{Ref: scope.Ref(current.ID, current.Revision), State: current.State}, nil)
	})
}
func (s *Service) readExtension(ctx context.Context, store runtime.Store, scope runtime.Scope, a runtime.Auth, q api.Query, in IDInput) (ExtensionRead, error) {
	if err := requireRole(a, "maintainer"); err != nil {
		return ExtensionRead{}, err
	}
	var out ExtensionRead
	var head BindingHead
	if _, err := store.Read(ctx, scope, ns("heads"), in.ID, 0, &head); err == nil {
		out.Head = &head
		if head.CurrentActivationRef != nil {
			var act Activation
			if _, err = store.Read(ctx, scope, ns("activations"), head.CurrentActivationRef.ObjectID, 0, &act); err != nil {
				return out, err
			}
			out.Activation = &act
		}
		if head.ReadinessRef != nil {
			var ready InstanceReadiness
			if _, err = store.Read(ctx, scope, ns("readiness"), head.ReadinessRef.ObjectID, 0, &ready); err != nil {
				return out, err
			}
			out.Readiness = &ready
		}
		return out, nil
	} else if !errMissing(err) {
		return out, err
	}
	var preparation PreparedInstall
	if _, err := store.Read(ctx, scope, ns("preparations"), in.ID, 0, &preparation); err == nil {
		out.Preparation = &preparation
		return out, nil
	} else if !errMissing(err) {
		return out, err
	}
	var pending PendingActivation
	if _, err := store.Read(ctx, scope, ns("activation_pending"), in.ID, 0, &pending); err == nil {
		out.Pending = &pending
		return out, nil
	} else if !errMissing(err) {
		return out, err
	}
	var catalog PreparedInstall
	if _, err := store.Read(ctx, scope, ns("installations"), in.ID, 0, &catalog); err == nil {
		out.Installation = &catalog
		return out, nil
	} else if !errMissing(err) {
		return out, err
	}
	var activation Activation
	_, err := store.Read(ctx, scope, ns("activations"), in.ID, 0, &activation)
	if err != nil {
		return out, err
	}
	out.Activation = &activation
	return out, nil
}

// CheckBindingTx 是实际同域启动门禁，当前批准、当前head和准确实例必须同时成立。
func (s *Service) CheckBindingTx(ctx context.Context, tx runtime.Tx, target string, bindingRef, instanceRef api.ObjectRef) error {
	var head BindingHead
	if _, err := tx.Get(ctx, ns("heads"), target, &head); err != nil {
		return err
	}
	if !head.Enabled || head.BindingRef == nil || !api.Equal(head.BindingRef, bindingRef) || head.ReadinessRef == nil || !api.Equal(head.ReadinessRef, instanceRef) {
		return api.E("forbidden", "binding_not_current")
	}
	var ready InstanceReadiness
	if _, err := tx.Get(ctx, ns("readiness"), instanceRef.ObjectID, &ready); err != nil {
		return err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return err
	}
	if ready.State != "ready" || ready.Generation != head.Generation || before(now, ready.ExpiresAt) != nil {
		return api.E("forbidden", "instance_not_ready")
	}
	var binding Binding
	if _, err := tx.Get(ctx, ns("bindings"), bindingRef.ObjectID, &binding); err != nil {
		return err
	}
	_, err = s.currentApproval(ctx, tx, ready.ApprovalRef, binding.InstallLockRef, target)
	return err
}

type ApprovalStopImpact struct {
	ID         string `json:"id"`
	Revision   uint64 `json:"revision"`
	ApprovalID string `json:"approval_id"`
	ReportID   string `json:"report_id,omitempty"`
	Cursor     string `json:"cursor,omitempty"`
}

func (s *Service) continueApprovalStop(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	return finish(ctx, store, scope, s.participants(), work, runtime.Done(), func(tx runtime.Tx) error {
		var impact ApprovalStopImpact
		rev, err := tx.Get(ctx, ns("approval_stop_impacts"), work.Job.ResponsibilityKey, &impact)
		if err != nil {
			return err
		}
		rows, err := tx.List(ctx, ns("heads"), "", impact.Cursor, 100)
		if err != nil {
			return err
		}
		for _, row := range rows {
			impact.Cursor = row.ID
			var head BindingHead
			if err = row.Decode(&head); err != nil {
				return err
			}
			if !head.Enabled || head.ReadinessRef == nil {
				continue
			}
			var ready InstanceReadiness
			if _, err = tx.Get(ctx, ns("readiness"), head.ReadinessRef.ObjectID, &ready); err != nil {
				return err
			}
			matches := ready.ApprovalRef.ObjectID == impact.ApprovalID
			if impact.ReportID != "" {
				var approval ReleaseApproval
				if _, err = tx.Get(ctx, ns("approvals"), ready.ApprovalRef.ObjectID, &approval); err != nil {
					return err
				}
				for _, ref := range approval.EvidenceRefs {
					if ref.ObjectID == impact.ReportID {
						matches = true
					}
				}
			}
			if !matches {
				continue
			}
			hrev := head.Revision
			head.Enabled = false
			head.Revision++
			if err = tx.Put(ctx, ns("heads"), head.TargetID, hrev, head); err != nil {
				return err
			}
			if head.CurrentActivationRef != nil {
				var activation Activation
				if _, err = tx.Get(ctx, ns("activations"), head.CurrentActivationRef.ObjectID, &activation); err != nil {
					return err
				}
				if err = s.requestStopTx(ctx, tx, activation, ready.InstanceID); err != nil {
					return err
				}
			}
		}
		impact.Revision = rev + 1
		if err = tx.Put(ctx, ns("approval_stop_impacts"), impact.ID, rev, impact); err != nil {
			return err
		}
		if len(rows) == 100 {
			now, err := tx.Now(ctx)
			if err != nil {
				return err
			}
			return tx.Hint(ctx, work.Job.JobID, now)
		}
		return nil
	})
}
