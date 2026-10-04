package content

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	d "github.com/ruipengliu/lerna/domain/content"
	"github.com/ruipengliu/lerna/runtime"
	"reflect"
	"time"
)

// CurrentSavingPolicy reads the actual complete policy under a share lock;
// missing, malformed or expired-use qualifications never become save=false.
func (s *Store) CurrentSavingPolicy(ctx context.Context, token runtime.Tx, subject v.SubjectBinding, ref v.ContentRef, purpose string) (*d.FixturePolicy, error) {
	tx, err := s.core.SQL(ctx, token, commonOwner(ref.Owner))
	if err != nil {
		return nil, err
	}
	key, _, err := subjectKey(subject)
	if err != nil {
		return nil, err
	}
	var body []byte
	var revision int64
	var valid time.Time
	err = tx.QueryRowContext(ctx, `SELECT body,revision,valid_until FROM `+s.core.Table("content_fixture_policies")+` WHERE tenant_id=$1 AND owner_id=$2 AND subject_key=$3 AND content_id=$4 AND version=$5 AND purpose=$6 FOR SHARE`, ref.Owner.TenantID, ref.Owner.OwnerID, key, ref.ContentID, ref.Version, purpose).Scan(&body, &revision, &valid)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var policy d.FixturePolicy
	if err = json.Unmarshal(body, &policy); err != nil {
		return nil, err
	}
	actualKey, _, err := subjectKey(policy.Subject)
	if err != nil {
		return nil, err
	}
	// pgx timestamptz encoding carries microseconds; the trusted policy JSON
	// may retain nanoseconds. Compare the actual representable column value.
	if policy.Revision != revision || !policy.ValidUntil.Truncate(time.Microsecond).Equal(valid) {
		return nil, runtime.ErrScope
	}
	if policy.Ref != ref || actualKey != key || policy.Purpose != purpose {
		return nil, nil
	}
	return &policy, nil
}

func (s *Store) BindPolicyCleanupSeal(ctx context.Context, token runtime.Tx, expected d.CleanupResponsibility, seal d.BodySeal) error {
	if expected.BodyCleanup != "pending" || !policySealMatches(expected, seal) {
		return runtime.ErrScope
	}
	_, body, err := s.lockPolicyCleanup(ctx, token, expected.Ref, expected.ChangeKey)
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
	return nil
}

func (s *Store) AcknowledgePolicyCleanupErased(ctx context.Context, token runtime.Tx, seal d.BodySeal) error {
	actual, body, err := s.lockPolicyCleanup(ctx, token, seal.Ref, seal.PolicyChangeKey)
	if err != nil {
		return err
	}
	if !policySealMatches(actual, seal) || (actual.BodyCleanup != "pending" && actual.BodyCleanup != "erased") {
		return runtime.ErrScope
	}
	if actual.BodyCleanup == "erased" {
		if actual.Residual != "" {
			return runtime.ErrScope
		}
		return nil
	}
	actual.BodyCleanup = "erased"
	actual.Residual = ""
	qualified, err := json.Marshal(actual)
	if err != nil {
		return err
	}
	id, _, err := d.VersionIdentity(seal.Ref)
	if err != nil {
		return err
	}
	tx, err := s.core.SQL(ctx, token, commonOwner(seal.Ref.Owner))
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE `+s.core.Table("content_cleanup_responsibilities")+` SET body=$5 WHERE tenant_id=$1 AND owner_id=$2 AND change_key=$3 AND object_id=$4 AND body=$6`, seal.Ref.Owner.TenantID, seal.Ref.Owner.OwnerID, seal.PolicyChangeKey, id, qualified, body)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count != 1 {
		return runtime.ErrScope
	}
	return err
}

func policySealMatches(responsibility d.CleanupResponsibility, seal d.BodySeal) bool {
	a, err := v.Encode(responsibility.Subject)
	if err != nil {
		return false
	}
	b, err := v.Encode(seal.Subject)
	return err == nil && seal.PolicyChangeKey != "" && seal.ID != "" && responsibility.ChangeKey == seal.PolicyChangeKey && responsibility.Ref == seal.Ref && string(a) == string(b) && responsibility.Purpose == seal.Purpose && responsibility.Deadline.Equal(seal.Deadline)
}

func (s *Store) lockPolicyCleanup(ctx context.Context, token runtime.Tx, ref v.ContentRef, key string) (d.CleanupResponsibility, []byte, error) {
	if key == "" || len(key) > 128 {
		return d.CleanupResponsibility{}, nil, runtime.ErrScope
	}
	id, _, err := d.VersionIdentity(ref)
	if err != nil {
		return d.CleanupResponsibility{}, nil, err
	}
	tx, err := s.core.SQL(ctx, token, commonOwner(ref.Owner))
	if err != nil {
		return d.CleanupResponsibility{}, nil, err
	}
	var body []byte
	err = tx.QueryRowContext(ctx, `SELECT body FROM `+s.core.Table("content_cleanup_responsibilities")+` WHERE tenant_id=$1 AND owner_id=$2 AND change_key=$3 AND object_id=$4 FOR UPDATE`, ref.Owner.TenantID, ref.Owner.OwnerID, key, id).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return d.CleanupResponsibility{}, nil, runtime.ErrScope
	}
	if err != nil {
		return d.CleanupResponsibility{}, nil, err
	}
	var actual d.CleanupResponsibility
	if err = json.Unmarshal(body, &actual); err != nil {
		return actual, nil, err
	}
	if actual.Ref != ref || actual.ChangeKey != key {
		return actual, nil, runtime.ErrScope
	}
	return actual, body, nil
}

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
