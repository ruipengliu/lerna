package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"github.com/ruipengliu/lerna/contract"
	demo "github.com/ruipengliu/lerna/internal/durableworkdemo"
	"github.com/ruipengliu/lerna/runtime"
)

func (s *Store) CommandBodyGone(ctx context.Context, token runtime.Tx, ref contract.CommandRef) (bool, error) {
	tx, err := s.token(ctx, token, ref.Owner)
	if err != nil {
		return false, err
	}
	var gone bool
	err = tx.QueryRowContext(ctx, `SELECT body_gone FROM command_receipts WHERE tenant_id=?1 AND owner_id=?2 AND command_id=?3`, ref.Owner.TenantID, ref.Owner.OwnerID, ref.CommandID).Scan(&gone)
	return gone, err
}
func (s *Store) LockRetention(ctx context.Context, token runtime.Tx, owner contract.OwnerRef, id contract.ID) (demo.RetentionState, error) {
	tx, err := s.token(ctx, token, owner)
	if err != nil {
		return demo.RetentionState{}, err
	}
	var result demo.RetentionState
	err = tx.QueryRowContext(ctx, `SELECT work_revision,completed_revision,state,(claimed_revision IS NOT NULL OR worker_id IS NOT NULL OR lease_until IS NOT NULL) FROM jobs WHERE tenant_id=?1 AND owner_id=?2 AND object_kind='durable_work' AND object_id=?3 AND phase='project'`, owner.TenantID, owner.OwnerID, id).Scan(&result.WorkRevision, &result.CompletedRevision, &result.State, &result.HasClaim)
	if err != nil {
		return result, err
	}
	var revision sql.NullInt64
	var digest sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT projected_revision,text_digest FROM durable_inputs WHERE tenant_id=?1 AND owner_id=?2 AND object_id=?3`, owner.TenantID, owner.OwnerID, id).Scan(&revision, &digest)
	if revision.Valid && digest.Valid {
		result.Projection = &demo.Projection{InputRevision: revision.Int64, TextDigest: digest.String}
	}
	return result, err
}
func (s *Store) ClearBody(ctx context.Context, token runtime.Tx, ref contract.CommandRef, id contract.ID, revision int64) error {
	tx, err := s.token(ctx, token, ref.Owner)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE durable_inputs SET text_value=X'',body_gone=true WHERE tenant_id=?1 AND owner_id=?2 AND object_id=?3 AND revision=?4 AND body_gone=false`, ref.Owner.TenantID, ref.Owner.OwnerID, id, revision)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("cleanup input changed")
	}
	result, err = tx.ExecContext(ctx, `UPDATE command_receipts SET body_gone=true WHERE tenant_id=?1 AND owner_id=?2 AND command_id=?3 AND body_gone=false`, ref.Owner.TenantID, ref.Owner.OwnerID, ref.CommandID)
	if err != nil {
		return err
	}
	count, err = result.RowsAffected()
	if err == nil && count != 1 {
		return errors.New("cleanup original command changed")
	}
	return err
}
