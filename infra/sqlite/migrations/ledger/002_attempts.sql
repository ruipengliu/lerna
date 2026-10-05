-- 执行尝试、物理发送和观察（执行管理 2.1）。

CREATE TABLE attempts (
    user_id                TEXT    NOT NULL,
    attempt_id             TEXT    NOT NULL,
    operation_id           TEXT    NOT NULL,
    attempt_no             INTEGER NOT NULL,
    external_key           TEXT    NOT NULL,
    external_key_scope     TEXT    NOT NULL,
    key_valid_until        INTEGER,
    phase                  INTEGER NOT NULL,
    send_count             INTEGER NOT NULL,
    first_possible_send_at INTEGER,
    created_at             INTEGER NOT NULL,
    PRIMARY KEY (user_id, attempt_id),
    UNIQUE (user_id, operation_id, attempt_no)
);

-- 每次物理发送一条记录；dispatch_possible 在实际 I/O 之前持久写入（出口 P5）。
CREATE TABLE sends (
    user_id              TEXT    NOT NULL,
    operation_id         TEXT    NOT NULL,
    attempt_id           TEXT    NOT NULL,
    send_seq             INTEGER NOT NULL,
    purpose              INTEGER NOT NULL,
    claim_epoch          INTEGER NOT NULL,
    start_receipt        BLOB,
    dispatch_possible    INTEGER NOT NULL,
    dispatch_possible_at INTEGER,
    observed             INTEGER NOT NULL,
    created_at           INTEGER NOT NULL,
    PRIMARY KEY (user_id, attempt_id, send_seq)
);

-- 观察：可信出口在实际 I/O 处固定的原始观察和适配器的解释；按观察标识幂等。
CREATE TABLE observations (
    user_id        TEXT    NOT NULL,
    observation_id TEXT    NOT NULL,
    operation_id   TEXT    NOT NULL,
    attempt_id     TEXT    NOT NULL,
    send_seq       INTEGER NOT NULL,
    purpose        INTEGER NOT NULL,
    record         BLOB    NOT NULL,
    report         BLOB    NOT NULL,
    decided        INTEGER NOT NULL,
    created_at     INTEGER NOT NULL,
    PRIMARY KEY (user_id, observation_id)
);
