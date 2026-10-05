-- 计费来源与用量回报（预算 2.2、2.3）。一个实际计费来源只计一次。

CREATE TABLE billing_sources (
    user_id        TEXT    NOT NULL,
    source_id      TEXT    NOT NULL,
    task_id        TEXT    NOT NULL,
    operation_id   TEXT    NOT NULL,
    reservation_id TEXT    NOT NULL,
    native_id      TEXT    NOT NULL DEFAULT '',
    external_key   TEXT    NOT NULL DEFAULT '',
    status         TEXT    NOT NULL,
    max_amount     INTEGER NOT NULL,
    portion        INTEGER NOT NULL,
    created_at     INTEGER NOT NULL,
    updated_at     INTEGER NOT NULL,
    PRIMARY KEY (user_id, source_id)
);
CREATE INDEX billing_sources_native ON billing_sources (user_id, native_id);

CREATE TABLE usage_reports (
    user_id     TEXT    NOT NULL,
    report_id   TEXT    NOT NULL,
    source_id   TEXT    NOT NULL,
    measurement INTEGER NOT NULL,
    amount      INTEGER NOT NULL,
    final       INTEGER NOT NULL,
    unknown     INTEGER NOT NULL,
    record      BLOB    NOT NULL,
    created_at  INTEGER NOT NULL,
    PRIMARY KEY (user_id, report_id)
);

-- 无法关联到来源的账单：保存后核对，不凭金额相等自动匹配。
CREATE TABLE unmatched_bills (
    user_id   TEXT    NOT NULL,
    report_id TEXT    NOT NULL,
    record    BLOB    NOT NULL,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (user_id, report_id)
);
