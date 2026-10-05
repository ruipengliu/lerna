CREATE TABLE IF NOT EXISTS verifications (
 user_id TEXT NOT NULL, domain_id TEXT NOT NULL, id TEXT NOT NULL, record BLOB NOT NULL,
 PRIMARY KEY(user_id,domain_id,id)
);
CREATE TABLE IF NOT EXISTS verification_versions (
 user_id TEXT NOT NULL, domain_id TEXT NOT NULL, id TEXT NOT NULL, revision INTEGER NOT NULL, record BLOB NOT NULL,
 PRIMARY KEY(user_id,domain_id,id,revision)
);
CREATE TABLE IF NOT EXISTS results (
 user_id TEXT NOT NULL, domain_id TEXT NOT NULL, task_id TEXT NOT NULL, record BLOB NOT NULL,
 PRIMARY KEY(user_id,domain_id,task_id)
);
CREATE TABLE IF NOT EXISTS completion_intents (
 user_id TEXT NOT NULL, domain_id TEXT NOT NULL, id TEXT NOT NULL, record BLOB NOT NULL,
 PRIMARY KEY(user_id,domain_id,id)
);
CREATE TABLE IF NOT EXISTS completion_seals (
 user_id TEXT NOT NULL, domain_id TEXT NOT NULL, id TEXT NOT NULL, record BLOB NOT NULL,
 PRIMARY KEY(user_id,domain_id,id)
);
CREATE TABLE IF NOT EXISTS completion_sealed_operations (
 user_id TEXT NOT NULL, domain_id TEXT NOT NULL, id TEXT NOT NULL, record BLOB NOT NULL,
 PRIMARY KEY(user_id,domain_id,id)
);
