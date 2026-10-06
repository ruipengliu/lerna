CREATE TABLE IF NOT EXISTS reasoner_drivers (user_id TEXT NOT NULL, domain_id TEXT NOT NULL, task_id TEXT NOT NULL, record BLOB NOT NULL, PRIMARY KEY(user_id,domain_id,task_id));
CREATE TABLE IF NOT EXISTS reasoner_driver_versions (user_id TEXT NOT NULL, domain_id TEXT NOT NULL, id TEXT NOT NULL, revision INTEGER NOT NULL, record BLOB NOT NULL, PRIMARY KEY(user_id,domain_id,id,revision));
