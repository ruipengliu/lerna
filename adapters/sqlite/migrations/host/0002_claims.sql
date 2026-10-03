-- Rebuild the v1 Job table to extend its published ready-only constraint.
-- Input and command tables retain their original identities and bytes.
CREATE TABLE jobs_v2 (
 tenant_id TEXT NOT NULL, owner_id TEXT NOT NULL, job_id TEXT NOT NULL,
 object_kind TEXT NOT NULL, object_id TEXT NOT NULL, phase TEXT NOT NULL,
 work_revision INTEGER NOT NULL CHECK (work_revision > 0),
 completed_revision INTEGER NOT NULL DEFAULT 0 CHECK (completed_revision >= 0 AND completed_revision <= work_revision),
 state TEXT NOT NULL CHECK (state IN ('ready','leased','done')), due_at DATETIME NOT NULL,
 lease_epoch INTEGER NOT NULL DEFAULT 0 CHECK (lease_epoch >= 0),
 claimed_revision INTEGER, worker_id TEXT, lease_until DATETIME,
 scan_at DATETIME GENERATED ALWAYS AS (CASE WHEN state='leased' AND lease_until>due_at THEN lease_until ELSE due_at END) STORED,
 PRIMARY KEY (tenant_id, owner_id, job_id),
 UNIQUE (tenant_id, owner_id, object_kind, object_id, phase),
 FOREIGN KEY (tenant_id, owner_id, object_id) REFERENCES durable_inputs (tenant_id, owner_id, object_id),
 CHECK ((state='leased' AND lease_epoch>0 AND claimed_revision IS NOT NULL AND claimed_revision>completed_revision AND claimed_revision<=work_revision AND worker_id IS NOT NULL AND length(worker_id) BETWEEN 1 AND 128 AND lease_until IS NOT NULL)
 OR (state<>'leased' AND claimed_revision IS NULL AND worker_id IS NULL AND lease_until IS NULL)),
 CHECK ((state='done' AND completed_revision=work_revision) OR (state<>'done' AND completed_revision<work_revision))
);
INSERT INTO jobs_v2 (tenant_id,owner_id,job_id,object_kind,object_id,phase,work_revision,completed_revision,state,due_at)
 SELECT tenant_id,owner_id,job_id,object_kind,object_id,phase,work_revision,completed_revision,state,due_at FROM jobs;
DROP TABLE jobs;
ALTER TABLE jobs_v2 RENAME TO jobs;
CREATE INDEX jobs_scan ON jobs (tenant_id,owner_id,scan_at,job_id) WHERE state<>'done' AND lease_epoch<9223372036854775807;
ALTER TABLE durable_inputs ADD COLUMN projected_revision INTEGER;
ALTER TABLE durable_inputs ADD COLUMN text_digest TEXT CHECK (
 (projected_revision IS NULL AND text_digest IS NULL) OR
 (projected_revision IS NOT NULL AND text_digest IS NOT NULL AND projected_revision>0 AND projected_revision<=revision AND length(text_digest)=71 AND substr(text_digest,1,7)='sha256:' AND substr(text_digest,8) NOT GLOB '*[^0-9a-f]*')
);
