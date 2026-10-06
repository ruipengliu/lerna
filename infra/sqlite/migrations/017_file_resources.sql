CREATE TABLE IF NOT EXISTS file_resources (user_id TEXT NOT NULL, id TEXT NOT NULL, send_id TEXT NOT NULL, record BLOB NOT NULL, PRIMARY KEY(user_id,id), UNIQUE(user_id,send_id));
CREATE TABLE IF NOT EXISTS managed_file_roots (user_id TEXT NOT NULL, root_id TEXT NOT NULL, record BLOB NOT NULL, PRIMARY KEY(user_id,root_id));
