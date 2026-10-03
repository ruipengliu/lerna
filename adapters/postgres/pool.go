package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/ruipengliu/lerna/contract"
	demo "github.com/ruipengliu/lerna/internal/durableworkdemo"
	"github.com/ruipengliu/lerna/runtime"
	"math"
	"strings"
	"time"
)

func (s *Store) poolCoordination(ctx context.Context, token runtime.Tx) (*sql.Tx, error) {
	tx, err := s.localToken(ctx, token)
	if err != nil {
		return nil, err
	}
	t := token.(*transaction)
	if !t.poolLocked {
		if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(3,hashtext($1))`, s.config.Schema+":durable-pool-registry"); err != nil {
			return nil, err
		}
		t.poolLocked = true
	}
	return tx, nil
}
func (s *Store) LockPool(ctx context.Context, token runtime.Tx) (demo.PoolState, error) {
	tx, err := s.poolCoordination(ctx, token)
	if err != nil {
		return demo.PoolState{}, err
	}
	t := token.(*transaction)
	if t.pool != nil {
		return *t.pool, nil
	}
	var cfg, cursors string
	err = tx.QueryRowContext(ctx, `SELECT p.configuration,p.cursors FROM `+s.table("durable_pools")+` p JOIN `+s.table("durable_pool_members")+` m ON m.pool_id=p.pool_id WHERE m.tenant_id=$1 AND m.owner_id=$2`, token.Owner().TenantID, token.Owner().OwnerID).Scan(&cfg, &cursors)
	if errors.Is(err, sql.ErrNoRows) {
		return demo.PoolState{}, demo.ErrPoolMissing
	}
	if err != nil {
		return demo.PoolState{}, err
	}
	var state demo.PoolState
	if err = json.Unmarshal([]byte(cfg), &state.Config); err != nil {
		return state, err
	}
	if err = state.Config.Validate(); err != nil {
		return state, err
	}
	if err = json.Unmarshal([]byte(cursors), &state.Cursors); err != nil {
		return state, err
	}
	if state.Cursors == nil {
		return state, demo.ErrPoolConfig
	}
	t.pool = &state
	return state, nil
}
func (s *Store) InstallPool(ctx context.Context, token runtime.Tx, cfg demo.PoolConfig, expected int64, now time.Time) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if expected < 0 || expected == math.MaxInt64 {
		return demo.ErrPoolConfig
	}
	tx, err := s.poolCoordination(ctx, token)
	if err != nil {
		return err
	}
	var old demo.PoolConfig
	var data, cursors string
	err = tx.QueryRowContext(ctx, `SELECT configuration,cursors FROM `+s.table("durable_pools")+` WHERE pool_id=$1`, cfg.ID).Scan(&data, &cursors)
	if errors.Is(err, sql.ErrNoRows) {
		if expected != 0 {
			return demo.ErrPoolConfig
		}
		cursors = `{}`
	} else if err != nil {
		return err
	} else {
		if err = json.Unmarshal([]byte(data), &old); err != nil {
			return err
		}
		if old.Revision != expected {
			return demo.ErrPoolConfig
		}
	}
	// Bound persisted metadata to current declared members while preserving
	// surviving tenant FIFO identities and success sequence across config edits.
	var saved map[string]demo.PoolCursor
	if err = json.Unmarshal([]byte(cursors), &saved); err != nil {
		return err
	}
	if saved == nil {
		saved = map[string]demo.PoolCursor{}
	}
	allowedKeys := map[string]bool{"ordinary": true, "control": true, "reconciliation": true, "maintenance-members": true}
	for _, member := range cfg.Members {
		allowedKeys["maintenance:"+string(member.TenantID)+"/"+string(member.OwnerID)] = true
	}
	for key := range saved {
		if !allowedKeys[key] {
			delete(saved, key)
		}
	}
	cursorBytes, err := json.Marshal(saved)
	if err != nil {
		return err
	}
	cursors = string(cursorBytes)
	cfg.Revision = expected + 1
	for _, m := range cfg.Members {
		var pool string
		err = tx.QueryRowContext(ctx, `SELECT pool_id FROM `+s.table("durable_pool_members")+` WHERE tenant_id=$1 AND owner_id=$2`, m.TenantID, m.OwnerID).Scan(&pool)
		if err == nil && pool != string(cfg.ID) {
			return demo.ErrPoolConfig
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	for _, m := range old.Members {
		keep := false
		for _, n := range cfg.Members {
			if m == n {
				keep = true
			}
		}
		if !keep {
			var count int64
			err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+s.table("jobs")+` WHERE tenant_id=$1 AND owner_id=$2 AND state<>'done'`, m.TenantID, m.OwnerID).Scan(&count)
			if err != nil {
				return err
			}
			if count != 0 {
				return demo.ErrPoolCapacity
			}
		}
	}
	// Count actual pre-existing Claims before changing membership; never start
	// an empty counter on upgrade or permit an unsafe concurrency decrease.
	for _, l := range cfg.Limits {
		var total int64
		tenantCounts := map[contract.ID]int64{}
		for _, m := range cfg.Members {
			var count int64
			err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+s.table("jobs")+` WHERE tenant_id=$1 AND owner_id=$2 AND lane=$3 AND state='leased' AND lease_until>$4`, m.TenantID, m.OwnerID, l.Lane, now).Scan(&count)
			if err != nil {
				return err
			}
			total += count
			tenantCounts[m.TenantID] += count
		}
		if total > l.Concurrent {
			return demo.ErrPoolCapacity
		}
		for tenant, count := range tenantCounts {
			if count > cfg.Quota(tenant, l.Lane) {
				return demo.ErrPoolCapacity
			}
		}
	}
	encoded, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO `+s.table("durable_pools")+`(pool_id,configuration,cursors)VALUES($1,$2,$3)ON CONFLICT(pool_id)DO UPDATE SET configuration=excluded.configuration,cursors=excluded.cursors`, cfg.ID, string(encoded), cursors)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM `+s.table("durable_pool_members")+` WHERE pool_id=$1`, cfg.ID)
	if err != nil {
		return err
	}
	for _, m := range cfg.Members {
		_, err = tx.ExecContext(ctx, `INSERT INTO `+s.table("durable_pool_members")+`(tenant_id,owner_id,pool_id)VALUES($1,$2,$3)`, m.TenantID, m.OwnerID, cfg.ID)
		if err != nil {
			return err
		}
		// Trusted first registration adopts legacy finite-lease reservations.
		// Updating an existing member must not bless a raw, unregistered Claim.
		alreadyMember := false
		for _, oldMember := range old.Members {
			if oldMember == m {
				alreadyMember = true
			}
		}
		if alreadyMember {
			continue
		}
		_, err = tx.ExecContext(ctx, `UPDATE `+s.table("jobs")+` SET pool_claim_epoch=lease_epoch WHERE tenant_id=$1 AND owner_id=$2 AND state='leased' AND lease_until>$3`, m.TenantID, m.OwnerID, now)
		if err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) SavePoolCursor(ctx context.Context, token runtime.Tx, state demo.PoolState, lane string, cursor demo.PoolCursor) error {
	tx, err := s.localToken(ctx, token)
	if err != nil {
		return err
	}
	state.Cursors[lane] = cursor
	data, err := json.Marshal(state.Cursors)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE `+s.table("durable_pools")+` SET cursors=$2 WHERE pool_id=$1`, state.Config.ID, string(data))
	if err == nil {
		token.(*transaction).pool = &state
	}
	return err
}
func (s *Store) PoolCounts(ctx context.Context, token runtime.Tx, state demo.PoolState, lane string, tenant contract.ID, now time.Time) (int64, int64, int64, error) {
	tx, err := s.localToken(ctx, token)
	if err != nil {
		return 0, 0, 0, err
	}
	var active, own, queued int64
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(CASE WHEN j.state='leased' AND j.lease_until>$3 THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN j.state='leased' AND j.lease_until>$3 AND j.tenant_id=$4 THEN 1 ELSE 0 END),0),COUNT(*) FROM `+s.table("jobs")+` j JOIN `+s.table("durable_pool_members")+` m ON m.tenant_id=j.tenant_id AND m.owner_id=j.owner_id WHERE m.pool_id=$1 AND j.lane=$2 AND j.state<>'done'`, state.Config.ID, lane, now, tenant).Scan(&active, &own, &queued)
	return active, own, queued, err
}
func (s *Store) PoolQueue(ctx context.Context, token runtime.Tx, state demo.PoolState, id contract.ID, lane string) (string, bool, error) {
	tx, err := s.localToken(ctx, token)
	if err != nil {
		return "", false, err
	}
	var existing, stateName string
	err = tx.QueryRowContext(ctx, `SELECT lane,state FROM `+s.table("jobs")+` WHERE tenant_id=$1 AND owner_id=$2 AND object_id=$3 AND phase='project'`, token.Owner().TenantID, token.Owner().OwnerID, id).Scan(&existing, &stateName)
	newJob := errors.Is(err, sql.ErrNoRows)
	if err != nil && !newJob {
		return "", false, err
	}
	if !newJob {
		lane = existing
		if stateName != "done" {
			return lane, false, nil
		}
	}
	limit, err := state.Config.Limit(lane)
	if err != nil {
		return "", false, err
	}
	_, _, queued, err := s.PoolCounts(ctx, token, state, lane, token.Owner().TenantID, time.Time{})
	if err != nil {
		return "", false, err
	}
	if queued >= limit.Queue {
		return "", false, demo.ErrPoolCapacity
	}
	return lane, newJob, nil
}
func (s *Store) SetJobLane(ctx context.Context, token runtime.Tx, id contract.ID, lane string) error {
	tx, err := s.localToken(ctx, token)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE `+s.table("jobs")+` SET lane=$4 WHERE tenant_id=$1 AND owner_id=$2 AND object_id=$3 AND phase='project'`, token.Owner().TenantID, token.Owner().OwnerID, id, lane)
	return err
}
func (s *Store) PoolPage(ctx context.Context, token runtime.Tx, state demo.PoolState, lane string, tenant contract.ID, after, through string, now time.Time) ([]runtime.Job, string, error) {
	tx, err := s.localToken(ctx, token)
	if err != nil {
		return nil, "", err
	}
	position := func(value string) (time.Time, string, error) {
		if value == "" {
			return time.Time{}, "", nil
		}
		parts := strings.SplitN(value, "|", 2)
		if len(parts) != 2 {
			return time.Time{}, "", demo.ErrPoolConfig
		}
		due, err := time.Parse(time.RFC3339Nano, parts[0])
		return due, parts[1], err
	}
	if through == "" {
		var id string
		var due time.Time
		err = tx.QueryRowContext(ctx, `SELECT j.due_at,j.job_id FROM `+s.table("jobs")+` j JOIN `+s.table("durable_pool_members")+` m ON m.tenant_id=j.tenant_id AND m.owner_id=j.owner_id WHERE m.pool_id=$1 AND j.lane=$2 AND j.tenant_id=$3 AND j.state<>'done' AND j.scan_at<=$4 ORDER BY j.due_at DESC,j.job_id DESC LIMIT 1`, state.Config.ID, lane, tenant, now).Scan(&due, &id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", nil
		}
		if err != nil {
			return nil, "", err
		}
		through = due.UTC().Format(time.RFC3339Nano) + "|" + id
	}
	afterDue, afterID, err := position(after)
	if err != nil {
		return nil, "", err
	}
	throughDue, throughID, err := position(through)
	if err != nil {
		return nil, "", err
	}
	rows, err := tx.QueryContext(ctx, `SELECT j.job_id,j.tenant_id,j.owner_id,j.object_kind,j.object_id,j.phase,j.work_revision,j.completed_revision,j.state,j.due_at FROM `+s.table("jobs")+` j JOIN `+s.table("durable_pool_members")+` m ON m.tenant_id=j.tenant_id AND m.owner_id=j.owner_id WHERE m.pool_id=$1 AND j.lane=$2 AND j.tenant_id=$3 AND (j.due_at>$4 OR (j.due_at=$4 AND j.job_id>$5)) AND (j.due_at<$6 OR (j.due_at=$6 AND j.job_id<=$7)) AND j.state<>'done' AND j.scan_at<=$8 AND j.lease_epoch<9223372036854775807 ORDER BY j.due_at,j.job_id LIMIT 64`, state.Config.ID, lane, tenant, afterDue, afterID, throughDue, throughID, now)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	var batch []runtime.Job
	for rows.Next() {
		var job runtime.Job
		var due time.Time
		if err = rows.Scan(&job.ID, &job.Object.TenantID, &job.Object.OwnerID, &job.Object.Kind, &job.Object.ID, &job.Phase, &job.WorkRevision, &job.CompletedRevision, &job.State, &due); err != nil {
			return nil, "", err
		}
		job.DueAt = due
		batch = append(batch, job)
	}
	return batch, through, rows.Err()
}
func (s *Store) RegisterPoolClaim(ctx context.Context, token runtime.Tx, state demo.PoolState, claim runtime.Claim) error {
	tx, err := s.localToken(ctx, token)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE `+s.table("jobs")+` SET pool_claim_epoch=$4 WHERE tenant_id=$1 AND owner_id=$2 AND job_id=$3 AND lease_epoch=$4 AND state='leased'`, token.Owner().TenantID, token.Owner().OwnerID, claim.JobID, claim.Epoch)
	return err
}
func (s *Store) ValidatePoolClaim(ctx context.Context, token runtime.Tx, state demo.PoolState, claim runtime.Claim, now time.Time) error {
	tx, err := s.localToken(ctx, token)
	if err != nil {
		return err
	}
	var lane string
	err = tx.QueryRowContext(ctx, `SELECT lane FROM `+s.table("jobs")+` WHERE tenant_id=$1 AND owner_id=$2 AND job_id=$3 AND pool_claim_epoch=$4 AND lease_epoch=$4 AND state='leased' AND lease_until>$5`, token.Owner().TenantID, token.Owner().OwnerID, claim.JobID, claim.Epoch, now).Scan(&lane)
	if errors.Is(err, sql.ErrNoRows) {
		return runtime.ErrClaim
	}
	if err != nil {
		return err
	}
	limits, err := state.Config.Limit(lane)
	if err != nil {
		return err
	}
	active, tenant, _, err := s.PoolCounts(ctx, token, state, lane, token.Owner().TenantID, now)
	if err != nil {
		return err
	}
	if active > limits.Concurrent || tenant > state.Config.Quota(token.Owner().TenantID, lane) {
		return demo.ErrPoolCapacity
	}
	return nil
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

func (s *Store) PoolReadyTenants(ctx context.Context, token runtime.Tx, state demo.PoolState, lane string, now time.Time) (map[contract.ID]time.Time, error) {
	tx, err := s.localToken(ctx, token)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT j.tenant_id,COALESCE(SUM(CASE WHEN j.state='leased' AND j.lease_until>$3 THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN j.scan_at<=$3 AND j.lease_epoch<9223372036854775807 THEN 1 ELSE 0 END),0),MIN(CASE WHEN j.scan_at<=$3 AND j.lease_epoch<9223372036854775807 THEN j.due_at ELSE NULL END) FROM `+s.table("jobs")+` j JOIN `+s.table("durable_pool_members")+` m ON m.tenant_id=j.tenant_id AND m.owner_id=j.owner_id WHERE m.pool_id=$1 AND j.lane=$2 AND j.state<>'done' GROUP BY j.tenant_id`, state.Config.ID, lane, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ready := map[contract.ID]time.Time{}
	for rows.Next() {
		var tenant contract.ID
		var active, candidates int64
		var due sql.NullTime
		if err = rows.Scan(&tenant, &active, &candidates, &due); err != nil {
			return nil, err
		}
		if candidates > 0 && active < state.Config.Quota(tenant, lane) {
			if !due.Valid {
				return nil, demo.ErrPoolConfig
			}
			ready[tenant] = due.Time
		}
	}
	return ready, rows.Err()
}

func (s *Store) PoolScope(ctx context.Context, token runtime.Tx) (string, error) {
	tx, err := s.localToken(ctx, token)
	if err != nil {
		return "", err
	}
	var scope string
	err = tx.QueryRowContext(ctx, `SELECT scope_id FROM `+s.table("durable_pool_scope")).Scan(&scope)
	if err != nil {
		return "", err
	}
	cfg, err := pgx.ParseConfig(s.config.DSN)
	if err != nil {
		return "", errors.New("PostgreSQL scope configuration rejected")
	}
	binding, err := json.Marshal([]any{"postgres", cfg.Host, cfg.Port, cfg.Database, s.config.Schema, scope})
	return string(binding), err
}

// PoolNextWake observes runtime scheduling boundaries across explicitly declared
// members, including both revisions while a newer input has a live old Claim.
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
