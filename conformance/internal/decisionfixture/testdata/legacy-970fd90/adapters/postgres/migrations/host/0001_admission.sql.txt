CREATE TABLE command_receipts (
 tenant_id text NOT NULL, owner_id text NOT NULL, command_id text NOT NULL,
 digest text NOT NULL, metadata bytea NOT NULL, receipt bytea NOT NULL,
 PRIMARY KEY (tenant_id, owner_id, command_id)
);
CREATE TABLE durable_inputs (
 tenant_id text NOT NULL, owner_id text NOT NULL, object_id text NOT NULL,
 revision bigint NOT NULL CHECK (revision > 0), text_value bytea NOT NULL,
 created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL,
 PRIMARY KEY (tenant_id, owner_id, object_id)
);
CREATE TABLE jobs (
 tenant_id text NOT NULL, owner_id text NOT NULL, job_id text NOT NULL,
 object_kind text NOT NULL, object_id text NOT NULL, phase text NOT NULL,
 work_revision bigint NOT NULL CHECK (work_revision > 0),
 completed_revision bigint NOT NULL DEFAULT 0 CHECK (completed_revision >= 0 AND completed_revision <= work_revision),
 state text NOT NULL CHECK (state = 'ready'), due_at timestamptz NOT NULL,
 PRIMARY KEY (tenant_id, owner_id, job_id),
 UNIQUE (tenant_id, owner_id, object_kind, object_id, phase),
 FOREIGN KEY (tenant_id, owner_id, object_id) REFERENCES durable_inputs (tenant_id, owner_id, object_id)
);
