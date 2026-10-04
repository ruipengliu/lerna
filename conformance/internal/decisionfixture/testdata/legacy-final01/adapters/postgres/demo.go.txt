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

func (s *Store) LockInput(ctx context.Context, token runtime.Tx, owner contract.OwnerRef, id contract.ID) (*demo.Input, error) {
	tx, err := s.token(ctx, token, owner)
	if err != nil {
		return nil, err
	}
	// Global admission order: original command key, business object, then Job.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(2,hashtext($1))`, s.lockKey("input", owner, string(id))); err != nil {
		return nil, err
	}
	input := demo.Input{ID: id}
	err = tx.QueryRowContext(ctx, `SELECT revision,text_value,created_at,updated_at,body_gone FROM `+s.table("durable_inputs")+` WHERE tenant_id=$1 AND owner_id=$2 AND object_id=$3 FOR UPDATE`, owner.TenantID, owner.OwnerID, id).Scan(&input.Revision, &input.Text, &input.CreatedAt, &input.UpdatedAt, &input.BodyGone)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err == nil {
		err = validateBody(&input)
	}
	return &input, err
}
func (s *Store) SaveInput(ctx context.Context, token runtime.Tx, owner contract.OwnerRef, input demo.Input) error {
	tx, err := s.token(ctx, token, owner)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO `+s.table("durable_inputs")+` (tenant_id,owner_id,object_id,revision,text_value,created_at,updated_at,body_gone) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(tenant_id,owner_id,object_id) DO UPDATE SET revision=EXCLUDED.revision,text_value=EXCLUDED.text_value,updated_at=EXCLUDED.updated_at,body_gone=EXCLUDED.body_gone`, owner.TenantID, owner.OwnerID, input.ID, input.Revision, []byte(input.Text), input.CreatedAt, input.UpdatedAt, input.BodyGone)
	return err
}
func (s *Store) Trigger(ctx context.Context, token runtime.Tx, object contract.ObjectRef, phase string, revision int64, due time.Time) (runtime.Job, error) {
	return s.core.Trigger(ctx, token, object, phase, revision, due)
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
		var projected sql.NullInt64
		var digest sql.NullString
		// One statement yields a consistent input/Job observation at READ COMMITTED.
		err = tx.QueryRowContext(ctx, `SELECT i.revision,i.text_value,i.created_at,i.updated_at,j.job_id,j.phase,j.work_revision,j.completed_revision,j.state,j.due_at,i.projected_revision,i.text_digest,i.body_gone FROM `+s.table("durable_inputs")+` i JOIN `+s.table("jobs")+` j ON j.tenant_id=i.tenant_id AND j.owner_id=i.owner_id AND j.object_id=i.object_id AND j.object_kind='durable_work' AND j.phase='project' WHERE i.tenant_id=$1 AND i.owner_id=$2 AND i.object_id=$3`, owner.TenantID, owner.OwnerID, id).Scan(&result.Input.Revision, &result.Input.Text, &result.Input.CreatedAt, &result.Input.UpdatedAt, &result.Job.ID, &result.Job.Phase, &result.Job.WorkRevision, &result.Job.CompletedRevision, &result.Job.State, &result.Job.DueAt, &projected, &digest, &result.Input.BodyGone)
		if projected.Valid {
			result.Projection = &demo.Projection{InputRevision: projected.Int64, TextDigest: digest.String}
		}
		if err == nil {
			err = validateBody(&result.Input)
		}
		return err
	})
	return result, err
}

func validateBody(input *demo.Input) error {
	input.StoredTextBytes = int64(len(input.Text))
	if input.BodyGone && input.StoredTextBytes != 0 {
		return errors.New("gone body retains stored bytes")
	}
	return nil
}
