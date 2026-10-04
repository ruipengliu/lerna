package content

import (
	"context"
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

type BodySeal struct {
	PrimaryHolderID string           `json:"primary_holder_id"`
	ID              string           `json:"id"`
	Ref             v.ContentRef     `json:"content_ref"`
	Subject         v.SubjectBinding `json:"subject"`
	Purpose         string           `json:"purpose"`
	StartedAt       time.Time        `json:"started_at"`
	Deadline        time.Time        `json:"deadline"`
}

type BodyHolder struct {
	Kind        string          `json:"kind"`
	Identity    ErasureIdentity `json:"identity"`
	Deadline    time.Time       `json:"deadline"`
	State       string          `json:"state"`
	Responsible string          `json:"responsible"`
	Reason      string          `json:"reason,omitempty"`
}

type LifecycleRepository interface {
	ManagementRepository
	SaveBodyHolder(context.Context, runtime.Tx, BodyHolder) error
	BodyHolders(context.Context, runtime.Tx, v.ContentRef, string, int) ([]BodyHolder, string, error)
	AllBodyHoldersErased(context.Context, runtime.Tx, v.ContentRef, string, string) (bool, error)
}

type BodyCleanupObservation struct {
	Seal            BodySeal     `json:"seal"`
	Holders         []BodyHolder `json:"holders"`
	NextCursor      string       `json:"next_cursor"`
	CleanupComplete bool         `json:"cleanup_complete"`
}

type LifecycleConfig struct {
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
	return &Lifecycle{config: config, manager: manager, store: store}, nil
}

func (l *Lifecycle) Seal(ctx context.Context, subject *v.SubjectBinding, request SealRequest) (BodyCleanupObservation, error) {
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
		if record.BodySeal != nil {
			seal := record.BodySeal
			if seal.ID != request.SealID || seal.Ref != request.Ref || seal.Purpose != request.Purpose || seal.PrimaryHolderID != l.config.PrimaryHolderID || !seal.Deadline.Equal(request.Deadline) {
				return &ManagementConflict{}
			}
			observed, err = l.observeRecord(ctx, tx, *record, "")
			return err
		}
		now, err := l.store.Now(ctx, tx)
		if err != nil {
			return err
		}
		if !request.Deadline.After(now) || request.Deadline.After(now.Add(l.config.WorkBudget)) || request.Deadline.After(l.config.TrustedUntil) {
			return refusal("expired")
		}
		record.BodySeal = &BodySeal{ID: request.SealID, Ref: record.Ref, Subject: record.Subject, Purpose: record.Purpose, PrimaryHolderID: l.config.PrimaryHolderID, StartedAt: now, Deadline: request.Deadline}
		record.CleanupPending = true
		record.Revision++
		if err = l.store.SaveVersion(ctx, tx, *record); err != nil {
			return err
		}
		for _, holder := range []struct{ kind, id string }{{"pg-staging", "postgres-staging"}, {"primary", l.config.PrimaryHolderID}} {
			identity := ErasureIdentity{Ref: record.Ref, ObjectKey: record.ObjectKey, HolderID: holder.id, SealID: request.SealID}
			if err = l.store.SaveBodyHolder(ctx, tx, BodyHolder{Kind: holder.kind, Identity: identity, Deadline: request.Deadline, State: "pending", Responsible: holder.id, Reason: "holder_unconfirmed"}); err != nil {
				return err
			}
		}
		if _, err = l.store.Trigger(ctx, tx, contract.ObjectRef{TenantID: contract.ID(record.Ref.Owner.TenantID), OwnerID: contract.ID(record.Ref.Owner.OwnerID), Kind: "content", ID: contract.ID(record.ObjectID)}, "body_cleanup", record.Revision, now); err != nil {
			return err
		}
		if err = l.manager.current(ctx, tx); err != nil {
			return err
		}
		now, err = l.store.Now(ctx, tx)
		if err != nil {
			return err
		}
		if !now.Before(request.Deadline) {
			return refusal("expired")
		}
		observed, err = l.observeRecord(ctx, tx, *record, "")
		return err
	})
	return observed, err
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
