CREATE TABLE IF NOT EXISTS runtime_query_bindings (
  tenant_id TEXT NOT NULL,
  owner_id TEXT NOT NULL,
  query_id TEXT NOT NULL,
  binding_id TEXT NOT NULL,
  principal_id TEXT NOT NULL,
  credential_generation BIGINT NOT NULL CHECK (credential_generation BETWEEN 1 AND 9007199254740991),
  roles_digest TEXT NOT NULL CHECK (roles_digest ~ '^sha256:[0-9a-f]{64}$'),
  query_digest TEXT NOT NULL CHECK (query_digest ~ '^sha256:[0-9a-f]{64}$'),
  result_digest TEXT CHECK (result_digest IS NULL OR result_digest ~ '^sha256:[0-9a-f]{64}$'),
  expires_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (tenant_id, owner_id, query_id)
);
CREATE INDEX IF NOT EXISTS runtime_query_bindings_expiry ON runtime_query_bindings (tenant_id, owner_id, expires_at, query_id);
