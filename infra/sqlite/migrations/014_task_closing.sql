CREATE TABLE IF NOT EXISTS task_closings (
 user_id TEXT NOT NULL, domain_id TEXT NOT NULL, id TEXT NOT NULL, task_id TEXT NOT NULL, record BLOB NOT NULL,
 PRIMARY KEY(user_id,domain_id,id), UNIQUE(user_id,domain_id,task_id)
);
CREATE TABLE IF NOT EXISTS task_closure_intents (
 user_id TEXT NOT NULL, domain_id TEXT NOT NULL, id TEXT NOT NULL, record BLOB NOT NULL,
 PRIMARY KEY(user_id,domain_id,id)
);
CREATE TABLE IF NOT EXISTS task_closure_seals (
 user_id TEXT NOT NULL, domain_id TEXT NOT NULL, id TEXT NOT NULL, record BLOB NOT NULL,
 PRIMARY KEY(user_id,domain_id,id)
);
CREATE TABLE IF NOT EXISTS task_sealed_operations (
 user_id TEXT NOT NULL, domain_id TEXT NOT NULL, id TEXT NOT NULL, record BLOB NOT NULL,
 PRIMARY KEY(user_id,domain_id,id)
);
CREATE TABLE IF NOT EXISTS execution_followups (
 user_id TEXT NOT NULL, domain_id TEXT NOT NULL, id TEXT NOT NULL, operation_id TEXT NOT NULL, record BLOB NOT NULL,
 PRIMARY KEY(user_id,domain_id,id)
);
CREATE TABLE IF NOT EXISTS settlement_followups (
 user_id TEXT NOT NULL, domain_id TEXT NOT NULL, id TEXT NOT NULL, record BLOB NOT NULL,
 PRIMARY KEY(user_id,domain_id,id)
);
CREATE TABLE IF NOT EXISTS execution_followup_jobs (
 user_id TEXT NOT NULL, domain_id TEXT NOT NULL, id TEXT NOT NULL, operation_id TEXT NOT NULL, record BLOB NOT NULL,
 PRIMARY KEY(user_id,domain_id,id), UNIQUE(user_id,domain_id,operation_id)
);
