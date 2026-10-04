CREATE TABLE %s (version integer PRIMARY KEY,checksum text NOT NULL);
CREATE TABLE %s (decision_key text PRIMARY KEY,permission_key text NOT NULL,body bytea NOT NULL);
CREATE TABLE %s (permission_key text PRIMARY KEY,owner_key text NOT NULL,subject_key text NOT NULL,read_commands boolean NOT NULL,valid_until timestamptz NOT NULL,body bytea NOT NULL);
CREATE INDEX fixture_owner_read_scope ON %s(owner_key,subject_key,valid_until) WHERE read_commands;
CREATE TABLE %s (object_key text PRIMARY KEY,kind text NOT NULL,body bytea NOT NULL);
CREATE TABLE %s (permission_key text NOT NULL REFERENCES %s(permission_key),publication_key text NOT NULL,digest text NOT NULL,ref bytea NOT NULL,sources bytea NOT NULL,PRIMARY KEY(permission_key,publication_key));
