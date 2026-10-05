package content

import (
	"context"
	"encoding/json"
	"reflect"

	d "github.com/ruipengliu/lerna/domain/content"
	"github.com/ruipengliu/lerna/runtime"
)

// BindLegacyPrimary is the dedicated whole-record CAS for an exact trusted
// stopped original namespace. Ordinary SaveVersion cannot change the binding.
func (s *Store) BindLegacyPrimary(ctx context.Context, token runtime.Tx, expected d.Record, qualification d.LegacyPrimaryQualification) (d.Record, error) {
	var empty d.Record
	if qualification.Namespace != s.schema || qualification.Owner != expected.Ref.Owner || qualification.Binding == "" || qualification.ID == "" || qualification.EvidenceDigest == "" {
		return empty, runtime.ErrScope
	}
	matched := false
	for _, version := range qualification.Versions {
		if version.Ref == expected.Ref && version.ObjectKey == expected.ObjectKey && version.Purpose == expected.Purpose && reflect.DeepEqual(version.Subject, expected.Subject) {
			matched = true
		}
	}
	if !matched {
		return empty, runtime.ErrScope
	}
	current, err := s.LockVersion(ctx, token, expected.Ref)
	if err != nil {
		return empty, err
	}
	if current == nil || !reflect.DeepEqual(*current, expected) {
		return empty, runtime.ErrClaim
	}
	if current.PrimaryHolderBinding != "" {
		if current.PrimaryHolderBinding == qualification.Binding && current.LegacyPrimaryQualificationID == qualification.ID && current.LegacyPrimaryEvidenceDigest == qualification.EvidenceDigest {
			return *current, nil
		}
		return empty, d.ErrHolderBinding
	}
	if current.LegacyPrimaryQualificationID != "" || current.LegacyPrimaryEvidenceDigest != "" || current.Publication != "published" || current.BodySeal != nil || current.BodyGone || current.Bytes != nil || current.StagingHolder || !current.ObjectHolder {
		return empty, runtime.ErrScope
	}
	tx, err := s.core.SQL(ctx, token, commonOwner(expected.Ref.Owner))
	if err != nil {
		return empty, err
	}
	// Preserve the exact stored JSON as the expected SQL operand. Old JSON did
	// not contain the additive fields, so remarshal(expected) is not that operand.
	var previous []byte
	err = tx.QueryRowContext(ctx, `SELECT body FROM `+s.core.Table("content_versions")+` WHERE tenant_id=$1 AND owner_id=$2 AND content_id=$3 AND version=$4 FOR UPDATE`, expected.Ref.Owner.TenantID, expected.Ref.Owner.OwnerID, expected.Ref.ContentID, expected.Ref.Version).Scan(&previous)
	if err != nil {
		return empty, err
	}
	next := expected
	next.PrimaryHolderBinding = qualification.Binding
	next.LegacyPrimaryQualificationID = qualification.ID
	next.LegacyPrimaryEvidenceDigest = qualification.EvidenceDigest
	next.Revision++
	body, err := json.Marshal(next)
	if err != nil {
		return empty, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE `+s.core.Table("content_versions")+` SET body=$1,revision=$2,primary_holder_binding=$3 WHERE tenant_id=$4 AND owner_id=$5 AND content_id=$6 AND version=$7 AND body=$8 AND staging IS NOT DISTINCT FROM $9::bytea AND object_id=$10 AND object_key=$11 AND tuple_digest=$12 AND publication='published' AND revision=$13 AND primary_holder_binding='' AND body_seal IS NULL AND NOT body_gone`, body, next.Revision, qualification.Binding, expected.Ref.Owner.TenantID, expected.Ref.Owner.OwnerID, expected.Ref.ContentID, expected.Ref.Version, previous, expected.Bytes, expected.ObjectID, expected.ObjectKey, expected.TupleDigest, expected.Revision)
	if err != nil {
		return empty, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return empty, err
	}
	if count != 1 {
		return empty, runtime.ErrClaim
	}
	return next, nil
}
