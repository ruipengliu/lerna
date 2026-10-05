-- 执行管理（core/ledger）：动作接纳、执行尝试、出口记录、效果与核对。

CREATE TABLE operations (
    user_id         TEXT    NOT NULL,
    operation_id    TEXT    NOT NULL,
    task_id         TEXT    NOT NULL,
    handoff_id      TEXT    NOT NULL,
    lifecycle       INTEGER NOT NULL,
    dispatch        INTEGER NOT NULL,
    effect          INTEGER NOT NULL,
    late_effect     INTEGER NOT NULL,
    ledger_revision INTEGER NOT NULL,
    record          BLOB    NOT NULL,
    intent          BLOB    NOT NULL,
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL,
    PRIMARY KEY (user_id, operation_id)
);
