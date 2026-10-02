-- +goose Up
CREATE TABLE durable_commands (
 tenant_id TEXT NOT NULL, owner_id TEXT NOT NULL, service_id TEXT NOT NULL, command_id TEXT NOT NULL,
 digest TEXT NOT NULL CHECK (length(digest)=64), method TEXT NOT NULL, target_id TEXT NOT NULL,
 state TEXT NOT NULL CHECK (state IN ('reserved','prepared','accepted','applied','rejected','gone')),
 preparation_ref TEXT NOT NULL DEFAULT '', receipt TEXT NOT NULL DEFAULT '', decided_type TEXT NOT NULL DEFAULT '',
 expires_at BIGINT NOT NULL, retain_until BIGINT NOT NULL DEFAULT 0,
 PRIMARY KEY (tenant_id,service_id,command_id)
);
CREATE TABLE durable_jobs (
 tenant_id TEXT NOT NULL, owner_id TEXT NOT NULL, kind TEXT NOT NULL, responsibility_key TEXT NOT NULL,
 job_id TEXT NOT NULL UNIQUE, source_ref TEXT NOT NULL,
 state TEXT NOT NULL CHECK (state IN ('ready','leased','waiting','done')),
 due_at BIGINT NOT NULL, work_revision BIGINT NOT NULL CHECK (work_revision BETWEEN 1 AND 9007199254740991),
 lease_epoch BIGINT NOT NULL CHECK (lease_epoch BETWEEN 0 AND 9007199254740991),
 lease_until BIGINT NOT NULL DEFAULT 0, holder_id TEXT NOT NULL DEFAULT '', wait_reason TEXT NOT NULL DEFAULT '',
 PRIMARY KEY (tenant_id,owner_id,kind,responsibility_key),
 CHECK ((state='leased' AND holder_id<>'' AND lease_until>0) OR (state<>'leased' AND holder_id='' AND lease_until=0))
);
CREATE INDEX durable_jobs_due ON durable_jobs (tenant_id,owner_id,kind,due_at,responsibility_key) WHERE state IN ('ready','waiting');
CREATE INDEX durable_jobs_expired ON durable_jobs (tenant_id,owner_id,kind,lease_until,responsibility_key) WHERE state='leased';

-- +goose Down
DROP TABLE durable_jobs;
DROP TABLE durable_commands;

