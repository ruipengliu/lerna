package content

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	d "github.com/ruipengliu/lerna/domain/content"
	"github.com/ruipengliu/lerna/runtime"
	"reflect"
)

// QualifyPolicyCleanupNotRequired only consumes an existing original row after
// Lifecycle has locked and checked this page's original saving bases. It never
// acquires version/change locks or creates a responsibility.
func (s *Store) QualifyPolicyCleanupNotRequired(ctx context.Context, token runtime.Tx, expected d.CleanupResponsibility) error {
	if expected.ChangeKey == "" || len(expected.ChangeKey) > 128 || expected.BodyCleanup != "pending" || expected.Residual != "holder_unconfirmed" || expected.Reason != "" || expected.Deadline.IsZero() {
		return runtime.ErrScope
	}
	id, _, err := d.VersionIdentity(expected.Ref)
	if err != nil {
		return err
	}
	tx, err := s.core.SQL(ctx, token, commonOwner(expected.Ref.Owner))
	if err != nil {
		return err
	}
	var body []byte
	err = tx.QueryRowContext(ctx, `SELECT body FROM `+s.core.Table("content_cleanup_responsibilities")+` WHERE tenant_id=$1 AND owner_id=$2 AND change_key=$3 AND object_id=$4 FOR UPDATE`, expected.Ref.Owner.TenantID, expected.Ref.Owner.OwnerID, expected.ChangeKey, id).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return runtime.ErrScope
	}
	if err != nil {
		return err
	}
	var actual d.CleanupResponsibility
	if err = json.Unmarshal(body, &actual); err != nil {
		return err
	}
	if !reflect.DeepEqual(actual, expected) {
		return &d.ManagementConflict{}
	}
	actual.BodyCleanup = "not_required"
	actual.Residual = "current_save_basis_restored"
	qualified, err := json.Marshal(actual)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE `+s.core.Table("content_cleanup_responsibilities")+` SET body=$5 WHERE tenant_id=$1 AND owner_id=$2 AND change_key=$3 AND object_id=$4 AND body=$6`, expected.Ref.Owner.TenantID, expected.Ref.Owner.OwnerID, expected.ChangeKey, id, qualified, body)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count != 1 {
		return runtime.ErrScope
	}
	return err
}
