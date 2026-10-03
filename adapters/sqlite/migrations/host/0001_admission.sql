CREATE TABLE command_receipts (
 tenant_id TEXT NOT NULL, owner_id TEXT NOT NULL, command_id TEXT NOT NULL,
 digest TEXT NOT NULL, metadata BLOB NOT NULL, receipt BLOB NOT NULL,
 PRIMARY KEY (tenant_id, owner_id, command_id)
);
CREATE TABLE durable_inputs (
 tenant_id TEXT NOT NULL, owner_id TEXT NOT NULL, object_id TEXT NOT NULL,
 revision INTEGER NOT NULL CHECK (revision > 0), text_value BLOB NOT NULL,
 created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL,
 PRIMARY KEY (tenant_id, owner_id, object_id)
);
CREATE TABLE jobs (
 tenant_id TEXT NOT NULL, owner_id TEXT NOT NULL, job_id TEXT NOT NULL,
 object_kind TEXT NOT NULL, object_id TEXT NOT NULL, phase TEXT NOT NULL,
 work_revision INTEGER NOT NULL CHECK (work_revision > 0),
 completed_revision INTEGER NOT NULL DEFAULT 0 CHECK (completed_revision >= 0 AND completed_revision <= work_revision),
 state TEXT NOT NULL CHECK (state = 'ready'), due_at DATETIME NOT NULL,
 PRIMARY KEY (tenant_id, owner_id, job_id),
 UNIQUE (tenant_id, owner_id, object_kind, object_id, phase),
 FOREIGN KEY (tenant_id, owner_id, object_id) REFERENCES durable_inputs (tenant_id, owner_id, object_id)
);
