package pgstore

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"github.com/ruipengliu/lerna/contract"
	"github.com/ruipengliu/lerna/runtime"
	"math"
	"time"
)

// Job mechanics operate only after the consumer locks its own business object.
// They fence the original worker, epoch, revision, identity, and finite lease.
func (s *Core) Scan(ctx context.Context, token runtime.Tx, now time.Time, limit int) ([]runtime.Job, error) {
	tx, err := s.LocalSQL(ctx, token)
	if err != nil {
		return nil, err
	}
	if limit < 1 || limit > 64 {
		return nil, runtime.ErrWorkBounds
	}
	owner := token.Owner()
	rows, err := tx.QueryContext(ctx, `SELECT job_id,object_kind,object_id,phase,work_revision,completed_revision,state,due_at FROM `+s.Table("jobs")+` WHERE tenant_id=$1 AND owner_id=$2 AND state <> 'done' AND scan_at <= $3 AND lease_epoch < 9223372036854775807 ORDER BY scan_at,job_id LIMIT $4`, owner.TenantID, owner.OwnerID, now, limit)
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

func (s *Core) Claim(ctx context.Context, token runtime.Tx, candidate runtime.Job, worker string, now, until time.Time) (*runtime.Claim, error) {
	owner := contract.OwnerRef{TenantID: candidate.Object.TenantID, OwnerID: candidate.Object.OwnerID}
	tx, err := s.SQL(ctx, token, owner)
	if err != nil {
		return nil, err
	}
	if worker == "" || len(worker) > 128 || !until.After(now) || until.Sub(now) > 5*time.Minute || candidate.Object.Revision != nil {
		return nil, runtime.ErrWorkBounds
	}
	claim := runtime.Claim{JobID: candidate.ID, Object: candidate.Object, Phase: candidate.Phase, Worker: worker}
	// This row lock comes only after the consumer object/input lock. SKIP LOCKED
	// bounds contention even when another storage consumer holds a Job row.
	err = tx.QueryRowContext(ctx, `WITH eligible AS (SELECT job_id FROM `+s.Table("jobs")+` WHERE tenant_id=$1 AND owner_id=$2 AND job_id=$3 AND object_kind=$4 AND object_id=$5 AND phase=$6 AND due_at <= $7 AND (state IN ('ready','waiting') OR (state='leased' AND lease_until <= $7)) AND lease_epoch < $10 FOR UPDATE SKIP LOCKED) UPDATE `+s.Table("jobs")+` j SET state='leased',worker_id=$8,claimed_revision=j.work_revision,lease_epoch=j.lease_epoch+1,lease_until=$9 FROM eligible e WHERE j.tenant_id=$1 AND j.owner_id=$2 AND j.job_id=e.job_id RETURNING j.claimed_revision,j.lease_epoch,j.lease_until`, owner.TenantID, owner.OwnerID, candidate.ID, candidate.Object.Kind, candidate.Object.ID, candidate.Phase, now, worker, until, int64(math.MaxInt64)).Scan(&claim.ClaimedRevision, &claim.Epoch, &claim.LeaseUntil)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &claim, err
}

func validClaim(claim runtime.Claim) bool {
	return claim.JobID != "" && claim.Object.Revision == nil && claim.ClaimedRevision > 0 && claim.Epoch > 0 && claim.Worker != "" && len(claim.Worker) <= 128 && !claim.LeaseUntil.IsZero()
}

func (s *Core) Complete(ctx context.Context, token runtime.Tx, claim runtime.Claim, now time.Time) error {
	owner := contract.OwnerRef{TenantID: claim.Object.TenantID, OwnerID: claim.Object.OwnerID}
	tx, err := s.SQL(ctx, token, owner)
	if err != nil {
		return err
	}
	if !validClaim(claim) {
		return runtime.ErrClaim
	}
	result, err := tx.ExecContext(ctx, `UPDATE `+s.Table("jobs")+` SET completed_revision=$7,state=CASE WHEN work_revision>$7 THEN 'ready' ELSE 'done' END,claimed_revision=NULL,worker_id=NULL,lease_until=NULL,pool_claim_epoch=NULL WHERE tenant_id=$1 AND owner_id=$2 AND job_id=$3 AND object_kind=$4 AND object_id=$5 AND phase=$6 AND state='leased' AND claimed_revision=$7 AND lease_epoch=$8 AND worker_id=$9 AND lease_until=$10 AND lease_until>$11`, owner.TenantID, owner.OwnerID, claim.JobID, claim.Object.Kind, claim.Object.ID, claim.Phase, claim.ClaimedRevision, claim.Epoch, claim.Worker, claim.LeaseUntil, now)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count != 1 {
		return runtime.ErrClaim
	}
	return err
}

func (s *Core) Renew(ctx context.Context, token runtime.Tx, claim runtime.Claim, now, until time.Time) (runtime.Claim, error) {
	owner := contract.OwnerRef{TenantID: claim.Object.TenantID, OwnerID: claim.Object.OwnerID}
	tx, err := s.SQL(ctx, token, owner)
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
	err = tx.QueryRowContext(ctx, `UPDATE `+s.Table("jobs")+` SET lease_until=$12 WHERE tenant_id=$1 AND owner_id=$2 AND job_id=$3 AND object_kind=$4 AND object_id=$5 AND phase=$6 AND state='leased' AND claimed_revision=$7 AND lease_epoch=$8 AND worker_id=$9 AND lease_until=$10 AND lease_until>$11 AND lease_until <= $12 RETURNING lease_until`, owner.TenantID, owner.OwnerID, claim.JobID, claim.Object.Kind, claim.Object.ID, claim.Phase, claim.ClaimedRevision, claim.Epoch, claim.Worker, claim.LeaseUntil, now, until).Scan(&renewed.LeaseUntil)
	if errors.Is(err, sql.ErrNoRows) {
		return runtime.Claim{}, runtime.ErrClaim
	}
	return renewed, err
}
func (s *Core) Trigger(ctx context.Context, token runtime.Tx, object contract.ObjectRef, phase string, revision int64, due time.Time) (runtime.Job, error) {
	var job runtime.Job
	owner := contract.OwnerRef{TenantID: object.TenantID, OwnerID: object.OwnerID}
	tx, err := s.SQL(ctx, token, owner)
	if err != nil {
		return job, err
	}
	if revision <= 0 {
		return job, runtime.ErrWorkBounds
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return job, err
	}
	job.Object = object
	job.Object.Revision = nil
	job.Phase = phase
	err = tx.QueryRowContext(ctx, `INSERT INTO `+s.Table("jobs")+`(tenant_id,owner_id,job_id,object_kind,object_id,phase,work_revision,state,due_at) VALUES($1,$2,$3,$4,$5,$6,$7,'ready',$8) ON CONFLICT(tenant_id,owner_id,object_kind,object_id,phase) DO UPDATE SET work_revision=EXCLUDED.work_revision,due_at=EXCLUDED.due_at,state=CASE WHEN `+s.Table("jobs")+`.state='leased' THEN 'leased' ELSE 'ready' END WHERE EXCLUDED.work_revision > `+s.Table("jobs")+`.work_revision RETURNING job_id,work_revision,completed_revision,state,due_at`, owner.TenantID, owner.OwnerID, "job-"+hex.EncodeToString(nonce[:]), object.Kind, object.ID, phase, revision, due).Scan(&job.ID, &job.WorkRevision, &job.CompletedRevision, &job.State, &job.DueAt)
	if errors.Is(err, sql.ErrNoRows) {
		return job, runtime.ErrWorkBounds
	}
	return job, err
}
func (s *Core) ClaimActive(ctx context.Context, token runtime.Tx, job runtime.Job, now time.Time) (bool, error) {
	owner := contract.OwnerRef{TenantID: job.Object.TenantID, OwnerID: job.Object.OwnerID}
	tx, err := s.SQL(ctx, token, owner)
	if err != nil {
		return false, err
	}
	var active bool
	err = tx.QueryRowContext(ctx, `SELECT state='leased' AND lease_until>$4 FROM `+s.Table("jobs")+` WHERE tenant_id=$1 AND owner_id=$2 AND job_id=$3`, owner.TenantID, owner.OwnerID, job.ID, (now)).Scan(&active)
	return active, err
}
func (s *Core) ValidateClaim(ctx context.Context, token runtime.Tx, claim runtime.Claim, now time.Time) error {
	owner := contract.OwnerRef{TenantID: claim.Object.TenantID, OwnerID: claim.Object.OwnerID}
	tx, err := s.SQL(ctx, token, owner)
	if err != nil {
		return err
	}
	if !validClaim(claim) {
		return runtime.ErrClaim
	}
	var found int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM `+s.Table("jobs")+` WHERE tenant_id=$1 AND owner_id=$2 AND job_id=$3 AND object_kind=$4 AND object_id=$5 AND phase=$6 AND state='leased' AND claimed_revision=$7 AND lease_epoch=$8 AND worker_id=$9 AND lease_until=$10 AND lease_until>$11 FOR UPDATE`, owner.TenantID, owner.OwnerID, claim.JobID, claim.Object.Kind, claim.Object.ID, claim.Phase, claim.ClaimedRevision, claim.Epoch, claim.Worker, (claim.LeaseUntil), (now)).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return runtime.ErrClaim
	}
	return err
}
func (s *Core) DeferClaim(ctx context.Context, token runtime.Tx, claim runtime.Claim, now, due time.Time) error {
	if !due.After(now) {
		return runtime.ErrWorkBounds
	}
	if err := s.ValidateClaim(ctx, token, claim, now); err != nil {
		return err
	}
	owner := contract.OwnerRef{TenantID: claim.Object.TenantID, OwnerID: claim.Object.OwnerID}
	tx, err := s.SQL(ctx, token, owner)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE `+s.Table("jobs")+` SET state=CASE WHEN work_revision>$4 THEN 'ready' ELSE 'waiting' END,due_at=CASE WHEN work_revision>$4 THEN due_at ELSE $5 END,claimed_revision=NULL,worker_id=NULL,lease_until=NULL,pool_claim_epoch=NULL WHERE tenant_id=$1 AND owner_id=$2 AND job_id=$3`, owner.TenantID, owner.OwnerID, claim.JobID, claim.ClaimedRevision, (due))
	return err
}
func (s *Core) ReleaseClaim(ctx context.Context, token runtime.Tx, claim runtime.Claim, now time.Time) error {
	if err := s.ValidateClaim(ctx, token, claim, now); err != nil {
		return err
	}
	owner := contract.OwnerRef{TenantID: claim.Object.TenantID, OwnerID: claim.Object.OwnerID}
	tx, err := s.SQL(ctx, token, owner)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE `+s.Table("jobs")+` SET state='ready',claimed_revision=NULL,worker_id=NULL,lease_until=NULL,pool_claim_epoch=NULL WHERE tenant_id=$1 AND owner_id=$2 AND job_id=$3`, owner.TenantID, owner.OwnerID, claim.JobID)
	return err
}
