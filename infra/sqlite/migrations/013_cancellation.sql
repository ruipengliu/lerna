CREATE TABLE IF NOT EXISTS cancellations (
 user_id TEXT NOT NULL, domain_id TEXT NOT NULL, task_id TEXT NOT NULL, record BLOB NOT NULL,
 PRIMARY KEY(user_id,domain_id,task_id)
);
CREATE TABLE IF NOT EXISTS cancellation_intents (
 user_id TEXT NOT NULL, domain_id TEXT NOT NULL, id TEXT NOT NULL, record BLOB NOT NULL,
 PRIMARY KEY(user_id,domain_id,id)
);
CREATE TABLE IF NOT EXISTS cancellation_seals (
 user_id TEXT NOT NULL, domain_id TEXT NOT NULL, id TEXT NOT NULL, record BLOB NOT NULL,
 PRIMARY KEY(user_id,domain_id,id)
);
CREATE TABLE IF NOT EXISTS cancellation_sealed_operations (
 user_id TEXT NOT NULL, domain_id TEXT NOT NULL, id TEXT NOT NULL, record BLOB NOT NULL,
 PRIMARY KEY(user_id,domain_id,id)
);
