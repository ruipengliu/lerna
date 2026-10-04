package content

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	d "github.com/ruipengliu/lerna/domain/content"
	"github.com/ruipengliu/lerna/runtime"
	"time"
)

func (s *Store) LockPolicy(ctx context.Context, token runtime.Tx, policy d.FixturePolicy) (*d.FixturePolicy, error) {
	tx, err := s.core.SQL(ctx, token, commonOwner(policy.Ref.Owner))
	if err != nil {
		return nil, err
	}
	subject, _, err := subjectKey(policy.Subject)
	if err != nil {
		return nil, err
	}
	key, _ := json.Marshal([]string{s.schema, subject, string(policy.Ref.ContentID), string(policy.Ref.Version), policy.Purpose})
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(3,hashtext($1))`, string(key)); err != nil {
		return nil, err
	}
	var body []byte
	err = tx.QueryRowContext(ctx, `SELECT body FROM `+s.core.Table("content_fixture_policies")+` WHERE tenant_id=$1 AND owner_id=$2 AND subject_key=$3 AND content_id=$4 AND version=$5 AND purpose=$6 FOR UPDATE`, policy.Ref.Owner.TenantID, policy.Ref.Owner.OwnerID, subject, policy.Ref.ContentID, policy.Ref.Version, policy.Purpose).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out d.FixturePolicy
	err = json.Unmarshal(body, &out)
	return &out, err
}
func (s *Store) SavePolicy(ctx context.Context, token runtime.Tx, policy d.FixturePolicy) error {
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
	_, err = tx.ExecContext(ctx, `INSERT INTO `+s.core.Table("content_fixture_policies")+`(tenant_id,owner_id,subject_key,content_id,version,purpose,revision,valid_until,body)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)ON CONFLICT(tenant_id,owner_id,subject_key,content_id,version,purpose)DO UPDATE SET revision=excluded.revision,valid_until=excluded.valid_until,body=excluded.body`, policy.Ref.Owner.TenantID, policy.Ref.Owner.OwnerID, subject, policy.Ref.ContentID, policy.Ref.Version, policy.Purpose, policy.Revision, policy.ValidUntil, body)
	return err
}

func (s *Store) PoliciesForVersion(ctx context.Context, token runtime.Tx, ref v.ContentRef, cursor string, limit int) ([]d.FixturePolicy, string, error) {
	tx, err := s.core.SQL(ctx, token, commonOwner(ref.Owner))
	if err != nil {
		return nil, "", err
	}
	if limit < 1 || limit > 64 {
		return nil, "", runtime.ErrWorkBounds
	}
	rows, err := tx.QueryContext(ctx, `SELECT body,subject_key||':'||purpose AS cursor FROM `+s.core.Table("content_fixture_policies")+` WHERE tenant_id=$1 AND owner_id=$2 AND content_id=$3 AND version=$4 AND subject_key||':'||purpose>$5 ORDER BY subject_key||':'||purpose LIMIT $6 FOR UPDATE`, ref.Owner.TenantID, ref.Owner.OwnerID, ref.ContentID, ref.Version, cursor, limit+1)
	if err != nil {
		return nil, "", err
	}
	return readPoliciesForVersionPage(rows, limit)
}

func readPoliciesForVersionPage(rows *sql.Rows, limit int) (policies []d.FixturePolicy, next string, resultErr error) {
	var err error
	defer func() { resultErr = errors.Join(resultErr, rows.Err(), rows.Close()) }()
	policies = []d.FixturePolicy{}
	last := ""
	for rows.Next() {
		var body []byte
		var key string
		if err = rows.Scan(&body, &key); err != nil {
			return nil, "", err
		}
		if len(policies) == limit {
			next = last
			break
		}
		var policy d.FixturePolicy
		if err = json.Unmarshal(body, &policy); err != nil {
			return nil, "", err
		}
		policies = append(policies, policy)
		last = key
	}
	return policies, next, nil
}
func (s *Store) LockSourceIndex(ctx context.Context, token runtime.Tx) (int64, error) {
	tx, err := s.core.LocalSQL(ctx, token)
	if err != nil {
		return 0, err
	}
	key, _ := json.Marshal([]string{s.schema, string(token.Owner().TenantID), string(token.Owner().OwnerID)})
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(5,hashtext($1))`, string(key)); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO `+s.core.Table("content_source_generation")+`(tenant_id,owner_id)VALUES($1,$2)ON CONFLICT DO NOTHING`, token.Owner().TenantID, token.Owner().OwnerID); err != nil {
		return 0, err
	}
	var generation int64
	err = tx.QueryRowContext(ctx, `SELECT generation FROM `+s.core.Table("content_source_generation")+` WHERE tenant_id=$1 AND owner_id=$2 FOR UPDATE`, token.Owner().TenantID, token.Owner().OwnerID).Scan(&generation)
	return generation, err
}
func (s *Store) UnindexedVersions(ctx context.Context, token runtime.Tx, limit int) ([]d.LegacySourcePage, error) {
	tx, err := s.core.LocalSQL(ctx, token)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT v.body,COALESCE(i.policy_cursor,'') FROM `+s.core.Table("content_versions")+` v LEFT JOIN `+s.core.Table("content_source_versions")+` i USING(tenant_id,owner_id,object_id) WHERE v.tenant_id=$1 AND v.owner_id=$2 AND NOT COALESCE(i.indexed AND i.maintenance_complete,false) ORDER BY v.object_id LIMIT $3`, token.Owner().TenantID, token.Owner().OwnerID, limit)
	if err != nil {
		return nil, err
	}
	return readUnindexedVersionsRows(rows)
}

func readUnindexedVersionsRows(rows *sql.Rows) (out []d.LegacySourcePage, resultErr error) {
	var err error
	defer func() { resultErr = errors.Join(resultErr, rows.Err(), rows.Close()) }()
	out = []d.LegacySourcePage{}
	for rows.Next() {
		var body []byte
		var cursor string
		if err = rows.Scan(&body, &cursor); err != nil {
			return nil, err
		}
		var record d.Record
		if err = json.Unmarshal(body, &record); err != nil {
			return nil, err
		}
		out = append(out, d.LegacySourcePage{Record: record, PolicyCursor: cursor})
	}
	return out, nil
}
func (s *Store) SaveSources(ctx context.Context, token runtime.Tx, ref v.ContentRef, sources []v.ContentRef) error {
	generation, err := s.LockSourceIndex(ctx, token)
	if err != nil {
		return err
	}
	tx, err := s.core.LocalSQL(ctx, token)
	if err != nil {
		return err
	}
	id, _, err := d.VersionIdentity(ref)
	if err != nil {
		return err
	}
	var indexed bool
	err = tx.QueryRowContext(ctx, `SELECT generation,indexed FROM `+s.core.Table("content_source_versions")+` WHERE tenant_id=$1 AND owner_id=$2 AND object_id=$3`, token.Owner().TenantID, token.Owner().OwnerID, id).Scan(&generation, &indexed)
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx, `UPDATE `+s.core.Table("content_source_generation")+` SET generation=generation+1 WHERE tenant_id=$1 AND owner_id=$2 RETURNING generation`, token.Owner().TenantID, token.Owner().OwnerID).Scan(&generation)
	}
	if err != nil {
		return err
	}
	if indexed {
		return nil
	}
	body, err := v.Encode(ref)
	if err != nil {
		return err
	}
	for _, source := range sources {
		ancestor, _, err := d.VersionIdentity(source)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO `+s.core.Table("content_source_edges")+`(tenant_id,owner_id,ancestor_id,descendant_id,ref,generation)VALUES($1,$2,$3,$4,$5,$6)ON CONFLICT DO NOTHING`, token.Owner().TenantID, token.Owner().OwnerID, ancestor, id, body, generation); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO `+s.core.Table("content_source_versions")+`(tenant_id,owner_id,object_id,generation,indexed,maintenance_complete)VALUES($1,$2,$3,$4,true,true)ON CONFLICT(tenant_id,owner_id,object_id)DO UPDATE SET indexed=true`, token.Owner().TenantID, token.Owner().OwnerID, id, generation)
	return err
}

func (s *Store) SaveBackfillProgress(ctx context.Context, token runtime.Tx, ref v.ContentRef, cursor string, complete bool) error {
	tx, err := s.core.SQL(ctx, token, commonOwner(ref.Owner))
	if err != nil {
		return err
	}
	id, _, err := d.VersionIdentity(ref)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE `+s.core.Table("content_source_versions")+` SET policy_cursor=$4,maintenance_complete=$5 WHERE tenant_id=$1 AND owner_id=$2 AND object_id=$3`, token.Owner().TenantID, token.Owner().OwnerID, id, cursor, complete)
	return err
}
func (s *Store) SaveChange(ctx context.Context, token runtime.Tx, change d.PolicyChange) error {
	tx, err := s.core.LocalSQL(ctx, token)
	if err != nil {
		return err
	}
	body, err := json.Marshal(change)
	if err != nil {
		return err
	}
	id, _, err := d.VersionIdentity(change.Policy.Ref)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO `+s.core.Table("content_policy_changes")+`(tenant_id,owner_id,change_key,object_id,state,due_at,body)VALUES($1,$2,$3,$4,$5,$6,$7)ON CONFLICT(tenant_id,owner_id,change_key)DO UPDATE SET state=excluded.state,due_at=excluded.due_at,body=excluded.body`, token.Owner().TenantID, token.Owner().OwnerID, change.Key, id, change.State, change.Due, body)
	return err
}
func (s *Store) ReadChange(ctx context.Context, token runtime.Tx, key string) (*d.PolicyChange, error) {
	tx, err := s.core.LocalSQL(ctx, token)
	if err != nil {
		return nil, err
	}
	var body []byte
	err = tx.QueryRowContext(ctx, `SELECT body FROM `+s.core.Table("content_policy_changes")+` WHERE tenant_id=$1 AND owner_id=$2 AND change_key=$3 FOR UPDATE`, token.Owner().TenantID, token.Owner().OwnerID, key).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var change d.PolicyChange
	err = json.Unmarshal(body, &change)
	return &change, err
}
func (s *Store) PendingChanges(ctx context.Context, token runtime.Tx, id string) ([]d.PolicyChange, error) {
	tx, err := s.core.LocalSQL(ctx, token)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT body FROM `+s.core.Table("content_policy_changes")+` WHERE tenant_id=$1 AND owner_id=$2 AND object_id=$3 AND state IN('pending','scheduled') AND due_at<=clock_timestamp() ORDER BY due_at,change_key LIMIT 64 FOR UPDATE`, token.Owner().TenantID, token.Owner().OwnerID, id)
	if err != nil {
		return nil, err
	}
	return readPendingChangesRows(rows)
}

func readPendingChangesRows(rows *sql.Rows) (out []d.PolicyChange, resultErr error) {
	var err error
	defer func() { resultErr = errors.Join(resultErr, rows.Err(), rows.Close()) }()
	out = []d.PolicyChange{}
	for rows.Next() {
		var body []byte
		if err = rows.Scan(&body); err != nil {
			return nil, err
		}
		var change d.PolicyChange
		if err = json.Unmarshal(body, &change); err != nil {
			return nil, err
		}
		out = append(out, change)
	}
	return out, nil
}

// HasSourceCoverage reads one exact registered edge, without changing a watermark.
func (s *Store) HasSourceCoverage(ctx context.Context, token runtime.Tx, source, target v.ContentRef, watermark int64) (bool, error) {
	tx, err := s.core.SQL(ctx, token, commonOwner(source.Owner))
	if err != nil {
		return false, err
	}
	if source.Owner != target.Owner {
		return false, runtime.ErrScope
	}
	ancestor, _, err := d.VersionIdentity(source)
	if err != nil {
		return false, err
	}
	descendant, _, err := d.VersionIdentity(target)
	if err != nil {
		return false, err
	}
	ref, err := v.Encode(target)
	if err != nil {
		return false, err
	}
	var covered bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM `+s.core.Table("content_source_edges")+` WHERE tenant_id=$1 AND owner_id=$2 AND ancestor_id=$3 AND descendant_id=$4 AND generation<=$5 AND ref=$6)`, source.Owner.TenantID, source.Owner.OwnerID, ancestor, descendant, watermark, ref).Scan(&covered)
	return covered, err
}

func (s *Store) Descendants(ctx context.Context, token runtime.Tx, source v.ContentRef, watermark int64, cursor string, limit int) ([]v.ContentRef, string, error) {
	tx, err := s.core.LocalSQL(ctx, token)
	if err != nil {
		return nil, "", err
	}
	id, _, err := d.VersionIdentity(source)
	if err != nil {
		return nil, "", err
	}
	rows, err := tx.QueryContext(ctx, `SELECT ref,descendant_id FROM `+s.core.Table("content_source_edges")+` WHERE tenant_id=$1 AND owner_id=$2 AND ancestor_id=$3 AND generation<=$4 AND descendant_id>$5 ORDER BY descendant_id LIMIT $6`, token.Owner().TenantID, token.Owner().OwnerID, id, watermark, cursor, limit+1)
	if err != nil {
		return nil, "", err
	}
	return readDescendantsPage(rows, limit)
}

func readDescendantsPage(rows *sql.Rows, limit int) (out []v.ContentRef, next string, resultErr error) {
	var err error
	defer func() { resultErr = errors.Join(resultErr, rows.Err(), rows.Close()) }()
	out = []v.ContentRef{}
	next = ""
	last := ""
	for rows.Next() {
		var body []byte
		var key string
		if err = rows.Scan(&body, &key); err != nil {
			return nil, "", err
		}
		if len(out) == limit {
			next = last
			break
		}
		ref, err := v.Decode[v.ContentRef](body)
		if err != nil {
			return nil, "", err
		}
		out = append(out, ref)
		last = key
	}
	return out, next, nil
}
func (s *Store) SaveResponsibility(ctx context.Context, token runtime.Tx, responsibility d.CleanupResponsibility) error {
	tx, err := s.core.LocalSQL(ctx, token)
	if err != nil {
		return err
	}
	id, _, err := d.VersionIdentity(responsibility.Ref)
	if err != nil {
		return err
	}
	var previousBody []byte
	err = tx.QueryRowContext(ctx, `SELECT body FROM `+s.core.Table("content_cleanup_responsibilities")+` WHERE tenant_id=$1 AND owner_id=$2 AND change_key=$3 AND object_id=$4 FOR UPDATE`, token.Owner().TenantID, token.Owner().OwnerID, responsibility.ChangeKey, id).Scan(&previousBody)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		var previous d.CleanupResponsibility
		if err = json.Unmarshal(previousBody, &previous); err != nil {
			return err
		}
		if previous.Ref != responsibility.Ref {
			return runtime.ErrScope
		}
		if previous.BodyCleanup == "pending" {
			// Renewal is a current-use observation, never native cleanup ACK.
			responsibility.BodyCleanup = previous.BodyCleanup
			responsibility.Residual = previous.Residual
			if previous.Reason != "" {
				responsibility.Reason = previous.Reason
			}
			responsibility.StagingHolder = responsibility.StagingHolder || previous.StagingHolder
			responsibility.ObjectHolder = responsibility.ObjectHolder || previous.ObjectHolder
			if previous.AttemptKey != "" {
				responsibility.AttemptKey = previous.AttemptKey
			}
		}
		if previous.Deadline.Before(responsibility.Deadline) {
			responsibility.Deadline = previous.Deadline
		}
		union := map[string]bool{}
		for _, action := range previous.Actions {
			union[action] = true
		}
		for _, action := range responsibility.Actions {
			union[action] = true
		}
		responsibility.Actions = []string{}
		for _, action := range []string{"read", "process", "save", "sync", "disclose"} {
			if union[action] {
				responsibility.Actions = append(responsibility.Actions, action)
			}
		}
	}
	body, err := json.Marshal(responsibility)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO `+s.core.Table("content_cleanup_responsibilities")+`(tenant_id,owner_id,change_key,object_id,body)VALUES($1,$2,$3,$4,$5)ON CONFLICT(tenant_id,owner_id,change_key,object_id)DO UPDATE SET body=excluded.body`, token.Owner().TenantID, token.Owner().OwnerID, responsibility.ChangeKey, id, body)
	return err
}
func (s *Store) Responsibilities(ctx context.Context, token runtime.Tx, key, cursor string, limit int) ([]d.CleanupResponsibility, string, error) {
	tx, err := s.core.LocalSQL(ctx, token)
	if err != nil {
		return nil, "", err
	}
	rows, err := tx.QueryContext(ctx, `SELECT body,object_id FROM `+s.core.Table("content_cleanup_responsibilities")+` WHERE tenant_id=$1 AND owner_id=$2 AND change_key=$3 AND object_id>$4 ORDER BY object_id LIMIT $5`, token.Owner().TenantID, token.Owner().OwnerID, key, cursor, limit+1)
	if err != nil {
		return nil, "", err
	}
	return readResponsibilitiesPage(rows, limit)
}

func readResponsibilitiesPage(rows *sql.Rows, limit int) (out []d.CleanupResponsibility, next string, resultErr error) {
	var err error
	defer func() { resultErr = errors.Join(resultErr, rows.Err(), rows.Close()) }()
	out = []d.CleanupResponsibility{}
	next = ""
	last := ""
	for rows.Next() {
		var body []byte
		var id string
		if err = rows.Scan(&body, &id); err != nil {
			return nil, "", err
		}
		if len(out) == limit {
			next = last
			break
		}
		var responsibility d.CleanupResponsibility
		if err = json.Unmarshal(body, &responsibility); err != nil {
			return nil, "", err
		}
		out = append(out, responsibility)
		last = id
	}
	return out, next, nil
}
func (s *Store) AdmissionChanges(ctx context.Context, token runtime.Tx, ref v.ContentRef, cursor string, limit int) (out []d.PolicyChange, next string, resultErr error) {
	tx, err := s.core.SQL(ctx, token, commonOwner(ref.Owner))
	if err != nil {
		return nil, "", err
	}
	if limit < 1 || limit > 64 {
		return nil, "", runtime.ErrWorkBounds
	}
	body, err := json.Marshal(ref)
	if err != nil {
		return nil, "", err
	}
	rows, err := tx.QueryContext(ctx, `SELECT body,change_key FROM `+s.core.Table("content_policy_changes")+` WHERE tenant_id=$1 AND owner_id=$2 AND convert_from(body,'UTF8')::jsonb->'admission_target'=$3::jsonb AND change_key>$4 ORDER BY change_key LIMIT $5`, ref.Owner.TenantID, ref.Owner.OwnerID, string(body), cursor, limit+1)
	if err != nil {
		return nil, "", err
	}
	return readAdmissionChangesPage(rows, limit)
}

func readAdmissionChangesPage(rows *sql.Rows, limit int) (out []d.PolicyChange, next string, resultErr error) {
	var err error
	defer func() { resultErr = errors.Join(resultErr, rows.Err(), rows.Close()) }()
	out = []d.PolicyChange{}
	last := ""
	for rows.Next() {
		var body []byte
		var key string
		if err = rows.Scan(&body, &key); err != nil {
			return nil, "", err
		}
		if len(out) == limit {
			next = last
			break
		}
		var change d.PolicyChange
		if err = json.Unmarshal(body, &change); err != nil {
			return nil, "", err
		}
		out = append(out, change)
		last = key
	}
	return out, next, nil
}

func (s *Store) NextPolicyWork(ctx context.Context, token runtime.Tx, id string) (int64, error) {
	tx, err := s.core.LocalSQL(ctx, token)
	if err != nil {
		return 0, err
	}
	var revision int64
	err = tx.QueryRowContext(ctx, `INSERT INTO `+s.core.Table("content_policy_work")+`(tenant_id,owner_id,object_id,revision)VALUES($1,$2,$3,1)ON CONFLICT(tenant_id,owner_id,object_id)DO UPDATE SET revision=content_policy_work.revision+1 RETURNING revision`, token.Owner().TenantID, token.Owner().OwnerID, id).Scan(&revision)
	return revision, err
}

func (s *Store) ScheduleRetention(ctx context.Context, tx runtime.Tx, policy *d.FixturePolicy, record d.Record, sources []d.Record, budget time.Duration) error {
	return d.ScheduleRetention(ctx, tx, s, policy, record, sources, budget)
}
func (s *Store) AdvancePolicyJob(ctx context.Context, tx runtime.Tx, job runtime.Job, record d.Record, worker string, lease, budget time.Duration) (bool, error) {
	return d.AdvancePolicyJob(ctx, tx, s, job, record, worker, lease, budget)
}

func (s *Store) NextPolicyDue(ctx context.Context, token runtime.Tx, id string) (time.Time, error) {
	tx, err := s.core.LocalSQL(ctx, token)
	if err != nil {
		return time.Time{}, err
	}
	var due sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT min(due_at) FROM `+s.core.Table("content_policy_changes")+` WHERE tenant_id=$1 AND owner_id=$2 AND object_id=$3 AND state IN('pending','scheduled')`, token.Owner().TenantID, token.Owner().OwnerID, id).Scan(&due)
	return due.Time, err
}
