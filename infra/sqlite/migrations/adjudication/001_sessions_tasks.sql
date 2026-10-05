-- 裁决域：会话（core/sessions）与任务编排（core/tasks）。写入方分开，同处一个事务设施。

CREATE TABLE sessions (
    user_id            TEXT    NOT NULL,
    session_id         TEXT    NOT NULL,
    status             INTEGER NOT NULL,
    last_committed_seq INTEGER NOT NULL,
    revision           INTEGER NOT NULL,
    created_at         INTEGER NOT NULL,
    PRIMARY KEY (user_id, session_id)
);

CREATE TABLE session_inputs (
    user_id        TEXT    NOT NULL,
    input_id       TEXT    NOT NULL,
    session_id     TEXT    NOT NULL,
    session_seq    INTEGER NOT NULL,
    input_kind     INTEGER NOT NULL,
    task_id        TEXT    NOT NULL DEFAULT '',
    request_id     TEXT    NOT NULL DEFAULT '',
    body           TEXT    NOT NULL,
    requirements   BLOB,
    content_ref    TEXT    NOT NULL DEFAULT '',
    routing_status INTEGER NOT NULL,
    recorded_at    INTEGER NOT NULL,
    PRIMARY KEY (user_id, input_id),
    UNIQUE (user_id, session_id, session_seq)
);

CREATE TABLE session_tasks (
    user_id    TEXT    NOT NULL,
    session_id TEXT    NOT NULL,
    task_id    TEXT    NOT NULL,
    role       TEXT    NOT NULL,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (user_id, session_id, task_id)
);

CREATE TABLE input_requests (
    user_id       TEXT    NOT NULL,
    request_id    TEXT    NOT NULL,
    session_id    TEXT    NOT NULL,
    task_id       TEXT    NOT NULL,
    question      TEXT    NOT NULL,
    changes_basis INTEGER NOT NULL,
    open          INTEGER NOT NULL,
    created_at    INTEGER NOT NULL,
    PRIMARY KEY (user_id, request_id)
);

CREATE TABLE tasks (
    user_id              TEXT    NOT NULL,
    task_id              TEXT    NOT NULL,
    owner_domain_id      TEXT    NOT NULL,
    session_id           TEXT    NOT NULL,
    goal_input_id        TEXT    NOT NULL,
    goal_ref             TEXT    NOT NULL,
    requirements_version INTEGER NOT NULL,
    input_version        INTEGER NOT NULL,
    lifecycle            INTEGER NOT NULL,
    control              INTEGER NOT NULL,
    progress             INTEGER NOT NULL,
    waiting_on           BLOB,
    planning_generation  INTEGER NOT NULL,
    control_generation   INTEGER NOT NULL,
    revision             INTEGER NOT NULL,
    result_ref           TEXT    NOT NULL DEFAULT '',
    created_at           INTEGER NOT NULL,
    PRIMARY KEY (user_id, task_id),
    UNIQUE (user_id, goal_input_id)
);

-- 条件集快照：不可变；修改即新版本。接纳状态和 bound_input_version 随版本保存。
CREATE TABLE requirement_sets (
    user_id             TEXT    NOT NULL,
    task_id             TEXT    NOT NULL,
    version             INTEGER NOT NULL,
    status              INTEGER NOT NULL,
    bound_input_version INTEGER NOT NULL,
    record              BLOB    NOT NULL,
    created_at          INTEGER NOT NULL,
    PRIMARY KEY (user_id, task_id, version)
);
