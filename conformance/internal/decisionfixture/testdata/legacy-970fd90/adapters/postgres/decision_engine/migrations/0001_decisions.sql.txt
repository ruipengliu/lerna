CREATE TABLE decisions (
 tenant_id text NOT NULL, owner_id text NOT NULL, decision_id text NOT NULL,
 input_digest text NOT NULL CHECK(input_digest ~ '^sha256:[0-9a-f]{64}$'),
 revision bigint NOT NULL CHECK(revision>0),
 status text NOT NULL CHECK(status IN ('accepted','running','waiting','completed','failed','cancelled')),
 deadline timestamptz, body bytea NOT NULL,
 PRIMARY KEY(tenant_id,owner_id,decision_id),
 CHECK(status='cancelled' OR deadline IS NOT NULL)
);
CREATE INDEX decisions_live_deadline ON decisions(tenant_id,owner_id,deadline,decision_id) WHERE status IN('accepted','running','waiting');
CREATE TABLE command_receipts (
 tenant_id text NOT NULL, owner_id text NOT NULL, command_id text NOT NULL,
 contract_version text NOT NULL CHECK(contract_version='1.1.0'),
 digest text NOT NULL CHECK(digest ~ '^sha256:[0-9a-f]{64}$'), metadata bytea NOT NULL, receipt bytea NOT NULL,
 PRIMARY KEY(tenant_id,owner_id,command_id)
);
CREATE TABLE jobs (
 tenant_id text NOT NULL,owner_id text NOT NULL,job_id text NOT NULL,
 object_kind text NOT NULL CHECK(object_kind='decision'),object_id text NOT NULL,
 phase text NOT NULL CHECK(phase='decide'),
 work_revision bigint NOT NULL CHECK(work_revision>0),
 completed_revision bigint NOT NULL DEFAULT 0 CHECK(completed_revision>=0 AND completed_revision<=work_revision),
 state text NOT NULL CHECK(state IN('ready','waiting','leased','done')),
 due_at timestamptz NOT NULL, lease_epoch bigint NOT NULL DEFAULT 0 CHECK(lease_epoch>=0),
 claimed_revision bigint,worker_id text,lease_until timestamptz,
 lane text NOT NULL DEFAULT 'ordinary' CHECK(lane IN('ordinary','control','reconciliation')),
 pool_claim_epoch bigint CHECK(pool_claim_epoch IS NULL OR(pool_claim_epoch>0 AND pool_claim_epoch<=lease_epoch)),
 scan_at timestamptz GENERATED ALWAYS AS(CASE WHEN state='leased' THEN GREATEST(due_at,lease_until) ELSE due_at END) STORED,
 PRIMARY KEY(tenant_id,owner_id,job_id),
 UNIQUE(tenant_id,owner_id,object_kind,object_id,phase),
 FOREIGN KEY(tenant_id,owner_id,object_id) REFERENCES decisions(tenant_id,owner_id,decision_id),
 CHECK((state='leased' AND lease_epoch>0 AND claimed_revision IS NOT NULL AND claimed_revision>completed_revision AND claimed_revision<=work_revision AND worker_id IS NOT NULL AND length(worker_id) BETWEEN 1 AND 128 AND lease_until IS NOT NULL) OR(state<>'leased' AND claimed_revision IS NULL AND worker_id IS NULL AND lease_until IS NULL)),
 CHECK((state='done' AND completed_revision=work_revision) OR(state<>'done' AND completed_revision<work_revision))
);
CREATE INDEX jobs_scan ON jobs(tenant_id,owner_id,scan_at,job_id) WHERE state<>'done' AND lease_epoch<9223372036854775807;
CREATE INDEX pool_jobs ON jobs(lane,tenant_id,owner_id,state,job_id);
CREATE INDEX pool_ready_order ON jobs(lane,tenant_id,owner_id,due_at,job_id) WHERE state<>'done';
CREATE TABLE durable_pools(pool_id text PRIMARY KEY,configuration text NOT NULL,cursors text NOT NULL);
CREATE TABLE durable_pool_members(tenant_id text NOT NULL,owner_id text NOT NULL,pool_id text NOT NULL REFERENCES durable_pools(pool_id),PRIMARY KEY(tenant_id,owner_id));
CREATE INDEX pool_members_scope ON durable_pool_members(pool_id,tenant_id,owner_id);
CREATE TABLE durable_pool_scope(scope_id text PRIMARY KEY);
INSERT INTO durable_pool_scope(scope_id) SELECT gen_random_uuid()::text;
