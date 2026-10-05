-- Content owner v1; independent of frozen Host and Decision migrations.
CREATE TABLE content_versions (
 tenant_id text NOT NULL, owner_id text NOT NULL, content_id text NOT NULL,
 version text NOT NULL CHECK(version ~ '^[1-9][0-9]*$'),
 object_key text NOT NULL CHECK(object_key ~ '^[0-9a-f]{64}$'),
 object_id text NOT NULL CHECK(object_id ~ '^cv-[0-9a-f]{64}$'),
 tuple_digest text NOT NULL CHECK(tuple_digest ~ '^sha256:[0-9a-f]{64}$'),
 publication text NOT NULL CHECK(publication IN('preparing','published','failed')),
 revision bigint NOT NULL CHECK(revision>0),
 body bytea NOT NULL, staging bytea,
 PRIMARY KEY(tenant_id,owner_id,content_id,version),
 UNIQUE(tenant_id,owner_id,object_key),
 UNIQUE(tenant_id,owner_id,object_id),
 CHECK(staging IS NULL OR octet_length(staging)<=262144),
 CHECK(publication<>'published' OR staging IS NULL)
);
CREATE TABLE command_receipts (
 tenant_id text NOT NULL, owner_id text NOT NULL, command_id text NOT NULL,
 contract_version text NOT NULL CHECK(contract_version='1.2.0'),
 digest text NOT NULL CHECK(digest ~ '^sha256:[0-9a-f]{64}$'),
 metadata bytea NOT NULL, receipt bytea NOT NULL,
 PRIMARY KEY(tenant_id,owner_id,command_id)
);
CREATE TABLE content_fixture_policies (
 tenant_id text NOT NULL, owner_id text NOT NULL, subject_key text NOT NULL,
 content_id text NOT NULL, version text NOT NULL, purpose text NOT NULL,
 revision bigint NOT NULL CHECK(revision>0), valid_until timestamptz NOT NULL,
 body bytea NOT NULL,
 PRIMARY KEY(tenant_id,owner_id,subject_key,content_id,version,purpose)
);
CREATE TABLE jobs (
 tenant_id text NOT NULL,owner_id text NOT NULL,job_id text NOT NULL,
 object_kind text NOT NULL CHECK(object_kind='content'),object_id text NOT NULL,
 phase text NOT NULL CHECK(phase='publish'),
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
 FOREIGN KEY(tenant_id,owner_id,object_id) REFERENCES content_versions(tenant_id,owner_id,object_id),
 CHECK((state='leased' AND lease_epoch>0 AND claimed_revision IS NOT NULL AND claimed_revision>completed_revision AND claimed_revision<=work_revision AND worker_id IS NOT NULL AND length(worker_id) BETWEEN 1 AND 128 AND lease_until IS NOT NULL) OR(state<>'leased' AND claimed_revision IS NULL AND worker_id IS NULL AND lease_until IS NULL)),
 CHECK((state='done' AND completed_revision=work_revision) OR(state<>'done' AND completed_revision<work_revision))
);
CREATE INDEX jobs_scan ON jobs(tenant_id,owner_id,scan_at,job_id) WHERE state<>'done' AND lease_epoch<9223372036854775807;
CREATE INDEX pool_jobs ON jobs(lane,tenant_id,owner_id,state,job_id);
CREATE INDEX pool_ready_order ON jobs(lane,tenant_id,owner_id,due_at,job_id) WHERE state<>'done';
CREATE TABLE content_fixture_command_readers (
 tenant_id text NOT NULL, owner_id text NOT NULL, subject_key text NOT NULL,
 subject bytea NOT NULL, valid_until timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,owner_id,subject_key)
);
