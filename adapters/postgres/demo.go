package postgres

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	"github.com/ruipengliu/lerna/contract"
	demo "github.com/ruipengliu/lerna/internal/durableworkdemo"
	"github.com/ruipengliu/lerna/runtime"
)

func (s *Store) LockInput(ctx context.Context, token runtime.Tx, owner contract.OwnerRef, id contract.ID) (*demo.Input, error) {
	tx, err := s.token(ctx, token, owner)
	if err != nil {
		return nil, err
	}
	// Global admission order: original command key, business object, then Job.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(2,hashtext($1))`, lockKey("input", owner, string(id))); err != nil {
		return nil, err
	}
	input := demo.Input{ID: id}
	err = tx.QueryRowContext(ctx, `SELECT revision,text_value,created_at,updated_at FROM `+s.table("durable_inputs")+` WHERE tenant_id=$1 AND owner_id=$2 AND object_id=$3 FOR UPDATE`, owner.TenantID, owner.OwnerID, id).Scan(&input.Revision, &input.Text, &input.CreatedAt, &input.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &input, err
}
func (s *Store) SaveInput(ctx context.Context, token runtime.Tx, owner contract.OwnerRef, input demo.Input) error {
	tx, err := s.token(ctx, token, owner)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO `+s.table("durable_inputs")+` (tenant_id,owner_id,object_id,revision,text_value,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(tenant_id,owner_id,object_id) DO UPDATE SET revision=EXCLUDED.revision,text_value=EXCLUDED.text_value,updated_at=EXCLUDED.updated_at`, owner.TenantID, owner.OwnerID, input.ID, input.Revision, []byte(input.Text), input.CreatedAt, input.UpdatedAt)
	return err
}
func (s *Store) Trigger(ctx context.Context, token runtime.Tx, object contract.ObjectRef, phase string, revision int64, due time.Time) (runtime.Job, error) {
	var job runtime.Job
	owner := contract.OwnerRef{TenantID: object.TenantID, OwnerID: object.OwnerID}
	tx, err := s.token(ctx, token, owner)
	if err != nil {
		return job, err
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return job, err
	}
	job.Object = object
	job.Object.Revision = nil
	job.Phase = phase
	err = tx.QueryRowContext(ctx, `INSERT INTO `+s.table("jobs")+`(tenant_id,owner_id,job_id,object_kind,object_id,phase,work_revision,state,due_at) VALUES($1,$2,$3,$4,$5,$6,$7,'ready',$8) ON CONFLICT(tenant_id,owner_id,object_kind,object_id,phase) DO UPDATE SET work_revision=EXCLUDED.work_revision,due_at=EXCLUDED.due_at RETURNING job_id,work_revision,completed_revision,state,due_at`, owner.TenantID, owner.OwnerID, "job-"+hex.EncodeToString(nonce[:]), object.Kind, object.ID, phase, revision, due).Scan(&job.ID, &job.WorkRevision, &job.CompletedRevision, &job.State, &job.DueAt)
	return job, err
}
func (s *Store) ObserveInput(ctx context.Context, owner contract.OwnerRef, id contract.ID) (demo.Observation, error) {
	var result demo.Observation
	result.Input.ID = id
	result.Job.Object = contract.ObjectRef{TenantID: owner.TenantID, OwnerID: owner.OwnerID, Kind: "durable_work", ID: id}
	err := s.Within(ctx, owner, func(ctx context.Context, token runtime.Tx) error {
		tx, err := s.token(ctx, token, owner)
		if err != nil {
			return err
		}
		// One statement yields a consistent input/Job observation at READ COMMITTED.
		return tx.QueryRowContext(ctx, `SELECT i.revision,i.text_value,i.created_at,i.updated_at,j.job_id,j.phase,j.work_revision,j.completed_revision,j.state,j.due_at FROM `+s.table("durable_inputs")+` i JOIN `+s.table("jobs")+` j ON j.tenant_id=i.tenant_id AND j.owner_id=i.owner_id AND j.object_id=i.object_id AND j.object_kind='durable_work' AND j.phase='project' WHERE i.tenant_id=$1 AND i.owner_id=$2 AND i.object_id=$3`, owner.TenantID, owner.OwnerID, id).Scan(&result.Input.Revision, &result.Input.Text, &result.Input.CreatedAt, &result.Input.UpdatedAt, &result.Job.ID, &result.Job.Phase, &result.Job.WorkRevision, &result.Job.CompletedRevision, &result.Job.State, &result.Job.DueAt)
	})
	return result, err
}
