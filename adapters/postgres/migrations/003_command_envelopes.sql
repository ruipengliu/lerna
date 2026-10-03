ALTER TABLE runtime_commands DROP CONSTRAINT runtime_commands_data_check;
ALTER TABLE runtime_commands ADD CONSTRAINT runtime_commands_data_check CHECK (octet_length(data) <= 1048576);
