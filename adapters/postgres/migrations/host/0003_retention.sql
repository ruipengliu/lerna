ALTER TABLE durable_inputs ADD COLUMN body_gone boolean NOT NULL DEFAULT false CHECK (NOT body_gone OR octet_length(text_value)=0);
ALTER TABLE command_receipts ADD COLUMN body_gone boolean NOT NULL DEFAULT false;
