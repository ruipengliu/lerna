-- Extend the v2 state constraint while preserving every live Claim binding.
-- Input and command tables retain their original identities and bytes.
CREATE TABLE jobs_v3 (
 tenant_id TEXT NOT NULL, owner_id TEXT NOT NULL, job_id TEXT NOT NULL,
 object_kind TEXT NOT NULL, object_id TEXT NOT NULL, phase TEXT NOT NULL,
 work_revision INTEGER NOT NULL CHECK (work_revision > 0),
 completed_revision INTEGER NOT NULL DEFAULT 0 CHECK (completed_revision >= 0 AND completed_revision <= work_revision),
 state TEXT NOT NULL CHECK (state IN ('ready','leased','waiting','done')), due_at DATETIME NOT NULL,
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
INSERT INTO jobs_v3 (tenant_id,owner_id,job_id,object_kind,object_id,phase,work_revision,completed_revision,state,due_at,lease_epoch,claimed_revision,worker_id,lease_until)
 SELECT tenant_id,owner_id,job_id,object_kind,object_id,phase,work_revision,completed_revision,state,due_at,lease_epoch,claimed_revision,worker_id,lease_until FROM jobs;
DROP TABLE jobs;
ALTER TABLE jobs_v3 RENAME TO jobs;
CREATE INDEX jobs_scan ON jobs (tenant_id,owner_id,scan_at,job_id) WHERE state<>'done' AND lease_epoch<9223372036854775807;
CREATE TABLE durable_schedules (
 tenant_id TEXT NOT NULL, owner_id TEXT NOT NULL, object_id TEXT NOT NULL,
 input_revision INTEGER NOT NULL CHECK (input_revision>0),
 policy TEXT NOT NULL, binding_source TEXT NOT NULL CHECK (binding_source IN ('admission','legacy-adoption')), adopted_at DATETIME NOT NULL, deadline DATETIME NOT NULL,
 attempts INTEGER NOT NULL CHECK (attempts BETWEEN 0 AND 64),
 start_epoch INTEGER NOT NULL CHECK (start_epoch>=0),
 outcome TEXT NOT NULL CHECK (outcome IN ('','running','waiting','retry','success','permanent','expired','stopped')),
 reason TEXT NOT NULL, due_at DATETIME NOT NULL, stopped INTEGER NOT NULL CHECK (stopped IN (0,1)),
 PRIMARY KEY (tenant_id,owner_id,object_id,input_revision),
 FOREIGN KEY (tenant_id,owner_id,object_id) REFERENCES durable_inputs(tenant_id,owner_id,object_id),
 CHECK (deadline>adopted_at)
);
CREATE TABLE durable_gates (
 tenant_id TEXT NOT NULL,owner_id TEXT NOT NULL,gate_id TEXT NOT NULL,
 revision INTEGER NOT NULL CHECK (revision>=0),
 PRIMARY KEY (tenant_id,owner_id,gate_id)
);
