-- Runtime persistence design v1. No execution, authorization, or UI tables.
-- Connection PRAGMAs, file permissions and durability checks are in 01B.
PRAGMA foreign_keys = ON;

CREATE TABLE rt_meta (
  singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
  format_version INTEGER NOT NULL CHECK (format_version = 1),
  boot_epoch INTEGER NOT NULL CHECK (boot_epoch > 0),
  write_state TEXT NOT NULL CHECK (write_state IN ('RECOVERING','OPEN','DRAINING','SEALED')),
  configuration_digest BLOB NOT NULL CHECK (length(configuration_digest) = 32)
);

CREATE TABLE rt_task (
  namespace TEXT NOT NULL,
  task_key TEXT NOT NULL,
  state_version INTEGER NOT NULL CHECK (state_version > 0),
  goal_revision INTEGER NOT NULL CHECK (goal_revision > 0),
  owner_ref TEXT NOT NULL,
  owner_epoch INTEGER NOT NULL CHECK (owner_epoch > 0),
  owner_phase TEXT NOT NULL CHECK (owner_phase IN ('ACTIVE','PREPARING','SEALED')),
  state TEXT NOT NULL CHECK (state IN ('QUEUED','RUNNING','WAITING','COMPLETED','FAILED','CANCELLED')),
  effective_control TEXT NOT NULL CHECK (effective_control IN ('RUN','PAUSE','CANCEL')),
  deadline_ms INTEGER NOT NULL,
  next_decision_seq INTEGER NOT NULL CHECK (next_decision_seq > 0),
  decision_needed INTEGER NOT NULL CHECK (decision_needed IN (0,1)),
  created_ms INTEGER NOT NULL,
  updated_ms INTEGER NOT NULL,
  document BLOB NOT NULL,
  PRIMARY KEY (namespace, task_key)
);

CREATE TABLE rt_admission_window (
  namespace TEXT NOT NULL,
  window_key TEXT NOT NULL,
  authority_ref TEXT NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('OPEN','CLOSED')),
  accept_until_ms INTEGER NOT NULL,
  document BLOB NOT NULL,
  PRIMARY KEY (namespace, window_key)
);

CREATE TABLE rt_operation (
  namespace TEXT NOT NULL,
  operation_id TEXT NOT NULL,
  task_key TEXT NOT NULL,
  window_key TEXT NOT NULL,
  kind TEXT NOT NULL,
  intent_schema TEXT NOT NULL,
  intent BLOB NOT NULL,
  intent_digest BLOB NOT NULL CHECK (length(intent_digest) = 32),
  admitted_goal_revision INTEGER NOT NULL CHECK (admitted_goal_revision > 0),
  applicable_goal_revision INTEGER NOT NULL CHECK (applicable_goal_revision > 0),
  admission_state TEXT NOT NULL CHECK (admission_state IN ('ACTIVE','FROZEN','DROPPED')),
  dispatch_phase TEXT NOT NULL CHECK (dispatch_phase IN ('UNSENT','MAY_HAVE_SENT','ACKNOWLEDGED')),
  report_revision INTEGER NOT NULL CHECK (report_revision >= 0),
  result_document BLOB,
  PRIMARY KEY (namespace, operation_id),
  UNIQUE (namespace, task_key, operation_id),
  FOREIGN KEY (namespace, task_key) REFERENCES rt_task(namespace, task_key),
  FOREIGN KEY (namespace, window_key) REFERENCES rt_admission_window(namespace, window_key),
  CHECK (admission_state != 'DROPPED' OR dispatch_phase = 'UNSENT')
);
CREATE TRIGGER rt_operation_immutable
BEFORE UPDATE OF namespace, operation_id, task_key, window_key, kind,
  intent_schema, intent, intent_digest, admitted_goal_revision ON rt_operation
BEGIN
  SELECT RAISE(ABORT, 'immutable operation identity or intent');
END;

CREATE TABLE rt_work (
  namespace TEXT NOT NULL,
  task_key TEXT NOT NULL,
  work_key TEXT NOT NULL,
  active_key TEXT NOT NULL,
  operation_id TEXT,
  kind TEXT NOT NULL CHECK (kind IN ('DECIDE','DISPATCH','APPLY_INBOX','RECONCILE','PROPAGATE_CONTROL','DELIVER','RECOVER')),
  lane TEXT NOT NULL CHECK (lane IN ('TARGET','CONTROL','DISPOSITION')),
  state TEXT NOT NULL CHECK (state IN ('PENDING','BLOCKED','LEASED','DONE','DROPPED')),
  goal_frozen INTEGER NOT NULL CHECK (goal_frozen IN (0,1)),
  due_ms INTEGER NOT NULL,
  enqueued_ms INTEGER NOT NULL,
  claim_epoch INTEGER NOT NULL CHECK (claim_epoch >= 0),
  worker_ref TEXT,
  claim_mode TEXT CHECK (claim_mode IN ('EXECUTE','RECOVER')),
  lease_boot_epoch INTEGER,
  lease_owner_epoch INTEGER,
  lease_until_ms INTEGER,
  attempt INTEGER NOT NULL CHECK (attempt >= 0),
  document BLOB NOT NULL,
  PRIMARY KEY (namespace, task_key, work_key),
  FOREIGN KEY (namespace, task_key) REFERENCES rt_task(namespace, task_key),
  FOREIGN KEY (namespace, task_key, operation_id) REFERENCES rt_operation(namespace, task_key, operation_id),
  CHECK ((state = 'LEASED' AND worker_ref IS NOT NULL AND claim_mode IS NOT NULL AND lease_boot_epoch IS NOT NULL
    AND lease_owner_epoch IS NOT NULL AND lease_until_ms IS NOT NULL AND claim_epoch > 0)
    OR (state != 'LEASED' AND worker_ref IS NULL AND claim_mode IS NULL AND lease_boot_epoch IS NULL
    AND lease_owner_epoch IS NULL AND lease_until_ms IS NULL))
);
CREATE UNIQUE INDEX rt_work_active ON rt_work(namespace, task_key, active_key)
  WHERE state IN ('PENDING','BLOCKED','LEASED');
CREATE INDEX rt_work_namespaces ON rt_work(namespace)
  WHERE state IN ('PENDING','BLOCKED','LEASED');
CREATE INDEX rt_work_due ON rt_work(namespace, lane, due_ms, task_key, work_key)
  WHERE state = 'PENDING' AND goal_frozen = 0;
CREATE INDEX rt_work_expired ON rt_work(namespace, lease_until_ms, task_key, work_key)
  WHERE state = 'LEASED';

CREATE TABLE rt_dependency (
  namespace TEXT NOT NULL,
  task_key TEXT NOT NULL,
  operation_id TEXT NOT NULL,
  predecessor_id TEXT NOT NULL,
  predicate TEXT NOT NULL CHECK (predicate IN ('SUCCESS_CONFIRMED','FINISHED_KNOWN','DELIVERY_CONFIRMED')),
  PRIMARY KEY (namespace, task_key, operation_id, predecessor_id, predicate),
  FOREIGN KEY (namespace, task_key, operation_id) REFERENCES rt_operation(namespace, task_key, operation_id),
  FOREIGN KEY (namespace, task_key, predecessor_id) REFERENCES rt_operation(namespace, task_key, operation_id),
  CHECK (operation_id != predecessor_id)
);
CREATE INDEX rt_dependency_reverse ON rt_dependency(namespace, task_key, predecessor_id);

CREATE TABLE rt_decision (
  namespace TEXT NOT NULL,
  task_key TEXT NOT NULL,
  decision_seq INTEGER NOT NULL CHECK (decision_seq > 0),
  work_key TEXT NOT NULL,
  claim_epoch INTEGER NOT NULL CHECK (claim_epoch > 0),
  base_version INTEGER NOT NULL CHECK (base_version > 0),
  state TEXT NOT NULL CHECK (state IN ('ACTIVE','ADMITTED','RETIRED','REJECTED')),
  binding_document BLOB NOT NULL,
  context_document BLOB,
  admission_change_id TEXT,
  PRIMARY KEY (namespace, task_key, decision_seq),
  FOREIGN KEY (namespace, task_key, work_key) REFERENCES rt_work(namespace, task_key, work_key)
);
CREATE UNIQUE INDEX rt_decision_active ON rt_decision(namespace, task_key) WHERE state = 'ACTIVE';

CREATE TABLE rt_budget (
  namespace TEXT NOT NULL,
  task_key TEXT NOT NULL,
  dimension TEXT NOT NULL,
  limit_amount INTEGER NOT NULL CHECK (limit_amount >= 0),
  used_amount INTEGER NOT NULL CHECK (used_amount >= 0),
  held_amount INTEGER NOT NULL CHECK (held_amount >= 0),
  PRIMARY KEY (namespace, task_key, dimension),
  FOREIGN KEY (namespace, task_key) REFERENCES rt_task(namespace, task_key)
);
CREATE TABLE rt_reservation (
  namespace TEXT NOT NULL,
  task_key TEXT NOT NULL,
  reservation_key TEXT NOT NULL,
  dimension TEXT NOT NULL,
  owner_kind TEXT NOT NULL CHECK (owner_kind IN ('DECISION','OPERATION','DELEGATION','DISPOSITION')),
  owner_key TEXT NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('OPEN','UNKNOWN','SETTLED','RELEASED')),
  held_amount INTEGER NOT NULL CHECK (held_amount >= 0),
  used_amount INTEGER NOT NULL CHECK (used_amount >= 0),
  usage_revision INTEGER NOT NULL CHECK (usage_revision >= 0),
  document BLOB NOT NULL,
  PRIMARY KEY (namespace, task_key, reservation_key, dimension),
  FOREIGN KEY (namespace, task_key, dimension) REFERENCES rt_budget(namespace, task_key, dimension),
  CHECK (state NOT IN ('SETTLED','RELEASED') OR held_amount = 0)
);

CREATE TABLE rt_control_source (
  namespace TEXT NOT NULL,
  task_key TEXT NOT NULL,
  origin_task_key TEXT NOT NULL,
  pause_operation_id TEXT NOT NULL,
  revision INTEGER NOT NULL CHECK (revision > 0),
  state TEXT NOT NULL CHECK (state IN ('ACTIVE','CLEARED')),
  scope TEXT NOT NULL CHECK (scope IN ('SELF','SUBTREE')),
  document BLOB NOT NULL,
  PRIMARY KEY (namespace, task_key, origin_task_key, pause_operation_id),
  FOREIGN KEY (namespace, task_key) REFERENCES rt_task(namespace, task_key)
);
CREATE TABLE rt_wait (
  namespace TEXT NOT NULL,
  task_key TEXT NOT NULL,
  wait_key TEXT NOT NULL,
  kind TEXT NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('OPEN','RESOLVED','WITHDRAWN')),
  document BLOB NOT NULL,
  PRIMARY KEY (namespace, task_key, wait_key),
  FOREIGN KEY (namespace, task_key) REFERENCES rt_task(namespace, task_key)
);

CREATE TABLE rt_inbox (
  namespace TEXT NOT NULL,
  message_id TEXT NOT NULL,
  task_key TEXT NOT NULL,
  work_key TEXT NOT NULL,
  source_ref TEXT NOT NULL,
  source_revision TEXT NOT NULL,
  payload BLOB NOT NULL,
  payload_digest BLOB NOT NULL CHECK (length(payload_digest) = 32),
  state TEXT NOT NULL CHECK (state IN ('PERSISTED','APPLIED','QUARANTINED')),
  document BLOB NOT NULL,
  PRIMARY KEY (namespace, message_id),
  FOREIGN KEY (namespace, task_key, work_key) REFERENCES rt_work(namespace, task_key, work_key)
);
CREATE INDEX rt_inbox_pending ON rt_inbox(namespace, task_key, message_id) WHERE state = 'PERSISTED';
CREATE TABLE rt_outbox (
  namespace TEXT NOT NULL,
  message_id TEXT NOT NULL,
  task_key TEXT NOT NULL,
  work_key TEXT NOT NULL,
  operation_id TEXT,
  destination_ref TEXT NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('HELD','READY','PERSISTED','CLOSED')),
  payload BLOB NOT NULL,
  payload_digest BLOB NOT NULL CHECK (length(payload_digest) = 32),
  document BLOB NOT NULL,
  PRIMARY KEY (namespace, message_id),
  FOREIGN KEY (namespace, task_key, work_key) REFERENCES rt_work(namespace, task_key, work_key),
  FOREIGN KEY (namespace, task_key, operation_id) REFERENCES rt_operation(namespace, task_key, operation_id)
);
CREATE INDEX rt_outbox_ready ON rt_outbox(namespace, task_key, work_key) WHERE state = 'READY';

CREATE TABLE rt_commit (
  namespace TEXT NOT NULL,
  task_key TEXT NOT NULL,
  change_id TEXT NOT NULL,
  semantic_digest BLOB NOT NULL CHECK (length(semantic_digest) = 32),
  canonical_change BLOB NOT NULL,
  disposition TEXT NOT NULL CHECK (disposition IN ('COMMITTED','CLOSED_NOT_COMMITTED')),
  committed_version INTEGER NOT NULL CHECK (committed_version >= 0),
  receipt BLOB,
  created_ms INTEGER NOT NULL,
  PRIMARY KEY (namespace, task_key, change_id),
  CHECK (disposition != 'COMMITTED' OR receipt IS NOT NULL)
);

CREATE TABLE rt_handoff (
  namespace TEXT NOT NULL,
  task_key TEXT NOT NULL,
  handoff_key TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('OWNER_TRANSFER','DELEGATION','CONTROL_PROPAGATION','DELIVERY')),
  revision INTEGER NOT NULL CHECK (revision > 0),
  document BLOB NOT NULL,
  PRIMARY KEY (namespace, task_key, handoff_key),
  FOREIGN KEY (namespace, task_key) REFERENCES rt_task(namespace, task_key)
);
CREATE TABLE rt_journal (
  namespace TEXT NOT NULL,
  task_key TEXT NOT NULL,
  record_seq INTEGER NOT NULL CHECK (record_seq > 0),
  change_id TEXT NOT NULL,
  task_version INTEGER NOT NULL CHECK (task_version > 0),
  kind TEXT NOT NULL,
  document BLOB NOT NULL,
  PRIMARY KEY (namespace, task_key, record_seq),
  FOREIGN KEY (namespace, task_key) REFERENCES rt_task(namespace, task_key)
);
CREATE TABLE rt_closed_range (
  namespace TEXT NOT NULL,
  range_key TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('OPERATION','MESSAGE','CHANGE','CONTROL')),
  authority_ref TEXT NOT NULL,
  document BLOB NOT NULL,
  PRIMARY KEY (namespace, range_key, kind)
);
