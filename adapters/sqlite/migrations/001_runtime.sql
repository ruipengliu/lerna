CREATE TABLE IF NOT EXISTS harness_store_metadata (
  singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
  database_id TEXT NOT NULL UNIQUE
);
CREATE TABLE IF NOT EXISTS harness_migrations (
  migration_id INTEGER PRIMARY KEY,
  artifact_digest TEXT NOT NULL,
  state TEXT NOT NULL CHECK (state = 'applied'),
  checkpoint TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS runtime_records (
  tenant_id TEXT NOT NULL,
  owner_id TEXT NOT NULL,
  namespace TEXT NOT NULL,
  object_id TEXT NOT NULL,
  revision INTEGER NOT NULL CHECK (revision BETWEEN 1 AND 9007199254740991),
  parent_id TEXT NOT NULL,
  data BLOB NOT NULL CHECK (length(data) <= 262144),
  PRIMARY KEY (tenant_id, owner_id, namespace, object_id)
);
CREATE INDEX IF NOT EXISTS runtime_records_related ON runtime_records (tenant_id, owner_id, namespace, parent_id, object_id);
CREATE TABLE IF NOT EXISTS runtime_versions (
  tenant_id TEXT NOT NULL,
  owner_id TEXT NOT NULL,
  namespace TEXT NOT NULL,
  object_id TEXT NOT NULL,
  revision INTEGER NOT NULL CHECK (revision BETWEEN 1 AND 9007199254740991),
  data BLOB NOT NULL CHECK (length(data) <= 262144),
  PRIMARY KEY (tenant_id, owner_id, namespace, object_id, revision),
  FOREIGN KEY (tenant_id, owner_id, namespace, object_id) REFERENCES runtime_records (tenant_id, owner_id, namespace, object_id) ON DELETE RESTRICT
);
CREATE TABLE IF NOT EXISTS runtime_semantic_keys (
  tenant_id TEXT NOT NULL,
  owner_id TEXT NOT NULL,
  namespace TEXT NOT NULL,
  semantic_key TEXT NOT NULL,
  object_id TEXT NOT NULL,
  digest TEXT NOT NULL,
  PRIMARY KEY (tenant_id, owner_id, namespace, semantic_key),
  FOREIGN KEY (tenant_id, owner_id, namespace, object_id) REFERENCES runtime_records (tenant_id, owner_id, namespace, object_id) ON DELETE RESTRICT
);
CREATE TABLE IF NOT EXISTS runtime_command_locks (
  tenant_id TEXT NOT NULL,
  owner_id TEXT NOT NULL,
  command_id TEXT NOT NULL,
  PRIMARY KEY (tenant_id, owner_id, command_id)
);
CREATE TABLE IF NOT EXISTS runtime_commands (
  tenant_id TEXT NOT NULL,
  owner_id TEXT NOT NULL,
  command_id TEXT NOT NULL,
  principal_id TEXT NOT NULL,
  digest TEXT NOT NULL,
  data BLOB NOT NULL CHECK (length(data) <= 262144),
  PRIMARY KEY (tenant_id, owner_id, command_id),
  FOREIGN KEY (tenant_id, owner_id, command_id) REFERENCES runtime_command_locks (tenant_id, owner_id, command_id) ON DELETE RESTRICT
);
CREATE TABLE IF NOT EXISTS runtime_jobs (
  tenant_id TEXT NOT NULL,
  owner_id TEXT NOT NULL,
  job_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  responsibility_key TEXT NOT NULL,
  source_ref BLOB NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('ready','leased','waiting','done')),
  due_at INTEGER NOT NULL,
  work_revision INTEGER NOT NULL CHECK (work_revision BETWEEN 1 AND 9007199254740991),
  lease_epoch INTEGER NOT NULL CHECK (lease_epoch BETWEEN 0 AND 9007199254740991),
  holder_id TEXT NOT NULL DEFAULT '',
  lease_until INTEGER NOT NULL DEFAULT 0,
  observed_work_revision INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (tenant_id, owner_id, job_id),
  UNIQUE (tenant_id, owner_id, kind, responsibility_key),
  CHECK ((state = 'leased' AND holder_id <> '' AND lease_epoch > 0 AND observed_work_revision > 0 AND lease_until > 0) OR (state <> 'leased' AND holder_id = '' AND lease_until = 0 AND observed_work_revision = 0))
);
CREATE INDEX IF NOT EXISTS runtime_jobs_due ON runtime_jobs (tenant_id, owner_id, kind, state, due_at, job_id) WHERE state <> 'done';
