-- 裁决域：提议、准入（core/tasks），授权（core/grants），预算（core/budget）。

CREATE TABLE proposal_requests (
    user_id             TEXT    NOT NULL,
    request_id          TEXT    NOT NULL,
    task_id             TEXT    NOT NULL,
    planning_generation INTEGER NOT NULL,
    snapshot            BLOB    NOT NULL,
    status              TEXT    NOT NULL,
    created_at          INTEGER NOT NULL,
    PRIMARY KEY (user_id, request_id),
    UNIQUE (user_id, task_id, planning_generation)
);

CREATE TABLE proposals (
    user_id             TEXT    NOT NULL,
    proposal_id         TEXT    NOT NULL,
    request_id          TEXT    NOT NULL,
    task_id             TEXT    NOT NULL,
    planning_generation INTEGER NOT NULL,
    kind                INTEGER NOT NULL,
    record              BLOB    NOT NULL,
    status              TEXT    NOT NULL,
    adjudications       INTEGER NOT NULL,
    created_at          INTEGER NOT NULL,
    PRIMARY KEY (user_id, proposal_id),
    UNIQUE (user_id, request_id)
);

-- 准入记录（动作意图）：记录准入依据，不保存效果。
CREATE TABLE admissions (
    user_id      TEXT    NOT NULL,
    admission_id TEXT    NOT NULL,
    task_id      TEXT    NOT NULL,
    operation_id TEXT    NOT NULL,
    origin       TEXT    NOT NULL,
    record       BLOB    NOT NULL,
    created_at   INTEGER NOT NULL,
    PRIMARY KEY (user_id, admission_id),
    UNIQUE (user_id, operation_id),
    UNIQUE (user_id, origin)
);

-- 任务编排对动作的视图：来自执行管理的通知，按修订接纳。
CREATE TABLE task_operations (
    user_id         TEXT    NOT NULL,
    task_id         TEXT    NOT NULL,
    operation_id    TEXT    NOT NULL,
    ledger_revision INTEGER NOT NULL,
    view            BLOB    NOT NULL,
    PRIMARY KEY (user_id, operation_id)
);

CREATE TABLE grants (
    user_id               TEXT    NOT NULL,
    grant_id              TEXT    NOT NULL,
    record                BLOB    NOT NULL,
    status                INTEGER NOT NULL,
    revocation_completion INTEGER NOT NULL,
    revocation_epoch      INTEGER NOT NULL,
    use_pool_id           TEXT    NOT NULL,
    state_version         INTEGER NOT NULL,
    created_at            INTEGER NOT NULL,
    PRIMARY KEY (user_id, grant_id)
);

-- 授权使用记录：动作使用的唯一键是"用户、消费来源、动作标识"。
CREATE TABLE grant_uses (
    user_id        TEXT    NOT NULL,
    use_id         TEXT    NOT NULL,
    use_pool_id    TEXT    NOT NULL,
    grant_id       TEXT    NOT NULL,
    operation_id   TEXT    NOT NULL,
    admission_id   TEXT    NOT NULL,
    request_digest BLOB    NOT NULL,
    state          TEXT    NOT NULL,
    created_at     INTEGER NOT NULL,
    PRIMARY KEY (user_id, use_id),
    UNIQUE (user_id, use_pool_id, operation_id)
);

-- 出口凭据：不透明引用，绑定准入、主体、出口、请求摘要和期限。
CREATE TABLE credentials (
    user_id          TEXT    NOT NULL,
    credential_id    TEXT    NOT NULL,
    operation_id     TEXT    NOT NULL,
    grant_id         TEXT    NOT NULL,
    use_id           TEXT    NOT NULL,
    audience         TEXT    NOT NULL,
    request_digest   BLOB    NOT NULL,
    issued_at        INTEGER NOT NULL,
    expires_at       INTEGER NOT NULL,
    revocation_epoch INTEGER NOT NULL,
    PRIMARY KEY (user_id, credential_id),
    UNIQUE (user_id, operation_id)
);

CREATE TABLE budgets (
    user_id       TEXT    NOT NULL,
    budget_id     TEXT    NOT NULL,
    scope         INTEGER NOT NULL,
    task_id       TEXT    NOT NULL,
    unit          TEXT    NOT NULL,
    limit_amount  INTEGER NOT NULL,
    limit_version INTEGER NOT NULL,
    used          INTEGER NOT NULL,
    held          INTEGER NOT NULL,
    closed        INTEGER NOT NULL,
    PRIMARY KEY (user_id, budget_id),
    UNIQUE (user_id, scope, task_id)
);

-- 预留：与准入原子创建；超时不自动释放。发送额度在开始门禁（开始-5）占用。
CREATE TABLE reservations (
    user_id        TEXT    NOT NULL,
    reservation_id TEXT    NOT NULL,
    admission_id   TEXT    NOT NULL,
    operation_id   TEXT    NOT NULL,
    task_id        TEXT    NOT NULL,
    budget_ids     TEXT    NOT NULL,
    unit           TEXT    NOT NULL,
    ceiling        INTEGER NOT NULL,
    reserved       INTEGER NOT NULL,
    remaining      INTEGER NOT NULL,
    send_quota     INTEGER NOT NULL,
    sends_used     INTEGER NOT NULL,
    query_quota    INTEGER NOT NULL,
    queries_used   INTEGER NOT NULL,
    state          TEXT    NOT NULL,
    created_at     INTEGER NOT NULL,
    PRIMARY KEY (user_id, reservation_id),
    UNIQUE (user_id, operation_id)
);
