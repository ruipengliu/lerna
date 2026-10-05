package durableworkdemo

import (
	"context"
	"errors"
	"strconv"

	"github.com/ruipengliu/lerna/contract"
	"github.com/ruipengliu/lerna/runtime"
)

type CleanupResult string

const (
	Cleaned         CleanupResult = "cleaned"
	AlreadyGone     CleanupResult = "already_gone"
	RevisionChanged CleanupResult = "revision_changed"
	WorkPending     CleanupResult = "work_pending"
	NotApplicable   CleanupResult = "not_applicable"
)

type RetentionState struct {
	WorkRevision, CompletedRevision int64
	State                           string
	HasClaim                        bool
	Projection                      *Projection
}

// RetentionRepository preserves command/input/Job lock order in the supplied Tx.
type RetentionRepository interface {
	CommandBodyGone(context.Context, runtime.Tx, contract.CommandRef) (bool, error)
	LockRetention(context.Context, runtime.Tx, contract.OwnerRef, contract.ID) (RetentionState, error)
	ClearBody(context.Context, runtime.Tx, contract.CommandRef, contract.ID, int64) error
}

// Cleanup removes only the exact applied command's successfully projected input.
func (s *Service) Cleanup(ctx context.Context, ref contract.CommandRef, expected contract.Revision, trusted *contract.SubjectBinding) (CleanupResult, error) {
	if trusted == nil || ref.Owner != s.Owner {
		return "", publicError("forbidden", nil)
	}
	data, err := contract.Encode(*trusted)
	if err != nil {
		return "", publicError("forbidden", nil)
	}
	subject, err := contract.Decode[contract.SubjectBinding](data)
	if err != nil || !s.Permissions.Allows(subject, s.Owner, "cleanup") {
		return "", publicError("forbidden", nil)
	}
	if _, err = contract.Encode(ref); err != nil {
		return "", publicError("schema_invalid", err)
	}
	if _, err = contract.Encode(expected); err != nil {
		return "", publicError("schema_invalid", err)
	}
	if err = workContext(ctx); err != nil || s.Retention == nil {
		return "", publicError("dependency_unavailable", err)
	}
	var result CleanupResult
	err = s.Runner.Within(ctx, s.Owner, func(ctx context.Context, tx runtime.Tx) error {
		original, err := s.Commands.LockCommand(ctx, tx, ref)
		if err != nil {
			return err
		}
		if original == nil {
			result = NotApplicable
			return nil
		}
		applied, ok := original.Receipt.AsApplied()
		if !ok || original.Metadata.Method != "durable_work.record" || original.Metadata.Version != "host-durable-work-1" || original.Metadata.Profile != "host" {
			result = NotApplicable
			return nil
		}
		target := original.Metadata.Target
		if target.Kind != applied.ObjectRef.Kind || target.ID != applied.ObjectRef.ID || target.TenantID != applied.ObjectRef.TenantID || target.OwnerID != applied.ObjectRef.OwnerID {
			result = NotApplicable
			return nil
		}
		if applied.CommandRef != ref || applied.Revision != expected || applied.ObjectRef.Kind != "durable_work" || applied.ObjectRef.TenantID != s.Owner.TenantID || applied.ObjectRef.OwnerID != s.Owner.OwnerID {
			result = NotApplicable
			return nil
		}
		gone, err := s.Retention.CommandBodyGone(ctx, tx, ref)
		if err != nil {
			return err
		}
		if gone {
			result = AlreadyGone
			return nil
		}
		input, err := s.Repository.LockInput(ctx, tx, s.Owner, applied.ObjectRef.ID)
		if err != nil {
			return err
		}
		if input == nil || strconv.FormatInt(input.Revision, 10) != string(expected) {
			result = RevisionChanged
			return nil
		}
		if input.BodyGone {
			return errors.New("input gone without original command tombstone")
		}
		retained, err := s.Retention.LockRetention(ctx, tx, s.Owner, input.ID)
		if err != nil {
			return err
		}
		if retained.State != "done" || retained.HasClaim || retained.WorkRevision != input.Revision || retained.CompletedRevision != input.Revision || retained.Projection == nil || retained.Projection.InputRevision != input.Revision {
			result = WorkPending
			return nil
		}
		if err = s.Retention.ClearBody(ctx, tx, ref, input.ID, input.Revision); err != nil {
			return err
		}
		result = Cleaned
		return nil
	})
	if err != nil {
		return "", publicError("dependency_unavailable", err)
	}
	return result, nil
}
