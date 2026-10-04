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
)

func validateCopy(copy d.SecondaryCopy) error {
	_, key, err := d.VersionIdentity(copy.Observation.Ref)
	if err != nil {
		return err
	}
	if copy.ObjectKey != key || copy.AttemptKey != key+".1.tmp" || copy.ID == "" || len(copy.ID) > 128 || copy.Observation.HolderID == "" || len(copy.Observation.HolderID) > 128 || copy.Observation.Binding == "" || len(copy.Observation.Binding) > 256 || copy.Purpose == "" || !copy.Observation.Deadline.After(copy.StartedAt) {
		return runtime.ErrScope
	}
	return nil
}
func (s *Store) LockSecondaryCopy(ctx context.Context, token runtime.Tx, ref v.ContentRef, holderID string) (*d.SecondaryCopy, error) {
	id, _, err := d.VersionIdentity(ref)
	if err != nil {
		return nil, err
	}
	tx, err := s.core.SQL(ctx, token, commonOwner(ref.Owner))
	if err != nil {
		return nil, err
	}
	var body []byte
	var binding, copyID string
	var confirmed bool
	err = tx.QueryRowContext(ctx, `SELECT binding,copy_id,confirmed,body FROM `+s.core.Table("content_secondary_copies")+` WHERE tenant_id=$1 AND owner_id=$2 AND object_id=$3 AND holder_id=$4 FOR UPDATE`, ref.Owner.TenantID, ref.Owner.OwnerID, id, holderID).Scan(&binding, &copyID, &confirmed, &body)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var copy d.SecondaryCopy
	if err = json.Unmarshal(body, &copy); err != nil {
		return nil, err
	}
	if err = validateCopy(copy); err != nil {
		return nil, err
	}
	if copy.Observation.Ref != ref || copy.Observation.HolderID != holderID || copy.Observation.Binding != binding || copy.ID != copyID || copy.Observation.Confirmed != confirmed {
		return nil, runtime.ErrScope
	}
	return &copy, nil
}
func (s *Store) SaveSecondaryCopy(ctx context.Context, token runtime.Tx, copy d.SecondaryCopy) error {
	if err := validateCopy(copy); err != nil {
		return err
	}
	record, err := s.LockVersion(ctx, token, copy.Observation.Ref)
	if err != nil {
		return err
	}
	if record == nil || record.Ref != copy.Observation.Ref || record.BodySeal != nil || record.Purpose != copy.Purpose || !reflect.DeepEqual(record.Subject, copy.Subject) {
		return runtime.ErrScope
	}
	old, err := s.LockSecondaryCopy(ctx, token, copy.Observation.Ref, copy.Observation.HolderID)
	if err != nil {
		return err
	}
	if old != nil {
		expected := *old
		expected.Observation.Confirmed = copy.Observation.Confirmed
		oldBody, err := json.Marshal(expected)
		if err != nil {
			return err
		}
		newBody, err := json.Marshal(copy)
		if err != nil {
			return err
		}
		if string(oldBody) != string(newBody) || old.Observation.Confirmed && !copy.Observation.Confirmed {
			return runtime.ErrScope
		}
	}
	id, _, err := d.VersionIdentity(copy.Observation.Ref)
	if err != nil {
		return err
	}
	tx, err := s.core.SQL(ctx, token, commonOwner(copy.Observation.Ref.Owner))
	if err != nil {
		return err
	}
	body, err := json.Marshal(copy)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO `+s.core.Table("content_secondary_copies")+`(tenant_id,owner_id,object_id,holder_id,binding,copy_id,confirmed,body) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(tenant_id,owner_id,object_id,holder_id) DO UPDATE SET confirmed=excluded.confirmed,body=excluded.body WHERE content_secondary_copies.binding=excluded.binding AND content_secondary_copies.copy_id=excluded.copy_id AND (NOT content_secondary_copies.confirmed OR excluded.confirmed)`, copy.Observation.Ref.Owner.TenantID, copy.Observation.Ref.Owner.OwnerID, id, copy.Observation.HolderID, copy.Observation.Binding, copy.ID, copy.Observation.Confirmed, body)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count != 1 {
		return runtime.ErrScope
	}
	return err
}
func (s *Store) SecondaryCopies(ctx context.Context, token runtime.Tx, ref v.ContentRef, cursor string, limit int) (copies []d.SecondaryCopy, next string, returnErr error) {
	if limit < 1 || limit > 64 || len(cursor) > 128 {
		return nil, "", runtime.ErrWorkBounds
	}
	id, _, err := d.VersionIdentity(ref)
	if err != nil {
		return nil, "", err
	}
	tx, err := s.core.SQL(ctx, token, commonOwner(ref.Owner))
	if err != nil {
		return nil, "", err
	}
	rows, err := tx.QueryContext(ctx, `SELECT holder_id,binding,copy_id,confirmed,body FROM `+s.core.Table("content_secondary_copies")+` WHERE tenant_id=$1 AND owner_id=$2 AND object_id=$3 AND holder_id>$4 ORDER BY holder_id LIMIT $5`, ref.Owner.TenantID, ref.Owner.OwnerID, id, cursor, limit+1)
	if err != nil {
		return nil, "", err
	}
	defer func() { returnErr = errors.Join(returnErr, rows.Close()) }()
	for rows.Next() {
		var holderID, binding, copyID string
		var confirmed bool
		var body []byte
		if err = rows.Scan(&holderID, &binding, &copyID, &confirmed, &body); err != nil {
			return nil, "", err
		}
		var copy d.SecondaryCopy
		if err = json.Unmarshal(body, &copy); err != nil {
			return nil, "", err
		}
		if err = validateCopy(copy); err != nil {
			return nil, "", err
		}
		if copy.Observation.Ref != ref || copy.Observation.HolderID != holderID || copy.Observation.Binding != binding || copy.ID != copyID || copy.Observation.Confirmed != confirmed {
			return nil, "", runtime.ErrScope
		}
		copies = append(copies, copy)
		if len(copies) > limit {
			next = copies[limit-1].Observation.HolderID
			copies = copies[:limit]
			break
		}
	}
	return copies, next, rows.Err()
}
