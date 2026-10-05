-- 封闭先于意图被接纳：保存对准确动作的封闭，迟到的意图仍被接纳为责任，但不会被执行。
CREATE TABLE seals (
    user_id      TEXT    NOT NULL,
    operation_id TEXT    NOT NULL,
    task_id      TEXT    NOT NULL,
    reason       TEXT    NOT NULL,
    created_at   INTEGER NOT NULL,
    PRIMARY KEY (user_id, operation_id)
);
