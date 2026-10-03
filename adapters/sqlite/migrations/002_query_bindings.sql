CREATE TABLE IF NOT EXISTS runtime_query_bindings (
  tenant_id TEXT NOT NULL,
  owner_id TEXT NOT NULL,
  query_id TEXT NOT NULL,
  binding_id TEXT NOT NULL,
  principal_id TEXT NOT NULL,
  credential_generation INTEGER NOT NULL CHECK (credential_generation BETWEEN 1 AND 9007199254740991),
  roles_digest TEXT NOT NULL CHECK (length(roles_digest) = 71),
  query_digest TEXT NOT NULL CHECK (length(query_digest) = 71),
  result_digest TEXT CHECK (result_digest IS NULL OR length(result_digest) = 71),
  expires_at INTEGER NOT NULL,
  PRIMARY KEY (tenant_id, owner_id, query_id)
);
CREATE INDEX IF NOT EXISTS runtime_query_bindings_expiry ON runtime_query_bindings (tenant_id, owner_id, expires_at, query_id);
