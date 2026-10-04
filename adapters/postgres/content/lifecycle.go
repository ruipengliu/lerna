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
		Ref        v.ContentRef `json:"content_ref"`
		ObjectKey  string       `json:"object_key"`
		AttemptKey string       `json:"attempt_key"`
	}{record.Ref, record.ObjectKey, record.AttemptKey})
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
