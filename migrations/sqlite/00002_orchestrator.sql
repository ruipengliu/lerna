-- +goose Up
CREATE TABLE orchestrator_tasks (
 tenant_id TEXT NOT NULL, owner_id TEXT NOT NULL, task_id TEXT NOT NULL,
 subject_id TEXT NOT NULL, parent_task_id TEXT NOT NULL DEFAULT '', depth BIGINT NOT NULL,
 created_at BIGINT NOT NULL, revision BIGINT NOT NULL, status TEXT NOT NULL,
 data TEXT NOT NULL, PRIMARY KEY(tenant_id,owner_id,task_id),
 CHECK(depth BETWEEN 0 AND 4), CHECK(revision BETWEEN 1 AND 9007199254740991),
 CHECK(status IN ('active','succeeded','failed','cancelled'))
);
CREATE INDEX orchestrator_tasks_list ON orchestrator_tasks(tenant_id,owner_id,created_at DESC,task_id);
CREATE INDEX orchestrator_tasks_active_children ON orchestrator_tasks(tenant_id,owner_id,parent_task_id,depth,task_id) WHERE status='active';
CREATE INDEX orchestrator_tasks_active_recovery ON orchestrator_tasks(tenant_id,owner_id,task_id) WHERE status='active';
CREATE TABLE orchestrator_balances (
 tenant_id TEXT NOT NULL, owner_id TEXT NOT NULL, task_id TEXT NOT NULL, unit TEXT NOT NULL,
 limit_value TEXT NOT NULL, spent TEXT NOT NULL, reserved TEXT NOT NULL,
 PRIMARY KEY(tenant_id,owner_id,task_id,unit),
 FOREIGN KEY(tenant_id,owner_id,task_id) REFERENCES orchestrator_tasks(tenant_id,owner_id,task_id)
);
CREATE TABLE orchestrator_records (
 tenant_id TEXT NOT NULL, owner_id TEXT NOT NULL, kind TEXT NOT NULL, id TEXT NOT NULL,
 task_id TEXT NOT NULL, revision BIGINT NOT NULL, state TEXT NOT NULL,
 current_key TEXT NOT NULL DEFAULT '', immutable BOOLEAN NOT NULL, data TEXT NOT NULL,
 PRIMARY KEY(tenant_id,owner_id,kind,id),
 CHECK(revision BETWEEN 1 AND 9007199254740991)
);
CREATE UNIQUE INDEX orchestrator_records_current ON orchestrator_records(tenant_id,owner_id,kind,task_id,current_key) WHERE current_key<>'';
CREATE INDEX orchestrator_records_task ON orchestrator_records(tenant_id,owner_id,task_id,kind,id);
CREATE INDEX orchestrator_records_open ON orchestrator_records(tenant_id,owner_id,kind,state,id) WHERE state='open';
CREATE INDEX orchestrator_records_current_scan ON orchestrator_records(tenant_id,owner_id,task_id,kind,id) WHERE current_key<>'';
CREATE INDEX orchestrator_records_open_task ON orchestrator_records(tenant_id,owner_id,task_id,kind,id) WHERE state='open';
CREATE INDEX orchestrator_records_recovery ON orchestrator_records(tenant_id,owner_id,task_id,kind) WHERE state='open';
CREATE TABLE orchestrator_gates (
 tenant_id TEXT NOT NULL, owner_id TEXT NOT NULL, gate_key TEXT NOT NULL,
 revision BIGINT NOT NULL DEFAULT 1, PRIMARY KEY(tenant_id,owner_id,gate_key)
);
CREATE TABLE orchestrator_receivers (
 tenant_id TEXT NOT NULL, owner_id TEXT NOT NULL, allocation_id TEXT NOT NULL,
 data TEXT NOT NULL, PRIMARY KEY(tenant_id,owner_id,allocation_id)
);
CREATE TABLE orchestrator_user_capacity (
 tenant_id TEXT NOT NULL, owner_id TEXT NOT NULL, subject_id TEXT NOT NULL,
 active_count BIGINT NOT NULL DEFAULT 0 CHECK(active_count>=0),
 PRIMARY KEY(tenant_id,owner_id,subject_id)
);

CREATE TABLE orchestrator_work_routes (
 tenant_id TEXT NOT NULL, owner_id TEXT NOT NULL, kind TEXT NOT NULL, responsibility_key TEXT NOT NULL,
 subject_id TEXT NOT NULL, task_id TEXT NOT NULL, provider_id TEXT NOT NULL, resource_id TEXT NOT NULL,
 PRIMARY KEY(tenant_id,owner_id,kind,responsibility_key)
);
CREATE INDEX orchestrator_work_provider ON orchestrator_work_routes(tenant_id,owner_id,provider_id,kind,responsibility_key);
CREATE INDEX orchestrator_work_resource ON orchestrator_work_routes(tenant_id,owner_id,resource_id,kind,responsibility_key);
CREATE TABLE orchestrator_scheduler (
 tenant_id TEXT NOT NULL, owner_id TEXT NOT NULL, kind TEXT NOT NULL, turn BIGINT NOT NULL DEFAULT 0,
 PRIMARY KEY(tenant_id,owner_id,kind)
);
CREATE TABLE orchestrator_schedule_turns (
 tenant_id TEXT NOT NULL, owner_id TEXT NOT NULL, kind TEXT NOT NULL, dimension TEXT NOT NULL, entity_id TEXT NOT NULL, turn BIGINT NOT NULL,
 PRIMARY KEY(tenant_id,owner_id,kind,dimension,entity_id)
);

-- +goose Down
DROP TABLE orchestrator_schedule_turns;
DROP TABLE orchestrator_scheduler;
DROP TABLE orchestrator_work_routes;
DROP TABLE orchestrator_user_capacity;
DROP TABLE orchestrator_receivers;
DROP TABLE orchestrator_gates;
DROP TABLE orchestrator_records;
DROP TABLE orchestrator_balances;
DROP TABLE orchestrator_tasks;
