-- Additive Content-owner upgrade. Stop v1 writers before applying/backfilling.
ALTER TABLE jobs DROP CONSTRAINT jobs_phase_check;
ALTER TABLE jobs ADD CONSTRAINT jobs_phase_check CHECK(phase IN('publish','policy_propagation'));
CREATE TABLE content_source_generation (
 tenant_id text NOT NULL,owner_id text NOT NULL,generation bigint NOT NULL DEFAULT 0,
 PRIMARY KEY(tenant_id,owner_id)
);
CREATE TABLE content_source_versions (
 tenant_id text NOT NULL,owner_id text NOT NULL,object_id text NOT NULL,
 generation bigint NOT NULL,indexed boolean NOT NULL DEFAULT false,
 policy_cursor text NOT NULL DEFAULT '',maintenance_complete boolean NOT NULL DEFAULT false,
 PRIMARY KEY(tenant_id,owner_id,object_id),
 FOREIGN KEY(tenant_id,owner_id,object_id) REFERENCES content_versions(tenant_id,owner_id,object_id)
);
-- Existing versions remain unindexed until the new owner advances the durable
-- finite backfill seam. Generation zero freezes their membership immediately.
INSERT INTO content_source_versions(tenant_id,owner_id,object_id,generation)
 SELECT tenant_id,owner_id,object_id,0 FROM content_versions;
INSERT INTO content_source_generation(tenant_id,owner_id)
 SELECT DISTINCT tenant_id,owner_id FROM content_versions;
CREATE TABLE content_source_edges (
 tenant_id text NOT NULL,owner_id text NOT NULL,ancestor_id text NOT NULL,descendant_id text NOT NULL,
 ref bytea NOT NULL,generation bigint NOT NULL,
 PRIMARY KEY(tenant_id,owner_id,ancestor_id,descendant_id),
 FOREIGN KEY(tenant_id,owner_id,descendant_id) REFERENCES content_versions(tenant_id,owner_id,object_id)
);
CREATE INDEX content_descendants ON content_source_edges(tenant_id,owner_id,ancestor_id,descendant_id,generation);
CREATE TABLE content_policy_changes (
 tenant_id text NOT NULL,owner_id text NOT NULL,change_key text NOT NULL,
 object_id text,state text NOT NULL,due_at timestamptz NOT NULL,body bytea NOT NULL,
 PRIMARY KEY(tenant_id,owner_id,change_key)
);
CREATE TABLE content_cleanup_responsibilities (
 tenant_id text NOT NULL,owner_id text NOT NULL,change_key text NOT NULL,object_id text NOT NULL,body bytea NOT NULL,
 PRIMARY KEY(tenant_id,owner_id,change_key,object_id),
 FOREIGN KEY(tenant_id,owner_id,change_key) REFERENCES content_policy_changes(tenant_id,owner_id,change_key) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,owner_id,object_id) REFERENCES content_versions(tenant_id,owner_id,object_id)
);
CREATE TABLE content_policy_work (
 tenant_id text NOT NULL,owner_id text NOT NULL,object_id text NOT NULL,revision bigint NOT NULL,
 PRIMARY KEY(tenant_id,owner_id,object_id)
);
