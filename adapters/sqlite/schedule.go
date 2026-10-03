package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/ruipengliu/lerna/contract"
	demo "github.com/ruipengliu/lerna/internal/durableworkdemo"
	"github.com/ruipengliu/lerna/runtime"
	"time"
)

func (s *Store) BindSchedule(ctx context.Context, token runtime.Tx, owner contract.OwnerRef, id contract.ID, revision int64, state demo.ScheduleState) error {
	tx, err := s.token(ctx, token, owner)
	if err != nil {
		return err
	}
	if err = state.Policy.Validate(); err != nil {
		return err
	}
	if revision < 1 || !state.Deadline.After(state.AdoptedAt) {
		return runtime.ErrWorkBounds
	}
	// A Job's first lane is immutable, including later input revisions.
	var original string
	err = tx.QueryRowContext(ctx, `SELECT policy FROM durable_schedules WHERE tenant_id=?1 AND owner_id=?2 AND object_id=?3 ORDER BY input_revision LIMIT 1`, owner.TenantID, owner.OwnerID, id).Scan(&original)
	if err == nil {
		var policy demo.Policy
		if err = json.Unmarshal([]byte(original), &policy); err != nil {
			return err
		}
		state.Policy.Lane = policy.Lane
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if state.Policy.Gate != nil {
		_, err = tx.ExecContext(ctx, `INSERT INTO durable_gates(tenant_id,owner_id,gate_id,revision)VALUES(?1,?2,?3,0) ON CONFLICT(tenant_id,owner_id,gate_id)DO NOTHING`, owner.TenantID, owner.OwnerID, state.Policy.Gate.ID)
		if err != nil {
			return err
		}
	}
	policy, err := json.Marshal(state.Policy)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO durable_schedules(tenant_id,owner_id,object_id,input_revision,policy,binding_source,adopted_at,deadline,attempts,start_epoch,outcome,reason,due_at,stopped)VALUES(?1,?2,?3,?4,?5,?9,?6,?7,0,0,'','',?8,0) ON CONFLICT(tenant_id,owner_id,object_id,input_revision) DO NOTHING`, owner.TenantID, owner.OwnerID, id, revision, string(policy), sqlTime(state.AdoptedAt), sqlTime(state.Deadline), sqlTime(state.Due), state.Source)
	return err
}
func (s *Store) LoadSchedule(ctx context.Context, token runtime.Tx, owner contract.OwnerRef, id contract.ID, revision int64) (*demo.ScheduleState, error) {
	tx, err := s.token(ctx, token, owner)
	if err != nil {
		return nil, err
	}
	var state demo.ScheduleState
	var policy string
	var stopped int
	err = tx.QueryRowContext(ctx, `SELECT policy,binding_source,adopted_at,deadline,attempts,start_epoch,outcome,reason,due_at,stopped FROM durable_schedules WHERE tenant_id=?1 AND owner_id=?2 AND object_id=?3 AND input_revision=?4`, owner.TenantID, owner.OwnerID, id, revision).Scan(&policy, &state.Source, &state.AdoptedAt, &state.Deadline, &state.Attempts, &state.StartEpoch, &state.Outcome, &state.Reason, &state.Due, &stopped)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	state.Stopped = stopped == 1
	if err = json.Unmarshal([]byte(policy), &state.Policy); err != nil {
		return nil, err
	}
	if err = state.Policy.Validate(); err != nil {
		return nil, err
	}
	return &state, nil
}
func (s *Store) SaveSchedule(ctx context.Context, token runtime.Tx, owner contract.OwnerRef, id contract.ID, revision int64, state demo.ScheduleState) error {
	tx, err := s.token(ctx, token, owner)
	if err != nil {
		return err
	}
	stopped := 0
	if state.Stopped {
		stopped = 1
	}
	result, err := tx.ExecContext(ctx, `UPDATE durable_schedules SET attempts=?5,start_epoch=?6,outcome=?7,reason=?8,due_at=?9,stopped=?10 WHERE tenant_id=?1 AND owner_id=?2 AND object_id=?3 AND input_revision=?4`, owner.TenantID, owner.OwnerID, id, revision, state.Attempts, state.StartEpoch, state.Outcome, state.Reason, sqlTime(state.Due), stopped)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count != 1 {
		return runtime.ErrClaim
	}
	return err
}
func (s *Store) GateRevision(ctx context.Context, token runtime.Tx, owner contract.OwnerRef, id contract.ID) (int64, error) {
	tx, err := s.token(ctx, token, owner)
	if err != nil {
		return 0, err
	}
	var revision int64
	err = tx.QueryRowContext(ctx, `SELECT revision FROM durable_gates WHERE tenant_id=?1 AND owner_id=?2 AND gate_id=?3`, owner.TenantID, owner.OwnerID, id).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, demo.ErrGate
	}
	return revision, err
}
func (s *Store) AdvanceGate(ctx context.Context, token runtime.Tx, owner contract.OwnerRef, id contract.ID, revision int64) error {
	tx, err := s.token(ctx, token, owner)
	if err != nil {
		return err
	}
	if revision < 1 {
		return demo.ErrGate
	}
	result, err := tx.ExecContext(ctx, `UPDATE durable_gates SET revision=?4 WHERE tenant_id=?1 AND owner_id=?2 AND gate_id=?3 AND revision<=?4`, owner.TenantID, owner.OwnerID, id, revision)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count != 1 {
		return demo.ErrGate
	}
	return err
}
func (s *Store) ObserveSchedule(ctx context.Context, owner contract.OwnerRef, id contract.ID, revision int64, clock runtime.Clock) (demo.ScheduleObservation, error) {
	var out demo.ScheduleObservation
	err := s.Within(ctx, owner, func(ctx context.Context, token runtime.Tx) error {
		state, err := s.LoadSchedule(ctx, token, owner, id, revision)
		if err != nil {
			return err
		}
		if state == nil {
			return demo.ErrPolicy
		}
		out.State = *state
		tx, err := s.token(ctx, token, owner)
		if err != nil {
			return err
		}
		out.Job.Object = contract.ObjectRef{TenantID: owner.TenantID, OwnerID: owner.OwnerID, ID: id, Kind: "durable_work"}
		err = tx.QueryRowContext(ctx, `SELECT job_id,phase,work_revision,completed_revision,state,due_at FROM jobs WHERE tenant_id=?1 AND owner_id=?2 AND object_id=?3 AND object_kind='durable_work' AND phase='project'`, owner.TenantID, owner.OwnerID, id).Scan(&out.Job.ID, &out.Job.Phase, &out.Job.WorkRevision, &out.Job.CompletedRevision, &out.Job.State, &out.Job.DueAt)
		if err != nil {
			return err
		}
		now, err := clock.Now(ctx, token)
		if err != nil {
			return err
		}
		out.ActiveClaim, err = s.ClaimActive(ctx, token, out.Job, now)
		return err
	})
	return out, err
}
func (s *Store) ClaimActive(ctx context.Context, token runtime.Tx, job runtime.Job, now time.Time) (bool, error) {
	owner := contract.OwnerRef{TenantID: job.Object.TenantID, OwnerID: job.Object.OwnerID}
	tx, err := s.token(ctx, token, owner)
	if err != nil {
		return false, err
	}
	var active bool
	err = tx.QueryRowContext(ctx, `SELECT state='leased' AND lease_until>?4 FROM jobs WHERE tenant_id=?1 AND owner_id=?2 AND job_id=?3`, owner.TenantID, owner.OwnerID, job.ID, sqlTime(now)).Scan(&active)
	return active, err
}
func (s *Store) ValidateClaim(ctx context.Context, token runtime.Tx, claim runtime.Claim, now time.Time) error {
	owner := contract.OwnerRef{TenantID: claim.Object.TenantID, OwnerID: claim.Object.OwnerID}
	tx, err := s.token(ctx, token, owner)
	if err != nil {
		return err
	}
	if !validClaim(claim) {
		return runtime.ErrClaim
	}
	var found int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM jobs WHERE tenant_id=?1 AND owner_id=?2 AND job_id=?3 AND object_kind=?4 AND object_id=?5 AND phase=?6 AND state='leased' AND claimed_revision=?7 AND lease_epoch=?8 AND worker_id=?9 AND lease_until=?10 AND lease_until>?11 `, owner.TenantID, owner.OwnerID, claim.JobID, claim.Object.Kind, claim.Object.ID, claim.Phase, claim.ClaimedRevision, claim.Epoch, claim.Worker, sqlTime(claim.LeaseUntil), sqlTime(now)).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return runtime.ErrClaim
	}
	return err
}
func (s *Store) DeferClaim(ctx context.Context, token runtime.Tx, claim runtime.Claim, now, due time.Time) error {
	if !due.After(now) {
		return runtime.ErrWorkBounds
	}
	if err := s.ValidateClaim(ctx, token, claim, now); err != nil {
		return err
	}
	owner := contract.OwnerRef{TenantID: claim.Object.TenantID, OwnerID: claim.Object.OwnerID}
	tx, err := s.token(ctx, token, owner)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE jobs SET state=CASE WHEN work_revision>?4 THEN 'ready' ELSE 'waiting' END,due_at=CASE WHEN work_revision>?4 THEN due_at ELSE ?5 END,claimed_revision=NULL,worker_id=NULL,lease_until=NULL WHERE tenant_id=?1 AND owner_id=?2 AND job_id=?3`, owner.TenantID, owner.OwnerID, claim.JobID, claim.ClaimedRevision, sqlTime(due))
	return err
}
func (s *Store) NextWake(ctx context.Context, token runtime.Tx, now, fallback time.Time) (time.Time, error) {
	tx, err := s.localToken(ctx, token)
	if err != nil {
		return time.Time{}, err
	}
	if !fallback.After(now) || fallback.Sub(now) > time.Second {
		return time.Time{}, runtime.ErrWorkBounds
	}
	owner := token.Owner()
	var next sql.NullString
	// An already-due but contended candidate gets a finite fallback, avoiding
	// busy loops. Eligible future boundaries include durable waits and leases.
	err = tx.QueryRowContext(ctx, `SELECT CAST(MIN(boundary) AS TEXT) FROM (SELECT scan_at AS boundary FROM jobs WHERE tenant_id=?1 AND owner_id=?2 AND state<>'done' AND scan_at>?3 AND lease_epoch<9223372036854775807 UNION ALL SELECT s.deadline AS boundary FROM durable_schedules s JOIN jobs j ON j.tenant_id=s.tenant_id AND j.owner_id=s.owner_id AND j.object_id=s.object_id AND j.object_kind='durable_work' AND j.phase='project' AND s.input_revision=CASE WHEN j.state='leased' THEN j.claimed_revision ELSE j.work_revision END WHERE j.tenant_id=?1 AND j.owner_id=?2 AND j.state<>'done' AND s.deadline>?3) wake`, owner.TenantID, owner.OwnerID, sqlTime(now)).Scan(&next)
	if err != nil {
		return time.Time{}, err
	}
	if next.Valid {
		due, err := time.Parse("2006-01-02T15:04:05.000000000Z", next.String)
		if err != nil {
			return time.Time{}, err
		}
		if due.Before(fallback) {
			return due, nil
		}
	}
	return fallback, nil
}
func (s *Store) StopRevision(ctx context.Context, token runtime.Tx, job runtime.Job, revision int64) error {
	owner := contract.OwnerRef{TenantID: job.Object.TenantID, OwnerID: job.Object.OwnerID}
	tx, err := s.token(ctx, token, owner)
	if err != nil {
		return err
	}
	if revision < 1 || revision > job.WorkRevision {
		return runtime.ErrWorkBounds
	}
	_, err = tx.ExecContext(ctx, `UPDATE jobs SET completed_revision=?4,state=CASE WHEN work_revision>?4 THEN 'ready' ELSE 'done' END,claimed_revision=NULL,worker_id=NULL,lease_until=NULL WHERE tenant_id=?1 AND owner_id=?2 AND object_id=?3 AND object_kind='durable_work' AND phase='project' AND completed_revision<?4 AND (claimed_revision IS NULL OR claimed_revision=?4)`, owner.TenantID, owner.OwnerID, job.Object.ID, revision)
	return err
}
func (s *Store) ReleaseClaim(ctx context.Context, token runtime.Tx, claim runtime.Claim, now time.Time) error {
	if err := s.ValidateClaim(ctx, token, claim, now); err != nil {
		return err
	}
	owner := contract.OwnerRef{TenantID: claim.Object.TenantID, OwnerID: claim.Object.OwnerID}
	tx, err := s.token(ctx, token, owner)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE jobs SET state='ready',claimed_revision=NULL,worker_id=NULL,lease_until=NULL WHERE tenant_id=?1 AND owner_id=?2 AND job_id=?3`, owner.TenantID, owner.OwnerID, claim.JobID)
	return err
}
