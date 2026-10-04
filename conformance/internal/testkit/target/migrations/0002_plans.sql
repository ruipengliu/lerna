-- Extend this target owner's published v1 ledger without changing 0001.
ALTER TABLE target_migrations RENAME TO target_migrations_v1;
CREATE TABLE target_migrations (
 version INTEGER PRIMARY KEY CHECK(version IN (1,2)),
 checksum TEXT NOT NULL
);
INSERT INTO target_migrations SELECT * FROM target_migrations_v1;
DROP TABLE target_migrations_v1;
ALTER TABLE target_receives RENAME TO target_receives_v1;
CREATE TABLE target_receives (
 sequence INTEGER PRIMARY KEY AUTOINCREMENT,
 original_key TEXT NOT NULL REFERENCES target_requests(original_key),
 at_ns INTEGER NOT NULL,
 digest TEXT NOT NULL,
 outcome TEXT NOT NULL CHECK(outcome IN ('applied','received','replayed','conflict','guarantee_expired','pending'))
);
INSERT INTO target_receives SELECT * FROM target_receives_v1;
DROP TABLE target_receives_v1;
CREATE TABLE target_pending (
 original_key TEXT PRIMARY KEY REFERENCES target_requests(original_key),
 data BLOB NOT NULL
);
CREATE TABLE target_plans (
 plan_id TEXT PRIMARY KEY,
 configuration BLOB NOT NULL,
 digest TEXT NOT NULL,
 cursor INTEGER NOT NULL CHECK(cursor BETWEEN 0 AND 64)
);
CREATE TABLE target_plan_events (
 plan_id TEXT NOT NULL REFERENCES target_plans(plan_id),
 event_id TEXT NOT NULL,
 step INTEGER NOT NULL CHECK(step BETWEEN 0 AND 63),
 kind TEXT NOT NULL CHECK(kind IN ('receive','apply','write','before_commit','drop_response')),
 outcome TEXT NOT NULL,
 phase TEXT NOT NULL,
 PRIMARY KEY(plan_id,event_id),
 UNIQUE(plan_id,step)
);
