-- Additive Content owner protocol. Old non-fencing writers must be stopped.
-- Unknown legacy media remains unbound until separately qualified by its owner.
ALTER TABLE content_versions ADD COLUMN primary_holder_binding text NOT NULL DEFAULT '';
ALTER TABLE content_versions ADD COLUMN body_seal bytea;
ALTER TABLE content_versions ADD COLUMN body_gone boolean NOT NULL DEFAULT false;
ALTER TABLE content_versions ADD CONSTRAINT content_body_gone_check CHECK(NOT body_gone OR (body_seal IS NOT NULL AND staging IS NULL));
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
CREATE TABLE content_metadata_policies (
 tenant_id text NOT NULL, owner_id text NOT NULL, subject_key text NOT NULL,
 content_id text NOT NULL, version text NOT NULL, purpose text NOT NULL,
 revision bigint NOT NULL CHECK(revision>0), valid_until timestamptz NOT NULL,
 body bytea NOT NULL,
 PRIMARY KEY(tenant_id,owner_id,subject_key,content_id,version,purpose)
);

-- Original finite copy duty is fixed before Tx-external transfer, including
-- copies not yet confirmed when a later seal closes all registered holders.
CREATE TABLE content_secondary_copies (
 tenant_id text NOT NULL, owner_id text NOT NULL, object_id text NOT NULL,
 holder_id text NOT NULL, binding text NOT NULL, copy_id text NOT NULL,
 confirmed boolean NOT NULL, body bytea NOT NULL,
 UNIQUE(tenant_id,owner_id,object_id),
 PRIMARY KEY(tenant_id,owner_id,object_id,holder_id),
 FOREIGN KEY(tenant_id,owner_id,object_id) REFERENCES content_versions(tenant_id,owner_id,object_id),
 CHECK(length(holder_id) BETWEEN 1 AND 128), CHECK(length(binding)>0)
);
