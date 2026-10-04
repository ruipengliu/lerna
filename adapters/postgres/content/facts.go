package content

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/ruipengliu/lerna/contract"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	d "github.com/ruipengliu/lerna/domain/content"
	"github.com/ruipengliu/lerna/runtime"
	"time"
)

func (s *Store) Within(ctx context.Context, owner contract.OwnerRef, fn func(context.Context, runtime.Tx) error) error {
	return s.core.Within(ctx, owner, fn)
}
func (s *Store) Now(ctx context.Context, token runtime.Tx) (time.Time, error) {
	return s.core.Now(ctx, token)
}
func (s *Store) LockCommand(ctx context.Context, token runtime.Tx, ref v.CommandRef) (*d.CommandRecord, error) {
	tx, err := s.core.SQL(ctx, token, commonOwner(ref.Owner))
	if err != nil {
		return nil, err
	}
	key, _ := v.Encode(ref)
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(1,hashtext($1))`, s.schema+string(key)); err != nil {
		return nil, err
	}
	return s.ReadCommandRecord(ctx, token, ref)
}
func (s *Store) ReadCommandRecord(ctx context.Context, token runtime.Tx, ref v.CommandRef) (*d.CommandRecord, error) {
	tx, err := s.core.SQL(ctx, token, commonOwner(ref.Owner))
	if err != nil {
		return nil, err
	}
	var metadata, receipt []byte
	var digest string
	err = tx.QueryRowContext(ctx, `SELECT metadata,receipt,digest FROM `+s.core.Table("command_receipts")+` WHERE tenant_id=$1 AND owner_id=$2 AND command_id=$3`, ref.Owner.TenantID, ref.Owner.OwnerID, ref.CommandID).Scan(&metadata, &receipt, &digest)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var record d.CommandRecord
	if err = json.Unmarshal(metadata, &record); err != nil {
		return nil, err
	}
	fixed, err := v.Decode[v.CommandReceipt](receipt)
	if err != nil {
		return nil, err
	}
	encoded, err := v.Encode(record.Receipt)
	if err != nil || string(encoded) != string(receipt) || record.Digest != digest {
		return nil, runtime.ErrScope
	}
	response := v.NewCommandGetResponseFound(v.CommandGetResponseFound{CommandRef: ref, Receipt: fixed, Progress: v.NewCommandProgressNone(v.CommandProgressNone{})})
	if _, accepted := fixed.AsAccepted(); accepted {
		response = v.NewCommandGetResponseFound(v.CommandGetResponseFound{CommandRef: ref, Receipt: fixed, Progress: v.NewCommandProgressUnavailable(v.CommandProgressUnavailable{Reason: "dependency_unavailable"})})
	}
	if _, err = v.EncodeCommandResponse(response, ref); err != nil {
		return nil, err
	}
	record.Receipt = fixed
	return &record, nil
}
func (s *Store) SaveCommand(ctx context.Context, token runtime.Tx, ref v.CommandRef, record d.CommandRecord) error {
	tx, err := s.core.SQL(ctx, token, commonOwner(ref.Owner))
	if err != nil {
		return err
	}
	receipt, err := v.Encode(record.Receipt)
	if err != nil {
		return err
	}
	metadata, err := json.Marshal(record)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO `+s.core.Table("command_receipts")+`(tenant_id,owner_id,command_id,contract_version,digest,metadata,receipt)VALUES($1,$2,$3,'1.2.0',$4,$5,$6)`, ref.Owner.TenantID, ref.Owner.OwnerID, ref.CommandID, record.Digest, metadata, receipt)
	return err
}
func (s *Store) LockVersion(ctx context.Context, token runtime.Tx, ref v.ContentRef) (*d.Record, error) {
	tx, err := s.core.SQL(ctx, token, commonOwner(ref.Owner))
	if err != nil {
		return nil, err
	}
	id, _, err := d.VersionIdentity(ref)
	if err != nil {
		return nil, err
	}
	key, _ := json.Marshal([]string{s.schema, string(ref.Owner.TenantID), string(ref.Owner.OwnerID), id})
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(2,hashtext($1))`, string(key)); err != nil {
		return nil, err
	}
	var body, staging []byte
	var objectID, keyStored, tuple, publication string
	var revision int64
	err = tx.QueryRowContext(ctx, `SELECT body,staging,object_id,object_key,tuple_digest,publication,revision FROM `+s.core.Table("content_versions")+` WHERE tenant_id=$1 AND owner_id=$2 AND content_id=$3 AND version=$4 FOR UPDATE`, ref.Owner.TenantID, ref.Owner.OwnerID, ref.ContentID, ref.Version).Scan(&body, &staging, &objectID, &keyStored, &tuple, &publication, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var record d.Record
	if err = json.Unmarshal(body, &record); err != nil {
		return nil, err
	}
	if record.Ref.Owner != ref.Owner || record.Ref.ContentID != ref.ContentID || record.Ref.Version != ref.Version || record.ObjectID != objectID || record.ObjectKey != keyStored || record.TupleDigest != tuple || record.Publication != publication || record.Revision != revision {
		return nil, runtime.ErrScope
	}
	if err = record.ValidateIdentity(); err != nil {
		return nil, err
	}
	record.Bytes = staging
	return &record, nil
}
func (s *Store) LockObject(ctx context.Context, token runtime.Tx, id string) (*d.Record, error) {
	tx, err := s.core.LocalSQL(ctx, token)
	if err != nil {
		return nil, err
	}
	var body []byte
	err = tx.QueryRowContext(ctx, `SELECT body FROM `+s.core.Table("content_versions")+` WHERE tenant_id=$1 AND owner_id=$2 AND object_id=$3`, token.Owner().TenantID, token.Owner().OwnerID, id).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var record d.Record
	if err = json.Unmarshal(body, &record); err != nil {
		return nil, err
	}
	if record.ObjectID != id {
		return nil, runtime.ErrScope
	}
	return s.LockVersion(ctx, token, record.Ref)
}
func (s *Store) SaveVersion(ctx context.Context, token runtime.Tx, record d.Record) error {
	if err := record.ValidateIdentity(); err != nil {
		return err
	}
	if len(record.Bytes) > v.MaxContentBytes || record.Publication == "published" && record.Bytes != nil {
		return runtime.ErrWorkBounds
	}
	tx, err := s.core.SQL(ctx, token, commonOwner(record.Ref.Owner))
	if err != nil {
		return err
	}
	body, err := json.Marshal(record)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO `+s.core.Table("content_versions")+`(tenant_id,owner_id,content_id,version,object_id,object_key,tuple_digest,publication,revision,body,staging)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)ON CONFLICT(tenant_id,owner_id,content_id,version)DO UPDATE SET publication=excluded.publication,revision=excluded.revision,body=excluded.body,staging=excluded.staging WHERE content_versions.tuple_digest=excluded.tuple_digest AND content_versions.object_id=excluded.object_id AND content_versions.revision<excluded.revision`, record.Ref.Owner.TenantID, record.Ref.Owner.OwnerID, record.Ref.ContentID, record.Ref.Version, record.ObjectID, record.ObjectKey, record.TupleDigest, record.Publication, record.Revision, body, record.Bytes)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return runtime.ErrClaim
	}
	return err
}
func (s *Store) CheckCapacity(ctx context.Context, token runtime.Tx, limits d.Limits, length int64) (bool, error) {
	tx, err := s.core.LocalSQL(ctx, token)
	if err != nil {
		return false, err
	}
	key, _ := json.Marshal([]string{s.schema, string(token.Owner().TenantID), string(token.Owner().OwnerID)})
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(4,hashtext($1))`, string(key)); err != nil {
		return false, err
	}
	var count, total int64
	err = tx.QueryRowContext(ctx, `SELECT count(*) FILTER(WHERE publication='preparing'),COALESCE(sum(octet_length(staging)),0) FROM `+s.core.Table("content_versions")+` WHERE tenant_id=$1 AND owner_id=$2`, token.Owner().TenantID, token.Owner().OwnerID).Scan(&count, &total)
	return err == nil && count < int64(limits.MaxPreparingVersions) && length <= limits.MaxStagingBytes-total, err
}
func (s *Store) Trigger(ctx context.Context, token runtime.Tx, ref contract.ObjectRef, phase string, revision int64, now time.Time) (runtime.Job, error) {
	if phase == "policy_propagation" {
		due, err := s.NextPolicyDue(ctx, token, string(ref.ID))
		if err != nil {
			return runtime.Job{}, err
		}
		if !due.IsZero() && due.Before(now) {
			now = due
		}
	}
	return s.core.Trigger(ctx, token, ref, phase, revision, now)
}
func (s *Store) Scan(ctx context.Context, token runtime.Tx, now time.Time, limit int) ([]runtime.Job, error) {
	return s.core.Scan(ctx, token, now, limit)
}
func (s *Store) Claim(ctx context.Context, token runtime.Tx, job runtime.Job, worker string, now, until time.Time) (*runtime.Claim, error) {
	return s.core.Claim(ctx, token, job, worker, now, until)
}
func (s *Store) ValidateClaim(ctx context.Context, token runtime.Tx, claim runtime.Claim, now time.Time) error {
	return s.core.ValidateClaim(ctx, token, claim, now)
}
func (s *Store) Complete(ctx context.Context, token runtime.Tx, claim runtime.Claim, now time.Time) error {
	return s.core.Complete(ctx, token, claim, now)
}
func (s *Store) Renew(ctx context.Context, token runtime.Tx, claim runtime.Claim, now, until time.Time) (runtime.Claim, error) {
	return s.core.Renew(ctx, token, claim, now, until)
}
func (s *Store) DeferClaim(ctx context.Context, token runtime.Tx, claim runtime.Claim, now, due time.Time) error {
	return s.core.DeferClaim(ctx, token, claim, now, due)
}
