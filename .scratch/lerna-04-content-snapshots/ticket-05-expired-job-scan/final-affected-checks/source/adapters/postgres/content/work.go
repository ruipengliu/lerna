package content

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/ruipengliu/lerna/contract"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	d "github.com/ruipengliu/lerna/domain/content"
	"github.com/ruipengliu/lerna/runtime"
)

// Content selects its own existing responsibilities before the bounded page.
// The frozen Core.Scan and all original Job states/deadlines are unchanged.
func (s *Store) ScanContentPhase(ctx context.Context, token runtime.Tx, now time.Time, phase string, limit int) ([]runtime.Job, error) {
	if limit < 1 || limit > 64 || (phase != "publish" && phase != "policy_propagation") {
		return nil, runtime.ErrWorkBounds
	}
	tx, err := s.core.LocalSQL(ctx, token)
	if err != nil {
		return nil, err
	}
	owner := token.Owner()
	rows, err := tx.QueryContext(ctx, `SELECT job_id,object_kind,object_id,phase,work_revision,completed_revision,state,due_at,true,NULL::bytea FROM `+s.core.Table("jobs")+` WHERE tenant_id=$1 AND owner_id=$2 AND state <> 'done' AND scan_at <= $3 AND lease_epoch < 9223372036854775807 AND phase=$4 ORDER BY scan_at,job_id LIMIT $5`, owner.TenantID, owner.OwnerID, now, phase, limit)
	if err != nil {
		return nil, err
	}
	return readContentJobs(rows, owner)
}

// Valid different saving subjects can be excluded. Unknown/invalid shapes are
// selected as errors, rather than COALESCE'd into another consumer's scope.
const cleanupSubjectShape = `CASE WHEN jsonb_typeof(subject)='object' THEN
 (subject-ARRAY['tenant_id','subject_id','delegation_chain'])='{}'::jsonb
 AND jsonb_typeof(subject->'tenant_id')='string' AND subject->>'tenant_id'=$1
 AND jsonb_typeof(subject->'subject_id')='string' AND subject->>'subject_id' ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
 AND CASE WHEN jsonb_typeof(subject->'delegation_chain')='array' THEN
  jsonb_array_length(subject->'delegation_chain')<=16
  AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(subject->'delegation_chain') e WHERE
   (CASE WHEN jsonb_typeof(e)='object' THEN
    (e-ARRAY['tenant_id','subject_id'])='{}'::jsonb
    AND jsonb_typeof(e->'tenant_id')='string' AND e->>'tenant_id' ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
    AND jsonb_typeof(e->'subject_id')='string' AND e->>'subject_id' ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
   ELSE false END) IS NOT TRUE)
 ELSE false END
 ELSE false END`

// These finite groups describe only today's persisted Go Record, not a schema
// interpreter. json retains integer lexemes; jsonb is only a structural hint.
const cleanupRecordDecodeShape = `CASE WHEN raw_text IS JSON OBJECT WITH UNIQUE KEYS THEN
 NOT EXISTS(SELECT 1 FROM json_each(raw_record) e WHERE e.key NOT IN (
  'legacy_primary_qualification_id','legacy_primary_evidence_digest','primary_holder_binding',
  'content_ref','sources','purpose','subject','tuple_digest','object_id','object_key',
  'requested_retain_until','effective_retain_until','current_retain_until','admitted_at',
  'publish_deadline','io_deadline','publication','failure','revision','attempts',
  'max_publication_attempts','attempt_key','staging_holder','object_holder','cleanup_pending','body_seal','body_gone'))
 AND NOT EXISTS(SELECT 1 FROM json_each(raw_record) e WHERE e.key IN (
  'legacy_primary_qualification_id','legacy_primary_evidence_digest','primary_holder_binding',
  'purpose','tuple_digest','object_id','object_key','requested_retain_until','effective_retain_until',
  'current_retain_until','admitted_at','publish_deadline','io_deadline','publication','failure','attempt_key')
  AND json_typeof(e.value) NOT IN ('string','null'))
 AND NOT EXISTS(SELECT 1 FROM json_each(raw_record) e WHERE e.key IN (
  'staging_holder','object_holder','cleanup_pending','body_gone')
  AND json_typeof(e.value) NOT IN ('boolean','null'))
 AND NOT EXISTS(SELECT 1 FROM json_each(raw_record) e WHERE e.key IN ('revision','attempts','max_publication_attempts')
  AND (CASE WHEN json_typeof(e.value)='null' THEN true
   WHEN json_typeof(e.value)='number' AND e.value::text ~ '^-?(0|[1-9][0-9]*)$' THEN
    CASE WHEN e.key='revision' THEN e.value::text::numeric BETWEEN -9223372036854775808 AND 9223372036854775807
    ELSE e.value::text::numeric BETWEEN -$8::numeric-1 AND $8::numeric END
   ELSE false END) IS NOT TRUE)
 AND CASE WHEN raw_record->'sources' IS NULL OR json_typeof(raw_record->'sources')='null' THEN true
  WHEN json_typeof(raw_record->'sources')='array' THEN NOT EXISTS(
   SELECT 1 FROM json_array_elements(raw_record->'sources') source WHERE
   (CASE WHEN json_typeof(source)='object' THEN
    NOT EXISTS(SELECT 1 FROM json_each(source) e WHERE e.key NOT IN ('owner','content_id','version','hash','media_type','byte_length'))
    AND source->'owner' IS NOT NULL
    AND CASE WHEN json_typeof(source->'owner')='object' THEN
     NOT EXISTS(SELECT 1 FROM json_each(source->'owner') e WHERE e.key NOT IN ('tenant_id','owner_id'))
     AND json_typeof(source->'owner'->'tenant_id')='string' AND json_typeof(source->'owner'->'owner_id')='string'
    ELSE false END
    AND json_typeof(source->'content_id')='string' AND json_typeof(source->'version')='string'
    AND json_typeof(source->'hash')='string' AND json_typeof(source->'media_type')='string'
    AND json_typeof(source->'byte_length')='string'
   ELSE false END) IS NOT TRUE)
  ELSE false END
 AND CASE WHEN json_typeof(raw_seal)='object' THEN
  NOT EXISTS(SELECT 1 FROM json_each(raw_seal) e WHERE e.key NOT IN (
   'policy_change_key','primary_holder_binding','primary_holder_id','id','content_ref','subject','purpose','started_at','deadline'))
  AND NOT EXISTS(SELECT 1 FROM json_each(raw_seal) e WHERE e.key IN (
   'policy_change_key','primary_holder_binding','primary_holder_id','id','purpose')
   AND json_typeof(e.value) NOT IN ('string','null'))
  AND (raw_seal->'started_at')::text ~ '^"[0-9]{4}-(0[1-9]|1[0-2])-(0[1-9]|[12][0-9]|3[01])T([01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9]([.][0-9]{1,9})?(Z|[+-]([01][0-9]|2[0-3]):[0-5][0-9])"$'
  AND (raw_seal->'deadline')::text ~ '^"[0-9]{4}-(0[1-9]|1[0-2])-(0[1-9]|[12][0-9]|3[01])T([01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9]([.][0-9]{1,9})?(Z|[+-]([01][0-9]|2[0-3]):[0-5][0-9])"$'
 ELSE false END
 ELSE false END`

func (s *Store) ScanBodyCleanup(ctx context.Context, token runtime.Tx, now time.Time, subject v.SubjectBinding, primary string, limit int) ([]runtime.Job, error) {
	if limit < 1 || limit > 64 || len(primary) < 1 || len(primary) > 128 {
		return nil, runtime.ErrWorkBounds
	}
	encoded, err := v.Encode(subject)
	if err != nil {
		return nil, err
	}
	if string(subject.TenantID) != string(token.Owner().TenantID) {
		return nil, runtime.ErrScope
	}
	tx, err := s.core.LocalSQL(ctx, token)
	if err != nil {
		return nil, err
	}
	owner := token.Owner()
	maxInt := int64(1<<(strconv.IntSize-1) - 1)
	// The LEFT JOIN retains a missing original record. Parsing errors remain
	// actual PG errors; invalid scope/seal shapes remain visible error candidates.
	// Deadline conversion only preselects; +1us is conservative for PG precision.
	query := `WITH candidates AS (
 SELECT j.*,v.body,v.content_id,v.version,v.object_key,v.tuple_digest,v.publication,v.revision,v.primary_holder_binding,v.body_gone,
  convert_from(v.body,'UTF8') AS raw_text,convert_from(v.body,'UTF8')::json AS raw_record,
  convert_from(v.body,'UTF8')::jsonb AS record,convert_from(v.body_seal,'UTF8')::jsonb AS stored_seal,
  convert_from(v.body_seal,'UTF8') AS stored_seal_text
 FROM ` + s.core.Table("jobs") + ` j LEFT JOIN ` + s.core.Table("content_versions") + ` v
 ON v.tenant_id=j.tenant_id AND v.owner_id=j.owner_id AND v.object_id=j.object_id
 WHERE j.tenant_id=$1 AND j.owner_id=$2 AND j.state <> 'done' AND j.scan_at <= $3
 AND j.lease_epoch < 9223372036854775807 AND j.phase='body_cleanup'
 ), shapes AS (
 SELECT candidates.*,record->'subject' AS subject,record->'body_seal' AS seal,record->'content_ref' AS ref,raw_record->'body_seal' AS raw_seal
 FROM candidates
 ), qualified AS (
 SELECT shapes.*,(` + cleanupRecordDecodeShape + `
  AND ` + cleanupSubjectShape + `
  AND jsonb_typeof(ref)='object' AND (ref-ARRAY['owner','content_id','version','hash','media_type','byte_length'])='{}'::jsonb
  AND jsonb_typeof(ref->'owner')='object' AND ((ref->'owner')-ARRAY['tenant_id','owner_id'])='{}'::jsonb
  AND jsonb_typeof(ref->'owner'->'tenant_id')='string' AND jsonb_typeof(ref->'owner'->'owner_id')='string'
  AND ref->'owner'->>'tenant_id'=tenant_id AND ref->'owner'->>'owner_id'=owner_id
  AND jsonb_typeof(ref->'content_id')='string' AND content_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
  AND jsonb_typeof(ref->'version')='string' AND version::numeric <= 9223372036854775807
  AND ref->>'content_id'=content_id AND ref->>'version'=version
  AND jsonb_typeof(ref->'hash')='string' AND ref->>'hash' ~ '^sha256:[0-9a-f]{64}$'
  AND jsonb_typeof(ref->'byte_length')='string' AND ref->>'byte_length' ~ '^(0|[1-9][0-9]*)$'
  AND (ref->>'byte_length')::numeric <= 9223372036854775807
  AND jsonb_typeof(ref->'media_type')='string' AND length(ref->>'media_type') BETWEEN 1 AND 255
  AND ref->>'media_type' ~ '^[A-Za-z0-9!#$&^_.+-]+/[A-Za-z0-9!#$&^_.+-]+$'
  AND object_key=encode(sha256(convert_to($7 || chr(10) || '["' || tenant_id || '","' || owner_id || '","' || content_id || '","' || version || '"]','UTF8')),'hex')
  AND object_id='cv-' || object_key
  AND record->>'object_id'=object_id AND record->>'object_key'=object_key
  AND record->>'tuple_digest'=tuple_digest AND record->>'publication'=publication
  AND record->>'revision'=revision::text AND record->>'primary_holder_binding'=primary_holder_binding
  AND CASE WHEN jsonb_typeof(record->'body_gone')='boolean' THEN (record->>'body_gone')::boolean ELSE false END = body_gone
  AND jsonb_typeof(seal)='object' AND seal=stored_seal
  AND stored_seal_text ~ $9
  AND (seal-ARRAY['policy_change_key','primary_holder_binding','primary_holder_id','id','content_ref','subject','purpose','started_at','deadline'])='{}'::jsonb
  AND seal->'content_ref'=ref AND seal->'subject'=subject AND seal->'purpose'=record->'purpose'
  AND seal->>'primary_holder_binding'=primary_holder_binding
  AND jsonb_typeof(seal->'primary_holder_id')='string' AND octet_length(seal->>'primary_holder_id') BETWEEN 1 AND 128
  AND jsonb_typeof(seal->'id')='string' AND octet_length(seal->>'id') BETWEEN 1 AND 128
  AND jsonb_typeof(seal->'started_at')='string' AND jsonb_typeof(seal->'deadline')='string'
  AND seal->>'started_at' ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}T'
  AND seal->>'deadline' ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}T'
 ) IS TRUE AS valid_shape FROM shapes
 ), windows AS (
 SELECT qualified.*,CASE WHEN valid_shape THEN
  (seal->>'started_at')::timestamptz > '0001-01-01T00:00:00Z'::timestamptz
  AND (seal->>'deadline')::timestamptz > (seal->>'started_at')::timestamptz
  AND (seal->>'deadline')::timestamptz <= (seal->>'started_at')::timestamptz + interval '24 hours'
 ELSE false END AS valid_window FROM qualified
 )
 SELECT job_id,object_kind,object_id,phase,work_revision,completed_revision,state,due_at,
 valid_shape AND valid_window,body FROM windows
 WHERE NOT(valid_shape AND valid_window) OR
 (subject=$4::jsonb AND seal->>'primary_holder_id'=$5
 AND (seal->>'deadline')::timestamptz + interval '1 microsecond' > $3)
 ORDER BY scan_at,job_id LIMIT $6`
	rows, err := tx.QueryContext(ctx, query, owner.TenantID, owner.OwnerID, now, string(encoded), primary, limit, d.VersionIdentityAlgorithm, maxInt, canonicalBodySealEncoding())
	if err != nil {
		return nil, err
	}
	return readContentJobs(rows, owner)
}

func readContentJobs(rows *sql.Rows, owner contract.OwnerRef) (jobs []runtime.Job, resultErr error) {
	defer func() { resultErr = errors.Join(resultErr, rows.Err(), rows.Close()) }()
	for rows.Next() {
		job := runtime.Job{Object: contract.ObjectRef{TenantID: owner.TenantID, OwnerID: owner.OwnerID}}
		var valid bool
		var body []byte
		if err := rows.Scan(&job.ID, &job.Object.Kind, &job.Object.ID, &job.Phase, &job.WorkRevision, &job.CompletedRevision, &job.State, &job.DueAt, &valid, &body); err != nil {
			return nil, err
		}
		if !valid {
			if body != nil {
				var record d.Record
				if err := json.Unmarshal(body, &record); err != nil {
					return nil, err
				}
			}
			return nil, runtime.ErrScope
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}
