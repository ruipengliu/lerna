-- 每行属于 producer 对应的源事务域；运行记录域只读取，不在接纳时修改。
CREATE TABLE IF NOT EXISTS source_trace_outbox (
 user_id TEXT NOT NULL, producer TEXT NOT NULL, seq INTEGER NOT NULL,
 issuer TEXT NOT NULL, target_domain TEXT NOT NULL, id TEXT NOT NULL,
 fingerprint TEXT NOT NULL, record BLOB NOT NULL,
 PRIMARY KEY(user_id,producer,seq), UNIQUE(user_id,issuer,target_domain,id)
);
CREATE TABLE IF NOT EXISTS source_trace_streams (
 user_id TEXT NOT NULL, producer TEXT NOT NULL, seq INTEGER NOT NULL,
 PRIMARY KEY(user_id,producer)
);
-- 此表属于运行记录域；只索引已经独立接纳的记录。
CREATE TABLE IF NOT EXISTS trace_indexes (
 user_id TEXT NOT NULL, id TEXT NOT NULL, PRIMARY KEY(user_id,id)
);
