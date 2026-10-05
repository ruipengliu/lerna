ALTER TABLE jobs DROP CONSTRAINT jobs_state_check;
ALTER TABLE jobs ADD CONSTRAINT jobs_state_check CHECK (state IN ('ready','leased','waiting','done'));
CREATE TABLE durable_schedules (
 tenant_id TEXT NOT NULL, owner_id TEXT NOT NULL, object_id TEXT NOT NULL,
 input_revision bigint NOT NULL CHECK (input_revision>0),
 policy TEXT NOT NULL, binding_source TEXT NOT NULL CHECK (binding_source IN ('admission','legacy-adoption')), adopted_at timestamptz NOT NULL, deadline timestamptz NOT NULL,
 attempts bigint NOT NULL CHECK (attempts BETWEEN 0 AND 64),
 start_epoch bigint NOT NULL CHECK (start_epoch>=0),
 outcome TEXT NOT NULL CHECK (outcome IN ('','running','waiting','retry','success','permanent','expired','stopped')),
 reason TEXT NOT NULL, due_at timestamptz NOT NULL, stopped INTEGER NOT NULL CHECK (stopped IN (0,1)),
 PRIMARY KEY (tenant_id,owner_id,object_id,input_revision),
 FOREIGN KEY (tenant_id,owner_id,object_id) REFERENCES durable_inputs(tenant_id,owner_id,object_id),
 CHECK (deadline>adopted_at)
);
CREATE TABLE durable_gates (
 tenant_id TEXT NOT NULL,owner_id TEXT NOT NULL,gate_id TEXT NOT NULL,
 revision bigint NOT NULL CHECK (revision>=0),
 PRIMARY KEY (tenant_id,owner_id,gate_id)
);
