ALTER TABLE jobs DROP CONSTRAINT jobs_state_check;
ALTER TABLE jobs ADD CONSTRAINT jobs_state_check CHECK (state IN ('ready','leased','done'));
ALTER TABLE jobs ADD COLUMN lease_epoch bigint NOT NULL DEFAULT 0 CHECK (lease_epoch >= 0);
ALTER TABLE jobs ADD COLUMN claimed_revision bigint;
ALTER TABLE jobs ADD COLUMN worker_id text;
ALTER TABLE jobs ADD COLUMN lease_until timestamptz;
ALTER TABLE jobs ADD CONSTRAINT jobs_claim_binding CHECK (
 (state = 'leased' AND lease_epoch > 0 AND claimed_revision IS NOT NULL AND claimed_revision > completed_revision AND claimed_revision <= work_revision AND worker_id IS NOT NULL AND length(worker_id) BETWEEN 1 AND 128 AND lease_until IS NOT NULL)
 OR (state <> 'leased' AND claimed_revision IS NULL AND worker_id IS NULL AND lease_until IS NULL)
);
ALTER TABLE jobs ADD CONSTRAINT jobs_progress_state CHECK (
 (state = 'done' AND completed_revision = work_revision) OR (state <> 'done' AND completed_revision < work_revision)
);
-- Index the actual eligibility boundary so a bounded scan does not walk all
-- live leases before finding its LIMIT. The bigint guard prevents epoch wrap.
ALTER TABLE jobs ADD COLUMN scan_at timestamptz GENERATED ALWAYS AS (
 CASE WHEN state = 'leased' THEN GREATEST(due_at,lease_until) ELSE due_at END
) STORED;
CREATE INDEX jobs_scan ON jobs (tenant_id,owner_id,scan_at,job_id)
 WHERE state <> 'done' AND lease_epoch < 9223372036854775807;
ALTER TABLE durable_inputs ADD COLUMN projected_revision bigint;
ALTER TABLE durable_inputs ADD COLUMN text_digest text;
ALTER TABLE durable_inputs ADD CONSTRAINT durable_inputs_projection CHECK (
 (projected_revision IS NULL AND text_digest IS NULL) OR
 (projected_revision IS NOT NULL AND text_digest IS NOT NULL AND projected_revision > 0 AND projected_revision <= revision AND text_digest ~ '^sha256:[0-9a-f]{64}$')
);
