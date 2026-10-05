package content

import (
	"context"
	"errors"
	"github.com/ruipengliu/lerna/contract"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/runtime"
	"time"
)

type SealRequest struct {
	Ref      v.ContentRef
	Purpose  string
	SealID   string
	Deadline time.Time
}

// MetadataPolicy grants only the exact ref/evidence_available view. It does
// not grant any of the five body actions or reveal the source list.
type MetadataPolicy struct {
	Ref        v.ContentRef     `json:"content_ref"`
	Subject    v.SubjectBinding `json:"subject"`
	Purpose    string           `json:"purpose"`
	Revision   int64            `json:"revision"`
	ValidUntil time.Time        `json:"valid_until"`
}

type StagingObservation struct {
	Ref     v.ContentRef `json:"content_ref"`
	Present bool         `json:"present"`
}

type BodySeal struct {
	PolicyChangeKey      string           `json:"policy_change_key,omitempty"`
	PrimaryHolderBinding string           `json:"primary_holder_binding"`
	PrimaryHolderID      string           `json:"primary_holder_id"`
	ID                   string           `json:"id"`
	Ref                  v.ContentRef     `json:"content_ref"`
	Subject              v.SubjectBinding `json:"subject"`
	Purpose              string           `json:"purpose"`
	StartedAt            time.Time        `json:"started_at"`
	Deadline             time.Time        `json:"deadline"`
}

type BodyHolder struct {
	CopyID         string          `json:"copy_id,omitempty"`
	EffectDeadline time.Time       `json:"effect_deadline,omitempty"`
	AttemptKeys    []string        `json:"attempt_keys,omitempty"`
	AttemptCursor  string          `json:"attempt_cursor,omitempty"`
	Kind           string          `json:"kind"`
	Identity       ErasureIdentity `json:"identity"`
	Deadline       time.Time       `json:"deadline"`
	State          string          `json:"state"`
	Responsible    string          `json:"responsible"`
	Reason         string          `json:"reason,omitempty"`
}

type LifecycleRepository interface {
	ManagementRepository
	// Selection only; the locked original identity, clock and Claim remain authority.
	ScanBodyCleanup(context.Context, runtime.Tx, time.Time, v.SubjectBinding, string, int) ([]runtime.Job, error)
	BindLegacyPrimary(context.Context, runtime.Tx, Record, LegacyPrimaryQualification) (Record, error)
	QualifyPolicyCleanupNotRequired(context.Context, runtime.Tx, CleanupResponsibility) error
	CurrentSavingPolicy(context.Context, runtime.Tx, v.SubjectBinding, v.ContentRef, string) (*FixturePolicy, error)
	BindPolicyCleanupSeal(context.Context, runtime.Tx, CleanupResponsibility, BodySeal) error
	AcknowledgePolicyCleanupErased(context.Context, runtime.Tx, BodySeal) error
	LockSecondaryCopy(context.Context, runtime.Tx, v.ContentRef, string) (*SecondaryCopy, error)
	SaveSecondaryCopy(context.Context, runtime.Tx, SecondaryCopy) error
	SecondaryCopies(context.Context, runtime.Tx, v.ContentRef, string, int) ([]SecondaryCopy, string, error)
	SaveBodyHolder(context.Context, runtime.Tx, BodyHolder) error
	BodyHolders(context.Context, runtime.Tx, v.ContentRef, string, int) ([]BodyHolder, string, error)
	AllBodyHoldersErased(context.Context, runtime.Tx, v.ContentRef, string, string) (bool, error)
	AuthoritativeBodyErased(context.Context, runtime.Tx, v.ContentRef, string, string) (bool, error)
	PublicationAttempts(context.Context, runtime.Tx, v.ContentRef, string, int) ([]string, string, error)
	ObserveStaging(context.Context, v.ContentRef) (StagingObservation, error)
	LockMetadataPolicy(context.Context, runtime.Tx, MetadataPolicy) (*MetadataPolicy, error)
	SaveMetadataPolicy(context.Context, runtime.Tx, MetadataPolicy) error
}

type BodyCleanupObservation struct {
	Seal            BodySeal     `json:"seal"`
	Holders         []BodyHolder `json:"holders"`
	NextCursor      string       `json:"next_cursor"`
	CleanupComplete bool         `json:"cleanup_complete"`
}

type LifecycleConfig struct {
	LegacyPrimary     *LegacyPrimaryQualification
	SecondaryHolderID string
	SecondaryObjects  ErasingObjects
	ManagementConfig
	PrimaryHolderID string
	Objects         ErasingObjects
	Worker          string
}

type Lifecycle struct {
	config  LifecycleConfig
	manager *Manager
	store   LifecycleRepository
}

func NewLifecycle(config LifecycleConfig) (*Lifecycle, error) {
	manager, err := NewManager(config.ManagementConfig)
	if err != nil {
		return nil, err
	}
	if config.PrimaryHolderID == "" || config.Objects == nil || config.Worker == "" {
		return nil, ErrUnavailable
	}
	store, ok := config.Store.(LifecycleRepository)
	if !ok || len(config.PrimaryHolderID) > 128 || config.PrimaryHolderID == "postgres-staging" || len(config.Worker) > 128 {
		return nil, ErrUnavailable
	}
	if len(config.SecondaryHolderID) > 128 || config.SecondaryHolderID == config.PrimaryHolderID || config.SecondaryHolderID == "postgres-staging" || config.SecondaryObjects != nil && config.SecondaryHolderID == "" {
		return nil, ErrUnavailable
	}
	config.LegacyPrimary = cloneLegacyPrimary(config.LegacyPrimary)
	return &Lifecycle{config: config, manager: manager, store: store}, nil
}

func (l *Lifecycle) Seal(ctx context.Context, subject *v.SubjectBinding, request SealRequest) (BodyCleanupObservation, error) {
	return l.seal(ctx, subject, request, false)
}

func (l *Lifecycle) seal(ctx context.Context, subject *v.SubjectBinding, request SealRequest, orphanOnly bool) (BodyCleanupObservation, error) {
	var observed BodyCleanupObservation
	if err := l.manager.authorize(ctx, subject); err != nil {
		return observed, err
	}
	if request.Ref.Owner != l.config.Owner || request.Purpose == "" || request.SealID == "" || len(request.SealID) > 128 || request.Deadline.IsZero() {
		return observed, refusal("forbidden")
	}
	if _, err := v.Encode(request.Ref); err != nil {
		return observed, err
	}
	err := l.store.Within(ctx, owner(l.config.Owner), func(ctx context.Context, tx runtime.Tx) error {
		if err := l.manager.current(ctx, tx); err != nil {
			return err
		}
		record, err := l.store.LockVersion(ctx, tx, request.Ref)
		if err != nil {
			return err
		}
		if err = l.manager.current(ctx, tx); err != nil {
			return err
		}
		// Trusted deletion is scoped to the original complete saving basis.
		if record == nil || record.Ref != request.Ref || record.Purpose != request.Purpose || !sameSavingSubject(record.Subject, l.config.TrustedSubject) {
			return refusal("forbidden")
		}
		// Publication finalization and orphan selection compete on this same
		// original locked row. A live published reference always wins safely.
		if orphanOnly {
			if record.Publication == "published" {
				return ErrOrphanReferenced
			}
			if record.Publication != "preparing" && record.Publication != "failed" {
				return runtime.ErrScope
			}
		}
		observed, err = l.sealLocked(ctx, tx, record, request, "")
		return err
	})
	return observed, err
}

// sealLocked is shared by the voluntary, orphan and policy consumers. The
// caller already owns the original version lock and its own cause/authority.
func (l *Lifecycle) sealLocked(ctx context.Context, tx runtime.Tx, record *Record, request SealRequest, policyKey string) (BodyCleanupObservation, error) {
	if record.PrimaryHolderBinding == "" || record.PrimaryHolderBinding != l.config.Objects.Binding() {
		return BodyCleanupObservation{}, ErrHolderBinding
	}
	if record.BodySeal != nil {
		seal := record.BodySeal
		if seal.ID != request.SealID || seal.Ref != request.Ref || seal.Purpose != request.Purpose || seal.PrimaryHolderID != l.config.PrimaryHolderID || seal.PolicyChangeKey != policyKey || !seal.Deadline.Equal(request.Deadline) {
			return BodyCleanupObservation{}, &ManagementConflict{}
		}
		return l.observeRecord(ctx, tx, *record, "")
	}
	now, err := l.store.Now(ctx, tx)
	if err != nil {
		return BodyCleanupObservation{}, err
	}
	if !request.Deadline.After(now) || request.Deadline.After(now.Add(l.config.WorkBudget)) || request.Deadline.After(l.config.TrustedUntil) {
		return BodyCleanupObservation{}, refusal("expired")
	}
	record.BodySeal = &BodySeal{PolicyChangeKey: policyKey, PrimaryHolderBinding: record.PrimaryHolderBinding, ID: request.SealID, Ref: record.Ref, Subject: record.Subject, Purpose: record.Purpose, PrimaryHolderID: l.config.PrimaryHolderID, StartedAt: now, Deadline: request.Deadline}
	record.CleanupPending = true
	record.Revision++
	if err = l.store.SaveVersion(ctx, tx, *record); err != nil {
		return BodyCleanupObservation{}, err
	}
	for _, holder := range []struct{ kind, id string }{{"pg-staging", "postgres-staging"}, {"primary", l.config.PrimaryHolderID}} {
		identity := ErasureIdentity{Ref: record.Ref, ObjectKey: record.ObjectKey, HolderID: holder.id, SealID: request.SealID}
		if holder.kind == "primary" {
			identity.Binding = record.PrimaryHolderBinding
		}
		if err = l.store.SaveBodyHolder(ctx, tx, BodyHolder{Kind: holder.kind, Identity: identity, Deadline: request.Deadline, State: "pending", Responsible: holder.id, Reason: "holder_unconfirmed"}); err != nil {
			return BodyCleanupObservation{}, err
		}
	}
	cursor := ""
	for {
		copies, next, err := l.store.SecondaryCopies(ctx, tx, record.Ref, cursor, l.config.PageSize)
		if err != nil {
			return BodyCleanupObservation{}, err
		}
		for _, copy := range copies {
			if copy.Observation.Ref != record.Ref || copy.ObjectKey != record.ObjectKey || copy.Purpose != record.Purpose || !sameSavingSubject(copy.Subject, record.Subject) {
				return BodyCleanupObservation{}, runtime.ErrScope
			}
			identity := ErasureIdentity{Ref: record.Ref, ObjectKey: record.ObjectKey, HolderID: copy.Observation.HolderID, SealID: request.SealID, Binding: copy.Observation.Binding}
			if err = l.store.SaveBodyHolder(ctx, tx, BodyHolder{Kind: "secondary", Identity: identity, Deadline: request.Deadline, State: "pending", Responsible: copy.Observation.HolderID, Reason: "holder_unconfirmed", CopyID: copy.ID, EffectDeadline: copy.Observation.Deadline, AttemptKeys: []string{copy.AttemptKey}}); err != nil {
				return BodyCleanupObservation{}, err
			}
		}
		if next == "" {
			break
		}
		cursor = next
		if err = l.manager.current(ctx, tx); err != nil {
			return BodyCleanupObservation{}, err
		}
	}
	if _, err = l.store.Trigger(ctx, tx, contract.ObjectRef{TenantID: contract.ID(record.Ref.Owner.TenantID), OwnerID: contract.ID(record.Ref.Owner.OwnerID), Kind: "content", ID: contract.ID(record.ObjectID)}, "body_cleanup", record.Revision, now); err != nil {
		return BodyCleanupObservation{}, err
	}
	if err = l.manager.current(ctx, tx); err != nil {
		return BodyCleanupObservation{}, err
	}
	now, err = l.store.Now(ctx, tx)
	if err != nil {
		return BodyCleanupObservation{}, err
	}
	if !now.Before(request.Deadline) {
		return BodyCleanupObservation{}, refusal("expired")
	}
	return l.observeRecord(ctx, tx, *record, "")
}

func (l *Lifecycle) Observe(ctx context.Context, subject *v.SubjectBinding, ref v.ContentRef, cursor string) (BodyCleanupObservation, error) {
	var observed BodyCleanupObservation
	if err := l.manager.authorize(ctx, subject); err != nil {
		return observed, err
	}
	if ref.Owner != l.config.Owner || len(cursor) > 128 {
		return observed, refusal("forbidden")
	}
	err := l.store.Within(ctx, owner(l.config.Owner), func(ctx context.Context, tx runtime.Tx) error {
		if err := l.manager.current(ctx, tx); err != nil {
			return err
		}
		record, err := l.store.LockVersion(ctx, tx, ref)
		if err != nil {
			return err
		}
		if err = l.manager.current(ctx, tx); err != nil {
			return err
		}
		if record == nil || record.Ref != ref || !sameSavingSubject(record.Subject, l.config.TrustedSubject) {
			return refusal("forbidden")
		}
		observed, err = l.observeRecord(ctx, tx, *record, cursor)
		return err
	})
	return observed, err
}

func (l *Lifecycle) observeRecord(ctx context.Context, tx runtime.Tx, record Record, cursor string) (BodyCleanupObservation, error) {
	if record.BodySeal == nil {
		return BodyCleanupObservation{}, ErrUnavailable
	}
	holders, next, err := l.store.BodyHolders(ctx, tx, record.Ref, cursor, l.config.PageSize)
	if err != nil {
		return BodyCleanupObservation{}, err
	}
	complete, err := l.store.AllBodyHoldersErased(ctx, tx, record.Ref, record.BodySeal.ID, record.BodySeal.PrimaryHolderID)
	if err != nil {
		return BodyCleanupObservation{}, err
	}
	if err = l.manager.current(ctx, tx); err != nil {
		return BodyCleanupObservation{}, err
	}
	return BodyCleanupObservation{Seal: *record.BodySeal, Holders: holders, NextCursor: next, CleanupComplete: complete}, nil
}

func sameSavingSubject(a, b v.SubjectBinding) bool {
	left, err := v.Encode(a)
	if err != nil {
		return false
	}
	right, err := v.Encode(b)
	return err == nil && string(left) == string(right)
}

func (l *Lifecycle) Step(ctx context.Context, subject *v.SubjectBinding) (bool, error) {
	if err := l.manager.authorize(ctx, subject); err != nil {
		return false, err
	}
	var selected *Record
	var holder BodyHolder
	var claim *runtime.Claim
	var attempts []string
	var nextAttempt string
	err := l.store.Within(ctx, owner(l.config.Owner), func(ctx context.Context, tx runtime.Tx) error {
		if err := l.manager.current(ctx, tx); err != nil {
			return err
		}
		now, err := l.store.Now(ctx, tx)
		if err != nil {
			return err
		}
		jobs, err := l.store.ScanBodyCleanup(ctx, tx, now, l.config.TrustedSubject, l.config.PrimaryHolderID, 64)
		if err != nil {
			return err
		}
		for _, job := range jobs {
			if job.Phase != "body_cleanup" {
				continue
			}
			record, err := l.store.LockObject(ctx, tx, string(job.Object.ID))
			if err != nil {
				return err
			}
			if record == nil || record.ValidateIdentity() != nil || record.BodySeal == nil || record.BodySeal.PrimaryHolderID != l.config.PrimaryHolderID || !sameSavingSubject(record.Subject, l.config.TrustedSubject) {
				return runtime.ErrScope
			}
			if err = l.manager.current(ctx, tx); err != nil {
				return err
			}
			now, err = l.store.Now(ctx, tx)
			if err != nil {
				return err
			}
			if !now.Before(record.BodySeal.Deadline) {
				continue
			}
			found := false
			cursor := ""
			for {
				holders, next, err := l.store.BodyHolders(ctx, tx, record.Ref, cursor, l.config.PageSize)
				if err != nil {
					return err
				}
				for _, candidate := range holders {
					if candidate.Identity.SealID != record.BodySeal.ID {
						return runtime.ErrScope
					}
					if candidate.State == "erased" {
						continue
					}
					holder = candidate
					found = true
					break
				}
				if found || next == "" {
					break
				}
				cursor = next
				if err = l.manager.current(ctx, tx); err != nil {
					return err
				}
			}
			now, err = l.store.Now(ctx, tx)
			if err != nil {
				return err
			}
			if !now.Before(record.BodySeal.Deadline) {
				continue
			}
			if found && holder.Kind == "primary" && (holder.Identity.Binding == "" || holder.Identity.Binding != record.PrimaryHolderBinding || holder.Identity.Binding != record.BodySeal.PrimaryHolderBinding || holder.Identity.Binding != l.config.Objects.Binding()) {
				return ErrHolderBinding
			}
			until := earlier(earlier(now.Add(time.Minute), record.BodySeal.Deadline), l.config.TrustedUntil)
			claim, err = l.store.Claim(ctx, tx, job, l.config.Worker, now, until)
			if err != nil {
				return err
			}
			if claim == nil {
				continue
			}
			if !found {
				complete, err := l.store.AllBodyHoldersErased(ctx, tx, record.Ref, record.BodySeal.ID, record.BodySeal.PrimaryHolderID)
				if err != nil {
					return err
				}
				if !complete {
					return runtime.ErrScope
				}
				return l.completeCleanup(ctx, tx, *record, *claim)
			}
			if holder.Kind == "pg-staging" {
				record.Bytes = nil
				record.StagingHolder = false
				record.Revision++
				if err = l.store.SaveVersion(ctx, tx, *record); err != nil {
					return err
				}
			} else if holder.Kind == "primary" {
				attempts, nextAttempt, err = l.store.PublicationAttempts(ctx, tx, record.Ref, holder.AttemptCursor, l.config.PageSize)
				if err != nil {
					return err
				}
			} else if holder.Kind == "secondary" {
				if holder.Identity.HolderID != l.config.SecondaryHolderID {
					return ErrHolderBinding
				}
				attempts = append([]string{}, holder.AttemptKeys...)
			} else {
				return runtime.ErrScope
			}
			if err = l.manager.current(ctx, tx); err != nil {
				return err
			}
			now, err = l.store.Now(ctx, tx)
			if err != nil {
				return err
			}
			if !now.Before(record.BodySeal.Deadline) {
				return refusal("expired")
			}
			if err = l.store.ValidateClaim(ctx, tx, *claim, now); err != nil {
				return err
			}
			selected = record
			return nil
		}
		return nil
	})
	if err != nil || claim == nil || selected == nil {
		return claim != nil, err
	}
	bounded, cancel := context.WithDeadline(ctx, earlier(claim.LeaseUntil, selected.BodySeal.Deadline))
	var observed ErasureObservation
	var effectErr error
	if holder.Kind == "pg-staging" {
		var staging StagingObservation
		staging, effectErr = l.store.ObserveStaging(bounded, selected.Ref)
		observed = ErasureObservation{Identity: holder.Identity, Fenced: true, Erased: effectErr == nil && staging.Ref == selected.Ref && !staging.Present}
	} else {
		objects := l.config.Objects
		if holder.Kind == "secondary" {
			objects = l.config.SecondaryObjects
		}
		if objects == nil {
			effectErr = ErrUnavailable
		} else if holder.Identity.Binding == "" || holder.Identity.Binding != objects.Binding() {
			effectErr = ErrHolderBinding
		} else {
			observed, effectErr = objects.FenceAndErase(bounded, holder.Identity, attempts)
			if effectErr == nil {
				// A separately acquired holder observation qualifies exact absence.
				observed, effectErr = objects.ObserveErasure(bounded, holder.Identity, "", l.config.PageSize)
			}
		}
	}
	if effectErr == nil {
		effectErr = bounded.Err()
	}
	cancel()
	err = l.store.Within(ctx, owner(l.config.Owner), func(ctx context.Context, tx runtime.Tx) error {
		if err := l.manager.current(ctx, tx); err != nil {
			return err
		}
		record, err := l.store.LockVersion(ctx, tx, selected.Ref)
		if err != nil {
			return err
		}
		if record == nil || record.Ref != selected.Ref || record.BodySeal == nil || record.BodySeal.ID != selected.BodySeal.ID {
			return runtime.ErrScope
		}
		if err = l.manager.current(ctx, tx); err != nil {
			return err
		}
		now, err := l.store.Now(ctx, tx)
		if err != nil {
			return err
		}
		if !now.Before(record.BodySeal.Deadline) {
			return refusal("expired")
		}
		if err = l.store.ValidateClaim(ctx, tx, *claim, now); err != nil {
			return err
		}
		if effectErr == nil && observed.Identity == holder.Identity && observed.Fenced && observed.Erased && observed.NextCursor == "" && nextAttempt == "" {
			holder.State = "erased"
			holder.Reason = ""
		} else if effectErr == nil && nextAttempt != "" {
			holder.AttemptCursor = nextAttempt
			holder.State = "pending"
			holder.Reason = "attempt_page_pending"
		} else {
			holder.State = "residual"
			holder.Reason = "holder_unconfirmed"
		}
		if err = l.store.SaveBodyHolder(ctx, tx, holder); err != nil {
			return err
		}
		authoritative, err := l.store.AuthoritativeBodyErased(ctx, tx, record.Ref, record.BodySeal.ID, record.BodySeal.PrimaryHolderID)
		if err != nil {
			return err
		}
		if authoritative && (!record.BodyGone || record.ObjectHolder) {
			record.BodyGone = true
			record.ObjectHolder = false
			record.Revision++
			if err = l.store.SaveVersion(ctx, tx, *record); err != nil {
				return err
			}
		}
		complete, err := l.store.AllBodyHoldersErased(ctx, tx, record.Ref, record.BodySeal.ID, record.BodySeal.PrimaryHolderID)
		if err != nil {
			return err
		}
		if err = l.manager.current(ctx, tx); err != nil {
			return err
		}
		now, err = l.store.Now(ctx, tx)
		if err != nil {
			return err
		}
		if !now.Before(record.BodySeal.Deadline) {
			return refusal("expired")
		}
		if complete {
			return l.completeCleanup(ctx, tx, *record, *claim)
		}
		// DeferClaim requires a strictly later due time. One microsecond is
		// finite continuation of the same original responsibility, never a
		// refreshed cleanup budget; ordinary DB round trips make it eligible.
		due := earlier(now.Add(time.Microsecond), record.BodySeal.Deadline)
		if holder.State == "residual" {
			due = earlier(now.Add(100*time.Millisecond), record.BodySeal.Deadline)
		}
		return l.store.DeferClaim(ctx, tx, *claim, now, due)
	})
	return true, errors.Join(err, effectErr)
}

func (l *Lifecycle) completeCleanup(ctx context.Context, tx runtime.Tx, record Record, claim runtime.Claim) error {
	validate := func() (time.Time, error) {
		if err := l.manager.current(ctx, tx); err != nil {
			return time.Time{}, err
		}
		now, err := l.store.Now(ctx, tx)
		if err != nil {
			return now, err
		}
		if record.BodySeal == nil || !now.Before(record.BodySeal.Deadline) {
			return now, refusal("expired")
		}
		return now, l.store.ValidateClaim(ctx, tx, claim, now)
	}
	if _, err := validate(); err != nil {
		return err
	}
	if record.BodySeal.PolicyChangeKey != "" {
		if err := l.store.AcknowledgePolicyCleanupErased(ctx, tx, *record.BodySeal); err != nil {
			return err
		}
	}
	// The original responsibility row lock/CAS can wait. Its ACK and this
	// completion commit together only after current clock/claim qualification.
	now, err := validate()
	if err != nil {
		return err
	}
	return l.store.Complete(ctx, tx, claim, now)
}

func (l *Lifecycle) ObserveStaging(ctx context.Context, subject *v.SubjectBinding, ref v.ContentRef) (StagingObservation, error) {
	if err := l.manager.authorize(ctx, subject); err != nil {
		return StagingObservation{}, err
	}
	if ref.Owner != l.config.Owner {
		return StagingObservation{}, refusal("forbidden")
	}
	qualify := func(ctx context.Context, tx runtime.Tx) error {
		if err := l.manager.current(ctx, tx); err != nil {
			return err
		}
		record, err := l.store.LockVersion(ctx, tx, ref)
		if err != nil {
			return err
		}
		if err = l.manager.current(ctx, tx); err != nil {
			return err
		}
		if record == nil || record.Ref != ref || !sameSavingSubject(record.Subject, l.config.TrustedSubject) {
			return refusal("forbidden")
		}
		return nil
	}
	if err := l.store.Within(ctx, owner(l.config.Owner), qualify); err != nil {
		return StagingObservation{}, err
	}
	observed, err := l.store.ObserveStaging(ctx, ref)
	if err != nil {
		return StagingObservation{}, err
	}
	if err = l.store.Within(ctx, owner(l.config.Owner), qualify); err != nil {
		return StagingObservation{}, err
	}
	return observed, nil
}

func (l *Lifecycle) InstallMetadataPolicy(ctx context.Context, subject *v.SubjectBinding, policy MetadataPolicy, expectedRevision int64) error {
	if err := l.manager.authorize(ctx, subject); err != nil {
		return err
	}
	if policy.Ref.Owner != l.config.Owner || policy.Subject.TenantID != l.config.Owner.TenantID || policy.Purpose == "" || len(policy.Purpose) > 128 || policy.Revision < 1 || expectedRevision < 0 || policy.Revision != expectedRevision+1 || policy.ValidUntil.IsZero() {
		return refusal("forbidden")
	}
	if _, err := v.Encode(policy.Ref); err != nil {
		return err
	}
	if _, err := v.Encode(policy.Subject); err != nil {
		return err
	}
	return l.store.Within(ctx, owner(l.config.Owner), func(ctx context.Context, tx runtime.Tx) error {
		if err := l.manager.current(ctx, tx); err != nil {
			return err
		}
		previous, err := l.store.LockMetadataPolicy(ctx, tx, policy)
		if err != nil {
			return err
		}
		if previous != nil && previous.Revision == policy.Revision && previous.Ref == policy.Ref && previous.Purpose == policy.Purpose && sameSavingSubject(previous.Subject, policy.Subject) && previous.ValidUntil.Equal(policy.ValidUntil) {
			return l.manager.current(ctx, tx)
		}
		if previous == nil && expectedRevision != 0 || previous != nil && previous.Revision != expectedRevision {
			return &ManagementConflict{}
		}
		if err = l.store.SaveMetadataPolicy(ctx, tx, policy); err != nil {
			return err
		}
		return l.manager.current(ctx, tx)
	})
}
