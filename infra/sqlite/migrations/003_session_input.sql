CREATE TABLE IF NOT EXISTS task_inputs(user_id TEXT NOT NULL, domain_id TEXT NOT NULL, id TEXT NOT NULL, record BLOB NOT NULL, PRIMARY KEY(user_id,domain_id,id));
CREATE TABLE IF NOT EXISTS questions(user_id TEXT NOT NULL, domain_id TEXT NOT NULL, id TEXT NOT NULL, record BLOB NOT NULL, PRIMARY KEY(user_id,domain_id,id));
CREATE TABLE IF NOT EXISTS input_deliveries(user_id TEXT NOT NULL, domain_id TEXT NOT NULL, id TEXT NOT NULL, record BLOB NOT NULL, PRIMARY KEY(user_id,domain_id,id));
