-- 确认（会话 2.2）：回应与消费分开记录；消费目标是互斥的类型联合，只能消费一次。
CREATE TABLE confirmations (
    user_id              TEXT    NOT NULL,
    confirmation_id      TEXT    NOT NULL,
    session_id           TEXT    NOT NULL,
    subject_kind         INTEGER NOT NULL,
    task_id              TEXT    NOT NULL,
    proposal_id          TEXT    NOT NULL,
    step_id              TEXT    NOT NULL,
    description          TEXT    NOT NULL,
    requirements_version INTEGER NOT NULL,
    input_version        INTEGER NOT NULL,
    intent_fingerprint   BLOB    NOT NULL,
    expires_at           INTEGER NOT NULL,
    status               INTEGER NOT NULL,
    consumed_kind        TEXT    NOT NULL DEFAULT '',
    consumed_ref         TEXT    NOT NULL DEFAULT '',
    created_at           INTEGER NOT NULL,
    responded_at         INTEGER,
    PRIMARY KEY (user_id, confirmation_id)
);
CREATE INDEX confirmations_by_proposal ON confirmations (user_id, proposal_id, step_id);
