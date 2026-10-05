-- 每个动作可有多个单次发送预留；旧记录按原标识迁移，重复启动不覆盖当前状态。
CREATE TABLE IF NOT EXISTS send_reservations (user_id TEXT NOT NULL, domain_id TEXT NOT NULL, id TEXT NOT NULL, operation_id TEXT NOT NULL, record BLOB NOT NULL, PRIMARY KEY(user_id,domain_id,id));
INSERT OR IGNORE INTO send_reservations SELECT * FROM reservations;
CREATE TABLE IF NOT EXISTS capability_versions (user_id TEXT NOT NULL, domain_id TEXT NOT NULL, id TEXT NOT NULL, revision INTEGER NOT NULL, record BLOB NOT NULL, PRIMARY KEY(user_id,domain_id,id,revision));
INSERT OR IGNORE INTO capability_versions SELECT user_id,domain_id,id,1,record FROM capabilities;
