-- 持久工作（core/durable）：每个事务域各有一份。

-- 命令回执：最小回执长期保留，M1 不设删除期限（持久工作 2.1）。
CREATE TABLE command_receipts (
    user_id             TEXT    NOT NULL,
    issuer_id           TEXT    NOT NULL,
    target_domain_id    TEXT    NOT NULL,
    command_id          TEXT    NOT NULL,
    command_kind        TEXT    NOT NULL,
    fingerprint_version TEXT    NOT NULL,
    fingerprint         BLOB    NOT NULL,
    phase               INTEGER NOT NULL,
    decision            INTEGER NOT NULL,
    commit_position     INTEGER NOT NULL UNIQUE,
    decided_at          INTEGER NOT NULL,
    receipt             BLOB    NOT NULL,
    PRIMARY KEY (user_id, issuer_id, target_domain_id, command_id)
);

-- 待办工作（持久工作 2.2）。
CREATE TABLE jobs (
    job_id      TEXT    NOT NULL PRIMARY KEY,
    user_id     TEXT    NOT NULL,
    kind        TEXT    NOT NULL,
    subject     TEXT    NOT NULL,
    purpose_key TEXT    NOT NULL,
    spec        BLOB,
    revision    INTEGER NOT NULL,
    state       TEXT    NOT NULL,
    not_before  INTEGER NOT NULL,
    claimer     TEXT,
    claim_epoch INTEGER NOT NULL,
    lease_until INTEGER,
    claim_count INTEGER NOT NULL,
    last_error  TEXT,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL,
    UNIQUE (user_id, purpose_key)
);
CREATE INDEX jobs_due ON jobs (state, not_before);

-- 领取回执：用原领取标识重试返回原清单。
CREATE TABLE claim_receipts (
    claim_id   TEXT    NOT NULL PRIMARY KEY,
    instance   TEXT    NOT NULL,
    claims     BLOB    NOT NULL,
    created_at INTEGER NOT NULL
);

-- 跨域交接记录（持久工作 2.3）。接收方用自己的命令回执去重，不另建收件箱。
CREATE TABLE handoffs (
    handoff_id       TEXT    NOT NULL PRIMARY KEY,
    user_id          TEXT    NOT NULL,
    source_domain_id TEXT    NOT NULL,
    target_domain_id TEXT    NOT NULL,
    command_kind     TEXT    NOT NULL,
    intent_ref       TEXT    NOT NULL,
    envelope         BLOB    NOT NULL,
    fingerprint      BLOB    NOT NULL,
    state            TEXT    NOT NULL,
    receipt          BLOB,
    created_at       INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL
);
