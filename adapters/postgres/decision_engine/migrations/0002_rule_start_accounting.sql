-- Retire the pre-accounted development v1 execution basis. Never fabricate a
-- durable start, fee, input binding, or historical measurement. Original fixed
-- Command receipts and published terminal results remain byte-for-byte facts.
WITH legacy AS (
 SELECT tenant_id,owner_id,decision_id,convert_from(body,'UTF8')::jsonb AS fact
 FROM decisions WHERE NOT (convert_from(body,'UTF8')::jsonb->'usage' ? 'rule_starts')
), upgraded AS (
 SELECT *,COALESCE((fact->>'started_epoch')::bigint,0)>0 AS started,
 status IN('accepted','running','waiting') AS active
 FROM legacy JOIN decisions USING(tenant_id,owner_id,decision_id)
)
UPDATE decisions d SET
 revision=CASE WHEN u.active THEN d.revision+1 ELSE d.revision END,
 status=CASE WHEN u.active THEN 'failed' ELSE d.status END,
 body=convert_to((u.fact
   || jsonb_build_object('usage',(u.fact->'usage')||jsonb_build_object('rule_starts','0','measurements_complete',NOT u.started))
   || CASE WHEN u.started THEN jsonb_build_object('legacy_unaccounted_start',true,'measurement_unknown',true) ELSE '{}'::jsonb END
   || CASE WHEN u.active THEN jsonb_build_object('status','failed','revision',d.revision+1,'failure',CASE WHEN u.started OR d.status<>'accepted' THEN 'usage_unavailable' ELSE 'billing_basis_unsupported' END) ELSE '{}'::jsonb END
 )::text,'UTF8')
FROM upgraded u WHERE d.tenant_id=u.tenant_id AND d.owner_id=u.owner_id AND d.decision_id=u.decision_id;
UPDATE jobs j SET completed_revision=work_revision,state='done',claimed_revision=NULL,worker_id=NULL,lease_until=NULL,pool_claim_epoch=NULL
FROM decisions d WHERE j.tenant_id=d.tenant_id AND j.owner_id=d.owner_id AND j.object_id=d.decision_id
 AND d.status='failed' AND convert_from(d.body,'UTF8')::jsonb->>'failure' IN('usage_unavailable','billing_basis_unsupported');
