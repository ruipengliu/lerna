package content

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	d "github.com/ruipengliu/lerna/domain/content"
	"github.com/ruipengliu/lerna/runtime"
	"strconv"
	"strings"
	"time"
)

func (s *Store) SavePublicationAttempt(ctx context.Context, token runtime.Tx, record d.Record) error {
	if err := record.ValidateIdentity(); err != nil {
		return err
	}
	if !strings.HasPrefix(record.AttemptKey, record.ObjectKey+".") || !strings.HasSuffix(record.AttemptKey, ".tmp") {
		return runtime.ErrScope
	}
	epoch := strings.TrimSuffix(strings.TrimPrefix(record.AttemptKey, record.ObjectKey+"."), ".tmp")
	n, err := strconv.ParseInt(epoch, 10, 64)
	if err != nil || n < 1 || strconv.FormatInt(n, 10) != epoch {
		return runtime.ErrScope
	}
	tx, err := s.core.SQL(ctx, token, commonOwner(record.Ref.Owner))
	if err != nil {
		return err
	}
	body, err := json.Marshal(struct {
		Binding    string       `json:"binding"`
		Ref        v.ContentRef `json:"content_ref"`
		ObjectKey  string       `json:"object_key"`
		AttemptKey string       `json:"attempt_key"`
	}{record.PrimaryHolderBinding, record.Ref, record.ObjectKey, record.AttemptKey})
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO `+s.core.Table("content_publication_attempts")+`(tenant_id,owner_id,object_id,attempt_key,body) VALUES($1,$2,$3,$4,$5) ON CONFLICT(tenant_id,owner_id,object_id,attempt_key) DO UPDATE SET body=excluded.body WHERE content_publication_attempts.body=excluded.body`, record.Ref.Owner.TenantID, record.Ref.Owner.OwnerID, record.ObjectID, record.AttemptKey, body)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count != 1 {
		return runtime.ErrScope
	}
	return err
}

func (s *Store) SaveBodyHolder(ctx context.Context, token runtime.Tx, holder d.BodyHolder) error {
	id, key, err := d.VersionIdentity(holder.Identity.Ref)
	if err != nil {
		return err
	}
	if key != holder.Identity.ObjectKey || holder.Identity.HolderID == "" || len(holder.Identity.HolderID) > 128 || holder.Identity.SealID == "" || len(holder.Identity.SealID) > 128 || holder.Responsible == "" || holder.Deadline.IsZero() || (holder.State != "pending" && holder.State != "residual" && holder.State != "erased") {
		return runtime.ErrScope
	}
	tx, err := s.core.SQL(ctx, token, commonOwner(holder.Identity.Ref.Owner))
	if err != nil {
		return err
	}
	var previousBody []byte
	err = tx.QueryRowContext(ctx, `SELECT body FROM `+s.core.Table("content_body_holders")+` WHERE tenant_id=$1 AND owner_id=$2 AND object_id=$3 AND holder_id=$4 FOR UPDATE`, holder.Identity.Ref.Owner.TenantID, holder.Identity.Ref.Owner.OwnerID, id, holder.Identity.HolderID).Scan(&previousBody)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		var previous d.BodyHolder
		if err = json.Unmarshal(previousBody, &previous); err != nil {
			return err
		}
		if previous.Identity != holder.Identity || previous.Kind != holder.Kind || previous.Responsible != holder.Responsible || !previous.Deadline.Equal(holder.Deadline) {
			return runtime.ErrScope
		}
		if previous.State == "erased" && holder.State != "erased" {
			return runtime.ErrScope
		}
	}
	body, err := json.Marshal(holder)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO `+s.core.Table("content_body_holders")+`(tenant_id,owner_id,object_id,holder_id,seal_id,state,body) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(tenant_id,owner_id,object_id,holder_id) DO UPDATE SET state=excluded.state,body=excluded.body WHERE content_body_holders.seal_id=excluded.seal_id`, holder.Identity.Ref.Owner.TenantID, holder.Identity.Ref.Owner.OwnerID, id, holder.Identity.HolderID, holder.Identity.SealID, holder.State, body)
	return err
}

func (s *Store) BodyHolders(ctx context.Context, token runtime.Tx, ref v.ContentRef, cursor string, limit int) (holders []d.BodyHolder, next string, returnErr error) {
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
	rows, err := tx.QueryContext(ctx, `SELECT holder_id,seal_id,state,body FROM `+s.core.Table("content_body_holders")+` WHERE tenant_id=$1 AND owner_id=$2 AND object_id=$3 AND holder_id>$4 ORDER BY holder_id LIMIT $5`, ref.Owner.TenantID, ref.Owner.OwnerID, id, cursor, limit+1)
	if err != nil {
		return nil, "", err
	}
	defer func() { returnErr = errors.Join(returnErr, rows.Close()) }()
	for rows.Next() {
		var holderID, sealID, state string
		var body []byte
		if err = rows.Scan(&holderID, &sealID, &state, &body); err != nil {
			return nil, "", err
		}
		var holder d.BodyHolder
		if err = json.Unmarshal(body, &holder); err != nil {
			return nil, "", err
		}
		if holder.Identity.Ref != ref || holder.Identity.HolderID != holderID || holder.Identity.SealID != sealID || holder.State != state {
			return nil, "", runtime.ErrScope
		}
		holders = append(holders, holder)
		if len(holders) > limit {
			next = holders[limit-1].Identity.HolderID
			holders = holders[:limit]
			break
		}
	}
	return holders, next, rows.Err()
}

func (s *Store) AllBodyHoldersErased(ctx context.Context, token runtime.Tx, ref v.ContentRef, sealID, primaryID string) (bool, error) {
	id, _, err := d.VersionIdentity(ref)
	if err != nil {
		return false, err
	}
	tx, err := s.core.SQL(ctx, token, commonOwner(ref.Owner))
	if err != nil {
		return false, err
	}
	var complete bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM `+s.core.Table("content_body_holders")+` WHERE tenant_id=$1 AND owner_id=$2 AND object_id=$3 AND seal_id=$4 AND holder_id='postgres-staging' AND state='erased') AND EXISTS(SELECT 1 FROM `+s.core.Table("content_body_holders")+` WHERE tenant_id=$1 AND owner_id=$2 AND object_id=$3 AND seal_id=$4 AND holder_id=$5 AND state='erased') AND NOT EXISTS(SELECT 1 FROM `+s.core.Table("content_body_holders")+` WHERE tenant_id=$1 AND owner_id=$2 AND object_id=$3 AND (seal_id<>$4 OR state<>'erased'))`, ref.Owner.TenantID, ref.Owner.OwnerID, id, sealID, primaryID).Scan(&complete)
	return complete, err
}

func (s *Store) AuthoritativeBodyErased(ctx context.Context, token runtime.Tx, ref v.ContentRef, sealID, primaryID string) (bool, error) {
	id, _, err := d.VersionIdentity(ref)
	if err != nil {
		return false, err
	}
	tx, err := s.core.SQL(ctx, token, commonOwner(ref.Owner))
	if err != nil {
		return false, err
	}
	var complete bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM `+s.core.Table("content_body_holders")+` WHERE tenant_id=$1 AND owner_id=$2 AND object_id=$3 AND seal_id=$4 AND holder_id='postgres-staging' AND state='erased') AND EXISTS(SELECT 1 FROM `+s.core.Table("content_body_holders")+` WHERE tenant_id=$1 AND owner_id=$2 AND object_id=$3 AND seal_id=$4 AND holder_id=$5 AND state='erased')`, ref.Owner.TenantID, ref.Owner.OwnerID, id, sealID, primaryID).Scan(&complete)
	return complete, err
}

func (s *Store) PublicationAttempts(ctx context.Context, token runtime.Tx, ref v.ContentRef, cursor string, limit int) (attempts []string, next string, returnErr error) {
	if limit < 1 || limit > 64 || len(cursor) > 128 {
		return nil, "", runtime.ErrWorkBounds
	}
	id, key, err := d.VersionIdentity(ref)
	if err != nil {
		return nil, "", err
	}
	tx, err := s.core.SQL(ctx, token, commonOwner(ref.Owner))
	if err != nil {
		return nil, "", err
	}
	record, err := s.LockVersion(ctx, token, ref)
	if err != nil {
		return nil, "", err
	}
	if record == nil || record.Ref != ref {
		return nil, "", runtime.ErrScope
	}
	rows, err := tx.QueryContext(ctx, `SELECT attempt_key,body FROM `+s.core.Table("content_publication_attempts")+` WHERE tenant_id=$1 AND owner_id=$2 AND object_id=$3 AND attempt_key>$4 ORDER BY attempt_key LIMIT $5`, ref.Owner.TenantID, ref.Owner.OwnerID, id, cursor, limit+1)
	if err != nil {
		return nil, "", err
	}
	defer func() { returnErr = errors.Join(returnErr, rows.Close()) }()
	for rows.Next() {
		var name string
		var body []byte
		if err = rows.Scan(&name, &body); err != nil {
			return nil, "", err
		}
		var registered struct {
			Binding    string       `json:"binding"`
			Ref        v.ContentRef `json:"content_ref"`
			ObjectKey  string       `json:"object_key"`
			AttemptKey string       `json:"attempt_key"`
		}
		if err = json.Unmarshal(body, &registered); err != nil {
			return nil, "", err
		}
		if registered.Ref != ref || registered.ObjectKey != key || registered.AttemptKey != name || registered.Binding != record.PrimaryHolderBinding {
			return nil, "", runtime.ErrScope
		}
		attempts = append(attempts, name)
		if len(attempts) > limit {
			next = attempts[limit-1]
			attempts = attempts[:limit]
			break
		}
	}
	return attempts, next, rows.Err()
}

// This observer starts its own connection transaction after the consumer's
// clear transaction has actually returned. NULL is distinct from empty bytes.
func (s *Store) ObserveStaging(ctx context.Context, ref v.ContentRef) (d.StagingObservation, error) {
	var observed d.StagingObservation
	err := s.core.Within(ctx, commonOwner(ref.Owner), func(ctx context.Context, token runtime.Tx) error {
		record, err := s.LockVersion(ctx, token, ref)
		if err != nil {
			return err
		}
		if record == nil || record.Ref != ref {
			return runtime.ErrScope
		}
		tx, err := s.core.SQL(ctx, token, commonOwner(ref.Owner))
		if err != nil {
			return err
		}
		var absent bool
		if err = tx.QueryRowContext(ctx, `SELECT staging IS NULL FROM `+s.core.Table("content_versions")+` WHERE tenant_id=$1 AND owner_id=$2 AND content_id=$3 AND version=$4`, ref.Owner.TenantID, ref.Owner.OwnerID, ref.ContentID, ref.Version).Scan(&absent); err != nil {
			return err
		}
		observed = d.StagingObservation{Ref: record.Ref, Present: !absent}
		return nil
	})
	return observed, err
}

func (s *Store) LockMetadataPolicy(ctx context.Context, token runtime.Tx, policy d.MetadataPolicy) (*d.MetadataPolicy, error) {
	tx, err := s.core.SQL(ctx, token, commonOwner(policy.Ref.Owner))
	if err != nil {
		return nil, err
	}
	subject, _, err := subjectKey(policy.Subject)
	if err != nil {
		return nil, err
	}
	key, err := json.Marshal([]string{s.schema, string(policy.Ref.Owner.TenantID), string(policy.Ref.Owner.OwnerID), subject, string(policy.Ref.ContentID), string(policy.Ref.Version), policy.Purpose})
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(6,hashtext($1))`, string(key)); err != nil {
		return nil, err
	}
	var body []byte
	err = tx.QueryRowContext(ctx, `SELECT body FROM `+s.core.Table("content_metadata_policies")+` WHERE tenant_id=$1 AND owner_id=$2 AND subject_key=$3 AND content_id=$4 AND version=$5 AND purpose=$6 FOR UPDATE`, policy.Ref.Owner.TenantID, policy.Ref.Owner.OwnerID, subject, policy.Ref.ContentID, policy.Ref.Version, policy.Purpose).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var previous d.MetadataPolicy
	if err = json.Unmarshal(body, &previous); err != nil {
		return nil, err
	}
	return &previous, nil
}

func (s *Store) SaveMetadataPolicy(ctx context.Context, token runtime.Tx, policy d.MetadataPolicy) error {
	tx, err := s.core.SQL(ctx, token, commonOwner(policy.Ref.Owner))
	if err != nil {
		return err
	}
	subject, _, err := subjectKey(policy.Subject)
	if err != nil {
		return err
	}
	body, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO `+s.core.Table("content_metadata_policies")+`(tenant_id,owner_id,subject_key,content_id,version,purpose,revision,valid_until,body) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(tenant_id,owner_id,subject_key,content_id,version,purpose) DO UPDATE SET revision=excluded.revision,valid_until=excluded.valid_until,body=excluded.body`, policy.Ref.Owner.TenantID, policy.Ref.Owner.OwnerID, subject, policy.Ref.ContentID, policy.Ref.Version, policy.Purpose, policy.Revision, policy.ValidUntil, body)
	return err
}

func (s *Store) CheckMetadataPolicy(ctx context.Context, token runtime.Tx, subject v.SubjectBinding, ref v.ContentRef, purpose string, now time.Time) (*d.MetadataPolicy, error) {
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
	var until time.Time
	err = tx.QueryRowContext(ctx, `WITH locked_metadata AS MATERIALIZED (SELECT body,revision,valid_until FROM `+s.core.Table("content_metadata_policies")+` WHERE tenant_id=$1 AND owner_id=$2 AND subject_key=$3 AND content_id=$4 AND version=$5 AND purpose=$6 FOR SHARE) SELECT body,revision,valid_until,clock_timestamp() FROM locked_metadata`, ref.Owner.TenantID, ref.Owner.OwnerID, key, ref.ContentID, ref.Version, purpose).Scan(&body, &revision, &until, &now)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var policy d.MetadataPolicy
	if err = json.Unmarshal(body, &policy); err != nil {
		return nil, err
	}
	if policy.Revision != revision || !policy.ValidUntil.Equal(until) {
		return nil, runtime.ErrScope
	}
	actual, _, err := subjectKey(policy.Subject)
	if err != nil {
		return nil, err
	}
	if actual != key || policy.Ref.Owner != ref.Owner || policy.Ref.ContentID != ref.ContentID || policy.Ref.Version != ref.Version || policy.Purpose != purpose || !now.Before(policy.ValidUntil) {
		return nil, nil
	}
	return &policy, nil
}
