package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"

	"github.com/ruipengliu/lerna/contract"
	demo "github.com/ruipengliu/lerna/internal/durableworkdemo"
	"github.com/ruipengliu/lerna/runtime"
)

func (s *Store) TryLockInput(ctx context.Context, token runtime.Tx, owner contract.OwnerRef, id contract.ID) (*demo.Input, error) {
	// BEGIN IMMEDIATE already holds the owner's only write transaction.
	if _, err := s.token(ctx, token, owner); err != nil {
		return nil, err
	}
	return s.LockInput(ctx, token, owner, id)
}

func (s *Store) Scan(ctx context.Context, token runtime.Tx, now time.Time, limit int) ([]runtime.Job, error) {
	tx, err := s.localToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if limit < 1 || limit > 64 {
		return nil, runtime.ErrWorkBounds
	}
	owner := token.Owner()
	rows, err := tx.QueryContext(ctx, `SELECT job_id,object_kind,object_id,phase,work_revision,completed_revision,state,due_at FROM jobs WHERE tenant_id=?1 AND owner_id=?2 AND state <> 'done' AND scan_at <= ?3 AND lease_epoch < 9223372036854775807 ORDER BY scan_at,job_id LIMIT ?4`, owner.TenantID, owner.OwnerID, sqlTime(now), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []runtime.Job
	for rows.Next() {
		job := runtime.Job{Object: contract.ObjectRef{TenantID: owner.TenantID, OwnerID: owner.OwnerID}}
		if err = rows.Scan(&job.ID, &job.Object.Kind, &job.Object.ID, &job.Phase, &job.WorkRevision, &job.CompletedRevision, &job.State, &job.DueAt); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *Store) Claim(ctx context.Context, token runtime.Tx, candidate runtime.Job, worker string, now, until time.Time) (*runtime.Claim, error) {
	owner := contract.OwnerRef{TenantID: candidate.Object.TenantID, OwnerID: candidate.Object.OwnerID}
	tx, err := s.token(ctx, token, owner)
	if err != nil {
		return nil, err
	}
	if worker == "" || len(worker) > 128 || !until.After(now) || until.Sub(now) > 5*time.Minute || candidate.Object.Revision != nil {
		return nil, runtime.ErrWorkBounds
	}
	claim := runtime.Claim{JobID: candidate.ID, Object: candidate.Object, Phase: candidate.Phase, Worker: worker}
	// The owner writer serializes this short conditional update with admission.
	err = tx.QueryRowContext(ctx, `UPDATE jobs SET state='leased',worker_id=?8,claimed_revision=work_revision,lease_epoch=lease_epoch+1,lease_until=?9 WHERE tenant_id=?1 AND owner_id=?2 AND job_id=?3 AND object_kind=?4 AND object_id=?5 AND phase=?6 AND due_at <= ?7 AND (state IN ('ready','waiting') OR (state='leased' AND lease_until <= ?7)) AND lease_epoch < ?10 RETURNING claimed_revision,lease_epoch,lease_until`, owner.TenantID, owner.OwnerID, candidate.ID, candidate.Object.Kind, candidate.Object.ID, candidate.Phase, sqlTime(now), worker, sqlTime(until), int64(math.MaxInt64)).Scan(&claim.ClaimedRevision, &claim.Epoch, &claim.LeaseUntil)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &claim, err
}

func validClaim(claim runtime.Claim) bool {
	return claim.JobID != "" && claim.Object.Revision == nil && claim.ClaimedRevision > 0 && claim.Epoch > 0 && claim.Worker != "" && len(claim.Worker) <= 128 && !claim.LeaseUntil.IsZero()
}

func (s *Store) Complete(ctx context.Context, token runtime.Tx, claim runtime.Claim, now time.Time) error {
	owner := contract.OwnerRef{TenantID: claim.Object.TenantID, OwnerID: claim.Object.OwnerID}
	tx, err := s.token(ctx, token, owner)
	if err != nil {
		return err
	}
	if !validClaim(claim) {
		return runtime.ErrClaim
	}
	result, err := tx.ExecContext(ctx, `UPDATE jobs SET completed_revision=?7,state=CASE WHEN work_revision>?7 THEN 'ready' ELSE 'done' END,claimed_revision=NULL,worker_id=NULL,lease_until=NULL WHERE tenant_id=?1 AND owner_id=?2 AND job_id=?3 AND object_kind=?4 AND object_id=?5 AND phase=?6 AND state='leased' AND claimed_revision=?7 AND lease_epoch=?8 AND worker_id=?9 AND lease_until=?10 AND lease_until>?11`, owner.TenantID, owner.OwnerID, claim.JobID, claim.Object.Kind, claim.Object.ID, claim.Phase, claim.ClaimedRevision, claim.Epoch, claim.Worker, sqlTime(claim.LeaseUntil), sqlTime(now))
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count != 1 {
		return runtime.ErrClaim
	}
	return err
}

func (s *Store) SaveProjection(ctx context.Context, token runtime.Tx, owner contract.OwnerRef, id contract.ID, projection demo.Projection) error {
	tx, err := s.token(ctx, token, owner)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE durable_inputs SET projected_revision=?4,text_digest=?5 WHERE tenant_id=?1 AND owner_id=?2 AND object_id=?3 AND revision >= ?4 AND (projected_revision IS NULL OR projected_revision < ?4)`, owner.TenantID, owner.OwnerID, id, projection.InputRevision, projection.TextDigest)
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
	owner := contract.OwnerRef{TenantID: claim.Object.TenantID, OwnerID: claim.Object.OwnerID}
	tx, err := s.token(ctx, token, owner)
	if err != nil {
		return runtime.Claim{}, err
	}
	if !validClaim(claim) {
		return runtime.Claim{}, runtime.ErrClaim
	}
	if !until.After(now) || until.Sub(now) > 5*time.Minute {
		return runtime.Claim{}, runtime.ErrWorkBounds
	}
	renewed := claim
	err = tx.QueryRowContext(ctx, `UPDATE jobs SET lease_until=?12 WHERE tenant_id=?1 AND owner_id=?2 AND job_id=?3 AND object_kind=?4 AND object_id=?5 AND phase=?6 AND state='leased' AND claimed_revision=?7 AND lease_epoch=?8 AND worker_id=?9 AND lease_until=?10 AND lease_until>?11 AND lease_until <= ?12 RETURNING lease_until`, owner.TenantID, owner.OwnerID, claim.JobID, claim.Object.Kind, claim.Object.ID, claim.Phase, claim.ClaimedRevision, claim.Epoch, claim.Worker, sqlTime(claim.LeaseUntil), sqlTime(now), sqlTime(until)).Scan(&renewed.LeaseUntil)
	if errors.Is(err, sql.ErrNoRows) {
		return runtime.Claim{}, runtime.ErrClaim
	}
	return renewed, err
}

// sqlTime keeps every SQLite time operand in the immutable v1 UTC encoding.
// Text comparison then preserves nanosecond precision without floating point.
func sqlTime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000000000Z") }
