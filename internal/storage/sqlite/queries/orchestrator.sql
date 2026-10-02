-- name: OrchestratorGetTask :one
SELECT data FROM orchestrator_tasks WHERE tenant_id=?1 AND owner_id=?2 AND task_id=?3;

-- name: OrchestratorSaveTask :execrows
INSERT INTO orchestrator_tasks(tenant_id,owner_id,task_id,subject_id,parent_task_id,depth,created_at,revision,status,data) VALUES(?1,?2,?3,?4,?5,?6,?7,?8,?9,?10)
ON CONFLICT(tenant_id,owner_id,task_id) DO UPDATE SET revision=excluded.revision,status=excluded.status,data=excluded.data
WHERE orchestrator_tasks.subject_id=excluded.subject_id AND orchestrator_tasks.parent_task_id=excluded.parent_task_id AND orchestrator_tasks.depth=excluded.depth AND orchestrator_tasks.created_at=excluded.created_at AND orchestrator_tasks.revision<excluded.revision;

-- name: OrchestratorTree :many
WITH RECURSIVE tree AS (
 SELECT origin.task_id,origin.parent_task_id,origin.depth,origin.data FROM orchestrator_tasks origin WHERE origin.tenant_id=?1 AND origin.owner_id=?2 AND origin.task_id=?3
 UNION ALL
 SELECT t.task_id,t.parent_task_id,t.depth,t.data FROM orchestrator_tasks t JOIN tree p ON t.parent_task_id=p.task_id WHERE t.tenant_id=?1 AND t.owner_id=?2 AND t.status='active'
) SELECT data FROM tree ORDER BY depth,task_id LIMIT ?4;

-- name: OrchestratorChain :many
WITH RECURSIVE chain AS (
 SELECT origin.task_id,origin.parent_task_id,origin.depth,origin.data FROM orchestrator_tasks origin WHERE origin.tenant_id=?1 AND origin.owner_id=?2 AND origin.task_id=?3
 UNION ALL
 SELECT t.task_id,t.parent_task_id,t.depth,t.data FROM orchestrator_tasks t JOIN chain c ON t.task_id=c.parent_task_id WHERE t.tenant_id=?1 AND t.owner_id=?2
) SELECT data FROM chain ORDER BY depth,task_id LIMIT 6;

-- name: OrchestratorList :many
SELECT data FROM orchestrator_tasks WHERE tenant_id=?1 AND owner_id=?2 AND created_at<=?3
 AND (created_at<?4 OR (created_at=?4 AND task_id>?5))
ORDER BY created_at DESC,task_id LIMIT ?6;

-- name: OrchestratorRecovery :many
WITH ids AS (
 SELECT active.task_id FROM (SELECT t.task_id FROM orchestrator_tasks t WHERE t.tenant_id=?1 AND t.owner_id=?2 AND t.status='active' AND t.task_id>?3 ORDER BY t.task_id LIMIT ?4) active
 UNION
 SELECT unresolved.task_id FROM (SELECT DISTINCT r.task_id FROM orchestrator_records r WHERE r.tenant_id=?1 AND r.owner_id=?2 AND r.state='open' AND r.kind IN ('reservation','control','intent','delegation','extraction') AND r.task_id>?3 ORDER BY r.task_id LIMIT ?4) unresolved
), page AS (SELECT ids.task_id FROM ids ORDER BY ids.task_id LIMIT ?4)
SELECT t.data FROM page p JOIN orchestrator_tasks t ON t.tenant_id=?1 AND t.owner_id=?2 AND t.task_id=p.task_id ORDER BY t.task_id;

-- name: OrchestratorBalances :many
SELECT unit,limit_value,spent,reserved FROM orchestrator_balances WHERE tenant_id=?1 AND owner_id=?2 AND task_id=?3 ORDER BY unit LIMIT 101;

-- name: OrchestratorLockBalances :many
SELECT unit,limit_value,spent,reserved FROM orchestrator_balances WHERE tenant_id=?1 AND owner_id=?2 AND task_id=?3 ORDER BY unit LIMIT 101;

-- name: OrchestratorSaveBalance :exec
INSERT INTO orchestrator_balances(tenant_id,owner_id,task_id,unit,limit_value,spent,reserved) VALUES(?1,?2,?3,?4,?5,?6,?7)
ON CONFLICT(tenant_id,owner_id,task_id,unit) DO UPDATE SET limit_value=excluded.limit_value,spent=excluded.spent,reserved=excluded.reserved;

-- name: OrchestratorGetRecord :one
SELECT kind,id,task_id,revision,state,current_key,immutable,data FROM orchestrator_records WHERE tenant_id=?1 AND owner_id=?2 AND kind=?3 AND id=?4;

-- name: OrchestratorRecords :many
SELECT kind,id,task_id,revision,state,current_key,immutable,data FROM orchestrator_records
WHERE tenant_id=?1 AND owner_id=?2 AND task_id=?3 AND kind=?4 AND id>?5
 AND (?6='' OR state=?6) AND (?7='' OR current_key=?7 OR (?7='@current' AND current_key<>''))
ORDER BY id LIMIT ?8;

-- name: OrchestratorOpenRecords :many
SELECT kind,id,task_id,revision,state,current_key,immutable,data FROM orchestrator_records
WHERE tenant_id=?1 AND owner_id=?2 AND kind=?3 AND state='open' AND id>?4 ORDER BY id LIMIT ?5;

-- name: OrchestratorSaveRecord :execrows
INSERT INTO orchestrator_records(tenant_id,owner_id,kind,id,task_id,revision,state,current_key,immutable,data) VALUES(?1,?2,?3,?4,?5,?6,?7,?8,?9,?10)
ON CONFLICT(tenant_id,owner_id,kind,id) DO UPDATE SET revision=excluded.revision,state=excluded.state,current_key=excluded.current_key,data=excluded.data
WHERE orchestrator_records.task_id=excluded.task_id AND orchestrator_records.immutable=excluded.immutable
 AND orchestrator_records.revision<=excluded.revision
 AND (orchestrator_records.immutable=FALSE OR orchestrator_records.data=excluded.data);

-- name: OrchestratorDeselect :exec
UPDATE orchestrator_records SET current_key='' WHERE tenant_id=?1 AND owner_id=?2 AND task_id=?3 AND kind=?4 AND current_key=?5;

-- name: OrchestratorGateCreate :exec
INSERT INTO orchestrator_gates(tenant_id,owner_id,gate_key,revision) VALUES(?1,?2,?3,1) ON CONFLICT DO NOTHING;

-- name: OrchestratorGateAdvance :exec
UPDATE orchestrator_gates SET revision=revision+1 WHERE tenant_id=?1 AND owner_id=?2 AND gate_key=?3;

-- name: OrchestratorCapacityCreate :exec
INSERT INTO orchestrator_user_capacity(tenant_id,owner_id,subject_id,active_count) VALUES(?1,?2,?3,0) ON CONFLICT DO NOTHING;

-- name: OrchestratorCapacityChange :execrows
UPDATE orchestrator_user_capacity SET active_count=active_count+?4 WHERE tenant_id=?1 AND owner_id=?2 AND subject_id=?3 AND active_count+?4 BETWEEN 0 AND ?5;

-- name: OrchestratorGetReceiver :one
SELECT data FROM orchestrator_receivers WHERE tenant_id=?1 AND owner_id=?2 AND allocation_id=?3;

-- name: OrchestratorSaveReceiver :exec
INSERT INTO orchestrator_receivers(tenant_id,owner_id,allocation_id,data) VALUES(?1,?2,?3,?4)
ON CONFLICT(tenant_id,owner_id,allocation_id) DO UPDATE SET data=excluded.data;

-- name: OrchestratorLockTask :one
SELECT data FROM orchestrator_tasks WHERE tenant_id=?1 AND owner_id=?2 AND task_id=?3;

-- name: OrchestratorLockGate :one
SELECT revision FROM orchestrator_gates WHERE tenant_id=?1 AND owner_id=?2 AND gate_key=?3;

-- name: OrchestratorReadGate :one
SELECT revision FROM orchestrator_gates WHERE tenant_id=?1 AND owner_id=?2 AND gate_key=?3;

-- name: OrchestratorLockCapacity :one
SELECT active_count FROM orchestrator_user_capacity WHERE tenant_id=?1 AND owner_id=?2 AND subject_id=?3;

-- name: OrchestratorSaveRoute :exec
INSERT INTO orchestrator_work_routes(tenant_id,owner_id,kind,responsibility_key,subject_id,task_id,provider_id,resource_id) VALUES(?1,?2,?3,?4,?5,?6,?7,?8) ON CONFLICT DO NOTHING;

-- name: OrchestratorScheduleCreate :exec
INSERT INTO orchestrator_scheduler(tenant_id,owner_id,kind,turn) VALUES(?1,?2,?3,0) ON CONFLICT DO NOTHING;

-- name: OrchestratorScheduleLock :one
SELECT turn FROM orchestrator_scheduler WHERE tenant_id=?1 AND owner_id=?2 AND kind=?3;

-- name: OrchestratorScheduleAdvance :exec
UPDATE orchestrator_scheduler SET turn=turn+1 WHERE tenant_id=?1 AND owner_id=?2 AND kind=?3;

-- name: OrchestratorScheduleTurns :exec
INSERT INTO orchestrator_schedule_turns(tenant_id,owner_id,kind,dimension,entity_id,turn)
VALUES(?1,?2,?3,'user',?4,?6),(?1,?2,?3,'task',?5,?6)
ON CONFLICT(tenant_id,owner_id,kind,dimension,entity_id) DO UPDATE SET turn=excluded.turn;

-- name: OrchestratorFairCandidates :many
SELECT j.tenant_id,j.owner_id,j.kind,j.responsibility_key,j.job_id,j.source_ref,j.state,j.due_at,j.work_revision,j.lease_epoch,j.lease_until,j.holder_id,j.wait_reason,
 COALESCE(r.subject_id,j.owner_id) AS subject_id,COALESCE(r.task_id,j.source_ref) AS task_id
FROM durable_jobs j LEFT JOIN orchestrator_work_routes r ON r.tenant_id=j.tenant_id AND r.owner_id=j.owner_id AND r.kind=j.kind AND r.responsibility_key=j.responsibility_key
LEFT JOIN orchestrator_schedule_turns u ON u.tenant_id=j.tenant_id AND u.owner_id=j.owner_id AND u.kind=j.kind AND u.dimension='user' AND u.entity_id=COALESCE(r.subject_id,j.owner_id)
LEFT JOIN orchestrator_schedule_turns t ON t.tenant_id=j.tenant_id AND t.owner_id=j.owner_id AND t.kind=j.kind AND t.dimension='task' AND t.entity_id=COALESCE(r.task_id,j.source_ref)
WHERE j.tenant_id=?1 AND j.owner_id=?2 AND j.kind=?3
 AND ((j.state IN ('ready','waiting') AND j.due_at<=CAST((julianday('now')-2440587.5)*86400000 AS INTEGER)) OR (j.state='leased' AND j.lease_until<=CAST((julianday('now')-2440587.5)*86400000 AS INTEGER)))
 AND (r.provider_id IS NULL OR r.provider_id='' OR (SELECT count(*) FROM orchestrator_work_routes pr JOIN durable_jobs pj ON pj.tenant_id=pr.tenant_id AND pj.owner_id=pr.owner_id AND pj.kind=pr.kind AND pj.responsibility_key=pr.responsibility_key WHERE pr.tenant_id=j.tenant_id AND pr.owner_id=j.owner_id AND pr.provider_id=r.provider_id AND pj.state='leased' AND pj.lease_until>CAST((julianday('now')-2440587.5)*86400000 AS INTEGER))<?5)
 AND (r.resource_id IS NULL OR r.resource_id='' OR (SELECT count(*) FROM orchestrator_work_routes rr JOIN durable_jobs rj ON rj.tenant_id=rr.tenant_id AND rj.owner_id=rr.owner_id AND rj.kind=rr.kind AND rj.responsibility_key=rr.responsibility_key WHERE rr.tenant_id=j.tenant_id AND rr.owner_id=j.owner_id AND rr.resource_id=r.resource_id AND rj.state='leased' AND rj.lease_until>CAST((julianday('now')-2440587.5)*86400000 AS INTEGER))<?6)
ORDER BY COALESCE(u.turn,0),COALESCE(t.turn,0),j.due_at,j.responsibility_key LIMIT ?4;

-- name: OrchestratorBatchLockTasks :many
SELECT data FROM orchestrator_tasks WHERE tenant_id=?1 AND owner_id=?2 AND task_id IN (SELECT value FROM json_each(?3)) ORDER BY depth,task_id;

-- name: OrchestratorBatchLockBalances :many
SELECT task_id,unit,limit_value,spent,reserved FROM orchestrator_balances WHERE tenant_id=?1 AND owner_id=?2 AND task_id IN (SELECT value FROM json_each(?3)) ORDER BY task_id,unit;

-- name: OrchestratorCurrentRecords :many
SELECT kind,id,task_id,revision,state,current_key,immutable,data FROM orchestrator_records WHERE tenant_id=?1 AND owner_id=?2 AND task_id=?3 AND kind=?4 AND current_key<>'' ORDER BY id LIMIT ?5;

-- name: OrchestratorOpenTaskRecords :many
SELECT kind,id,task_id,revision,state,current_key,immutable,data FROM orchestrator_records WHERE tenant_id=?1 AND owner_id=?2 AND task_id=?3 AND kind=?4 AND state='open' ORDER BY id LIMIT ?5;

-- name: OrchestratorSaveBalances :exec
INSERT INTO orchestrator_balances(tenant_id,owner_id,task_id,unit,limit_value,spent,reserved) SELECT ?1,?2,?3,json_extract(value,'$.unit'),json_extract(value,'$.limit.amount'),json_extract(value,'$.spent.amount'),json_extract(value,'$.reserved.amount') FROM json_each(?4) WHERE 1=1
ON CONFLICT(tenant_id,owner_id,task_id,unit) DO UPDATE SET limit_value=excluded.limit_value,spent=excluded.spent,reserved=excluded.reserved;

-- name: OrchestratorGetRecords :many
SELECT kind,id,task_id,revision,state,current_key,immutable,data FROM orchestrator_records WHERE tenant_id=?1 AND owner_id=?2 AND kind=?3 AND id IN (SELECT value FROM json_each(?4)) ORDER BY id LIMIT 101;

-- name: OrchestratorSaveRecords :execrows
INSERT INTO orchestrator_records(tenant_id,owner_id,kind,id,task_id,revision,state,current_key,immutable,data)
SELECT ?1,?2,json_extract(x.value,'$.Kind'),json_extract(x.value,'$.ID'),json_extract(x.value,'$.TaskID'),json_extract(x.value,'$.Revision'),json_extract(x.value,'$.State'),json_extract(x.value,'$.CurrentKey'),json_extract(x.value,'$.Immutable'),json_extract(x.value,'$.Data') FROM json_each(?3) AS x WHERE true
ON CONFLICT(tenant_id,owner_id,kind,id) DO UPDATE SET revision=excluded.revision,state=excluded.state,current_key=excluded.current_key,data=excluded.data
WHERE orchestrator_records.task_id=excluded.task_id AND orchestrator_records.immutable=excluded.immutable AND orchestrator_records.revision<=excluded.revision AND (orchestrator_records.immutable=FALSE OR orchestrator_records.data=excluded.data);

-- name: OrchestratorDeselectKeys :exec
UPDATE orchestrator_records SET current_key='' WHERE tenant_id=?1 AND owner_id=?2 AND task_id=?3 AND kind=?4 AND current_key IN (SELECT value FROM json_each(?5));

-- name: OrchestratorSaveRoutes :execrows
INSERT INTO orchestrator_work_routes(tenant_id,owner_id,kind,responsibility_key,subject_id,task_id,provider_id,resource_id)
SELECT ?1,?2,json_extract(x.value,'$.Kind'),json_extract(x.value,'$.Responsibility'),json_extract(x.value,'$.SubjectID'),json_extract(x.value,'$.TaskID'),json_extract(x.value,'$.ProviderID'),json_extract(x.value,'$.ResourceID') FROM json_each(?3) AS x WHERE true
ON CONFLICT(tenant_id,owner_id,kind,responsibility_key) DO UPDATE SET task_id=excluded.task_id
WHERE orchestrator_work_routes.subject_id=excluded.subject_id AND orchestrator_work_routes.task_id=excluded.task_id AND orchestrator_work_routes.provider_id=excluded.provider_id AND orchestrator_work_routes.resource_id=excluded.resource_id;
