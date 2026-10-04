package content

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/ruipengliu/lerna/contract"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	contentdomain "github.com/ruipengliu/lerna/domain/content"
	"github.com/ruipengliu/lerna/runtime"
)

func subjectKey(subject v.SubjectBinding) (string, []byte, error) {
	data, err := v.Encode(subject)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), data, nil
}

// InstallFixturePolicy is a trusted host-management seam. Public clients cannot
// install, expand, or change policy through content.put payloads.
func (s *Store) InstallFixturePolicy(ctx context.Context, policy contentdomain.FixturePolicy, expected int64) error {
	if _, err := v.Encode(policy.Ref); err != nil {
		return err
	}
	if policy.Revision != expected+1 || expected < 0 || policy.ValidUntil.IsZero() || policy.RetainUntil.IsZero() || policy.Purpose == "" || policy.Subject.TenantID != policy.Ref.Owner.TenantID {
		return errors.New("invalid explicit Content fixture policy")
	}
	subject, data, err := subjectKey(policy.Subject)
	if err != nil {
		return err
	}
	_ = data
	body, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	return s.core.Within(ctx, commonOwner(policy.Ref.Owner), func(ctx context.Context, token runtime.Tx) error {
		tx, err := s.core.SQL(ctx, token, commonOwner(policy.Ref.Owner))
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(3,hashtext($1))`, s.schema+subject+string(policy.Ref.ContentID)+string(policy.Ref.Version)+policy.Purpose); err != nil {
			return err
		}
		var revision int64
		err = tx.QueryRowContext(ctx, `SELECT revision FROM `+s.core.Table("content_fixture_policies")+` WHERE tenant_id=$1 AND owner_id=$2 AND subject_key=$3 AND content_id=$4 AND version=$5 AND purpose=$6 FOR UPDATE`, policy.Ref.Owner.TenantID, policy.Ref.Owner.OwnerID, subject, policy.Ref.ContentID, policy.Ref.Version, policy.Purpose).Scan(&revision)
		if errors.Is(err, sql.ErrNoRows) {
			revision = 0
		} else if err != nil {
			return err
		}
		if revision != expected {
			return runtime.ErrClaim
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO `+s.core.Table("content_fixture_policies")+`(tenant_id,owner_id,subject_key,content_id,version,purpose,revision,valid_until,body)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)ON CONFLICT(tenant_id,owner_id,subject_key,content_id,version,purpose)DO UPDATE SET revision=excluded.revision,valid_until=excluded.valid_until,body=excluded.body`, policy.Ref.Owner.TenantID, policy.Ref.Owner.OwnerID, subject, policy.Ref.ContentID, policy.Ref.Version, policy.Purpose, policy.Revision, policy.ValidUntil, body)
		return err
	})
}
func (s *Store) CheckPolicy(ctx context.Context, token runtime.Tx, subject v.SubjectBinding, ref v.ContentRef, purpose, action string, now time.Time) (*contentdomain.FixturePolicy, error) {
	tx, err := s.core.SQL(ctx, token, commonOwner(ref.Owner))
	if err != nil {
		return nil, err
	}
	key, _, err := subjectKey(subject)
	if err != nil {
		return nil, err
	}
	var body []byte
	err = tx.QueryRowContext(ctx, `SELECT body FROM `+s.core.Table("content_fixture_policies")+` WHERE tenant_id=$1 AND owner_id=$2 AND subject_key=$3 AND content_id=$4 AND version=$5 AND purpose=$6 FOR SHARE`, ref.Owner.TenantID, ref.Owner.OwnerID, key, ref.ContentID, ref.Version, purpose).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var policy contentdomain.FixturePolicy
	if err = json.Unmarshal(body, &policy); err != nil {
		return nil, err
	}
	if policy.Ref.Owner != ref.Owner || policy.Ref.ContentID != ref.ContentID || policy.Ref.Version != ref.Version || policy.Purpose != purpose || !now.Before(policy.ValidUntil) || !now.Before(policy.RetainUntil) {
		return nil, nil
	}
	match, _, err := subjectKey(policy.Subject)
	if err != nil {
		return nil, err
	}
	if match != key {
		return nil, nil
	}
	allowed := false
	switch action {
	case "read":
		allowed = policy.Read
	case "process":
		allowed = policy.Process
	case "save":
		allowed = policy.Save
	case "sync":
		allowed = policy.Sync
	case "disclose":
		allowed = policy.Disclose
	}
	if !allowed {
		return nil, nil
	}
	return &policy, nil
}
func (s *Store) InstallFixtureCommandReader(ctx context.Context, owner v.OwnerRef, subject v.SubjectBinding, until time.Time) error {
	if subject.TenantID != owner.TenantID || until.IsZero() {
		return errors.New("invalid fixture command reader")
	}
	key, data, err := subjectKey(subject)
	if err != nil {
		return err
	}
	return s.core.Within(ctx, commonOwner(owner), func(ctx context.Context, token runtime.Tx) error {
		tx, err := s.core.SQL(ctx, token, commonOwner(owner))
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO `+s.core.Table("content_fixture_command_readers")+`(tenant_id,owner_id,subject_key,subject,valid_until)VALUES($1,$2,$3,$4,$5)ON CONFLICT(tenant_id,owner_id,subject_key)DO UPDATE SET subject=excluded.subject,valid_until=excluded.valid_until`, owner.TenantID, owner.OwnerID, key, data, until)
		return err
	})
}
func (s *Store) CheckCommandReader(ctx context.Context, token runtime.Tx, subject v.SubjectBinding, now time.Time) (bool, error) {
	tx, err := s.core.LocalSQL(ctx, token)
	if err != nil {
		return false, err
	}
	key, data, err := subjectKey(subject)
	if err != nil {
		return false, err
	}
	var stored []byte
	var until time.Time
	err = tx.QueryRowContext(ctx, `SELECT subject,valid_until FROM `+s.core.Table("content_fixture_command_readers")+` WHERE tenant_id=$1 AND owner_id=$2 AND subject_key=$3 FOR SHARE`, token.Owner().TenantID, token.Owner().OwnerID, key).Scan(&stored, &until)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil && string(data) == string(stored) && now.Before(until), err
}

func commonOwner(owner v.OwnerRef) contract.OwnerRef {
	return contract.OwnerRef{TenantID: contract.ID(owner.TenantID), OwnerID: contract.ID(owner.OwnerID)}
}
