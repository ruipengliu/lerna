-- 任务输入记录（会话 2.2）：由任务编排写，统一分配 task_input_seq；不是第二份正文。
CREATE TABLE task_inputs (
    user_id        TEXT    NOT NULL,
    task_id        TEXT    NOT NULL,
    task_input_seq INTEGER NOT NULL,
    input_id       TEXT    NOT NULL,
    input_kind     INTEGER NOT NULL,
    input_version  INTEGER NOT NULL,
    changes_basis  INTEGER NOT NULL,
    created_at     INTEGER NOT NULL,
    PRIMARY KEY (user_id, task_id, task_input_seq),
    UNIQUE (user_id, input_id)
);

ALTER TABLE input_requests ADD COLUMN answer_input_id TEXT NOT NULL DEFAULT '';
