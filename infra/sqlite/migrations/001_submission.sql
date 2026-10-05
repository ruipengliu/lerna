CREATE TABLE IF NOT EXISTS domain_config (singleton INTEGER PRIMARY KEY CHECK(singleton=1), user_id TEXT NOT NULL, domain_id TEXT NOT NULL, durability_profile TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS commit_clock (singleton INTEGER PRIMARY KEY CHECK(singleton=1), position INTEGER NOT NULL);
INSERT OR IGNORE INTO commit_clock VALUES(1,0);
CREATE TABLE IF NOT EXISTS command_receipts (user_id TEXT NOT NULL, issuer_id TEXT NOT NULL, domain_id TEXT NOT NULL, command_id TEXT NOT NULL, record BLOB NOT NULL, PRIMARY KEY(user_id,issuer_id,domain_id,command_id));
CREATE TABLE IF NOT EXISTS jobs (user_id TEXT NOT NULL, domain_id TEXT NOT NULL, id TEXT NOT NULL, purpose_key TEXT NOT NULL, state TEXT NOT NULL, record BLOB NOT NULL, PRIMARY KEY(user_id,domain_id,id), UNIQUE(user_id,domain_id,purpose_key));
CREATE TABLE IF NOT EXISTS tasks (user_id TEXT NOT NULL, domain_id TEXT NOT NULL, id TEXT NOT NULL, record BLOB NOT NULL, PRIMARY KEY(user_id,domain_id,id));
CREATE TABLE IF NOT EXISTS sessions (user_id TEXT NOT NULL, domain_id TEXT NOT NULL, id TEXT NOT NULL, record BLOB NOT NULL, PRIMARY KEY(user_id,domain_id,id));
CREATE TABLE IF NOT EXISTS content (user_id TEXT NOT NULL, domain_id TEXT NOT NULL, id TEXT NOT NULL, source_issuer TEXT NOT NULL, source_command TEXT NOT NULL, fingerprint TEXT NOT NULL, record BLOB NOT NULL, PRIMARY KEY(user_id,domain_id,id), UNIQUE(user_id,domain_id,source_issuer,source_command,fingerprint));
