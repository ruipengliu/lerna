-- name: DBNow :one
SELECT CAST(unixepoch('subsec')*1000 AS INTEGER) AS current_ms;

-- name: ReserveCommand :execrows
INSERT INTO durable_commands(tenant_id,owner_id,service_id,command_id,digest,method,target_id,state,expires_at)
VALUES(?1,?2,?3,?4,?5,?6,?7,'reserved',?8) ON CONFLICT(tenant_id,service_id,command_id) DO NOTHING;

-- name: LookupCommand :one
SELECT digest,method,target_id,state,preparation_ref,receipt,decided_type,expires_at,retain_until FROM durable_commands WHERE tenant_id=?1 AND owner_id=?2 AND service_id=?3 AND command_id=?4;

-- name: SaveCommand :execrows
UPDATE durable_commands SET state=?5,preparation_ref=?6,receipt=?7,decided_type=?8,retain_until=?9 WHERE tenant_id=?1 AND owner_id=?2 AND service_id=?3 AND command_id=?4;

-- name: RaiseJob :one
INSERT INTO durable_jobs(tenant_id,owner_id,kind,responsibility_key,job_id,source_ref,state,due_at,work_revision,lease_epoch,lease_until,holder_id,wait_reason) VALUES(?1,?2,?3,?4,?5,?6,'ready',?7,1,0,0,'','')
ON CONFLICT(tenant_id,owner_id,kind,responsibility_key) DO UPDATE SET
 work_revision=durable_jobs.work_revision+1,
 due_at=min(durable_jobs.due_at,excluded.due_at),
 state=CASE WHEN durable_jobs.state='leased' THEN 'leased' ELSE 'ready' END,
 wait_reason=''
WHERE durable_jobs.source_ref=excluded.source_ref
RETURNING tenant_id,owner_id,kind,responsibility_key,job_id,source_ref,state,due_at,work_revision,lease_epoch,lease_until,holder_id,wait_reason;

-- name: HintJob :exec
UPDATE durable_jobs SET due_at=min(due_at,?5) WHERE tenant_id=?1 AND owner_id=?2 AND kind=?3 AND responsibility_key=?4 AND state<>'done';

-- name: LockJob :one
SELECT tenant_id,owner_id,kind,responsibility_key,job_id,source_ref,state,due_at,work_revision,lease_epoch,lease_until,holder_id,wait_reason FROM durable_jobs WHERE tenant_id=?1 AND owner_id=?2 AND job_id=?3;

-- name: ClaimCandidates :many
SELECT tenant_id,owner_id,kind,responsibility_key,job_id,source_ref,state,due_at,work_revision,lease_epoch,lease_until,holder_id,wait_reason FROM durable_jobs WHERE tenant_id=?1 AND owner_id=?2 AND kind=?3 AND
 ((state IN ('ready','waiting') AND due_at<=CAST(unixepoch('subsec')*1000 AS INTEGER)) OR (state='leased' AND lease_until<=CAST(unixepoch('subsec')*1000 AS INTEGER)))
ORDER BY due_at,responsibility_key LIMIT ?4;

-- name: LeaseJob :one
UPDATE durable_jobs SET state='leased',holder_id=?4,lease_epoch=lease_epoch+1,lease_until=?5,wait_reason=''
WHERE tenant_id=?1 AND owner_id=?2 AND job_id=?3 RETURNING tenant_id,owner_id,kind,responsibility_key,job_id,source_ref,state,due_at,work_revision,lease_epoch,lease_until,holder_id,wait_reason;

-- name: UpdateJob :execrows
UPDATE durable_jobs SET state=?4,due_at=?5,holder_id=?6,lease_until=?7,wait_reason=?8 WHERE tenant_id=?1 AND owner_id=?2 AND job_id=?3;

-- name: DeleteDone :execrows
DELETE FROM durable_jobs WHERE tenant_id=?1 AND owner_id=?2 AND kind=?3 AND responsibility_key=?4 AND state='done';

-- name: OpenJobs :one
SELECT count(*) AS open_count,COALESCE(min(due_at),0) AS oldest_due FROM durable_jobs WHERE tenant_id=?1 AND owner_id=?2 AND state<>'done';

