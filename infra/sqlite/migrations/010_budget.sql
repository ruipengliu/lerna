CREATE TABLE IF NOT EXISTS budget_versions (
 user_id TEXT NOT NULL, domain_id TEXT NOT NULL, id TEXT NOT NULL,
 revision INTEGER NOT NULL, record BLOB NOT NULL,
 PRIMARY KEY(user_id,domain_id,id,revision)
);
CREATE TABLE IF NOT EXISTS billing_sources (user_id TEXT NOT NULL, domain_id TEXT NOT NULL, send_id TEXT NOT NULL, record BLOB NOT NULL, PRIMARY KEY(user_id,domain_id,send_id));
CREATE TABLE IF NOT EXISTS billing_source_versions (user_id TEXT NOT NULL, domain_id TEXT NOT NULL, id TEXT NOT NULL, revision INTEGER NOT NULL, record BLOB NOT NULL, PRIMARY KEY(user_id,domain_id,id,revision));
CREATE TABLE IF NOT EXISTS billing_entries (user_id TEXT NOT NULL, domain_id TEXT NOT NULL, id TEXT NOT NULL, source_id TEXT NOT NULL, source_version INTEGER NOT NULL, record BLOB NOT NULL, PRIMARY KEY(user_id,domain_id,id), UNIQUE(user_id,domain_id,source_id,source_version));
CREATE TABLE IF NOT EXISTS billing_aliases (user_id TEXT NOT NULL, alias TEXT NOT NULL, send_id TEXT NOT NULL, PRIMARY KEY(user_id,alias));
CREATE TABLE IF NOT EXISTS reservation_releases (user_id TEXT NOT NULL, domain_id TEXT NOT NULL, reservation_id TEXT NOT NULL, record BLOB NOT NULL, PRIMARY KEY(user_id,domain_id,reservation_id));
CREATE TABLE IF NOT EXISTS billing_conflicts (user_id TEXT NOT NULL, domain_id TEXT NOT NULL, id TEXT NOT NULL, record BLOB NOT NULL, PRIMARY KEY(user_id,domain_id,id));
