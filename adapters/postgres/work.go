package postgres

import (
	"context"
	"time"

	"github.com/ruipengliu/lerna/contract"
	demo "github.com/ruipengliu/lerna/internal/durableworkdemo"
	"github.com/ruipengliu/lerna/runtime"
)

func (s *Store) TryLockInput(ctx context.Context, token runtime.Tx, owner contract.OwnerRef, id contract.ID) (*demo.Input, error) {
	tx, err := s.token(ctx, token, owner)
	if err != nil {
		return nil, err
	}
	var locked bool
	if err = tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(2,hashtext($1))`, s.lockKey("input", owner, string(id))).Scan(&locked); err != nil || !locked {
		return nil, err
	}
	return s.LockInput(ctx, token, owner, id)
}

func (s *Store) Scan(ctx context.Context, token runtime.Tx, now time.Time, limit int) ([]runtime.Job, error) {
	return s.core.Scan(ctx, token, now, limit)
}
func (s *Store) Claim(ctx context.Context, token runtime.Tx, candidate runtime.Job, worker string, now, until time.Time) (*runtime.Claim, error) {
	return s.core.Claim(ctx, token, candidate, worker, now, until)
}
func validClaim(claim runtime.Claim) bool {
	return claim.JobID != "" && claim.Object.Revision == nil && claim.ClaimedRevision > 0 && claim.Epoch > 0 && claim.Worker != "" && len(claim.Worker) <= 128 && !claim.LeaseUntil.IsZero()
}

func (s *Store) Complete(ctx context.Context, token runtime.Tx, claim runtime.Claim, now time.Time) error {
	return s.core.Complete(ctx, token, claim, now)
}
func (s *Store) SaveProjection(ctx context.Context, token runtime.Tx, owner contract.OwnerRef, id contract.ID, projection demo.Projection) error {
	tx, err := s.token(ctx, token, owner)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE `+s.table("durable_inputs")+` SET projected_revision=$4,text_digest=$5 WHERE tenant_id=$1 AND owner_id=$2 AND object_id=$3 AND revision >= $4 AND (projected_revision IS NULL OR projected_revision < $4)`, owner.TenantID, owner.OwnerID, id, projection.InputRevision, projection.TextDigest)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count != 1 {
		return runtime.ErrClaim
	}
	return err
}

func (s *Store) Renew(ctx context.Context, token runtime.Tx, claim runtime.Claim, now, until time.Time) (runtime.Claim, error) {
	return s.core.Renew(ctx, token, claim, now, until)
}
