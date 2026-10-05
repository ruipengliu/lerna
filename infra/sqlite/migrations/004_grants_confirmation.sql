CREATE TABLE IF NOT EXISTS exit_credentials (user_id TEXT NOT NULL, domain_id TEXT NOT NULL, id TEXT NOT NULL, record BLOB NOT NULL, PRIMARY KEY(user_id,domain_id,id));
CREATE TABLE IF NOT EXISTS exit_credential_uses (user_id TEXT NOT NULL, domain_id TEXT NOT NULL, credential_id TEXT NOT NULL, record BLOB NOT NULL, PRIMARY KEY(user_id,domain_id,credential_id));
