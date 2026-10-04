CREATE TABLE decision_stops (
 tenant_id text NOT NULL, owner_id text NOT NULL, decision_id text NOT NULL,
 control_revision numeric(19,0) NOT NULL CHECK(control_revision>=0 AND control_revision<=9223372036854775807),
 body bytea NOT NULL,
 PRIMARY KEY(tenant_id,owner_id,decision_id),
 FOREIGN KEY(tenant_id,owner_id,decision_id) REFERENCES decisions(tenant_id,owner_id,decision_id)
);
