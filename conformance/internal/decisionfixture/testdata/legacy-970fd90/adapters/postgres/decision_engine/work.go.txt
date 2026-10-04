package decision_engine

import (
	"context"
	"database/sql"
	"time"

	"github.com/ruipengliu/lerna/contract"
	v "github.com/ruipengliu/lerna/contract/v1_1"
	"github.com/ruipengliu/lerna/runtime"
	"github.com/ruipengliu/lerna/runtime/workpool"
)

func (s *Store) Trigger(ctx context.Context, tx runtime.Tx, ref contract.ObjectRef, phase string, revision int64, now time.Time) (runtime.Job, error) {
	return s.core.Trigger(ctx, tx, ref, phase, revision, now)
}
func (s *Store) Scan(ctx context.Context, tx runtime.Tx, now time.Time, limit int) ([]runtime.Job, error) {
	return s.core.Scan(ctx, tx, now, limit)
}
func (s *Store) Claim(ctx context.Context, tx runtime.Tx, job runtime.Job, worker string, now, until time.Time) (*runtime.Claim, error) {
	return s.core.Claim(ctx, tx, job, worker, now, until)
}
func (s *Store) ValidateClaim(ctx context.Context, tx runtime.Tx, claim runtime.Claim, now time.Time) error {
	return s.core.ValidateClaim(ctx, tx, claim, now)
}
func (s *Store) Complete(ctx context.Context, tx runtime.Tx, claim runtime.Claim, now time.Time) error {
	record, err := s.ReadDecision(ctx, tx, v.DecisionRef{TenantID: v.ID(claim.Object.TenantID), OwnerID: v.ID(claim.Object.OwnerID), Kind: "decision", ID: v.ID(claim.Object.ID)})
	if err != nil {
		return err
	}
	if record == nil || record.StartedEpoch != claim.Epoch || (record.Status != "completed" && record.Status != "failed") {
		return runtime.ErrClaim
	}
	return s.core.Complete(ctx, tx, claim, now)
}
func (s *Store) Renew(ctx context.Context, tx runtime.Tx, claim runtime.Claim, now, until time.Time) (runtime.Claim, error) {
	return s.core.Renew(ctx, tx, claim, now, until)
}
func (s *Store) ReleaseClaim(ctx context.Context, tx runtime.Tx, claim runtime.Claim, now time.Time) error {
	return s.core.ReleaseClaim(ctx, tx, claim, now)
}
func (s *Store) DeferClaim(ctx context.Context, tx runtime.Tx, claim runtime.Claim, now, due time.Time) error {
	return s.core.DeferClaim(ctx, tx, claim, now, due)
}
func (s *Store) ClaimActive(ctx context.Context, tx runtime.Tx, job runtime.Job, now time.Time) (bool, error) {
	return s.core.ClaimActive(ctx, tx, job, now)
}
func (s *Store) StopRevision(ctx context.Context, token runtime.Tx, job runtime.Job, revision int64) error {
	tx, err := s.core.SQL(ctx, token, contract.OwnerRef{TenantID: job.Object.TenantID, OwnerID: job.Object.OwnerID})
	if err != nil {
		return err
	}
	if revision < 1 || revision > job.WorkRevision {
		return runtime.ErrWorkBounds
	}
	_, err = tx.ExecContext(ctx, `UPDATE `+s.table("jobs")+` SET completed_revision=$4,state=CASE WHEN work_revision>$4 THEN 'ready' ELSE 'done' END,claimed_revision=NULL,worker_id=NULL,lease_until=NULL,pool_claim_epoch=NULL WHERE tenant_id=$1 AND owner_id=$2 AND job_id=$3 AND object_kind='decision' AND phase='decide' AND completed_revision<$4 AND (claimed_revision IS NULL OR claimed_revision=$4)`, token.Owner().TenantID, token.Owner().OwnerID, job.ID, revision)
	return err
}
func (s *Store) LockPool(ctx context.Context, tx runtime.Tx) (workpool.State, error) {
	return s.core.LockPool(ctx, tx)
}
func (s *Store) InstallPool(ctx context.Context, tx runtime.Tx, cfg workpool.Config, expected int64, now time.Time) error {
	return s.core.InstallPool(ctx, tx, cfg, expected, now)
}
func (s *Store) PoolScope(ctx context.Context, tx runtime.Tx) (string, error) {
	return s.core.PoolScope(ctx, tx)
}
func (s *Store) SavePoolCursor(ctx context.Context, tx runtime.Tx, state workpool.State, lane string, cursor workpool.Cursor) error {
	return s.core.SavePoolCursor(ctx, tx, state, lane, cursor)
}
func (s *Store) PoolCounts(ctx context.Context, tx runtime.Tx, state workpool.State, lane string, tenant contract.ID, now time.Time) (int64, int64, int64, error) {
	return s.core.PoolCounts(ctx, tx, state, lane, tenant, now)
}
func (s *Store) PoolQueue(ctx context.Context, tx runtime.Tx, state workpool.State, ref contract.ObjectRef, phase, lane string) (string, bool, error) {
	return s.core.PoolQueue(ctx, tx, state, ref, phase, lane)
}
func (s *Store) PoolReadyTenants(ctx context.Context, tx runtime.Tx, state workpool.State, lane string, now time.Time) (map[contract.ID]time.Time, error) {
	return s.core.PoolReadyTenants(ctx, tx, state, lane, now)
}
func (s *Store) PoolPage(ctx context.Context, tx runtime.Tx, state workpool.State, lane string, tenant contract.ID, after, through string, now time.Time) ([]runtime.Job, string, error) {
	return s.core.PoolPage(ctx, tx, state, lane, tenant, after, through, now)
}
func (s *Store) RegisterPoolClaim(ctx context.Context, tx runtime.Tx, state workpool.State, claim runtime.Claim) error {
	return s.core.RegisterPoolClaim(ctx, tx, state, claim)
}
func (s *Store) ValidatePoolClaim(ctx context.Context, tx runtime.Tx, state workpool.State, claim runtime.Claim, now time.Time) error {
	return s.core.ValidatePoolClaim(ctx, tx, state, claim, now)
}
func (s *Store) NextWake(ctx context.Context, token runtime.Tx, now, fallback time.Time) (time.Time, error) {
	state, err := s.LockPool(ctx, token)
	if err != nil {
		return time.Time{}, err
	}
	return s.PoolNextWake(ctx, token, state, "ordinary", now, fallback)
}
func (s *Store) PoolNextWake(ctx context.Context, token runtime.Tx, state workpool.State, lane string, now, fallback time.Time) (time.Time, error) {
	tx, err := s.core.LocalSQL(ctx, token)
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
	err = tx.QueryRowContext(ctx, `SELECT MIN(boundary) FROM(
 SELECT j.scan_at AS boundary FROM `+s.table("jobs")+` j JOIN `+s.table("durable_pool_members")+` m USING(tenant_id,owner_id) WHERE m.pool_id=$1 AND j.lane=$2 AND j.state<>'done' AND j.scan_at>$3 AND j.lease_epoch<9223372036854775807
 UNION ALL SELECT d.deadline FROM `+s.table("decisions")+` d JOIN `+s.table("jobs")+` j ON j.tenant_id=d.tenant_id AND j.owner_id=d.owner_id AND j.object_id=d.decision_id JOIN `+s.table("durable_pool_members")+` m ON m.tenant_id=j.tenant_id AND m.owner_id=j.owner_id WHERE m.pool_id=$1 AND j.lane=$2 AND j.state<>'done' AND d.status IN('accepted','running','waiting') AND d.deadline>$3
 )wake`, state.Config.ID, lane, now).Scan(&next)
	if err != nil {
		return time.Time{}, err
	}
	if next.Valid && next.Time.Before(fallback) {
		return next.Time, nil
	}
	return fallback, nil
}
func (s *Store) ExpiredCandidates(ctx context.Context, token runtime.Tx, now time.Time, limit int) ([]runtime.Job, error) {
	tx, err := s.core.LocalSQL(ctx, token)
	if err != nil {
		return nil, err
	}
	if limit < 1 || limit > 64 {
		return nil, runtime.ErrWorkBounds
	}
	rows, err := tx.QueryContext(ctx, `SELECT j.job_id,j.object_kind,j.object_id,j.phase,j.work_revision,j.completed_revision,j.state,j.due_at FROM `+s.table("jobs")+` j JOIN `+s.table("decisions")+` d ON d.tenant_id=j.tenant_id AND d.owner_id=j.owner_id AND d.decision_id=j.object_id WHERE j.tenant_id=$1 AND j.owner_id=$2 AND j.state<>'done' AND d.status IN('accepted','running','waiting') AND d.deadline<=$3 ORDER BY d.deadline,j.job_id LIMIT $4`, token.Owner().TenantID, token.Owner().OwnerID, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []runtime.Job
	for rows.Next() {
		job := runtime.Job{Object: contract.ObjectRef{TenantID: token.Owner().TenantID, OwnerID: token.Owner().OwnerID}}
		if err = rows.Scan(&job.ID, &job.Object.Kind, &job.Object.ID, &job.Phase, &job.WorkRevision, &job.CompletedRevision, &job.State, &job.DueAt); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}
