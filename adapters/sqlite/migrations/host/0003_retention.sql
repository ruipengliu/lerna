ALTER TABLE durable_inputs ADD COLUMN body_gone INTEGER NOT NULL DEFAULT 0 CHECK (body_gone IN (0,1) AND (body_gone=0 OR length(text_value)=0));
ALTER TABLE command_receipts ADD COLUMN body_gone INTEGER NOT NULL DEFAULT 0 CHECK (body_gone IN (0,1));
