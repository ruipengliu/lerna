CREATE TABLE runtime_commands_expanded (
  tenant_id TEXT NOT NULL,
  owner_id TEXT NOT NULL,
  command_id TEXT NOT NULL,
  principal_id TEXT NOT NULL,
  digest TEXT NOT NULL,
  data BLOB NOT NULL CHECK (length(data) <= 1048576),
  PRIMARY KEY (tenant_id, owner_id, command_id),
  FOREIGN KEY (tenant_id, owner_id, command_id) REFERENCES runtime_command_locks (tenant_id, owner_id, command_id) ON DELETE RESTRICT
);
INSERT INTO runtime_commands_expanded SELECT tenant_id, owner_id, command_id, principal_id, digest, data FROM runtime_commands;
DROP TABLE runtime_commands;
ALTER TABLE runtime_commands_expanded RENAME TO runtime_commands;
