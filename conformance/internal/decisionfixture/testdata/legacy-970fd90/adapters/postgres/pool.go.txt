package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/ruipengliu/lerna/contract"
	demo "github.com/ruipengliu/lerna/internal/durableworkdemo"
	"github.com/ruipengliu/lerna/runtime"
)

func (s *Store) poolCoordination(ctx context.Context, token runtime.Tx) (*sql.Tx, error) {
	return s.core.PoolCoordination(ctx, token)
}
func (s *Store) LockPool(ctx context.Context, token runtime.Tx) (demo.PoolState, error) {
	return s.core.LockPool(ctx, token)
}
func (s *Store) InstallPool(ctx context.Context, token runtime.Tx, cfg demo.PoolConfig, expected int64, now time.Time) error {
	return s.core.InstallPool(ctx, token, cfg, expected, now)
}
func (s *Store) SavePoolCursor(ctx context.Context, token runtime.Tx, state demo.PoolState, lane string, cursor demo.PoolCursor) error {
	return s.core.SavePoolCursor(ctx, token, state, lane, cursor)
}
func (s *Store) PoolCounts(ctx context.Context, token runtime.Tx, state demo.PoolState, lane string, tenant contract.ID, now time.Time) (int64, int64, int64, error) {
	return s.core.PoolCounts(ctx, token, state, lane, tenant, now)
}
func (s *Store) PoolQueue(ctx context.Context, token runtime.Tx, state demo.PoolState, id contract.ID, lane string) (string, bool, error) {
	if _, err := s.localToken(ctx, token); err != nil {
		return "", false, err
	}
	return s.core.PoolQueue(ctx, token, state, contract.ObjectRef{TenantID: token.Owner().TenantID, OwnerID: token.Owner().OwnerID, Kind: "durable_work", ID: id}, "project", lane)
}
func (s *Store) SetJobLane(ctx context.Context, token runtime.Tx, id contract.ID, lane string) error {
	if _, err := s.localToken(ctx, token); err != nil {
		return err
	}
	return s.core.SetJobLane(ctx, token, contract.ObjectRef{TenantID: token.Owner().TenantID, OwnerID: token.Owner().OwnerID, Kind: "durable_work", ID: id}, "project", lane)
}
func (s *Store) PoolPage(ctx context.Context, token runtime.Tx, state demo.PoolState, lane string, tenant contract.ID, after, through string, now time.Time) ([]runtime.Job, string, error) {
	return s.core.PoolPage(ctx, token, state, lane, tenant, after, through, now)
}
func (s *Store) RegisterPoolClaim(ctx context.Context, token runtime.Tx, state demo.PoolState, claim runtime.Claim) error {
	return s.core.RegisterPoolClaim(ctx, token, state, claim)
}
func (s *Store) ValidatePoolClaim(ctx context.Context, token runtime.Tx, state demo.PoolState, claim runtime.Claim, now time.Time) error {
	return s.core.ValidatePoolClaim(ctx, token, state, claim, now)
}
func (s *Store) PoolReadyTenants(ctx context.Context, token runtime.Tx, state demo.PoolState, lane string, now time.Time) (map[contract.ID]time.Time, error) {
	return s.core.PoolReadyTenants(ctx, token, state, lane, now)
}
func (s *Store) PoolScope(ctx context.Context, token runtime.Tx) (string, error) {
	return s.core.PoolScope(ctx, token)
}
func (s *Store) ObservePool(ctx context.Context, owner contract.OwnerRef, clock runtime.Clock) (demo.PoolObservation, error) {
	result := demo.PoolObservation{Active: map[string]int64{}, Queued: map[string]int64{}}
	err := s.Within(ctx, owner, func(ctx context.Context, tx runtime.Tx) error {
		state, err := s.LockPool(ctx, tx)
		if err != nil {
			return err
		}
		result.State = state
		result.Scope, err = s.PoolScope(ctx, tx)
		if err != nil {
			return err
		}
		now, err := clock.Now(ctx, tx)
		if err != nil {
			return err
		}
		for _, l := range state.Config.Limits {
			active, _, queued, err := s.PoolCounts(ctx, tx, state, l.Lane, owner.TenantID, now)
			if err != nil {
				return err
			}
			result.Active[l.Lane] = active
			result.Queued[l.Lane] = queued
		}
		return nil
	})
	return result, err
}
func (s *Store) MaintenancePage(ctx context.Context, token runtime.Tx, after, through string) ([]runtime.Job, string, error) {
	tx, err := s.localToken(ctx, token)
	if err != nil {
		return nil, "", err
	}
	owner := token.Owner()
	if through == "" {
		err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(job_id),'') FROM `+s.table("jobs")+` WHERE tenant_id=$1 AND owner_id=$2 AND state<>'done'`, owner.TenantID, owner.OwnerID).Scan(&through)
		if err != nil {
			return nil, "", err
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT job_id,object_kind,object_id,phase,work_revision,completed_revision,state FROM `+s.table("jobs")+` WHERE tenant_id=$1 AND owner_id=$2 AND state<>'done' AND job_id>$3 AND job_id<=$4 ORDER BY job_id LIMIT 64`, owner.TenantID, owner.OwnerID, after, through)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	var jobs []runtime.Job
	for rows.Next() {
		job := runtime.Job{Object: contract.ObjectRef{TenantID: owner.TenantID, OwnerID: owner.OwnerID}}
		if err = rows.Scan(&job.ID, &job.Object.Kind, &job.Object.ID, &job.Phase, &job.WorkRevision, &job.CompletedRevision, &job.State); err != nil {
			return nil, "", err
		}
		jobs = append(jobs, job)
	}
	return jobs, through, rows.Err()
}
func (s *Store) LockMaintenanceJob(ctx context.Context, token runtime.Tx, job runtime.Job) (*demo.MaintenanceJob, error) {
	tx, err := s.localToken(ctx, token)
	if err != nil {
		return nil, err
	}
	var claimed sql.NullInt64
	var lease sql.NullTime
	result := demo.MaintenanceJob{Job: job}
	err = tx.QueryRowContext(ctx, `SELECT work_revision,completed_revision,state,claimed_revision,lease_until FROM `+s.table("jobs")+` WHERE tenant_id=$1 AND owner_id=$2 AND job_id=$3 FOR UPDATE SKIP LOCKED`, token.Owner().TenantID, token.Owner().OwnerID, job.ID).Scan(&result.Job.WorkRevision, &result.Job.CompletedRevision, &result.Job.State, &claimed, &lease)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	result.ClaimedRevision = claimed.Int64
	if lease.Valid {
		result.LeaseUntil = lease.Time
	}
	return &result, nil
}
func (s *Store) ClosePoolRevision(ctx context.Context, token runtime.Tx, job runtime.Job, revision int64, now time.Time) (bool, error) {
	tx, err := s.localToken(ctx, token)
	if err != nil {
		return false, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE `+s.table("jobs")+` SET completed_revision=$4,state=CASE WHEN work_revision>$4 THEN 'ready' ELSE 'done' END,claimed_revision=NULL,worker_id=NULL,lease_until=NULL,pool_claim_epoch=NULL WHERE tenant_id=$1 AND owner_id=$2 AND job_id=$3 AND completed_revision<$4 AND work_revision>=$4 AND (claimed_revision IS NULL OR claimed_revision=$4 OR lease_until<=$5)`, token.Owner().TenantID, token.Owner().OwnerID, job.ID, revision, now)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

func (s *Store) PoolNextWake(ctx context.Context, token runtime.Tx, state demo.PoolState, lane string, now, fallback time.Time) (time.Time, error) {
	tx, err := s.localToken(ctx, token)
	if err != nil {
		return time.Time{}, err
	}
	if _, err = state.Config.Limit(lane); err != nil {
		return time.Time{}, err
	}
	if !fallback.After(now) || fallback.Sub(now) > time.Second {
		return time.Time{}, runtime.ErrWorkBounds
	}
	var next sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT MIN(boundary) FROM (
 SELECT j.scan_at AS boundary FROM `+s.table("jobs")+` j JOIN `+s.table("durable_pool_members")+` m ON m.tenant_id=j.tenant_id AND m.owner_id=j.owner_id
 WHERE m.pool_id=$1 AND j.lane=$2 AND j.state<>'done' AND j.scan_at>$3 AND j.lease_epoch<9223372036854775807
 UNION ALL SELECT s.deadline FROM `+s.table("durable_schedules")+` s JOIN `+s.table("jobs")+` j ON j.tenant_id=s.tenant_id AND j.owner_id=s.owner_id AND j.object_id=s.object_id
 JOIN `+s.table("durable_pool_members")+` m ON m.tenant_id=j.tenant_id AND m.owner_id=j.owner_id
 WHERE m.pool_id=$1 AND j.lane=$2 AND j.object_kind='durable_work' AND j.phase='project' AND j.state<>'done'
 AND (s.input_revision=j.work_revision OR (j.state='leased' AND s.input_revision=j.claimed_revision))
 AND s.input_revision>j.completed_revision AND s.deadline>$3 AND s.outcome NOT IN ('success','permanent','expired','stopped')
 ) wake`, state.Config.ID, lane, now).Scan(&next)
	if err != nil {
		return time.Time{}, err
	}
	if next.Valid {
		due := next.Time
		if due.Before(fallback) {
			return due, nil
		}
	}
	return fallback, nil
}
