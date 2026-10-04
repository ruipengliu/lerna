-- Additive Content owner protocol. Old non-fencing writers must be stopped.
ALTER TABLE content_versions ADD COLUMN body_seal bytea;
ALTER TABLE jobs DROP CONSTRAINT jobs_phase_check;
ALTER TABLE jobs ADD CONSTRAINT jobs_phase_check CHECK(phase IN('publish','policy_propagation','body_cleanup'));
CREATE TABLE content_body_holders (
 tenant_id text NOT NULL, owner_id text NOT NULL, object_id text NOT NULL,
 holder_id text NOT NULL, seal_id text NOT NULL, state text NOT NULL,
 body bytea NOT NULL,
 PRIMARY KEY(tenant_id,owner_id,object_id,holder_id),
 FOREIGN KEY(tenant_id,owner_id,object_id) REFERENCES content_versions(tenant_id,owner_id,object_id),
 CHECK(length(holder_id) BETWEEN 1 AND 128),
 CHECK(state IN('pending','residual','erased'))
);
CREATE TABLE content_publication_attempts (
 tenant_id text NOT NULL, owner_id text NOT NULL, object_id text NOT NULL,
 attempt_key text NOT NULL, body bytea NOT NULL,
 PRIMARY KEY(tenant_id,owner_id,object_id,attempt_key),
 FOREIGN KEY(tenant_id,owner_id,object_id) REFERENCES content_versions(tenant_id,owner_id,object_id)
);
