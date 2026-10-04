-- Retire the pre-accounted development v1 execution basis. Never fabricate a
-- durable start, fee, input binding, or historical measurement. Original fixed
-- Command receipts and published terminal results remain byte-for-byte facts.
WITH legacy AS (
 SELECT tenant_id,owner_id,decision_id,convert_from(body,'UTF8')::jsonb AS fact
 FROM decisions WHERE NOT (convert_from(body,'UTF8')::jsonb->'usage' ? 'rule_starts')
), upgraded AS (
 SELECT *,(
  COALESCE((fact->>'started_epoch')::bigint,0)>0
  OR status IN('running','waiting','completed')
  OR COALESCE((fact->'usage'->>'rule_steps')::numeric,0)<>0
  OR COALESCE((fact->'usage'->>'input_bytes')::numeric,0)<>0
  OR COALESCE((fact->'usage'->>'output_bytes')::numeric,0)<>0
  OR COALESCE((fact->'usage'->'cost'->>'integer_value')::numeric,0)<>0
 ) AS started
 FROM legacy JOIN decisions USING(tenant_id,owner_id,decision_id)
)
UPDATE decisions d SET
 body=convert_to((u.fact
   || jsonb_build_object('usage',(u.fact->'usage')||jsonb_build_object('rule_starts','0','measurements_complete',NOT u.started))
   || jsonb_build_object('legacy_billing_retired',true)
   || CASE WHEN u.started THEN jsonb_build_object('legacy_unaccounted_start',true,'measurement_unknown',true) ELSE '{}'::jsonb END
 )::text,'UTF8')
FROM upgraded u WHERE d.tenant_id=u.tenant_id AND d.owner_id=u.owner_id AND d.decision_id=u.decision_id;
