CREATE TABLE IF NOT EXISTS target_migrations (
 version INTEGER PRIMARY KEY CHECK(version=1),
 checksum TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS target_identity (
 singleton INTEGER PRIMARY KEY CHECK(singleton=1),
 identity TEXT NOT NULL UNIQUE,
 database_id TEXT NOT NULL UNIQUE,
 window_ns INTEGER NOT NULL CHECK(window_ns>0),
 query_mode TEXT NOT NULL CHECK(query_mode IN ('enabled','disabled'))
);
CREATE TABLE IF NOT EXISTS target_requests (
 original_key TEXT PRIMARY KEY,
 resource TEXT NOT NULL,
 digest TEXT NOT NULL,
 start_ns INTEGER NOT NULL,
 deadline_ns INTEGER NOT NULL CHECK(deadline_ns>start_ns)
);
CREATE TABLE IF NOT EXISTS target_commits (
 original_key TEXT PRIMARY KEY REFERENCES target_requests(original_key),
 version INTEGER NOT NULL CHECK(version>0),
 data BLOB NOT NULL
);
CREATE TABLE IF NOT EXISTS target_values (
 resource TEXT PRIMARY KEY,
 version INTEGER NOT NULL CHECK(version>0),
 data BLOB NOT NULL
);
CREATE TABLE IF NOT EXISTS target_receives (
 sequence INTEGER PRIMARY KEY AUTOINCREMENT,
 original_key TEXT NOT NULL REFERENCES target_requests(original_key),
 at_ns INTEGER NOT NULL,
 digest TEXT NOT NULL,
 outcome TEXT NOT NULL CHECK(outcome IN ('applied','replayed','conflict','guarantee_expired'))
);
