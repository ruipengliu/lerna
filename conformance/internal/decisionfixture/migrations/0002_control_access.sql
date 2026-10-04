CREATE TABLE %s (
  owner_key text NOT NULL,
  subject_key text NOT NULL,
  decision_key text NOT NULL,
  valid_until timestamptz NOT NULL,
  body bytea NOT NULL,
  PRIMARY KEY(owner_key,subject_key,decision_key)
);
