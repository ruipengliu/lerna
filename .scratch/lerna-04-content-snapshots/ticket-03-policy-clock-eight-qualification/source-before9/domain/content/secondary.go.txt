package content

import (
	"context"
	"errors"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/runtime"
	"strconv"
	"time"
)

// CopyRequest is an internal trusted holder responsibility, never a wire API.
type CopyRequest struct {
	Ref      v.ContentRef
	Purpose  string
	ID       string
	Deadline time.Time
}
type CopyObservation struct {
	Ref       v.ContentRef `json:"content_ref"`
	HolderID  string       `json:"holder_id"`
	Binding   string       `json:"binding"`
	Deadline  time.Time    `json:"deadline"`
	Confirmed bool         `json:"confirmed"`
}

// SecondaryCopy is registered under the original version lock before any body
// leaves its primary holder. A pending invocation remains in the seal's set.
type SecondaryCopy struct {
	Observation CopyObservation  `json:"observation"`
	ID          string           `json:"id"`
	ObjectKey   string           `json:"object_key"`
	AttemptKey  string           `json:"attempt_key"`
	Subject     v.SubjectBinding `json:"subject"`
	Purpose     string           `json:"purpose"`
	StartedAt   time.Time        `json:"started_at"`
}

func (l *Lifecycle) copyBasis(ctx context.Context, tx runtime.Tx, request CopyRequest) (*Record, time.Time, error) {
	if err := l.manager.current(ctx, tx); err != nil {
		return nil, time.Time{}, err
	}
	now, err := l.store.Now(ctx, tx)
	if err != nil {
		return nil, time.Time{}, err
	}
	policy, err := l.store.CheckPolicy(ctx, tx, l.config.TrustedSubject, request.Ref, request.Purpose, []string{"read", "save", "sync"}, now)
	if err != nil {
		return nil, time.Time{}, err
	}
	if policy == nil {
		return nil, time.Time{}, refusal("forbidden")
	}
	record, err := l.store.LockVersion(ctx, tx, request.Ref)
	if err != nil {
		return nil, time.Time{}, err
	}
	now, err = l.store.Now(ctx, tx)
	if err != nil {
		return nil, time.Time{}, err
	}
	if record == nil && policy.Ref != request.Ref || record != nil && policy.Ref != record.Ref {
		return nil, time.Time{}, refusal("forbidden")
	}
	bound := earlier(policy.ValidUntil, policy.RetainUntil)
	if !now.Before(bound) {
		return nil, time.Time{}, refusal("expired")
	}
	if record == nil || record.Ref != request.Ref || record.Publication != "published" || record.Purpose != request.Purpose || !sameSavingSubject(record.Subject, l.config.TrustedSubject) {
		return nil, time.Time{}, refusal("source_unavailable")
	}
	if record.BodySeal != nil {
		return nil, time.Time{}, refusal("forbidden")
	}
	if record.PrimaryHolderBinding == "" || record.PrimaryHolderBinding != l.config.Objects.Binding() {
		return nil, time.Time{}, ErrHolderBinding
	}
	service := Service{config: Config{Owner: l.config.Owner, Store: l.store}}
	sources, err := service.registeredClosure(ctx, tx, record.Ref, record.Sources, record.Subject, record.Purpose, []string{"read", "save", "sync"})
	if err != nil {
		return nil, time.Time{}, err
	}
	bound = earlier(bound, cutoff(record.CurrentRetainUntil))
	if len(sources.refs) > 0 {
		bound = earlier(bound, earlier(sources.validBefore, sources.retainBefore))
	}
	if err = l.manager.current(ctx, tx); err != nil {
		return nil, time.Time{}, err
	}
	now, err = l.store.Now(ctx, tx)
	if err != nil {
		return nil, time.Time{}, err
	}
	if !now.Before(bound) {
		return nil, time.Time{}, refusal("expired")
	}
	return record, bound, nil
}

func (l *Lifecycle) CopyToSecondary(ctx context.Context, subject *v.SubjectBinding, request CopyRequest) (CopyObservation, error) {
	var copy SecondaryCopy
	if err := l.manager.authorize(ctx, subject); err != nil {
		return copy.Observation, err
	}
	if request.Ref.Owner != l.config.Owner || request.ID == "" || len(request.ID) > 128 || request.Purpose == "" || request.Deadline.IsZero() {
		return copy.Observation, refusal("forbidden")
	}
	if l.config.SecondaryHolderID == "" || l.config.SecondaryObjects == nil {
		return copy.Observation, ErrUnavailable
	}
	if l.config.SecondaryObjects.Binding() == "" || l.config.SecondaryObjects.Binding() == l.config.Objects.Binding() {
		return copy.Observation, ErrHolderBinding
	}
	var record *Record
	var bound time.Time
	replay := false
	err := l.store.Within(ctx, owner(l.config.Owner), func(ctx context.Context, tx runtime.Tx) error {
		var err error
		record, bound, err = l.copyBasis(ctx, tx, request)
		if err != nil {
			return err
		}
		now, err := l.store.Now(ctx, tx)
		if err != nil {
			return err
		}
		existing, err := l.store.LockSecondaryCopy(ctx, tx, record.Ref, l.config.SecondaryHolderID)
		if err != nil {
			return err
		}
		if existing != nil {
			copy = *existing
			if copy.ID != request.ID || copy.Observation.Ref != record.Ref || copy.Purpose != request.Purpose || copy.Observation.Binding != l.config.SecondaryObjects.Binding() || !copy.Observation.Deadline.Equal(request.Deadline) {
				return &ManagementConflict{}
			}
			replay = copy.Observation.Confirmed
		} else {
			if !request.Deadline.After(now) || request.Deadline.After(now.Add(l.config.WorkBudget)) || request.Deadline.After(l.config.TrustedUntil) {
				return refusal("expired")
			}
			copy = SecondaryCopy{Observation: CopyObservation{Ref: record.Ref, HolderID: l.config.SecondaryHolderID, Binding: l.config.SecondaryObjects.Binding(), Deadline: request.Deadline}, ID: request.ID, ObjectKey: record.ObjectKey, AttemptKey: record.ObjectKey + ".1.tmp", Subject: record.Subject, Purpose: record.Purpose, StartedAt: now}
			if err = l.store.SaveSecondaryCopy(ctx, tx, copy); err != nil {
				return err
			}
		}
		now, err = l.store.Now(ctx, tx)
		if err != nil {
			return err
		}
		bound = earlier(bound, earlier(copy.Observation.Deadline, l.config.TrustedUntil))
		if !now.Before(bound) {
			return refusal("expired")
		}
		return l.manager.current(ctx, tx)
	})
	if err != nil {
		copy.Observation.Confirmed = false
		return copy.Observation, err
	}
	if replay {
		return copy.Observation, nil
	}
	bounded, cancel := context.WithDeadline(ctx, bound)
	defer cancel()
	length, _ := strconv.ParseInt(string(record.Ref.ByteLength), 10, 64)
	bytes, err := l.config.Objects.Read(bounded, record.ObjectKey, record.Ref.Hash, length)
	if err == nil {
		err = bounded.Err()
	}
	if err != nil {
		return copy.Observation, err
	}
	if copy.Observation.Binding != l.config.SecondaryObjects.Binding() {
		return copy.Observation, ErrHolderBinding
	}
	err = l.config.SecondaryObjects.Put(bounded, copy.ObjectKey, copy.AttemptKey, record.Ref.Hash, length, bytes)
	if err == nil {
		_, err = l.config.SecondaryObjects.Read(bounded, copy.ObjectKey, record.Ref.Hash, length)
	}
	if err == nil {
		err = bounded.Err()
	}
	if err != nil {
		return copy.Observation, err
	}
	err = l.store.Within(ctx, owner(l.config.Owner), func(ctx context.Context, tx runtime.Tx) error {
		_, current, err := l.copyBasis(ctx, tx, request)
		if err != nil {
			return err
		}
		registered, err := l.store.LockSecondaryCopy(ctx, tx, request.Ref, l.config.SecondaryHolderID)
		if err != nil {
			return err
		}
		if registered == nil || registered.ID != copy.ID || registered.Observation.Binding != copy.Observation.Binding || !registered.Observation.Deadline.Equal(copy.Observation.Deadline) {
			return runtime.ErrScope
		}
		now, err := l.store.Now(ctx, tx)
		if err != nil {
			return err
		}
		if !now.Before(earlier(current, copy.Observation.Deadline)) {
			return refusal("expired")
		}
		copy.Observation.Confirmed = true
		if err = l.store.SaveSecondaryCopy(ctx, tx, copy); err != nil {
			return err
		}
		if err = l.manager.current(ctx, tx); err != nil {
			return err
		}
		now, err = l.store.Now(ctx, tx)
		if err != nil {
			return err
		}
		if !now.Before(earlier(current, copy.Observation.Deadline)) {
			return refusal("expired")
		}
		return nil
	})
	err = errors.Join(err, bounded.Err())
	if err != nil {
		copy.Observation.Confirmed = false
	}
	return copy.Observation, err
}
